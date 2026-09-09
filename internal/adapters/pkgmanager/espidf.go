package pkgmanager

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// espidf reads what the ESP-IDF component manager recorded about the
// components it downloaded (section 21). It is strategy 2 of section 19.2 --
// exact package-manager metadata -- and nothing more: the SDK adapter of
// strategy 4, with project_description.json, sdkconfig and the IDF's own
// components/ tree, is a separate piece of work and is not this one.
//
// Two known locations, and no search. <source>/dependencies.lock names every
// component that was resolved, with its version and where it came from, which
// is install state (rank 3). <source>/managed_components/<namespace>__<name>/
// is where the manager put the component, so that directory is the package
// root, and the idf_component.yml lying in it is the component's own declared
// manifest (rank 2), which states its licence.
//
// The IDF's own components/ directory is deliberately untouched. Those are
// part of the framework, not packages the component manager installed, and
// treating them as managed components would publish a dependency the manager
// never resolved.
type espidf struct{}

func (espidf) Manager() string { return "idf-component-manager" }

// The two file names the component manager writes. They are constants rather
// than literals because they are the entire surface this adapter touches: a
// reviewer can name every path it can open by reading these three lines.
const (
	idfLockName     = "dependencies.lock"
	idfManifestName = "idf_component.yml"
	idfManagedDir   = "managed_components"
)

// The bounds of section 30 for the files this adapter reads. A lock file lists
// one short block per resolved component and a manifest describes one
// component, so a megabyte each is generous; the component count is bounded
// separately because a lock file that is small in bytes can still name an
// absurd number of entries.
const (
	maxIDFLockBytes     = 1 << 20
	maxIDFManifestBytes = 1 << 20
	maxIDFComponents    = 10_000
)

// idfComponent is one component the evidence named, before its directory has
// been looked for. The version is the one the lock recorded, which is empty
// when the lock was absent or unreadable and the directories answered instead.
type idfComponent struct {
	namespace string
	name      string
	version   string
}

// fullName is the name the registry uses and the name this tool publishes.
// Two namespaces may hold a component of the same name, so the namespace is
// part of the identity rather than decoration.
func (c idfComponent) fullName() string {
	if c.namespace == "" {
		return c.name
	}
	return c.namespace + "/" + c.name
}

// directory is the folder the component manager unpacks the component into. It
// joins namespace and name with a double underscore, and that convention is the
// only link between a lock entry and a tree on disk; where it does not hold,
// the component is reported as missing rather than attached to a directory that
// might belong to something else.
func (c idfComponent) directory() string {
	if c.namespace == "" {
		return c.name
	}
	return c.namespace + "__" + c.name
}

func (a espidf) Discover(options Options) ([]Package, []domain.Finding) {
	source := options.SourceDir
	if source == "" || source == "." {
		return nil, nil
	}
	lockPath := filepath.Join(source, idfLockName)
	managedDir := filepath.Join(source, idfManagedDir)
	lockPresent := fileExists(lockPath)
	managedPresent := dirExists(managedDir)
	if !lockPresent && !managedPresent {
		// A project that does not use the component manager has nothing
		// missing about it, so nothing is opened and nothing is reported.
		return nil, nil
	}

	findings := make([]domain.Finding, 0)
	// A lock file that was read settles the question on its own, even when it
	// names nothing: it is the manager's record of what the project depends on,
	// and a directory it does not mention is what an earlier resolve left
	// behind. Only a lock that is absent or was refused hands the question on.
	components, fromLock := a.componentsFromLock(lockPath, lockPresent, &findings)
	if !fromLock {
		// The lock is the better evidence, but its absence is not the end of
		// the run: the directories the manager filled still name the
		// components, and each one's own manifest still states a version. That
		// is weaker -- a manifest says what the component claims to be, not
		// what was resolved -- and the components discovered this way say so
		// through the rank their version carries.
		components = a.componentsFromDirectories(managedDir, &findings)
	}

	packages := make([]Package, 0, len(components))
	seen := map[string]bool{}
	for _, component := range components {
		if seen[component.fullName()] {
			// One anchor key per package (section 21), and a key registered
			// twice aborts the run. Two entries naming one component is a
			// contradiction in the evidence, and the first is kept.
			continue
		}
		seen[component.fullName()] = true
		found, componentFindings, ok := a.packageFor(managedDir, component)
		findings = append(findings, componentFindings...)
		if ok {
			packages = append(packages, found)
		}
	}
	return packages, findings
}

