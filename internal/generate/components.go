package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/adapters/pkgmanager"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/componentmap"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/version"
)

// packageMetadataFiles are the manifests that mark a directory as the root of
// a distinct software component, used by strategy 6 of section 19.2.
var packageMetadataFiles = []string{
	"conanfile.txt", "conanfile.py", "conandata.yml",
	"vcpkg.json", "CONTROL",
	"idf_component.yml",
	"Cargo.toml",
	"west.yml",
}

// componentResolver maps used files onto components, following the priority
// order of section 19.2. Only strategies 1, 6, 7 and 8 exist so far; package
// managers, SDK layouts and submodule boundaries are later work (D7).
type componentResolver struct {
	curated     *componentmap.Mapper
	curatedByID map[string]config.Component
	physical    map[string]string
	projectName string
	anchorRoots map[string]string
	logger      *Logger
	// packages are what the package-manager adapters proved, keyed by the
	// canonical identity prefix their root corresponds to. This is strategy 2
	// of section 19.2, which outranks everything except curated configuration.
	packages []resolvedPackage
}

// resolvedPackage is one package-manager result expressed in identity terms,
// so that mapping a file needs no filesystem access.
type resolvedPackage struct {
	// id is the package root as an identity. Matching happens on the anchor
	// and the relative path separately: a package that received its own anchor
	// has an empty relative path, and string surgery on the canonical form
	// would have to special-case that.
	id  domain.FileID
	pkg pkgmanager.Package
}

func newComponentResolver(cfg config.Config, physical map[string]string, anchorRoots map[string]string, logger *Logger) *componentResolver {
	rules := make([]componentmap.Rule, 0, len(cfg.Components))
	curatedByID := make(map[string]config.Component, len(cfg.Components))
	for _, entry := range cfg.Components {
		name := entry.Name
		if name == "" {
			name = filepath.Base(strings.TrimSuffix(entry.Path, "/"))
		}
		rules = append(rules, componentmap.Rule{
			Path:  entry.Path,
			Match: entry.Match,
			Name:  name,
			Type:  entry.Type,
		})
		curatedByID["component:"+name] = entry
	}
	projectName := cfg.Project.Name
	if projectName == "" {
		projectName = "project"
	}
	return &componentResolver{
		curated:     componentmap.NewMapper(rules),
		curatedByID: curatedByID,
		physical:    physical,
		projectName: projectName,
		anchorRoots: anchorRoots,
		logger:      logger,
	}
}

// resolve names the component a file belongs to and records which strategy
// found it, so that the review report can show how a mapping was reached.
func (r *componentResolver) resolve(file domain.UsedFile) (id, name, componentType, scope, detectedBy string) {
	// Strategy 1: curated configuration is the highest authority.
	if mapped, ok := r.curated.MapFile(file.ID); ok {
		return "component:" + mapped.Name, mapped.Name, componentTypeOrDefault(mapped.Type), string(anchors.ScopeThirdParty), "curated"
	}

	// Strategy 2: exact package-manager metadata. A manager states the name,
	// the version and often the licence, which is better evidence than any
	// directory layout, so it comes before every heuristic below.
	if found, ok := r.packageFor(file); ok {
		return "component:" + found.pkg.Name, found.pkg.Name, "library",
			string(anchors.ScopeThirdParty), found.pkg.Manager
	}

	anchorKey := string(file.ID.Anchor)
	kind, anchorName, _ := strings.Cut(anchorKey, ":")

	// Strategy 6: the nearest ancestor directory carrying package metadata.
	// Only inside the file's own anchor, so that the search cannot walk out of
	// the project (section 22.1).
	if root, manifest, found := r.nearestPackageRoot(file); found {
		componentName := filepath.Base(root)
		return "component:" + componentName, componentName, "library", string(anchors.ScopeThirdParty), "package-metadata:" + manifest
	}

	// Strategy 7: the anchor root itself.
	switch kind {
	case "project", "build":
		return "component:" + r.projectName, r.projectName, "application", string(anchors.ScopeProject), "anchor:project"
	case "pkg", "sdk", "extern":
		return anchorKey, anchorName, "library", scopeForAnchorKind(kind), "anchor:" + kind
	case "toolchain":
		return anchorKey, anchorName, "library", string(anchors.ScopeToolchain), "anchor:toolchain"
	case "sysroot":
		return anchorKey, anchorName, "library", string(anchors.ScopeSystem), "anchor:sysroot"
	}

	// Strategy 8: unknown, emitted and flagged rather than dropped (19.3).
	segment := file.ID.RelPath
	if index := strings.IndexByte(segment, '/'); index >= 0 {
		segment = segment[:index]
	}
	unknown := "unknown:" + anchorKey + "/" + segment
	return unknown, unknown, "library", string(anchors.ScopeUnknown), "unresolved"
}

