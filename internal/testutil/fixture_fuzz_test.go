package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzLoadFixtureManifest(f *testing.F) {
	f.Add([]byte(`{"toolchain":"gcc-12","project":"hello","host":"linux"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		path := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(path, input, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = LoadFixtureManifest(path)
	})
}
