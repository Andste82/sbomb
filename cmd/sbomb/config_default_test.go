package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// chdirTemp moves the test into a fresh directory, which is where section
// 32.2 looks for the default sbomb.json, and moves it back afterwards.
func chdirTemp(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	return directory
}

// TestAnInvalidSbombJSONInTheWorkingDirectoryIsReadWithoutConfig: without
// --config, generate, evidence and foss read sbomb.json from the working
// directory (sections 32.2 and 32.6), so an invalid one stops each of them with
// its configuration error rather than being passed over.
func TestAnInvalidSbombJSONInTheWorkingDirectoryIsReadWithoutConfig(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	directory := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(directory, "sbomb.json"), []byte(`{"mode": "bogus-invalid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const want = `invalid mode "bogus-invalid"`
	for name, args := range map[string][]string{
		"generate": {"generate", "--build-dir", buildDir, "--policy", "lenient"},
		"evidence": {"evidence", "--build-dir", buildDir},
		"foss":     {"foss", "--build-dir", buildDir, "--out", filepath.Join(directory, "foss")},
	} {
		code, _, stderr := execute(args)
		if code != 1 || !strings.Contains(stderr, want) {
			t.Errorf("%s = code %d, stderr %q; want 1 and %q", name, code, stderr, want)
		}
	}
}

// TestAValidSbombJSONInTheWorkingDirectoryIsApplied: the default file is
// honoured like a named one, and the run says it picked it up.
func TestAValidSbombJSONInTheWorkingDirectoryIsApplied(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "")
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	directory := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(directory, "sbomb.json"), []byte(`{"output": {"format": "spdx-json"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, logs, stderr := execute([]string{"generate", "-v", "--build-dir", buildDir, "--policy", "lenient"})
	if code != 0 {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(directory, "sbomb.spdx.json")); err != nil {
		t.Errorf("the implicit sbomb.json did not select SPDX: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "sbomb.cdx.json")); err == nil {
		t.Error("generate wrote sbomb.cdx.json although sbomb.json asks for SPDX")
	}
	if !strings.Contains(logs+stderr, "Using configuration 'sbomb.json' from the working directory") {
		t.Errorf("generate did not log the implicit configuration; output %q", logs+stderr)
	}
}

// TestAnExplicitConfigWinsOverSbombJSON: --config names the one file read; a
// sbomb.json beside it is not consulted.
func TestAnExplicitConfigWinsOverSbombJSON(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	directory := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(directory, "sbomb.json"), []byte(`{"mode": "bogus-invalid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "other.json"), []byte(`{"output": {"format": "spdx-json"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient", "--config", "other.json"})
	if code != 0 {
		t.Fatalf("generate --config other.json = code %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(directory, "sbomb.spdx.json")); err != nil {
		t.Errorf("the explicit configuration was not applied: %v", err)
	}
	if code, _, stderr := execute([]string{"evidence", "--build-dir", buildDir, "--config", "other.json"}); code != 0 {
		t.Errorf("evidence --config other.json = code %d, stderr %q", code, stderr)
	}
}

// TestNoSbombJSONMeansNoConfiguration: without the flag and without the
// default file the run uses the built-in defaults, and a directory named
// sbomb.json is not taken for one.
func TestNoSbombJSONMeansNoConfiguration(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	directory := chdirTemp(t)
	if path, implicit := resolveConfigPath("", false); path != "" || implicit {
		t.Errorf("resolveConfigPath without sbomb.json = %q, %v; want none", path, implicit)
	}
	if err := os.Mkdir(filepath.Join(directory, "sbomb.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if path, implicit := resolveConfigPath("", false); path != "" || implicit {
		t.Errorf("resolveConfigPath with a sbomb.json directory = %q, %v; want none", path, implicit)
	}
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient"})
	if code != 0 {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(directory, "sbomb.cdx.json")); err != nil {
		t.Errorf("generate without a configuration did not write sbomb.cdx.json: %v", err)
	}
}

// TestAnEmptyConfigSkipsSbombJSON: an explicitly empty --config is the way to
// run without the sbomb.json of the working directory, in either spelling and
// for each command that reads a configuration.
func TestAnEmptyConfigSkipsSbombJSON(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	directory := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(directory, "sbomb.json"), []byte(`{"mode": "bogus-invalid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if path, implicit := resolveConfigPath("", true); path != "" || implicit {
		t.Errorf("resolveConfigPath with an empty --config = %q, %v; want none", path, implicit)
	}
	for name, args := range map[string][]string{
		"generate --config=":     {"generate", "--build-dir", buildDir, "--policy", "lenient", "--config="},
		"generate --config \"\"": {"generate", "--build-dir", buildDir, "--policy", "lenient", "--config", ""},
		"evidence --config=":     {"evidence", "--build-dir", buildDir, "--config="},
		"foss --config=":         {"foss", "--build-dir", buildDir, "--out", filepath.Join(directory, "foss"), "--config="},
	} {
		if code, _, stderr := execute(args); code != 0 {
			t.Errorf("%s = code %d, stderr %q; want the sbomb.json skipped", name, code, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "sbomb.cdx.json")); err != nil {
		t.Errorf("generate --config= did not run on the defaults: %v", err)
	}
}
