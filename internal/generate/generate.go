package generate

import (
	"crypto/sha256"
	"encoding/hex"
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
	makeadapter "github.com/example/sbomb/internal/adapters/make"
	"github.com/example/sbomb/internal/adapters/manifest"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/buildinfo"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/google/uuid"
)

type Result struct {
	Graph    *evidence.Graph
	Findings []domain.Finding
	BOM      cyclonedx.BOM
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
	logger := NewLogger(options.Verbosity, options.LogWriter)

	projectRoot := cfg.Project.Root
	if projectRoot == "" {
		projectRoot = "."
	}
	logger.Info("Starting SBOM generation (build-dir: '%s', project-root: '%s', verbosity: level %d)", buildDir, projectRoot, options.Verbosity)
	logger.Debug("Path flavor: %T", options.PathFlavor)

	graph := evidence.New()
	artifactName := "build"
	if len(cfg.Artifacts) > 0 && cfg.Artifacts[0].Path != "" {
		artifactName = pathmodel.Base(cfg.Artifacts[0].Path, options.PathFlavor)
	}
	artifactID := domain.NodeID("artifact:" + pathmodel.Slug(artifactName, 64))
	graph.AddNode(domain.Node{ID: artifactID, Kind: domain.NodeArtifact})
	logger.Debug("Created root artifact node: %s", artifactID)

	findings := make([]domain.Finding, 0)
	components := make([]cyclonedx.Component, 0)

	if len(cfg.Manifests) > 0 {
		logger.Info("Parsing %d package manifest(s)...", len(cfg.Manifests))
	}
	for _, manifestPath := range cfg.Manifests {
		path := manifestPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectRoot, path)
		}
		logger.Debug("Parsing package manifest at '%s'", path)
		if _, err := manifest.ParseFile(path); err != nil {
			id := "MISSING_PACKAGE_EVIDENCE"
			if errors.Is(err, manifest.ErrInputLimitExceeded) {
				id = "INPUT_LIMIT_EXCEEDED"
			}
			logger.Info("Manifest parsing error on '%s': %v", manifestPath, err)
			findings = append(findings, domain.Finding{ID: id, Severity: domain.SeverityWarning, Subject: domain.Subject{Kind: "configuration", Ref: manifestPath}, Message: err.Error()})
		}
	}

	compilePath := filepath.Join(buildDir, "compile_commands.json")
	logger.Info("Reading compile database from '%s'...", compilePath)
	commands, err := compiledb.ParseFile(compilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return Result{}, fmt.Errorf("parse compile database: %w", err)
		}
		logger.Info("compile_commands.json not found in '%s'", buildDir)
		findings = append(findings, domain.Finding{ID: "MISSING_COMPILE_EVIDENCE", Severity: domain.SeverityWarning, Subject: domain.Subject{Kind: "build", Ref: buildDir}, Message: "compile_commands.json was not found"})
	} else {
		logger.Info("Discovered %d compile command(s) in compile_commands.json", len(commands))
	}

	// The CMake File API is the authoritative source for the project and build
	// roots and for the toolchain layout (sections 10.2 and 24.4).
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

	compileFlags := make([]string, 0)
	for _, command := range commands {
		compileFlags = append(compileFlags, command.Arguments...)
	}
	// Identity uses the logical build path -- the path as it appeared in the
	// build evidence (section 7.6) -- not the directory the evidence is being
	// read from now. Configuration wins, then what the File API recorded, then
	// the directory on the command line made absolute.
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
	if projectRootForIdentity == "" && replyModel == nil {
		projectRootForIdentity = absolutePath(".")
	}

	anchorResult, err := anchors.Assemble(anchors.Options{
		Flavor:        options.PathFlavor,
		ProjectRoot:   projectRootForIdentity,
		BuildRoot:     buildRootForIdentity,
		ConfigAnchors: cfg.Anchors,
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
	logger.Info("Registered %d anchor(s)", len(anchorResult.Registry.Anchors()))

	seen := map[string]bool{}
	componentSeen := map[string]bool{}
	excludedByScope := map[anchors.Scope]int{}

	// identify resolves a path to its portable identity and origin scope.
	identify := func(base, path string) (string, anchors.Scope) {
		id, scope := anchorResult.ScopeOfPath(base, path)
		return id.Canonical(), scope
	}

	addComponent := func(base, path string) {
		id, scope := anchorResult.ScopeOfPath(base, path)
		canonical := id.Canonical()
		if componentSeen[canonical] {
			return
		}
		componentSeen[canonical] = true
		if id.Anchor == domain.AnchorKey(pathmodel.AnchorAbs) {
			findings = append(findings, anchors.UnanchoredFinding(id))
		}
		if !anchors.IncludedByDefault(scope) {
			// Toolchain and system files are evidence, not project
			// dependencies (section 24.1). Their omission is counted and
			// reported rather than silent.
			excludedByScope[scope]++
			logger.Debug("Excluded %s file '%s' from the SBOM", scope, canonical)
			return
		}
		logger.Debug("Discovered component file '%s' (canonical: '%s', scope: %s)", path, canonical, scope)
		components = append(components, fileComponent(canonical, path, scope, options.PathFlavor, logger))
	}

	for _, command := range commands {
		// Compile database entries are relative to their own directory field.
		base := command.Directory
		if base == "" {
			base = buildDir
		}
		rel, _ := identify(base, command.File)
		if seen[rel] {
			continue
		}
		seen[rel] = true
		sourceID := domain.NodeID(rel)
		objectRef := command.Output
		if objectRef == "" {
			objectRef = command.File + ".o"
		}
		objectCanonical, _ := identify(base, objectRef)
		objectID := domain.NodeID("object:" + objectCanonical)
		logger.Trace("CompileDB entry: file='%s', output='%s'", command.File, command.Output)
		graph.AddNode(domain.Node{ID: objectID, Kind: domain.NodeObject})
		graph.AddNode(domain.Node{ID: sourceID, Kind: domain.NodeSource})
		graph.AddEdge(domain.Edge{From: artifactID, To: objectID, Type: "compile-output", Strength: "linked", Confidence: domain.ConfidenceMedium, Source: "compile_commands", Adapter: "compiledb"})
		graph.AddEdge(domain.Edge{From: objectID, To: sourceID, Type: "compile", Strength: "derived", Confidence: domain.ConfidenceHigh, Source: "compile_commands", Adapter: "compiledb"})
		logger.Debug("Added compile graph edges: %s -> %s -> %s", artifactID, objectID, sourceID)
		addComponent(base, command.File)
	}

	if len(commands) == 0 {
		logger.Info("Attempting Make adapter fallback for build directory '%s'...", buildDir)
		if makeBuild, makeErr := makeadapter.Parse(buildDir); makeErr == nil {
			logger.Info("Make adapter parsed %d target(s)", len(makeBuild.Targets))
			for _, target := range makeBuild.Targets {
				logger.Debug("Processing Make target in directory '%s' with %d link input(s)", target.Directory, len(target.LinkInputs))
				for _, input := range target.LinkInputs {
					canonical, _ := identify(buildDir, input)
					objectID := domain.NodeID("object:" + canonical)
					kind := domain.NodeObject
					if strings.HasSuffix(input, ".a") || strings.HasSuffix(input, ".lib") {
						kind = domain.NodeArchive
					}
					graph.AddNode(domain.Node{ID: objectID, Kind: kind})
					graph.AddEdge(domain.Edge{From: artifactID, To: objectID, Type: "link", Strength: "linked", Confidence: domain.ConfidenceHigh, Source: filepath.Join(target.Directory, "link.txt"), Adapter: "make"})
					if source, ok := target.ObjectSources[input]; ok {
						sourceCanonical, _ := identify(buildDir, source)
						sourceID := domain.NodeID(sourceCanonical)
						graph.AddNode(domain.Node{ID: sourceID, Kind: domain.NodeSource})
						graph.AddEdge(domain.Edge{From: objectID, To: sourceID, Type: "source-mapping", Strength: "derived", Confidence: domain.ConfidenceHigh, Source: filepath.Join(target.Directory, "build.make"), Adapter: "make"})
						logger.Debug("Make source mapping: %s -> %s", input, source)
						addComponent(buildDir, source)
					}
					for _, dependency := range target.ObjectDeps[input] {
						if dependency == target.ObjectSources[input] {
							continue
						}
						dependencyCanonical, _ := identify(buildDir, dependency)
						dependencyID := domain.NodeID(dependencyCanonical)
						graph.AddNode(domain.Node{ID: dependencyID, Kind: domain.NodeHeader})
						graph.AddEdge(domain.Edge{From: objectID, To: dependencyID, Type: "include", Strength: "derived", Confidence: domain.ConfidenceMedium, Source: target.CompilerDepend, Adapter: "make"})
						logger.Trace("Make header dependency for '%s': '%s'", input, dependency)
						addComponent(buildDir, dependency)
					}
					addComponent(buildDir, input)
				}
			}
		} else {
			logger.Debug("Make adapter parsing result: %v", makeErr)
		}
	}

	if (len(commands) == 0 && !hasMakeLinkEvidence(buildDir)) || !hasLinkEvidence(buildDir) {
		logger.Info("Link evidence check: missing compile or link evidence")
		findings = append(findings, domain.Finding{ID: "MISSING_LINK_EVIDENCE", Severity: domain.SeverityError, Subject: domain.Subject{Kind: "artifact", Ref: string(artifactID)}, Message: "no compile or link evidence was discovered"})
	}

	for _, scope := range []anchors.Scope{anchors.ScopeToolchain, anchors.ScopeSystem} {
		if count := excludedByScope[scope]; count > 0 {
			logger.Info("Excluded %d %s file(s) from the SBOM (section 24.1 default)", count, scope)
			findings = append(findings, domain.Finding{
				ID:       "DYNAMIC_DEPENDENCIES_IGNORED",
				Severity: domain.SeverityInfo,
				Subject:  domain.Subject{Kind: "build", Ref: buildDir},
				Message:  fmt.Sprintf("%d %s file(s) were excluded by the default inclusion policy", count, scope),
			})
		}
	}

	logger.Info("Sorting %d component(s) deterministically...", len(components))
	sort.Slice(components, func(i, j int) bool { return components[i].BomRef < components[j].BomRef })
	deps := make([]cyclonedx.Dependency, 0, len(components)+1)
	for _, component := range components {
		deps = append(deps, cyclonedx.Dependency{Ref: component.BomRef})
	}
	bom := cyclonedx.BOM{BomFormat: "CycloneDX", SpecVersion: "1.6", Version: 1, Components: components, Dependencies: deps, Metadata: &cyclonedx.Metadata{Tools: []cyclonedx.Tool{{Vendor: buildinfo.Vendor, Name: buildinfo.Name, Version: buildinfo.Version}}}}
	if reproducible {
		bom.SerialNumber = cyclonedx.ReproducibleSerialNumber(bom)
		logger.Debug("Reproducible mode enabled: generated deterministic serial '%s'", bom.SerialNumber)
	} else {
		bom.SerialNumber = "urn:uuid:" + uuid.NewString()
		bom.Metadata.Timestamp = buildTimestamp()
		logger.Debug("Generated UUID serial '%s' (timestamp: '%s')", bom.SerialNumber, bom.Metadata.Timestamp)
	}

	logger.Info("Evidence graph assembled: %d node(s), %d edge(s)", len(graph.Nodes()), len(graph.Edges()))
	logger.Info("CycloneDX 1.6 BOM constructed: %d component(s)", len(components))

	return Result{Graph: graph, Findings: findings, BOM: bom}, nil
}

func fileComponent(canonical, path string, scope anchors.Scope, flavor pathmodel.Flavor, logger *Logger) cyclonedx.Component {
	component := cyclonedx.Component{Type: "file", Name: pathmodel.Base(path, flavor), BomRef: "file:" + canonical, Properties: []cyclonedx.Property{
		{Name: "sbomb:path:canonical", Value: canonical},
		{Name: "sbomb:component:scope", Value: string(scope)},
	}}
	data, err := os.ReadFile(path)
	if err == nil {
		hash := sha256.Sum256(data)
		hashStr := hex.EncodeToString(hash[:])
		component.Hashes = []cyclonedx.Hash{{Alg: "SHA-256", Value: hashStr}}
		logger.Trace("Hashed '%s' (%d bytes): sha256=%s", path, len(data), hashStr)
	} else {
		logger.Debug("Could not read file '%s': %v", path, err)
	}

	if finding := resolveLicense(path, data, err, logger); finding.Name != "" {
		licenseEntry := cyclonedx.License{}
		if finding.Expression != "" {
			licenseEntry.Expression = finding.Expression
		} else {
			licenseEntry.License = &cyclonedx.LicenseIdentifier{Name: finding.Name}
		}
		component.Licenses = []cyclonedx.License{licenseEntry}
		if finding.Reason != "" {
			component.Properties = append(component.Properties, cyclonedx.Property{Name: "sbomb:license:reason", Value: finding.Reason})
		}
		component.Properties = append(component.Properties, cyclonedx.Property{Name: "sbomb:license:evidence", Value: finding.Evidence})
	}
	return component
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

func hasLinkEvidence(buildDir string) bool {
	for _, name := range []string{"link-trace.txt", "link.d", "link.map"} {
		if info, err := os.Stat(filepath.Join(buildDir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	if hasMakeLinkEvidence(buildDir) {
		return true
	}
	for _, pattern := range []string{"*.map", "*.d"} {
		matches, err := filepath.Glob(filepath.Join(buildDir, pattern))
		if err == nil && len(matches) > 0 {
			return true
		}
	}
	return false
}

func hasMakeLinkEvidence(buildDir string) bool {
	matches, err := filepath.Glob(filepath.Join(buildDir, "CMakeFiles", "*.dir", "link.txt"))
	return err == nil && len(matches) > 0
}
