package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/testutil"
)

// generateAt writes the Make corpus build at one specification version and
// returns the bytes. An empty version exercises the default path.
func generateAt(t *testing.T, specVersion string) []byte {
	t.Helper()
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	output := filepath.Join(t.TempDir(), "out.cdx.json")
	args := []string{"generate", "--build-dir", buildDir, "--policy", "lenient", "--output", output, "--reproducible"}
	if specVersion != "" {
		args = append(args, "--spec-version", specVersion)
	}
	code, _, stderr := execute(args)
	if code != 0 || stderr != "" {
		t.Fatalf("generate at %q = code %d, stderr %q", specVersion, code, stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestDefaultSpecVersionIsUnchanged is the regression this feature must not
// cause. TestMilestone17MakefilesAcceptance already compares the default
// output against the 1.6 golden; this says why that golden may not move, and
// pins that saying "1.6" explicitly is the same thing as saying nothing.
func TestDefaultSpecVersionIsUnchanged(t *testing.T) {
	byDefault := generateAt(t, "")
	explicit := generateAt(t, "1.6")
	if !bytes.Equal(byDefault, explicit) {
		t.Fatal("--spec-version=1.6 produced a different document from the default")
	}
	assertGolden(t, "gcc-make-p02.cdx.json", byDefault)
}

// TestSpecVersion17Golden records what 1.7 output looks like, so that a change
// to it is a reviewed diff rather than a surprise.
func TestSpecVersion17Golden(t *testing.T) {
	assertGolden(t, "gcc-make-p02-1.7.cdx.json", generateAt(t, "1.7"))
}

// TestBothVersionsAreDeterministic is the determinism check of section 29 run
// for each version: the same evidence, the same bytes.
func TestBothVersionsAreDeterministic(t *testing.T) {
	for _, specVersion := range []string{"1.6", "1.7"} {
		t.Run(specVersion, func(t *testing.T) {
			first := generateAt(t, specVersion)
			second := generateAt(t, specVersion)
			if !bytes.Equal(first, second) {
				t.Fatal("a reproducible document changed between runs")
			}
		})
	}
}

// TestTheTwoVersionsDifferOnlyInSayingSo holds §28.1 against this corpus: a
// document may not change shape with the version beyond what the version
// requires.
//
// Four 1.7-only differences are permitted and documented in §28.1, and this
// build produces none of them here: the corpus has no compound licence
// expression, no dynamically linked system library, no package-manager
// repository record and no configured TLP. So for this corpus the two
// documents must still differ only in saying which version they are, and a new
// difference appearing is either a bug or an undocumented decision.
//
// Three things legitimately differ: specVersion, the property that records it,
// and the serial number -- which is a digest of the canonical document and so
// must differ once anything in it does.
func TestTheTwoVersionsDifferOnlyInSayingSo(t *testing.T) {
	at16 := parseDocument(t, generateAt(t, "1.6"))
	at17 := parseDocument(t, generateAt(t, "1.7"))

	if at16["specVersion"] != "1.6" || at17["specVersion"] != "1.7" {
		t.Fatalf("specVersion = %v and %v", at16["specVersion"], at17["specVersion"])
	}
	if at16["serialNumber"] == at17["serialNumber"] {
		t.Error("the two documents share a serial number; the version is part of the document's identity")
	}

	for _, document := range []map[string]any{at16, at17} {
		delete(document, "specVersion")
		delete(document, "serialNumber")
		stripRunSpecVersion(t, document)
	}
	if !reflect.DeepEqual(at16, at17) {
		t.Error("the 1.7 document differs from the 1.6 document by more than the version")
	}
}

// TestValidateReadsADocumentItDidNotWrite pins both halves of the validate
// gap: the format is detected rather than assumed, and the schema is chosen by
// what the document claims to be. The document carries component.isExternal,
// which exists at 1.7 and not at 1.6 -- so relabelling it 1.6 must fail, and
// that failure is the proof that the choice is real.
func TestValidateReadsADocumentItDidNotWrite(t *testing.T) {
	const foreign17 = `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.7",
  "version": 1,
  "serialNumber": "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79",
  "metadata": {
    "timestamp": "2026-01-01T00:00:00Z",
    "tools": [{"name": "some-other-tool", "version": "1.0.0"}],
    "component": {"type": "application", "name": "product", "bom-ref": "product:app"}
  },
  "components": [
    {"type": "library", "name": "libc", "bom-ref": "component:libc", "isExternal": true}
  ],
  "dependencies": [
    {"ref": "product:app", "dependsOn": ["component:libc"]},
    {"ref": "component:libc"}
  ]
}
`
	directory := t.TempDir()
	path := filepath.Join(directory, "foreign.cdx.json")
	if err := os.WriteFile(path, []byte(foreign17), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := execute([]string{"validate", "--input", path})
	if code != 0 {
		t.Fatalf("validate a 1.7 document = code %d, stderr %q", code, stderr)
	}
	if want := "valid CycloneDX 1.7 document: "; !bytes.Contains([]byte(stdout), []byte(want)) {
		t.Errorf("validate said %q, want it to name the version it read", stdout)
	}

	// The same document, claiming to be 1.6. isExternal does not exist there
	// and components forbid unknown properties, so the schema must reject it.
	mislabelled := bytes.Replace([]byte(foreign17), []byte(`"specVersion": "1.7"`), []byte(`"specVersion": "1.6"`), 1)
	mislabelledPath := filepath.Join(directory, "mislabelled.cdx.json")
	if err := os.WriteFile(mislabelledPath, mislabelled, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := execute([]string{"validate", "--input", mislabelledPath}); code != 4 {
		t.Errorf("a 1.7-only field labelled 1.6 = code %d, want 4", code)
	}

	// A version this build has no schema for is refused, not guessed at.
	older := bytes.Replace([]byte(foreign17), []byte(`"specVersion": "1.7"`), []byte(`"specVersion": "1.4"`), 1)
	olderPath := filepath.Join(directory, "older.cdx.json")
	if err := os.WriteFile(olderPath, older, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := execute([]string{"validate", "--input", olderPath}); code != 4 {
		t.Errorf("CycloneDX 1.4 = code %d, want 4", code)
	}

	// A document in no format this build knows names what it can read rather
	// than failing as though it were a broken CycloneDX file.
	otherPath := filepath.Join(directory, "other.json")
	if err := os.WriteFile(otherPath, []byte(`{"spdxVersion":"SPDX-2.3"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = execute([]string{"validate", "--input", otherPath})
	if code != 4 {
		t.Errorf("an SPDX document = code %d, want 4", code)
	}
	if !bytes.Contains([]byte(stderr), []byte("cyclonedx-json")) {
		t.Errorf("the error does not say what can be read: %q", stderr)
	}
}

// TestSpecVersionFlagRejectsWhatNoWriterEmits pins that an unsupported version
// is a usage error before any work happens, not a failure at the last step.
func TestSpecVersionFlagRejectsWhatNoWriterEmits(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	output := filepath.Join(t.TempDir(), "out.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--output", output, "--spec-version", "1.5"})
	if code != 1 {
		t.Fatalf("--spec-version=1.5 = code %d, want 1", code)
	}
	if !bytes.Contains([]byte(stderr), []byte("1.6, 1.7")) {
		t.Errorf("the error does not name the supported versions: %q", stderr)
	}
	if _, err := os.Stat(output); err == nil {
		t.Error("a rejected version still wrote a document")
	}
}

// TestSelfHonoursTheSpecVersion: a release attaches a self-SBOM to the binary
// it publishes, so the same choice has to be available there.
func TestSelfHonoursTheSpecVersion(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "self.cdx.json")
	code, _, stderr := execute([]string{"self", executable,
		"--output", output, "--version", "9.9.9", "--spec-version", "1.7", "--reproducible"})
	if code != 0 {
		t.Fatalf("self --spec-version 1.7 = code %d, stderr %q", code, stderr)
	}
	if got := parseDocument(t, readFile(t, output))["specVersion"]; got != "1.7" {
		t.Errorf("specVersion = %v, want 1.7", got)
	}
	if code, _, _ := execute([]string{"self", executable, "--output", output, "--spec-version", "1.5"}); code != 1 {
		t.Errorf("self --spec-version 1.5 = code %d, want 1", code)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestSchemaCommandServesEitherVersion: which schema a document is checked
// against is a question a user has to be able to answer for either version.
func TestSchemaCommandServesEitherVersion(t *testing.T) {
	for _, testCase := range []struct{ args, want string }{
		{"", "bom-1.6.schema.json"},
		{"1.6", "bom-1.6.schema.json"},
		{"1.7", "bom-1.7.schema.json"},
	} {
		args := []string{"schema", "--cyclonedx"}
		if testCase.args != "" {
			args = append(args, "--spec-version="+testCase.args)
		}
		code, stdout, stderr := execute(args)
		if code != 0 {
			t.Fatalf("schema %v = code %d, stderr %q", args, code, stderr)
		}
		if !bytes.Contains([]byte(stdout), []byte(testCase.want)) {
			t.Errorf("schema %v did not serve %s", args, testCase.want)
		}
	}
	if code, _, _ := execute([]string{"schema", "--cyclonedx", "--spec-version=1.5"}); code != 1 {
		t.Error("schema served a version this build has no schema for")
	}
}

// TestConfigEnumsMatchTheWriter holds the configuration's published enum and
// the writer registry together. The loader cannot ask the registry without
// depending on a serializer, so the two are written apart; this is what stops
// them drifting.
func TestConfigEnumsMatchTheWriter(t *testing.T) {
	var published map[string]any
	if err := json.Unmarshal([]byte(config.Schema()), &published); err != nil {
		t.Fatal(err)
	}
	output, ok := published["properties"].(map[string]any)["output"].(map[string]any)
	if !ok {
		t.Fatal("the published schema has no output section")
	}
	properties := output["properties"].(map[string]any)

	writer, _, err := sbomwriter.Resolve("cyclonedx-json", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := enumOf(t, properties, "specVersion"); !reflect.DeepEqual(got, writer.Versions()) {
		t.Errorf("output.specVersion publishes %v but the writer emits %v", got, writer.Versions())
	}
	if got := enumOf(t, properties, "format"); !reflect.DeepEqual(got, sbomwriter.IDs()) {
		t.Errorf("output.format publishes %v but the registry holds %v", got, sbomwriter.IDs())
	}
	// Every published version must actually have a schema to check against.
	for _, version := range enumOf(t, properties, "specVersion") {
		if _, err := cyclonedx.EmbeddedSchema(version); err != nil {
			t.Errorf("output.specVersion offers %s with no embedded schema: %v", version, err)
		}
	}
}

func enumOf(t *testing.T, properties map[string]any, name string) []string {
	t.Helper()
	field, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("the published schema has no output.%s", name)
	}
	raw, ok := field["enum"].([]any)
	if !ok {
		t.Fatalf("output.%s publishes no enum", name)
	}
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		values = append(values, value.(string))
	}
	return values
}

func parseDocument(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

// stripRunSpecVersion removes the run property that records the specification
// version, which differs for the same reason specVersion does.
func stripRunSpecVersion(t *testing.T, document map[string]any) {
	t.Helper()
	metadata, ok := document["metadata"].(map[string]any)
	if !ok {
		t.Fatal("the document has no metadata")
	}
	properties, ok := metadata["properties"].([]any)
	if !ok {
		t.Fatal("the document records no run properties")
	}
	kept := make([]any, 0, len(properties))
	found := false
	for _, entry := range properties {
		property := entry.(map[string]any)
		if property["name"] == "sbomb:run:specVersion" {
			found = true
			continue
		}
		kept = append(kept, entry)
	}
	if !found {
		t.Error("the document does not record which specification version wrote it")
	}
	metadata["properties"] = kept
}