// componentsFromLock reads dependencies.lock. The second return says whether
// the lock answered at all, so that a missing or refused lock falls back to the
// directories instead of blanking the run.
func (a espidf) componentsFromLock(path string, present bool, findings *[]domain.Finding) ([]idfComponent, bool) {
	if !present {
		return nil, false
	}
	document, ok := a.readYAMLFile(path, maxIDFLockBytes, "lock file", findings)
	if !ok {
		return nil, false
	}
	dependencies := document.child("dependencies")
	if dependencies == nil || dependencies.mapping == nil {
		*findings = append(*findings, idfEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the ESP-IDF lock file names no dependencies mapping, so no component was read from it"))
		return nil, false
	}
	keys := dependencies.keysOf()
	if len(keys) > maxIDFComponents {
		// Refused whole rather than in part: half a lock file would attribute
		// some components and leave the rest looking as though the project
		// never depended on them.
		*findings = append(*findings, idfEvidenceFinding("INPUT_LIMIT_EXCEEDED", path,
			"the ESP-IDF lock file names more components than the parser limit of section 30, so no component was read from it"))
		return nil, false
	}

	components := make([]idfComponent, 0, len(keys))
	for _, key := range keys {
		entry := dependencies.child(key)
		sourceType := entry.scalarAt("source", "type")
		// The framework itself is recorded as a dependency of every project and
		// is not a package the manager installed. A local component's files lie
		// in the project's own tree, where whichever strategy already covers
		// them keeps covering them; calling it a managed package would move a
		// boundary on the strength of a lock entry.
		if key == "idf" || sourceType == "idf" || sourceType == "local" || sourceType == "path" {
			continue
		}
		component, ok := idfComponentFromKey(key)
		if !ok {
			*findings = append(*findings, idfEvidenceFinding("EVIDENCE_UNREADABLE", path,
				fmt.Sprintf("the ESP-IDF lock file names a dependency %q, which is not a component name this tool can resolve to a directory, so it was skipped", key)))
			continue
		}
		component.version = entry.scalarAt("version")
		components = append(components, component)
	}
	return components, true
}

// componentsFromDirectories enumerates managed_components. It lists one
// directory that the manager itself filled, which is a known location and not
// a search of the source tree.
func (a espidf) componentsFromDirectories(managedDir string, findings *[]domain.Finding) []idfComponent {
	entries, err := os.ReadDir(managedDir)
	if err != nil {
		return nil
	}
	if len(entries) > maxIDFComponents {
		*findings = append(*findings, idfEvidenceFinding("INPUT_LIMIT_EXCEEDED", managedDir,
			"managed_components holds more entries than the parser limit of section 30, so none of them was read"))
		return nil
	}
	components := make([]idfComponent, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if component, ok := idfComponentFromDirectory(entry.Name()); ok {
			components = append(components, component)
		}
	}
	return components
}

// packageFor turns one named component into a package, or explains why it
// could not. Everything published about it comes from the two files below and
// from nothing else.
func (a espidf) packageFor(managedDir string, component idfComponent) (Package, []domain.Finding, bool) {
	root := filepath.Join(managedDir, component.directory())
	if !dirExists(root) {
		// A lock entry records what was resolved; without the directory the
		// component owns no file, and a package owning nothing would name a
		// component that no evidence chain can ever reach. conan.go refuses a
		// dependency without a package folder in the same way.
		return Package{}, []domain.Finding{{
			ID: "MISSING_PACKAGE_EVIDENCE", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "component", Ref: component.fullName()},
			Message: fmt.Sprintf("the ESP-IDF lock file resolved this component, but %s does not exist, so none of its files can be attributed to it",
				root),
		}}, false
	}

	found := Package{
		Name:      component.fullName(),
		Roots:     []string{root},
		Manager:   a.Manager(),
		AnchorKey: "pkg:idf/" + component.fullName(),
	}
	// The lock is asked first, and it wins: it records the version the manager
	// actually resolved and unpacked, while the manifest beside it states what
	// the component says about itself. Both are kept -- Take pushes the loser
	// into Superseded -- because a disagreement between the two is worth
	// reporting rather than dropping.
	found.Take(FieldVersion, Claim{
		Value: component.version, Source: idfVersionSource, Rank: RankInstallState,
		// Section 20.3: a package manager states an exact declared version.
		Confidence: domain.ConfidenceHigh,
	})

	findings := make([]domain.Finding, 0)
	manifestPath := filepath.Join(root, idfManifestName)
	if fileExists(manifestPath) {
		if manifest, ok := a.readYAMLFile(manifestPath, maxIDFManifestBytes, "component manifest", &findings); ok {
			found.Take(FieldVersion, Claim{
				Value: manifest.scalarAt("version"), Source: idfVersionSource, Rank: RankDeclaredManifest,
				Confidence: domain.ConfidenceHigh,
			})
			// Section 22.2 ranks a manager's declared licence above a licence
			// file found by walking the component root.
			found.Take(FieldLicense, Claim{
				Value: manifest.scalarAt("license"), Source: a.Manager(), Rank: RankDeclaredManifest,
			})
			// `repository` is the manifest's own statement about where the
			// component's sources live. `url` is not read: it is a home page,
			// which this tool has nowhere to put, and section 20.5 forbids
			// turning either of them into a supplier.
			found.VCSURL = NormalizeVCSURL(manifest.scalarAt("repository"))
		}
	}

	if found.Version.Value == "" {
		findings = append(findings, domain.Finding{
			ID: "UNKNOWN_VERSION", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "component", Ref: found.Name},
			Message: "neither the ESP-IDF lock file nor the component's own manifest states a version for this managed component",
		})
	}
	// The purl restates whichever version claim won, so it is taken with that
	// claim's standing; where no version was found it still names the component
	// the manager unpacked here.
	purlRank := found.Version.Rank
	if purlRank == RankNone {
		purlRank = RankInstallState
	}
	found.Take(FieldPURL, Claim{
		Value:  idfPURL(component.namespace, component.name, found.Version.Value),
		Source: a.Manager(), Rank: purlRank,
	})
	return found, findings, true
}

