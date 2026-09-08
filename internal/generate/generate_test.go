package generate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/domain"
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
	shuffled := copyFixtureBuild(t, buildDir)

	original, err := os.ReadFile(filepath.Join(shuffled, "compile_commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(original, &entries); err != nil {
		t.Fatal(err)
	}
	entries[0], entries[1] = entries[1], entries[0]
	reordered, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shuffled, "compile_commands.json"), reordered, 0o600); err != nil {
		t.Fatal(err)
	}

	first := marshalPortable(t, cfg, buildDir, pathmodel.PosixFlavor{})
	second := marshalPortable(t, cfg, shuffled, pathmodel.PosixFlavor{})
	if !bytes.Equal(first, second) {
		t.Fatal("shuffling compile database entries changed the serialized BOM")
	}
}

// copyFixtureBuild copies a committed build directory so a test can modify it
// without touching the corpus.
func copyFixtureBuild(t *testing.T, source string) string {
	t.Helper()
	destination := t.TempDir()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func TestNoAbsolutePathsInOutput(t *testing.T) {
	cfg, buildDir := portableFixture(t)
	output := string(marshalPortable(t, cfg, buildDir, pathmodel.PosixFlavor{}))
	if strings.Contains(output, "\\") || strings.Contains(output, "\"/") || strings.Contains(output, "C:") {
		t.Fatalf("serialized BOM contains a host path: %s", output)
	}
}

func TestRunBuildsGraphAndReportsMissingEvidence(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "fixtures", "gcc-ninja", "p02-static", "build")
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
	// The fixture carries a map and a dependency file, so link evidence must
	// be present and the artifact must anchor a chain.
	for _, finding := range result.Findings {
		if finding.ID == "MISSING_LINK_EVIDENCE" {
			t.Fatal("the fixture has a map and a dependency file; link evidence must be found")
		}
	}
	var artifacts, sources int
	for _, node := range result.Graph.Nodes() {
		switch node.Kind {
		case domain.NodeArtifact:
			artifacts++
		case domain.NodeSource:
			sources++
		}
	}
	if artifacts != 1 {
		t.Errorf("got %d artifact nodes, want exactly one deliverable", artifacts)
	}
	if sources == 0 {
		t.Error("no source node was reached from the deliverable")
	}
	if err := result.Graph.CheckInvariants(); err != nil {
		t.Errorf("graph invariants violated: %v", err)
	}
}

// TestMissingLinkEvidenceIsReported checks the finding on a build directory
// that has compile evidence but nothing the linker produced.
func TestMissingLinkEvidenceIsReported(t *testing.T) {
	buildDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(buildDir, "app"), []byte("binary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "compile_commands.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Artifacts: []config.Artifact{{Path: "app"}}}
	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, finding := range result.Findings {
		if finding.ID == "MISSING_LINK_EVIDENCE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected MISSING_LINK_EVIDENCE, got %v", result.Findings)
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

	if err := os.WriteFile(filepath.Join(buildDir, "app"), []byte("binary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The Makefiles adapter applies to a Makefiles build tree, and a Makefile
	// at the build root is what says it is one (section 9.1).
	if err := os.WriteFile(filepath.Join(buildDir, "Makefile"), []byte("all:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	makeCfg := config.Config{
		Project:   config.Project{Root: root},
		Artifacts: []config.Artifact{{Path: "app", Role: "application"}},
	}
	result, err := RunWithOptions(makeCfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	// A Makefiles build without a map or dependency file still yields a full
	// chain: link.txt reconstructs the link, build.make maps the object to its
	// source, and compiler_depend.make names the headers.
	byType := map[domain.EvidenceType]int{}
	for _, edge := range result.Graph.Edges() {
		byType[edge.Type]++
	}
	for _, required := range []domain.EvidenceType{"link", "source-mapping", "header-dependency"} {
		if byType[required] == 0 {
			t.Errorf("no %s edge was produced; edges are %v", required, byType)
		}
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

	spdx := fileLicenses(spdxFile, nil)
	if len(spdx) != 1 || spdx[0].Expression != "MIT" {
		t.Fatalf("SPDX license was not resolved: %#v", spdx)
	}
	nearest := fileLicenses(licensedFile, nil)
	if len(nearest) != 1 || nearest[0].Expression != "MIT" {
		t.Fatalf("nearest license was not resolved: %#v", nearest)
	}
}

// A map or dependency file named in configuration is a statement, not a hint.
// Falling back to the locations beside the artifact would put evidence in the
// document that the caller did not name, so the run stops instead.
func TestNamedEvidenceThatIsNotThereStopsTheRun(t *testing.T) {
	present := filepath.Join(t.TempDir(), "app.map")
	if err := os.WriteFile(present, []byte("Memory Configuration\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name          string
		mapPath       string
		depfilePath   string
		wantExitError bool
	}{
		{name: "nothing named", wantExitError: false},
		{name: "map is there", mapPath: present, wantExitError: false},
		{name: "map is not", mapPath: filepath.Join(t.TempDir(), "absent.map"), wantExitError: true},
		{name: "depfile is not", depfilePath: filepath.Join(t.TempDir(), "absent.d"), wantExitError: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := requireConfiguredEvidence(testCase.mapPath, testCase.depfilePath)
			if !testCase.wantExitError {
				if err != nil {
					t.Fatalf("requireConfiguredEvidence() = %v, want nil", err)
				}
				return
			}
			var exit *ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("requireConfiguredEvidence() = %v, want an ExitError", err)
			}
			if exit.Code != 2 {
				t.Errorf("exit code = %d, want 2 as for a configured artifact that is not there", exit.Code)
			}
			if exit.Finding.ID != "CONFIGURED_EVIDENCE_MISSING" || exit.Finding.Severity != domain.SeverityError {
				t.Errorf("finding = %s/%s", exit.Finding.ID, exit.Finding.Severity)
			}
			// The path has to be in the finding, or the message cannot be acted on.
			named := testCase.mapPath
			if named == "" {
				named = testCase.depfilePath
			}
			if exit.Finding.Subject.Ref != named {
				t.Errorf("subject = %q, want the path that was named", exit.Finding.Subject.Ref)
			}
		})
	}
}

// A directory is not a map file, and treating it as one would fail later and
// less clearly.
func TestNamedEvidenceThatIsADirectoryIsRefused(t *testing.T) {
	if err := requireConfiguredEvidence(t.TempDir(), ""); err == nil {
		t.Error("a directory was accepted as a linker map")
	}
}
