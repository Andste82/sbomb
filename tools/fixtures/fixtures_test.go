package fixtures_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/tools/fixtures"
)

func TestFixtureCorpusPresent(t *testing.T) {
	for _, pair := range fixtures.AllPairs() {
		toolchain := pair[0]
		project := pair[1]
		dir := filepath.Join("..", "..", "testdata", "fixtures", toolchain, project)
		if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
			t.Fatalf("fixture missing manifest.json for %s/%s: %v", toolchain, project, err)
		}
	}
}

func TestFixturesContainNoHostPaths(t *testing.T) {
	if err := fixtures.CheckNoHostPaths(filepath.Join("..", "..", "testdata", "fixtures")); err != nil {
		t.Fatalf("fixture corpus contains host-specific paths: %v", err)
	}
}

func TestFixtureProvenancePresent(t *testing.T) {
	for _, pair := range fixtures.AllPairs() {
		dir := filepath.Join("..", "..", "testdata", "fixtures", pair[0], pair[1])
		if err := fixtures.CheckFixtureProvenance(dir); err != nil {
			t.Fatalf("fixture provenance missing or invalid for %s/%s: %v", pair[0], pair[1], err)
		}
	}
}

func TestNoThirdPartySourceCommitted(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "fixtures")
	var foundBadDir bool
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "include" || name == "src" || strings.HasSuffix(name, "-src") {
				foundBadDir = true
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 256*1024 {
			t.Fatalf("fixture file too large: %s (%d bytes)", path, info.Size())
		}
		return nil
	}); err != nil {
		t.Fatalf("walking fixture tree: %v", err)
	}
	if foundBadDir {
		t.Fatal("fixture tree contains source directories that are disallowed")
	}
}

func TestFixgenIdempotent(t *testing.T) {
	tempDir := t.TempDir()
	fixtureFile := filepath.Join(tempDir, "sample.txt")
	content := "workspace=/workspaces/sbomb/build/debug\nartifact=/workspaces/sbomb/build/hello\n"
	if err := os.WriteFile(fixtureFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fixtures.RewriteFixtureTree(tempDir); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixtures.RewriteFixtureTree(tempDir); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("fixture rewrite drifted across runs")
	}
	if strings.Contains(string(second), "/workspaces/sbomb") {
		t.Fatal("sentinel rewriting failed to remove host path")
	}
}
