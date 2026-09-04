package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMilestone13Acceptance(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "fixtures", "gcc-13", "p02-static", "build")
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
	goldenReport, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "gcc-13-p02-report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	actualReport, err := os.ReadFile(strictReport)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actualReport, goldenReport) {
		t.Fatalf("report differs from golden:\n%s", actualReport)
	}
	_, _, explainErr := execute([]string{"explain", "--build-dir", buildDir, "--file", "project:src/crypto.c"})
	if explainErr != "" {
		t.Fatalf("explain returned stderr: %s", explainErr)
	}
	_, explainOutput, _ := execute([]string{"explain", "--build-dir", buildDir, "--file", "project:src/crypto.c"})
	goldenExplain, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "explain-crypto.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal([]byte(explainOutput), goldenExplain) {
		t.Fatalf("explain differs from golden:\n%s", explainOutput)
	}

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
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(workingDirectory)
	buildDir := filepath.Join("testdata", "fixtures", "gcc-12-make", "p02-static", "build")
	defer os.Remove(filepath.Join(buildDir, "evidence.json"))
	output := filepath.Join(t.TempDir(), "mk.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("Make acceptance result = code %d, stderr %q", code, stderr)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join("testdata", "golden", "gcc-12-make-p02.cdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatal("Makefiles SBOM differs from golden")
	}
}
