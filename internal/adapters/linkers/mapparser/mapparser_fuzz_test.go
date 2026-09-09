package mapparser

import (
	"strings"
	"testing"
)

func FuzzParse(f *testing.F) {
	seeds := []struct {
		format string
		text   string
	}{
		{"gnu-ld", "Archive member included\nlib/libfoo.a(foo.o)\n"},
		{"gnu-gold", "Archive member included to satisfy reference by file\nlib/libfoo.a(foo.o)\n"},
		{"lld", "VMA LMA Size Align Out In Symbol\nmain.o\n"},
		{"msvc", "Preferred load address is 00010000\n\n Start         Length     Name                   Class\n 0001:00000000 00000010H .text                  CODE\n\n Address         Publics by Value              Rva+Base               Lib:Object\n 0001:00000000 main 0000000140001000 f   main.c.obj\n\n Static symbols\n 0001:00000000 helper 0000000140001000     crypto:crypto.c.obj\n"},
	}
	for _, seed := range seeds {
		f.Add(seed.format, seed.text)
	}
	f.Fuzz(func(t *testing.T, format, input string) {
		result := Parse(strings.NewReader(input), format)
		if result.Err != nil && result.Records == nil {
			return
		}
	})
}
