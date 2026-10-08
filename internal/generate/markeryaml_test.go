package generate

import (
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// The licence text the retention of an upstream licence file is read from.
// Shared by the wrapper cases below, which all need a real MIT text rather
// than a token: the detector answers on the text, not on the file name.
const mitLicenceText = "MIT License\n\nCopyright (c) 2020 Dave Gamble\n\nPermission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the \"Software\"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:\n\nThe above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.\n\nTHE SOFTWARE IS PROVIDED \"AS IS\", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.\n"

// A dependency whose only marker is the sbom.yml it ships. The file says the
// directory is a separate piece of software just as the JSON forms do, and
// without it in the marker list the reader that would have described the
// component never gets in front of it.
func TestAnSbomYamlBoundsAComponentAndIsThenRead(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dep", "lwip", "src", "tcp.c")
	write(t, source, "int tcp(void){return 0;}\n")
	write(t, filepath.Join(root, "dep", "lwip", "sbom.yml"),
		"name: lwip\nversion: 2.1.3\nsupplier: 'Organization: lwIP Project'\n")

	file := domain.UsedFile{ID: fileID("project", "dep/lwip/src/tcp.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, limits.Config{}, nil)
	component := &domain.Component{ID: "component:lwip", Name: "lwip", DistributionRole: domain.RoleDistributed}
	findings := resolver.enrichComponent(component, []domain.UsedFile{file})

	if hasFindingID(findings, "COMPONENT_ROOT_UNRESOLVED") {
		t.Errorf("the root fell back to the used files although a marker names it: %v", findingIDs(findings))
	}
	if component.Version != "2.1.3" {
		t.Errorf("version = %q, want 2.1.3 out of the manifest the marker names", component.Version)
	}
	if component.VersionSource != "bundled-sbom" {
		t.Errorf("version source = %q, want bundled-sbom", component.VersionSource)
	}
	if component.Supplier != "lwIP Project" {
		t.Errorf("supplier = %q, want the SPDX role prefix stripped", component.Supplier)
	}
}

func TestAnSbomYamlWithComponentRootRedirectsRootAndRetainsLicense(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dep", "cjson", "cJSON", "cJSON.c")
	write(t, source, "int cjson(void){return 0;}\n")
	mitText := mitLicenceText
	write(t, filepath.Join(root, "dep", "cjson", "cJSON", "LICENSE"), mitText)
	write(t, filepath.Join(root, "dep", "cjson", "sbom.yml"),
		"name: cjson\ncomponent-root: ./cJSON\nsupplier: 'Organization: DaveGamble'\n")

	file := domain.UsedFile{ID: fileID("project", "dep/cjson/cJSON/cJSON.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, limits.Config{}, nil)
	component := &domain.Component{ID: "component:cjson", Name: "cjson", DistributionRole: domain.RoleDistributed}
	findings := resolver.enrichComponent(component, []domain.UsedFile{file})

	if hasFindingID(findings, "COMPONENT_ROOT_UNRESOLVED") {
		t.Errorf("the root fell back to the used files although a marker names it: %v", findingIDs(findings))
	}
	if component.Root == nil || component.Root.Canonical() != "project:dep/cjson/cJSON" {
		t.Errorf("component.Root = %v, want project:dep/cjson/cJSON", component.Root)
	}
	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "MIT" {
		t.Errorf("license = %v, want MIT", component.Licenses)
	}
	if len(component.LicenseArtifacts) != 1 {
		t.Errorf("license artifacts = %v, want 1 retained license file", component.LicenseArtifacts)
	}
	if component.Supplier != "DaveGamble" {
		t.Errorf("supplier = %q, want DaveGamble", component.Supplier)
	}
}

// The same wrapper, but described the way a run describes it: the component is
// named by whatever bounded it, not by hand. The upstream brings its own
// licence file, so that marker settles the root and the name comes from the
// subdirectory it sits in -- which is spelt differently from the name the
// upstream gives itself in sbom.yml. The metadata still has to arrive: the
// component-root the wrapper states is what settles that the file describes
// this component, and a name comparison would refuse it over the spelling.
func TestAnSbomYamlDescribesAComponentABoundaryMarkerNamedDifferently(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dep", "cjson", "cJSON", "cJSON.c")
	write(t, source, "int cjson(void){return 0;}\n")
	write(t, filepath.Join(root, "dep", "cjson", "cJSON", "LICENSE"), mitLicenceText)
	write(t, filepath.Join(root, "dep", "cjson", "CMakeLists.txt"), "add_subdirectory(cJSON)\n")
	write(t, filepath.Join(root, "dep", "cjson", "sbom.yml"),
		"name: cjson\ncomponent-root: ./cJSON\nversion: 1.7.18\nsupplier: 'Organization: DaveGamble'\n")

	file := domain.UsedFile{ID: fileID("project", "dep/cjson/cJSON/cJSON.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, limits.Config{}, nil)

	// The name the run would carry, rather than one the test chose: the
	// boundary marker is the licence file in cJSON/, so the component is named
	// after that directory and never after the wrapper.
	id, name, componentType, _, _ := resolver.resolve(file)
	if name != "cJSON" {
		t.Fatalf("name = %q, want cJSON out of the directory the licence file bounded", name)
	}
	component := &domain.Component{ID: id, Name: name, Type: componentType, DistributionRole: domain.RoleDistributed}
	resolver.enrichComponent(component, []domain.UsedFile{file})

	if component.Supplier != "DaveGamble" {
		t.Errorf("supplier = %q, want DaveGamble: the component-root settles that sbom.yml describes this component", component.Supplier)
	}
	if component.Version != "1.7.18" {
		t.Errorf("version = %q, want 1.7.18 out of the sbom.yml above the root", component.Version)
	}
	if component.VersionSource != "bundled-sbom" {
		t.Errorf("version source = %q, want bundled-sbom", component.VersionSource)
	}
}

// component-root takes any relative path inside the component, so the
// directory holding the sbom.yml can be more than one level above the root it
// names. Looking only at the immediate parent would leave the metadata unread.
func TestAnSbomYamlIsFoundSeveralLevelsAboveTheRootItNames(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dep", "cjson", "vendor", "cJSON", "cJSON.c")
	write(t, source, "int cjson(void){return 0;}\n")
	write(t, filepath.Join(root, "dep", "cjson", "vendor", "cJSON", "LICENSE"), mitLicenceText)
	write(t, filepath.Join(root, "dep", "cjson", "sbom.yml"),
		"name: cjson\ncomponent-root: ./vendor/cJSON\nsupplier: 'Organization: DaveGamble'\n")

	file := domain.UsedFile{ID: fileID("project", "dep/cjson/vendor/cJSON/cJSON.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, limits.Config{}, nil)
	component := &domain.Component{ID: "component:cJSON", Name: "cJSON", DistributionRole: domain.RoleDistributed}
	resolver.enrichComponent(component, []domain.UsedFile{file})

	if component.Root == nil || component.Root.Canonical() != "project:dep/cjson/vendor/cJSON" {
		t.Errorf("component.Root = %v, want project:dep/cjson/vendor/cJSON", component.Root)
	}
	if component.Supplier != "DaveGamble" {
		t.Errorf("supplier = %q, want DaveGamble out of the sbom.yml two levels up", component.Supplier)
	}
}

// An sbom.yml above a component it says nothing about stays out of it. The
// walk is answered by the file: a component-root that resolves elsewhere is a
// statement about another component, and a bundled SBOM with none at all
// describes only the directory it lies in.
func TestAnSbomYamlThatNamesAnotherRootDoesNotDescribeThisComponent(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dep", "wrapper", "cJSON", "cJSON.c")
	write(t, source, "int cjson(void){return 0;}\n")
	write(t, filepath.Join(root, "dep", "wrapper", "cJSON", "LICENSE"), mitLicenceText)
	write(t, filepath.Join(root, "dep", "wrapper", "other"), "")
	write(t, filepath.Join(root, "dep", "wrapper", "sbom.yml"),
		"name: wrapper\ncomponent-root: ./other\nsupplier: 'Organization: Somebody Else'\n")

	file := domain.UsedFile{ID: fileID("project", "dep/wrapper/cJSON/cJSON.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, limits.Config{}, nil)
	component := &domain.Component{ID: "component:cJSON", Name: "cJSON", DistributionRole: domain.RoleDistributed}
	resolver.enrichComponent(component, []domain.UsedFile{file})

	if component.Supplier != "" {
		t.Errorf("supplier = %q, want none: the sbom.yml above names a different root", component.Supplier)
	}
}

// cve-exclude-list travels with the rest of what the wrapper's sbom.yml
// states. It carries no claim of its own, so a fold that only looks at the
// named fields would drop it.
func TestAnSbomYamlAboveTheRootAlsoCarriesItsCVEExclusions(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dep", "cjson", "cJSON", "cJSON.c")
	write(t, source, "int cjson(void){return 0;}\n")
	write(t, filepath.Join(root, "dep", "cjson", "cJSON", "LICENSE"), mitLicenceText)
	write(t, filepath.Join(root, "dep", "cjson", "sbom.yml"),
		"name: cjson\ncomponent-root: ./cJSON\nversion: 1.7.18\n"+
			"cve-exclude-list:\n  - cve: CVE-2023-0001\n    reason: the parser this affects is not built\n")

	file := domain.UsedFile{ID: fileID("project", "dep/cjson/cJSON/cJSON.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, limits.Config{}, nil)
	component := &domain.Component{ID: "component:cJSON", Name: "cJSON", DistributionRole: domain.RoleDistributed}
	resolver.enrichComponent(component, []domain.UsedFile{file})

	if len(component.CVEExclusions) != 1 {
		t.Fatalf("exclusions = %v, want the one the wrapper's sbom.yml states", component.CVEExclusions)
	}
	if component.CVEExclusions[0].CVE != "CVE-2023-0001" {
		t.Errorf("cve = %q, want CVE-2023-0001", component.CVEExclusions[0].CVE)
	}
	if component.CVEExclusions[0].Reason == "" {
		t.Errorf("reason is empty, want the one beside the cve")
	}
}
