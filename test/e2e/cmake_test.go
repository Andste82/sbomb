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
	// The include comes above the target, which is the documented order and
	// the one that matters: CMAKE_EXPORT_COMPILE_COMMANDS has to be set before
	// the generator processes a target, or the compile database appears only
	// on the next configure.
	write("CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\nproject(p01 C)\ninclude(\""+filepath.Join(root, "cmake", "Sbomb.cmake")+"\")\nadd_executable(app main.c)\nsbomb_enable(TARGET app)\n")
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

// MAP says "the build already produces this, here it is". The module must
// therefore not add a linker flag of its own -- that would put -Wl,-Map= on
// the link line twice, with the command-line order deciding which file wins --
// and it has to tell sbomb where the file is, or the map is written and never
// read.
//
// The silent version of this failure is what the test exists for: without the
// path being passed, sbomb looks beside the artifact, finds nothing, and
// produces an SBOM that is quietly missing its archive-member evidence.
func TestMapNamedByTheProjectIsUsedAndNotSetTwice(t *testing.T) {
	if _, err := exec.LookPath("cmake"); err != nil {
		t.Skip("cmake is not installed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(mustThisFile(t)), "..", ".."))
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
	// A toolchain file that produces the map itself, which is the situation
	// MAP exists for.
	write("toolchain.cmake", "set(CMAKE_EXE_LINKER_FLAGS_INIT \"-Wl,-Map=${CMAKE_BINARY_DIR}/own.map\")\n")
	write("CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\nproject(mapped C)\n"+
		"include(\""+filepath.Join(root, "cmake", "Sbomb.cmake")+"\")\n"+
		"add_executable(app main.c)\n"+
		"sbomb_enable(TARGET app POLICY lenient MAP \"${CMAKE_BINARY_DIR}/own.map\")\n")
	write("main.c", "int main(void) { return 0; }\n")

	sbomb := filepath.Join(work, "sbomb")
	command := exec.Command("go", "build", "-o", sbomb, "./cmd/sbomb")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	if output, err := run(root, "cmake", "-S", src, "-B", build,
		"-DCMAKE_TOOLCHAIN_FILE="+filepath.Join(src, "toolchain.cmake"),
		"-DSBOMB_EXECUTABLE="+sbomb); err != nil {
		t.Fatalf("cmake configure: %v\n%s", err, output)
	}
	if output, err := run(root, "cmake", "--build", build); err != nil {
		t.Fatalf("ordinary build: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(build, "own.map")); err != nil {
		t.Fatalf("the toolchain file's map was not produced: %v", err)
	}

	// The SBOM run must read that map. -v puts the adapter's answer on stderr.
	output, err := run(root, sbomb, "generate", "--build-dir", build, "--policy", "lenient",
		"--map", filepath.Join(build, "own.map"),
		"--output", filepath.Join(work, "app.cdx.json"), "-v")
	if err != nil {
		t.Fatalf("generate with the named map: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Linker map") {
		t.Errorf("the named map was not read:\n%s", output)
	}

	// And the module must not have added a second -Wl,-Map=. Where the link
	// command is written down depends on the generator, so both are tried
	// rather than pinning one -- and not finding either is a reason to say so,
	// not to skip the assertions already made above.
	linkCommand := ""
	for _, candidate := range []string{
		filepath.Join(build, "build.ninja"),
		filepath.Join(build, "CMakeFiles", "app.dir", "link.txt"),
	} {
		if contents, readErr := os.ReadFile(candidate); readErr == nil {
			linkCommand = string(contents)
			break
		}
	}
	if linkCommand == "" {
		t.Log("neither build.ninja nor link.txt was found; the duplicate-flag check did not run")
		return
	}
	if count := strings.Count(linkCommand, "-Wl,-Map="); count != 1 {
		t.Errorf("-Wl,-Map= appears %d times on the link line, want 1", count)
	}
}

// A path nobody produces is a wrong answer, not a missing one, and the build
// has to stop rather than write an SBOM without the evidence it was told about.
func TestAMapThatNothingProducesFailsTheSbombTarget(t *testing.T) {
	if _, err := exec.LookPath("cmake"); err != nil {
		t.Skip("cmake is not installed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(mustThisFile(t)), "..", ".."))
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
	write("CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\nproject(absent C)\n"+
		"include(\""+filepath.Join(root, "cmake", "Sbomb.cmake")+"\")\n"+
		"add_executable(app main.c)\n"+
		"sbomb_enable(TARGET app POLICY lenient MAP \"${CMAKE_BINARY_DIR}/nobody-writes-this.map\")\n")
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
	if output, err := run(root, "cmake", "--build", build); err != nil {
		t.Fatalf("ordinary build: %v\n%s", err, output)
	}
	output, err := run(root, "cmake", "--build", build, "--target", "sbomb")
	if err == nil {
		t.Fatalf("the sbomb target succeeded although the named map is not there:\n%s", output)
	}
	if !strings.Contains(string(output), "CONFIGURED_EVIDENCE_MISSING") {
		t.Errorf("the failure did not name the finding:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(build, "sbom", "app.cdx.json")); statErr == nil {
		t.Error("an SBOM was written although the named evidence was missing")
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
