package pkgmanager

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
	"github.com/example/sbomb/internal/pathmodel"
)

// vcpkg reads the SPDX document vcpkg writes for every package it installs
// (section 21). It is the best evidence any of these managers produces: the
// name, the version and the purl are stated outright, so nothing has to be
// reconstructed from a layout.
type vcpkg struct{}

func (vcpkg) Manager() string { return "vcpkg" }

// The bounds of section 30 for the two files this adapter reads. The SPDX
// document is one package's metadata; a file list can name every header of a
// large library, so it is bounded by entries as well as by bytes.
const (
	maxSPDXBytes       = 8 << 20
	maxFileListBytes   = 8 << 20
	maxFileListEntries = 100_000
)

// spdxDocument is the part of vcpkg.spdx.json this adapter reads.
type spdxDocument struct {
	Packages []struct {
		Name             string `json:"name"`
		VersionInfo      string `json:"versionInfo"`
		LicenseConcluded string `json:"licenseConcluded"`
		LicenseDeclared  string `json:"licenseDeclared"`
		Supplier         string `json:"supplier"`
		Homepage         string `json:"homepage"`
		ExternalRefs     []struct {
			ReferenceCategory string `json:"referenceCategory"`
			ReferenceType     string `json:"referenceType"`
			ReferenceLocator  string `json:"referenceLocator"`
		} `json:"externalRefs"`
	} `json:"packages"`
}

func (a vcpkg) Discover(options Options) ([]Package, []domain.Finding) {
	roots := a.installRoots(options.BuildDir)
	packages := make([]Package, 0)
	findings := make([]domain.Finding, 0)
	seen := map[string]bool{}

	for _, tree := range roots {
		matches, err := filepath.Glob(filepath.Join(tree.root, "share", "*", "vcpkg.spdx.json"))
		if err != nil {
			continue
		}
		sort.Strings(matches)
		for _, path := range matches {
			found, ok := a.readPackage(path, tree.root)
			if !ok || seen[found.Name] {
				continue
			}
			seen[found.Name] = true
			// What the package installed is a list vcpkg wrote itself, so the
			// headers and libraries in the shared triplet tree can be looked up
			// instead of guessed from a path.
			files, listFindings := a.installedFiles(tree, found.Name)
			found.Files = files
			findings = append(findings, listFindings...)
			if found.Version == "" {
				findings = append(findings, domain.Finding{
					ID: "UNKNOWN_VERSION", Severity: domain.SeverityWarning,
					Subject: domain.Subject{Kind: "component", Ref: found.Name},
					Message: "the vcpkg SPDX document states no version for this package",
				})
			}
			packages = append(packages, found)
		}
	}
	return packages, findings
}

// installTree is one per-triplet install tree. The base and the triplet are
// kept apart from the joined path because the per-package file lists live
// beside the triplet trees, under base/vcpkg/info, and name their entries with
// the triplet in front.
type installTree struct {
	base    string
	triplet string
	root    string
}

// installRoots finds the per-triplet install trees. vcpkg puts them under
// vcpkg_installed in manifest mode and under an --x-install-root otherwise, so
// both shapes are searched, one directory deep.
func (vcpkg) installRoots(buildDir string) []installTree {
	roots := make([]installTree, 0)
	for _, base := range []string{
		filepath.Join(buildDir, "vcpkg_installed"),
		filepath.Join(buildDir, "installed"),
	} {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "vcpkg" {
				roots = append(roots, installTree{
					base:    base,
					triplet: entry.Name(),
					root:    filepath.Join(base, entry.Name()),
				})
			}
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].root < roots[j].root })
	return roots
}

