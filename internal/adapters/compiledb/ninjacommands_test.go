package compiledb

import (
	"strings"
	"testing"
)

// The lines are what `ninja -t commands libx.a` printed for a two-rule build:
// one compilation and one archive step.
const realNinjaCommands = `cc -MD -MF main.o.d -c main.c -o main.o
ar rcs libx.a main.o
`

func TestParseNinjaCommandsReadsTheCompilations(t *testing.T) {
	commands, err := ParseNinjaCommands(strings.NewReader(realNinjaCommands), "/build")
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 {
		t.Fatalf("got %d command(s), want only the compilation: %+v", len(commands), commands)
	}
	command := commands[0]
	if command.File != "/build/main.c" {
		t.Errorf("file = %q", command.File)
	}
	if command.Output != "/build/main.o" {
		t.Errorf("output = %q", command.Output)
	}
	if command.Directory != "/build" {
		t.Errorf("directory = %q; ninja runs its commands in the build directory", command.Directory)
	}
}

// Without a "-c <path>" nothing states which file was compiled, and picking
// the argument that looks most like a source would be a guess.
func TestParseNinjaCommandsSkipsWhatDoesNotNameItsSource(t *testing.T) {
	input := strings.Join([]string{
		"cc -o app main.o",              // a link, not a compilation
		"cl /c /Fomain.obj main.c",      // MSVC names the source positionally
		"cc -c -o out.o",                // -c with no path after it
		"cd sub && cc -c main.c -o m.o", // a shell composite: which directory?
		"",
	}, "\n")
	commands, err := ParseNinjaCommands(strings.NewReader(input), "/build")
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 {
		t.Fatalf("commands = %+v; a line that does not state its source is a gap, not a guess", commands)
	}
}

func TestParseNinjaCommandsIsBounded(t *testing.T) {
	oversized := strings.Repeat("cc -c a.c -o a.o\n", (maxNinjaCommandsSize/17)+2)
	if _, err := ParseNinjaCommands(strings.NewReader(oversized), "/build"); err == nil {
		t.Error("output past the bound was accepted")
	}
}

func FuzzParseNinjaCommands(f *testing.F) {
	f.Add(realNinjaCommands)
	f.Add("cc -c \"quoted file.c\" -o q.o\n")
	f.Add("cc @flags.rsp -c a.c\n")
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ParseNinjaCommands(strings.NewReader(input), "/build")
	})
}
