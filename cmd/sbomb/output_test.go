package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAConfiguredTLPTheVersionCannotCarryIsRefusedBeforeAnythingIsCreated:
// the writer is asked whether it can honour the request as soon as the
// configuration that makes it is read, so the refusal comes before the build
// directory is created or a byte of evidence is read -- for generate and for
// foss alike, in the same words.
func TestAConfiguredTLPTheVersionCannotCarryIsRefusedBeforeAnythingIsCreated(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "sbomb.json")
	if err := os.WriteFile(configPath, []byte(`{
  "schemaVersion": 1,
  "project": {"name": "tlp"},
  "output": {"format": "cyclonedx-json", "specVersion": "1.6", "tlp": "AMBER"}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	buildDir := filepath.Join(directory, "never-created")
	const want = "output.tlp needs CycloneDX 1.7; this run writes 1.6"

	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath,
		"--output", filepath.Join(directory, "out.cdx.json")})
	if code != 1 || !strings.Contains(stderr, want) {
		t.Errorf("generate = code %d, stderr %q; want 1 and %q", code, stderr, want)
	}
	if _, err := os.Stat(buildDir); err == nil {
		t.Error("generate created the build directory before refusing")
	}

	code, _, stderr = execute([]string{"foss", "--build-dir", buildDir, "--config", configPath,
		"--out", filepath.Join(directory, "foss")})
	if code != 1 || !strings.Contains(stderr, want) {
		t.Errorf("foss = code %d, stderr %q; want 1 and %q", code, stderr, want)
	}

	// At 1.7 the same configuration is honoured, which is what makes the
	// refusal above about the version and not about the TLP.
	code, _, stderr = execute([]string{"generate", "--build-dir", buildDir, "--config", configPath,
		"--spec-version", "1.7", "--output", filepath.Join(directory, "out.cdx.json")})
	if strings.Contains(stderr, want) {
		t.Errorf("generate at 1.7 = code %d, still refused the TLP: %q", code, stderr)
	}
}
