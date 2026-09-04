package compiledb

import (
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
	if got.File != filepath.Clean("/work/src/main.cpp") || got.Output != filepath.Clean("/work/build/app.o") {
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
	data := []byte(`[{"directory":"` + dir + `","file":"src/main.c","command":"cc @flags.rsp -c src/main.c"}]`)
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
