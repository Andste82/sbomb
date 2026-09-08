package generate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/adapters/compiledb"
	"github.com/example/sbomb/internal/adapters/manifest"
	"github.com/example/sbomb/internal/adapters/pkgmanager"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/buildinfo"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/headers"
	"github.com/example/sbomb/internal/inventory"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/limits"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/policy"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/google/uuid"
)

type Result struct {
	Graph    *evidence.Graph
	Findings []domain.Finding
	// Document is the format-neutral hand-off to a writer (section 36.1).
	Document *sbomwriter.Document
	// BOM is the CycloneDX rendering of Document, kept for callers that need
	// the serialized form directly.
	BOM cyclonedx.BOM
	// Adapters names the evidence sources that contributed, for the review
	// report (section 34 point 1).
	Adapters []string
	// HeaderNarrowing counts, per component, the headers that DWARF narrowing
	// removed. Section 4.4 requires the narrowing to be auditable rather than
	// silent, so the count is carried out of the run even though the headers
	// themselves are not in the document.
	HeaderNarrowing []NarrowingCount
}

// NarrowingCount is one component's share of the headers DWARF narrowing
// removed, with the headers themselves for --report-chains all.
type NarrowingCount struct {
	Component string
	Count     int
	Headers   []string
}

// Run assembles the currently available build evidence into one deterministic
// graph and CycloneDX document. Adapters contribute only evidence they can
// prove; missing evidence is returned as a finding for policy evaluation.
func Run(cfg config.Config, buildDir string, reproducible bool) (Result, error) {
	return RunWithOptions(cfg, buildDir, reproducible, Options{PathFlavor: pathmodel.DefaultFlavor()})
}

type Options struct {
	PathFlavor pathmodel.Flavor
	Verbosity  int
	LogWriter  io.Writer
	// RedactUnanchoredPaths replaces the identity of files that match no
	// anchor with a digest, per specification section 7.5.
	RedactUnanchoredPaths bool
	// Policy carries the scope options of section 33.1. They are discovery
	// settings, not verdicts: section 33.3 applies them before output and
	// requires each removal to be reported.
	Policy policy.Config
	// Introspection enables the command allowlist of section 9.2. Every group
	// is off unless the caller turned it on; an adapter that needs a command
	// it may not run degrades and says which evidence it could not obtain.
	Introspection exec.Features
	// Limits is the parser policy of section 30. The zero value is the
	// specified default, so a caller with no opinion still gets the bounds.
	Limits limits.Config
	// MapPath and LinkDepfilePath name the link evidence explicitly, for a
	// build whose map does not sit beside its artifact. They override
	// artifacts[].map and artifacts[].linkDepfile.
	MapPath         string
	LinkDepfilePath string
}

type Logger struct {
	Verbosity int
	Writer    io.Writer
}

func NewLogger(verbosity int, w io.Writer) *Logger {
	if w == nil && verbosity > 0 {
		w = os.Stdout
	}
	return &Logger{Verbosity: verbosity, Writer: w}
}

func (l *Logger) Info(format string, args ...any) {
	if l != nil && l.Verbosity >= 1 && l.Writer != nil {
		fmt.Fprintf(l.Writer, "[INFO] "+format+"\n", args...)
	}
}

func (l *Logger) Debug(format string, args ...any) {
	if l != nil && l.Verbosity >= 2 && l.Writer != nil {
		fmt.Fprintf(l.Writer, "[DEBUG] "+format+"\n", args...)
	}
}

func (l *Logger) Trace(format string, args ...any) {
	if l != nil && l.Verbosity >= 3 && l.Writer != nil {
		fmt.Fprintf(l.Writer, "[TRACE] "+format+"\n", args...)
	}
}

