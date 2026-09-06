package cyclonedx

import (
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
	writer, err := sbomwriter.Get("cyclonedx-json", "1.6")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if writer.ID() != "cyclonedx-json" {
		t.Errorf("ID() = %q", writer.ID())
	}
	if _, err := sbomwriter.Get("cyclonedx-json", "1.7"); err == nil {
		t.Error("an unsupported specification version should be rejected")
	}
	if _, err := sbomwriter.Get("spdx-json", ""); err == nil {
		t.Error("an unregistered format should be rejected")
	}
}
