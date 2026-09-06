package binfmt

import "testing"

// An artifact is the least trustworthy input of all: it is a binary from a
// build this tool did not run, and phase 6 made its DWARF line table an
// evidence source. Inspect must return findings rather than panic on anything.
func FuzzInspectBytes(f *testing.F) {
	f.Add([]byte{0x7f, 'E', 'L', 'F'})
	f.Add([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0})
	f.Add([]byte("MZ"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		path := writeTemp(t, data)
		result, err := Inspect(path, Options{IncludeRuntimeLibraries: true})
		if err != nil {
			return
		}
		for _, unit := range result.CompilationUnits {
			// A unit that carried no line program cannot have contributed
			// headers; the two must not disagree.
			if !unit.LineTable && len(unit.Headers) != 0 {
				t.Fatalf("%d header(s) from a unit with no line table", len(unit.Headers))
			}
		}
	})
}