func RunWithOptions(cfg config.Config, buildDir string, reproducible bool, options Options) (Result, error) {
	if buildDir == "" {
		return Result{}, fmt.Errorf("build dir is required")
	}
	if options.PathFlavor == nil {
		options.PathFlavor = pathmodel.DefaultFlavor()
	}
	if options.Policy.Profile == "" {
		options.Policy = policy.DefaultConfig()
	}
	logger := NewLogger(options.Verbosity, options.LogWriter)
	logger.Info("Starting SBOM generation (build-dir: '%s', verbosity: level %d)", buildDir, options.Verbosity)

	findings := make([]domain.Finding, 0)

	// 1. Structured build metadata. The File API is the richest source there
	//    is and anchors everything that follows (section 10).
	var replyModel *cmakeapi.Model
	if replyDir, discoverErr := cmakeapi.DiscoverReplyDir(buildDir); discoverErr == nil {
		model, parseErr := cmakeapi.ParseReplyDir(replyDir)
		if parseErr == nil {
			replyModel = model
			logger.Info("Read CMake File API reply: %d configuration(s), %d toolchain(s)", len(model.Configurations), len(model.Toolchains))
		} else {
			logger.Info("CMake File API reply could not be read: %v", parseErr)
			findings = append(findings, domain.Finding{ID: "CMAKE_FILE_API_UNAVAILABLE", Severity: domain.SeverityWarning, Subject: domain.Subject{Kind: "build", Ref: buildDir}, Message: parseErr.Error()})
		}
	} else {
		logger.Info("No CMake File API reply in '%s'", buildDir)
		findings = append(findings, domain.Finding{ID: "CMAKE_FILE_API_UNAVAILABLE", Severity: domain.SeverityWarning, Subject: domain.Subject{Kind: "build", Ref: buildDir}, Message: "no CMake File API reply directory was found"})
	}

	// 2. Compile evidence, needed both for object mappings and for the
	//    --sysroot flag the anchor model looks for.
	compilePath := filepath.Join(buildDir, "compile_commands.json")
	commands, err := compiledb.ParseFile(compilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return Result{}, fmt.Errorf("parse compile database: %w", err)
		}
		logger.Info("compile_commands.json not found in '%s'", buildDir)
		findings = append(findings, domain.Finding{ID: "MISSING_COMPILE_EVIDENCE", Severity: domain.SeverityWarning, Subject: domain.Subject{Kind: "build", Ref: buildSubject(cfg, buildDir)}, Message: "compile_commands.json was not found"})
	} else {
		logger.Info("Discovered %d compile command(s)", len(commands))
	}

	for _, manifestPath := range cfg.Manifests {
		path := manifestPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(cfg.Project.Root, path)
		}
		if _, manifestErr := manifest.ParseFile(path); manifestErr != nil {
			id := "MISSING_PACKAGE_EVIDENCE"
			if errors.Is(manifestErr, manifest.ErrInputLimitExceeded) {
				id = "INPUT_LIMIT_EXCEEDED"
			}
			findings = append(findings, domain.Finding{ID: id, Severity: domain.SeverityWarning, Subject: domain.Subject{Kind: "configuration", Ref: manifestPath}, Message: manifestErr.Error()})
		}
	}

	// 3. Anchors, so that every path below has a portable identity.
	compileFlags := make([]string, 0)
	for _, command := range commands {
		compileFlags = append(compileFlags, command.Arguments...)
	}
	buildRootForIdentity := cfg.Build.Dir
	if buildRootForIdentity == "" && replyModel != nil {
		buildRootForIdentity = replyModel.BuildRoot
	}
	if buildRootForIdentity == "" {
		buildRootForIdentity = absolutePath(buildDir)
	}
	projectRootForIdentity := cfg.Project.Root
	if projectRootForIdentity == "." {
		projectRootForIdentity = ""
	}
	if projectRootForIdentity == "" && replyModel != nil {
		projectRootForIdentity = replyModel.SourceRoot
	}
	if projectRootForIdentity == "" {
		projectRootForIdentity = absolutePath(".")
	}
	// Package managers are consulted before the anchors are assembled, because
	// an installed dependency needs an anchor of its own (section 21): without
	// one its files keep the absolute path of a package cache, which is
	// neither portable nor the same on the next machine.
	runner := &exec.Runner{
		Features: options.Introspection,
		Anchors:  []string{projectRootForIdentity, buildRootForIdentity, absolutePath(buildDir)},
		Log: func(record exec.Record) {
			logger.Info("Introspection: %s (%s)", strings.Join(record.Argv, " "), record.Duration.Round(time.Millisecond))
		},
	}
	packages, packageFindings := pkgmanager.Discover(pkgmanager.Options{
		BuildDir: buildDir,
		// The source root the File API reports, not the configured one: a run
		// without a configuration file still has to find .gitmodules, and the
		// build system knows where it configured from.
		SourceDir: projectRootForIdentity,
		Runner:    runner,
	})
	findings = append(findings, packageFindings...)
	packageAnchors := make([]anchors.PackageAnchor, 0, len(packages))
	for _, entry := range packages {
		logger.Info("Package %s %s from %s at %s", entry.Name, entry.Version, entry.Manager, entry.Root)
		if entry.AnchorKey != "" && entry.Root != "" {
			packageAnchors = append(packageAnchors, anchors.PackageAnchor{Key: entry.AnchorKey, Root: entry.Root})
		}
	}

	anchorResult, err := anchors.Assemble(anchors.Options{
		Flavor:        options.PathFlavor,
		ProjectRoot:   projectRootForIdentity,
		BuildRoot:     buildRootForIdentity,
		ConfigAnchors: cfg.Anchors,
		Packages:      packageAnchors,
		Model:         replyModel,
		CompileFlags:  compileFlags,
		Redact:        options.RedactUnanchoredPaths,
	})
	if err != nil {
		return Result{}, fmt.Errorf("assemble anchors: %w", err)
	}
	findings = append(findings, anchorResult.Findings...)
	for _, anchor := range anchorResult.Registry.Anchors() {
		logger.Debug("Anchor %s -> %s (from %s)", anchor.Key, anchor.Root, anchor.Source)
	}

	// 4. What is this SBOM about? (section 5)
	graph := evidence.New()
	deliverables, deliverableFindings, err := resolveDeliverables(cfg, buildDir, replyModel, logger)
	findings = append(findings, deliverableFindings...)
	if err != nil {
		var exit *ExitError
		if errors.As(err, &exit) {
			findings = append(findings, exit.Finding)
			return Result{Graph: graph, Findings: findings}, err
		}
		return Result{}, err
	}

	// 5. Build the evidence graph from link and compile evidence.
	b := newBuilder(graph, anchorResult, buildRootForIdentity, buildDir, logger)
	b.headerClass = headers.New(anchorResult.ImplicitIncludeDirs, toolchainRoots(anchorResult), componentRoots(cfg))
	compile := collectCompileEvidence(buildDir, commands, logger)
	mapPath, depfilePath := "", ""
	if len(cfg.Artifacts) > 0 {
		mapPath, depfilePath = cfg.Artifacts[0].Map, cfg.Artifacts[0].LinkDepfile
	}
	// The command line wins, so a one-off run can name evidence that lies
	// somewhere the configuration does not describe.
	if options.MapPath != "" {
		mapPath = options.MapPath
	}
	if options.LinkDepfilePath != "" {
		depfilePath = options.LinkDepfilePath
	}
	// A named path that is not there is a wrong answer, not a missing one.
	// Section 11.2 lets evidence be discovered beside the artifact, and that
	// fallback is right when nobody said where to look -- but silently reading
	// a different file than the one the caller named would make the document
	// describe evidence nobody asked for.
	if err := requireConfiguredEvidence(mapPath, depfilePath); err != nil {
		var exit *ExitError
		if errors.As(err, &exit) {
			findings = append(findings, exit.Finding)
		}
		return Result{Graph: graph, Findings: findings}, err
	}
	outcome := buildEvidenceGraph(graph, b, deliverables, compile, buildDir, mapPath, depfilePath, cfg, options.Policy, logger)
	artifactIDs := outcome.artifactIDs
	findings = append(findings, b.Findings()...)
	findings = append(findings, outcome.findings...)
	logger.Info("Evidence graph: %d node(s), %d edge(s) [%s]", len(graph.Nodes()), len(graph.Edges()), describeCounts(graph.Nodes()))

	// 6. The reachability filter. This is what makes the output evidence-based
	//    rather than a listing of everything the adapters happened to see.
	reachable := usedFiles(graph, artifactIDs, outcome.excludedByGC)
	logger.Info("Reachable from a deliverable: %d node(s) [%s]", len(reachable), describeCounts(reachable))
	findings = append(findings, unresolvedObjects(graph, reachable, anchorResult)...)
	findings = append(findings, evidenceQualityFindings(graph, reachable, anchorResult, options.Policy)...)

	// 7. Inventory: scope filter, representation rules, hashing.
	excludedByScope := map[anchors.Scope]int{}
	used := make([]domain.UsedFile, 0, len(reachable))
	for _, node := range reachable {
		if outcome.excludedByGC[string(node.ID)] {
			logger.Debug("Excluded fully discarded object '%s'", node.ID)
			continue
		}
		scope := scopeOfNode(node, anchorResult)
		if !includedByPolicy(scope, node, options.Policy) {
			excludedByScope[scope]++
			logger.Debug("Excluded %s file '%s'", scope, node.ID)
			continue
		}
		represent, reason := representInSBOM(graph, node)
		if options.Policy.IncludeTransientBuildArtifacts {
			represent, reason = true, "includeTransientBuildArtifacts"
		}
		if !represent {
			logger.Debug("Transient build artifact '%s' is evidence only", node.ID)
			continue
		}
		if reason != "" {
			logger.Debug("Keeping '%s' as a component: %s", node.ID, reason)
		}
		canonical := string(node.ID)
		logger.Trace("Used file: %s (%s, scope %s)", canonical, node.Kind, scope)
		properties := map[string][]string{"sbomb:component:scope": {string(scope)}}
		if node.Kind == domain.NodeHeader {
			// Section 14.4: the class that decided inclusion is part of the
			// record, so a reviewer can see why a header is here.
			properties["sbomb:evidence:header:class"] = []string{string(headerClassOf(node))}
		}
		used = append(used, domain.UsedFile{
			ID:         domain.FileID{Anchor: anchorOf(canonical), RelPath: relOf(canonical)},
			Class:      fileClassOf(node),
			Properties: properties,
		})
	}
	used = inventory.MergeUsedFiles(used)
	used, hashFindings := hashUsedFiles(used, b.physical, options.Limits, logger)
	findings = append(findings, hashFindings...)

	// Staleness: the hashes describe the files as they are now, which is only
	// meaningful when the build is not out of date (section 27).
	artifactPaths := make([]string, 0, len(deliverables))
	for _, deliverable := range deliverables {
		artifactPaths = append(artifactPaths, deliverable.Path)
	}
	if staleFindings, staleErr := inventory.DetectStaleness(used, artifactPaths, "",
		func(id domain.FileID) string { return b.physical[id.Canonical()] }); staleErr == nil {
		findings = append(findings, staleFindings...)
	}

	for _, scope := range []anchors.Scope{anchors.ScopeToolchain, anchors.ScopeSystem} {
		if count := excludedByScope[scope]; count > 0 {
			logger.Info("Excluded %d %s file(s) from the SBOM (section 24.1 default)", count, scope)
			findings = append(findings, domain.Finding{
				ID: "DYNAMIC_DEPENDENCIES_IGNORED", Severity: domain.SeverityInfo,
				Subject: domain.Subject{Kind: "build", Ref: buildRootForIdentity},
				Message: fmt.Sprintf("%d %s file(s) were excluded by the default inclusion policy", count, scope),
			})
		}
	}

	// 8. Graph invariants must hold before anything is written (section 8.8).
	if invariantErr := graph.CheckInvariants(); invariantErr != nil {
		return Result{Graph: graph, Findings: findings}, &ExitError{
			Code: 70,
			Finding: domain.Finding{
				ID: "INTERNAL_INVARIANT_VIOLATION", Severity: domain.SeverityError,
				Subject: domain.Subject{Kind: "run", Ref: buildRootForIdentity}, Message: invariantErr.Error(),
			},
		}
	}

	// 9. Hand the resolved facts to a writer.
	run := sbomwriter.RunMetadata{
		ToolName:     buildinfo.Name,
		ToolVendor:   buildinfo.Vendor,
		ToolVersion:  buildinfo.Version,
		Reproducible: reproducible,
		Timestamp:    buildTimestamp(),
	}
	if replyModel != nil {
		run.Generator = replyModel.Cache["CMAKE_GENERATOR"]
		run.BuildConfig = replyModel.Cache["CMAKE_BUILD_TYPE"]
	}
	if reproducible {
		findings = append(findings, domain.Finding{
			ID: "REPRODUCIBLE_MODE_OMITS_TIMESTAMP", Severity: domain.SeverityInfo,
			Subject: domain.Subject{Kind: "run", Ref: buildRootForIdentity},
			Message: "the document omits metadata.timestamp and is therefore not a CRA deliverable SBOM",
		})
	}

	anchorRoots := map[string]string{}
	for _, anchor := range anchorResult.Registry.Anchors() {
		anchorRoots[anchor.Key] = anchor.Root
	}
	resolver := newComponentResolver(cfg, b.physical, anchorRoots, logger)
	resolver.setPackages(packages, func(root string) domain.FileID {
		// A root below the build directory being read has to be expressed in
		// the logical build root first (section 7.6), exactly as every other
		// path the adapters hand over. A root outside it -- a package cache --
		// is already absolute and resolves against its own anchor.
		if relative, err := filepath.Rel(buildDir, root); err == nil && !strings.HasPrefix(relative, "..") {
			root = relative
		}
		canonical, _ := b.identify(root)
		return domain.FileID{Anchor: anchorOf(canonical), RelPath: relOf(canonical)}
	})
	if replyModel != nil {
		resolver.setTargets(targetsByFile(replyModel, b))
	}
	narrowing := narrowingByComponent(resolver, outcome.narrowed)
	// The build system already says what the project is called and which
	// version it is; writing that into the configuration a second time is a
	// chance for the two to disagree. A configured value still wins.
	projectVersionSource := projectFromCMake(&cfg, replyModel)
	if projectVersionSource == "cmake" {
		logger.Info("Project version %q read from CMAKE_PROJECT_VERSION", cfg.Project.Version)
	}
	document, findings := buildDocument(cfg, projectVersionSource, resolver, deliverables, used, findings, run)
	// The configuration names the serialization; an empty value is the
	// writer's default rather than a guess made here (section 32.2).
	format := cfg.Output.Format
	if format == "" {
		format = "cyclonedx-json"
	}
	writer, specVersion, err := sbomwriter.Resolve(format, cfg.Output.SpecVersion)
	if err != nil {
		return Result{Graph: graph, Findings: findings}, err
	}
	bom, err := writer.(cyclonedx.Writer).Build(document, sbomwriter.Options{
		SpecVersion:  specVersion,
		TLP:          cfg.Output.TLP,
		Reproducible: reproducible,
	})
	if err != nil {
		return Result{Graph: graph, Findings: findings}, &ExitError{
			Code: 70,
			Finding: domain.Finding{
				ID: "INTERNAL_INVARIANT_VIOLATION", Severity: domain.SeverityError,
				Subject: domain.Subject{Kind: "run", Ref: buildRootForIdentity}, Message: err.Error(),
			},
		}
	}
	if reproducible {
		bom.SerialNumber = cyclonedx.ReproducibleSerialNumber(bom)
	} else {
		bom.SerialNumber = "urn:uuid:" + uuid.NewString()
	}

	logger.Info("CycloneDX %s BOM constructed: %d component(s) in %d group(s)", specVersion, len(bom.Components), len(document.Components))
	adapters := map[string]bool{}
	for _, edge := range graph.Edges() {
		if edge.Adapter != "" {
			adapters[edge.Adapter] = true
		}
	}
	adapterNames := make([]string, 0, len(adapters))
	for name := range adapters {
		adapterNames = append(adapterNames, name)
	}
	sort.Strings(adapterNames)

	return Result{
		Graph: graph, Findings: findings, Document: document, BOM: bom,
		Adapters: adapterNames, HeaderNarrowing: narrowing,
	}, nil
}

