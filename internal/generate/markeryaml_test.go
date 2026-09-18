package generate

import (
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

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
	mitText := "MIT License\n\nCopyright (c) 2020 Dave Gamble\n\nPermission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the \"Software\"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:\n\nThe above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.\n\nTHE SOFTWARE IS PROVIDED \"AS IS\", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.\n"
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