// setPackages records what the package-manager adapters found, expressed as
// canonical identity prefixes. The longest prefix wins, so a package nested
// inside another maps to the inner one (section 19.2).
func (r *componentResolver) setPackages(packages []pkgmanager.Package, identify func(string) domain.FileID) {
	r.packages = make([]resolvedPackage, 0, len(packages))
	for _, entry := range packages {
		id := identify(entry.Root)
		if id.Anchor == "" {
			continue
		}
		// A path that is its own anchor root comes back with "." as the
		// relative part; the package then covers everything under the anchor.
		if id.RelPath == "." || id.RelPath == "/" {
			id.RelPath = ""
		}
		r.logger.Debug("Package %s maps to identity %s", entry.Name, id.Canonical())
		r.packages = append(r.packages, resolvedPackage{id: id, pkg: entry})
	}
	sort.Slice(r.packages, func(i, j int) bool {
		return len(r.packages[i].id.RelPath) > len(r.packages[j].id.RelPath)
	})
}

// packageFor finds the package a file belongs to, matching on whole path
// segments so that a sibling directory with a shared prefix cannot claim it.
func (r *componentResolver) packageFor(file domain.UsedFile) (resolvedPackage, bool) {
	for _, entry := range r.packages {
		if file.ID.Anchor != entry.id.Anchor {
			continue
		}
		root := entry.id.RelPath
		// An empty relative path means the package is the anchor root, so
		// everything under that anchor belongs to it.
		if root == "" || file.ID.RelPath == root || strings.HasPrefix(file.ID.RelPath, root+"/") {
			return entry, true
		}
	}
	return resolvedPackage{}, false
}

// packageByName finds the package behind a component, for enrichment.
func (r *componentResolver) packageByName(name string) (pkgmanager.Package, bool) {
	for _, entry := range r.packages {
		if entry.pkg.Name == name {
			return entry.pkg, true
		}
	}
	return pkgmanager.Package{}, false
}