// addProperty appends a value to a multi-valued property map.
func addProperty(properties map[string][]string, name, value string) map[string][]string {
	if value == "" {
		return properties
	}
	if properties == nil {
		properties = map[string][]string{}
	}
	properties[name] = append(properties[name], value)
	return properties
}

func fileClassOf(node domain.Node) domain.FileClass {
	// A manifest states what an input is, which is better than inferring it
	// from the node kind (section 18).
	if class, ok := packagingClassOf(node); ok {
		return class
	}
	switch node.Kind {
	case domain.NodeHeader:
		return domain.FileClassHeader
	case domain.NodeObject:
		return domain.FileClassObject
	case domain.NodeArchive:
		return domain.FileClassArchive
	case domain.NodeAsset:
		return domain.FileClassAsset
	default:
		return domain.FileClassSource
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// fileLicenses resolves the licenses of one used file. It is the only place
// that reads file bytes for licensing, and only for evidence-selected files
// (section 22.1).
func fileLicenses(path string, logger *Logger) []domain.LicenseFinding {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	finding := resolveLicense(path, data, err, logger)
	if finding.Name == "" && finding.Expression == "" {
		return nil
	}
	return []domain.LicenseFinding{finding}
}

// buildSubject names the build in a finding using the identity the evidence
// carries, so that a finding does not embed the directory the run read from.
func buildSubject(cfg config.Config, buildDir string) string {
	if cfg.Build.Dir != "" {
		return cfg.Build.Dir
	}
	return buildDir
}

func resolveLicense(path string, data []byte, readErr error, logger *Logger) domain.LicenseFinding {
	if readErr == nil {
		finding := license.ResolveFromText(string(data), path)
		if finding.Expression != "" {
			logger.Debug("License for '%s': resolved SPDX header '%s'", path, finding.Expression)
			return finding
		}
	}
	for dir := filepath.Dir(path); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		logger.Trace("Scanning parent directory '%s' for license files for '%s'...", dir, path)
		for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING", "COPYING.txt", "NOTICE"} {
			licensePath := filepath.Join(dir, name)
			finding, err := license.ResolveFile(licensePath)
			if err == nil && finding.Expression != "" {
				logger.Debug("License for '%s': resolved '%s' from parent file '%s'", path, finding.Expression, licensePath)
				return finding
			}
		}
	}
	logger.Trace("License for '%s': NOASSERTION (reason: %s)", path, license.ReasonNoEvidence)
	return domain.LicenseFinding{Name: "NOASSERTION", Evidence: "unknown", Reason: license.ReasonNoEvidence}
}

