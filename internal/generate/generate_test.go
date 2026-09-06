package generate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/pathmodel"
)

func portableFixture(t *testing.T) (config.Config, string) {
	t.Helper()
	buildDir := filepath.Join("..", "..", "testdata", "fixtures", "gcc-ninja", "p02-static", "build")
	return config.Config{Project: config.Project{Root: "/__fixture_src__"}}, buildDir
}

func marshalPortable(t *testing.T, cfg config.Config, buildDir string, flavor pathmodel.Flavor) []byte {
	t.Helper()
	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: flavor})
	if err != nil {
		t.Fatal(err)
	}
	output, err := cyclonedx.MarshalBOM(result.BOM)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(output)
}

func TestByteIdenticalAcrossRuns(t *testing.T) {
	cfg, buildDir := portableFixture(t)
	first := marshalPortable(t, cfg, buildDir, pathmodel.PosixFlavor{})
	for run := 1; run < 10; run++ {
		if got := marshalPortable(t, cfg, buildDir, pathmodel.PosixFlavor{}); !bytes.Equal(first, got) {
			t.Fatalf("run %d changed the serialized BOM", run+1)
		}
	}
}

func TestByteIdenticalWithShuffledInputOrder(t *testing.T) {
	cfg, buildDir := portableFixture(t)
	original, err := os.ReadFile(filepath.Join(buildDir, "compile_commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(original, &entries); err != nil {
		t.Fatal(err)
	}
	entries[0], entries[1] = entries[1], entries[0]
	shuffled, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	temporaryBuild := t.TempDir()
	if err := os.WriteFile(filepath.Join(temporaryBuild, "compile_commands.json"), shuffled, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temporaryBuild, "link-trace.txt"), []byte("portable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := marshalPortable(t, cfg, buildDir, pathmodel.PosixFlavor{})
	second := marshalPortable(t, cfg, temporaryBuild, pathmodel.PosixFlavor{})
	if !bytes.Equal(first, second) {
		t.Fatal("shuffling compile database entries changed the serialized BOM")
	}
}

func TestNoAbsolutePathsInOutput(t *testing.T) {
	cfg, buildDir := portableFixture(t)
	output := string(marshalPortable(t, cfg, buildDir, pathmodel.PosixFlavor{}))
	if strings.Contains(output, "\\") || strings.Contains(output, "\"/") || strings.Contains(output, "C:") {
		t.Fatalf("serialized BOM contains a host path: %s", output)
	}
}

func TestRunBuildsGraphAndReportsMissingEvidence(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "fixtures", "gcc-ninja", "p02-static")
	result, err := Run(config.Config{}, fixture, true)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Graph == nil || len(result.Graph.Nodes()) == 0 {
		t.Fatal("Run did not create an evidence graph")
	}
	if result.BOM.Metadata == nil || result.BOM.Metadata.Timestamp != "" {
		t.Fatal("reproducible BOM should omit metadata timestamp")
	}
	found := false
	for _, finding := range result.Findings {
		if finding.ID == "MISSING_LINK_EVIDENCE" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected missing link evidence finding")
	}
}

func TestRunUsesMakeEvidenceWhenCompileDatabaseIsMissing(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	targetDir := filepath.Join(buildDir, "CMakeFiles", "app.dir")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "src", "main.c")
	header := filepath.Join(root, "include", "config.h")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(header), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("int main(void) { return 0; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(header, []byte("#define VALUE 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(targetDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("build.make", "CMakeFiles/app.dir/main.c.o: "+source+"\n")
	write("link.txt", "cc -o app CMakeFiles/app.dir/main.c.o\n")
	write("compiler_depend.make", "CMakeFiles/app.dir/main.c.o: "+source+" "+header+"\n")

	result, err := RunWithOptions(config.Config{Project: config.Project{Root: root}}, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	makeEdges := 0
	for _, edge := range result.Graph.Edges() {
		if edge.Adapter == "make" {
			makeEdges++
		}
	}
	if makeEdges < 3 {
		t.Fatalf("got %d Make evidence edges, want link, source, and header edges", makeEdges)
	}
	for _, finding := range result.Findings {
		if finding.ID == "MISSING_LINK_EVIDENCE" {
			t.Fatal("Make link.txt should satisfy link evidence")
		}
	}
}

func TestFileComponentResolvesSPDXAndNearestLicense(t *testing.T) {
	root := t.TempDir()
	spdxFile := filepath.Join(root, "src", "spdx.c")
	licensedDir := filepath.Join(root, "vendor", "lib")
	licensedFile := filepath.Join(licensedDir, "lib.c")
	if err := os.MkdirAll(filepath.Dir(spdxFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(licensedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spdxFile, []byte("/* SPDX-License-Identifier: MIT */\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(licensedFile, []byte("int answer(void) { return 42; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(licensedDir, "LICENSE"), []byte("SPDX-License-Identifier: MIT\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	spdx := fileComponent("project:src/spdx.c", spdxFile, anchors.ScopeProject, pathmodel.PosixFlavor{}, nil)
	if len(spdx.Licenses) != 1 || spdx.Licenses[0].Expression != "MIT" {
		t.Fatalf("SPDX license was not resolved: %#v", spdx.Licenses)
	}
	nearest := fileComponent("project:vendor/lib/lib.c", licensedFile, anchors.ScopeProject, pathmodel.PosixFlavor{}, nil)
	if len(nearest.Licenses) != 1 || nearest.Licenses[0].Expression != "MIT" {
		t.Fatalf("nearest license was not resolved: %#v", nearest.Licenses)
	}
}
