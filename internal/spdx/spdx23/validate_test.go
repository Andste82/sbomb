package spdx23

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func testdata(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "..", "testdata", "spdx"}, parts...)...)
}

// The schema is the upstream v2.3 file, byte for byte.
func TestTheEmbeddedSchemaIsTheUpstreamFile(t *testing.T) {
	sum := sha256.Sum256(SchemaJSON())
	if got := hex.EncodeToString(sum[:]); got != "239208b7ac287b3cf5d9a9af23f9d69863971102a5e1587a27a398b43490b89b" || len(SchemaJSON()) != 45312 {
		t.Fatalf("the embedded schema is %d bytes with SHA-256 %s; it is not https://raw.githubusercontent.com/spdx/spdx-spec/v2.3/schemas/spdx-schema.json", len(SchemaJSON()), got)
	}
	if _, err := compiled(); err != nil {
		t.Fatal(err)
	}
}

// The official example of the 2.3.1 release -- https://raw.githubusercontent.com/spdx/spdx-spec/v2.3.1/examples/SPDXJSONExample-v2.3.spdx.json,
// SHA-256 2de3741a7be1be5f5e54e837524f2ec627fedfb82307dc004ae03b195abc092f --
// passes tier a: it refers to another document, describes its root with
// documentDescribes and states an MD5 beside its SHA-1, all of which are
// conformant. It is not sbomb's output, and tier b says so.
func TestTheOfficialExampleConforms(t *testing.T) {
	data, err := os.ReadFile(testdata("examples-2.3", "SPDXJSONExample-v2.3.spdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != "2de3741a7be1be5f5e54e837524f2ec627fedfb82307dc004ae03b195abc092f" {
		t.Fatalf("the example is not the upstream file (SHA-256 %s)", got)
	}
	if err := Validate(data); err != nil {
		t.Fatalf("the official example: %v", err)
	}
	err = CheckOwnOutput(data)
	if err == nil {
		t.Fatal("the official example passes as sbomb's own output")
	}
	if !strings.Contains(err.Error(), "is not one sbomb's encoding writes") {
		t.Errorf("tier b does not say the identifiers are foreign:\n%v", err)
	}
}

// The probes CI hands the SPDX project's tools are refused here too, each for
// the rule it probes; the semantic probes pass the schema, so that the
// refusal is the rule's.
func TestTheKnownBadProbesBreakWhatTheyProbe(t *testing.T) {
	for name, want := range map[string]string{
		"file-without-sha1.json":     "has 0 SHA1 checksums",
		"undefined-licenseref.json":  "LicenseRef-undefined, which hasExtractedLicensingInfos does not define",
		"dangling-relationship.json": `"SPDXRef-nowhere" is no element of the document`,
	} {
		data, err := os.ReadFile(testdata("known-bad-2.3", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := validateAgainstSchema(mustInstance(t, data)); err != nil {
			t.Errorf("%s breaks the schema: %v", name, err)
		}
		if err := Validate(data); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v; want %q", name, err, want)
		}
	}
	data, err := os.ReadFile(testdata("known-bad-schema-2.3", "file-without-checksums.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(data); err == nil || !strings.Contains(err.Error(), "schema validation failed") || !strings.Contains(err.Error(), "checksums") {
		t.Errorf("the schema probe: %v", err)
	}
}

func mustInstance(t *testing.T, data []byte) any {
	t.Helper()
	p, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return p.instance
}

// minimalDocument is the smallest conformant document the mutations start
// from.
func minimalDocument() map[string]any {
	var doc map[string]any
	if err := json.Unmarshal([]byte(`{
  "spdxVersion": "SPDX-2.3",
  "dataLicense": "CC0-1.0",
  "SPDXID": "SPDXRef-DOCUMENT",
  "name": "probe",
  "documentNamespace": "urn:uuid:6f1d3a52-6c49-4f0e-9a52-2b1f4d6c0e11",
  "creationInfo": {"created": "2023-11-14T22:13:20Z", "creators": ["Tool: probe-1.0"]},
  "packages": [
    {"SPDXID": "SPDXRef-app", "name": "app", "downloadLocation": "NOASSERTION", "licenseConcluded": "MIT"},
    {"SPDXID": "SPDXRef-lib", "name": "lib", "downloadLocation": "NOASSERTION", "filesAnalyzed": false}
  ],
  "files": [
    {"SPDXID": "SPDXRef-main.c", "fileName": "./main.c",
     "checksums": [{"algorithm": "SHA1", "checksumValue": "da39a3ee5e6b4b0d3255bfef95601890afd80709"}]}
  ],
  "hasExtractedLicensingInfos": [{"licenseId": "LicenseRef-acme", "extractedText": "Acme licence"}],
  "relationships": [
    {"spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": "SPDXRef-app"},
    {"spdxElementId": "SPDXRef-app", "relationshipType": "CONTAINS", "relatedSpdxElement": "SPDXRef-main.c"},
    {"spdxElementId": "SPDXRef-app", "relationshipType": "DEPENDS_ON", "relatedSpdxElement": "SPDXRef-lib"},
    {"spdxElementId": "SPDXRef-lib", "relationshipType": "DEPENDS_ON", "relatedSpdxElement": "NONE"}
  ]
}`), &doc); err != nil {
		panic(err)
	}
	return doc
}

func encodeDocument(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// declaredDocument declares another document under the given identifier.
func declaredDocument(id string) map[string]any {
	return map[string]any{
		"externalDocumentId": id, "spdxDocument": "https://example.com/other",
		"checksum": map[string]any{"algorithm": "SHA1", "checksumValue": "da39a3ee5e6b4b0d3255bfef95601890afd80709"},
	}
}

func list(doc map[string]any, key string) []any { items, _ := doc[key].([]any); return items }

func entry(doc map[string]any, key string, index int) map[string]any {
	return list(doc, key)[index].(map[string]any)
}

// Each rule of tier a refuses a document that breaks it, in a sentence that
// names the rule; the minimal document breaks none.
func TestEachConformanceRuleRefusesWhatItForbids(t *testing.T) {
	if err := Validate(encodeDocument(t, minimalDocument())); err != nil {
		t.Fatalf("the minimal document: %v", err)
	}
	for name, test := range map[string]struct {
		mutate func(map[string]any)
		want   string
	}{
		"spdxVersion":        {func(d map[string]any) { d["spdxVersion"] = "SPDX-2.2" }, `spdxVersion is "SPDX-2.2"`},
		"dataLicense":        {func(d map[string]any) { d["dataLicense"] = "MIT" }, "clause 6.2"},
		"document SPDXID":    {func(d map[string]any) { d["SPDXID"] = "SPDXRef-doc" }, "clause 6.3"},
		"no namespace":       {func(d map[string]any) { delete(d, "documentNamespace") }, "has no documentNamespace"},
		"relative namespace": {func(d map[string]any) { d["documentNamespace"] = "probe" }, "not an absolute URI"},
		"namespace fragment": {func(d map[string]any) { d["documentNamespace"] = "https://example.com/doc#x" }, "not an absolute URI"},
		"created":            {func(d map[string]any) { d["creationInfo"].(map[string]any)["created"] = "2023-11-14" }, "clause 6.9"},
		"creator":            {func(d map[string]any) { d["creationInfo"].(map[string]any)["creators"] = []any{"probe"} }, "clause 6.8"},
		"SPDXID pattern":     {func(d map[string]any) { entry(d, "packages", 1)["SPDXID"] = "SPDXRef-l_b" }, `"SPDXRef-l_b" is not SPDXRef-[idstring]`},
		"SPDXID twice":       {func(d map[string]any) { entry(d, "packages", 1)["SPDXID"] = "SPDXRef-main.c" }, "used by more than one element"},
		"two SHA1": {func(d map[string]any) {
			f := entry(d, "files", 0)
			f["checksums"] = append(f["checksums"].([]any), map[string]any{"algorithm": "SHA1", "checksumValue": "0000000000000000000000000000000000000000"})
		}, "has 2 SHA1 checksums"},
		"checksum length": {func(d map[string]any) {
			entry(d, "files", 0)["checksums"] = []any{map[string]any{"algorithm": "SHA1", "checksumValue": "da39"}}
		}, "4 hex digits, not 40"},
		"checksum hex": {func(d map[string]any) {
			entry(d, "files", 0)["checksums"] = []any{map[string]any{"algorithm": "SHA1", "checksumValue": "zz39a3ee5e6b4b0d3255bfef95601890afd80709"}}
		}, "not hexadecimal"},
		"dangling from": {func(d map[string]any) {
			d["relationships"] = append(list(d, "relationships"), map[string]any{"spdxElementId": "SPDXRef-x", "relationshipType": "DEPENDS_ON", "relatedSpdxElement": "SPDXRef-app"})
		}, `"SPDXRef-x" is no element`},
		"undeclared DocumentRef": {func(d map[string]any) {
			d["relationships"] = append(list(d, "relationships"), map[string]any{"spdxElementId": "SPDXRef-app", "relationshipType": "DEPENDS_ON", "relatedSpdxElement": "DocumentRef-other:SPDXRef-x"})
		}, `"DocumentRef-other:SPDXRef-x" is no element`},
		"NONE as source": {func(d map[string]any) {
			d["relationships"] = append(list(d, "relationships"), map[string]any{"spdxElementId": "NONE", "relationshipType": "DEPENDS_ON", "relatedSpdxElement": "SPDXRef-app"})
		}, `"NONE" is no element`},
		"externalDocumentId pattern": {func(d map[string]any) {
			d["externalDocumentRefs"] = []any{declaredDocument("DocumentRef-a_b")}
		}, `externalDocumentId "DocumentRef-a_b" is not DocumentRef-[idstring] (clause 6.6)`},
		"documentDescribes dangling": {func(d map[string]any) { d["documentDescribes"] = []any{"SPDXRef-nowhere"} },
			`documentDescribes names "SPDXRef-nowhere", which is no element of the document`},
		"no root":              {func(d map[string]any) { d["relationships"] = list(d, "relationships")[1:] }, "clause 11.1"},
		"undefined LicenseRef": {func(d map[string]any) { entry(d, "packages", 0)["licenseConcluded"] = "LicenseRef-beta" }, "LicenseRef-beta, which hasExtractedLicensingInfos does not define"},
		"unparsable licence":   {func(d map[string]any) { entry(d, "packages", 0)["licenseConcluded"] = "MIT AND" }, "licenseConcluded"},
		"AdditionRef":          {func(d map[string]any) { entry(d, "packages", 0)["licenseConcluded"] = "MIT WITH AdditionRef-x" }, "no AdditionRef"},
		"undeclared DocumentRef licence": {func(d map[string]any) {
			entry(d, "packages", 0)["licenseConcluded"] = "DocumentRef-other:LicenseRef-x"
		}, "declares no DocumentRef-other"},
		"licenseId twice": {func(d map[string]any) {
			d["hasExtractedLicensingInfos"] = append(list(d, "hasExtractedLicensingInfos"), map[string]any{"licenseId": "LicenseRef-acme", "extractedText": "again"})
		}, "defined more than once"},
		"licenseId shape": {func(d map[string]any) { entry(d, "hasExtractedLicensingInfos", 0)["licenseId"] = "Acme" }, "is not LicenseRef-[idstring]"},
		"empty text":      {func(d map[string]any) { entry(d, "hasExtractedLicensingInfos", 0)["extractedText"] = " " }, "clause 10.2"},
		"verification code without analysis": {func(d map[string]any) {
			entry(d, "packages", 1)["packageVerificationCode"] = map[string]any{"packageVerificationCodeValue": "da39a3ee5e6b4b0d3255bfef95601890afd80709"}
		}, "clause 7.9"},
		"licence from files without analysis": {func(d map[string]any) { entry(d, "packages", 1)["licenseInfoFromFiles"] = []any{"MIT"} }, "clause 7.14"},
		"hasFiles without analysis":           {func(d map[string]any) { entry(d, "packages", 1)["hasFiles"] = []any{"SPDXRef-main.c"} }, "shall not contain any files"},
		"contains without analysis": {func(d map[string]any) {
			d["relationships"] = append(list(d, "relationships"), map[string]any{"spdxElementId": "SPDXRef-lib", "relationshipType": "CONTAINS", "relatedSpdxElement": "SPDXRef-main.c"})
		}, "package SPDXRef-lib has filesAnalyzed false and contains file SPDXRef-main.c"},
		"contained by without analysis": {func(d map[string]any) {
			d["relationships"] = append(list(d, "relationships"), map[string]any{"spdxElementId": "SPDXRef-main.c", "relationshipType": "CONTAINED_BY", "relatedSpdxElement": "SPDXRef-lib"})
		}, "package SPDXRef-lib has filesAnalyzed false and contains file SPDXRef-main.c"},
	} {
		doc := minimalDocument()
		test.mutate(doc)
		err := Validate(encodeDocument(t, doc))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v; want %q", name, err, test.want)
		}
	}
}

// documentDescribes and the inverse DESCRIBED_BY are as good as DESCRIBES for
// tier a, and a document of one element needs none of them.
func TestTheRootMayBeStatedEitherWay(t *testing.T) {
	doc := minimalDocument()
	doc["relationships"] = list(doc, "relationships")[1:]
	doc["documentDescribes"] = []any{"SPDXRef-app"}
	if err := Validate(encodeDocument(t, doc)); err != nil {
		t.Errorf("documentDescribes: %v", err)
	}
	// Clause 11.1: "SPDXRef-A DESCRIBED_BY SPDXRef-DOCUMENT" equals
	// "SPDXRef-DOCUMENT DESCRIBES SPDXRef-A".
	inverse := minimalDocument()
	inverse["relationships"] = append([]any{map[string]any{
		"spdxElementId": "SPDXRef-app", "relationshipType": "DESCRIBED_BY", "relatedSpdxElement": "SPDXRef-DOCUMENT",
	}}, list(inverse, "relationships")[1:]...)
	if err := Validate(encodeDocument(t, inverse)); err != nil {
		t.Errorf("DESCRIBED_BY: %v", err)
	}
	// Clause 6.6: an idstring may hold "+", and a relationship may end in an
	// element of the document it names.
	plus := minimalDocument()
	plus["externalDocumentRefs"] = []any{declaredDocument("DocumentRef-spdx+tool-1.2")}
	plus["relationships"] = append(list(plus, "relationships"), map[string]any{
		"spdxElementId": "SPDXRef-app", "relationshipType": "DEPENDS_ON", "relatedSpdxElement": "DocumentRef-spdx+tool-1.2:SPDXRef-ToolsElement",
	})
	if err := Validate(encodeDocument(t, plus)); err != nil {
		t.Errorf("a DocumentRef with a +: %v", err)
	}
	single := minimalDocument()
	single["packages"] = list(single, "packages")[:1]
	delete(single, "files")
	delete(single, "relationships")
	if err := Validate(encodeDocument(t, single)); err != nil {
		t.Errorf("one package: %v", err)
	}
}

// Tier b refuses what a valid document may do and sbomb never does.
func TestTierBHoldsAValidDocumentToSbombsPromises(t *testing.T) {
	data := renderAllFields(t)
	for name, test := range map[string]struct {
		mutate func(map[string]any)
		want   string
	}{
		"documentDescribes": {func(d map[string]any) { d["documentDescribes"] = []any{entry(d, "packages", 0)["SPDXID"]} }, "documentDescribes"},
		"external document": {func(d map[string]any) {
			d["externalDocumentRefs"] = []any{map[string]any{
				"externalDocumentId": "DocumentRef-other", "spdxDocument": "https://example.com/other",
				"checksum": map[string]any{"algorithm": "SHA1", "checksumValue": "da39a3ee5e6b4b0d3255bfef95601890afd80709"},
			}}
		}, "external documents"},
		"foreign SPDXID": {func(d map[string]any) {
			old := entry(d, "packages", 1)["SPDXID"].(string)
			entry(d, "packages", 1)["SPDXID"] = "SPDXRef-app"
			for _, item := range list(d, "relationships") {
				rel := item.(map[string]any)
				for _, key := range []string{"spdxElementId", "relatedSpdxElement"} {
					if rel[key] == old {
						rel[key] = "SPDXRef-app"
					}
				}
			}
		}, `"SPDXRef-app" is not one sbomb's encoding writes`},
		"unreachable": {func(d map[string]any) {
			target := elementID("component:type-file")
			var kept []any
			for _, item := range list(d, "relationships") {
				if item.(map[string]any)["relatedSpdxElement"] == target {
					continue
				}
				kept = append(kept, item)
			}
			d["relationships"] = kept
		}, "cannot be reached from the described package"},
		"download location": {func(d map[string]any) { entry(d, "packages", 1)["downloadLocation"] = "https://example.com/x.tgz" }, "download location"},
		"free-text annotation": {func(d map[string]any) {
			entry(d, "annotations", 0)["comment"] = "reviewed by hand"
		}, "is not an sbomb:name=value property"},
		"licence list version": {func(d map[string]any) { d["creationInfo"].(map[string]any)["licenseListVersion"] = "3.0" }, "licenseListVersion"},
		"two roots": {func(d map[string]any) {
			d["relationships"] = append(list(d, "relationships"), relationshipEntry(documentID, "DESCRIBES", entry(d, "packages", 1)["SPDXID"]))
		}, "the document describes 2 packages"},
		"root not first": {func(d map[string]any) {
			packages := list(d, "packages")
			packages[0], packages[1] = packages[1], packages[0]
		}, "is not the first package"},
		"DESCRIBES between packages": {func(d map[string]any) {
			d["relationships"] = append(list(d, "relationships"), relationshipEntry(entry(d, "packages", 1)["SPDXID"], "DESCRIBES", entry(d, "packages", 2)["SPDXID"]))
		}, "is not the document describing a package"},
		"relationship to another document": {func(d map[string]any) {
			d["externalDocumentRefs"] = []any{declaredDocument("DocumentRef-other")}
			d["relationships"] = append(list(d, "relationships"), relationshipEntry(entry(d, "packages", 1)["SPDXID"], "DEPENDS_ON", "DocumentRef-other:SPDXRef-x"))
		}, "refers to another document"},
		"package without a name": {func(d map[string]any) { entry(d, "packages", 1)["name"] = " " }, "has no name"},
		"file without a name":    {func(d map[string]any) { entry(d, "files", 0)["fileName"] = " " }, "has no name"},
		"licence list version without a listed licence": {func(d map[string]any) {
			for _, key := range []string{"packages", "files"} {
				for _, item := range list(d, key) {
					element := item.(map[string]any)
					for _, field := range []string{"licenseConcluded", "licenseDeclared"} {
						if _, stated := element[field]; stated {
							element[field] = noAssertion
						}
					}
					delete(element, "licenseInfoInFiles")
					delete(element, "licenseInfoFromFiles")
				}
			}
		}, "is stated, but no listed licence is named"},
		"package after the file packages": {func(d map[string]any) {
			packages := list(d, "packages")
			moved := packages[1]
			d["packages"] = append(append([]any{packages[0]}, packages[2:]...), moved)
		}, "follows a package that stands for a file"},
		"file package order": {func(d map[string]any) {
			packages := list(d, "packages")
			last := len(packages) - 1
			packages[last-1], packages[last] = packages[last], packages[last-1]
		}, "is out of order"},
		"file order": {func(d map[string]any) {
			files := list(d, "files")
			files[0], files[1] = files[1], files[0]
		}, "is out of order"},
		"relationship order": {func(d map[string]any) {
			rels := list(d, "relationships")
			rels[1], rels[2] = rels[2], rels[1]
		}, "out of order"},
	} {
		mutated := map[string]any{}
		if err := json.Unmarshal(data, &mutated); err != nil {
			t.Fatal(err)
		}
		test.mutate(mutated)
		encoded := encodeDocument(t, mutated)
		if err := Validate(encoded); err != nil {
			t.Errorf("%s: the mutation is not valid SPDX 2.3, so tier b proves nothing: %v", name, err)
			continue
		}
		if err := CheckOwnOutput(encoded); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v; want %q", name, err, test.want)
		}
	}
}

func relationshipEntry(from any, kind string, to any) map[string]any {
	return map[string]any{"spdxElementId": from, "relationshipType": kind, "relatedSpdxElement": to}
}

// A document with many defects is reported as the 3.0.1 validator reports
// one: sorted, and capped with a count of the rest.
func TestTheReportIsCappedAndSorted(t *testing.T) {
	doc := minimalDocument()
	for index := 0; index < maxReportedIssues+5; index++ {
		doc["relationships"] = append(list(doc, "relationships"),
			relationshipEntry(fmt.Sprintf("SPDXRef-nowhere%02d", index), "DEPENDS_ON", "SPDXRef-app"))
	}
	err := Validate(encodeDocument(t, doc))
	if err == nil {
		t.Fatal("no issues")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 1+maxReportedIssues+1 || lines[len(lines)-1] != "  ... and 5 more" {
		t.Fatalf("the report is not capped at %d:\n%v", maxReportedIssues, err)
	}
	if !sort.StringsAreSorted(lines[1 : 1+maxReportedIssues]) {
		t.Errorf("the issues are not sorted:\n%v", err)
	}
}
