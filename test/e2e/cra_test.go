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

// TestCRAProfileGatesOnFieldCompleteness is the phase 4 acceptance criterion.
// It builds a real project so the sources are on disk and can be hashed, then
// checks that the cra profile fails when the CRA fields of section 1.5(1) are
// missing and passes once configuration supplies them.
//
// It needs a real toolchain, hence the e2e tag: the committed corpus carries
// build evidence but not sources, so no file there can be hashed.
func TestCRAProfileGatesOnFieldCompleteness(t *testing.T) {
	for _, tool := range []string{"cmake", "ninja"} {
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

	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.c", "#include \"app.h\"\n\nint main(void) { return answer() & 1; }\n")
	write("app.c", "/* SPDX-License-Identifier: MIT */\n#include \"app.h\"\n\nint answer(void) { return 42; }\n")
	write("app.h", "/* SPDX-License-Identifier: MIT */\n#pragma once\nint answer(void);\n")
	write("CMakeLists.txt", `cmake_minimum_required(VERSION 3.20)
project(cra_demo C)
add_executable(app main.c app.c)
target_link_options(app PRIVATE "-Wl,-Map=$<TARGET_FILE:app>.map" "-Wl,--dependency-file=$<TARGET_FILE:app>.d")
install(TARGETS app)
`)

	// Ask for the File API reply, exactly as cmake/Sbomb.cmake does.
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

	run := func(configPath, output string) (int, string) {
		t.Helper()
		args := []string{"generate", "--build-dir", build, "--policy", "cra",
			"--output", output, "--findings-json", output + ".findings.json"}
		if configPath != "" {
			args = append(args, "--config", configPath)
		}
		command := exec.Command(sbomb, args...)
		command.Dir = root
		combined, err := command.CombinedOutput()
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(combined)
		}
		if err != nil {
			t.Fatalf("running sbomb: %v\n%s", err, combined)
		}
		return 0, string(combined)
	}

	// Without configuration the component has no version and no supplier.
	bare := filepath.Join(work, "bare.cdx.json")
	code, output := run("", bare)
	if code != 3 {
		t.Fatalf("cra without configuration = %d, want 3\n%s", code, output)
	}
	missing := findingIDs(t, bare+".findings.json")
	for _, required := range []string{"UNKNOWN_VERSION", "MISSING_SUPPLIER"} {
		if !missing[required] {
			t.Errorf("cra did not report %s; findings were %v", required, keys(missing))
		}
	}

	// With configuration the CRA fields are present and the run passes.
	configPath := filepath.Join(work, "sbomb.json")
	configJSON := `{
  "project": {"name": "cra-demo", "version": "1.0.0", "supplier": "Example Org", "license": "MIT", "root": "` + source + `"},
  "build": {"dir": "` + build + `"},
  "components": [
    {"path": ".", "name": "cra-demo", "type": "application", "version": "1.0.0", "supplier": "Example Org", "license": "MIT"}
  ]
}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	complete := filepath.Join(work, "complete.cdx.json")
	code, output = run(configPath, complete)
	if code != 0 {
		t.Fatalf("cra with a complete configuration = %d, want 0\n%s\nfindings: %v",
			code, output, keys(findingIDs(t, complete+".findings.json")))
	}

	// The document must actually carry the fields, not merely pass the gate.
	data, err := os.ReadFile(complete)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []struct {
			Type     string `json:"type"`
			BomRef   string `json:"bom-ref"`
			Version  string `json:"version"`
			Supplier *struct {
				Name string `json:"name"`
			} `json:"supplier"`
			Hashes   []map[string]string `json:"hashes"`
			Licenses []map[string]any    `json:"licenses"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var checked int
	for _, component := range document.Components {
		if component.Type == "file" {
			if len(component.Hashes) == 0 {
				t.Errorf("%s has no hash although its bytes are on disk", component.BomRef)
			}
			continue
		}
		checked++
		if component.Version == "" {
			t.Errorf("%s has no version", component.BomRef)
		}
		if component.Supplier == nil || component.Supplier.Name == "" {
			t.Errorf("%s has no supplier", component.BomRef)
		}
		if len(component.Licenses) == 0 {
			t.Errorf("%s has no license", component.BomRef)
		}
	}
	if checked == 0 {
		t.Fatal("the document contains no grouping component to check")
	}
}

func findingIDs(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Findings []struct {
			ID string `json:"id"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, finding := range document.Findings {
		ids[finding.ID] = true
	}
	return ids
}

func keys(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return strings.Join(out, ", ")
}
