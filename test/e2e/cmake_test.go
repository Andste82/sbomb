//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCMakeTargetRunsSbombOnlyOnDemand(t *testing.T) {
	if _, err := exec.LookPath("cmake"); err != nil {
		t.Skip("cmake is not installed")
	}
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	work := t.TempDir()
	src := filepath.Join(work, "src")
	build := filepath.Join(work, "build")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\nproject(p01 C)\nadd_executable(app main.c)\ninclude(\""+filepath.Join(root, "cmake", "Sbomb.cmake")+"\")\nsbomb_enable(TARGET app)\n")
	write("main.c", "int main(void) { return 0; }\n")
	sbomb := filepath.Join(work, "sbomb")
	command := exec.Command("go", "build", "-o", sbomb, "./cmd/sbomb")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	if output, err := run(root, "cmake", "-S", src, "-B", build, "-DSBOMB_EXECUTABLE="+sbomb); err != nil {
		t.Fatalf("cmake configure: %v\n%s", err, output)
	}
	// The module has to arrange for a File API reply, and there are two ways
	// depending on the CMake it is run with. From 3.27 cmake_file_api() files
	// the query for the configure that is happening, so the reply is already
	// there; below that the query is written by hand and only the next run
	// sees it, which is why the SBOM target re-configures. Asserting the
	// mechanism would pin whichever one this machine happens to take, so what
	// is asserted is that one of them did its job.
	queried := false
	if entries, globErr := filepath.Glob(filepath.Join(build, ".cmake", "api", "v1", "reply", "index-*.json")); globErr == nil && len(entries) > 0 {
		queried = true
	}
	if _, err := os.Stat(filepath.Join(build, ".cmake", "api", "v1", "query", "client-sbomb", "query.json")); err == nil {
		queried = true
	}
	if !queried {
		t.Fatal("the module neither filed a File API query nor produced a reply")
	}
	if output, err := run(root, "cmake", "--build", build); err != nil {
		t.Fatalf("ordinary build: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(build, "sbom", "app.cdx.json")); !os.IsNotExist(err) {
		t.Fatalf("ordinary build unexpectedly produced SBOM: %v", err)
	}
	if output, err := run(root, "cmake", "--build", build, "--target", "sbomb"); err != nil {
		t.Fatalf("sbomb target: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(build, "compile_commands.json")); err != nil {
		t.Fatalf("compile_commands.json was not enabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(build, "sbom", "app.cdx.json")); err != nil {
		t.Fatalf("sbomb target did not produce SBOM: %v", err)
	}
	// By now the reply must exist on either path: it is what the query was
	// for, and without it sbomb knows no targets, anchors or toolchain.
	replies, err := filepath.Glob(filepath.Join(build, ".cmake", "api", "v1", "reply", "index-*.json"))
	if err != nil || len(replies) == 0 {
		t.Fatalf("no File API reply after the sbomb target ran: %v", err)
	}
}

func TestWindowsPathFlavorWithHostileBuildPath(t *testing.T) {
	root := filepath.Clean(filepath.Join(filepath.Dir(mustThisFile(t)), "..", ".."))
	work := t.TempDir()
	build := filepath.Join(work, "build space é")
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	source := `C:\fixture\project\src\main.c`
	escapedSource := strings.ReplaceAll(source, `\`, `\\`)
	compileDB := `[{"directory":"C:\\fixture\\project","file":"` + escapedSource + `","output":"C:\\fixture\\build\\main.o","arguments":["cc","-c","` + escapedSource + `"]}]`
	if err := os.WriteFile(filepath.Join(build, "compile_commands.json"), []byte(compileDB), 0o600); err != nil {
		t.Fatal(err)
	}
	// A deliverable and its map: without a configured artifact the run refuses
	// to guess what the SBOM is about (section 5.2).
	if err := os.WriteFile(filepath.Join(build, "app.exe"), []byte("MZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkMap := "Archive member included to satisfy reference by file (symbol)\n\n" +
		"Linker script and memory map\n\nLOAD C:\\fixture\\build\\main.o\n"
	if err := os.WriteFile(filepath.Join(build, "app.exe.map"), []byte(linkMap), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(work, "config.json")
	configJSON := `{"project":{"name":"windows-paths","root":"C:\\fixture\\project"},` +
		`"build":{"dir":"C:\\fixture\\build"},` +
		`"artifacts":[{"path":"app.exe","role":"application"}]}`
	if err := os.WriteFile(config, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	sbomb := filepath.Join(work, "sbomb")
	command := exec.Command("go", "build", "-o", sbomb, "./cmd/sbomb")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	output := filepath.Join(work, "windows-paths.cdx.json")
	if outputBytes, err := run(root, sbomb, "generate", "--build-dir", build, "--config", config, "--path-flavor", "windows", "--output", output, "--reproducible", "--policy", "lenient"); err != nil {
		t.Fatalf("windows path generation: %v\n%s", err, outputBytes)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("windows path SBOM was not written: %v", err)
	}
}

func mustThisFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return file
}

func run(dir, name string, args ...string) ([]byte, error) {
	command := exec.Command(name, args...)
	command.Dir = dir
	return command.CombinedOutput()
}
