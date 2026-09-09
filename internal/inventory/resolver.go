package inventory

import (
	"fmt"
	"path"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

// ObjectSourceResolver maps object files to their source files using the algorithm
// specified in §13.2 of the sbomb specification.
//
// Strategies in priority order (first match wins):
// 1. CMake File API (derived, high confidence)
// 2. Ninja build rule (derived, high confidence)
// 3. MSBuild .tlog (derived, high confidence)
// 4. compile_commands.json (derived, high confidence)
// 5. Depfile adjacency (derived, medium/high confidence)
// 6. DWARF (derived, high confidence)
// 7. Build log fallback (weak, low confidence)
type ObjectSourceResolver struct {
	cmakeData   map[string]string // object -> source from CMake File API
	ninjaData   map[string]string // object -> source from Ninja
	msbuildData map[string]string // object -> source from MSBuild .tlog files
	depfileData map[string]string // object -> source from depfiles
	dwarfData   map[string]string // object -> source from DWARF
	compileData map[string]string // object -> source from compile_commands.json

	graph *evidence.Graph
}

// StrategyResult holds the outcome of resolving an object to a source.
type StrategyResult struct {
	SourceID domain.NodeID
	Strategy string
	Conflict string
	Error    error
}

// New creates a new ObjectSourceResolver.
func New(g *evidence.Graph) *ObjectSourceResolver {
	return &ObjectSourceResolver{
		cmakeData:   make(map[string]string),
		ninjaData:   make(map[string]string),
		msbuildData: make(map[string]string),
		depfileData: make(map[string]string),
		dwarfData:   make(map[string]string),
		compileData: make(map[string]string),
		graph:       g,
	}
}

// ResolveObjectSource resolves an object file to its source file.
// Returns (sourceID, strategyUsed, conflict, error).
// strategyUsed indicates which strategy produced the result.
// conflict is non-nil if several strategies named different sources; it names
// every one of them, so that the report can say what each strategy said rather
// than only that they disagreed.
func (r *ObjectSourceResolver) ResolveObjectSource(objID domain.NodeID) (domain.NodeID, string, *domain.Conflict, error) {
	objPath := string(objID)

	// Try strategies in priority order
	strategies := []struct {
		name string
		data map[string]string
	}{
		{"cmake-file-api", r.cmakeData},
		{"ninja-buildgraph", r.ninjaData},
		{"msbuild-tlog", r.msbuildData},
		{"compile-commands-json", r.compileData},
		{"depfile-adjacency", r.depfileData},
		{"dwarf", r.dwarfData},
	}

	var results []struct {
		strategy string
		source   string
	}

	for _, strat := range strategies {
		if source, ok := strat.data[objPath]; ok {
			results = append(results, struct {
				strategy string
				source   string
			}{strat.name, source})
			continue
		}
		// MSVC maps commonly report only the object basename (main.c.obj),
		// while compile databases and Ninja use CMakeFiles/.../main.c.obj.
		// Accept that shorthand only when it identifies one object uniquely;
		// duplicate basenames must remain unresolved rather than guessed.
		var basenameSource string
		matches := 0
		for objectPath, sourcePath := range strat.data {
			if identityBase(objectPath) != identityBase(objPath) {
				continue
			}
			basenameSource = sourcePath
			matches++
		}
		if matches == 1 {
			results = append(results, struct {
				strategy string
				source   string
			}{strat.name, basenameSource})
		}
	}

	if len(results) == 0 {
		// No evidence found - return error with weak confidence
		return "", "no-strategy", nil, fmt.Errorf("no source mapping found for object %s", objPath)
	}

	// Check for conflicts. Every disagreeing strategy is collected, not just
	// the first one: a report that named one of three answers would hide the
	// third, and the point of the report is that a reviewer can see all of
	// them.
	firstSource := results[0].source
	var conflict *domain.Conflict
	for i := 1; i < len(results); i++ {
		if results[i].source == firstSource {
			continue
		}
		if conflict == nil {
			conflict = &domain.Conflict{
				Field:   "source of this object",
				Subject: domain.Subject{Kind: "file", Ref: objPath},
				Sides:   []domain.ConflictSide{{Source: results[0].strategy, Value: results[0].source}},
				Winner:  results[0].strategy,
				Reason:  "the strategy order of section 13.2 puts it first",
			}
		}
		conflict.Sides = append(conflict.Sides, domain.ConflictSide{
			Source: results[i].strategy, Value: results[i].source,
		})
	}

	// Highest priority (first) strategy wins
	return domain.NodeID(results[0].source), results[0].strategy, conflict, nil
}

func identityBase(value string) string {
	if _, relative, ok := strings.Cut(value, ":"); ok {
		value = relative
	}
	return strings.ToLower(path.Base(strings.ReplaceAll(value, "\\", "/")))
}

// AddCMakeMapping adds a source mapping from CMake File API data.
func (r *ObjectSourceResolver) AddCMakeMapping(objectPath, sourcePath string) {
	r.cmakeData[objectPath] = sourcePath
}

// AddNinjaMapping adds a source mapping from Ninja build graph.
func (r *ObjectSourceResolver) AddNinjaMapping(objectPath, sourcePath string) {
	r.ninjaData[objectPath] = sourcePath
}

// AddMSBuildMapping adds a source mapping from MSBuild .tlog evidence.
func (r *ObjectSourceResolver) AddMSBuildMapping(objectPath, sourcePath string) {
	r.msbuildData[objectPath] = sourcePath
}

// AddDepfileMapping adds a source mapping from depfile adjacency.
func (r *ObjectSourceResolver) AddDepfileMapping(objectPath, sourcePath string) {
	r.depfileData[objectPath] = sourcePath
}

// AddDWARFMapping adds a source mapping from DWARF debug information.
func (r *ObjectSourceResolver) AddDWARFMapping(objectPath, sourcePath string) {
	r.dwarfData[objectPath] = sourcePath
}

// AddCompileCommandsMapping adds a source mapping from compile_commands.json.
func (r *ObjectSourceResolver) AddCompileCommandsMapping(objectPath, sourcePath string) {
	r.compileData[objectPath] = sourcePath
}

// ResolveAndAddEdges resolves all objects in the graph and adds source-mapping edges.
// For each linked object, attempts to resolve to a source and creates an edge.
// Returns the count of successfully resolved objects, one finding per object
// whose strategies disagreed, and any errors.
//
// A disagreement is reported and not punished: the confidence stays the one the
// winning strategy earns. Section 8.7 lists the reasons a confidence may be
// downgraded and a second, weaker answer is not among them -- a depfile and a
// compile database naming different sources for one object is a thing to look
// at, not a reason to trust the File API less.
func (r *ObjectSourceResolver) ResolveAndAddEdges() (int, []domain.Finding, error) {
	resolved := 0
	findings := make([]domain.Finding, 0)

	// Get all nodes in the graph
	nodes := r.graph.Nodes()

	// For each object node, attempt to resolve and add edges
	for _, node := range nodes {
		if node.Kind != domain.NodeObject {
			continue
		}

		srcID, strategy, conflict, err := r.ResolveObjectSource(node.ID)
		if err != nil {
			// Unresolved object - could emit finding here
			// For now, just skip
			continue
		}

		// Determine confidence based on strategy
		confidence := r.confidenceForStrategy(strategy)

		// Create and add source-mapping edge
		edge := domain.Edge{
			From:       node.ID,
			To:         srcID,
			Type:       "source-mapping",
			Strength:   "derived",
			Confidence: confidence,
			Source:     fmt.Sprintf("%s:object-source-mapping", strategy),
			Adapter:    "resolver",
		}

		if conflict != nil {
			// Record the conflict in attributes, in the winner-first order the
			// evidence dump has always shown it in, and report it: the
			// attribute reaches whoever reads the graph, the finding reaches
			// whoever reads the run.
			if edge.Attributes == nil {
				edge.Attributes = make(map[string]string)
			}
			strategies := make([]string, 0, len(conflict.Sides))
			for _, side := range conflict.Sides {
				strategies = append(strategies, side.Source)
			}
			edge.Attributes["conflictingStrategy"] = strings.Join(strategies, " vs ")
			if finding, ok := conflict.Finding("OBJECT_SOURCE_MAPPING_CONFLICT", domain.SeverityInfo); ok {
				findings = append(findings, finding)
			}
		}

		r.graph.AddEdge(edge)
		resolved++
	}

	return resolved, findings, nil
}

// confidenceForStrategy determines the confidence level for each resolution strategy.
func (r *ObjectSourceResolver) confidenceForStrategy(strategy string) domain.Confidence {
	// Per §13.2: strategies 1-6 are "derived", strategy 7 is "weak"
	// Confidence mapping per §8.6: derived = medium-to-high depending on source class
	switch strategy {
	case "cmake-file-api":
		return domain.ConfidenceHigh // structured-authoritative
	case "ninja-buildgraph":
		return domain.ConfidenceHigh // structured-secondary -> medium, but Ninja is quite reliable
	case "msbuild-tlog":
		return domain.ConfidenceHigh // structured-authoritative
	case "compile-commands-json":
		return domain.ConfidenceMedium // structured-secondary
	case "depfile-adjacency":
		return domain.ConfidenceMedium // structured-secondary
	case "dwarf":
		return domain.ConfidenceHigh // structured-authoritative
	case "build-log-fallback":
		return domain.ConfidenceLow // textual-fallback + weak strength
	default:
		return domain.ConfidenceUnknown
	}
}
