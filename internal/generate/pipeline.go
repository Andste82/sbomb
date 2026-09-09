package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/adapters/archive"
	"github.com/example/sbomb/internal/adapters/binfmt"
	"github.com/example/sbomb/internal/adapters/compiledb"
	makeadapter "github.com/example/sbomb/internal/adapters/make"
	"github.com/example/sbomb/internal/adapters/ninja"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/headers"
	"github.com/example/sbomb/internal/inventory"
	"github.com/example/sbomb/internal/limits"
	"github.com/example/sbomb/internal/policy"
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
	// makeBuild is the Makefiles generator's build tree, parsed once. It names
	// object sources, link inputs and header dependencies, and reading it is
	// the most expensive single step of a large run.
	makeBuild *makeadapter.Build
	// objectForcedIncludes maps an object to the headers its compile command
	// forced in with -include / /FI. This is how a precompiled header reaches
	// a translation unit whose dependency file never mentions it (section 14.5).
	objectForcedIncludes map[string][]string
	// lto records that a compile command asked for link-time optimization
	// (section 17.2).
	lto bool
	// findings are what the compile-side adapters could not obtain, named
	// rather than passed over in silence (section 9.2).
	findings []domain.Finding
}

func newCompileEvidence() *compileEvidence {
	return &compileEvidence{
		objectSources:        map[string]string{},
		objectHeaders:        map[string][]string{},
		strategy:             map[string]string{},
		objectForcedIncludes: map[string][]string{},
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

// addDWARFMappings is strategy 6 of section 13.2: an object that no build
// record accounts for still carries, in its own debug information, the name of
// the translation unit it was compiled from.
//
// It is the only strategy that asks the object instead of the build system,
// which is why it is the one that answers for a prebuilt archive shipped by a
// vendor: nothing in the compile database, the build graph or a depfile
// mentions its members, because this build did not compile them.
//
// Only objects no earlier strategy claimed are read. Opening every object file
// of a fifty-thousand-unit build to parse DWARF would cost more than the whole
// run does today, and would tell us nothing we do not already know.
func addDWARFMappings(graph *evidence.Graph, b *builder, resolver *inventory.ObjectSourceResolver, mapped map[string]bool, logger *Logger) {
	inspected, found := 0, 0
	// Archives are parsed at most once each, however many of their members
	// need reading.
	archives := map[string][]archive.Member{}
	for _, node := range graph.Nodes() {
		if node.Kind != domain.NodeObject {
			continue
		}
		canonical := string(node.ID)
		if mapped[canonical] {
			continue
		}
		result, ok := inspectObject(node, b, archives, logger)
		if !ok {
			continue
		}
		inspected++
		// Exactly one unit, or the object is an amalgamation and naming one of
		// its sources would be a guess. A unity build is resolved by section
		// 17.1, not here.
		if len(result.CompilationUnits) != 1 {
			if len(result.CompilationUnits) > 1 {
				logger.Debug("Object '%s' holds %d translation units; DWARF cannot name one", canonical, len(result.CompilationUnits))
			}
			continue
		}
		unit := result.CompilationUnits[0]
		if unit.Source == "" {
			continue
		}
		sourceCanonical, _ := b.identify(unit.Source)
		resolver.AddDWARFMapping(canonical, sourceCanonical)
		found++
	}
	if inspected > 0 {
		logger.Info("Read debug information from %d unmapped object(s); %d named their source", inspected, found)
	}
}

// inspectObject reads one object's debug information, whether it is a file of
// its own or a member of a static archive. A prebuilt library is the second
// case, and it is the case that matters: a vendor ships an archive, and its
// members exist nowhere else.
func inspectObject(node domain.Node, b *builder, archives map[string][]archive.Member, logger *Logger) (binfmt.Result, bool) {
	canonical := string(node.ID)
	memberName := node.Attributes["member"]
	if memberName == "" {
		path := b.physical[canonical]
		if path == "" {
			return binfmt.Result{}, false
		}
		result, err := binfmt.Inspect(path, binfmt.Options{})
		if err != nil {
			logger.Debug("Object '%s' could not be read: %v", canonical, err)
			return binfmt.Result{}, false
		}
		return result, true
	}

	archiveCanonical := node.Attributes["archive"]
	archivePath := b.physical[archiveCanonical]
	if archivePath == "" {
		return binfmt.Result{}, false
	}
	members, parsed := archives[archiveCanonical]
	if !parsed {
		var err error
		members, err = archive.ParseFile(archivePath)
		if err != nil {
			logger.Debug("Archive '%s' could not be read: %v", archiveCanonical, err)
		}
		archives[archiveCanonical] = members
	}
	for _, member := range members {
		if member.Name != memberName || len(member.Content) == 0 {
			continue
		}
		return binfmt.InspectBytes(member.Content, archiveCanonical+"("+memberName+")", binfmt.Options{}), true
	}
	return binfmt.Result{}, false
}

// collectCompileEvidence gathers object-to-source mappings and header
// dependencies from every adapter that can supply them, in the priority order
// of section 13.2. The first strategy to claim an object wins.
//
// commandStrategy names where the compile lines came from, because they have
// two sources with two different standings: the compile database is strategy
// 4, while the same lines printed by `ninja -t commands` are the build graph
// itself and belong to strategy 2. The caller knows which it read; guessing
// here would put a file name on evidence that never came from a file.
func collectCompileEvidence(buildDir string, commands []compiledb.Command, commandStrategy string, logger *Logger) *compileEvidence {
	if commandStrategy == "" {
		commandStrategy = "compile-commands-json"
	}
	evidence := newCompileEvidence()
	if parsed, err := makeadapter.Parse(buildDir); err == nil {
		evidence.makeBuild = parsed
	} else {
		logger.Debug("Makefiles adapter does not apply: %v", err)
	}

	// Strategy 2: the Ninja build graph names the source of every object. That
	// the file opened is also what says this is a Ninja build, and a build that
	// is not one has no deps log that could be missing.
	ninjaBuild := false
	if file, err := os.Open(filepath.Join(buildDir, "build.ninja")); err == nil {
		ninjaBuild = true
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
	// When it was absent and `ninja -t commands` answered instead, the very
	// same loop feeds strategy 2, which is where section 13.2 puts that
	// command.
	for _, command := range commands {
		if command.Output != "" {
			evidence.addSource(command.Output, command.File, commandStrategy)
		}
		if hasLTOFlag(command.Arguments) {
			evidence.lto = true
		}
		if command.Output != "" {
			if forced := forcedIncludes(command.Arguments); len(forced) > 0 {
				evidence.objectForcedIncludes[command.Output] = forced
			}
		}
	}

	// The Makefiles generator supplies both mappings and header dependencies.
	// Parsed once and carried, because reading fifty thousand dependency files
	// twice per run is what it cost before.
	if makeBuild := evidence.makeBuild; makeBuild != nil {
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
	// A log that cannot be read used to be passed over in silence; it is now
	// named. There is no command to fall back on: `ninja -t deps` reads the
	// same file, and when it dislikes what it finds it rewrites it -- measured
	// against ninja 1.11, which truncates a damaged log and deletes one whose
	// header it does not accept. A tool pointed at somebody's build directory
	// reads it; it does not repair it (deviation D29).
	records, err := ninja.ParseDepsFile(filepath.Join(buildDir, ".ninja_deps"))
	if err != nil {
		if ninjaBuild {
			evidence.findings = append(evidence.findings, ninjaDepsUnavailableFinding(buildDir, err))
		}
		return evidence
	}
	for _, record := range records {
		evidence.addHeaders(record.Output, record.Dependencies)
	}
	logger.Debug("Ninja deps log contributed header evidence for %d object(s)", len(records))

	return evidence
}

// ninjaDepsUnavailableFinding names the evidence that could not be obtained,
// as section 9.2 requires of an adapter that degrades. Its remediation names
// the only thing that helps: the log is written while ninja builds, and
// nothing sbomb is allowed to run can reconstruct it.
func ninjaDepsUnavailableFinding(buildDir string, fileErr error) domain.Finding {
	return domain.Finding{
		ID:       "NINJA_DEPS_UNAVAILABLE",
		Severity: domain.SeverityInfo,
		Subject:  domain.Subject{Kind: "build", Ref: buildDir},
		Message: fmt.Sprintf("the Ninja deps log holding the headers each compilation read is unavailable: %v",
			fileErr),
		Remediation: "Build again so that ninja writes its deps log. It is the only record of which headers each " +
			"compilation read, and no permitted command can recover it: `ninja -t deps` reads the same file and " +
			"rewrites it when it cannot.",
	}
}

// graphOutcome carries what the graph construction learned beyond the graph
// itself: which translation units debug information covered, what DWARF
// narrowing removed, and which headers exist only behind a precompiled header.
type graphOutcome struct {
	artifactIDs []domain.NodeID
	dwarf       *dwarfEvidence
	narrowed    []narrowedHeader
	pchOnly     map[string]bool
	// excludedByGC are the objects removed because every section they
	// contributed was discarded (section 4.5, sectionGarbageCollection=exclude).
	excludedByGC map[string]bool
	findings     []domain.Finding
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
	project config.Config,
	cfg policy.Config,
	logger *Logger,
) graphOutcome {
	b.loadNinjaArchiveInputs(buildDir)
	// A Makefiles target records both the objects it archives and the command
	// line that produced its artifact, in link.txt.
	if makeBuild := compile.makeBuild; makeBuild != nil {
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

		inputs := b.collectLinkEvidence(deliverable, buildDir, mapPath, depfilePath)
		b.addLinkEdges(artifactID, inputs)
	}

	// Object to source, using the resolver of section 13.2.
	resolver := inventory.New(graph)
	mappedObjects := map[string]bool{}
	for object, source := range compile.objectSources {
		objectCanonical, _ := b.identify(object)
		sourceCanonical, _ := b.identify(source)
		mappedObjects[objectCanonical] = true
		switch compile.strategy[object] {
		case "ninja-buildgraph":
			resolver.AddNinjaMapping(objectCanonical, sourceCanonical)
		case "msbuild-tlog":
			resolver.AddMSBuildMapping(objectCanonical, sourceCanonical)
		case "compile-commands-json":
			resolver.AddCompileCommandsMapping(objectCanonical, sourceCanonical)
		case "make-buildgraph":
			resolver.AddCMakeMapping(objectCanonical, sourceCanonical)
		default:
			resolver.AddDepfileMapping(objectCanonical, sourceCanonical)
		}
	}
	addDWARFMappings(graph, b, resolver, mappedObjects, logger)
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
		scope := b.scopeOfCanonical(canonical)
		graph.AddNode(domain.Node{
			ID:         edge.To,
			Kind:       kind,
			File:       &domain.FileID{Anchor: anchorOf(canonical), RelPath: relOf(canonical)},
			Attributes: map[string]string{"scope": string(scope)},
		})
	}

	// Section 18: what a package or image manifest declares. It runs before
	// the reachability filter and adds only edges, so a manifest describing
	// something nothing delivers still contributes nothing.
	packagingFindings := addPackagingEvidence(graph, b, project, buildDir, deliverables, logger)

	// Section 17.1: a unity translation unit stands for several sources. Its
	// object maps to the generated aggregation file, which is not what belongs
	// in the bill of materials.
	unityFindings := addUnityMappings(graph, b, compile, logger)

	// Debug information says which translation units are really in the
	// artifact and which headers reached them (section 11.4). It is read after
	// the object-to-source mapping, because a compilation unit is identified by
	// its source and has to be attached to the object it produced.
	dwarf := inspectArtifacts(b, deliverables, logger)
	outcome := graphOutcome{artifactIDs: artifactIDs, dwarf: dwarf, findings: dwarf.Findings}
	outcome.findings = append(outcome.findings, unityFindings...)
	outcome.findings = append(outcome.findings, packagingFindings...)

	attachments := headerAttachments(graph, b, compile, dwarf)
	resolution := resolveHeaderEvidence(attachments, cfg.HeaderEvidence)
	outcome.narrowed = resolution.narrowed
	outcome.pchOnly = resolution.pchOnly
	outcome.findings = append(outcome.findings, resolution.findings...)

	excludePCH := cfg.PCHHeaders == "exclude"
	var excludedPCH int
	for _, edge := range resolution.edges {
		if edge.viaPCH && resolution.pchOnly[edge.header] && excludePCH {
			excludedPCH++
			continue
		}
		headerCanonical := edge.header
		scope := b.scopeOfCanonical(headerCanonical)
		graph.AddNode(domain.Node{
			ID:   domain.NodeID(headerCanonical),
			Kind: domain.NodeHeader,
			File: &domain.FileID{Anchor: anchorOf(headerCanonical), RelPath: relOf(headerCanonical)},
			Attributes: map[string]string{
				"scope": string(scope),
				// Section 14.4: the class, not the anchor, decides whether a
				// header belongs in the SBOM.
				"headerClass": string(b.classify(headerCanonical, scope == anchors.ScopeBuild)),
			},
		})
		attributes := map[string]string{}
		if edge.viaPCH {
			// The property records how the header reached the unit, which is
			// also what justifies the lower confidence (section 14.5).
			attributes["sbomb:evidence:header:viaPch"] = "true"
		}
		strength := domain.Strength("derived")
		if attributes["sbomb:evidence:header:viaPch"] == "true" && cfg.PCHHeaders == "annotate-only" {
			// The header stays, but the annotation has to be visible to the
			// weak-evidence gate rather than only in a property.
			strength = domain.Strength("weak")
		}
		graph.AddEdge(domain.Edge{
			From: domain.NodeID(edge.object), To: domain.NodeID(headerCanonical),
			Type: "header-dependency", Strength: strength, Confidence: edge.confidence,
			Source: edge.source, Adapter: adapterForHeaderSource(edge.source),
			Attributes: attributes,
		})
	}
	if excludedPCH > 0 {
		logger.Info("Excluded %d header(s) reached only through the precompiled header", excludedPCH)
		outcome.findings = append(outcome.findings, pchExcludedFinding(excludedPCH, b.logicalBuild))
	}
	if len(resolution.narrowed) > 0 {
		logger.Info("Headers excluded by DWARF narrowing: %d", len(resolution.narrowed))
	}

	// Section 17.3: LTO degrades symbol- and section-level attribution, so the
	// object-to-source edges keep their strategy but lose one confidence level.
	if dwarf.LTO || compile.lto {
		affected := graph.Downgrade("lto", func(edge domain.Edge) bool { return edge.Type == "source-mapping" })
		logger.Info("Link-time optimization detected: %d source mapping(s) downgraded", affected)
		if !dwarf.available() {
			outcome.findings = append(outcome.findings, domain.Finding{
				ID: "LTO_ATTRIBUTION_DEGRADED", Severity: domain.SeverityWarning,
				Subject:     domain.Subject{Kind: "build", Ref: b.logicalBuild},
				Message:     "the build used link-time optimization and no debug information is available to attribute the result",
				Remediation: "Build with -g, or with -ffat-lto-objects, so that object-level attribution survives.",
			})
		}
	}

	var gcFindings []domain.Finding
	outcome.excludedByGC, gcFindings = applySectionGC(graph, b, cfg, logger)
	outcome.findings = append(outcome.findings, gcFindings...)
	for canonical := range outcome.excludedByGC {
		outcome.findings = append(outcome.findings, domain.Finding{
			ID: "SECTION_GC_EXCLUDED", Severity: domain.SeverityInfo,
			Subject: domain.Subject{Kind: "file", Ref: canonical},
			Message: "every section this object contributed was discarded by the linker",
		})
	}

	return outcome
}

// applySectionGC implements section 4.5. An object counts as fully discarded
// only when the evidence enumerates both what was kept and what was dropped and
// the object appears solely among the dropped; partial information must never
// remove a file.
func applySectionGC(graph *evidence.Graph, b *builder, cfg policy.Config, logger *Logger) (map[string]bool, []domain.Finding) {
	if cfg.SectionGarbageCollection == "" || cfg.SectionGarbageCollection == "ignore" {
		return nil, nil
	}
	// Asking for section garbage collection when the evidence enumerates only
	// one half of the picture would silently do nothing, and section 4.5
	// forbids acting on partial information.
	if len(b.discardedObjects) == 0 || len(b.retainedObjects) == 0 {
		return nil, []domain.Finding{{
			ID: "SECTION_GC_INFO_UNAVAILABLE", Severity: domain.SeverityInfo,
			Subject: domain.Subject{Kind: "run", Ref: cfg.SectionGarbageCollection},
			Message: "section garbage collection was requested but the link evidence does not enumerate both retained and discarded sections",
		}}
	}
	excluded := map[string]bool{}
	for canonical := range b.discardedObjects {
		if b.retainedObjects[canonical] {
			continue
		}
		if _, known := graph.Node(domain.NodeID(canonical)); !known {
			continue
		}
		switch cfg.SectionGarbageCollection {
		case "annotate":
			match := func(edge domain.Edge) bool { return string(edge.To) == canonical && edge.Type == "link" }
			graph.SetEdgeAttribute("sbomb:evidence:link:fullyDiscarded", "true", match)
			graph.Downgrade("section-gc", match)
			logger.Debug("Object '%s' was fully discarded; annotated", canonical)
		case "exclude":
			excluded[canonical] = true
			logger.Debug("Object '%s' was fully discarded; excluded", canonical)
		}
	}
	return excluded, nil
}

// headerAttachments joins the two header sources onto the objects they belong
// to. A compilation unit is named by its source, so the object-to-source
// mapping is what connects debug information to the graph.
func headerAttachments(graph *evidence.Graph, b *builder, compile *compileEvidence, dwarf *dwarfEvidence) []headerAttachment {
	objectForSource := map[string]string{}
	sourceForObject := map[string]string{}
	for object, source := range compile.objectSources {
		objectCanonical, _ := b.identify(object)
		sourceCanonical, _ := b.identify(source)
		sourceForObject[objectCanonical] = sourceCanonical
		if _, taken := objectForSource[sourceCanonical]; !taken {
			objectForSource[sourceCanonical] = objectCanonical
		}
	}

	byObject := map[string]*headerAttachment{}
	attach := func(objectCanonical string) *headerAttachment {
		if existing, known := byObject[objectCanonical]; known {
			return existing
		}
		source := sourceForObject[objectCanonical]
		created := &headerAttachment{object: objectCanonical, source: source}
		byObject[objectCanonical] = created
		return created
	}

	for object, headers := range compile.objectHeaders {
		objectCanonical, _ := b.identify(object)
		if _, known := graph.Node(domain.NodeID(objectCanonical)); !known {
			continue
		}
		entry := attach(objectCanonical)
		for _, header := range dedupe(append([]string{}, headers...)) {
			if !isHeaderPath(header) {
				continue
			}
			headerCanonical, _ := b.identify(header)
			entry.depfileHeaders = append(entry.depfileHeaders, headerCanonical)
		}
	}

	// Section 14.5: a precompiled header reaches a translation unit through the
	// compile command, not through the dependency file. The generated
	// aggregation header is the only evidence of which headers it carries.
	for object, forced := range compile.objectForcedIncludes {
		objectCanonical, _ := b.identify(object)
		if _, known := graph.Node(domain.NodeID(objectCanonical)); !known {
			continue
		}
		entry := attach(objectCanonical)
		for _, include := range forced {
			includeCanonical, _ := b.identify(include)
			if !isPCHArtifact(includeCanonical) {
				continue
			}
			for _, included := range pchIncludes(b.physical[includeCanonical]) {
				headerCanonical, _ := b.identify(included)
				entry.pchHeaders = append(entry.pchHeaders, headerCanonical)
			}
		}
	}

	for source, headers := range dwarf.headersBySource {
		objectCanonical, known := objectForSource[source]
		if !known {
			continue
		}
		if _, inGraph := graph.Node(domain.NodeID(objectCanonical)); !inGraph {
			continue
		}
		entry := attach(objectCanonical)
		for _, header := range headers {
			if !isHeaderPath(header) {
				continue
			}
			entry.dwarfHeaders = append(entry.dwarfHeaders, header)
		}
		// Coverage is decided on the header subset, not on the file table as a
		// whole. A unity unit's file table, for instance, names the aggregated
		// sources and no header at all; narrowing the dependency file against
		// that would delete every header the unit demonstrably read.
		entry.dwarfCovered = len(entry.dwarfHeaders) > 0
	}

	out := make([]headerAttachment, 0, len(byObject))
	for _, entry := range byObject {
		out = append(out, *entry)
	}
	return out
}

func adapterForHeaderSource(source string) string {
	switch {
	case strings.HasPrefix(source, "debug-info"):
		return "binfmt"
	case source == "pch":
		return "pch"
	default:
		return "depfiles"
	}
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
func usedFiles(graph *evidence.Graph, artifactIDs []domain.NodeID, excluded map[string]bool) []domain.Node {
	reachable := map[domain.NodeID]bool{}
	for _, artifactID := range artifactIDs {
		for id := range graph.ReachableExcept(artifactID, excluded) {
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
	case domain.NodeSource:
		// Section 14.5: the precompiled-header aggregation source and its
		// object are transient build artifacts. The headers it pulls in are
		// what belongs in the bill of materials, not the generated glue.
		if isPCHArtifact(string(node.ID)) {
			return false, ""
		}
		return true, ""
	case domain.NodeHeader:
		if isPCHArtifact(string(node.ID)) {
			return false, ""
		}
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
func hashUsedFiles(files []domain.UsedFile, physical map[string]string, bounds limits.Config, logger *Logger) ([]domain.UsedFile, []domain.Finding) {
	findings := []domain.Finding{}
	for index := range files {
		canonical := files[index].ID.Canonical()
		path := physical[canonical]
		if path == "" {
			files[index].Missing = true
			continue
		}
		// Section 30.4 and 30.5: a file the evidence names may be a symbolic
		// link out of every anchor, and hashing it would follow the link.
		info, err := bounds.Stat(path)
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

// includedByPolicy decides whether a file of a given origin belongs in the
// SBOM. The defaults of section 24.1 apply unless a scope option of section
// 33.1 says otherwise.
func includedByPolicy(scope anchors.Scope, node domain.Node, cfg policy.Config) bool {
	// Section 18: assets are in scope by default and can be turned off, which
	// is a scope decision like any other and is counted when it removes
	// something.
	if node.Kind == domain.NodeAsset {
		return cfg.IncludeAssets
	}

	// A header is decided by its class (section 14.4), not by its anchor: a
	// vendored dependency and the project it sits in share an anchor, and a
	// toolchain installation holds both compiler and distribution headers.
	if node.Kind == domain.NodeHeader {
		class := headerClassOf(node)
		if headers.IncludedByDefault(class) {
			return true
		}
		return cfg.IncludeSystemHeaders
	}
	switch scope {
	case anchors.ScopeSystem:
		return cfg.SystemLibraries == "main-sbom" || cfg.SystemLibraries == "separate-component"
	case anchors.ScopeToolchain:
		return cfg.IncludeToolchainRuntime == "main-sbom" || cfg.IncludeToolchainRuntime == "separate-component"
	default:
		return anchors.IncludedByDefault(scope)
	}
}

// headerClassOf reads the class recorded on a header node, defaulting to
// unknown so that an unclassified header is included and flagged rather than
// silently dropped.
func headerClassOf(node domain.Node) headers.Class {
	if node.Attributes != nil {
		if value := node.Attributes["headerClass"]; value != "" {
			return headers.Class(value)
		}
	}
	return headers.ClassUnknown
}

// evidenceQualityFindings reports what the evidence could not establish, so
// that the gates of section 33.1 have something to act on.
func evidenceQualityFindings(graph *evidence.Graph, used []domain.Node, anchorResult *anchors.Result, cfg policy.Config) []domain.Finding {
	findings := []domain.Finding{}

	// Weak evidence must never pass as equivalent to linked evidence (8.4).
	for _, edge := range graph.Edges() {
		if edge.Strength != "weak" {
			continue
		}
		findings = append(findings, domain.Finding{
			ID: "WEAK_EVIDENCE", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "evidence", Ref: string(edge.To)},
			Message: fmt.Sprintf("the only evidence for this file is %s, which is a fallback source", edge.Source),
		})
	}

	for _, node := range used {
		scope := scopeOfNode(node, anchorResult)
		switch node.Kind {
		case domain.NodeHeader:
			// Section 14.4: an unclassifiable header is included and flagged.
			if headerClassOf(node) == headers.ClassUnknown {
				findings = append(findings, domain.Finding{
					ID: "UNKNOWN_HEADER_CLASS", Severity: domain.SeverityInfo,
					Subject: domain.Subject{Kind: "file", Ref: string(node.ID)},
					Message: "the header could not be classified; it is included and flagged for review",
				})
			}
		case domain.NodeObject:
			if !anchors.IncludedByDefault(scope) {
				continue
			}
			var hasSource, hasHeaders bool
			for _, edge := range graph.EdgesFrom(node.ID) {
				switch edge.Type {
				case "source-mapping":
					hasSource = true
				case "header-dependency":
					hasHeaders = true
				}
			}
			if hasSource && !hasHeaders {
				findings = append(findings, domain.Finding{
					ID: "MISSING_HEADER_DEPENDENCY_EVIDENCE", Severity: domain.SeverityWarning,
					Subject: domain.Subject{Kind: "file", Ref: string(node.ID)},
					Message: "the translation unit reached the link but no dependency evidence names the headers it read",
				})
			}
		case domain.NodeArchive:
			// Section 33.1: a prebuilt library that maps to no component is a
			// gap in the bill of materials, not a detail.
			if strings.HasPrefix(string(node.ID), "build:") || !anchors.IncludedByDefault(scope) {
				continue
			}
			findings = append(findings, domain.Finding{
				ID: "PREBUILT_LIBRARY_UNMAPPED", Severity: domain.SeverityWarning,
				Subject:     domain.Subject{Kind: "file", Ref: string(node.ID)},
				Message:     "a prebuilt library is linked but belongs to no configured component",
				Remediation: "Add a components[] entry naming the library, its version and its supplier.",
			})
		}
	}

	return findings
}

// hasLTOFlag reports link-time optimization from the compile flags
// (section 17.2). The ELF section scan of the binary adapter is the other
// source; either is sufficient.
func hasLTOFlag(arguments []string) bool {
	for _, argument := range arguments {
		switch {
		case argument == "-flto", strings.HasPrefix(argument, "-flto="):
			return true
		case argument == "/GL", argument == "/LTCG", strings.HasPrefix(argument, "/LTCG:"):
			return true
		}
	}
	return false
}

// addUnityMappings recovers the constituent sources of every unity translation
// unit and records them as source mappings (section 17.1). The generated
// aggregation file itself stays out of the bill of materials: it is build glue,
// not a source the product is made of.
func addUnityMappings(graph *evidence.Graph, b *builder, compile *compileEvidence, logger *Logger) []domain.Finding {
	findings := []domain.Finding{}
	objects := make([]string, 0, len(compile.objectSources))
	for object := range compile.objectSources {
		objects = append(objects, object)
	}
	sort.Strings(objects)

	for _, object := range objects {
		source := compile.objectSources[object]
		if !looksLikeUnityPath(source) {
			continue
		}
		objectCanonical, _ := b.identify(object)
		sourceCanonical, _ := b.identify(source)
		if _, known := graph.Node(domain.NodeID(objectCanonical)); !known {
			continue
		}
		dependencies := make([]string, 0, len(compile.objectHeaders[object]))
		for _, dependency := range compile.objectHeaders[object] {
			canonical, _ := b.identify(dependency)
			dependencies = append(dependencies, canonical)
		}
		tu := resolveUnityTU(b, objectCanonical, sourceCanonical, dependencies, b.physical[sourceCanonical])
		if len(tu.constituents) == 0 {
			logger.Info("Unity translation unit '%s': no strategy recovered its sources", sourceCanonical)
			findings = append(findings, unityUnresolvedFinding(objectCanonical))
			continue
		}
		for _, constituent := range dedupe(tu.constituents) {
			graph.AddNode(domain.Node{
				ID:         domain.NodeID(constituent),
				Kind:       domain.NodeSource,
				File:       &domain.FileID{Anchor: anchorOf(constituent), RelPath: relOf(constituent)},
				Attributes: map[string]string{"scope": string(b.scopeOfCanonical(constituent))},
			})
			graph.AddEdge(domain.Edge{
				From: domain.NodeID(objectCanonical), To: domain.NodeID(constituent),
				Type: "source-mapping", Strength: "derived", Confidence: tu.confidence,
				Source: tu.strategy, Adapter: "unity",
			})
		}
		logger.Info("Unity translation unit '%s': %d source(s) recovered by %s",
			sourceCanonical, len(tu.constituents), tu.strategy)
	}
	return findings
}

// forcedIncludes extracts the headers a compile command forces into the
// translation unit. CMake uses this to attach a precompiled header, and it is
// the only per-unit evidence that the PCH applies to that unit: the dependency
// file of a GCC build does not name the aggregation header at all.
func forcedIncludes(arguments []string) []string {
	out := make([]string, 0)
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "-include", argument == "-include-pch":
			if index+1 < len(arguments) {
				out = append(out, arguments[index+1])
				index++
			}
		case strings.HasPrefix(argument, "-include="):
			out = append(out, strings.TrimPrefix(argument, "-include="))
		case strings.HasPrefix(argument, "/FI"), strings.HasPrefix(argument, "-FI"):
			if value := argument[3:]; value != "" {
				out = append(out, value)
			}
		}
	}
	return out
}
