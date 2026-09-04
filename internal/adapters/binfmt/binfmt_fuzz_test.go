package binfmt

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzInspect(f *testing.F) {
	f.Add([]byte{0x7f, 'E', 'L', 'F'})
	f.Add([]byte{'M', 'Z'})
	f.Add([]byte("not a binary"))
	f.Fuzz(func(t *testing.T, input []byte) {
		path := filepath.Join(t.TempDir(), "artifact.bin")
		if err := os.WriteFile(path, input, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = Inspect(path, Options{})
	})
}
