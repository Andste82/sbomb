package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// A prebuilt archive is the case strategy 6 of section 13.2 exists for. Nothing
// in this build compiled its members: they are absent from the compile
// database, from the build graph and from every depfile, because a vendor
// shipped them. Their own debug information is the only place their source is
// named.
//
// Without it the SBOM carries an opaque `libvendor.a(vendor_blob.o)` and a
// LINKED_OBJECT_SOURCE_UNRESOLVED finding. With it, the source appears.
func TestAPrebuiltArchiveResolvesThroughItsDebugInformation(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p13-prebuilt")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "sbomb.json")
	if err := os.WriteFile(configPath, []byte(
		`{"project":{"name":"prebuilt","root":"/__fixture_src__"},"build":{"dir":"/__fixture_build__"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "out.cdx.json")
	findingsPath := filepath.Join(dir, "findings.json")

	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
		"--config", configPath, "--output", output, "--policy", "lenient",
		"--reproducible", "--findings-json", findingsPath, "--evidence-dump=off"})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []struct {
			Name       string                         `json:"name"`
			Properties []struct{ Name, Value string } `json:"properties"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}

	var canonicals []string
	for _, component := range document.Components {
		for _, property := range component.Properties {
			if property.Name == "sbomb:path:canonical" {
				canonicals = append(canonicals, property.Value)
			}
		}
	}
	joined := strings.Join(canonicals, " ")
	if !strings.Contains(joined, "vendor_blob.c") {
		t.Errorf("the prebuilt member's source is not in the document: %v", canonicals)
	}
	// And the opaque member must be gone: it is the source that belongs in an
	// SBOM, not the object somebody handed us.
	if strings.Contains(joined, "libvendor.a(") {
		t.Errorf("the archive member is still reported as an unresolved object: %v", canonicals)
	}

	findings, err := os.ReadFile(findingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(findings), "LINKED_OBJECT_SOURCE_UNRESOLVED") {
		t.Error("the object still counts as unresolved")
	}
}
