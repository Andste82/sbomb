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
	"time"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/adapters/compiledb"
	"github.com/example/sbomb/internal/adapters/manifest"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/buildinfo"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/inventory"
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
	compile := collectCompileEvidence(buildDir, commands, logger)
	mapPath, depfilePath := "", ""
	if len(cfg.Artifacts) > 0 {
		mapPath, depfilePath = cfg.Artifacts[0].Map, cfg.Artifacts[0].LinkDepfile
	}
	artifactIDs := buildEvidenceGraph(graph, b, deliverables, compile, buildDir, mapPath, depfilePath, logger)
	findings = append(findings, b.Findings()...)
	logger.Info("Evidence graph: %d node(s), %d edge(s) [%s]", len(graph.Nodes()), len(graph.Edges()), describeCounts(graph.Nodes()))

	// 6. The reachability filter. This is what makes the output evidence-based
	//    rather than a listing of everything the adapters happened to see.
	reachable := usedFiles(graph, artifactIDs)
	logger.Info("Reachable from a deliverable: %d node(s) [%s]", len(reachable), describeCounts(reachable))
	findings = append(findings, unresolvedObjects(graph, reachable, anchorResult)...)

	// 7. Inventory: scope filter, representation rules, hashing.
	excludedByScope := map[anchors.Scope]int{}
	used := make([]domain.UsedFile, 0, len(reachable))
	for _, node := range reachable {
		scope := scopeOfNode(node, anchorResult)
		if !anchors.IncludedByDefault(scope) {
			excludedByScope[scope]++
			logger.Debug("Excluded %s file '%s'", scope, node.ID)
			continue
		}
		represent, reason := representInSBOM(graph, node)
		if !represent {
			logger.Debug("Transient build artifact '%s' is evidence only", node.ID)
			continue
		}
		if reason != "" {
			logger.Debug("Keeping '%s' as a component: %s", node.ID, reason)
		}
		canonical := string(node.ID)
		used = append(used, domain.UsedFile{
			ID:         domain.FileID{Anchor: anchorOf(canonical), RelPath: relOf(canonical)},
			Class:      fileClassOf(node),
			Properties: map[string][]string{"sbomb:component:scope": {string(scope)}},
		})
	}
	used = inventory.MergeUsedFiles(used)
	used, hashFindings := hashUsedFiles(used, b.physical, logger)
	findings = append(findings, hashFindings...)

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

	// 9. Serialize.
	components := make([]cyclonedx.Component, 0, len(used))
	for _, file := range used {
		canonical := file.ID.Canonical()
		scope := anchors.ScopeUnknown
		if values := file.Properties["sbomb:component:scope"]; len(values) > 0 {
			scope = anchors.Scope(values[0])
		}
		component := fileComponent(canonical, b.physical[canonical], scope, options.PathFlavor, logger)
		if len(file.Hashes) > 0 {
			component.Hashes = component.Hashes[:0]
			for _, algorithm := range sortedKeys(file.Hashes) {
				component.Hashes = append(component.Hashes, cyclonedx.Hash{Alg: algorithm, Value: file.Hashes[algorithm]})
			}
		}
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool { return components[i].BomRef < components[j].BomRef })

	deps := make([]cyclonedx.Dependency, 0, len(components))
	for _, component := range components {
		deps = append(deps, cyclonedx.Dependency{Ref: component.BomRef})
	}
	bom := cyclonedx.BOM{
		BomFormat: "CycloneDX", SpecVersion: "1.6", Version: 1,
		Components: components, Dependencies: deps,
		Metadata: &cyclonedx.Metadata{Tools: []cyclonedx.Tool{{Vendor: buildinfo.Vendor, Name: buildinfo.Name, Version: buildinfo.Version}}},
	}
	if reproducible {
		bom.SerialNumber = cyclonedx.ReproducibleSerialNumber(bom)
		findings = append(findings, domain.Finding{
			ID: "REPRODUCIBLE_MODE_OMITS_TIMESTAMP", Severity: domain.SeverityInfo,
			Subject: domain.Subject{Kind: "run", Ref: buildRootForIdentity},
			Message: "the document omits metadata.timestamp and is therefore not a CRA deliverable SBOM",
		})
	} else {
		bom.SerialNumber = "urn:uuid:" + uuid.NewString()
		bom.Metadata.Timestamp = buildTimestamp()
	}

	logger.Info("CycloneDX 1.6 BOM constructed: %d component(s)", len(components))
	return Result{Graph: graph, Findings: findings, BOM: bom}, nil
}

// buildSubject names the build in a finding using the identity the evidence
// carries, so that a finding does not embed the directory the run happened to
// read from.
func buildSubject(cfg config.Config, buildDir string) string {
	if cfg.Build.Dir != "" {
		return cfg.Build.Dir
	}
	return buildDir
}

func fileClassOf(node domain.Node) domain.FileClass {
	switch node.Kind {
	case domain.NodeHeader:
		return domain.FileClassHeader
	case domain.NodeObject:
		return domain.FileClassObject
	case domain.NodeArchive:
		return domain.FileClassArchive
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
