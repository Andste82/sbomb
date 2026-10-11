package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/testutil"
)

// SPDX 2.3 as the command line reaches it: --spec-version 2.3 beside
// --format spdx-json (or output.specVersion), read back by validate, schema
// and explain, refused before discovery when it cannot be honoured, and kept
// out of the FOSS review record like 3.0.1. What the document says is tested
// in internal/spdx/spdx23 and by the goldens; these tests are about the
// wiring.

// TestGenerateWritesSpdx23: the version is selected by the flag and by the
// configuration, the document is named for its format, it is created at
// SOURCE_DATE_EPOCH under --reproducible, and validate names it the way a
// person does.
func TestGenerateWritesSpdx23(t *testing.T) {
	_, output := generateSpdx(t, "--spec-version", "2.3")
	data := readFile(t, output)
	writer, version, err := sbomwriter.DetectFormat(data)
	if err != nil || writer.ID() != "spdx-json" || version != "2.3" {
		t.Fatalf("--spec-version 2.3 wrote a %v %q document (%v)", writer, version, err)
	}
	var document struct {
		SPDXVersion  string `json:"spdxVersion"`
		CreationInfo struct {
			Created string `json:"created"`
		} `json:"creationInfo"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.SPDXVersion != "SPDX-2.3" || document.CreationInfo.Created != pinnedCreated {
		t.Errorf("spdxVersion %q, created %q; want SPDX-2.3 created %s", document.SPDXVersion, document.CreationInfo.Created, pinnedCreated)
	}
	code, stdout, stderr := execute([]string{"validate", "--input", output})
	if want := "valid SPDX 2.3 document: " + output + "\n"; code != 0 || stdout != want {
		t.Errorf("validate = code %d, stdout %q, stderr %q; want %q", code, stdout, stderr, want)
	}

	// The configuration names the version as well as the flag does.
	t.Setenv("SOURCE_DATE_EPOCH", pinnedEpoch)
	configPath := filepath.Join(t.TempDir(), "sbomb.json")
	if err := os.WriteFile(configPath, []byte(`{"schemaVersion": 1, "project": {"name": "app"},
  "output": {"format": "spdx-json", "specVersion": "2.3"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configured := filepath.Join(t.TempDir(), "configured.spdx.json")
	code, _, stderr = execute([]string{"generate", "--build-dir", testutil.CorpusBuildDir(t, "gcc-make", "p02-static"),
		"--config", configPath, "--policy", "lenient", "--output", configured, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate with output.specVersion 2.3 = code %d, stderr %q", code, stderr)
	}
	if _, version, err := sbomwriter.DetectFormat(readFile(t, configured)); err != nil || version != "2.3" {
		t.Errorf("output.specVersion 2.3 wrote version %q (%v)", version, err)
	}

	selected, err := resolveOutput("spdx-json", "2.3", sbomwriter.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := "sbomb" + outputExtension(selected.writer); got != "sbomb.spdx.json" {
		t.Errorf("default output = %q, want sbomb.spdx.json", got)
	}
}

// TestValidateRefusesABrokenSpdx23Document: a 2.3 document that breaks a rule
// of the specification text the schema does not hold -- here a File without
// the SHA-1 clause 8.4 requires -- is exit code 4, naming the rule.
func TestValidateRefusesABrokenSpdx23Document(t *testing.T) {
	probe := filepath.Join(testutil.RepoRoot(t), "testdata", "spdx", "known-bad-2.3", "file-without-sha1.json")
	code, _, stderr := execute([]string{"validate", "--input", probe})
	if code != 4 || !strings.Contains(stderr, "SHA1") {
		t.Errorf("validate = code %d, stderr %q; want 4 naming the missing SHA1", code, stderr)
	}
	example := filepath.Join(testutil.RepoRoot(t), "testdata", "spdx", "examples-2.3", "SPDXJSONExample-v2.3.spdx.json")
	if code, stdout, stderr := execute([]string{"validate", "--input", example}); code != 0 || !strings.HasPrefix(stdout, "valid SPDX 2.3 document: ") {
		t.Errorf("the SPDX project's own 2.3 example = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// TestSchemaCommandServesTheSpdx23Schema prints the schema a 2.3 document is
// checked against: the one released with SPDX 2.3, byte for byte.
func TestSchemaCommandServesTheSpdx23Schema(t *testing.T) {
	code, stdout, stderr := execute([]string{"schema", "--format", "spdx-json", "--spec-version", "2.3"})
	if code != 0 {
		t.Fatalf("schema = code %d, stderr %q", code, stderr)
	}
	embedded := readFile(t, filepath.Join(testutil.RepoRoot(t), "internal", "spdx", "spdx23", "schema", "spdx-schema-2.3.json"))
	if !bytes.Equal(bytes.TrimSpace([]byte(stdout)), bytes.TrimSpace(embedded)) {
		t.Error("schema --spec-version 2.3 is not the embedded SPDX 2.3 schema")
	}
}

// TestSpdx23IsRefusedBeforeDiscoveryWhereItCannotBeWritten: the checks that
// stand before discovery hold for 2.3 as for 3.0.1 -- a reproducible run
// needs SOURCE_DATE_EPOCH, a TLP cannot be carried, and 2.3 is no CycloneDX
// version.
func TestSpdx23IsRefusedBeforeDiscoveryWhereItCannotBeWritten(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "")
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	output := filepath.Join(t.TempDir(), "out.spdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--output", output, "--reproducible", "--format", "spdx-json", "--spec-version", "2.3"})
	if code != 1 || !strings.HasPrefix(stderr, "REPRODUCIBLE_CREATION_TIME_MISSING: ") {
		t.Errorf("reproducible without SOURCE_DATE_EPOCH = code %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(output); err == nil {
		t.Error("a document was written")
	}

	directory := t.TempDir()
	configPath := filepath.Join(directory, "sbomb.json")
	if err := os.WriteFile(configPath, []byte(`{"schemaVersion": 1, "project": {"name": "tlp"},
  "output": {"format": "spdx-json", "specVersion": "2.3", "tlp": "AMBER"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const want = "output.tlp cannot be carried by SPDX 2.3"
	code, _, stderr = execute([]string{"generate", "--build-dir", filepath.Join(directory, "never-created"),
		"--config", configPath, "--output", filepath.Join(directory, "out.spdx.json")})
	if code != 1 || !strings.Contains(stderr, want) {
		t.Errorf("TLP = code %d, stderr %q; want 1 and %q", code, stderr, want)
	}

	code, _, stderr = execute([]string{"generate", "--build-dir", buildDir, "--format", "cyclonedx-json", "--spec-version", "2.3"})
	if want := `format "cyclonedx-json" does not support version "2.3"`; code != 1 || !strings.Contains(stderr, want) {
		t.Errorf("cyclonedx-json 2.3 = code %d, stderr %q; want 1 and %q", code, stderr, want)
	}
}

// TestSelfWritesSpdx23: self takes the version like generate, and what it
// writes passes both tiers of the writer's checks.
func TestSelfWritesSpdx23(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOURCE_DATE_EPOCH", pinnedEpoch)
	output := filepath.Join(t.TempDir(), "self.spdx.json")
	code, _, stderr := execute([]string{"self", executable, "--output", output, "--version", "9.9.9",
		"--format", "spdx-json", "--spec-version", "2.3", "--reproducible"})
	if code != 0 {
		t.Fatalf("self = code %d, stderr %q", code, stderr)
	}
	holdToBothTiers(t, "self", "2.3", readFile(t, output))
}

// TestFossReviewStaysCycloneDXWhenTheSbomIsSpdx23: the review record is
// CycloneDX 1.6 whatever the SBOM is written in, and it does not move with the
// SBOM's format or version (section 32.6). A 2.3 run computes a SHA-1 of every
// file only because its writer asked for it; that digest stays in the SBOM.
func TestFossReviewStaysCycloneDXWhenTheSbomIsSpdx23(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", pinnedEpoch)
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	sourceDir := testutil.CorpusSourceTree(t)
	review := func(name string, format ...string) []byte {
		directory := t.TempDir()
		out := filepath.Join(directory, "foss")
		args := append([]string{"generate", "--build-dir", buildDir, "--source-dir", sourceDir, "--policy", "lenient",
			"--reproducible", "--output", filepath.Join(directory, "out.json"), "--foss-out", out}, format...)
		code, _, stderr := execute(args)
		if code != 0 || stderr != "" {
			t.Fatalf("%s: generate = code %d, stderr %q", name, code, stderr)
		}
		return readFile(t, filepath.Join(out, "foss-review.json"))
	}
	spdx23 := review("SPDX 2.3", "--format", "spdx-json", "--spec-version", "2.3")
	document := parseDocument(t, spdx23)
	if document["bomFormat"] != "CycloneDX" || document["specVersion"] != "1.6" {
		t.Errorf("foss-review.json of an SPDX 2.3 run is %v %v, want CycloneDX 1.6", document["bomFormat"], document["specVersion"])
	}
	if !bytes.Contains(spdx23, []byte(`"SHA-256"`)) {
		t.Fatal("the review record states no SHA-256; a fixture that hashes nothing proves nothing here")
	}
	if bytes.Contains(spdx23, []byte(`"SHA-1"`)) {
		t.Error("the review record of a 2.3 run carries the SHA-1 only the SPDX 2.3 writer asked for")
	}
	for name, format := range map[string][]string{
		"SPDX 3.0.1": {"--format", "spdx-json", "--spec-version", "3.0.1"},
		"CycloneDX":  {"--format", "cyclonedx-json"},
	} {
		if other := review(name, format...); !bytes.Equal(spdx23, other) {
			t.Errorf("foss-review.json of an SPDX 2.3 run differs from that of a %s run", name)
		}
	}
}

// TestInventoryDumpDoesNotMoveWithTheSpdxVersion: SPDX 2.3 asks for a SHA-1
// of every file, and that digest is the document's, not the inventory's. The
// dump of the same build is byte-identical whichever version, or format, the
// run writes (section 40: it must not move when a serialization does).
func TestInventoryDumpDoesNotMoveWithTheSpdxVersion(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", pinnedEpoch)
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	sourceDir := testutil.CorpusSourceTree(t)
	dump := func(name string, format ...string) []byte {
		directory := t.TempDir()
		dumpPath := filepath.Join(directory, "inventory.json")
		args := append([]string{"generate", "--build-dir", buildDir, "--policy", "lenient", "--source-dir", sourceDir,
			"--output", filepath.Join(directory, "out.json"), "--inventory-dump", dumpPath, "--reproducible"}, format...)
		code, _, stderr := execute(args)
		if code != 0 || stderr != "" {
			t.Fatalf("%s: generate = code %d, stderr %q", name, code, stderr)
		}
		return readFile(t, dumpPath)
	}
	spdx23 := dump("SPDX 2.3", "--format", "spdx-json", "--spec-version", "2.3")
	if !bytes.Contains(spdx23, []byte(`"SHA-256"`)) {
		t.Fatal("the dump states no SHA-256; a fixture that hashes nothing proves nothing here")
	}
	if bytes.Contains(spdx23, []byte(`"SHA-1"`)) {
		t.Error("the SPDX 2.3 run put the SHA-1 its writer asked for into the inventory dump")
	}
	for name, format := range map[string][]string{
		"SPDX 3.0.1": {"--format", "spdx-json", "--spec-version", "3.0.1"},
		"CycloneDX":  {"--format", "cyclonedx-json"},
	} {
		if other := dump(name, format...); !bytes.Equal(spdx23, other) {
			t.Errorf("the inventory dump of an SPDX 2.3 run differs from that of a %s run", name)
		}
	}
}
