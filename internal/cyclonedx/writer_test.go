package cyclonedx

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

func sampleDocument() *sbomwriter.Document {
	return &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "firmware", Type: "firmware", Version: "1.2.3", Supplier: "Example Org"},
		Components: []domain.Component{
			{ID: "component:project", Name: "firmware", Type: "application", Scope: "project"},
			{ID: "pkg:conan/mbedtls", Name: "mbedtls", Type: "library", Version: "3.5.0", Scope: "third-party"},
		},
		Files: []domain.UsedFile{
			{ID: domain.FileID{Anchor: "project", RelPath: "main.c"}, Class: domain.FileClassSource,
				Hashes: map[string]string{"SHA-256": strings.Repeat("a", 64)}},
			{ID: domain.FileID{Anchor: "pkg:conan/mbedtls", RelPath: "aes.h"}, Class: domain.FileClassHeader},
		},
		Relations: []sbomwriter.Relation{
			{From: "product", To: []string{"component:project", "pkg:conan/mbedtls"}},
			{From: "component:project", To: []string{"project:main.c"}},
			{From: "pkg:conan/mbedtls", To: []string{"pkg:conan/mbedtls:aes.h"}},
		},
		Run: sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", ToolVersion: "0.0.0-test", Timestamp: "2026-01-01T00:00:00Z"},
	}
}

func buildSample(t *testing.T, reproducible bool) BOM {
	t.Helper()
	bom, err := (Writer{}).Build(sampleDocument(), sbomwriter.Options{SpecVersion: "1.6", Reproducible: reproducible})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	return bom
}

func TestRootComponentIsInMetadataAndNotRepeated(t *testing.T) {
	bom := buildSample(t, true)
	if bom.Metadata == nil || bom.Metadata.Component == nil {
		t.Fatal("metadata.component is missing")
	}
	if got := bom.Metadata.Component.BomRef; got != "product:firmware" {
		t.Errorf("root bom-ref = %q, want product:firmware", got)
	}
	for _, component := range bom.Components {
		if component.BomRef == bom.Metadata.Component.BomRef {
			t.Error("the root component is repeated in components[] (section 28.2)")
		}
	}
}

func TestDependencyGraphIsClosed(t *testing.T) {
	bom := buildSample(t, true)

	refs := map[string]bool{bom.Metadata.Component.BomRef: true}
	for _, component := range bom.Components {
		refs[component.BomRef] = true
	}
	entries := map[string]int{}
	for _, dependency := range bom.Dependencies {
		entries[dependency.Ref]++
		for _, target := range dependency.DependsOn {
			if !refs[target] {
				t.Errorf("dependency %q points at unknown ref %q", dependency.Ref, target)
			}
		}
	}
	// Section 28.5: exactly one entry per ref, even with no dependencies.
	for ref := range refs {
		switch entries[ref] {
		case 1:
		case 0:
			t.Errorf("ref %q has no dependencies entry", ref)
		default:
			t.Errorf("ref %q has %d dependencies entries, want 1", ref, entries[ref])
		}
	}
}

func TestDependencyCascadeFollowsTheProductStructure(t *testing.T) {
	bom := buildSample(t, true)
	byRef := map[string][]string{}
	for _, dependency := range bom.Dependencies {
		byRef[dependency.Ref] = dependency.DependsOn
	}

	product := byRef["product:firmware"]
	if len(product) != 2 {
		t.Fatalf("product depends on %v, want the two grouping components", product)
	}
	if got := byRef["component:firmware"]; len(got) != 1 || got[0] != "file:project:main.c" {
		t.Errorf("project component depends on %v, want its file", got)
	}
	if got := byRef["file:project:main.c"]; len(got) != 0 {
		t.Errorf("a file component depends on %v, want nothing (section 28.5)", got)
	}
}

func TestBSIPropertiesArePresentOnEveryComponent(t *testing.T) {
	bom := buildSample(t, true)
	required := []string{
		"sbomb:cdx:archiveProperty",
		"sbomb:cdx:executableProperty",
		"sbomb:cdx:structuredProperty",
	}
	check := func(name string, properties []Property) {
		present := map[string]bool{}
		for _, property := range properties {
			present[property.Name] = true
		}
		for _, want := range required {
			if !present[want] {
				t.Errorf("%s is missing %s (BSI TR-03183-2, section 1.5(3))", name, want)
			}
		}
	}
	check("metadata.component", bom.Metadata.Component.Properties)
	for _, component := range bom.Components {
		check(component.BomRef, component.Properties)
	}
}

