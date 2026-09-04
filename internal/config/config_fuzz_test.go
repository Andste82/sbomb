package config

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzLoad(f *testing.F) {
	f.Add([]byte(`{"project":{"name":"demo"},"build":{"dir":"build"}}`))
	f.Add([]byte(`{"mode":"assembly","project":{"name":"demo","root":"."},"build":{"dir":"build"}}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		path := filepath.Join(t.TempDir(), "sbomb.json")
		if err := os.WriteFile(path, input, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = Load(path)
	})
}
