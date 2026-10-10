package cyclonedx

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func BenchmarkMarshalBOM(b *testing.B) {
	document := BOM{BomFormat: "CycloneDX", SpecVersion: "1.6", Version: 1, Components: make([]Component, 1000)}
	for index := range document.Components {
		document.Components[index] = Component{Type: "file", Name: "source", BomRef: "file:project:src/" + strings.Repeat("x", index%20), Properties: []Property{{Name: "sbomb:file:class", Value: "source"}}}
	}
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := MarshalBOM(document); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSourceDateEpoch(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	out, err := MarshalEmpty("", false)
	if err != nil {
		t.Fatalf("MarshalEmpty() error = %v", err)
	}
	if !strings.Contains(out, `"timestamp": "2023-11-14T22:13:20Z"`) {
		t.Fatalf("timestamp did not use SOURCE_DATE_EPOCH: %s", out)
	}
}

func TestReproducibleSerialIsUUID(t *testing.T) {
	serial := ReproducibleSerialNumber(BOM{BomFormat: "CycloneDX", SpecVersion: "1.6", Version: 1})
	if len(serial) != len("urn:uuid:123e4567-e89b-12d3-a456-426614174000") || !strings.HasPrefix(serial, "urn:uuid:") {
		t.Fatalf("serial = %q, want UUID format", serial)
	}
}

func TestEmptyDocumentReproducible(t *testing.T) {
	for _, specVersion := range supportedVersions {
		t.Run(specVersion, func(t *testing.T) {
			out1, err := MarshalEmpty(specVersion, true)
			if err != nil {
				t.Fatalf("MarshalEmpty() error = %v", err)
			}
			out2, err := MarshalEmpty(specVersion, true)
			if err != nil {
				t.Fatalf("MarshalEmpty() second call error = %v", err)
			}
			if !strings.Contains(out1, "\"bomFormat\": \"CycloneDX\"") {
				t.Fatalf("MarshalEmpty() missing bomFormat: %s", out1)
			}
			if !strings.Contains(out1, "\"specVersion\": \""+specVersion+"\"") {
				t.Fatalf("MarshalEmpty(%s) did not write that version: %s", specVersion, out1)
			}
			if out1 != out2 {
				t.Fatalf("reproducible output changed between calls:\n%s\n---\n%s", out1, out2)
			}
			if strings.Contains(out1, "\"timestamp\"") {
				t.Fatalf("MarshalEmpty() reproducible mode should omit timestamp: %s", out1)
			}
			// The schema layer only: the empty document has no root
			// component, which the semantic layer requires of a real one.
			if err := ValidateAgainstSchema([]byte(out1)); err != nil {
				t.Fatalf("MarshalEmpty(%s) fails its own schema: %v", specVersion, err)
			}
		})
	}
}

// TestMarshalEmptyRejectsAnUnknownVersion pins that a version this build
// cannot write is a refusal rather than a silent downgrade to the default.
func TestMarshalEmptyRejectsAnUnknownVersion(t *testing.T) {
	if _, err := MarshalEmpty("1.5", true); err == nil {
		t.Fatal("MarshalEmpty accepted CycloneDX 1.5")
	}
}

// TestReproducibleSerialDiffersByVersion pins that the specification version
// is part of the document's identity: two documents, two serial numbers.
func TestReproducibleSerialDiffersByVersion(t *testing.T) {
	at16, err := MarshalEmpty(Version16, true)
	if err != nil {
		t.Fatal(err)
	}
	at17, err := MarshalEmpty(Version17, true)
	if err != nil {
		t.Fatal(err)
	}
	serial16 := serialNumberOf(t, at16)
	serial17 := serialNumberOf(t, at17)
	if serial16 == serial17 {
		t.Fatalf("1.6 and 1.7 share the serial number %s", serial16)
	}
}

func serialNumberOf(t *testing.T, document string) string {
	t.Helper()
	var parsed BOM
	if err := json.Unmarshal([]byte(document), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.SerialNumber == "" {
		t.Fatalf("document has no serial number: %s", document)
	}
	return parsed.SerialNumber
}

func TestValidateDocumentRejectsDanglingDependencyRef(t *testing.T) {
	data := []byte(`{
		"bomFormat": "CycloneDX",
		"specVersion": "1.6",
		"version": 1,
		"serialNumber": "urn:uuid:123e4567-e89b-12d3-a456-426614174000",
		"metadata": {
			"tools": [{"name": "sbomb", "version": "test"}],
			"timestamp": "2026-09-04T00:00:00Z"
		},
		"components": [{"type": "application", "name": "app", "bom-ref": "component:app"}],
		"dependencies": [{"ref": "component:missing", "dependsOn": ["component:app"]}]
	}`)
	if err := ValidateDocument(data); err == nil {
		t.Fatal("ValidateDocument() accepted a dangling dependency reference")
	}
}

// thirdPartyDocument is conformant CycloneDX the way other tools write it:
// tools in the object form, nested components, a service, no dependencies
// entry for every component, and properties in a namespace of their own.
const thirdPartyDocument = `{
	"bomFormat": "CycloneDX",
	"specVersion": "%s",
	"version": 1,
	"serialNumber": "urn:uuid:123e4567-e89b-12d3-a456-426614174000",
	"metadata": {
		"timestamp": "2026-09-04T00:00:00Z",
		"tools": {
			"components": [{"type": "application", "name": "other-tool", "version": "2.1", "supplier": {"name": "Other Inc."}}],
			"services": [{"name": "scanner", "version": "1"}]
		},
		"component": {"type": "application", "name": "app", "bom-ref": "app"}
	},
	"components": [{
		"type": "library", "name": "outer", "bom-ref": "outer",
		"properties": [{"name": "acme:origin", "value": "vendored"}],
		"components": [{"type": "library", "name": "inner", "bom-ref": "inner", "purl": "pkg:generic/inner@1"}]
	}],
	"services": [{"bom-ref": "api", "name": "api"}],
	"dependencies": [{"ref": "app", "dependsOn": ["outer", "inner", "api"]}]
}`

// Q23: validate judges a document against the specification it declares,
// not against how sbomb writes, so a conformant third-party document passes.
func TestValidateAcceptsAConformantThirdPartyDocument(t *testing.T) {
	for _, version := range supportedVersions {
		data := []byte(fmt.Sprintf(thirdPartyDocument, version))
		if err := Validate(data); err != nil {
			t.Errorf("CycloneDX %s: Validate() = %v, want a conformant document to pass", version, err)
		}
		if err := CheckDocument(data); err == nil {
			t.Errorf("CycloneDX %s: CheckDocument() accepted a document sbomb would not write", version)
		}
	}
}

func TestToolsReadInBothForms(t *testing.T) {
	var object Metadata
	if err := json.Unmarshal([]byte(`{"tools": {"components": [{"name": "a", "version": "1", "group": "g"}], "services": [{"name": "b", "provider": {"name": "P"}}]}}`), &object); err != nil {
		t.Fatal(err)
	}
	want := Tools{{Vendor: "g", Name: "a", Version: "1"}, {Vendor: "P", Name: "b"}}
	if !reflect.DeepEqual(object.Tools, want) {
		t.Errorf("object form = %#v, want %#v", object.Tools, want)
	}
	var array Metadata
	if err := json.Unmarshal([]byte(`{"tools": [{"vendor": "v", "name": "a", "version": "1"}]}`), &array); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(array.Tools, Tools{{Vendor: "v", Name: "a", Version: "1"}}) {
		t.Errorf("array form = %#v", array.Tools)
	}
}

func TestValidateRejectsADuplicateNestedBomRef(t *testing.T) {
	data := []byte(strings.Replace(fmt.Sprintf(thirdPartyDocument, Version16), `"bom-ref": "inner"`, `"bom-ref": "outer"`, 1))
	if err := ValidateDocument(data); err == nil {
		t.Fatal("ValidateDocument() accepted a bom-ref declared twice")
	}
}
