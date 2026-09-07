// Package selfsbom builds an SBOM for a Go binary out of the evidence the
// binary carries.
//
// It exists because a release of this tool had to ship a bill of materials for
// itself, and the first attempt fabricated one: a compile database written on
// the spot, naming a single Go file beside an empty linker map. That is the
// evidence-free guessing the tool exists to refuse (deviation D16), so it was
// removed and this took its place.
//
// The rules are the ones section 4.1 applies to everything else. A component
// enters this document because the linker recorded it in the deliverable, not
// because a manifest mentioned it or a directory contains it. The vendor tree
// is consulted only for licence text, only for modules the binary already
// named, and only where its version agrees with the binary's.
//
// What this deliberately does not do is enumerate source files. Nothing in the
// binary names them; the tool that could, "go list", would have to be run as a
// subprocess outside the allowlist of section 9.2, and its answer would be a
// statement about the working tree rather than about the artifact. A Go
// module is versioned, licensed and published as a unit, so the module is the
// component, and the document says so rather than implying file-level
// evidence it does not have.
package selfsbom

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example/sbomb/internal/adapters/gobin"
	"github.com/example/sbomb/internal/adapters/govendor"
	"github.com/example/sbomb/internal/buildinfo"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/limits"
	"github.com/example/sbomb/internal/sbomwriter"
)

// Options are the facts the binary cannot supply about itself.
type Options struct {
	// Version is the release version of the product. A binary built from a
	// working tree records "(devel)" as its main module version, so without
	// this the product has no version and says so.
	Version string
	// ModuleDir is the source module root, used for the product's own licence
	// and for the vendored licences of its dependencies. Empty means no
	// licence evidence is available.
	ModuleDir string
	// GOROOT is the toolchain root, used for the standard library's licence.
	// Empty means the standard library's licence is unknown.
	GOROOT string
	// Licenses maps a module path to an SPDX expression the caller asserts,
	// with "std" for the standard library and the main module's path for the
	// product itself. Exact licence-text matching cannot recognize a text
	// whose copyright holder has been filled in or whose clauses have been
	// renumbered, which is most of them, so curation is how a real licence
	// reaches the document. It is recorded as curated, never as detected.
	Licenses map[string]string
	// Supplier is the product's supplier. No evidence in a Go binary names
	// one, and section 20.5 forbids deriving it from a repository URL, so it
	// is curated or absent.
	Supplier string
	// Reproducible omits the timestamp, per section 29.
	Reproducible bool
	// Timestamp is the run time; the zero value means now.
	Timestamp time.Time
	// Limits is the parser policy of section 30.
	Limits limits.Config
}

// Result is the document, the evidence behind it and what could not be proven.
type Result struct {
	Document *sbomwriter.Document
	Graph    *evidence.Graph
	Findings []domain.Finding
	// Binary is the raw evidence, for a caller that wants to log it.
	Binary *gobin.Binary
}

const (
	productID     = "product"
	stdComponent  = "component:go/std"
	stdModulePath = "std"
)

// Build reads a Go binary and assembles the document describing it.
func Build(binaryPath string, options Options) (Result, error) {
	// A binary with no build information is reported as an error rather than
	// as a finding. MALFORMED_BINARY would be the nearest identifier and it is
	// the wrong one -- appendix A defines it for an artifact that could not be
	// parsed as ELF or PE, and this one parsed fine; what is missing is the
	// module record. There is no document to attach a finding to either.
	binary, err := gobin.Inspect(binaryPath, options.Limits)
	if err != nil {
		return Result{}, err
	}
	return Assemble(binary, filepath.Base(binaryPath), options)
}

