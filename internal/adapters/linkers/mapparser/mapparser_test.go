package mapparser

import (
	"strings"
	"testing"
)

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
	if result.Err == nil {
		t.Fatal("Parse() error = nil, want malformed evidence")
	}
	if len(result.Records) != 0 {
		t.Fatalf("Parse() records = %#v, want none", result.Records)
	}
}