// absolutePath makes a path absolute without failing: an anchor root that
// stayed relative would match nothing.
func absolutePath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

func buildTimestamp() string {
	if value := os.Getenv("SOURCE_DATE_EPOCH"); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
		}
	}
	return time.Now().UTC().Format(time.RFC3339)
}

// requireConfiguredEvidence refuses a linker map or link dependency file that
// was named and is not there.
//
// The paths reach here from --map and --link-depfile, and from artifacts[].map
// and artifacts[].linkDepfile in the configuration. Either way somebody wrote
// the path down, so its absence is a configuration error of the same kind as
// MISSING_ARTIFACT rather than evidence that could not be collected.
func requireConfiguredEvidence(mapPath, depfilePath string) error {
	for _, named := range []struct{ kind, path, remediation string }{
		{"linker map", mapPath, "Correct artifacts[].map or --map, or leave it unset and let sbomb look beside the artifact."},
		{"link dependency file", depfilePath, "Correct artifacts[].linkDepfile or --link-depfile, or leave it unset and let sbomb look beside the artifact."},
	} {
		if named.path == "" {
			continue
		}
		if info, err := os.Stat(named.path); err == nil && !info.IsDir() {
			continue
		}
		return &ExitError{
			Code: 2,
			Finding: domain.Finding{
				ID:          "CONFIGURED_EVIDENCE_MISSING",
				Severity:    domain.SeverityError,
				Subject:     domain.Subject{Kind: "configuration", Ref: named.path},
				Message:     fmt.Sprintf("the configured %s does not exist", named.kind),
				Remediation: named.remediation,
			},
		}
	}
	return nil
}

