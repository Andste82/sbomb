package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
	"github.com/example/sbomb/internal/testutil"
)

// The SPDX writer as the command line reaches it: selected by --format or by
// output.format, refused before discovery when it cannot honour the request,
// read back by validate and explain, and kept out of the FOSS review record,
// which stays CycloneDX. What the document says is tested in internal/spdx and
// by the goldens; these tests are about the wiring.

// pinnedEpoch is the SOURCE_DATE_EPOCH every reproducible SPDX run here pins,
// and pinnedCreated is the creation time a document of such a run states.
const (
	pinnedEpoch   = "1700000000"
	pinnedCreated = "2023-11-14T22:13:20Z"
)

// generateSpdx writes the Make corpus build as SPDX and returns the build
// directory and the document's path. Extra arguments are appended.
func generateSpdx(t *testing.T, extra ...string) (string, string) {
	t.Helper()
	t.Setenv("SOURCE_DATE_EPOCH", pinnedEpoch)
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	output := filepath.Join(t.TempDir(), "out.spdx.json")
	args := append([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--output", output, "--reproducible", "--format", "spdx-json"}, extra...)
	code, _, stderr := execute(args)
	if code != 0 || stderr != "" {
		t.Fatalf("generate --format spdx-json = code %d, stderr %q", code, stderr)
	}
	return buildDir, output
}

// spdxCreationInfo returns the CreationInfo node of an SPDX document, failing
// the test when the document does not have exactly one.
func spdxCreationInfo(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var document struct {
		Context string           `json:"@context"`
		Graph   []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("not an SPDX JSON-LD document: %v", err)
	}
	if document.Context != "https://spdx.org/rdf/3.0.1/spdx-context.jsonld" {
		t.Errorf("@context = %q, want the plain SPDX 3.0.1 context", document.Context)
	}
	var found []map[string]any
	for _, node := range document.Graph {
		if node["type"] == "CreationInfo" {
			found = append(found, node)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the document has %d CreationInfo nodes, want 1", len(found))
	}
	return found[0]
}

// extensionProperties collects every sbomb property an SPDX document carries,
// wherever in the graph it sits, as name -> values.
func extensionProperties(t *testing.T, data []byte) map[string][]string {
	t.Helper()
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	properties := map[string][]string{}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if name, isProperty := typed["extension_cdxPropName"].(string); isProperty {
				text, _ := typed["extension_cdxPropValue"].(string)
				properties[name] = append(properties[name], text)
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(root)
	return properties
}

// TestTheFormatFlagSelectsTheWriter: --format spdx-json writes an SPDX 3.0.1
// document, output.format does the same from the configuration, and the flag
// overrides the configuration as every other output flag does (section 32.2).
func TestTheFormatFlagSelectsTheWriter(t *testing.T) {
	_, output := generateSpdx(t)
	data := readFile(t, output)
	writer, version, err := sbomwriter.DetectFormat(data)
	if err != nil || writer.ID() != "spdx-json" || version != "3.0.1" {
		t.Fatalf("--format spdx-json wrote a %v %q document (%v)", writer, version, err)
	}
	if info := spdxCreationInfo(t, data); info["specVersion"] != "3.0.1" {
		t.Errorf("CreationInfo.specVersion = %v, want 3.0.1", info["specVersion"])
	}

	directory := t.TempDir()
	for _, testCase := range []struct {
		name, format, flag, want string
	}{
		{"configured", "spdx-json", "", "spdx-json"},
		{"the flag over the configuration", "cyclonedx-json", "spdx-json", "spdx-json"},
		{"the configuration back the other way", "spdx-json", "cyclonedx-json", "cyclonedx-json"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			configPath := filepath.Join(directory, strings.ReplaceAll(testCase.name, " ", "-")+".json")
			if err := os.WriteFile(configPath, []byte(`{"schemaVersion": 1, "project": {"name": "format"},
  "output": {"format": "`+testCase.format+`"}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "out.json")
			args := []string{"generate", "--build-dir", testutil.CorpusBuildDir(t, "gcc-make", "p02-static"),
				"--config", configPath, "--policy", "lenient", "--output", output, "--reproducible"}
			if testCase.flag != "" {
				args = append(args, "--format", testCase.flag)
			}
			code, _, stderr := execute(args)
			if code != 0 || stderr != "" {
				t.Fatalf("generate = code %d, stderr %q", code, stderr)
			}
			writer, _, err := sbomwriter.DetectFormat(readFile(t, output))
			if err != nil || writer.ID() != testCase.want {
				t.Errorf("wrote %v (%v), want %s", writer, err, testCase.want)
			}
		})
	}
}

// TestAnUnknownFormatIsAUsageError: a format no writer emits is refused
// before the build directory is created, naming every format there is.
func TestAnUnknownFormatIsAUsageError(t *testing.T) {
	directory := t.TempDir()
	buildDir := filepath.Join(directory, "never-created")
	output := filepath.Join(directory, "out.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--output", output, "--format", "spdx-tag-value"})
	if code != 1 {
		t.Fatalf("--format spdx-tag-value = code %d, want 1", code)
	}
	if want := `unknown output format "spdx-tag-value"; available: cyclonedx-json, spdx-json`; !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	for _, path := range []string{buildDir, output} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s exists after a usage error", path)
		}
	}

	code, _, stderr = execute([]string{"self", "some-binary", "--output", output, "--format", "spdx-tag-value"})
	if code != 1 || !strings.Contains(stderr, "available: cyclonedx-json, spdx-json") {
		t.Errorf("self --format spdx-tag-value = code %d, stderr %q", code, stderr)
	}
}

// TestAVersionTheFormatDoesNotHaveIsAUsageError: format and version are two
// settings, and a pair that does not belong together is a usage error naming
// the versions the chosen format has -- whichever of the two was the mistake.
func TestAVersionTheFormatDoesNotHaveIsAUsageError(t *testing.T) {
	directory := t.TempDir()
	buildDir := filepath.Join(directory, "never-created")
	for _, testCase := range []struct {
		args []string
		want string
	}{
		{[]string{"--format", "spdx-json", "--spec-version", "1.7"}, `format "spdx-json" does not support version "1.7"; supported: 3.0.1`},
		{[]string{"--spec-version", "3.0.1"}, `format "cyclonedx-json" does not support version "3.0.1"; supported: 1.6, 1.7`},
	} {
		args := append([]string{"generate", "--build-dir", buildDir, "--output", filepath.Join(directory, "out.json")}, testCase.args...)
		code, _, stderr := execute(args)
		if code != 1 || !strings.Contains(stderr, testCase.want) {
			t.Errorf("%v = code %d, stderr %q; want 1 and %q", testCase.args, code, stderr, testCase.want)
		}
	}
	if _, err := os.Stat(buildDir); err == nil {
		t.Error("a refused pair still created the build directory")
	}
}

// TestTheDefaultOutputNameFollowsTheFormat: with no --output, the file is
// named by the format it is written in, so an SPDX run never writes an SPDX
// document under a CycloneDX name.
func TestTheDefaultOutputNameFollowsTheFormat(t *testing.T) {
	for format, want := range map[string]string{"cyclonedx-json": "sbomb.cdx.json", "spdx-json": "sbomb.spdx.json"} {
		selected, err := resolveOutput(format, "", sbomwriter.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got := "sbomb" + outputExtension(selected.writer); got != want {
			t.Errorf("%s: default output = %q, want %q", format, got, want)
		}
	}
	selected, err := resolveOutput("spdx-json", "", sbomwriter.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := formatLabel(selected.writer); got != "SPDX" {
		t.Errorf("label = %q, want SPDX", got)
	}
	if selected.version != "3.0.1" {
		t.Errorf("default SPDX version = %q, want 3.0.1", selected.version)
	}
}

// TestARunWithoutOutputNamesTheSpdxDocumentForItsFormat runs generate and
// self the way a user first does: --format spdx-json, no --output, no
// --reproducible, no SOURCE_DATE_EPOCH. The document is named for its format
// (sbomb.spdx.json in the working directory, <binary>.spdx.json beside the
// binary) and never under a CycloneDX name; it is valid, and its creation time
// is the run's wall clock in the one form SPDX 3.0.1 states, which a
// regression in how either command fills the run's time would break.
func TestARunWithoutOutputNamesTheSpdxDocumentForItsFormat(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "")
	// The corpus is found from the repository, so before the directory
	// changes.
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	directory := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	before := time.Now().UTC().Truncate(time.Second)
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient", "--format", "spdx-json"})
	if code != 0 {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	assertWallClockSpdx(t, filepath.Join(directory, "sbomb.spdx.json"), before)
	if _, err := os.Stat(filepath.Join(directory, "sbomb.cdx.json")); err == nil {
		t.Error("an SPDX run wrote sbomb.cdx.json")
	}

	binary := copyExecutable(t, directory)
	before = time.Now().UTC().Truncate(time.Second)
	if code, _, stderr := execute([]string{"self", binary, "--version", "9.9.9", "--format", "spdx-json"}); code != 0 {
		t.Fatalf("self = code %d, stderr %q", code, stderr)
	}
	assertWallClockSpdx(t, binary+".spdx.json", before)
	if _, err := os.Stat(binary + ".cdx.json"); err == nil {
		t.Error("an SPDX self run wrote a .cdx.json document")
	}
}

// TestSelfFallsBackToTheClockOnAnUnreadableSourceDateEpoch: outside
// reproducible mode a SOURCE_DATE_EPOCH no RFC 3339 time can state is no pin,
// for self as for generate. The run states the wall clock and succeeds; it
// does not format a five-digit or negative year and then fail its own document
// as an internal invariant violation (exit 70).
func TestSelfFallsBackToTheClockOnAnUnreadableSourceDateEpoch(t *testing.T) {
	binary := copyExecutable(t, t.TempDir())
	for _, epoch := range []string{"253402300800", "-62167219201", "yesterday"} {
		t.Run(epoch, func(t *testing.T) {
			t.Setenv("SOURCE_DATE_EPOCH", epoch)
			output := filepath.Join(t.TempDir(), "self.spdx.json")
			before := time.Now().UTC().Truncate(time.Second)
			if code, _, stderr := execute([]string{"self", binary, "--version", "9.9.9", "--format", "spdx-json", "--output", output}); code != 0 {
				t.Fatalf("self = code %d, stderr %q", code, stderr)
			}
			assertWallClockSpdx(t, output, before)
		})
	}
}

// copyExecutable copies the test binary -- a Go binary with build information,
// which is what self reads -- into a directory, so that a document written
// beside it lands there.
func copyExecutable(t *testing.T, directory string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "tool")
	if err := os.WriteFile(binary, data, 0o700); err != nil {
		t.Fatal(err)
	}
	return binary
}

// assertWallClockSpdx holds a document written without a pinned time to what
// SPDX 3.0.1 requires of it: it is valid, and its creation time is
// YYYY-MM-DDThh:mm:ssZ and lies between the start of the run and now.
func assertWallClockSpdx(t *testing.T, path string, before time.Time) {
	t.Helper()
	if code, stdout, stderr := execute([]string{"validate", "--input", path}); code != 0 || !strings.HasPrefix(stdout, "valid SPDX 3.0.1") {
		t.Fatalf("validate %s = code %d, stdout %q, stderr %q", path, code, stdout, stderr)
	}
	created, _ := spdxCreationInfo(t, readFile(t, path))["created"].(string)
	if !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$`).MatchString(created) {
		t.Fatalf("created = %q, want YYYY-MM-DDThh:mm:ssZ", created)
	}
	stated, err := time.Parse(time.RFC3339, created)
	if err != nil {
		t.Fatal(err)
	}
	if after := time.Now().UTC(); stated.Before(before) || stated.After(after) {
		t.Errorf("created = %s, want the run's wall clock between %s and %s", created, before.Format(time.RFC3339), after.Format(time.RFC3339))
	}
}

// TestGenerateRefusesSpdxUnderReproducibleWithoutSourceDateEpoch: SPDX
// requires a creation time, and a reproducible run has none to state unless
// SOURCE_DATE_EPOCH pins one. That is refused before discovery with the
// appendix A finding -- no document and no evidence dump -- and an unreadable
// value is refused the same way, saying why. A number of seconds beyond the
// year 9999 is unreadable too: no RFC 3339 time states it, and finding that
// out after discovery would be an internal invariant violation (exit 70) for
// what is a user input error.
func TestGenerateRefusesSpdxUnderReproducibleWithoutSourceDateEpoch(t *testing.T) {
	for _, epoch := range []string{"", "yesterday", "253402300800"} {
		t.Run("SOURCE_DATE_EPOCH="+epoch, func(t *testing.T) {
			t.Setenv("SOURCE_DATE_EPOCH", epoch)
			buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
			output := filepath.Join(t.TempDir(), "out.spdx.json")
			code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
				"--output", output, "--reproducible", "--format", "spdx-json"})
			if code != 1 {
				t.Fatalf("code %d, want 1; stderr %q", code, stderr)
			}
			if !strings.HasPrefix(stderr, "REPRODUCIBLE_CREATION_TIME_MISSING: ") {
				t.Errorf("stderr = %q, want the finding first", stderr)
			}
			if epoch != "" && !strings.Contains(stderr, `"`+epoch+`"`) {
				t.Errorf("an unreadable SOURCE_DATE_EPOCH is not named: %q", stderr)
			}
			if _, err := os.Stat(output); err == nil {
				t.Error("a document was written")
			}
			if _, err := os.Stat(filepath.Join(buildDir, "evidence.json")); err == nil {
				t.Error("discovery ran: the evidence dump was written")
			}
		})
	}

	// The configuration asking for reproducibility is the same request.
	t.Setenv("SOURCE_DATE_EPOCH", "")
	configPath := filepath.Join(t.TempDir(), "sbomb.json")
	if err := os.WriteFile(configPath, []byte(`{"schemaVersion": 1, "project": {"name": "r"},
  "output": {"format": "spdx-json", "reproducible": true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execute([]string{"generate", "--build-dir", testutil.CorpusBuildDir(t, "gcc-make", "p02-static"),
		"--config", configPath, "--output", filepath.Join(t.TempDir(), "out.spdx.json")})
	if code != 1 || !strings.Contains(stderr, "REPRODUCIBLE_CREATION_TIME_MISSING") {
		t.Errorf("output.reproducible = code %d, stderr %q", code, stderr)
	}
	// No --reproducible was passed, so the refusal must not tell the user to
	// drop it: it names both places reproducible mode comes from.
	if strings.Contains(stderr, "drop --reproducible") || !strings.Contains(stderr, "output.reproducible") ||
		!strings.Contains(stderr, "turn reproducible mode off") {
		t.Errorf("the refusal blames a flag that was not passed: %q", stderr)
	}
}

// TestAReproducibleSpdxDocumentIsCreatedAtSourceDateEpoch: the creation time
// SPDX requires is the pinned one, and because the document states it, the
// run does not report that the document omits its timestamp -- a finding
// that stays true of reproducible CycloneDX.
func TestAReproducibleSpdxDocumentIsCreatedAtSourceDateEpoch(t *testing.T) {
	findingsPath := filepath.Join(t.TempDir(), "findings.json")
	_, output := generateSpdx(t, "--findings-json", findingsPath)
	if created := spdxCreationInfo(t, readFile(t, output))["created"]; created != pinnedCreated {
		t.Errorf("created = %v, want %s", created, pinnedCreated)
	}
	if bytes.Contains(readFile(t, findingsPath), []byte("REPRODUCIBLE_MODE_OMITS_TIMESTAMP")) {
		t.Error("an SPDX run reports that its document omits the timestamp, and it states one")
	}

	cycloneFindings := filepath.Join(t.TempDir(), "findings.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", testutil.CorpusBuildDir(t, "gcc-make", "p02-static"),
		"--policy", "lenient", "--output", filepath.Join(t.TempDir(), "out.cdx.json"), "--reproducible",
		"--findings-json", cycloneFindings})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	if !bytes.Contains(readFile(t, cycloneFindings), []byte("REPRODUCIBLE_MODE_OMITS_TIMESTAMP")) {
		t.Error("a reproducible CycloneDX run no longer reports the omitted timestamp")
	}
}

// TestSpdxIsDeterministic is the determinism check of section 29 for SPDX:
// the same evidence pinned to the same SOURCE_DATE_EPOCH gives the same bytes,
// and another pin gives another document, because the creation time is part
// of the document's identity.
func TestSpdxIsDeterministic(t *testing.T) {
	_, first := generateSpdx(t)
	_, second := generateSpdx(t)
	if !bytes.Equal(readFile(t, first), readFile(t, second)) {
		t.Fatal("a reproducible SPDX document changed between runs")
	}
	t.Setenv("SOURCE_DATE_EPOCH", "1700000001")
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	other := filepath.Join(t.TempDir(), "other.spdx.json")
	if code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--output", other, "--reproducible", "--format", "spdx-json"}); code != 0 {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	if bytes.Equal(readFile(t, first), readFile(t, other)) {
		t.Error("two different SOURCE_DATE_EPOCH values gave the same document")
	}
}

// TestSpdxRefusesTLP: SPDX has no field for a distribution constraint on the
// document, so a configured TLP is refused before anything is created --
// rather than written somewhere no consumer reads it as a constraint.
func TestSpdxRefusesTLP(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "sbomb.json")
	if err := os.WriteFile(configPath, []byte(`{"schemaVersion": 1, "project": {"name": "tlp"},
  "output": {"format": "spdx-json", "tlp": "AMBER"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	buildDir := filepath.Join(directory, "never-created")
	const want = "output.tlp cannot be carried by SPDX 3.0.1"
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath,
		"--output", filepath.Join(directory, "out.spdx.json")})
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
}

// TestValidateReadsAnSpdxDocument: validate detects the format and names it
// the way a person does.
func TestValidateReadsAnSpdxDocument(t *testing.T) {
	_, output := generateSpdx(t)
	code, stdout, stderr := execute([]string{"validate", "--input", output})
	if code != 0 {
		t.Fatalf("validate = code %d, stderr %q", code, stderr)
	}
	if want := "valid SPDX 3.0.1 document: " + output + "\n"; stdout != want {
		t.Errorf("validate said %q, want %q", stdout, want)
	}

	// A broken document is exit code 4, not a usage error.
	broken := bytes.Replace(readFile(t, output), []byte(`"specVersion": "3.0.1"`), []byte(`"specVersion": 3`), 1)
	brokenPath := filepath.Join(t.TempDir(), "broken.spdx.json")
	if err := os.WriteFile(brokenPath, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := execute([]string{"validate", "--input", brokenPath}); code != 4 {
		t.Errorf("a schema-invalid SPDX document = code %d, want 4", code)
	}
}

// TestValidateReadsAnSpdxDocumentItDidNotWrite: validate judges conformance,
// not whether sbomb wrote the document. An official example of the SPDX 3
// model repository -- written for 3.0.0, so its context and specVersion are
// moved to 3.0.1 first -- is valid; left as it is, it is refused by version.
func TestValidateReadsAnSpdxDocumentItDidNotWrite(t *testing.T) {
	raw := readFile(t, filepath.Join(testutil.RepoRoot(t), "testdata", "spdx", "examples", "spdx_document4.json"))
	directory := t.TempDir()
	original := filepath.Join(directory, "original.json")
	if err := os.WriteFile(original, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execute([]string{"validate", "--input", original})
	if code != 4 || !strings.Contains(stderr, "SPDX 3.0.0") {
		t.Errorf("a 3.0.0 example = code %d, stderr %q; want 4 naming SPDX 3.0.0", code, stderr)
	}

	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document["@context"] = "https://spdx.org/rdf/3.0.1/spdx-context.jsonld"
	var lift func(any)
	lift = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if typed["type"] == "CreationInfo" {
				typed["specVersion"] = "3.0.1"
			}
			for _, child := range typed {
				lift(child)
			}
		case []any:
			for _, child := range typed {
				lift(child)
			}
		}
	}
	lift(document)
	lifted, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "example.spdx.json")
	if err := os.WriteFile(path, lifted, 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := execute([]string{"validate", "--input", path})
	if code != 0 {
		t.Fatalf("validate an official example = code %d, stderr %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "valid SPDX 3.0.1 document: ") {
		t.Errorf("validate said %q", stdout)
	}
}

// TestValidateReportsWhyAnSpdxDocumentIsNotConformant: validate holds an SPDX
// document to more than the schema. A rule the schema cannot express -- here,
// one element defined twice -- is exit code 4 with the element named, as a
// schema violation is.
func TestValidateReportsWhyAnSpdxDocumentIsNotConformant(t *testing.T) {
	_, output := generateSpdx(t)
	broken := editSpdxGraph(t, readFile(t, output), func(graph []any) []any {
		for _, item := range graph {
			if node := item.(map[string]any); node["type"] == "software_Package" {
				copied := map[string]any{}
				for key, value := range node {
					copied[key] = value
				}
				return append(graph, copied)
			}
		}
		t.Fatal("the document has no package")
		return nil
	})
	path := filepath.Join(t.TempDir(), "duplicate.spdx.json")
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := execute([]string{"validate", "--input", path})
	if code != 4 || stdout != "" {
		t.Errorf("a non-conformant SPDX document = code %d, stdout %q; want 4 and nothing on stdout", code, stdout)
	}
	if !strings.Contains(stderr, "SPDX 3.0.1 conformance check failed") || !strings.Contains(stderr, "is defined twice") {
		t.Errorf("stderr = %q; want the conformance failure and the duplicate named", stderr)
	}
}

// TestValidateDoesNotHoldADocumentToHowSbombWrites: a document that refers to
// an element defined in another document is conformant SPDX 3.0.1, and
// validate says so, although sbomb itself never writes one (its own output is
// self-contained, and WriteFile would refuse it).
func TestValidateDoesNotHoldADocumentToHowSbombWrites(t *testing.T) {
	_, output := generateSpdx(t)
	external := editSpdxGraph(t, readFile(t, output), func(graph []any) []any {
		for _, item := range graph {
			if node := item.(map[string]any); node["relationshipType"] == "contains" {
				node["to"] = append(node["to"].([]any), "https://elsewhere.example/doc#file")
				return graph
			}
		}
		t.Fatal("the document has no contains relationship")
		return nil
	})
	path := filepath.Join(t.TempDir(), "external.spdx.json")
	if err := os.WriteFile(path, external, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := execute([]string{"validate", "--input", path}); code != 0 || !strings.HasPrefix(stdout, "valid SPDX 3.0.1 document: ") {
		t.Errorf("validate a document that refers outside itself = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// editSpdxGraph decodes an SPDX document, lets edit change its @graph, and
// encodes it again.
func editSpdxGraph(t *testing.T, data []byte, edit func([]any) []any) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	root["@graph"] = edit(root["@graph"].([]any))
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestSelfWritesSpdx: a release that publishes SPDX for its product can
// publish SPDX for the tool, with the Go module facts and the adapter that
// read them carried as sbomb properties.
func TestSelfWritesSpdx(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOURCE_DATE_EPOCH", pinnedEpoch)
	output := filepath.Join(t.TempDir(), "self.spdx.json")
	code, _, stderr := execute([]string{"self", executable, "--output", output, "--version", "9.9.9",
		"--format", "spdx-json", "--reproducible"})
	if code != 0 {
		t.Fatalf("self --format spdx-json = code %d, stderr %q", code, stderr)
	}
	data := readFile(t, output)
	if created := spdxCreationInfo(t, data)["created"]; created != pinnedCreated {
		t.Errorf("created = %v, want %s", created, pinnedCreated)
	}
	properties := extensionProperties(t, data)
	if got := properties["sbomb:run:adapters"]; len(got) != 1 || got[0] != "gobin" {
		t.Errorf("sbomb:run:adapters = %v, want [gobin]", got)
	}
	goProperties := 0
	for name := range properties {
		if strings.HasPrefix(name, "sbomb:go:") {
			goProperties++
		}
	}
	if goProperties == 0 {
		t.Error("no sbomb:go: property reached the SPDX document")
	}
	if code, stdout, stderr := execute([]string{"validate", "--input", output}); code != 0 || !strings.HasPrefix(stdout, "valid SPDX 3.0.1") {
		t.Errorf("validate the self document = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	t.Setenv("SOURCE_DATE_EPOCH", "")
	code, _, stderr = execute([]string{"self", executable, "--output", output, "--format", "spdx-json", "--reproducible"})
	if code != 1 || !strings.Contains(stderr, "REPRODUCIBLE_CREATION_TIME_MISSING") {
		t.Errorf("self without SOURCE_DATE_EPOCH = code %d, stderr %q", code, stderr)
	}
}

// TestExplainGivesTheSameAnswerForEveryNameInEitherFormat: the same question
// about the same run gets the same answer, refusal included, whichever format
// the document is in. The names cover every kind of element: the product, which
// CycloneDX states as metadata and not as a component; an artifact; a grouping
// component, by name and by local identity; a used file, by base name and by
// identity; a file that is only licence evidence; and a name nothing has.
func TestExplainGivesTheSameAnswerForEveryNameInEitherFormat(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", pinnedEpoch)
	buildDir, cyclone := explainFixture(t)
	spdxPath := filepath.Join(t.TempDir(), "app.spdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
		"--source-dir", testutil.CorpusSourceTree(t), "--policy", "lenient",
		"--output", spdxPath, "--reproducible", "--format", "spdx-json"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	var metadata struct {
		Metadata struct {
			Component struct {
				Name string `json:"name"`
			} `json:"component"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(readFile(t, cyclone), &metadata); err != nil {
		t.Fatal(err)
	}
	answer := func(document, name string) string {
		code, out, stderr := execute([]string{"explain", "--build-dir", buildDir, "--sbom", document, "--component", name})
		return fmt.Sprintf("code %d\n%s\n%s", code, out, strings.ReplaceAll(stderr, document, "<document>"))
	}
	for _, name := range []string{
		metadata.Metadata.Component.Name, "fossapp", "artifact:build:fossapp", "mit-lib", "component:mit-lib",
		"mit_a.c", "file:project:dep/mit-lib/src/mit_a.c", "LICENSE", "no-such-lib",
	} {
		if fromCycloneDX, fromSpdx := answer(cyclone, name), answer(spdxPath, name); fromCycloneDX != fromSpdx {
			t.Errorf("%q is answered differently:\nCycloneDX: %s\nSPDX: %s", name, fromCycloneDX, fromSpdx)
		}
	}
}

// TestExplainReadsComponentFilesFromAnSpdxDocument: which files a component
// groups is read from whichever document the user names, and the answer does
// not depend on its format. A file's SPDX IRI is accepted wherever its bom-ref
// is, since that is what a reader of an SPDX document has to copy.
func TestExplainReadsComponentFilesFromAnSpdxDocument(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", pinnedEpoch)
	buildDir, cyclone := explainFixture(t)
	spdxPath := filepath.Join(t.TempDir(), "app.spdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
		"--source-dir", testutil.CorpusSourceTree(t), "--policy", "lenient",
		"--output", spdxPath, "--reproducible", "--format", "spdx-json"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}

	explain := func(args ...string) string {
		t.Helper()
		code, out, stderr := execute(append([]string{"explain", "--build-dir", buildDir}, args...))
		if code != 0 || stderr != "" {
			t.Fatalf("explain %v = code %d, stderr %q", args, code, stderr)
		}
		return out
	}
	fromCycloneDX := explain("--sbom", cyclone, "--component", "mit-lib")
	fromSpdx := explain("--sbom", spdxPath, "--component", "mit-lib")
	if fromSpdx != fromCycloneDX {
		t.Errorf("the SPDX document answers differently:\nSPDX:\n%s\nCycloneDX:\n%s", fromSpdx, fromCycloneDX)
	}
	if !strings.Contains(fromSpdx, "project:dep/mit-lib/src/mit_a.c") {
		t.Errorf("the answer does not name the component's file:\n%s", fromSpdx)
	}

	code, _, stderr = execute([]string{"explain", "--build-dir", buildDir, "--sbom", spdxPath, "--component", "no-such-lib"})
	if code != 1 || !strings.Contains(stderr, "names no component no-such-lib") {
		t.Errorf("an unknown component = code %d, stderr %q", code, stderr)
	}

	var document struct {
		Graph []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal(readFile(t, spdxPath), &document); err != nil {
		t.Fatal(err)
	}
	iri := ""
	for _, node := range document.Graph {
		if id, _ := node["spdxId"].(string); node["type"] == "software_File" && strings.HasSuffix(id, "#file:project:dep/mit-lib/src/mit_a.c") {
			iri = id
		}
	}
	if iri == "" {
		t.Fatal("the SPDX document has no software_File for mit_a.c")
	}
	if byIRI, byRef := explain("--bom-ref", iri), explain("--bom-ref", "file:project:dep/mit-lib/src/mit_a.c"); byIRI != byRef {
		t.Errorf("the IRI is answered differently from the bom-ref:\n%s\n%s", byIRI, byRef)
	}
}

// TestALocalIdentityIsReadBackFromAnSpdxIRI covers the decoding on its own:
// the fragment is percent-decoded, and anything that is not an SPDX element
// IRI is returned as typed.
func TestALocalIdentityIsReadBackFromAnSpdxIRI(t *testing.T) {
	for input, want := range map[string]string{
		"urn:uuid:782f81fc-5138-5883-8b0a-38f1756e5e91#file:project:a%20b.c": "file:project:a b.c",
		"urn:uuid:782f81fc-5138-5883-8b0a-38f1756e5e91#file:project:100%":    "file:project:100%",
		"file:project:main.c":       "file:project:main.c",
		"urn:uuid:no-fragment-here": "urn:uuid:no-fragment-here",
		"component:zlib@1.3#frag":   "component:zlib@1.3#frag",
	} {
		if got := localIdentity(input); got != want {
			t.Errorf("localIdentity(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestFossReviewStaysCycloneDXWhenTheSbomIsSpdx: foss-review.json is the
// CycloneDX review record whatever the SBOM is written in, and the SPDX
// version the configuration names for the SBOM does not reach it. It is the
// same file a CycloneDX run of the same evidence writes.
func TestFossReviewStaysCycloneDXWhenTheSbomIsSpdx(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", pinnedEpoch)
	directory := t.TempDir()
	configPath := filepath.Join(directory, "sbomb.json")
	if err := os.WriteFile(configPath, []byte(`{"schemaVersion": 1,
  "output": {"format": "spdx-json", "specVersion": "3.0.1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(name string, extra ...string) []byte {
		t.Helper()
		out := filepath.Join(directory, name)
		args := append([]string{"generate", "--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
			"--source-dir", testutil.CorpusSourceTree(t), "--policy", "lenient", "--reproducible",
			"--output", filepath.Join(directory, name+".json"), "--foss-out", out}, extra...)
		code, _, stderr := execute(args)
		if code != 0 || stderr != "" {
			t.Fatalf("generate %v = code %d, stderr %q", extra, code, stderr)
		}
		return readFile(t, filepath.Join(out, "foss-review.json"))
	}
	fromSpdx := run("spdx", "--config", configPath)
	review := parseDocument(t, fromSpdx)
	if review["bomFormat"] != "CycloneDX" || review["specVersion"] != "1.6" {
		t.Errorf("foss-review.json of an SPDX run is %v %v, want CycloneDX 1.6", review["bomFormat"], review["specVersion"])
	}
	if fromCycloneDX := run("cyclonedx"); !bytes.Equal(fromSpdx, fromCycloneDX) {
		t.Error("foss-review.json differs between an SPDX and a CycloneDX run of the same evidence")
	}

	// The command `foss` takes the same rule.
	out := filepath.Join(directory, "foss-command")
	code, _, stderr := execute([]string{"foss", "--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
		"--source-dir", testutil.CorpusSourceTree(t), "--config", configPath, "--policy", "lenient", "--reproducible", "--out", out})
	if code != 0 || stderr != "" {
		t.Fatalf("foss = code %d, stderr %q", code, stderr)
	}
	if !bytes.Equal(readFile(t, filepath.Join(out, "foss-review.json")), fromSpdx) {
		t.Error("foss and generate --foss-out wrote different review records for an SPDX configuration")
	}
}

// TestFossIsReproducibleWithoutSourceDateEpochWhenTheSbomIsSpdx: the foss
// command writes no SBOM, and nothing it writes states a creation time, so an
// SPDX configuration does not make --reproducible need SOURCE_DATE_EPOCH --
// which only the SPDX document would. The run writes the same review record
// as the same command under a CycloneDX configuration.
func TestFossIsReproducibleWithoutSourceDateEpochWhenTheSbomIsSpdx(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "")
	directory := t.TempDir()
	configPath := filepath.Join(directory, "sbomb.json")
	if err := os.WriteFile(configPath, []byte(`{"schemaVersion": 1,
  "output": {"format": "spdx-json", "specVersion": "3.0.1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(name string, extra ...string) string {
		t.Helper()
		out := filepath.Join(directory, name)
		args := append([]string{"foss", "--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
			"--source-dir", testutil.CorpusSourceTree(t), "--policy", "lenient", "--reproducible", "--out", out}, extra...)
		code, _, stderr := execute(args)
		if code != 0 || stderr != "" {
			t.Fatalf("foss %v = code %d, stderr %q", extra, code, stderr)
		}
		return out
	}
	fromSpdx := run("spdx", "--config", configPath)
	fromCycloneDX := run("cyclonedx")
	for _, name := range []string{"THIRD-PARTY-NOTICES.txt", "foss-review.json", "foss-review.txt", "source-obligations.txt"} {
		if !bytes.Equal(readFile(t, filepath.Join(fromSpdx, name)), readFile(t, filepath.Join(fromCycloneDX, name))) {
			t.Errorf("%s differs between an SPDX and a CycloneDX configuration", name)
		}
	}
	// The configured format is still a configuration the build must be able
	// to honour: a version the SPDX writer does not emit is a usage error,
	// and it is the writer that says so. 1.7 is a version the configuration
	// accepts -- CycloneDX has it -- so only pairing the format with the
	// version through the writer refuses it.
	bad := filepath.Join(directory, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"schemaVersion": 1, "output": {"format": "spdx-json", "specVersion": "1.7"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execute([]string{"foss", "--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
		"--config", bad, "--out", filepath.Join(directory, "bad")})
	if code != 1 || !strings.Contains(stderr, `format "spdx-json" does not support version "1.7"`) {
		t.Errorf("an SPDX version no writer emits: code %d, stderr %q", code, stderr)
	}
}

// TestSchemaCommandServesTheSpdxSchema: the schema an SPDX document is checked
// against is printable like the CycloneDX ones, --cyclonedx stays an alias of
// --format cyclonedx-json, and a version or pair the format does not have is a
// usage error.
func TestSchemaCommandServesTheSpdxSchema(t *testing.T) {
	for _, args := range [][]string{
		{"schema", "--format", "spdx-json"},
		{"schema", "--format=spdx-json", "--spec-version", "3.0.1"},
	} {
		code, stdout, stderr := execute(args)
		if code != 0 {
			t.Fatalf("%v = code %d, stderr %q", args, code, stderr)
		}
		var schema map[string]any
		if err := json.Unmarshal([]byte(stdout), &schema); err != nil {
			t.Fatalf("%v printed no JSON: %v", args, err)
		}
		if !strings.Contains(stdout, `"https://spdx.org/rdf/3.0.1/spdx-context.jsonld"`) {
			t.Errorf("%v is not the SPDX 3.0.1 schema", args)
		}
	}
	_, alias, _ := execute([]string{"schema", "--cyclonedx", "--spec-version", "1.7"})
	_, named, _ := execute([]string{"schema", "--format", "cyclonedx-json", "--spec-version", "1.7"})
	if alias == "" || alias != named {
		t.Error("--cyclonedx is no longer an alias of --format cyclonedx-json")
	}
	// A conflict repeats what the user typed, in the order it was typed.
	for _, conflict := range []struct {
		args []string
		want string
	}{
		{[]string{"schema", "--cyclonedx", "--format", "spdx-json"}, "--cyclonedx and --format spdx-json name two different formats\n"},
		{[]string{"schema", "--format", "spdx-json", "--cyclonedx"}, "--format spdx-json and --cyclonedx name two different formats\n"},
		{[]string{"schema", "--format=spdx-json", "--format", "cyclonedx-json"}, "--format spdx-json and --format cyclonedx-json name two different formats\n"},
	} {
		if code, _, stderr := execute(conflict.args); code != 1 || stderr != conflict.want {
			t.Errorf("%v = code %d, stderr %q, want %q", conflict.args, code, stderr, conflict.want)
		}
	}
	for _, args := range [][]string{
		{"schema", "--format", "spdx-json", "--spec-version", "1.6"},
		{"schema", "--cyclonedx", "--format", "spdx-json"},
		{"schema", "--format", "spdx-tag-value"},
		{"schema", "--spec-version", "3.0.1"},
	} {
		if code, _, _ := execute(args); code != 1 {
			t.Errorf("%v = code %d, want 1", args, code)
		}
	}
}

// TestBothReadersExpandEveryComponentAlike: explain --component reads the
// files of a component out of whichever document it is given, and the two
// readers find a component by different means -- CycloneDX by bom-ref and
// name, SPDX by IRI fragment, package name and the base name of a used file.
// The corpus fixture explain is tested on has no file directly under an
// anchor, whose base name is what follows the anchor's colon rather than a
// slash. So the synthetic document is given one, rendered by both writers,
// and every name and reference the CycloneDX document carries must be
// answered alike.
func TestBothReadersExpandEveryComponentAlike(t *testing.T) {
	document := sbomwritertest.AllFields()
	mainFile := domain.UsedFile{
		ID:    domain.FileID{Anchor: "project", RelPath: "main.c"},
		Class: domain.FileClassSource,
		Properties: map[string][]string{
			"sbomb:component:scope":       {"project"},
			"sbomb:file:distributionRole": {domain.RoleDistributed},
		},
	}
	document.Files = append(document.Files, mainFile)
	render := func(id, version string) (sbomwriter.Writer, []byte) {
		t.Helper()
		writer, err := sbomwriter.Get(id, version)
		if err != nil {
			t.Fatal(err)
		}
		options := sbomwritertest.AllFieldsOptions()
		options.SpecVersion = version
		var buffer bytes.Buffer
		if err := writer.Write(&buffer, document, options); err != nil {
			t.Fatal(err)
		}
		return writer, buffer.Bytes()
	}
	cdxWriter, cdx := render("cyclonedx-json", "1.6")
	spdxWriter, spdx := render("spdx-json", "3.0.1")
	reader := func(writer sbomwriter.Writer) sbomwriter.ComponentReader {
		t.Helper()
		reader, ok := writer.(sbomwriter.ComponentReader)
		if !ok {
			t.Fatalf("the %s writer does not read component files", writer.ID())
		}
		return reader
	}
	answer := func(writer sbomwriter.Writer, data []byte, name string) string {
		files, err := reader(writer).ComponentFiles(data, name)
		sort.Strings(files)
		return fmt.Sprintf("%v %v", files, err)
	}

	var parsed struct {
		Components []struct {
			BomRef string `json:"bom-ref"`
			Name   string `json:"name"`
		} `json:"components"`
	}
	if err := json.Unmarshal(cdx, &parsed); err != nil {
		t.Fatal(err)
	}
	names := []string{"main.c"}
	for _, component := range parsed.Components {
		names = append(names, component.Name, component.BomRef)
	}
	for _, name := range names {
		if fromCycloneDX, fromSpdx := answer(cdxWriter, cdx, name), answer(spdxWriter, spdx, name); fromCycloneDX != fromSpdx {
			t.Errorf("%q is answered differently:\nCycloneDX: %s\nSPDX:      %s", name, fromCycloneDX, fromSpdx)
		}
	}
	// The bare name must find the file, or agreeing on it proves nothing.
	if got := answer(cdxWriter, cdx, "main.c"); got != "[] <nil>" {
		t.Errorf("CycloneDX answers %q with %s; want the file, which groups nothing", "main.c", got)
	}
}
