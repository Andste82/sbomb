package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// Section 30.7 point 7 requires --redact-unanchored-paths to apply to the
// SBOM, the findings JSON and the review report equally. Redaction happens
// where a path becomes an identity, so all three should be covered by
// construction -- but a finding's message or a report's prose is written by
// hand, and one raw path in any of them defeats the option entirely. That is
// what this checks, in all three at once.
func TestRedactionAppliesToEveryOutput(t *testing.T) {
	// p14-foss, because its document contains a generated file that lives in
	// the build tree. The build root is the anchor this test can still move:
	// since section 7.9 the configured source root is the *physical* one and
	// no longer re-anchors anything, so pointing project.root elsewhere leaves
	// the source files anchored exactly where the evidence put them.
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")

	// The corpus records the sentinel roots. Pointing the build anchor
	// somewhere else leaves the generated file under no anchor at all, which is
	// the case section 7.5 redacts.
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "unanchored.json")
	config := `{
  "schemaVersion": 1,
  "project": {"name": "unanchored", "root": "/nowhere/source"},
  "build": {"dir": "/nowhere/build"},
  "mode": "single",
  "output": {"format": "cyclonedx-json", "specVersion": "1.6", "reproducible": true}
}`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	// The sentinel roots are what the corpus records, so their appearance in an
	// output is exactly the leak this option exists to prevent.
	const secret = "__fixture_"

	run := func(t *testing.T, redact bool) map[string]string {
		t.Helper()
		outputDir := t.TempDir()
		fossDir := filepath.Join(outputDir, "foss")
		paths := map[string]string{
			"SBOM":          filepath.Join(outputDir, "out.cdx.json"),
			"findings JSON": filepath.Join(outputDir, "findings.json"),
			"review report": filepath.Join(outputDir, "report.txt"),
			// Section 30.7 is extended by name to the FOSS review record
			// (section 32.6). The notices document is checked separately: it
			// adds no path of its own, and an unmapped component's section
			// 19.3 name is the one identity that can carry one into it.
			"foss review text": filepath.Join(fossDir, "foss-review.txt"),
			"foss review JSON": filepath.Join(fossDir, "foss-review.json"),
			"notices":          filepath.Join(fossDir, "THIRD-PARTY-NOTICES.txt"),
		}
		args := []string{"generate",
			"--build-dir", buildDir,
			"--config", configPath,
			"--policy", "lenient",
			"--output", paths["SBOM"],
			"--findings-json", paths["findings JSON"],
			"--review-report", paths["review report"],
			"--foss-out", fossDir,
			"--reproducible"}
		if redact {
			args = append(args, "--redact-unanchored-paths")
		}
		code, _, stderr := execute(args)
		if code != 0 {
			t.Fatalf("exit code = %d, stderr = %s", code, stderr)
		}
		contents := map[string]string{}
		for name, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			contents[name] = string(data)
		}
		return contents
	}

	// Without the option every output names the paths. Without this half the
	// test would pass just as well against three empty files.
	for name, content := range run(t, false) {
		if !strings.Contains(content, secret) {
			t.Errorf("the unredacted %s does not contain %q, so the redacted comparison proves nothing", name, secret)
		}
	}

	redacted := run(t, true)
	for name, content := range redacted {
		if strings.Contains(content, secret) {
			t.Errorf("the %s still contains %q with --redact-unanchored-paths", name, secret)
		}
	}
	if !strings.Contains(redacted["SBOM"], "redacted/") {
		t.Error("the SBOM contains no redacted identity, so nothing was unanchored to begin with")
	}
}
