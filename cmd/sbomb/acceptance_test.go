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
}
