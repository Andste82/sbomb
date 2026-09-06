package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/config"
)

// Section 18 lists the CMake install manifest as a manifest kind of its own.
func TestInstallManifestIsReadAsAPackage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "install_manifest.txt")
	content := "/opt/app/bin/app\n/opt/app/share/config.yaml\n\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, ok := readInstallManifest(path)
	if !ok {
		t.Fatal("the install manifest was not read")
	}
	if len(parsed.Outputs) != 1 || parsed.Outputs[0].Kind != "package" {
		t.Fatalf("outputs = %#v", parsed.Outputs)
	}
	if len(parsed.Outputs[0].Inputs) != 2 {
		t.Errorf("inputs = %#v, want the two listed files and not the blank line", parsed.Outputs[0].Inputs)
	}
}

func TestAMissingInstallManifestIsNotAnError(t *testing.T) {
	if _, ok := readInstallManifest(filepath.Join(t.TempDir(), "nothing.txt")); ok {
		t.Error("a manifest was invented from a missing file")
	}
}

// Appendix E: relative paths resolve against the project root, absolute paths
// stand as they are.
func TestManifestPathsResolveAgainstTheProjectRoot(t *testing.T) {
	cfg := config.Config{Project: config.Project{Root: "/src"}}
	if got := resolveManifestPath(cfg, "/build", "assets/index.html"); got != filepath.Join("/src", "assets/index.html") {
		t.Errorf("relative path resolved to %q", got)
	}
	if got := resolveManifestPath(cfg, "/build", "/build/generated/config.bin"); got != "/build/generated/config.bin" {
		t.Errorf("absolute path was rewritten to %q", got)
	}
	// Without a project root the path stays relative, so identity is computed
	// against the logical build root rather than the directory being read.
	bare := config.Config{}
	if got := resolveManifestPath(bare, "/somewhere/build", "generated/config.bin"); got != "generated/config.bin" {
		t.Errorf("path with no project root resolved to %q", got)
	}
}
