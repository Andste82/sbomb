package cyclonedx

import (
	"strings"
	"testing"
)

func TestSourceDateEpoch(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	out, err := MarshalEmpty(false)
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
	out1, err := MarshalEmpty(true)
	if err != nil {
		t.Fatalf("MarshalEmpty() error = %v", err)
	}
	out2, err := MarshalEmpty(true)
	if err != nil {
		t.Fatalf("MarshalEmpty() second call error = %v", err)
	}
	if !strings.Contains(out1, "\"bomFormat\": \"CycloneDX\"") {
		t.Fatalf("MarshalEmpty() missing bomFormat: %s", out1)
	}
	if out1 != out2 {
		t.Fatalf("reproducible output changed between calls:\n%s\n---\n%s", out1, out2)
	}
	if strings.Contains(out1, "\"timestamp\"") {
		t.Fatalf("MarshalEmpty() reproducible mode should omit timestamp: %s", out1)
	}
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
