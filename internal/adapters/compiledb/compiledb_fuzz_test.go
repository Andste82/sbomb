package compiledb

import "testing"

func FuzzParse(f *testing.F) {
	f.Add([]byte(`[{"directory":"/build","file":"main.c","arguments":["cc","-c","main.c","-o","main.o"]}]`))
	f.Add([]byte(`[{"directory":"/build","file":"main.c","command":"cc -c main.c -o main.o"}]`))
	f.Fuzz(func(t *testing.T, input []byte) {
		_, _ = Parse(input)
	})
}
