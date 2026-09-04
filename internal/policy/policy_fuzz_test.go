package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzLoadWaivers(f *testing.F) {
	f.Add([]byte(`[{"id":"UNKNOWN_LICENSE","subject":"component:demo","reason":"accepted"}]`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, input []byte) {
		path := filepath.Join(t.TempDir(), "waivers.json")
		if err := os.WriteFile(path, input, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = LoadWaivers(path)
	})
}
