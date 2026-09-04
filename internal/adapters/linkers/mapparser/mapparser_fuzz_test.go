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
		{"msvc", "Preferred load address is 00010000\nmain.obj\n"},
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
