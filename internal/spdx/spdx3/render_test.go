package spdx3

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/licenselist"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// TestTheEmbeddedSchemaIsTheUpstreamFile pins the embedded schema to the file
// SPDX publishes as https://spdx.org/schema/3.0.1/spdx-json-schema.json, so
// that nobody edits it to make a document pass.
func TestTheEmbeddedSchemaIsTheUpstreamFile(t *testing.T) {
	sum := sha256.Sum256(SchemaJSON())
	if got := hex.EncodeToString(sum[:]); got != "582c64e809d5b3ef9bd0c4de13a32391b47b0284a3e8d199569fb96f649234b1" {
		t.Fatalf("the embedded schema has SHA-256 %s; it is not the published 3.0.1 schema", got)
	}
}

// TestTheSchemaCompilesDespiteItsLookahead: the IRI pattern is a lookahead Go
// cannot compile; the hand matcher stands in for exactly that pattern.
func TestTheSchemaCompilesDespiteItsLookahead(t *testing.T) {
	if _, err := compiled(); err != nil {
		t.Fatal(err)
	}
	matcher, err := schemaRegexp(iriPattern)
	if err != nil {
		t.Fatal(err)
	}
	for value, want := range map[string]bool{
		"urn:uuid:x#a": true, "https://a/b": true, "_:blank": false, "nocolon": false, "a:": false,
	} {
		if matcher.MatchString(value) != want {
			t.Errorf("IRI pattern on %q = %v, want %v", value, !want, want)
		}
	}
}

// TestTheOfficialExamplesConform: the example documents of the SPDX 3 model
// repository (serialization/jsonld/examples) were written for 3.0.0 and
// declare prefixes in an @context array, so validate refuses every one of
// them as published. With the plain 3.0.1 context and specVersion, every one
// passes the conformance rules, and five of the seven pass the schema. The
// other two carry upstream defects the schema finds: sbom1.json has a scalar
// software_sbomType, and converted_from_spdx_2.json a scalar
// software_attributionText -- both arrays in 3.0.1 -- and a software_Snippet
// without the required software_snippetFromFile. The test holds each to
// exactly that set, so a new defect or a lost one is noticed.
func TestTheOfficialExamplesConform(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "spdx", "examples", "*.json"))
	if err != nil || len(paths) != 7 {
		t.Fatalf("expected the seven official examples, found %d (%v)", len(paths), err)
	}
	schemaFailures := map[string]string{
		"sbom1.json": "SPDX 3.0.1 schema validation failed:\n" +
			"  /software_sbomType: got string, want array",
		"converted_from_spdx_2.json": "SPDX 3.0.1 schema validation failed:\n" +
			"  /@graph/16: missing property 'software_snippetFromFile'\n" +
			"  /@graph/7/software_attributionText: got string, want array",
	}
	for _, path := range paths {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data := as301(t, raw)
			if err := conformance(mustParse(t, data)).err("conformance"); err != nil {
				t.Errorf("an official example is not conformant: %v", err)
			}
			schemaErr := ValidateAgainstSchema(data)
			if want, expected := schemaFailures[name]; expected {
				if schemaErr == nil || schemaErr.Error() != want {
					t.Errorf("schema = %v; want exactly the known upstream defects:\n%s", schemaErr, want)
				}
				return
			}
			if schemaErr != nil {
				t.Errorf("schema: %v", schemaErr)
			}
		})
	}
}

