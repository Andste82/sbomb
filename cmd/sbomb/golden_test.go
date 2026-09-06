package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateGolden rewrites golden files instead of comparing against them.
// Run `go test ./cmd/sbomb -update` after a deliberate change to generated
// output, then review the resulting diff like any other change.
var updateGolden = flag.Bool("update", false, "rewrite golden files instead of comparing")

// goldenPath resolves a golden file name relative to the repository root,
// independent of the test's working directory.
func goldenPath(t *testing.T, name string) string {
	t.Helper()
	for _, prefix := range []string{filepath.Join("..", ".."), "."} {
		candidate := filepath.Join(prefix, "testdata", "golden", name)
		if _, err := os.Stat(filepath.Dir(candidate)); err == nil {
			return candidate
		}
	}
	t.Fatalf("golden directory not found for %s", name)
	return ""
}

// assertGolden compares actual against the named golden file, or rewrites the
// golden when -update is set.
func assertGolden(t *testing.T, name string, actual []byte) {
	t.Helper()
	path := goldenPath(t, name)
	if *updateGolden {
		if err := os.WriteFile(path, actual, 0o644); err != nil {
			t.Fatalf("updating golden %s: %v", name, err)
		}
		return
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden %s: %v (run with -update to create it)", name, err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("output differs from golden %s\n--- got ---\n%s", name, actual)
	}
}