// Assemble turns evidence that has already been read into a document. Build is
// Inspect followed by this; keeping them apart lets the assembly be exercised
// against evidence a test constructs, rather than only against whatever binary
// the test machine can produce.
func Assemble(binary *gobin.Binary, artifactName string, options Options) (Result, error) {
	if binary == nil {
		return Result{}, errors.New("no build information to assemble")
	}
	findings := make([]domain.Finding, 0, 8)
	licences := newLicenceSource(options, binary, &findings)

	document := &sbomwriter.Document{
		Run: sbomwriter.RunMetadata{
			ToolName:      buildinfo.Name,
			ToolVendor:    buildinfo.Vendor,
			ToolVersion:   buildinfo.Version,
			Reproducible:  options.Reproducible,
			Timestamp:     timestamp(options),
			PolicyProfile: "",
			Generator:     "go" + goToolchainSuffix(binary),
			Adapters:      []string{"gobin"},
		},
	}

	product, productFindings := productComponent(artifactName, binary, options, licences)
	document.Product = product
	findings = append(findings, productFindings...)

	targets := make([]string, 0, len(binary.Deps)+1)
	for _, module := range binary.Deps {
		component, moduleFindings := moduleComponent(module, licences)
		document.Components = append(document.Components, component)
		findings = append(findings, moduleFindings...)
		targets = append(targets, component.ID)
	}

	standard, stdFindings := standardLibraryComponent(binary, licences)
	document.Components = append(document.Components, standard)
	findings = append(findings, stdFindings...)
	targets = append(targets, standard.ID)

	sort.Slice(document.Components, func(i, j int) bool {
		return document.Components[i].ID < document.Components[j].ID
	})
	sort.Strings(targets)
	document.Relations = []sbomwriter.Relation{{From: productID, To: targets}}

	sortFindings(findings)
	document.Findings = findings

	graph, err := buildGraph(artifactName, binary, document)
	if err != nil {
		return Result{}, err
	}

	return Result{Document: document, Graph: graph, Findings: findings, Binary: binary}, nil
}

// productComponent describes the binary itself.
func productComponent(artifactName string, binary *gobin.Binary, options Options, licences *licenceSource) (domain.Component, []domain.Finding) {
	findings := make([]domain.Finding, 0, 4)

	name := path.Base(binary.MainPackage)
	if name == "" || name == "." {
		name = strings.TrimSuffix(artifactName, filepath.Ext(artifactName))
	}

	version := strings.TrimSpace(options.Version)
	versionSource := "configuration"
	if version == "" && !binary.Devel() {
		version, versionSource = binary.Main.Version, "go-build-info"
	}

	component := domain.Component{
		ID:            productID,
		Name:          name,
		Version:       version,
		VersionSource: versionSource,
		VersionConf:   domain.ConfidenceHigh,
		Type:          "application",
		Supplier:      options.Supplier,
		PURL:          goPURL(binary.Main.Path, version),
		DetectedBy:    "gobin",
		Properties: map[string][]string{
			"sbomb:component:detectedBy": {"gobin"},
			"sbomb:go:mainPackage":       {binary.MainPackage},
			"sbomb:go:module":            {binary.Main.Path},
			"sbomb:go:toolchain":         {binary.GoVersion},
		},
	}
	if goos := binary.GOOS(); goos != "" {
		component.Properties["sbomb:go:goos"] = []string{goos}
	}
	if goarch := binary.GOARCH(); goarch != "" {
		component.Properties["sbomb:go:goarch"] = []string{goarch}
	}
	// The revision is the strongest statement this document makes about the
	// product's own sources: not "these files were present" but "this commit
	// was built".
	if revision, ok := binary.Setting("vcs.revision"); ok && revision != "" {
		component.VCS = &domain.VCSRecord{Commit: revision}
	}
	if modified, ok := binary.Setting("vcs.modified"); ok && modified == "true" {
		if component.VCS == nil {
			component.VCS = &domain.VCSRecord{}
		}
		component.VCS.Dirty = true
		findings = append(findings, componentFinding("VCS_DIRTY", domain.SeverityWarning, component.ID,
			"the binary was built from a working tree with uncommitted changes",
			"Commit or stash the changes and rebuild before releasing."))
	}

	if version == "" {
		component.VersionSource = ""
		component.VersionConf = domain.ConfidenceUnknown
		findings = append(findings, componentFinding("UNKNOWN_VERSION", domain.SeverityWarning, component.ID,
			"the main module records no released version and none was supplied",
			"Pass --version, or build from a tagged module."))
	}
	if component.Supplier == "" {
		findings = append(findings, componentFinding("MISSING_SUPPLIER", domain.SeverityWarning, component.ID,
			"the component has no supplier, which BSI TR-03183-2 requires",
			"Pass --supplier."))
	}

	licenceFinding, licenceFindings := licences.forProduct(component.ID, binary.Main.Path)
	component.Licenses = []domain.LicenseFinding{licenceFinding}
	annotateLicence(&component, licenceFinding)
	component.LicenseEvidence = licences.observed[component.ID]
	findings = append(findings, licenceFindings...)

	findings = append(findings, missingComponentHash(component.ID, "")...)
	return component, findings
}