// targetsByFile records which CMake target owns which file, for strategy 5 of
// section 19.2. The File API states it; nothing here infers anything.
//
// A file two targets both list is dropped rather than assigned to one of them.
// That happens for a source compiled into two targets, and the build system
// having said two things is not a licence to pick one.
func targetsByFile(model *cmakeapi.Model, b *builder) map[string]string {
	byFile := map[string]string{}
	contested := map[string]bool{}
	for _, configuration := range model.Configurations {
		for _, target := range configuration.Targets {
			for _, source := range target.Sources {
				path := source.Path
				if !filepath.IsAbs(path) {
					path = filepath.Join(model.SourceRoot, path)
				}
				canonical := b.identityOf(path).Canonical()
				if canonical == "" {
					continue
				}
				if owner, seen := byFile[canonical]; seen && owner != target.Name {
					contested[canonical] = true
					continue
				}
				byFile[canonical] = target.Name
			}
		}
	}
	for canonical := range contested {
		delete(byFile, canonical)
	}
	return byFile
}

// narrowingByComponent groups the headers DWARF narrowing removed by the
// component they would have belonged to (section 4.4). Grouping uses the same
// resolver as the document, so a reviewer sees the count next to the component
// it concerns rather than one undifferentiated total.
func narrowingByComponent(resolver *componentResolver, narrowed []narrowedHeader) []NarrowingCount {
	if len(narrowed) == 0 {
		return nil
	}
	byComponent := map[string][]string{}
	seen := map[string]bool{}
	for _, entry := range narrowed {
		if seen[entry.header] {
			continue
		}
		seen[entry.header] = true
		file := domain.UsedFile{ID: domain.FileID{Anchor: anchorOf(entry.header), RelPath: relOf(entry.header)}}
		_, name, _, _, _ := resolver.resolve(file)
		byComponent[name] = append(byComponent[name], entry.header)
	}
	out := make([]NarrowingCount, 0, len(byComponent))
	for name, headers := range byComponent {
		sort.Strings(headers)
		out = append(out, NarrowingCount{Component: name, Count: len(headers), Headers: headers})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Component < out[j].Component })
	return out
}

