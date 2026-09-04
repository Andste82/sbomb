package depfiles

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseRejectsOversizedLine(t *testing.T) {
	_, err := Parse(strings.Repeat("x", MaxLineLength+1))
	if !errors.Is(err, ErrInputLimitExceeded) {
		t.Fatalf("Parse() error = %v, want input limit exceeded", err)
	}
}

func BenchmarkParse(b *testing.B) {
	input := "main.o: main.c include/config.h include/platform.h\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(input); err != nil {
			b.Fatal(err)
		}
	}
}

func TestParseDepfileHandlesEscapesAndContinuations(t *testing.T) {
	text := "app.elf: src/main.cpp \\\n  src/with\\ space.h \\#note.h libfoo.a(bar.o)\n\\\n" +
		"# comment\n" +
		"other.elf: build/obj.o C$:/src/pkg/lib.a\n"

	rules, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []Rule{
		{Targets: []string{"app.elf"}, Prereqs: []string{"src/main.cpp", "src/with space.h", "#note.h", "libfoo.a(bar.o)"}},
		{Targets: []string{"other.elf"}, Prereqs: []string{"build/obj.o", "C:/src/pkg/lib.a"}},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("Parse() = %#v, want %#v", rules, want)
	}
}

func TestParseDepfileDeduplicatesAndIgnoresEmptyPrereqs(t *testing.T) {
	text := "main: a.h a.h\n\nsecond: \n"
	rules, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2", len(rules))
	}
	if !reflect.DeepEqual(rules[0].Prereqs, []string{"a.h"}) {
		t.Fatalf("rules[0].Prereqs = %#v, want %#v", rules[0].Prereqs, []string{"a.h"})
	}
	if len(rules[1].Prereqs) != 0 {
		t.Fatalf("rules[1].Prereqs = %#v, want empty", rules[1].Prereqs)
	}
}
