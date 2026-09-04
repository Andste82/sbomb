package ninja

import (
	"strings"
	"testing"
)

func FuzzParseFile(f *testing.F) {
	f.Add("cc = cc\nbuild app: link main.o\n")
	f.Add("rule cc\n  command = cc -c $in -o $out\nbuild main.o: cc main.c\n")
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ParseFile(strings.NewReader(input))
	})
}

func FuzzParseDeps(f *testing.F) {
	f.Add(append([]byte(depsSignature), []byte{4, 0, 0, 0}...))
	f.Add([]byte("# ninjadeps\n"))
	f.Fuzz(func(t *testing.T, input []byte) {
		_, _ = ParseDeps(input)
	})
}