// moduleComponent describes one dependency module.
func moduleComponent(recorded gobin.Module, licences *licenceSource) (domain.Component, []domain.Finding) {
	findings := make([]domain.Finding, 0, 3)
	effective := recorded.Effective()

	component := domain.Component{
		ID:            "component:go/" + effective.Path,
		Name:          effective.Path,
		Version:       effective.Version,
		VersionSource: "go-build-info",
		VersionConf:   domain.ConfidenceHigh,
		Type:          "library",
		Scope:         "third-party",
		PURL:          goPURL(effective.Path, effective.Version),
		DetectedBy:    "gobin",
		Properties: map[string][]string{
			"sbomb:component:detectedBy": {"gobin"},
			"sbomb:go:module":            {effective.Path},
		},
	}
	if recorded.ReplacedBy != nil {
		// The document names what was compiled. It also names what was asked
		// for, because a consumer matching this against an advisory for the
		// original module needs to see that the substitution happened.
		component.Properties["sbomb:go:replaces"] = []string{joinModule(recorded.Path, recorded.Version)}
	}
	if effective.Sum != "" {
		// Not a CycloneDX hash: h1 hashes a module's whole file tree, not any
		// artifact this document names. It is recorded as what it is, so that
		// "go mod verify" can be run against it and nobody mistakes it for the
		// digest of a downloadable file.
		component.Properties["sbomb:go:moduleSum"] = []string{effective.Sum}
	}

	if component.Version == "" {
		findings = append(findings, componentFinding("UNKNOWN_VERSION", domain.SeverityWarning, component.ID,
			"the linker recorded no version for this module",
			"Build with a module cache rather than a local replacement, or curate the version."))
	}
	findings = append(findings, componentFinding("MISSING_SUPPLIER", domain.SeverityWarning, component.ID,
		"a Go module records no supplier, and section 20.5 forbids deriving one from its import path",
		"Curate the supplier if the SBOM has to carry one."))

	licenceFinding, licenceFindings := licences.forModule(component.ID, effective)
	component.Licenses = []domain.LicenseFinding{licenceFinding}
	annotateLicence(&component, licenceFinding)
	component.LicenseEvidence = licences.observed[component.ID]
	findings = append(findings, licenceFindings...)

	findings = append(findings, missingComponentHash(component.ID, effective.Sum)...)
	return component, findings
}

// standardLibraryComponent describes the part of the binary that came from the
// toolchain. It is linked into the deliverable exactly like any dependency, so
// it is a component of the product and not of the build environment.
func standardLibraryComponent(binary *gobin.Binary, licences *licenceSource) (domain.Component, []domain.Finding) {
	findings := make([]domain.Finding, 0, 3)
	component := domain.Component{
		ID:            stdComponent,
		Name:          stdModulePath,
		Version:       binary.GoVersion,
		VersionSource: "go-build-info",
		VersionConf:   domain.ConfidenceHigh,
		Type:          "library",
		Scope:         "toolchain",
		PURL:          goPURL(stdModulePath, binary.GoVersion),
		DetectedBy:    "gobin",
		Properties: map[string][]string{
			"sbomb:component:detectedBy": {"gobin"},
			"sbomb:go:toolchain":         {binary.GoVersion},
		},
	}
	if component.Version == "" {
		findings = append(findings, componentFinding("UNKNOWN_VERSION", domain.SeverityWarning, component.ID,
			"the binary records no Go toolchain version",
			"Rebuild with a toolchain that records its version."))
	}
	findings = append(findings, componentFinding("MISSING_SUPPLIER", domain.SeverityWarning, component.ID,
		"the standard library records no supplier",
		"Curate the supplier if the SBOM has to carry one."))

	licenceFinding, licenceFindings := licences.forStandardLibrary(component.ID)
	component.Licenses = []domain.LicenseFinding{licenceFinding}
	annotateLicence(&component, licenceFinding)
	component.LicenseEvidence = licences.observed[component.ID]
	findings = append(findings, licenceFindings...)

	findings = append(findings, missingComponentHash(component.ID, "")...)
	return component, findings
}

