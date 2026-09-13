package generate

import (
	"testing"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
)

func cmakeModel(name, version string) *cmakeapi.Model {
	return &cmakeapi.Model{Cache: map[string]string{
		"CMAKE_PROJECT_NAME":    name,
		"CMAKE_PROJECT_VERSION": version,
	}}
}

// TestTheBuildSystemSuppliesWhatTheConfigurationDidNot: a CMake project states
// its name and version once, in project(). Requiring them again in sbomb.json
// is two places for one fact, and two places can disagree.
func TestTheBuildSystemSuppliesWhatTheConfigurationDidNot(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		cfg         config.Config
		model       *cmakeapi.Model
		wantName    string
		wantVersion string
		wantSource  string
	}{
		{
			name:        "nothing configured, single mode",
			cfg:         config.Config{Mode: "single"},
			model:       cmakeModel("device", "1.4.2"),
			wantVersion: "1.4.2",
			wantSource:  "cmake",
			// The name is deliberately not taken: section 6.1 makes the
			// deliverable the root of a single-artifact document, and the
			// project is not the deliverable.
			wantName: "",
		},
		{
			name:        "nothing configured, assembly mode",
			cfg:         config.Config{Mode: "assembly"},
			model:       cmakeModel("device", "1.4.2"),
			wantName:    "device",
			wantVersion: "1.4.2",
			wantSource:  "cmake",
		},
		{
			name:        "the configuration wins",
			cfg:         config.Config{Mode: "assembly", Project: config.Project{Name: "curated-name", Version: "9.9.9"}},
			model:       cmakeModel("device", "1.4.2"),
			wantName:    "curated-name",
			wantVersion: "9.9.9",
			wantSource:  "curated",
		},
		{
			name:        "no reply to read",
			cfg:         config.Config{Mode: "assembly", Project: config.Project{Version: "9.9.9"}},
			model:       nil,
			wantVersion: "9.9.9",
			wantSource:  "curated",
		},
		{
			name:  "a project that declared no version",
			cfg:   config.Config{Mode: "assembly"},
			model: cmakeModel("device", ""),
			// A version nobody stated is absent, not invented.
			wantName:   "device",
			wantSource: "",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := testCase.cfg
			source := projectFromCMake(&cfg, testCase.model)
			if cfg.Project.Name != testCase.wantName {
				t.Errorf("name = %q, want %q", cfg.Project.Name, testCase.wantName)
			}
			if cfg.Project.Version != testCase.wantVersion {
				t.Errorf("version = %q, want %q", cfg.Project.Version, testCase.wantVersion)
			}
			if source != testCase.wantSource {
				t.Errorf("version source = %q, want %q", source, testCase.wantSource)
			}
		})
	}
}

// TestTheProductRecordsWhereItsVersionCameFrom: the product's version had
// carried no source at all, so a curated version and a read one looked alike.
func TestTheProductRecordsWhereItsVersionCameFrom(t *testing.T) {
	cfg := config.Config{Project: config.Project{Version: "1.4.2"}}
	product := productComponent(cfg, "cmake", nil)
	if product.VersionSource != "cmake" {
		t.Errorf("VersionSource = %q, want cmake", product.VersionSource)
	}
	if product.VersionConf == "" {
		t.Error("a version with a known source carries no confidence")
	}

	// No source means no claim about one, rather than a default.
	unsourced := productComponent(cfg, "", nil)
	if unsourced.VersionSource != "" {
		t.Errorf("VersionSource = %q, want empty", unsourced.VersionSource)
	}
}

// Section 24.2 is normative about where a toolchain component hangs: not under
// the product but under the synthetic build-environment component, because
// "toolchain files MUST NOT silently appear as ordinary project dependencies".
// Its files are reached from an artifact like every other file, so hanging
// components under the artifacts that reached them puts it straight back on
// the product's dependency path -- product -> artifact -> gnu-13.3.0 -- unless
// the role is asked.
func TestABuildEnvironmentComponentHangsUnderNoArtifact(t *testing.T) {
	artifacts := []domain.Component{{ID: "build:app", Name: "app", Type: "application"}}
	groups := []fileGroup{
		{
			component: domain.Component{ID: "component:lib", Scope: string(anchors.ScopeThirdParty)},
			files: []domain.UsedFile{{
				ID:         domain.FileID{Anchor: "project", RelPath: "lib.c"},
				Properties: map[string][]string{"sbomb:evidence:artifacts": {"artifact:build:app"}},
			}},
		},
		{
			component: domain.Component{ID: "component:gnu", Scope: string(anchors.ScopeToolchain)},
			files: []domain.UsedFile{{
				ID:         domain.FileID{Anchor: "toolchain:gnu", RelPath: "libgcc.a"},
				Properties: map[string][]string{"sbomb:evidence:artifacts": {"artifact:build:app"}},
			}},
		},
	}

	for _, relation := range artifactRelations(artifacts, groups, []string{"component:lib", "component:gnu"}) {
		if relation.From != "build:app" {
			continue
		}
		for _, target := range relation.To {
			if target == "component:gnu" {
				t.Errorf("the toolchain component hangs under the deliverable: %v", relation.To)
			}
		}
	}
}

// Section 6.2 is about assembly mode and says "each artifact", so an assembly
// of one still names its deliverable -- and a single-artifact run that happens
// to configure two artifacts is a configuration sbomb reports rather than a
// shape to impose on the document. The count decided both before.
func TestTheModeDecidesWhetherArtifactsAreComponents(t *testing.T) {
	deliverables := []Deliverable{{EvidencePath: "app", Role: "application"}}
	ids := []domain.NodeID{"artifact:build:app"}

	if got := artifactComponents("assembly", deliverables, ids); len(got) != 1 || got[0].ID != "build:app" {
		t.Errorf("an assembly of one names %#v, want its one deliverable", got)
	}
	two := append(deliverables, Deliverable{EvidencePath: "image.img", Role: "image"})
	twoIDs := append(ids, "artifact:build:image.img")
	if got := artifactComponents("single", two, twoIDs); got != nil {
		t.Errorf("single mode was given the assembly shape: %#v", got)
	}
}

// Two configured artifacts can name one file -- "app" and "./app" -- and they
// are then one deliverable. Emitting the component twice collides on its
// bom-ref and stops the run with an internal invariant, which is a defect
// reported as a crash.
func TestTwoConfiguredPathsToOneFileAreOneArtifact(t *testing.T) {
	deliverables := []Deliverable{
		{EvidencePath: "app", Role: "application"},
		{EvidencePath: "./app", Role: "application"},
	}
	ids := []domain.NodeID{"artifact:build:app", "artifact:build:app"}
	if got := artifactComponents("assembly", deliverables, ids); len(got) != 1 {
		t.Errorf("one file named twice produced %d components, want one", len(got))
	}
}
