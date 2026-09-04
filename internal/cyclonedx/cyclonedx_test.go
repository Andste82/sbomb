package cyclonedx

import (
	"strings"
	"testing"
)

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
