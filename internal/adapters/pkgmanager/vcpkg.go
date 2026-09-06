package pkgmanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// vcpkg reads the SPDX document vcpkg writes for every package it installs
// (section 21). It is the best evidence any of these managers produces: the
// name, the version and the purl are stated outright, so nothing has to be
// reconstructed from a layout.
type vcpkg struct{}

func (vcpkg) Manager() string { return "vcpkg" }

const maxSPDXBytes = 8 << 20

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

	for _, root := range roots {
		matches, err := filepath.Glob(filepath.Join(root, "share", "*", "vcpkg.spdx.json"))
		if err != nil {
			continue
		}
		sort.Strings(matches)
		for _, path := range matches {
			found, ok := a.readPackage(path, root)
			if !ok || seen[found.Name] {
				continue
			}
			seen[found.Name] = true
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

// installRoots finds the per-triplet install trees. vcpkg puts them under
// vcpkg_installed in manifest mode and under an --x-install-root otherwise, so
// both shapes are searched, one directory deep.
func (vcpkg) installRoots(buildDir string) []string {
	roots := make([]string, 0)
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
				roots = append(roots, filepath.Join(base, entry.Name()))
			}
		}
	}
	sort.Strings(roots)
	return roots
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
		// The share directory is what belongs to this package and holds its
		// copyright file; headers and libraries are merged into the triplet
		// tree and cannot be attributed to one package from the layout alone.
		Root: filepath.Join(root, "share", entry.Name),
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
	if copyright := filepath.Join(found.Root, "copyright"); fileExists(copyright) {
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
