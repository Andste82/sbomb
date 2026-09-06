//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSubmoduleBecomesItsOwnComponent covers strategy 3 of section 19.2 end to
// end. It cannot be a corpus fixture: .gitmodules lives in the source tree and
// the committed corpus carries build evidence only, so the declaration would
// not be there to read.
//
// It also pins the rule of section 19.4 that matters most: git metadata may
// describe a component but must never expand the used-file set. The project
// checks out two submodules and links one of them; only that one appears.
func TestSubmoduleBecomesItsOwnComponent(t *testing.T) {
	for _, tool := range []string{"cmake", "ninja", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(mustThisFile(t)), "..", ".."))
	work := t.TempDir()
	source := filepath.Join(work, "src")
	build := filepath.Join(work, "build")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	git := func(dir string, args ...string) {
		t.Helper()
		full := append([]string{"-C", dir,
			"-c", "user.name=fixture", "-c", "user.email=fixture@invalid",
			"-c", "protocol.file.allow=always", "-c", "commit.gpgsign=false"}, args...)
		command := exec.Command("git", full...)
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	writeIn := func(dir, name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Two standalone repositories, one linked and one not.
	for _, dep := range []string{"tinyhash", "unusedlib"} {
		repo := filepath.Join(work, "repos", dep)
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		writeIn(repo, dep+".h", "#pragma once\nint "+dep+"_value(void);\n")
		writeIn(repo, dep+".c", "#include \""+dep+".h\"\nint "+dep+"_value(void) { return 7; }\n")
		writeIn(repo, "LICENSE", "SPDX-License-Identifier: MIT\n")
		writeIn(repo, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\nproject("+dep+" C)\n"+
			"add_library("+dep+" STATIC "+dep+".c)\ntarget_include_directories("+dep+" PUBLIC ${CMAKE_CURRENT_SOURCE_DIR})\n")
		git(repo, "init", "-q", "-b", "main")
		git(repo, "add", "-A")
		git(repo, "commit", "-qm", "initial")
		git(repo, "tag", "v3.1.0")
	}

	writeIn(source, "main.c", "#include \"tinyhash.h\"\n\nint main(void) { return tinyhash_value() & 1; }\n")
	writeIn(source, "CMakeLists.txt", `cmake_minimum_required(VERSION 3.20)
project(submodule_demo C)
add_subdirectory(dep/tinyhash)
add_subdirectory(dep/unusedlib)
add_executable(app main.c)
target_link_libraries(app PRIVATE tinyhash)
target_link_options(app PRIVATE "-Wl,-Map=$<TARGET_FILE:app>.map" "-Wl,--dependency-file=$<TARGET_FILE:app>.d")
`)
	git(source, "init", "-q", "-b", "main")
	git(source, "submodule", "add", "-q", filepath.Join(work, "repos", "tinyhash"), "dep/tinyhash")
	git(source, "submodule", "add", "-q", filepath.Join(work, "repos", "unusedlib"), "dep/unusedlib")
	git(source, "add", "-A")
	git(source, "commit", "-qm", "project")

	queryDir := filepath.Join(build, ".cmake", "api", "v1", "query", "client-sbomb")
	if err := os.MkdirAll(queryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	query := `{"requests":[{"kind":"codemodel","version":2},{"kind":"cache","version":2},{"kind":"toolchains","version":1}]}`
	if err := os.WriteFile(filepath.Join(queryDir, "query.json"), []byte(query), 0o644); err != nil {
		t.Fatal(err)
	}
	configure := exec.Command("cmake", "-S", source, "-B", build, "-G", "Ninja",
		"-DCMAKE_BUILD_TYPE=Debug", "-DCMAKE_EXPORT_COMPILE_COMMANDS=ON")
	if output, err := configure.CombinedOutput(); err != nil {
		t.Fatalf("cmake configure: %v\n%s", err, output)
	}
	if output, err := exec.Command("cmake", "--build", build).CombinedOutput(); err != nil {
		t.Fatalf("cmake build: %v\n%s", err, output)
	}

	sbomb := filepath.Join(work, "sbomb")
	buildTool := exec.Command("go", "build", "-o", sbomb, "./cmd/sbomb")
	buildTool.Dir = root
	if output, err := buildTool.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}

	generate := func(extra ...string) map[string]componentView {
		t.Helper()
		output := filepath.Join(t.TempDir(), "out.cdx.json")
		args := append([]string{"generate", "--build-dir", build, "--policy", "lenient",
			"--output", output}, extra...)
		command := exec.Command(sbomb, args...)
		command.Dir = root
		if combined, err := command.CombinedOutput(); err != nil {
			t.Fatalf("sbomb generate: %v\n%s", err, combined)
		}
		return readComponents(t, output)
	}

	// Without introspection the boundary and the repository URL are known, but
	// .gitmodules records no version and section 20.1 forbids inventing one.
	plain := generate()
	linked, ok := plain["component:tinyhash"]
	if !ok {
		t.Fatalf("the linked submodule is not its own component; got %v", keysOf(plain))
	}
	if linked.Version != "" {
		t.Errorf("version = %q without introspection, want none", linked.Version)
	}
	// Section 19.4: a checked-out submodule that nothing links is not a
	// dependency of the product.
	if _, present := plain["component:unusedlib"]; present {
		t.Error("a submodule that nothing links became a component")
	}

	// With introspection the checkout answers for itself.
	introspected := generate("--allow-introspection=git")
	described, ok := introspected["component:tinyhash"]
	if !ok {
		t.Fatalf("component missing with introspection; got %v", keysOf(introspected))
	}
	if described.Version != "3.1.0" {
		t.Errorf("version = %q, want the tag the checkout carries", described.Version)
	}
	if !strings.HasPrefix(described.PURL, "pkg:generic/tinyhash@3.1.0?vcs_url=") {
		t.Errorf("purl = %q, want the generic form of section 20.4", described.PURL)
	}
}

type componentView struct {
	BomRef  string `json:"bom-ref"`
	Version string `json:"version"`
	PURL    string `json:"purl"`
}

func readComponents(t *testing.T, path string) map[string]componentView {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []componentView `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	out := map[string]componentView{}
	for _, component := range document.Components {
		out[component.BomRef] = component
	}
	return out
}

func keysOf(m map[string]componentView) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
