package spdx

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/sbommap"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// write renders a document through the registered writer and holds the result
// to both tiers, as WriteFile does: every document a test here writes is one
// sbomb could have put on disk.
func write(t *testing.T, document *sbomwriter.Document, options sbomwriter.Options) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := (Writer{}).Write(&buffer, document, options); err != nil {
		t.Fatal(err)
	}
	if err := (Writer{}).Validate(bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("the written document is not conformant: %v", err)
	}
	if err := (Writer{}).CheckOutput(buffer.Bytes()); err != nil {
		t.Fatalf("the written document breaks an invariant of sbomb's own output: %v", err)
	}
	return buffer.Bytes()
}

var namespacePattern = regexp.MustCompile(`urn:uuid:([0-9a-f-]{36})#`)

func namespaceOf(t *testing.T, data []byte) string {
	t.Helper()
	match := namespacePattern.FindSubmatch(data)
	if match == nil {
		t.Fatal("the document has no namespace")
	}
	return string(match[1])
}

func TestTheWriterIsRegisteredAsSpdxJson(t *testing.T) {
	writer, version, err := sbomwriter.Resolve("spdx-json", "")
	if err != nil || writer.ID() != ID || version != Version301 {
		t.Fatalf("Resolve = %v %q %v", writer, version, err)
	}
	if _, err := sbomwriter.Get("spdx-json", "2.3"); err == nil {
		t.Error("SPDX 2.3 is not written by this build")
	}
	var _ sbomwriter.Detector = Writer{}
	var _ sbomwriter.Describer = Writer{}
	var _ sbomwriter.SchemaProvider = Writer{}
	var _ sbomwriter.Preflighter = Writer{}
	var _ sbomwriter.ComponentReader = Writer{}
	var _ sbomwriter.OutputChecker = Writer{}
	if (Writer{}).Label() != "SPDX" || (Writer{}).Extension() != ".spdx.json" {
		t.Error("label or extension")
	}
	if (Writer{}).OmitsTimestamp(Version301, sbomwriter.Options{Reproducible: true}) {
		t.Error("an SPDX document always states its creation time")
	}
}

func TestDetectRecognisesTheContext(t *testing.T) {
	cases := map[string]string{
		`{"@context": "https://spdx.org/rdf/3.0.1/spdx-context.jsonld", "@graph": []}`:                "3.0.1",
		`{"@context": ["https://spdx.org/rdf/3.0.0/spdx-context.jsonld", {"x": "y#"}], "@graph": []}`: "3.0.0",
	}
	for document, want := range cases {
		if version, ok := (Writer{}).Detect([]byte(document)); !ok || version != want {
			t.Errorf("Detect(%s) = %q %v, want %q", document, version, ok, want)
		}
	}
	for _, other := range []string{`{"bomFormat": "CycloneDX", "specVersion": "1.6"}`, `{"@context": "https://schema.org"}`, `[]`, `not json`} {
		if _, ok := (Writer{}).Detect([]byte(other)); ok {
			t.Errorf("%s was taken for SPDX", other)
		}
	}
	data := write(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions())
	writer, version, err := sbomwriter.DetectFormat(data)
	if err != nil || writer.ID() != ID || version != Version301 {
		t.Errorf("DetectFormat = %v %q %v", writer, version, err)
	}
}

func TestDetectReportsAnSpdx2DocumentSoValidateCanRefuseIt(t *testing.T) {
	document := []byte(`{"spdxVersion": "SPDX-2.3", "SPDXID": "SPDXRef-DOCUMENT"}`)
	if version, ok := (Writer{}).Detect(document); !ok || version != "2.3" {
		t.Fatalf("Detect = %q %v", version, ok)
	}
	err := (Writer{}).Validate(bytes.NewReader(document))
	if err == nil || !strings.Contains(err.Error(), "SPDX 2.3 documents are not read by this build") {
		t.Errorf("Validate = %v", err)
	}
}

// TestAVersionIsOneLineOfTheDispatchTable holds the package's promise that
// adding a version is a renderer, its checks and one line of the table: the
// versions the writer lists are the table's, and a document of a version the
// table holds is read by that version's renderer -- an SPDX 2 version too,
// which is otherwise refused before the table is asked.
func TestAVersionIsOneLineOfTheDispatchTable(t *testing.T) {
	t.Cleanup(func() { delete(renderers, "2.3") })
	if got := (Writer{}).Versions(); strings.Join(got, ",") != Version301 {
		t.Fatalf("Versions = %v, want the one version of the table", got)
	}
	refused := (Writer{}).Validate(strings.NewReader(`{"spdxVersion": "SPDX-2.3"}`))
	if refused == nil || !strings.Contains(refused.Error(), "SPDX 2.3 documents are not read by this build") {
		t.Fatalf("without a 2.3 renderer, Validate = %v", refused)
	}
	called := false
	renderers["2.3"] = renderer{validate: func([]byte) error { called = true; return nil }}
	if got := (Writer{}).Versions(); strings.Join(got, ",") != "2.3,"+Version301 {
		t.Errorf("Versions = %v, want the table's versions in order", got)
	}
	if err := (Writer{}).Validate(strings.NewReader(`{"spdxVersion": "SPDX-2.3"}`)); err != nil || !called {
		t.Errorf("a 2.3 document was not handed to the 2.3 renderer: %v (called %v)", err, called)
	}
}