// buildGraph records why each component is in the document. There is one
// artifact, and every component hangs off it with packaged evidence, because
// the linker put it there.
func buildGraph(artifactName string, binary *gobin.Binary, document *sbomwriter.Document) (*evidence.Graph, error) {
	graph := evidence.New()
	artifactID := domain.NodeID("artifact:" + artifactName)
	graph.AddNode(domain.Node{
		ID:   artifactID,
		Kind: domain.NodeArtifact,
		Attributes: map[string]string{
			"goVersion": binary.GoVersion,
			"module":    binary.Main.Path,
		},
	})
	for _, component := range document.Components {
		nodeID := domain.NodeID(component.ID)
		graph.AddNode(domain.Node{
			ID:         nodeID,
			Kind:       domain.NodePackage,
			Attributes: map[string]string{"version": component.Version},
		})
		graph.AddEdge(domain.Edge{
			From:     artifactID,
			To:       nodeID,
			Type:     "package",
			Strength: "packaged",
			// The linker's own record is structured and authoritative: it is
			// not a report about the build, it is part of the artifact.
			Confidence: evidence.DeriveConfidence("packaged", evidence.SourceClassStructuredAuthoritative),
			Source:     "go build info",
			Adapter:    "gobin",
		})
	}
	if err := graph.CheckInvariants(); err != nil {
		return nil, fmt.Errorf("self SBOM evidence graph: %w", err)
	}
	return graph, nil
}

// licenceSource resolves licences from the two places that hold the text of a
// licence for code that is in this binary -- the vendored sources and GOROOT
// -- and from what the caller curated.
type licenceSource struct {
	moduleDir string
	goroot    string
	bounds    limits.Config
	// curated maps module path to an SPDX expression the caller asserted. It
	// wins over detected text, per the priority order of section 22.2, and a
	// disagreement between the two is reported rather than resolved silently.
	curated map[string]string
	// vendored indexes vendor/modules.txt by module path, so that a licence is
	// used only when the vendored version is the one that was linked.
	vendored map[string]govendor.Module
	// vendorErr is why the vendor tree could not be read, if it could not.
	vendorErr error
	// observed collects the licence texts found in a file that is not itself
	// one licence, keyed by component.
	observed map[string][]domain.LicenseFinding
}

// licenceNames lists the identifiers of a set of observations, for a message.
func licenceNames(findings []domain.LicenseFinding) []string {
	names := make([]string, 0, len(findings))
	for _, finding := range findings {
		names = append(names, finding.Expression)
	}
	return names
}

func newLicenceSource(options Options, binary *gobin.Binary, findings *[]domain.Finding) *licenceSource {
	source := &licenceSource{
		moduleDir: options.ModuleDir,
		goroot:    options.GOROOT,
		bounds:    options.Limits,
		curated:   options.Licenses,
		observed:  map[string][]domain.LicenseFinding{},
	}
	if options.ModuleDir == "" {
		return source
	}
	modules, err := govendor.Parse(options.ModuleDir, options.Limits)
	if err != nil {
		source.vendorErr = err
		*findings = append(*findings, domain.Finding{
			ID:       "MISSING_PACKAGE_EVIDENCE",
			Severity: domain.SeverityWarning,
			Subject:  domain.Subject{Kind: "configuration", Ref: "vendor/modules.txt"},
			Message:  err.Error(),
		})
		return source
	}
	source.vendored = govendor.Index(modules)
	return source
}

func (s *licenceSource) forProduct(componentID, modulePath string) (domain.LicenseFinding, []domain.Finding) {
	return s.decide(componentID, modulePath, s.moduleDir)
}

func (s *licenceSource) forStandardLibrary(componentID string) (domain.LicenseFinding, []domain.Finding) {
	return s.decide(componentID, stdModulePath, s.goroot)
}

