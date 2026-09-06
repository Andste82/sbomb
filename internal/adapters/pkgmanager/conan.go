package pkgmanager

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// conan reads what the CMakeDeps generator of Conan 2 wrote into the build
// directory (section 21). Two files per dependency carry everything needed:
// the config-version file states the version, and the per-configuration data
// file states the package folder, which is the component root and holds the
// licence Conan copied out of the recipe.
type conan struct{}

func (conan) Manager() string { return "conan" }

const maxConanFileBytes = 1 << 20

var (
	conanVersionPattern = regexp.MustCompile(`(?m)^\s*set\(PACKAGE_VERSION\s+"?([^"\s)]+)"?\s*\)`)
	// set(tinycbor_PACKAGE_FOLDER_RELEASE "/path/to/package")
	conanFolderPattern = regexp.MustCompile(`(?m)^\s*set\(\w+_PACKAGE_FOLDER_[A-Z]+\s+"([^"]+)"\s*\)`)
)

func (a conan) Discover(options Options) ([]Package, []domain.Finding) {
	matches, err := filepath.Glob(filepath.Join(options.BuildDir, "*-config-version.cmake"))
	if err != nil || len(matches) == 0 {
		return nil, nil
	}
	sort.Strings(matches)

	packages := make([]Package, 0, len(matches))
	findings := make([]domain.Finding, 0)
	for _, path := range matches {
		name := strings.TrimSuffix(filepath.Base(path), "-config-version.cmake")
		if name == "" {
			continue
		}
		found := Package{
			Name:      name,
			Manager:   a.Manager(),
			AnchorKey: "pkg:conan/" + name,
		}
		if version, ok := firstSubmatch(path, conanVersionPattern); ok {
			found.Version = version
			found.VersionSource = "conan"
			// Section 20.3: a package manager states an exact declared version.
			found.VersionConfidence = domain.ConfidenceHigh
		}
		found.Root = a.packageFolder(options.BuildDir, name)
		if found.Root == "" {
			// Without a package folder the files of this dependency cannot be
			// mapped to it, so the entry would name a component owning nothing.
			findings = append(findings, domain.Finding{
				ID: "MISSING_PACKAGE_EVIDENCE", Severity: domain.SeverityWarning,
				Subject: domain.Subject{Kind: "component", Ref: name},
				Message: "Conan generated a config file for this dependency but no package folder could be read from it",
			})
			continue
		}
		found.LicenseFile = a.licenseFile(found.Root)
		found.PURL = "pkg:conan/" + name
		if found.Version != "" {
			found.PURL += "@" + found.Version
		}
		if found.Version == "" {
			findings = append(findings, domain.Finding{
				ID: "UNKNOWN_VERSION", Severity: domain.SeverityWarning,
				Subject: domain.Subject{Kind: "component", Ref: name},
				Message: "the Conan config file states no version for this dependency",
			})
		}
		packages = append(packages, found)
	}
	return packages, findings
}

// packageFolder reads the installed package root out of the per-configuration
// data file. Conan writes one per build type, and any of them names the same
// root for the configuration that was installed.
func (conan) packageFolder(buildDir, name string) string {
	matches, err := filepath.Glob(filepath.Join(buildDir, name+"-*-data.cmake"))
	if err != nil {
		return ""
	}
	sort.Strings(matches)
	for _, path := range matches {
		if folder, ok := firstSubmatch(path, conanFolderPattern); ok {
			return folder
		}
	}
	return ""
}

// licenseFile finds the licence Conan copied into the package. Section 22.2
// ranks package-manager metadata above a licence file found by walking a
// component root, and this one is unambiguous: the manager put it there.
func (conan) licenseFile(root string) string {
	entries, err := os.ReadDir(filepath.Join(root, "licenses"))
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return filepath.Join(root, "licenses", names[0])
}

// firstSubmatch reads a bounded file and returns the first capture of a
// pattern (section 30).
func firstSubmatch(path string, pattern *regexp.Regexp) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxConanFileBytes {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	if match := pattern.FindSubmatch(data); match != nil {
		return string(match[1]), true
	}
	return "", false
}
