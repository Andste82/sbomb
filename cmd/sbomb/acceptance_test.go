package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

func TestMilestone13Acceptance(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "fixtures", "gcc-ninja", "p02-static", "build")
	buildDir := t.TempDir()
	for _, name := range []string{"compile_commands.json", "build.ninja"} {
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(buildDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join("..", "..", "testdata", "config", "p02-full.json")
	strictOutput := filepath.Join(t.TempDir(), "strict.cdx.json")
	lenientOutput := filepath.Join(t.TempDir(), "lenient.cdx.json")
	strictReport := filepath.Join(t.TempDir(), "strict.txt")
	strictCode, _, strictErr := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--policy", "strict", "--output", strictOutput, "--review-report", strictReport, "--reproducible"})
	if strictCode != 3 || strictErr != "" {
		t.Fatalf("strict result = code %d, stderr %q; want code 3", strictCode, strictErr)
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
	if !bytes.Equal(strictSBOM, lenientSBOM) {
		t.Fatal("strict and lenient SBOMs differ")
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

	secondBuild := t.TempDir()
	for _, name := range []string{"compile_commands.json", "build.ninja"} {
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(secondBuild, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	secondOutput := filepath.Join(t.TempDir(), "second.cdx.json")
	secondReport := filepath.Join(t.TempDir(), "second.txt")
	secondCode, _, secondErr := execute([]string{"generate", "--build-dir", secondBuild, "--config", configPath, "--policy", "strict", "--output", secondOutput, "--review-report", secondReport, "--reproducible"})
	if secondCode != 3 || secondErr != "" {
		t.Fatalf("second strict result = code %d, stderr %q; want code 3", secondCode, secondErr)
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