// forModule resolves a dependency's licence out of the vendor tree, but only
// when the vendored module is the one the linker recorded. A vendor directory
// from another commit would otherwise hand the wrong licence to the right
// component, and it would look exactly as confident as a correct one.
func (s *licenceSource) forModule(componentID string, module gobin.Module) (domain.LicenseFinding, []domain.Finding) {
	vendored, present := s.vendored[module.Path]
	switch {
	case !present:
		return s.decide(componentID, module.Path, "")
	case vendored.Version != module.Version:
		finding, findings := s.decide(componentID, module.Path, "")
		findings = append(findings, componentFinding("STALE_BUILD_EVIDENCE", domain.SeverityWarning, componentID,
			fmt.Sprintf("the binary was linked against %s but the vendor directory holds %s, so its licence text was not used",
				joinModule(module.Path, module.Version), joinModule(vendored.Path, vendored.Version)),
			"Run go mod vendor and rebuild, so that the sources on disk are the sources in the binary."))
		return finding, findings
	default:
		return s.decide(componentID, module.Path, vendored.Dir(filepath.Join(s.moduleDir, "vendor")))
	}
}

// decide applies the priority order of section 22.2 to one component: what the
// caller curated, then what the licence file in dir says. Curation wins, but a
// curated value that contradicts a recognized licence text is a finding --
// silently preferring one over the other is how an SBOM ends up asserting a
// licence nobody can reproduce from the sources.
func (s *licenceSource) decide(componentID, key, dir string) (domain.LicenseFinding, []domain.Finding) {
	detected, findings := domain.LicenseFinding{}, []domain.Finding(nil)
	if dir != "" {
		detected, findings = s.detect(componentID, dir)
	}
	curated := strings.TrimSpace(s.curated[key])
	if curated == "" {
		if dir == "" {
			return unknownLicence(componentID)
		}
		return detected, findings
	}

	// The detected expression either confirms the curated one or contradicts
	// it. Unrecognized text does neither, so it is not treated as a conflict.
	effective, conflicting := license.ResolveConflict(curated, detected.Expression, "configuration")
	if conflicting {
		return effective, []domain.Finding{componentFinding("LICENSE_CONFLICT", domain.SeverityWarning, componentID,
			fmt.Sprintf("the curated licence is %q but the licence text says %q", curated, detected.Expression),
			"Resolve the disagreement, then correct --license.")}
	}
	if detected.Expression == "" {
		// Curation with nothing to confirm it is still curation, and the
		// document says so rather than borrowing the confidence of a file it
		// could not read.
		effective.Evidence = "curated"
		effective.Confidence = domain.ConfidenceMedium
	}
	return effective, nil
}

// licenceFileNames are the names a licence is published under. The list is
// closed on purpose: searching a directory for anything licence-shaped is how
// a NOTICE or a third-party licence ends up attributed to the wrong component.
var licenceFileNames = []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "LICENCE.txt", "COPYING", "COPYING.txt"}

func (s *licenceSource) detect(componentID, dir string) (domain.LicenseFinding, []domain.Finding) {
	for _, name := range licenceFileNames {
		path := filepath.Join(dir, name)
		if _, err := s.bounds.Stat(path); err != nil {
			continue
		}
		data, err := s.bounds.ReadFile(path)
		if err != nil {
			continue
		}
		finding := license.ResolveFromText(string(data), s.label(path))
		if finding.Expression == "" {
			// The file may still hold complete licence texts without being any
			// one of them -- two licences one after the other. Which ones are
			// present is recorded as evidence; how they combine is not.
			if observed := license.ObserveFindings(string(data), s.label(path)); len(observed) > 0 {
				s.observed[componentID] = observed
				finding.Reason = license.ReasonLicenseCompositionUnresolved
				return finding, []domain.Finding{componentFinding("UNKNOWN_LICENSE", domain.SeverityWarning, componentID,
					fmt.Sprintf("%s contains the text of %s but is not any one of them; how they combine is not machine-readable",
						name, strings.Join(licenceNames(observed), " and ")),
					"Read the file and pass --license <module>=<SPDX expression>.")}
			}
			// The file is there and unrecognized. Saying so is more useful
			// than saying nothing was found: the remedy is different. Exact
			// text matching cannot see through a filled-in copyright holder or
			// a renumbered clause list, which is most real licence files.
			return finding, []domain.Finding{componentFinding("UNKNOWN_LICENSE", domain.SeverityWarning, componentID,
				fmt.Sprintf("the licence text in %s matched no known licence", name),
				"Curate the licence with --license <module>=<SPDX expression>.")}
		}
		return finding, nil
	}
	return unknownLicence(componentID)
}