func TestSourceFilesAreStructuredAndFirmwareIsExecutable(t *testing.T) {
	bom := buildSample(t, true)
	value := func(properties []Property, name string) string {
		for _, property := range properties {
			if property.Name == name {
				return property.Value
			}
		}
		return ""
	}
	if got := value(bom.Metadata.Component.Properties, "sbomb:cdx:executableProperty"); got != "executable" {
		t.Errorf("firmware executableProperty = %q, want executable", got)
	}
	for _, component := range bom.Components {
		if component.Type != "file" {
			continue
		}
		if got := value(component.Properties, "sbomb:cdx:structuredProperty"); got != "structured" {
			t.Errorf("%s structuredProperty = %q, want structured", component.BomRef, got)
		}
	}
}

func TestSupplierAndVersionReachTheDocument(t *testing.T) {
	bom := buildSample(t, true)
	if bom.Metadata.Component.Version != "1.2.3" {
		t.Errorf("root version = %q, want 1.2.3", bom.Metadata.Component.Version)
	}
	if bom.Metadata.Component.Supplier == nil || bom.Metadata.Component.Supplier.Name != "Example Org" {
		t.Errorf("root supplier = %v, want Example Org", bom.Metadata.Component.Supplier)
	}
}

func TestBuiltDocumentPassesBothValidationLayers(t *testing.T) {
	bom := buildSample(t, true)
	bom.SerialNumber = ReproducibleSerialNumber(bom)
	serialized, err := MarshalBOM(bom)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate([]byte(serialized)); err != nil {
		t.Fatalf("the writer produced a document its own validator rejects: %v", err)
	}
}

func TestSchemaValidationRejectsAnInvalidComponentType(t *testing.T) {
	bom := buildSample(t, true)
	serialized, err := MarshalBOM(bom)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(serialized), &document); err != nil {
		t.Fatal(err)
	}
	document["components"].([]any)[0].(map[string]any)["type"] = "not-a-type"
	broken, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateAgainstSchema(broken)
	if err == nil {
		t.Fatal("an invalid component type passed schema validation")
	}
	if !strings.Contains(err.Error(), "/components/0/type") {
		t.Errorf("the error does not name the offending location: %v", err)
	}
}

func TestSemanticValidationRejectsAMissingRootComponent(t *testing.T) {
	bom := buildSample(t, true)
	bom.Metadata.Component = nil
	serialized, err := MarshalBOM(bom)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument([]byte(serialized)); err == nil {
		t.Fatal("a document without a root component passed semantic validation")
	}
}

func TestSemanticValidationRejectsAnIncompleteDependencyGraph(t *testing.T) {
	bom := buildSample(t, true)
	bom.Dependencies = bom.Dependencies[:1]
	serialized, err := MarshalBOM(bom)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateDocument([]byte(serialized))
	if err == nil {
		t.Fatal("a document missing dependency entries passed semantic validation")
	}
	if !strings.Contains(err.Error(), "28.5") {
		t.Errorf("the error does not explain which rule was broken: %v", err)
	}
}

func TestReproducibleModeOmitsTheTimestamp(t *testing.T) {
	if got := buildSample(t, true); got.Metadata.Timestamp != "" {
		t.Errorf("reproducible mode kept the timestamp %q", got.Metadata.Timestamp)
	}
	if got := buildSample(t, false); got.Metadata.Timestamp == "" {
		t.Error("normal mode dropped the timestamp, which the CRA requires")
	}
}

func TestWriterIsRegisteredUnderItsFormatIdentifier(t *testing.T) {
	for _, specVersion := range supportedVersions {
		writer, err := sbomwriter.Get("cyclonedx-json", specVersion)
		if err != nil {
			t.Fatalf("Get(%s) error = %v", specVersion, err)
		}
		if writer.ID() != "cyclonedx-json" {
			t.Errorf("ID() = %q", writer.ID())
		}
	}
	if _, err := sbomwriter.Get("cyclonedx-json", "1.5"); err == nil {
		t.Error("an unsupported specification version should be rejected")
	}
	if _, err := sbomwriter.Get("spdx-json", ""); err == nil {
		t.Error("an unregistered format should be rejected")
	}
}

