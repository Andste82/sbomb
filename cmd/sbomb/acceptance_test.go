package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

func TestMilestone13Acceptance(t *testing.T) {
	// The whole build directory, so the run sees the CMake File API reply and
	// can register the toolchain anchor, not just the compile database.
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	configPath := filepath.Join("..", "..", "testdata", "config", "p02-full.json")
	strictOutput := filepath.Join(t.TempDir(), "strict.cdx.json")
	lenientOutput := filepath.Join(t.TempDir(), "lenient.cdx.json")
	strictReport := filepath.Join(t.TempDir(), "strict.txt")
	strictCode, _, strictErr := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--policy", "strict", "--output", strictOutput, "--review-report", strictReport, "--reproducible"})
	if strictCode != 0 || strictErr != "" {
		t.Fatalf("strict result = code %d, stderr %q; want code 0 on complete evidence", strictCode, strictErr)
	}
	lenientCode, _, lenientErr := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--policy", "lenient", "--output", lenientOutput, "--reproducible"})
	if lenientCode != 0 || lenientErr != "" {
		t.Fatalf("lenient result = code %d, stderr %q; want code 0", lenientCode, lenientErr)
	}
	strictSBOM, err := os.ReadFile(strictOutput)
	if err != nil {
		t.Fatal(err)
	}
	lenientSBOM, err := os.ReadFile(lenientOutput)
	if err != nil {
		t.Fatal(err)
	}
	// Policy must never change the document, only the exit code (section 33.3).
	if !bytes.Equal(strictSBOM, lenientSBOM) {
		t.Fatal("policy changed the SBOM; it may only change the verdict")
	}
	actualReport, err := os.ReadFile(strictReport)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "gcc-13-p02-report.txt", actualReport)
	_, _, explainErr := execute([]string{"explain", "--build-dir", buildDir, "--file", "project:crypto.c"})
	if explainErr != "" {
		t.Fatalf("explain returned stderr: %s", explainErr)
	}
	_, explainOutput, _ := execute([]string{"explain", "--build-dir", buildDir, "--file", "project:crypto.c"})
	assertGolden(t, "explain-crypto.txt", []byte(explainOutput))

	// A second, independent copy of the same evidence must produce the same
	// bytes; this is the reproducibility half of the acceptance criterion.
	secondBuild := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	secondOutput := filepath.Join(t.TempDir(), "second.cdx.json")
	secondReport := filepath.Join(t.TempDir(), "second.txt")
	secondCode, _, secondErr := execute([]string{"generate", "--build-dir", secondBuild, "--config", configPath, "--policy", "strict", "--output", secondOutput, "--review-report", secondReport, "--reproducible"})
	if secondCode != 0 || secondErr != "" {
		t.Fatalf("second strict result = code %d, stderr %q; want code 0", secondCode, secondErr)
	}
	secondSBOM, err := os.ReadFile(secondOutput)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(strictSBOM, secondSBOM) {
		t.Fatal("reproducible SBOM changed between runs")
	}
	secondReportBytes, err := os.ReadFile(secondReport)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actualReport, secondReportBytes) {
		t.Fatal("reproducible report changed between runs")
	}

	brokenBuild := t.TempDir()
	if err := os.WriteFile(filepath.Join(brokenBuild, "compile_commands.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	discoveryCode, _, discoveryErr := execute([]string{"generate", "--build-dir", brokenBuild, "--config", configPath, "--policy", "strict", "--output", filepath.Join(t.TempDir(), "broken.cdx.json"), "--reproducible"})
	if discoveryCode != 2 || discoveryErr == "" {
		t.Fatalf("discovery precedence = code %d, stderr %q; want code 2 with an error", discoveryCode, discoveryErr)
	}
}

// TestLinkEvidenceGateSeparatesProfiles exercises the one policy gate that is
// wired end to end today: a build directory with compile evidence but no link
// evidence fails under default and passes under lenient. The remaining gates of
// section 33.1 cannot be exercised until their findings are produced.
func TestLinkEvidenceGateSeparatesProfiles(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	for _, name := range []string{"app.map", "app.d", "libcrypto.a"} {
		if err := os.Remove(filepath.Join(buildDir, name)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join("..", "..", "testdata", "config", "p02-full.json")

	defaultCode, _, _ := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--output", filepath.Join(t.TempDir(), "d.cdx.json"), "--reproducible"})
	if defaultCode != 3 {
		t.Errorf("default profile without link evidence = %d, want 3", defaultCode)
	}
	lenientCode, _, _ := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--policy", "lenient", "--output", filepath.Join(t.TempDir(), "l.cdx.json"), "--reproducible"})
	if lenientCode != 0 {
		t.Errorf("lenient profile without link evidence = %d, want 0", lenientCode)
	}
}

// TestToolchainAndSystemFilesAreNotComponents is the phase 1 acceptance
// criterion at the CLI level: real evidence names many toolchain and system
// files, and none of them may appear as a project dependency.
func TestToolchainAndSystemFilesAreNotComponents(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	output := filepath.Join(t.TempDir(), "scoped.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []struct {
			BomRef     string `json:"bom-ref"`
			Properties []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"properties"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Components) == 0 {
		t.Fatal("the SBOM has no components at all")
	}
	for _, component := range document.Components {
		for _, property := range component.Properties {
			if property.Name != "sbomb:component:scope" {
				continue
			}
			switch property.Value {
			case "toolchain", "system":
				t.Errorf("%s is %s scope and must not be a component", component.BomRef, property.Value)
			case "":
				t.Errorf("%s carries no scope", component.BomRef)
			}
		}
		// Every identity must be anchored, never an absolute host path.
		if strings.HasPrefix(component.BomRef, "file:abs:") {
			t.Errorf("%s was not anchored", component.BomRef)
		}
	}
}

func TestMilestone17MakefilesAcceptance(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	output := filepath.Join(t.TempDir(), "mk.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("Make acceptance result = code %d, stderr %q", code, stderr)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "gcc-make-p02.cdx.json", actual)
}
