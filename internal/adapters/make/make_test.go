package make

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseTargetEvidence(t *testing.T) {
	root := t.TempDir()
	// The adapter applies to a Makefiles build tree, which a Makefile is what
	// identifies (section 9.1).
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("all:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(root, "CMakeFiles", "app.dir")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(targetDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("build.make", "CMakeFiles/app.dir/src/main.cpp.o: /project/src/main.cpp\nCMakeFiles/app.dir/other/main.cpp.o: /project/other/main.cpp\n")
	write("link.txt", "c++ -o app CMakeFiles/app.dir/src/main.cpp.o @objects.rsp\n")
	write("objects.rsp", "CMakeFiles/app.dir/other/main.cpp.o /libs/libthing.a\n")
	write("compiler_depend.make", "CMakeFiles/app.dir/src/main.cpp.o: /project/src/main.cpp /project/include/a.h\n")

	parsed, err := Parse(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Targets) != 1 {
		t.Fatalf("got %d targets, want 1", len(parsed.Targets))
	}
	target := parsed.Targets[0]
	wantSources := map[string]string{
		filepath.Join(root, "CMakeFiles", "app.dir", "src", "main.cpp.o"):   "/project/src/main.cpp",
		filepath.Join(root, "CMakeFiles", "app.dir", "other", "main.cpp.o"): "/project/other/main.cpp",
	}
	if !reflect.DeepEqual(target.ObjectSources, wantSources) {
		t.Fatalf("sources = %#v, want %#v", target.ObjectSources, wantSources)
	}
	wantInputs := []string{filepath.Join(root, "CMakeFiles", "app.dir", "src", "main.cpp.o"), filepath.Join(root, "CMakeFiles", "app.dir", "other", "main.cpp.o"), "/libs/libthing.a"}
	if !reflect.DeepEqual(target.LinkInputs, wantInputs) {
		t.Fatalf("link inputs = %#v, want %#v", target.LinkInputs, wantInputs)
	}
	if got := target.ObjectDeps[filepath.Join(root, "CMakeFiles", "app.dir", "CMakeFiles", "app.dir", "src", "main.cpp.o")]; got != nil {
		t.Fatalf("dependency path unexpectedly duplicated: %#v", got)
	}
}

func TestResolvePathAcceptsWindowsAbsolutePathsOnLinux(t *testing.T) {
	if got := resolvePath("/build", "C:/__fixture_src__/main.c"); got != "C:/__fixture_src__/main.c" {
		t.Fatalf("resolvePath() = %q, want C:/__fixture_src__/main.c", got)
	}
}

func TestSplitMakeWordsPreservesWindowsBackslashes(t *testing.T) {
	got := splitMakeWords(`C:\__fixture_src__\crypto.c C:\__fixture_src__\main.c`)
	want := []string{`C:\__fixture_src__\crypto.c`, `C:\__fixture_src__\main.c`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitMakeWords() = %#v, want %#v", got, want)
	}
}

func TestParseBuildMakeReadsNMakeArchiveInputs(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "CMakeFiles", "crypto.dir")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(targetDir, "build.make")
	content := "crypto.lib: CMakeFiles\\crypto.dir\\crypto.c.obj CMakeFiles\\crypto.dir\\unused.c.obj\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	target := Target{ObjectSources: map[string]string{}, ObjectDeps: map[string][]string{}}
	if err := parseBuildMake(path, root, &target); err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "crypto.lib"),
		filepath.Join(root, "CMakeFiles", "crypto.dir", "crypto.c.obj"),
		filepath.Join(root, "CMakeFiles", "crypto.dir", "unused.c.obj"),
	}
	if !reflect.DeepEqual(target.LinkInputs, want) {
		t.Fatalf("LinkInputs = %#v, want %#v", target.LinkInputs, want)
	}
}

func TestParseCommittedNMakeFixtureArchiveInputs(t *testing.T) {
	buildDir := filepath.Join("..", "..", "..", "testdata", "fixtures", "msvc-nmake", "p02-static", "build")
	parsed, err := Parse(buildDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range parsed.Targets {
		if target.Name != "crypto" {
			continue
		}
		if len(target.LinkInputs) != 3 || !strings.HasSuffix(target.LinkInputs[0], "crypto.lib") {
			t.Fatalf("crypto LinkInputs = %#v, want archive plus two objects", target.LinkInputs)
		}
		return
	}
	t.Fatal("crypto target not found")
}

func TestParseMSVCObjectDependencyList(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "CMakeFiles", "app.dir")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "main.c.obj.d"), []byte("C:/__fixture_src__/main.h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := Target{ObjectSources: map[string]string{}, ObjectDeps: map[string][]string{}}
	if err := parseDependencyFiles(directory, &target); err != nil {
		t.Fatal(err)
	}
	object := filepath.Join(root, "CMakeFiles", "app.dir", "main.c.obj")
	if got := target.ObjectDeps[object]; !reflect.DeepEqual(got, []string{"C:/__fixture_src__/main.h"}) {
		t.Fatalf("ObjectDeps[%q] = %#v, want the MSVC header list", object, got)
	}
}

func TestParseFallsBackToDFile(t *testing.T) {
	root := t.TempDir()
	// The adapter applies to a Makefiles build tree, which a Makefile is what
	// identifies (section 9.1).
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("all:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "CMakeFiles", "app.dir", "nested")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := filepath.Join(root, "CMakeFiles", "app.dir", "main.cpp.o") + ": /project/main.cpp /project/include/a.h\n"
	if err := os.WriteFile(filepath.Join(directory, "main.cpp.o.d"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Targets) != 1 {
		t.Fatalf("got %d targets, want 1", len(parsed.Targets))
	}
	target := parsed.Targets[0]
	deps := target.ObjectDeps[filepath.Join(root, "CMakeFiles", "app.dir", "nested", "..", "main.cpp.o")]
	if len(deps) != 2 {
		t.Fatalf("got deps %#v, want source and header", deps)
	}
	if len(target.DependencyFiles) != 1 {
		t.Fatalf("got %d dependency files, want 1", len(target.DependencyFiles))
	}
}