// label names a licence file the way the rest of the tool names files: by
// where it sits relative to a root that is part of the build, never by its
// absolute path on the machine that ran the tool. Section 7.5 keeps host paths
// out of the document, and a licence source is no exception.
func (s *licenceSource) label(path string) string {
	for _, root := range []struct{ key, dir string }{{"", s.moduleDir}, {"goroot", s.goroot}} {
		if root.dir == "" {
			continue
		}
		relative, err := filepath.Rel(root.dir, path)
		if err != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		relative = filepath.ToSlash(relative)
		if root.key == "" {
			return relative
		}
		return root.key + "/" + relative
	}
	return filepath.Base(path)
}

func unknownLicence(componentID string) (domain.LicenseFinding, []domain.Finding) {
	return domain.LicenseFinding{
			Name:     "NOASSERTION",
			Evidence: "unknown",
			Reason:   license.ReasonNoEvidence,
		}, []domain.Finding{componentFinding("UNKNOWN_LICENSE", domain.SeverityWarning, componentID,
			"no licence evidence was found for this component",
			"Point --module-dir at a module with a vendor directory and --goroot at the toolchain, or curate with --license.")}
}

func annotateLicence(component *domain.Component, finding domain.LicenseFinding) {
	if component.Properties == nil {
		component.Properties = map[string][]string{}
	}
	if finding.Evidence != "" {
		component.Properties["sbomb:license:evidenceClass"] = []string{finding.Evidence}
	}
	if finding.Technique != "" {
		component.Properties["sbomb:license:technique"] = []string{finding.Technique}
	}
	if finding.Source != "" {
		component.Properties["sbomb:license:source"] = []string{finding.Source}
	}
	if finding.Expression == "" {
		component.Properties["sbomb:license:review"] = []string{"true"}
		component.Properties["sbomb:license:reason"] = []string{finding.Reason}
	}
}

// missingComponentHash reports that a component carries no CycloneDX hash. A
// Go module has no single artifact to hash: the module sum covers a file tree,
// and the compiled code is inside the deliverable rather than beside it.
func missingComponentHash(componentID, sum string) []domain.Finding {
	message := "this document names no file of the component, so it carries no component hash"
	if sum != "" {
		message += "; the module sum is recorded as sbomb:go:moduleSum instead"
	}
	return []domain.Finding{componentFinding("MISSING_COMPONENT_HASH", domain.SeverityWarning, componentID, message, "")}
}

// goPURL builds a purl for a Go module. version.PURL is not used because it
// percent-encodes the slashes in a name, and a golang purl carries its module
// path as a namespace with the separators intact.
func goPURL(modulePath, moduleVersion string) string {
	if modulePath == "" {
		return ""
	}
	encoded := strings.ReplaceAll(modulePath, "%", "%25")
	encoded = strings.ReplaceAll(encoded, "@", "%40")
	encoded = strings.ReplaceAll(encoded, "+", "%2B")
	if moduleVersion == "" {
		return "pkg:golang/" + encoded
	}
	return "pkg:golang/" + encoded + "@" + moduleVersion
}

func joinModule(path, version string) string {
	if version == "" {
		return path
	}
	return path + "@" + version
}

func goToolchainSuffix(binary *gobin.Binary) string {
	if binary.GoVersion == "" {
		return ""
	}
	return " " + binary.GoVersion
}

func timestamp(options Options) string {
	if options.Reproducible {
		return ""
	}
	at := options.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	return at.UTC().Format(time.RFC3339)
}

func componentFinding(id string, severity domain.Severity, componentID, message, remediation string) domain.Finding {
	return domain.Finding{
		ID:          id,
		Severity:    severity,
		Subject:     domain.Subject{Kind: "component", Ref: componentID},
		Message:     message,
		Remediation: remediation,
	}
}

func sortFindings(findings []domain.Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].ID != findings[j].ID {
			return findings[i].ID < findings[j].ID
		}
		return findings[i].Subject.Ref < findings[j].Subject.Ref
	})
}

// SourceModuleDir finds the module root that contains a directory, so that a
// caller run from anywhere inside the repository still finds go.mod. It stops
// at the filesystem root and returns false rather than guessing.
func SourceModuleDir(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