// TestResolveFillsInTheDefaultVersion pins the seam gap this closes: asking
// for a format without naming a version yields a version, and it is the one
// the writer states rather than whichever happens to be first in the slice.
func TestResolveFillsInTheDefaultVersion(t *testing.T) {
	writer, specVersion, err := sbomwriter.Resolve("cyclonedx-json", "")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if specVersion != Version16 {
		t.Errorf("default version = %q, want %q", specVersion, Version16)
	}
	if specVersion != writer.DefaultVersion() {
		t.Errorf("Resolve gave %q but the writer's default is %q", specVersion, writer.DefaultVersion())
	}
	if _, _, err := sbomwriter.Resolve("cyclonedx-json", "1.5"); err == nil {
		t.Error("Resolve accepted a version the writer cannot emit")
	}
}

// TestDetectReadsTheDocumentsOwnClaims pins that `validate` can identify a
// document without being told what it is -- including a version this build
// cannot write, so that it can say what it is looking at.
func TestDetectReadsTheDocumentsOwnClaims(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		data    string
		version string
		ok      bool
	}{
		{"1.6", `{"bomFormat":"CycloneDX","specVersion":"1.6"}`, "1.6", true},
		{"1.7", `{"bomFormat":"CycloneDX","specVersion":"1.7"}`, "1.7", true},
		{"a version this build cannot write", `{"bomFormat":"CycloneDX","specVersion":"1.4"}`, "1.4", true},
		{"another format", `{"spdxVersion":"SPDX-2.3"}`, "", false},
		{"not JSON at all", `<?xml version="1.0"?>`, "", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			version, ok := (Writer{}).Detect([]byte(testCase.data))
			if ok != testCase.ok || version != testCase.version {
				t.Errorf("Detect() = %q, %v; want %q, %v", version, ok, testCase.version, testCase.ok)
			}
		})
	}

	writer, version, err := sbomwriter.DetectFormat([]byte(`{"bomFormat":"CycloneDX","specVersion":"1.7"}`))
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if writer.ID() != "cyclonedx-json" || version != Version17 {
		t.Errorf("DetectFormat() = %q %q", writer.ID(), version)
	}
	if _, _, err := sbomwriter.DetectFormat([]byte(`{"spdxVersion":"SPDX-2.3"}`)); err == nil {
		t.Error("DetectFormat accepted a document in no registered format")
	}
}

