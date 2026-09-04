package mapparser

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkParse(b *testing.B) {
	var input strings.Builder
	for index := 0; index < 1000; index++ {
		fmt.Fprintf(&input, "build/obj/file%d.o\n", index)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := Parse(strings.NewReader(input.String()), "gnu-ld")
		if result.Err != nil {
			b.Fatal(result.Err)
		}
	}
}

func TestSniff(t *testing.T) {
	tests := map[string]string{
		"gnu-ld":   "Archive member included\n",
		"gnu-gold": "Archive member included to satisfy reference by file\n",
		"lld":      "VMA LMA Size Align Out In Symbol\n",
		"msvc":     " Preferred load address is 00010000\n",
		"iar":      "IAR ELF Linker\n",
	}
	for want, text := range tests {
		if got := Sniff(text); got != want {
			t.Errorf("Sniff(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestParseRecordsAndDiscardedSections(t *testing.T) {
	text := "Archive member included\nlib/libfoo.a(foo.o)\nmain.o\nDiscarded input sections\n .text.dead 0x0 0x4 build/dead.o\n"
	result := Parse(strings.NewReader(text), "gnu-ld")
	if result.Err != nil {
		t.Fatalf("Parse() error = %v", result.Err)
	}
	if len(result.Records) < 3 {
		t.Fatalf("Parse() records = %#v", result.Records)
	}
	var found bool
	for _, record := range result.Records {
		if record.Kind == DiscardedSection && record.Path == "build/dead.o" {
			found = true
		}
	}
	if !found {
		t.Fatalf("discarded object missing from %#v", result.Records)
	}
}

func TestParseTruncatedLine(t *testing.T) {
	result := Parse(strings.NewReader(strings.Repeat("x", MaxLineLength+1)), "gnu-ld")
	if !errors.Is(result.Err, ErrInputLimitExceeded) {
		t.Fatalf("Parse() error = %v, want input limit exceeded", result.Err)
	}
	if len(result.Records) != 0 {
		t.Fatalf("Parse() records = %#v, want none", result.Records)
	}
}