func TestValidateNamesAnotherSpdx3Version(t *testing.T) {
	err := (Writer{}).Validate(strings.NewReader(`{"@context": "https://spdx.org/rdf/3.0.0/spdx-context.jsonld", "@graph": []}`))
	if err == nil || !strings.Contains(err.Error(), "the document is SPDX 3.0.0") {
		t.Errorf("Validate = %v", err)
	}
}

func TestTheReproducibleNamespaceIsTheDigestOfTheModel(t *testing.T) {
	data := write(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions())
	model, err := mapping.Build(sbomwritertest.AllFields(), mapping.Options{
		SpecVersion: Version301, LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true, CustomAdditions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := namespaceOf(t, data), sbommap.DocumentUUID(mapping.Digest(model)); got != want {
		t.Errorf("namespace %s, want %s", got, want)
	}
}

// TestTheNamespaceFollowsSourceDateEpoch: the creation time is part of what
// the identity is derived from, so the same evidence pinned to another time is
// another document -- and two documents never share element IRIs while
// disagreeing about when those elements were created.
func TestTheNamespaceFollowsSourceDateEpoch(t *testing.T) {
	first := write(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions())
	again := write(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions())
	if !bytes.Equal(first, again) {
		t.Fatal("the same evidence at the same time gave two documents")
	}
	later := sbomwritertest.AllFields()
	later.Run.Timestamp = "2024-02-02T00:00:00Z"
	if namespaceOf(t, write(t, later, sbomwritertest.AllFieldsOptions())) == namespaceOf(t, first) {
		t.Error("another creation time kept the identity")
	}
}

func TestTwoRunsWithoutReproducibleDifferOnlyInNamespaceAndCreated(t *testing.T) {
	options := sbomwritertest.AllFieldsOptions()
	options.Reproducible = false
	firstDocument := sbomwritertest.AllFields()
	secondDocument := sbomwritertest.AllFields()
	secondDocument.Run.Timestamp = "2025-05-05T05:05:05Z"
	first := write(t, firstDocument, options)
	second := write(t, secondDocument, options)
	normalise := func(data []byte) string {
		text := strings.ReplaceAll(string(data), namespaceOf(t, data), "NAMESPACE")
		return regexp.MustCompile(`"created": "[^"]*"`).ReplaceAllString(text, `"created": "TIME"`)
	}
	if namespaceOf(t, first) == namespaceOf(t, second) {
		t.Error("two documents that are not reproducible share an identity")
	}
	if normalise(first) != normalise(second) {
		t.Error("two runs differ in more than their identity and creation time")
	}
}

func TestTLPIsRefused(t *testing.T) {
	err := (Writer{}).Write(&bytes.Buffer{}, sbomwritertest.AllFields(), sbomwriter.Options{TLP: "AMBER", Reproducible: true})
	if err == nil || !strings.Contains(err.Error(), "output.tlp cannot be carried by SPDX 3.0.1") {
		t.Errorf("Write = %v", err)
	}
	if err := (Writer{}).Preflight(Version301, sbomwriter.Options{TLP: "CLEAR"}); err == nil || !strings.Contains(err.Error(), "write CycloneDX 1.7 to carry it") {
		t.Errorf("Preflight = %v", err)
	}
}

func TestPreflightRefusesReproducibleWithoutSourceDateEpoch(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "")
	var refusal *sbomwriter.RefusalError
	err := (Writer{}).Preflight(Version301, sbomwriter.Options{Reproducible: true})
	if !errors.As(err, &refusal) || refusal.ID != "REPRODUCIBLE_CREATION_TIME_MISSING" {
		t.Fatalf("Preflight = %v", err)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "yesterday")
	if err := (Writer{}).Preflight(Version301, sbomwriter.Options{Reproducible: true}); !errors.As(err, &refusal) || !strings.Contains(err.Error(), "yesterday") {
		t.Errorf("an unreadable value = %v", err)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	if err := (Writer{}).Preflight(Version301, sbomwriter.Options{Reproducible: true}); err != nil {
		t.Errorf("a pinned time = %v", err)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "")
	if err := (Writer{}).Preflight(Version301, sbomwriter.Options{}); err != nil {
		t.Errorf("without --reproducible the clock is a creation time: %v", err)
	}
	document := sbomwritertest.AllFields()
	document.Run.Timestamp = ""
	if err := (Writer{}).Write(&bytes.Buffer{}, document, sbomwriter.Options{Reproducible: true}); !errors.Is(err, mapping.ErrNoCreationTime) {
		t.Errorf("Write without a time = %v", err)
	}
}

// TestValidateDoesNotApplyTheOutputInvariants: a conformant document that is
// not the way sbomb writes passes validate, and is refused on the way out.
func TestValidateDoesNotApplyTheOutputInvariants(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(write(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions()), &root); err != nil {
		t.Fatal(err)
	}
	for _, item := range root["@graph"].([]any) {
		node := item.(map[string]any)
		if node["relationshipType"] == "contains" {
			node["to"] = append(node["to"].([]any), "https://elsewhere.example/doc#file")
			break
		}
	}
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Writer{}).Validate(bytes.NewReader(data)); err != nil {
		t.Fatalf("validate refused a conformant document: %v", err)
	}
	if err := (Writer{}).CheckOutput(data); err == nil {
		t.Fatal("a reference outside the document passed sbomb's own invariants")
	}
	path := filepath.Join(t.TempDir(), "out.spdx.json")
	if err := sbomwriter.WriteFile(path, Writer{}, data); err == nil {
		t.Fatal("WriteFile wrote a document that breaks the invariants")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a refused document appeared at the output path")
	}
}

func TestEmbeddedSchemaIsServedPerVersion(t *testing.T) {
	schema, err := (Writer{}).EmbeddedSchema("")
	if err != nil || !bytes.Contains(schema, []byte(`"const": "https://spdx.org/rdf/3.0.1/spdx-context.jsonld"`)) {
		t.Errorf("EmbeddedSchema = %v", err)
	}
	if _, err := (Writer{}).EmbeddedSchema("2.3"); err == nil {
		t.Error("a version this build does not write has a schema")
	}
}

func TestComponentFilesReadsTheDocument(t *testing.T) {
	files, err := (Writer{}).ComponentFiles(write(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions()), "libc")
	if err != nil || len(files) != 1 || files[0] != "sysroot:usr/lib/libc.so.6" {
		t.Errorf("files = %v %v", files, err)
	}
}

// TestATypelessProductIsTheSameThingInBothFormats: the neutral document may
// leave a type unset, and what that means is settled once
// (sbommap.EffectiveType). Before, the CycloneDX writer called a typeless
// product a library, the SPDX writer gave it the purpose "application", and the
// BSI triple inside the SPDX document was derived from "library" -- one run,
// three answers. Both documents now state one type, and the purpose and the
// triple derive from it.
func TestATypelessProductIsTheSameThingInBothFormats(t *testing.T) {
	document := sbomwritertest.AllFields()
	document.Product.Type = ""
	options := sbomwritertest.AllFieldsOptions()

	cdxOptions := options
	cdxOptions.SpecVersion = cyclonedx.Version16
	bom, err := (cyclonedx.Writer{}).Build(document, cdxOptions)
	if err != nil {
		t.Fatal(err)
	}
	cdxProperties := map[string]string{}
	for _, property := range bom.Metadata.Component.Properties {
		cdxProperties[property.Name] = property.Value
	}

	var parsed struct {
		Graph []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal(write(t, document, options), &parsed); err != nil {
		t.Fatal(err)
	}
	var product map[string]any
	for _, node := range parsed.Graph {
		if node["type"] == "software_Package" && node["name"] == document.Product.Name {
			product = node
		}
	}
	if product == nil {
		t.Fatal("the SPDX document has no package for the product")
	}
	spdxProperties := map[string]string{}
	for _, extension := range product["extension"].([]any) {
		entries, _ := extension.(map[string]any)["extension_cdxProperty"].([]any)
		for _, entry := range entries {
			entry := entry.(map[string]any)
			spdxProperties[entry["extension_cdxPropName"].(string)] = entry["extension_cdxPropValue"].(string)
		}
	}

	want := sbommap.EffectiveType(document.Product)
	if bom.Metadata.Component.Type != want {
		t.Errorf("CycloneDX states the type %q, want %q", bom.Metadata.Component.Type, want)
	}
	if purpose := product["software_primaryPurpose"]; purpose != want {
		t.Errorf("SPDX states the purpose %v, want %q", purpose, want)
	}
	for _, name := range []string{"sbomb:cdx:archiveProperty", "sbomb:cdx:executableProperty", "sbomb:cdx:structuredProperty"} {
		if cdxProperties[name] == "" || cdxProperties[name] != spdxProperties[name] {
			t.Errorf("%s is %q in CycloneDX and %q in SPDX", name, cdxProperties[name], spdxProperties[name])
		}
	}
	if _, stated := spdxProperties["sbomb:component:type"]; stated {
		t.Error("an unset type is stated as sbomb:component:type; the document carried none")
	}
}