// TestACompoundExpressionInEvidenceNeedsSeventeen is the one place where 1.7
// lets sbomb say something 1.6 forbids (deviation D19).
//
// At 1.6, licenseChoice is a choice: a list of licence objects, or a tuple of
// exactly one expression. So an observation that did carry a relation between
// licences -- an SPDX-License-Identifier line reading "MIT OR Apache-2.0" --
// had to be flattened to the identifier of one of them as soon as a second
// observation stood beside it. At 1.7 one array may mix the two forms.
//
// The second half of the test is what makes the first half mean anything: the
// mixed array is checked against the 1.6 schema and must be rejected there.
func TestACompoundExpressionInEvidenceNeedsSeventeen(t *testing.T) {
	document := func() *sbomwriter.Document {
		return &sbomwriter.Document{
			Product: domain.Component{ID: "product", Name: "app", Type: "application"},
			Components: []domain.Component{{
				ID:       "component:mixed",
				Name:     "mixed",
				Type:     "library",
				Licenses: []domain.LicenseFinding{{Name: "NOASSERTION", Evidence: "unknown"}},
				LicenseEvidence: []domain.LicenseFinding{
					{Expression: "MIT OR Apache-2.0", SPDXID: "MIT", Name: "MIT OR Apache-2.0"},
					{Expression: "BSD-3-Clause", SPDXID: "BSD-3-Clause", Name: "BSD-3-Clause"},
				},
			}},
			Relations: []sbomwriter.Relation{{From: "product", To: []string{"component:mixed"}}},
			Run:       sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", ToolVersion: "0.0.0-test"},
		}
	}

	at16, err := MarshalDocument(document(), sbomwriter.Options{SpecVersion: Version16, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(at16); err != nil {
		t.Fatalf("the 1.6 document is not valid: %v", err)
	}
	// 1.6 flattens: two identifiers, no expression, because a list is the only
	// form that admits two entries there.
	for _, license := range evidenceLicensesOf(t, at16, "mixed") {
		if license.Expression != "" {
			t.Errorf("1.6 emitted the expression %q, which its licenseChoice forbids in a list", license.Expression)
		}
	}

	at17, err := MarshalDocument(document(), sbomwriter.Options{SpecVersion: Version17, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(at17); err != nil {
		t.Fatalf("the 1.7 document is not valid: %v", err)
	}
	observed := evidenceLicensesOf(t, at17, "mixed")
	expressions, identifiers := 0, 0
	for _, license := range observed {
		switch {
		case license.Expression != "":
			expressions++
			if license.Expression != "MIT OR Apache-2.0" {
				t.Errorf("expression = %q", license.Expression)
			}
		case license.License != nil:
			identifiers++
			// A bare identifier stays an identifier: rendering "BSD-3-Clause"
			// as an expression says nothing more and loses the distinction.
			if license.License.ID != "BSD-3-Clause" {
				t.Errorf("identifier = %+v", license.License)
			}
		}
	}
	if expressions != 1 || identifiers != 1 {
		t.Errorf("1.7 evidence = %d expression(s) and %d identifier(s), want one of each", expressions, identifiers)
	}

	// The proof that this needed 1.7: the same array, labelled 1.6, is not a
	// valid document.
	mislabelled := bytes.Replace(at17, []byte(`"specVersion": "1.7"`), []byte(`"specVersion": "1.6"`), 1)
	if err := ValidateAgainstSchema(mislabelled); err == nil {
		t.Error("the 1.6 schema accepted a licenses array mixing an expression with a licence object")
	}
}

func evidenceLicensesOf(t *testing.T, data []byte, name string) []License {
	t.Helper()
	var bom BOM
	if err := json.Unmarshal(data, &bom); err != nil {
		t.Fatal(err)
	}
	for _, component := range bom.Components {
		if component.Name != name {
			continue
		}
		if component.Evidence == nil {
			t.Fatalf("component %q carries no evidence", name)
		}
		return component.Evidence.Licenses
	}
	t.Fatalf("component %q is not in the document", name)
	return nil
}

// A licence file holding two complete texts says which licences are present
// and nothing about how they relate. CycloneDX keeps those apart: the finding
// goes to component.evidence.licenses, and the component's own licence stays
// NOASSERTION until somebody concludes it.
func TestObservedLicensesAreEvidenceNotConclusion(t *testing.T) {
	document := &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "app", Type: "application"},
		Components: []domain.Component{{
			ID:   "component:dual",
			Name: "dual",
			Type: "library",
			Licenses: []domain.LicenseFinding{{
				Name:     "NOASSERTION",
				Evidence: "unknown",
				Reason:   "license-composition-unresolved",
			}},
			LicenseEvidence: []domain.LicenseFinding{
				{Expression: "MIT", SPDXID: "MIT"},
				{Expression: "Apache-2.0", SPDXID: "Apache-2.0"},
			},
		}},
		Relations: []sbomwriter.Relation{{From: "product", To: []string{"component:dual"}}},
		Run:       sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", ToolVersion: "test"},
	}

	data, err := MarshalDocument(document, sbomwriter.Options{SpecVersion: "1.6", Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	// Both layers, because a consumer runs a schema check on this.
	if err := Validate(data); err != nil {
		t.Fatalf("validate: %v", err)
	}

	var bom BOM
	if err := json.Unmarshal(data, &bom); err != nil {
		t.Fatal(err)
	}
	var subject *Component
	for index := range bom.Components {
		if bom.Components[index].Name == "dual" {
			subject = &bom.Components[index]
		}
	}
	if subject == nil {
		t.Fatal("the component is not in the document")
	}
	if subject.Evidence == nil || len(subject.Evidence.Licenses) != 2 {
		t.Fatalf("evidence.licenses = %+v, want two", subject.Evidence)
	}
	// The observation must not have leaked into the concluded licence.
	if len(subject.Licenses) != 1 || subject.Licenses[0].License == nil || subject.Licenses[0].License.Name != "NOASSERTION" {
		t.Errorf("licenses = %+v, want a single NOASSERTION", subject.Licenses)
	}
}
