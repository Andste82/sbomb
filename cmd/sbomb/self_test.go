package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The test binary is a Go executable this module produced, so "sbomb self"
// pointed at it exercises the whole path a release runs: read the linker's
// record, build the document, validate it, write it.
func TestSelfWritesAValidatedDocumentForTheTestBinary(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "self.cdx.json")

	code, _, stderr := execute([]string{"self", executable,
		"--output", output, "--version", "9.9.9", "--supplier", "Example", "--reproducible"})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		BomFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Metadata    struct {
			Component struct {
				Version string `json:"version"`
				Type    string `json:"type"`
			} `json:"component"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.BomFormat != "CycloneDX" || document.SpecVersion != "1.6" {
		t.Errorf("format = %s %s", document.BomFormat, document.SpecVersion)
	}
	if document.Metadata.Component.Version != "9.9.9" {
		t.Errorf("product version = %q, want the supplied one", document.Metadata.Component.Version)
	}
	if document.Metadata.Component.Type != "application" {
		t.Errorf("product type = %q, want application", document.Metadata.Component.Type)
	}
}

// Reproducible mode has to mean it: a release publishes the document beside
// the binary, and two runs that disagree make the checksum meaningless.
func TestSelfIsReproducible(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	run := func(name string) []byte {
		path := filepath.Join(dir, name)
		if code, _, stderr := execute([]string{"self", executable, "--output", path, "--version", "1.0.0", "--reproducible"}); code != 0 {
			t.Fatalf("exit code = %d, stderr = %s", code, stderr)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if first, second := run("a.json"), run("b.json"); string(first) != string(second) {
		t.Error("two reproducible runs produced different documents")
	}
}

func TestSelfRefusesAFileThatIsNotAGoBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execute([]string{"self", path, "--output", filepath.Join(t.TempDir(), "out.json")})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %s", code, stderr)
	}
}

func TestSelfRejectsAMalformedLicenceCuration(t *testing.T) {
	code, _, stderr := execute([]string{"self", "binary", "--license", "no-equals-sign"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Error("a malformed --license should say what it expected")
	}
}

func TestSelfWithoutABinaryPrintsUsage(t *testing.T) {
	code, _, stderr := execute([]string{"self"})
	if code != 1 || stderr == "" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
}
