package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/adapters/compiledb"
	makeadapter "github.com/example/sbomb/internal/adapters/make"
	"github.com/example/sbomb/internal/adapters/ninja"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/inventory"
)

// compileEvidence is what the compile-side adapters contribute: object to
// source mappings and the headers each object depended on.
type compileEvidence struct {
	// objectSources maps an object path, as recorded, to its source path.
	objectSources map[string]string
	// objectHeaders maps an object path to the headers its compilation read.
	objectHeaders map[string][]string
	// strategy records which adapter produced each object mapping.
	strategy map[string]string
}

func newCompileEvidence() *compileEvidence {
	return &compileEvidence{
		objectSources: map[string]string{},
		objectHeaders: map[string][]string{},
		strategy:      map[string]string{},
	}
}

func (c *compileEvidence) addSource(object, source, strategy string) {
	if object == "" || source == "" {
		return
	}
	if _, taken := c.objectSources[object]; taken {
		return
	}
	c.objectSources[object] = source
	c.strategy[object] = strategy
}

func (c *compileEvidence) addHeaders(object string, headers []string) {
	if object == "" || len(headers) == 0 {
		return
	}
	c.objectHeaders[object] = append(c.objectHeaders[object], headers...)
}

// collectCompileEvidence gathers object-to-source mappings and header
// dependencies from every adapter that can supply them, in the priority order
// of section 13.2. The first strategy to claim an object wins.
func collectCompileEvidence(buildDir string, commands []compiledb.Command, logger *Logger) *compileEvidence {
	evidence := newCompileEvidence()

	// Strategy 2: the Ninja build graph names the source of every object.
	if file, err := os.Open(filepath.Join(buildDir, "build.ninja")); err == nil {
		parsed, parseErr := ninja.ParseFile(file)
		file.Close()
		if parseErr == nil {
			for _, rule := range parsed.Rules {
				if len(rule.Outputs) == 0 || !isObjectPath(rule.Outputs[0]) {
					continue
				}
				for _, input := range rule.Inputs {
					if isSourcePath(input) {
						evidence.addSource(rule.Outputs[0], input, "ninja-buildgraph")
						break
					}
				}
			}
			logger.Debug("Ninja build graph contributed %d object mapping(s)", len(evidence.objectSources))
		} else {
			logger.Debug("build.ninja could not be parsed: %v", parseErr)
		}
	}

	// Strategy 4: the compile database names the output of every compilation.
	for _, command := range commands {
		if command.Output != "" {
			evidence.addSource(command.Output, command.File, "compile-commands-json")
		}
	}

	// The Makefiles generator supplies both mappings and header dependencies.
	if makeBuild, err := makeadapter.Parse(buildDir); err == nil {
		for _, target := range makeBuild.Targets {
			for object, source := range target.ObjectSources {
				evidence.addSource(object, source, "make-buildgraph")
			}
			for object, dependencies := range target.ObjectDeps {
				evidence.addHeaders(object, dependencies)
			}
		}
	}

	// Ninja records the headers each compilation read in its own deps log.
	if records, err := ninja.ParseDepsFile(filepath.Join(buildDir, ".ninja_deps")); err == nil {
		for _, record := range records {
			evidence.addHeaders(record.Output, record.Dependencies)
		}
		logger.Debug("Ninja deps log contributed header evidence for %d object(s)", len(records))
	}

	return evidence
}

