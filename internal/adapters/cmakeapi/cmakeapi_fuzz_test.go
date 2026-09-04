package cmakeapi

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzParseReplyDir(f *testing.F) {
	f.Add([]byte(`{"configurations":[]}`))
	f.Add([]byte(`{"entries":[]}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		dir := t.TempDir()
		for _, name := range []string{"codemodel-v2.json", "cache-v2.json", "toolchains-v1.json"} {
			if err := os.WriteFile(filepath.Join(dir, name), input, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = ParseReplyDir(dir)
	})
}
