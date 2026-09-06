package respfile

import (
	"errors"
	"strings"
	"testing"
)

var errNotFound = errors.New("no such response file")

// The two tokenizers are the newest untrusted-input path in the tool: a
// response file comes from somebody else's build tree, and the quoting rules
// they implement are exactly where a state machine loops or over-allocates.
func FuzzTokenize(f *testing.F) {
	f.Add(`a.o "with space.o" b.o`)
	f.Add(`with\ space.o`)
	f.Add(`"C:\build\obj\main.obj" C:\build\obj\util.obj`)
	f.Add(`a\\"b c"`)
	f.Add(`"unterminated`)
	f.Add(`'`)
	f.Add(strings.Repeat(`\`, 64))
	f.Add("")

	f.Fuzz(func(t *testing.T, content string) {
		for _, quoting := range []Quoting{GNU, MSVC} {
			args := Tokenize(content, quoting)
			// A tokenizer may drop characters -- quotes and escapes are not
			// content -- but it must never invent them.
			var total int
			for _, arg := range args {
				total += len(arg)
			}
			if total > len(content) {
				t.Fatalf("%s produced %d bytes from %d", quoting, total, len(content))
			}
		}
	})
}

// Expansion is recursive and reads files, so its limits are what stop a cycle
// or a bomb.
func FuzzExpand(f *testing.F) {
	f.Add("@a.rsp", "-o app @b.rsp", "x.o y.o")
	f.Add("@a.rsp", "@a.rsp", "")
	f.Add("plain.o", "", "")

	f.Fuzz(func(t *testing.T, arg, a, b string) {
		files := map[string]string{"a.rsp": a, "b.rsp": b}
		_, _ = Expand([]string{arg}, Options{
			MaxBytes: 4096,
			ReadFile: func(path string) ([]byte, error) {
				content, ok := files[path]
				if !ok {
					return nil, errNotFound
				}
				return []byte(content), nil
			},
		})
	})
}