// TestTheKnownBadProbesBreakWhatTheyProbe: CI feeds the probes under
// testdata/spdx to the SPDX project's validators and requires a refusal, so
// that a validator that accepts everything cannot pass (D51). A probe proves
// that only if its defect is the one it is there for: the schema probe breaks
// the schema and nothing else, and the SHACL probes pass the schema, so that
// the SHACL check is what has to refuse them.
func TestTheKnownBadProbesBreakWhatTheyProbe(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "spdx")
	schemaProbe, err := os.ReadFile(filepath.Join(root, "known-bad-schema", "scalar-sbom-type.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "SPDX 3.0.1 schema validation failed:\n  /@graph/2/software_sbomType: got string, want array"
	if err := ValidateAgainstSchema(schemaProbe); err == nil || err.Error() != want {
		t.Errorf("schema probe: %v; want exactly\n%s", err, want)
	}
	shaclProbes, err := filepath.Glob(filepath.Join(root, "known-bad", "*.json"))
	if err != nil || len(shaclProbes) == 0 {
		t.Fatalf("no SHACL probes (%v)", err)
	}
	for _, path := range shaclProbes {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateAgainstSchema(data); err != nil {
			t.Errorf("%s breaks the schema, so it does not show that the SHACL check refuses: %v", filepath.Base(path), err)
		}
	}
}

// as301 rewrites an official example to the plain 3.0.1 context and spec
// version, which is all that separates it from a 3.0.1 document.
func as301(t *testing.T, raw []byte) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	root["@context"] = contextURL
	walkObjects(root, func(object map[string]any) {
		if object["type"] == "CreationInfo" {
			object["specVersion"] = specVersion
		}
	})
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustParse(t *testing.T, data []byte) *parsedDocument {
	t.Helper()
	parsed, err := parseDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestIRIsArePercentEncodedInjectively(t *testing.T) {
	cases := map[string]string{
		"file:build:_deps/zlib-src/zlib.c": "file:build:_deps/zlib-src/zlib.c",
		"file:project:a b.c":               "file:project:a%20b.c",
		"file:project:100%.c":              "file:project:100%25.c",
		"component:zlib#0123abcd":          "component:zlib%230123abcd",
		"file:project:café.c":              "file:project:caf%C3%A9.c",
		"license:MIT OR Apache-2.0":        "license:MIT%20OR%20Apache-2.0",
	}
	for local, want := range cases {
		if got := escapeFragment(local); got != want {
			t.Errorf("escapeFragment(%q) = %q, want %q", local, got, want)
		}
		if back := unescapeFragment(escapeFragment(local)); back != local {
			t.Errorf("%q did not survive a round trip: %q", local, back)
		}
	}
	// "%" itself is escaped, so a literal "%20" and a space never meet.
	if escapeFragment("a%20b") == escapeFragment("a b") {
		t.Error("two local identities escaped to one fragment")
	}
}

// TestTheRenderingIsByteStable: one document, one rendering.
func TestTheRenderingIsByteStable(t *testing.T) {
	first := renderAllFields(t)
	second := renderAllFields(t)
	if !bytes.Equal(first, second) {
		t.Fatal("two renderings of one document differ")
	}
	if !bytes.HasSuffix(first, []byte("}\n")) || bytes.HasSuffix(first, []byte("\n\n")) {
		t.Error("the document must end with exactly one newline")
	}
}

// TestTheRenderingDoesNotDependOnInputOrder shuffles every slice of the
// synthetic document except Components -- whose order is identity-relevant by
// section 28.4 -- and every property value list, and expects the same bytes.
func TestTheRenderingDoesNotDependOnInputOrder(t *testing.T) {
	want := renderAllFields(t)
	random := rand.New(rand.NewSource(1))
	for round := 0; round < 5; round++ {
		document := sbomwritertest.AllFields()
		shuffle := func(n int, swap func(i, j int)) { random.Shuffle(n, swap) }
		shuffleProperties := func(properties map[string][]string) {
			for _, values := range properties {
				shuffle(len(values), func(i, j int) { values[i], values[j] = values[j], values[i] })
			}
		}
		shuffle(len(document.Artifacts), func(i, j int) {
			document.Artifacts[i], document.Artifacts[j] = document.Artifacts[j], document.Artifacts[i]
		})
		shuffle(len(document.Files), func(i, j int) { document.Files[i], document.Files[j] = document.Files[j], document.Files[i] })
		shuffle(len(document.Relations), func(i, j int) {
			document.Relations[i], document.Relations[j] = document.Relations[j], document.Relations[i]
		})
		for _, relation := range document.Relations {
			to := relation.To
			shuffle(len(to), func(i, j int) { to[i], to[j] = to[j], to[i] })
		}
		for index := range document.Files {
			shuffleProperties(document.Files[index].Properties)
		}
		for index := range document.Components {
			c := &document.Components[index]
			shuffle(len(c.LicenseArtifacts), func(i, j int) {
				c.LicenseArtifacts[i], c.LicenseArtifacts[j] = c.LicenseArtifacts[j], c.LicenseArtifacts[i]
			})
			shuffle(len(c.Copyrights), func(i, j int) { c.Copyrights[i], c.Copyrights[j] = c.Copyrights[j], c.Copyrights[i] })
			shuffle(len(c.CVEExclusions), func(i, j int) { c.CVEExclusions[i], c.CVEExclusions[j] = c.CVEExclusions[j], c.CVEExclusions[i] })
			shuffle(len(c.Modification.Patches), func(i, j int) {
				c.Modification.Patches[i], c.Modification.Patches[j] = c.Modification.Patches[j], c.Modification.Patches[i]
			})
			shuffle(len(c.Licenses), func(i, j int) { c.Licenses[i], c.Licenses[j] = c.Licenses[j], c.Licenses[i] })
			shuffle(len(c.LicenseEvidence), func(i, j int) {
				c.LicenseEvidence[i], c.LicenseEvidence[j] = c.LicenseEvidence[j], c.LicenseEvidence[i]
			})
			shuffle(len(c.LinkageForms), func(i, j int) { c.LinkageForms[i], c.LinkageForms[j] = c.LinkageForms[j], c.LinkageForms[i] })
			shuffleProperties(c.Properties)
		}
		got := render(t, document, sbomwritertest.AllFieldsOptions())
		if !bytes.Equal(got, want) {
			t.Fatalf("round %d: the rendering depends on the order the document lists things in", round)
		}
	}
}

// TestEveryRenderedNodeKeyIsInTheSchema: every key of every node, nested
// objects included, is one the schema defines. The schema itself refuses
// unknown keys; this names the key when it does.
func TestEveryRenderedNodeKeyIsInTheSchema(t *testing.T) {
	data := renderAllFields(t)
	known := map[string]bool{"@context": true, "@graph": true, "@id": true, "type": true, "spdxId": true}
	for _, match := range regexp.MustCompile(`"prop_[A-Za-z0-9_]+?_([A-Za-z0-9]+(?:_[A-Za-z0-9]+)?)"`).FindAllSubmatch(SchemaJSON(), -1) {
		known[string(match[1])] = true
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	schema := string(SchemaJSON())
	walkObjects(root, func(object map[string]any) {
		for key := range object {
			if !known[key] && !strings.Contains(schema, `"`+key+`":`) {
				t.Errorf("key %q is not in the schema", key)
			}
		}
	})
}

func TestNoDocumentClaimsTheExtensionProfile(t *testing.T) {
	document := nodeAt(t, renderAllFields(t), "document")
	var profiles []string
	for _, profile := range document["profileConformance"].([]any) {
		profiles = append(profiles, profile.(string))
	}
	if containsString(profiles, "extension") {
		t.Error("the extension profile is claimed; see deviation D49b")
	}
	want := []string{"core", "expandedLicensing", "security", "simpleLicensing", "software"}
	if strings.Join(profiles, ",") != strings.Join(want, ",") {
		t.Errorf("profiles = %v, want %v", profiles, want)
	}
}

func TestEachLinkageEdgeHasItsTypeClassAndScope(t *testing.T) {
	data := renderAllFields(t)
	type spelled struct{ class, relationshipType, scope string }
	want := map[string]spelled{
		"component:kitchen": {"Relationship", "hasStaticLink", ""},
		"component:gen":     {"LifecycleScopedRelationship", "usesTool", "build"},
		"component:libc":    {"LifecycleScopedRelationship", "hasDynamicLink", "runtime"},
		"component:hdr":     {"Relationship", "dependsOn", ""},
	}
	found := map[string]bool{}
	for _, rel := range relationshipsFrom(t, data, "artifact:build:bin/app") {
		for _, to := range rel["to"].([]any) {
			local := localOf(to.(string))
			expected, tracked := want[local]
			if !tracked || rel["relationshipType"] != expected.relationshipType {
				continue
			}
			scope, _ := rel["scope"].(string)
			if rel["type"] != expected.class || scope != expected.scope {
				t.Errorf("%s: class %v scope %q, want %s scope %q", local, rel["type"], scope, expected.class, expected.scope)
			}
			found[local] = true
		}
	}
	for local := range want {
		if !found[local] {
			t.Errorf("the app has no %s edge to %s", want[local].relationshipType, local)
		}
	}
	if !containsString(targetsOf(t, data, "artifact:build:fs.img", "contains"), "component:assets") {
		t.Error("an embedded asset is contained in the image")
	}
}

func TestAKnownLeafDependsOnNoneElementCompletely(t *testing.T) {
	data := renderAllFields(t)
	for _, local := range []string{"component:kitchen@1.2.3-sentinel", "file:build:_deps/kitchen-src/kitchen.c"} {
		ok := false
		for _, rel := range relationshipsFrom(t, data, local) {
			to := rel["to"].([]any)
			if rel["relationshipType"] == "dependsOn" && len(to) == 1 && to[0] == noneElement && rel["completeness"] == "complete" {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s depends on nothing and does not say so", local)
		}
	}
	for _, rel := range graphOf(t, data) {
		if rel["completeness"] != nil && rel["to"].([]any)[0] != noneElement {
			t.Errorf("completeness is claimed for %v; sbomb lists used files only, so only a leaf is complete", rel["spdxId"])
		}
	}
	// A patch file and an evidence-only file have no CycloneDX counterpart,
	// so no leaf statement either.
	if len(relationshipsFrom(t, data, "patch:component:kitchen/1")) != 0 {
		t.Error("a patch file states something about its dependencies")
	}
}

func TestEveryCustomLicenseRefIsDefined(t *testing.T) {
	data := renderAllFields(t)
	ref := "LicenseRef-sbomb-Sentinel-Retained-Licence-d581e9c0-083d786f"
	expression := nodeAt(t, data, "license:"+ref)
	entries := expression["simplelicensing_customIdToUri"].([]any)
	entry := entries[0].(map[string]any)
	if entry["key"] != ref || entry["value"] != iriOf("license-text:"+ref) {
		t.Fatalf("customIdToUri = %v", entries)
	}
	text := nodeAt(t, data, "license-text:"+ref)
	if text["simplelicensing_licenseText"] != "sentinel retained custom licence text\n" || text["comment"] != nil {
		t.Errorf("a retained text is the definition: %v", text)
	}
	// Retained bytes that are not UTF-8 cannot be the text field; the
	// definition is the name, and the comment names the file the bytes travel
	// on rather than saying no text was retained.
	binary := nodeAt(t, data, "license-text:LicenseRef-sbomb-Sentinel-Custom-Licence-2ce69ab6-d9c0bbf6")
	comment, _ := binary["comment"].(string)
	if binary["simplelicensing_licenseText"] != "Sentinel Custom Licence" ||
		!strings.Contains(comment, "retained but is not UTF-8") || !strings.HasSuffix(comment, "build:_deps/kitchen-src/COPYING.custom") {
		t.Errorf("a retained text that is not UTF-8 is said to be retained, and where: %v", binary)
	}
	carrier := nodeAt(t, data, "file:build:_deps/kitchen-src/COPYING.custom")
	if refs, _ := carrier["externalRef"].([]any); len(refs) != 1 || !strings.HasPrefix(fmt.Sprint(refs[0].(map[string]any)["locator"]), "[data:text/plain;base64,") {
		t.Errorf("the file the comment names does not carry the bytes: %v", carrier["externalRef"])
	}
	// Without licence texts in the SBOM, the definition is the name, and says
	// that no text was retained.
	options := sbomwritertest.AllFieldsOptions()
	options.LicenseText = ""
	unretained := nodeAt(t, render(t, sbomwritertest.AllFields(), options), "license-text:LicenseRef-sbomb-Sentinel-Custom-Licence-2ce69ab6")
	if unretained["simplelicensing_licenseText"] != "Sentinel Custom Licence" || !strings.Contains(fmt.Sprint(unretained["comment"]), "no licence text was retained") {
		t.Errorf("without a retained text the definition is the name, and says so: %v", unretained)
	}
}

func TestEveryListedExpressionNamesItsListVersion(t *testing.T) {
	data := renderAllFields(t)
	for _, node := range graphOf(t, data) {
		if node["type"] != "simplelicensing_LicenseExpression" {
			continue
		}
		expression := node["simplelicensing_licenseExpression"].(string)
		listed := !strings.HasPrefix(expression, "LicenseRef-")
		if version, _ := node["simplelicensing_licenseListVersion"].(string); listed != (version == licenselist.Version) {
			t.Errorf("%s: list version %q", expression, version)
		}
	}
}

func TestTheVendorAndSupplierAreTheSameOrganizationWhenNamedAlike(t *testing.T) {
	data := renderAllFields(t)
	vendor := iriOf("agent:Sentinel Vendor Org")
	count := 0
	for _, node := range graphOf(t, data) {
		if node["type"] == "Organization" && node["name"] == "Sentinel Vendor Org" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("%d organizations named Sentinel Vendor Org, want one", count)
	}
	if nodeAt(t, data, "product:sentinel-product")["suppliedBy"] != vendor {
		t.Error("the product's supplier is not the vendor organization")
	}
	if graphOf(t, data)[0]["createdBy"].([]any)[0] != vendor {
		t.Error("the creator is not the vendor organization")
	}
}

func TestAHeaderOnlyComponentIsASourceDependency(t *testing.T) {
	data := renderAllFields(t)
	header := nodeAt(t, data, "component:hdr")
	if header["software_primaryPurpose"] != "library" || header["software_additionalPurpose"].([]any)[0] != "source" {
		t.Errorf("purposes = %v / %v", header["software_primaryPurpose"], header["software_additionalPurpose"])
	}
	if !containsString(targetsOf(t, data, "artifact:build:bin/app", "dependsOn"), "component:hdr") {
		t.Error("the header-only component is not a dependency of the app")
	}
}

func TestALicenceFileThatIsAlsoAUsedFileKeepsItsPurpose(t *testing.T) {
	data := renderAllFields(t)
	license := nodeAt(t, data, "file:build:_deps/kitchen-src/LICENSE")
	if license["software_primaryPurpose"] != "data" {
		t.Errorf("an asset that is a licence file is data first: %v", license["software_primaryPurpose"])
	}
	if purposes, _ := license["software_additionalPurpose"].([]any); len(purposes) != 1 || purposes[0] != "documentation" {
		t.Errorf("and documentation besides: %v", purposes)
	}
	if notice := nodeAt(t, data, "file:build:_deps/kitchen-src/NOTICE"); notice["software_primaryPurpose"] != "documentation" {
		t.Errorf("an evidence-only file is documentation: %v", notice["software_primaryPurpose"])
	}
}

// TestASourceFileACopyrightLineWasReadFromIsNoDocumentation: a copyright
// statement read from kitchen.h makes the header evidence for its component,
// not documentation; only a retained licence, notice or copyright file is.
func TestASourceFileACopyrightLineWasReadFromIsNoDocumentation(t *testing.T) {
	header := nodeAt(t, renderAllFields(t), "file:build:_deps/kitchen-src/kitchen.h")
	if header["software_copyrightText"] == nil {
		t.Fatal("kitchen.h carries no copyright statement; the test no longer exercises the case")
	}
	if header["software_primaryPurpose"] != "source" || header["software_additionalPurpose"] != nil {
		t.Errorf("purposes = %v / %v", header["software_primaryPurpose"], header["software_additionalPurpose"])
	}
	for _, origin := range []mapping.Origin{mapping.OriginEvidence, mapping.OriginUsed | mapping.OriginEvidence} {
		file := mapping.File{Origin: origin, Class: string(domain.FileClassSource)}
		if purposes := fileAdditionalPurposes(file); purposes != nil {
			t.Errorf("origin %b: additional purposes %v", origin, purposes)
		}
	}
	if got := filePurpose(mapping.File{Origin: mapping.OriginEvidence}); got != "" {
		t.Errorf("a file that is only a copyright source is %q; what it is was never established", got)
	}
}

func TestObservedLicencesAreDeclaredAndTheConclusionIsConcluded(t *testing.T) {
	data := renderAllFields(t)
	if got := targetsOf(t, data, "component:kitchen", "hasConcludedLicense"); len(got) != 1 || got[0] != "license:MIT OR Apache-2.0" {
		t.Errorf("concluded = %v", got)
	}
	declared := targetsOf(t, data, "component:kitchen", "hasDeclaredLicense")
	sort.Strings(declared)
	if len(declared) != 2 || declared[1] != "license:MIT" {
		t.Errorf("declared = %v", declared)
	}
}

func TestAMissingFileHasNoHashAndSaysItIsMissing(t *testing.T) {
	missing := nodeAt(t, renderAllFields(t), "file:project:hdr/hdr.h")
	if missing["verifiedUsing"] != nil {
		t.Error("a file nobody read carries a digest")
	}
	if !containsString(propertiesOf(missing), "sbomb:file:missing=true") {
		t.Error("the file does not say it is missing")
	}
}

func TestAnUnknownHashKeyIsOtherWithTheKeyInTheComment(t *testing.T) {
	archive := nodeAt(t, renderAllFields(t), "file:build:lib/libkitchen.a")
	hash := archive["verifiedUsing"].([]any)[0].(map[string]any)
	if hash["algorithm"] != "other" || hash["comment"] != sbomwritertest.HashUnknownKey || hash["hashValue"] != sbomwritertest.HashUnknownValue {
		t.Errorf("hash = %v", hash)
	}
}

// TestNoLicenceTextIsEverAttributionText holds the renderer to the 3.0.1
// definition of attributionText, which "is not meant to include the software
// Package, File or Snippet's actual complete license text": a retained text
// is attached to its file as a licence reference, and a copyright statement
// that names no file is a copyright notice of the package.
func TestNoLicenceTextIsEverAttributionText(t *testing.T) {
	raw := renderAllFields(t)
	if bytes.Contains(raw, []byte("software_attributionText")) {
		t.Error("the document has an attribution text")
	}
	file := nodeAt(t, raw, "file:build:_deps/kitchen-src/LICENSE")
	references, _ := file["externalRef"].([]any)
	want := "data:text/plain;charset=utf-8;base64," + base64.StdEncoding.EncodeToString([]byte("sentinel licence text of MIT\n"))
	found := false
	for _, reference := range references {
		object := reference.(map[string]any)
		found = found || object["externalRefType"] == "license" && object["locator"].([]any)[0] == want
	}
	if !found {
		t.Errorf("the retained MIT text is not a licence reference of its file: %v", references)
	}
	kitchen := nodeAt(t, raw, "component:kitchen")
	if kitchen["software_copyrightText"] != "Copyright sentinel curated notice\nCopyright sentinel holder C, of no file" {
		t.Errorf("copyright text = %q", kitchen["software_copyrightText"])
	}
}

func TestATextThatIsNotUTF8TravelsAsADataURL(t *testing.T) {
	file := nodeAt(t, renderAllFields(t), "file:build:_deps/kitchen-src/COPYING.custom")
	if file["software_attributionText"] != nil {
		t.Error("bytes that are not UTF-8 were put into a string")
	}
	reference := file["externalRef"].([]any)[0].(map[string]any)
	if reference["locator"].([]any)[0] != "data:text/plain;base64,//5zZW50aW5lbA==" {
		t.Errorf("reference = %v", reference)
	}
}

func TestPurposesFollowTypeAndClass(t *testing.T) {
	packages := map[string]string{
		"application": "application", "operating-system": "operatingSystem", "device-driver": "deviceDriver",
		"machine-learning-model": "model", "cryptographic-asset": "other", "data": "data", "container": "container",
	}
	for componentType, want := range packages {
		if got := packagePurpose(mapping.Package{StatedType: componentType, Role: mapping.RoleComponent}); got != want {
			t.Errorf("%q -> %q, want %q", componentType, got, want)
		}
	}
	// The purpose reads the stated type and decides nothing about an unset
	// one: that is settled once, below the writers (sbommap.EffectiveType),
	// and the mapping states it for a typeless product as for any package.
	if got := packagePurpose(mapping.Package{Type: "", StatedType: "library", Role: mapping.RoleProduct}); got != "library" {
		t.Errorf("a typeless product stated as a library has the purpose %q", got)
	}
	classes := map[domain.FileClass]string{
		domain.FileClassSource: "source", domain.FileClassHeader: "source", domain.FileClassGeneratedSource: "source",
		domain.FileClassGeneratedHeader: "source", domain.FileClassObject: "other", domain.FileClassArchive: "archive",
		domain.FileClassSharedLibrary: "library", domain.FileClassAsset: "data", domain.FileClassUnknown: "",
	}
	for class, want := range classes {
		if got := filePurpose(mapping.File{Origin: mapping.OriginUsed, Class: string(class)}); got != want {
			t.Errorf("%q -> %q, want %q", class, got, want)
		}
	}
}

func TestComponentFilesFindsTheFilesOfAPackage(t *testing.T) {
	data := renderAllFields(t)
	files, err := ComponentFiles(data, "kitchen")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	if len(files) != 7 || files[0] != "build:_deps/kitchen-src/LICENSE" {
		t.Errorf("files = %v", files)
	}
	if files, err := ComponentFiles(data, "component:assets"); err != nil || len(files) != 1 || files[0] != "project:assets/logo png.png" {
		t.Errorf("by local identity, with an escaped path: %v %v", files, err)
	}
	if _, err := ComponentFiles(data, "nothing"); err != sbomwriter.ErrNoSuchComponent {
		t.Errorf("an unknown component = %v", err)
	}
	// explain looks a component up by a toolchain: identity as well (section
	// 32.3). sbomb names a toolchain's grouping component: today, so the
	// identity is put there by hand; a bare name must still find it.
	toolchain := bytes.ReplaceAll(data, []byte("#component:toolchain-headers\""), []byte("#toolchain:gcc-13\""))
	if bytes.Equal(toolchain, data) {
		t.Fatal("the synthetic document has no component:toolchain-headers")
	}
	if files, err := ComponentFiles(toolchain, "gcc-13"); err != nil || len(files) != 1 || files[0] != "sysroot:usr/include/stdio.h" {
		t.Errorf("by a toolchain identity: %v %v", files, err)
	}
}

// TestTheGraphIsInItsDocumentedOrder: creation information, the document,
// the Sbom, then each group sorted, then the relationships by number.
func TestTheGraphIsInItsDocumentedOrder(t *testing.T) {
	graph := graphOf(t, renderAllFields(t))
	order := map[string]int{
		"CreationInfo": 0, "SpdxDocument": 1, "software_Sbom": 2, "Organization": 3, "Tool": 4,
		"software_Package": 5, "software_File": 6, "security_Vulnerability": 7,
		"simplelicensing_SimpleLicensingText": 8, "simplelicensing_LicenseExpression": 9,
		"Relationship": 10, "LifecycleScopedRelationship": 10, classVexNotAffected: 10,
	}
	previousGroup, previousID := -1, ""
	for _, node := range graph {
		group, known := order[node["type"].(string)]
		if !known {
			t.Fatalf("unexpected class %v", node["type"])
		}
		id, _ := node["spdxId"].(string)
		if group < previousGroup || group == previousGroup && group < 10 && id < previousID {
			t.Fatalf("%s is out of order", id)
		}
		previousGroup, previousID = group, id
	}
}

// TestACpeIsIdentifiedByTheVersionItIsWrittenIn: a CPE 2.2 URI and a CPE 2.3
// formatted string are different identifier types in 3.0.1, and stating one as
// the other tells a consumer to parse it with the wrong grammar.
func TestACpeIsIdentifiedByTheVersionItIsWrittenIn(t *testing.T) {
	identifiers := packageIdentifiers(mapping.Package{
		PURL: "pkg:generic/zlib@1.3",
		CPEs: []string{"cpe:2.3:a:zlib:zlib:1.3:*:*:*:*:*:*:*", "cpe:/a:zlib:zlib:1.3"},
	})
	want := []externalIdentifier{
		{Type: "ExternalIdentifier", ExternalIdentifierType: "cpe22", Identifier: "cpe:/a:zlib:zlib:1.3"},
		{Type: "ExternalIdentifier", ExternalIdentifierType: "cpe23", Identifier: "cpe:2.3:a:zlib:zlib:1.3:*:*:*:*:*:*:*"},
		{Type: "ExternalIdentifier", ExternalIdentifierType: "packageUrl", Identifier: "pkg:generic/zlib@1.3"},
	}
	if !reflect.DeepEqual(identifiers, want) {
		t.Errorf("identifiers = %+v\nwant %+v", identifiers, want)
	}
}

// TestAnUnlistedExceptionIsDefinedAndMapped: an exception the list does not
// carry is an AdditionRef sbomb mints, defined by a SimpleLicensingText and
// mapped by the expression's customIdToUri, so that the listed licence before
// WITH stays machine-readable and the document passes both tiers.
func TestAnUnlistedExceptionIsDefinedAndMapped(t *testing.T) {
	document := sbomwritertest.AllFields()
	document.Components[0].Licenses = []domain.LicenseFinding{{Expression: "GPL-2.0-or-later WITH Acme-linking-exception"}}
	data := render(t, document, sbomwritertest.AllFieldsOptions())
	expression := nodeAt(t, data, "license:GPL-2.0-or-later WITH AdditionRef-sbomb-Acme-linking-exception")
	entries, _ := expression["simplelicensing_customIdToUri"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["key"] != "AdditionRef-sbomb-Acme-linking-exception" ||
		entries[0].(map[string]any)["value"] != iriOf("license-text:AdditionRef-sbomb-Acme-linking-exception") {
		t.Errorf("customIdToUri = %v", entries)
	}
	if expression["simplelicensing_licenseListVersion"] != licenselist.Version {
		t.Error("the listed licence keeps its list version")
	}
	text := nodeAt(t, data, "license-text:AdditionRef-sbomb-Acme-linking-exception")
	if text["type"] != "simplelicensing_SimpleLicensingText" || text["name"] != "Acme-linking-exception" {
		t.Errorf("definition = %v", text)
	}
	// What sbomb saw is an exception name after WITH, never a licence text:
	// the comment must not send a reader to licenseTextInSBOM or a licence
	// file that was never there.
	if comment, _ := text["comment"].(string); !strings.Contains(comment, "licence exception name, after WITH") || strings.Contains(comment, "licenseTextInSBOM") {
		t.Errorf("comment = %q, want it to call the definition an exception name observed after WITH", comment)
	}
}

// TestAnAdvisoryThatIsNoCVEIsNotLabelledOne: sbom.yml accepts any identifier
// for an exclusion, so a GHSA advisory reaches the renderer as well. Labelling
// it "cve" would send a consumer to resolve it against the CVE list.
func TestAnAdvisoryThatIsNoCVEIsNotLabelledOne(t *testing.T) {
	data := renderAllFields(t)
	for id, want := range map[string]string{"CVE-2024-0001": "cve", "GHSA-sent-inel-0001": "securityOther"} {
		vulnerability := nodeAt(t, data, "vulnerability:"+id)
		identifiers, _ := vulnerability["externalIdentifier"].([]any)
		if len(identifiers) != 1 {
			t.Fatalf("%s: identifiers = %v", id, identifiers)
		}
		identifier := identifiers[0].(map[string]any)
		if identifier["externalIdentifierType"] != want || identifier["identifier"] != id {
			t.Errorf("%s: identifier = %v, want type %s", id, identifier, want)
		}
	}
}
