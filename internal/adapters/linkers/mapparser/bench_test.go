package mapparser

import (
	"os"
	"strings"
	"testing"
)

// Section 31 names map parsing as one of the four paths that must have a
// benchmark. It is the one that meets the largest input: a 200 MB map is
// ordinary for a firmware link, and the parser reads every line of it.
func BenchmarkParseGNULargeMap(b *testing.B) {
	// A memory map of the shape generate-large writes: placement lines that
	// name an object, and the symbol lines a real map is mostly made of.
	var builder strings.Builder
	builder.WriteString("Archive member included to satisfy reference by file (symbol)\n\n")
	builder.WriteString("\nLinker script and memory map\n\n")
	builder.WriteString(".text           0x0000000000001000     0x1000\n")
	for index := 0; index < 20000; index++ {
		builder.WriteString(" .text          0x0000000000001000       0x20 CMakeFiles/large.dir/unit.c.o\n")
		builder.WriteString("                0x0000000000001000                some_symbol_name\n")
	}
	content := builder.String()
	b.SetBytes(int64(len(content)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := Parse(strings.NewReader(content), "gnu-ld")
		if len(result.Records) == 0 {
			b.Fatal("no records parsed")
		}
	}
}

// The real fixture, when it has been generated. This is the number section 31
// is actually about.
func BenchmarkParseGeneratedMap(b *testing.B) {
	path := os.Getenv("SBOMB_LARGE_MAP")
	if path == "" {
		b.Skip("set SBOMB_LARGE_MAP to a generated large.map")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Skip(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Parse(strings.NewReader(string(data)), "gnu-ld")
	}
}