// installedFiles reads the list vcpkg wrote when it installed the package.
// This is the only evidence that separates one package from another inside a
// triplet tree: every port's headers land in the same include directory and
// every library in the same lib directory, so a path prefix cannot tell them
// apart, while the list names each file outright.
//
// A missing list is not a finding. It improves an attribution that already
// works without it, and the run should not report evidence it never required.
func (vcpkg) installedFiles(tree installTree, name string) ([]string, []domain.Finding) {
	// vcpkg names the list <name>_<version>_<triplet>.list. The version is not
	// matched: the port version and the version in the SPDX document need not
	// be spelled the same, and a wrong guess would silently read nothing.
	matches, err := filepath.Glob(filepath.Join(tree.base, "vcpkg", "info", name+"_*_"+tree.triplet+".list"))
	if err != nil || len(matches) != 1 {
		// Two lists mean two versions of one port are installed side by side.
		// Choosing between them would be a guess, so neither is read.
		return nil, nil
	}
	path := matches[0]
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}
	if info.Size() > maxFileListBytes {
		return nil, []domain.Finding{fileListLimitFinding(path,
			"the vcpkg file list is larger than the parser limit of section 30, so the package's files were not read from it")}
	}
	handle, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer handle.Close()

	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, limits.InitialBuffer), limits.MaxLine)
	files := make([]string, 0)
	prefix := tree.triplet + "/"
	for scanner.Scan() {
		if len(files) >= maxFileListEntries {
			// The list is refused whole rather than in part: a truncated list
			// would attribute some of the package's files and quietly leave the
			// rest to the heuristics, with nothing to say which is which.
			return nil, []domain.Finding{fileListLimitFinding(path,
				"the vcpkg file list names more entries than the parser limit of section 30, so the package's files were not read from it")}
		}
		entry := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		// A directory entry ends in a slash and names no file.
		if entry == "" || strings.HasSuffix(entry, "/") {
			continue
		}
		relative, ok := strings.CutPrefix(entry, prefix)
		if !ok || relative == "" {
			continue
		}
		// Section 30.3: a listed path that is absolute or walks upwards would
		// name a file outside the install tree, which no install list may do.
		if pathmodel.IsAbsolute(relative) || strings.HasPrefix(relative, "/") || pathEscapes(relative) {
			continue
		}
		files = append(files, filepath.Join(tree.root, filepath.FromSlash(relative)))
	}
	if err := scanner.Err(); err != nil {
		return nil, []domain.Finding{fileListLimitFinding(path,
			"the vcpkg file list could not be read to its end, so the package's files were not read from it")}
	}
	sort.Strings(files)
	return files, nil
}

// pathEscapes reports whether a slash-separated relative path leaves the
// directory it is relative to.
func pathEscapes(relative string) bool {
	for _, segment := range strings.Split(relative, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

// fileListLimitFinding reports a list that was found but could not be used.
// The subject is the list itself, not the component: what could not be read is
// a piece of evidence, and the package is still known without it.
func fileListLimitFinding(path, message string) domain.Finding {
	return domain.Finding{
		ID: "INPUT_LIMIT_EXCEEDED", Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: path},
		Message: message,
	}
}

func (a vcpkg) readPackage(path, root string) (Package, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxSPDXBytes {
		return Package{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Package{}, false
	}
	var document spdxDocument
	if err := json.Unmarshal(data, &document); err != nil || len(document.Packages) == 0 {
		return Package{}, false
	}
	// The first entry is the port itself; the later ones describe the build for
	// a triplet and carry an abi hash where a version belongs.
	entry := document.Packages[0]
	if entry.Name == "" {
		return Package{}, false
	}
	found := Package{
		Name:              entry.Name,
		Version:           entry.VersionInfo,
		VersionSource:     "vcpkg",
		VersionConfidence: domain.ConfidenceHigh,
		Manager:           a.Manager(),
		AnchorKey:         "pkg:vcpkg/" + entry.Name,
		// The share directory is what belongs to this package by layout and
		// holds its copyright file; headers and libraries are merged into the
		// triplet tree and cannot be attributed to one package from the layout
		// alone, which is what the installed file list answers instead.
		Roots: []string{filepath.Join(root, "share", entry.Name)},
	}
	if found.Version == "" {
		found.VersionSource = ""
		found.VersionConfidence = ""
	}
	// NOASSERTION is vcpkg saying it does not know, which is not a licence.
	if declared := firstNonNoAssertion(entry.LicenseDeclared, entry.LicenseConcluded); declared != "" {
		found.License = declared
	}
	if supplier := strings.TrimPrefix(entry.Supplier, "Organization: "); supplier != "" && supplier != "NOASSERTION" {
		found.Supplier = supplier
	}
	for _, reference := range entry.ExternalRefs {
		if reference.ReferenceType == "purl" && reference.ReferenceLocator != "" {
			found.PURL = reference.ReferenceLocator
			break
		}
	}
	if found.PURL == "" && found.Version != "" {
		found.PURL = "pkg:vcpkg/" + entry.Name + "@" + found.Version
	}
	if copyright := filepath.Join(found.Root(), "copyright"); fileExists(copyright) {
		found.LicenseFile = copyright
	}
	return found, true
}

func firstNonNoAssertion(values ...string) string {
	for _, value := range values {
		if value != "" && value != "NOASSERTION" && value != "NONE" {
			return value
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
