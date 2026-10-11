package generate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/policy"
)

// TestOnlyAnUnmappedPrebuiltLibraryIsReported links two prebuilt libraries
// from the source tree, one that a components[] entry claims and one that
// nothing claims. PREBUILT_LIBRARY_UNMAPPED is gated by
// prebuiltLibrariesRequireMapping, which is on in the default profile, so a
// finding raised for the mapped library would fail a run whose remediation has
// already been done.
func TestOnlyAnUnmappedPrebuiltLibraryIsReported(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "build")
	mapped := filepath.Join(sourceRoot, "third_party", "libfoo", "lib", "libfoo.a")
	unmapped := filepath.Join(sourceRoot, "third_party", "libbar", "libbar.a")
	for _, path := range []string{mapped, unmapped, filepath.Join(buildDir, "app")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("!<arch>\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(buildDir, "compile_commands.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	depfile := "app: " + filepath.ToSlash(mapped) + " " + filepath.ToSlash(unmapped) + "\n"
	if err := os.WriteFile(filepath.Join(buildDir, "app.d"), []byte(depfile), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Project:   config.Project{Name: "firmware", Root: sourceRoot},
		Artifacts: []config.Artifact{{Path: "app", LinkDepfile: filepath.Join(buildDir, "app.d")}},
		Components: []config.Component{{
			Path: "third_party/libfoo", Name: "libfoo", Version: "1.0.0", Supplier: "Foo Inc.",
		}},
	}
	options := Options{PathFlavor: pathmodel.PosixFlavor{}, Policy: policy.DefaultConfig()}
	result, err := RunWithOptions(cfg, buildDir, true, options)
	if err != nil {
		t.Fatal(err)
	}

	var reported []domain.Finding
	for _, finding := range result.Findings {
		if finding.ID == "PREBUILT_LIBRARY_UNMAPPED" {
			reported = append(reported, finding)
		}
	}
	if len(reported) != 1 || reported[0].Subject.Ref != "project:third_party/libbar/libbar.a" {
		t.Fatalf("PREBUILT_LIBRARY_UNMAPPED = %+v, want exactly one, for the unmapped library", reported)
	}

	// The unmapped library alone fails the default profile.
	verdict := policy.Evaluate(reported, policy.DefaultConfig(), nil, time.Time{})
	if !verdict.Fail || verdict.ExitCode != 3 {
		t.Fatalf("verdict = %+v, want the default profile to fail with exit 3", verdict)
	}

	// Once the second library is claimed too, nothing is reported.
	cfg.Components = append(cfg.Components, config.Component{
		Path: "third_party/libbar", Name: "libbar", Version: "2.0.0", Supplier: "Bar Ltd.",
	})
	result, err = RunWithOptions(cfg, buildDir, true, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range result.Findings {
		if finding.ID == "PREBUILT_LIBRARY_UNMAPPED" {
			t.Fatalf("a mapped library is still reported: %+v", finding)
		}
	}
}