// buildEvidenceGraph assembles the whole graph for one run: deliverables, link
// evidence, object-to-source mappings and header dependencies.
func buildEvidenceGraph(
	graph *evidence.Graph,
	b *builder,
	deliverables []Deliverable,
	compile *compileEvidence,
	buildDir string,
	mapPath, depfilePath string,
	logger *Logger,
) []domain.NodeID {
	b.loadNinjaArchiveInputs(buildDir)
	// A Makefiles target records both the objects it archives and the command
	// line that produced its artifact, in link.txt.
	if makeBuild, err := makeadapter.Parse(buildDir); err == nil {
		for _, target := range makeBuild.Targets {
			var archived bool
			for _, input := range target.LinkInputs {
				if strings.HasSuffix(input, ".a") || strings.HasSuffix(input, ".lib") {
					b.recordArchiveInputs(input, collectObjects(target.LinkInputs))
					archived = true
				}
			}
			if !archived {
				b.recordReconstructedLink(target.Name, target.LinkInputs)
			}
		}
	}

	artifactIDs := make([]domain.NodeID, 0, len(deliverables))
	for _, deliverable := range deliverables {
		canonical, _ := b.identify(deliverable.EvidencePath)
		artifactID := domain.NodeID("artifact:" + canonical)
		graph.AddNode(domain.Node{
			ID:         artifactID,
			Kind:       domain.NodeArtifact,
			Attributes: map[string]string{"role": deliverable.Role, "discoveredBy": deliverable.DiscoveredBy, "path": canonical},
		})
		artifactIDs = append(artifactIDs, artifactID)
		logger.Info("Final deliverable: %s (role %s, %s)", canonical, deliverable.Role, deliverable.DiscoveredBy)

		inputs := b.collectLinkEvidence(deliverable, mapPath, depfilePath)
		b.addLinkEdges(artifactID, inputs)
	}

	// Object to source, using the resolver of section 13.2.
	resolver := inventory.New(graph)
	for object, source := range compile.objectSources {
		objectCanonical, _ := b.identify(object)
		sourceCanonical, _ := b.identify(source)
		switch compile.strategy[object] {
		case "ninja-buildgraph":
			resolver.AddNinjaMapping(objectCanonical, sourceCanonical)
		case "compile-commands-json":
			resolver.AddCompileCommandsMapping(objectCanonical, sourceCanonical)
		case "make-buildgraph":
			resolver.AddCMakeMapping(objectCanonical, sourceCanonical)
		default:
			resolver.AddDepfileMapping(objectCanonical, sourceCanonical)
		}
	}
	resolved, _ := resolver.ResolveAndAddEdges()
	logger.Info("Resolved %d object(s) to their source", resolved)

	// The resolver records source-mapping edges but not the nodes they point
	// at; materialize them, otherwise the sources are reachable yet invisible.
	for _, edge := range graph.Edges() {
		if edge.Type != "source-mapping" {
			continue
		}
		if _, known := graph.Node(edge.To); known {
			continue
		}
		canonical := string(edge.To)
		kind := domain.NodeSource
		if isHeaderPath(canonical) {
			kind = domain.NodeHeader
		}
		_, scope := b.anchors.ScopeOfPath(b.logicalBuild, canonical)
		graph.AddNode(domain.Node{
			ID:         edge.To,
			Kind:       kind,
			File:       &domain.FileID{Anchor: anchorOf(canonical), RelPath: relOf(canonical)},
			Attributes: map[string]string{"scope": string(scope)},
		})
	}

	// Header dependencies hang off the object whose compilation read them.
	for object, headers := range compile.objectHeaders {
		objectCanonical, _ := b.identify(object)
		if _, known := graph.Node(domain.NodeID(objectCanonical)); !known {
			continue
		}
		for _, header := range dedupe(append([]string{}, headers...)) {
			if !isHeaderPath(header) {
				continue
			}
			headerCanonical, scope := b.identify(header)
			graph.AddNode(domain.Node{
				ID:         domain.NodeID(headerCanonical),
				Kind:       domain.NodeHeader,
				File:       &domain.FileID{Anchor: anchorOf(headerCanonical), RelPath: relOf(headerCanonical)},
				Attributes: map[string]string{"scope": string(scope)},
			})
			graph.AddEdge(domain.Edge{
				From: domain.NodeID(objectCanonical), To: domain.NodeID(headerCanonical),
				Type: "header-dependency", Strength: "derived", Confidence: domain.ConfidenceMedium,
				Source: "depfile", Adapter: "depfiles",
			})
		}
	}

	return artifactIDs
}

func collectObjects(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if isObjectPath(path) {
			out = append(out, path)
		}
	}
	return out
}

