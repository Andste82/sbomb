package generate

import (
	"testing"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/config"
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
