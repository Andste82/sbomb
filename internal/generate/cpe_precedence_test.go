package generate

import (
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// An upstream sbom.yml states a cpe about its own product, and it can be wrong
// about it. Curated configuration is how an operator says so, and it settles
// the question: section 20.4 ranks it above the manifest.
func TestACuratedCPEOutranksTheUpstreamsOwn(t *testing.T) {
	const upstream = "cpe:2.3:o:amazon:freertos:{}:*:*:*:*:*:*:*"
	const operator = "cpe:2.3:a:espressif:freertos_esp32:{}:*:*:*:*:*:*:*"

	for _, c := range []struct {
		name    string
		curated string
		want    string
	}{
		{"curated wins", operator, "cpe:2.3:a:espressif:freertos_esp32:10.5.1:*:*:*:*:*:*:*"},
		{"upstream when none is curated", "", "cpe:2.3:o:amazon:freertos:10.5.1:*:*:*:*:*:*:*"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "dep", "freertos", "src", "tasks.c")
			write(t, source, "int task(void){return 0;}\n")
			// A marker makes dep/freertos a component root, which is what
			// puts the reader in front of the manifest beside it.
			write(t, filepath.Join(root, "dep", "freertos", "idf_component.yml"), "version: 10.5.1\n")
			write(t, filepath.Join(root, "dep", "freertos", "sbom.yml"),
				"name: freertos\nversion: 10.5.1\ncpe: "+upstream+"\n")

			cfg := config.Config{Project: config.Project{Name: "firmware"}}
			if c.curated != "" {
				cfg.Components = []config.Component{{Name: "freertos", CPE: c.curated}}
			}

			file := domain.UsedFile{ID: fileID("project", "dep/freertos/src/tasks.c")}
			resolver := newComponentResolver(cfg,
				map[string]string{file.ID.Canonical(): source},
				map[string]string{"project": root}, limits.Config{}, nil)
			component := &domain.Component{ID: "component:freertos", Name: "freertos", DistributionRole: domain.RoleDistributed}
			resolver.enrichComponent(component, []domain.UsedFile{file})

			if component.CPE != c.want {
				t.Errorf("cpe = %q, want %q", component.CPE, c.want)
			}
		})
	}
}

// A cpe whose placeholder cannot be filled is published by neither origin: the
// rule is about the string reaching the document, not about where it came from.
func TestAnUnfillableCPEIsPublishedByNeitherOrigin(t *testing.T) {
	const withPlaceholder = "cpe:2.3:o:amazon:freertos:{}:*:*:*:*:*:*:*"

	for _, c := range []struct {
		name    string
		curated string
	}{
		{"from the upstream", ""},
		{"from the configuration", withPlaceholder},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "dep", "freertos", "src", "tasks.c")
			write(t, source, "int task(void){return 0;}\n")
			// A manifest that names a cpe and no version, which is the shape
			// that made the placeholder reach the document.
			// A marker makes dep/freertos a component root, which is what
			// puts the reader in front of the manifest beside it.
			write(t, filepath.Join(root, "dep", "freertos", "idf_component.yml"), "version: 10.5.1\n")
			write(t, filepath.Join(root, "dep", "freertos", "sbom.yml"),
				"name: freertos\ncpe: "+withPlaceholder+"\n")

			cfg := config.Config{Project: config.Project{Name: "firmware"}}
			if c.curated != "" {
				cfg.Components = []config.Component{{Name: "freertos", CPE: c.curated}}
			}

			file := domain.UsedFile{ID: fileID("project", "dep/freertos/src/tasks.c")}
			resolver := newComponentResolver(cfg,
				map[string]string{file.ID.Canonical(): source},
				map[string]string{"project": root}, limits.Config{}, nil)
			component := &domain.Component{ID: "component:freertos", Name: "freertos", DistributionRole: domain.RoleDistributed}
			resolver.enrichComponent(component, []domain.UsedFile{file})

			if component.CPE != "" {
				t.Errorf("cpe = %q, want none: the version it leaves a place for is unknown", component.CPE)
			}
		})
	}
}