// toolchainRoots are the installation roots of the registered toolchain
// anchors. Section 14.4 separates the compiler's own headers from the
// distribution's, and the anchor root is what marks the boundary.
func toolchainRoots(result *anchors.Result) []string {
	roots := make([]string, 0)
	for _, anchor := range result.Registry.Anchors() {
		if strings.HasPrefix(string(anchor.Key), "toolchain:") {
			roots = append(roots, anchor.Root)
		}
	}
	return roots
}

// componentRoots turns the curated components[] entries into the component
// roots section 14.4 needs to tell a vendored third-party header apart from a
// project header that happens to live under the same anchor.
func componentRoots(cfg config.Config) []headers.ComponentRoot {
	roots := make([]headers.ComponentRoot, 0, len(cfg.Components))
	for _, component := range cfg.Components {
		if component.Path == "" {
			continue
		}
		roots = append(roots, headers.ComponentRoot{
			Canonical: "project:" + strings.Trim(filepath.ToSlash(component.Path), "/"),
			SDK:       component.Type == "sdk",
			External:  component.Type != "sdk",
		})
	}
	return roots
}

// anchorRootList is the set of directories introspection may be pointed at.
// Section 9.2 requires every path argument to lie inside one of them.
func anchorRootList(result *anchors.Result) []string {
	roots := make([]string, 0)
	for _, anchor := range result.Registry.Anchors() {
		if anchor.Root != "" {
			roots = append(roots, anchor.Root)
		}
	}
	return roots
}
