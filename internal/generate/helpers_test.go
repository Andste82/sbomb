package generate

import (
	"encoding/json"
	"testing"

	"github.com/example/sbomb/internal/cyclonedx"
)

// renderedBOM decodes what the run rendered. The run hands its caller bytes
// and a writer rather than a CycloneDX value, so a test that wants to look
// inside the CycloneDX document reads it back the way any consumer would.
func renderedBOM(t *testing.T, result Result) cyclonedx.BOM {
	t.Helper()
	if result.Writer == nil || result.Writer.ID() != "cyclonedx-json" {
		t.Fatalf("the run rendered %v, not CycloneDX", result.Writer)
	}
	var bom cyclonedx.BOM
	if err := json.Unmarshal(result.Rendered, &bom); err != nil {
		t.Fatalf("the rendered document does not decode: %v", err)
	}
	return bom
}
