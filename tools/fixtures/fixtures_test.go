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
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Nothing is decided by a directory name: CMake writes a "src"
			// directory of stamp files for every populated dependency, and
			// FetchContent writes a "<name>-src" whose licence file is
			// required evidence for section 22.2. What the rule forbids is
			// source code, and that is checked file by file below.
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 256*1024 {
			t.Fatalf("fixture file too large: %s (%d bytes)", path, info.Size())
		}
		if isSourceExtension(path) && !generatedIntoTheBuildTree(path) {
			t.Errorf("fixture tree contains source code: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walking fixture tree: %v", err)
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

func isSourceExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp", ".hxx", ".s", ".asm":
		return true
	}
	return false
}

// generatedIntoTheBuildTree recognizes the source-shaped files a generator
// wrote into the build tree. Those are evidence -- the unity aggregation file
// and the precompiled-header glue are what sections 17.1 and 14.5 are read
// from -- and are not part of anyone's source tree.
func generatedIntoTheBuildTree(path string) bool {
	slashed := filepath.ToSlash(path)
	base := filepath.Base(slashed)
	return strings.Contains(slashed, "/CMakeFiles/") ||
		strings.HasPrefix(base, "cmake_pch.") ||
		strings.HasPrefix(base, "unity_")
}
