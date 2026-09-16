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