// usedFiles derives the SBOM's file set from reachability. This is the whole
// premise of the tool (section 4.1): a file belongs in the SBOM only when a
// chain of evidence connects it to a final deliverable.
func usedFiles(graph *evidence.Graph, artifactIDs []domain.NodeID) []domain.Node {
	reachable := map[domain.NodeID]bool{}
	for _, artifactID := range artifactIDs {
		for id := range graph.Reachable(artifactID) {
			reachable[id] = true
		}
	}
	out := make([]domain.Node, 0, len(reachable))
	for _, node := range graph.Nodes() {
		if !reachable[node.ID] || node.Kind == domain.NodeArtifact || node.Kind == domain.NodeProduct {
			continue
		}
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// representInSBOM applies the representation rules of sections 12 and 13.1:
// a transient build artifact whose inputs are fully represented is evidence
// only, and never becomes a file component.
func representInSBOM(graph *evidence.Graph, node domain.Node) (bool, string) {
	switch node.Kind {
	case domain.NodeSource, domain.NodeHeader:
		return true, ""
	case domain.NodeObject:
		// An object is transient when it lives in the build tree and its
		// source is known. Otherwise it must appear, so that nothing silently
		// disappears.
		if !strings.HasPrefix(string(node.ID), "build:") {
			return true, "prebuilt external object"
		}
		for _, edge := range graph.EdgesFrom(node.ID) {
			if edge.Type == "source-mapping" {
				return false, ""
			}
		}
		return true, "source unresolved"
	case domain.NodeArchive:
		if !strings.HasPrefix(string(node.ID), "build:") {
			return true, "prebuilt external archive"
		}
		return false, ""
	default:
		return true, ""
	}
}

// unresolvedObjects reports objects that reached the link but could not be
// traced to a source (section 13.1). Toolchain and system objects -- the C
// runtime startup files and the like -- are outside the SBOM by section 24.1,
// so demanding a source for them would be noise, not a finding.
func unresolvedObjects(graph *evidence.Graph, used []domain.Node, anchorResult *anchors.Result) []domain.Finding {
	findings := []domain.Finding{}
	for _, node := range used {
		if node.Kind != domain.NodeObject {
			continue
		}
		if !anchors.IncludedByDefault(scopeOfNode(node, anchorResult)) {
			continue
		}
		var resolved bool
		for _, edge := range graph.EdgesFrom(node.ID) {
			if edge.Type == "source-mapping" {
				resolved = true
			}
		}
		if resolved {
			continue
		}
		findings = append(findings, domain.Finding{
			ID:       "LINKED_OBJECT_SOURCE_UNRESOLVED",
			Severity: domain.SeverityWarning,
			Subject:  domain.Subject{Kind: "file", Ref: string(node.ID)},
			Message:  "the object reached the linker but no strategy mapped it to a source",
		})
	}
	return findings
}

// hashUsedFiles reads and hashes the files the SBOM will contain. Only
// evidence-selected files are read; the source tree is never walked
// (section 23).
func hashUsedFiles(files []domain.UsedFile, physical map[string]string, logger *Logger) ([]domain.UsedFile, []domain.Finding) {
	findings := []domain.Finding{}
	for index := range files {
		canonical := files[index].ID.Canonical()
		path := physical[canonical]
		if path == "" {
			files[index].Missing = true
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			files[index].Missing = true
			logger.Debug("File '%s' is not readable: %v", path, err)
			findings = append(findings, domain.Finding{
				ID:       "MISSING_FILE_HASH",
				Severity: domain.SeverityWarning,
				Subject:  domain.Subject{Kind: "file", Ref: canonical},
				Message:  "the file is not readable, so no hash could be computed",
			})
			continue
		}
		files[index].SizeBytes = info.Size()
	}
	hashed := inventory.HashUsedFilesWithOptions(files, inventory.HashOptions{
		Resolve: func(id domain.FileID) string { return physical[id.Canonical()] },
	})
	for index := range hashed {
		if len(hashed[index].Hashes) == 0 && !hashed[index].Missing {
			findings = append(findings, domain.Finding{
				ID:       "MISSING_FILE_HASH",
				Severity: domain.SeverityWarning,
				Subject:  domain.Subject{Kind: "file", Ref: hashed[index].ID.Canonical()},
				Message:  "no hash could be computed for the file",
			})
		}
	}
	return hashed, findings
}

func isObjectPath(path string) bool {
	return strings.HasSuffix(path, ".o") || strings.HasSuffix(path, ".obj")
}

func isHeaderPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".h", ".hh", ".hpp", ".hxx", ".inc", ".ipp", ".def":
		return true
	default:
		return false
	}
}

func isSourcePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".cc", ".cpp", ".cxx", ".c++", ".m", ".mm", ".s", ".asm", ".cu", ".rc":
		return true
	default:
		return false
	}
}

// describeCounts renders a stable summary of the graph for the log.
func describeCounts(nodes []domain.Node) string {
	counts := map[domain.NodeKind]int{}
	for _, node := range nodes {
		counts[node.Kind]++
	}
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		parts = append(parts, fmt.Sprintf("%s=%d", kind, counts[domain.NodeKind(kind)]))
	}
	return strings.Join(parts, " ")
}

// scopeOfNode reads the origin scope recorded on a node, defaulting to unknown.
func scopeOfNode(node domain.Node, anchorResult *anchors.Result) anchors.Scope {
	if node.Attributes != nil {
		if value, ok := node.Attributes["scope"]; ok && value != "" {
			return anchors.Scope(value)
		}
	}
	return anchorResult.Scope(domain.FileID{Anchor: anchorOf(string(node.ID)), RelPath: relOf(string(node.ID))})
}
