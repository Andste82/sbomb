package cmakeapi

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseReplyDir feeds arbitrary bytes through the index and the object it
// names, which is the whole untrusted surface of the adapter.
func FuzzParseReplyDir(f *testing.F) {
	f.Add([]byte(`{"objects":[{"kind":"codemodel","jsonFile":"o.json"}]}`), []byte(`{"configurations":[]}`))
	f.Add([]byte(`{"objects":[{"kind":"cache","jsonFile":"o.json"}]}`), []byte(`{"entries":[]}`))
	f.Add([]byte(`{"objects":[{"kind":"toolchains","jsonFile":"o.json"}]}`), []byte(`{"toolchains":[{}]}`))
	f.Add([]byte(`{"objects":[{"kind":"codemodel","jsonFile":"../../../etc/passwd"}]}`), []byte(`{}`))
	f.Fuzz(func(t *testing.T, index, object []byte) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index-fuzz.json"), index, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "o.json"), object, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = ParseReplyDir(dir)
	})
}
