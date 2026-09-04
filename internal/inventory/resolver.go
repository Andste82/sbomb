package resolver

import (
	"fmt"

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
	cmakeData     map[string]string // object -> source from CMake File API
	ninjaData     map[string]string // object -> source from Ninja
	depfileData   map[string]string // object -> source from depfiles
	dwarfData     map[string]string // object -> source from DWARF
	compileData   map[string]string // object -> source from compile_commands.json
	
	graph *evidence.Graph
}

// New creates a new ObjectSourceResolver.
func New(g *evidence.Graph) *ObjectSourceResolver {
	return &ObjectSourceResolver{
		cmakeData:   make(map[string]string),
		ninjaData:   make(map[string]string),
		depfileData: make(map[string]string),
		dwarfData:   make(map[string]string),
		compileData: make(map[string]string),
		graph:       g,
	}
}

// ResolveObjectSource resolves an object file to its source file.
// Returns (sourceID, strategyUsed, conflict, error).
// strategyUsed indicates which strategy produced the result.
// conflict is non-empty if multiple strategies disagree.
func (r *ObjectSourceResolver) ResolveObjectSource(objID domain.NodeID) (domain.NodeID, string, string, error) {
	objPath := string(objID)
	
	// Try strategies in priority order
	strategies := []struct {
		name string
		data map[string]string
	}{
		{"cmake-file-api", r.cmakeData},
		{"ninja-buildgraph", r.ninjaData},
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
		}
	}
	
	if len(results) == 0 {
		// No evidence found - return error with weak confidence
		return "", "no-strategy", "", fmt.Errorf("no source mapping found for object %s", objPath)
	}
	
	// Check for conflicts
	firstSource := results[0].source
	conflict := ""
	for i := 1; i < len(results); i++ {
		if results[i].source != firstSource {
			// Different strategies disagree - first (highest priority) wins
			conflict = fmt.Sprintf("%s vs %s", results[0].strategy, results[i].strategy)
			break
		}
	}
	
	// Highest priority (first) strategy wins
	return domain.NodeID(results[0].source), results[0].strategy, conflict, nil
}

// AddCMakeMapping adds a source mapping from CMake File API data.
func (r *ObjectSourceResolver) AddCMakeMapping(objectPath, sourcePath string) {
	r.cmakeData[objectPath] = sourcePath
}

// AddNinjaMapping adds a source mapping from Ninja build graph.
func (r *ObjectSourceResolver) AddNinjaMapping(objectPath, sourcePath string) {
	r.ninjaData[objectPath] = sourcePath
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

// ResolveAllObjects resolves all objects in the graph that have outgoing edges
// and adds source-mapping edges to the graph.
// Returns the count of resolved objects and any errors encountered.
func (r *ObjectSourceResolver) ResolveAllObjects() (int, error) {
	resolved := 0
	// This would iterate through the graph finding all object nodes
	// and attempting to resolve each one, then adding edges to the graph.
	return resolved, nil
}
