package testutil

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

type FixtureManifest struct {
	Toolchain  string `json:"toolchain"`
	Project    string `json:"project"`
	Host       string `json:"host"`
	SourceRoot string `json:"sourceRoot"`
	BuildRoot  string `json:"buildRoot"`
	Generated  string `json:"generatedAt,omitempty"`
}

func LoadFixtureManifest(path string) (FixtureManifest, error) {
	var m FixtureManifest
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, nil
}

func FixtureDir(toolchain, project string) string {
	return filepath.Join("testdata", "fixtures", toolchain, project)
}

// CorpusBuildDir copies a fixture's build directory into a temporary directory
// and returns the copy. The committed corpus is a golden artifact: commands
// under test write evidence dumps and SBOMs into their build directory, so
// they must never be pointed at the corpus itself.
func CorpusBuildDir(t *testing.T, toolchain, project string) string {
	t.Helper()
	source := filepath.Join(RepoRoot(t), "testdata", "fixtures", toolchain, project, "build")
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("fixture %s/%s is missing: %v", toolchain, project, err)
	}
	destination := t.TempDir()
	if err := copyTree(source, destination); err != nil {
		t.Fatalf("copying fixture %s/%s: %v", toolchain, project, err)
	}
	return destination
}

// RepoRoot locates the repository root from any test's working directory by
// walking up to the directory holding go.mod.
func RepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root (go.mod) not found")
		}
		dir = parent
	}
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
