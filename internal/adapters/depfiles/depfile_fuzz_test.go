package depfiles

import "testing"

func FuzzParse(f *testing.F) {
	f.Add("main.o: main.c header.h\n")
	f.Add("build/main.o: src/main.c include/a\\ b.h \\\n+ include/c.h\n")
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = Parse(input)
	})
}