// readYAMLFile reads one bounded file through the subset reader of yamlsubset.go.
// A file over its bound or one the reader refuses contributes nothing at all:
// a value taken out of a file whose remainder could not be read would be
// published with nothing behind it.
func (espidf) readYAMLFile(path string, maxBytes int64, kind string, findings *[]domain.Finding) (*yamlNode, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if info.Size() > maxBytes {
		*findings = append(*findings, idfEvidenceFinding("INPUT_LIMIT_EXCEEDED", path,
			fmt.Sprintf("the ESP-IDF %s is larger than the parser limit of section 30, so nothing was read from it", kind)))
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		*findings = append(*findings, idfEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the ESP-IDF %s could not be read, so nothing was taken from it", kind)))
		return nil, false
	}
	document, err := parseYAMLSubset(data)
	if err != nil {
		*findings = append(*findings, idfEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the ESP-IDF %s holds %s, so nothing was taken from it", kind, err.Error())))
		return nil, false
	}
	return document, true
}

// idfEvidenceFinding reports a file that was found but could not be used. The
// subject is the file rather than the component: what could not be read is a
// piece of evidence, and there may not be a component to name yet.
func idfEvidenceFinding(id, path, message string) domain.Finding {
	return domain.Finding{
		ID: id, Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: path},
		Message: message,
	}
}

// idfVersionSource is the source string a version claim from either of these
// two files carries. It reaches the document as the value beside the technique
// of section 20.3, so it is output rather than an internal label, and
// internal/cyclonedx maps it onto manifest-analysis.
const idfVersionSource = "idf"

// idfComponentFromKey reads a lock file's dependency key, which is
// <namespace>/<name> for a registry component and a bare name otherwise.
func idfComponentFromKey(key string) (idfComponent, bool) {
	namespace, name, hasNamespace := strings.Cut(key, "/")
	if !hasNamespace {
		namespace, name = "", key
	}
	if namespace != "" && !idfSafeSegment(namespace) {
		return idfComponent{}, false
	}
	if !idfSafeSegment(name) {
		return idfComponent{}, false
	}
	return idfComponent{namespace: namespace, name: name}, true
}

// idfComponentFromDirectory reads the <namespace>__<name> convention back. The
// first double underscore separates the two: a component name may carry single
// underscores -- led_strip does -- and the manager joins on the first pair.
func idfComponentFromDirectory(dir string) (idfComponent, bool) {
	namespace, name, joined := strings.Cut(dir, "__")
	if !joined {
		namespace, name = "", dir
	}
	if namespace != "" && !idfSafeSegment(namespace) {
		return idfComponent{}, false
	}
	if !idfSafeSegment(name) {
		return idfComponent{}, false
	}
	return idfComponent{namespace: namespace, name: name}, true
}

// idfSafeSegment reports whether a name may be joined onto a path. Section 30.3
// again: a lock file is somebody else's file, and a dependency named ".." or
// "/etc" must not be able to point a component root out of managed_components.
func idfSafeSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	if strings.HasPrefix(segment, ".") {
		return false
	}
	// Both separators are refused, not only this platform's: a lock file
	// written on Windows is read on Linux and the other way round.
	if strings.ContainsAny(segment, `/\`) {
		return false
	}
	return !strings.ContainsAny(segment, "\x00\n\r")
}

// idfPURL builds the purl of section 20.4:
// pkg:idf/<namespace>/<name>@<version>, each segment percent-encoded on its
// own.
//
// It is here rather than in internal/version because version.PURL escapes a
// slash in the name to %2F, which is right for a type that has no namespace and
// wrong for this one -- pkg:idf/espressif%2Fled_strip@1.0 is not the form the
// specification prescribes. Widening that shared helper for one adapter would
// touch every purl the tool writes, so this mirrors GenericPURL in
// fetchcontent.go instead and escapes per segment.
func idfPURL(namespace, name, version string) string {
	if name == "" {
		return ""
	}
	purl := "pkg:idf/"
	if namespace != "" {
		purl += url.PathEscape(namespace) + "/"
	}
	purl += url.PathEscape(name)
	if version != "" {
		purl += "@" + url.PathEscape(version)
	}
	return purl
}