// nearestPackageRoot walks up from a file looking for a package manifest,
// stopping at the anchor root so the search never leaves the component tree.
func (r *componentResolver) nearestPackageRoot(file domain.UsedFile) (root, manifest string, found bool) {
	path := r.physical[file.ID.Canonical()]
	if path == "" {
		return "", "", false
	}
	boundary := r.anchorRoots[string(file.ID.Anchor)]
	dir := filepath.Dir(path)
	for depth := 0; depth < 64 && dir != "" && dir != "/" && dir != "."; depth++ {
		for _, name := range packageMetadataFiles {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
				return dir, name, true
			}
		}
		if boundary != "" && filepath.Clean(dir) == filepath.Clean(boundary) {
			return "", "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
	return "", "", false
}

func componentTypeOrDefault(value string) string {
	if value == "" {
		return "library"
	}
	return value
}

// enrichComponent fills in the CRA fields of section 1.5(1): version,
// supplier, license and purl. Each one is either resolved from an authorized
// source or reported as missing; nothing is guessed from a directory name.
func (r *componentResolver) enrichComponent(component *domain.Component, files []domain.UsedFile) []domain.Finding {
	findings := []domain.Finding{}
	curated, hasCurated := r.curatedByID[component.ID]
	managed, isManaged := r.packageByName(component.Name)

	// Version (section 20.2): curated first, then exact package-manager
	// metadata, then what curated versionFrom permits.
	switch {
	case hasCurated && curated.Version != "":
		component.Version = curated.Version
		component.VersionSource = "curated"
		component.VersionConf = domain.ConfidenceHigh
	case isManaged && managed.Version != "":
		component.Version = managed.Version
		component.VersionSource = managed.VersionSource
		component.VersionConf = managed.VersionConfidence
	case hasCurated && len(curated.VersionFrom) > 0:
		root := r.componentRoot(files)
		if value, source, confidence, ok := version.Resolve(*component, root, curated.VersionFrom); ok {
			component.Version, component.VersionSource, component.VersionConf = value, source, confidence
		}
	}
	if component.Version == "" {
		findings = append(findings, componentFinding("UNKNOWN_VERSION", domain.SeverityWarning, component,
			"no authorized source supplied a version",
			"Add components[].version, or components[].versionFrom naming where the version can be read."))
	}

	// Supplier (section 20.5): curated or package metadata only. Deriving it
	// from a repository URL host is explicitly forbidden.
	if hasCurated && curated.Supplier != "" {
		component.Supplier = curated.Supplier
	} else if isManaged && managed.Supplier != "" {
		component.Supplier = managed.Supplier
	}
	if component.Supplier == "" {
		findings = append(findings, componentFinding("MISSING_SUPPLIER", domain.SeverityWarning, component,
			"the component has no supplier, which BSI TR-03183-2 requires",
			"Add components[].supplier for this component."))
	}

	// PURL (section 20.4): only when a package type and name can be asserted.
	if hasCurated && curated.PURL != "" {
		component.PURL = curated.PURL
	} else if isManaged && managed.PURL != "" {
		component.PURL = managed.PURL
	} else if kind, name, ok := purlFromAnchor(component.ID); ok {
		component.PURL = version.PURL(kind, name, component.Version)
	}
	if isManaged {
		// Section 20.5: the repository URL belongs in externalReferences, and
		// it is emitted only because the manager recorded it -- never as a
		// stand-in for a supplier.
		component.Properties = addProperty(component.Properties, "sbomb:component:vcsUrl", managed.VCSURL)
		if managed.Commit != "" {
			component.Properties = addProperty(component.Properties, "sbomb:component:vcsCommit", managed.Commit)
		}
		if managed.Dirty {
			component.Properties = addProperty(component.Properties, "sbomb:component:vcsDirty", "true")
		}
	}
	if component.PURL == "" {
		findings = append(findings, componentFinding("UNKNOWN_PURL", domain.SeverityInfo, component,
			"no package type could be asserted, so no purl is emitted", ""))
	}

	// Licenses (section 22.2).
	licenseFindings := r.resolveComponentLicense(component, files, curated, hasCurated)
	findings = append(findings, licenseFindings...)

	// A component with no hashable file cannot carry a component hash
	// (section 1.5(1) via the MISSING_COMPONENT_HASH gate).
	var hashed int
	for _, file := range files {
		if len(file.Hashes) > 0 {
			hashed++
		}
	}
	if hashed == 0 {
		findings = append(findings, componentFinding("MISSING_COMPONENT_HASH", domain.SeverityWarning, component,
			"no file of this component could be hashed", ""))
	}

	if strings.HasPrefix(component.Name, "unknown:") {
		findings = append(findings, componentFinding("UNKNOWN_COMPONENT", domain.SeverityWarning, component,
			fmt.Sprintf("%d file(s) could not be mapped to a known component", len(files)),
			"Add a components[] entry covering these paths."))
	}
	return findings
}

// resolveComponentLicense applies the priority order of section 22.2, limited
// to the sources that exist today: curated configuration, an SPDX identifier
// in a used file, and a recognized license file in the component root.
func (r *componentResolver) resolveComponentLicense(component *domain.Component, files []domain.UsedFile, curated config.Component, hasCurated bool) []domain.Finding {
	findings := []domain.Finding{}

	// Section 22.2 in order: an SPDX identifier in a used file (2), then what
	// the package manager declared or placed in the package (3 and 4), then a
	// licence file found by walking the component root (5).
	var fromFiles domain.LicenseFinding
	for _, file := range files {
		path := r.physical[file.ID.Canonical()]
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if found := license.ResolveFromText(string(data), path); found.Expression != "" {
			fromFiles = found
			break
		}
	}
	if fromFiles.Expression == "" {
		if found, ok := r.licenseFromPackageManager(component.Name); ok {
			fromFiles = found
		}
	}
	if fromFiles.Expression == "" {
		if found, ok := r.licenseFromComponentRoot(files); ok {
			fromFiles = found
		}
	}

	switch {
	case hasCurated && curated.License != "":
		effective := domain.LicenseFinding{
			Expression: curated.License,
			Evidence:   "component-level",
			Confidence: domain.ConfidenceHigh,
			Source:     "curated",
		}
		// Section 22.5: a conflict is never resolved silently. The curated
		// value stays effective and the conflicting one is recorded.
		if conflict, differs := license.ResolveConflict(curated.License, fromFiles.Expression, fromFiles.Source); differs {
			component.Licenses = []domain.LicenseFinding{conflict}
			component.Properties = addProperty(component.Properties, "sbomb:license:conflictingValue", fromFiles.Expression)
			component.Properties = addProperty(component.Properties, "sbomb:review:required", "true")
			findings = append(findings, componentFinding("LICENSE_CONFLICT", domain.SeverityWarning, component,
				fmt.Sprintf("configuration says %q but the component's own files say %q", curated.License, fromFiles.Expression),
				"Resolve the disagreement, then update components[].license."))
			return findings
		}
		component.Licenses = []domain.LicenseFinding{effective}
	case fromFiles.Expression != "":
		component.Licenses = []domain.LicenseFinding{fromFiles}
	default:
		component.Licenses = []domain.LicenseFinding{{
			Name:     "NOASSERTION",
			Evidence: "unknown",
			Reason:   license.ReasonNoEvidence,
		}}
		component.Properties = addProperty(component.Properties, "sbomb:license:review", "true")
		component.Properties = addProperty(component.Properties, "sbomb:license:reason", license.ReasonNoEvidence)
		findings = append(findings, componentFinding("UNKNOWN_LICENSE", domain.SeverityWarning, component,
			"no license evidence was found for this component",
			"Add components[].license, or place a recognized license file in the component root."))
	}

	if len(component.Licenses) > 0 {
		component.Properties = addProperty(component.Properties, "sbomb:license:evidenceClass", component.Licenses[0].Evidence)
		if component.Licenses[0].Source != "" {
			component.Properties = addProperty(component.Properties, "sbomb:license:source", component.Licenses[0].Source)
		}
	}
	return findings
}

// licenseFromPackageManager uses what the manager declared, or the licence
// file it placed in the package itself. Both are stronger than walking a
// directory looking for something licence-shaped: the manager put it there and
// says which package it belongs to.
func (r *componentResolver) licenseFromPackageManager(name string) (domain.LicenseFinding, bool) {
	managed, ok := r.packageByName(name)
	if !ok {
		return domain.LicenseFinding{}, false
	}
	if managed.License != "" {
		return domain.LicenseFinding{
			Expression: managed.License,
			Evidence:   "component-level",
			Confidence: domain.ConfidenceHigh,
		}, true
	}
	if managed.LicenseFile == "" {
		return domain.LicenseFinding{}, false
	}
	found, err := license.ResolveFile(managed.LicenseFile)
	if err != nil || found.Expression == "" {
		return domain.LicenseFinding{}, false
	}
	found.Evidence = "component-level"
	return found, true
}

// licenseFromComponentRoot looks for a recognized license file, but only in
// the component root itself. Section 22.1 forbids scanning the repository for
// license files outside mapped component roots.
func (r *componentResolver) licenseFromComponentRoot(files []domain.UsedFile) (domain.LicenseFinding, bool) {
	root := r.componentRoot(files)
	if root == "" {
		return domain.LicenseFinding{}, false
	}
	for _, name := range recognizedLicenseFiles {
		found, err := license.ResolveFile(filepath.Join(root, name))
		if err == nil && found.Expression != "" {
			found.Evidence = "component-level"
			found.Source = name
			return found, true
		}
	}
	return domain.LicenseFinding{}, false
}

// recognizedLicenseFiles is the exhaustive list of section 22.3.
var recognizedLicenseFiles = []string{
	"LICENSE", "LICENSE.txt", "LICENSE.md",
	"LICENCE", "LICENCE.txt", "LICENCE.md",
	"COPYING", "COPYING.txt", "COPYING.md",
	"NOTICE", "COPYRIGHT",
}

// componentRoot is the deepest directory containing every file of a component,
// which is where its license file and version header would live.
func (r *componentResolver) componentRoot(files []domain.UsedFile) string {
	var common string
	for _, file := range files {
		path := r.physical[file.ID.Canonical()]
		if path == "" {
			continue
		}
		dir := filepath.Dir(path)
		if common == "" {
			common = dir
			continue
		}
		common = commonDirectory(common, dir)
	}
	return common
}

func commonDirectory(a, b string) string {
	aParts := strings.Split(filepath.Clean(a), string(filepath.Separator))
	bParts := strings.Split(filepath.Clean(b), string(filepath.Separator))
	var shared []string
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		if aParts[i] != bParts[i] {
			break
		}
		shared = append(shared, aParts[i])
	}
	joined := strings.Join(shared, string(filepath.Separator))
	if joined == "" {
		return string(filepath.Separator)
	}
	return joined
}

// purlFromAnchor asserts a package type from an anchor key, which is the only
// place a package type is currently known (section 20.4).
func purlFromAnchor(componentID string) (kind, name string, ok bool) {
	prefix, rest, found := strings.Cut(componentID, ":")
	if !found || prefix != "pkg" {
		return "", "", false
	}
	packageType, packageName, split := strings.Cut(rest, "/")
	if !split {
		return "", "", false
	}
	return packageType, packageName, true
}

func componentFinding(id string, severity domain.Severity, component *domain.Component, message, remediation string) domain.Finding {
	return domain.Finding{
		ID:          id,
		Severity:    severity,
		Subject:     domain.Subject{Kind: "component", Ref: component.ID},
		Message:     message,
		Remediation: remediation,
	}
}

func sortFindings(findings []domain.Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].ID != findings[j].ID {
			return findings[i].ID < findings[j].ID
		}
		if findings[i].Subject.Kind != findings[j].Subject.Kind {
			return findings[i].Subject.Kind < findings[j].Subject.Kind
		}
		return findings[i].Subject.Ref < findings[j].Subject.Ref
	})
}
