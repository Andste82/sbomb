package govendor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/limits"
)

func writeModulesTxt(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	vendor := filepath.Join(root, "vendor")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendor, "modules.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestParseReadsModulesVersionsAndPackages(t *testing.T) {
	root := writeModulesTxt(t, `# github.com/google/uuid v1.6.0
## explicit
github.com/google/uuid
# golang.org/x/text v0.14.0
## explicit; go 1.18
golang.org/x/text/language
golang.org/x/text/message
`)
	modules, err := Parse(root, limits.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 2 {
		t.Fatalf("got %d modules, want 2", len(modules))
	}
	if modules[0].Path != "github.com/google/uuid" || modules[0].Version != "v1.6.0" {
		t.Errorf("first module = %s@%s", modules[0].Path, modules[0].Version)
	}
	if !modules[0].Explicit {
		t.Error("the explicit annotation was lost")
	}
	// "## explicit; go 1.18" carries two annotations on one line; the language
	// version is not one an SBOM cares about, but it must not hide the other.
	if !modules[1].Explicit {
		t.Error("explicit was missed when it shared a line with the go directive")
	}
	if len(modules[1].Packages) != 2 {
		t.Errorf("golang.org/x/text packages = %v, want two", modules[1].Packages)
	}
}

func TestParseKeepsBothSidesOfAReplaceDirective(t *testing.T) {
	root := writeModulesTxt(t, `# example.com/original v1.0.0 => example.com/fork v1.2.3
## explicit
example.com/original
# example.com/local v2.0.0 => ../local
example.com/local
`)
	modules, err := Parse(root, limits.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if modules[0].ReplacementPath != "example.com/fork" || modules[0].ReplacementVersion != "v1.2.3" {
		t.Errorf("replacement = %s %s", modules[0].ReplacementPath, modules[0].ReplacementVersion)
	}
	// A replacement by filesystem path carries no version. Reading the next
	// field as one would invent a version that nothing states.
	if modules[1].ReplacementPath != "../local" || modules[1].ReplacementVersion != "" {
		t.Errorf("local replacement = %q version %q", modules[1].ReplacementPath, modules[1].ReplacementVersion)
	}
}

// A module with no vendor directory is the normal case for a build that uses
// the module cache. It is not an error, and it must not be reported as one.
func TestParseTreatsAMissingVendorDirectoryAsNoEvidence(t *testing.T) {
	modules, err := Parse(t.TempDir(), limits.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if modules != nil {
		t.Errorf("got %v, want nil", modules)
	}
}

func TestDirLocatesTheVendoredSources(t *testing.T) {
	module := Module{Path: "github.com/google/uuid"}
	want := filepath.Join("root", "vendor", "github.com", "google", "uuid")
	if got := module.Dir(filepath.Join("root", "vendor")); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}
