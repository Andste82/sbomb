package compiledb

import (
	"encoding/json"
	"github.com/example/sbomb/internal/pathmodel"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseArgumentsAndOutput(t *testing.T) {
	data := []byte(`[{"directory":"/work/build","file":"../src/main.cpp","arguments":["g++","-Iinclude","-o","app.o","-c","../src/main.cpp"],"command":"ignored -o ignored.o"}]`)
	commands, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 {
		t.Fatalf("got %d commands, want 1", len(commands))
	}
	got := commands[0]
	// The paths are the build machine's, joined to its directory as evidence:
	// /work/src/main.cpp on every host, not \work\src\main.cpp on Windows.
	if got.File != "/work/src/main.cpp" || got.Output != "/work/build/app.o" {
		t.Fatalf("paths = (%q, %q)", got.File, got.Output)
	}
	wantArgs := []string{"g++", "-Iinclude", "-o", "app.o", "-c", "../src/main.cpp"}
	if !reflect.DeepEqual(got.Arguments, wantArgs) {
		t.Fatalf("arguments = %#v, want %#v", got.Arguments, wantArgs)
	}
}

func TestParseCommandAndResponseFiles(t *testing.T) {
	dir := t.TempDir()
	rsp := filepath.Join(dir, "flags.rsp")
	if err := os.WriteFile(rsp, []byte(`-I"include dir" -o obj/main.o`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The directory is quoted by encoding/json: a Windows temporary directory
	// spelled into the JSON by hand is a string of invalid escapes.
	quoted, err := json.Marshal(dir)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`[{"directory":` + string(quoted) + `,"file":"src/main.c","command":"cc @flags.rsp -c src/main.c"}]`)
	commands, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := commands[0].Arguments; !reflect.DeepEqual(got, []string{"cc", "-Iinclude dir", "-o", "obj/main.o", "-c", "src/main.c"}) {
		t.Fatalf("arguments = %#v", got)
	}
	if commands[0].Output != filepath.Join(dir, "obj", "main.o") {
		t.Fatalf("output = %q", commands[0].Output)
	}
	if !reflect.DeepEqual(commands[0].ResponseFiles, []string{rsp}) {
		t.Fatalf("response files = %#v", commands[0].ResponseFiles)
	}
}

func TestParseWithoutOutput(t *testing.T) {
	commands, err := Parse([]byte(`[{"directory":"/tmp","file":"main.c","command":"cc -c main.c"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if commands[0].Output != "" {
		t.Fatalf("output = %q, want empty", commands[0].Output)
	}
}

func TestParseWindowsAbsolutePathsOnLinux(t *testing.T) {
	data := []byte(`[{"directory":"C:/__fixture_build__","file":"C:/__fixture_src__/main.c","command":"cl /c /Foobj/main.obj C:/__fixture_src__/main.c"}]`)
	commands, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	// On a Windows host the path is the host's own and keeps its spelling;
	// what is asserted is that it stays the absolute path it is.
	if got := pathmodel.NormalizeSeparators(commands[0].File); got != `C:/__fixture_src__/main.c` {
		t.Fatalf("file = %q, want C:/__fixture_src__/main.c", got)
	}
}
