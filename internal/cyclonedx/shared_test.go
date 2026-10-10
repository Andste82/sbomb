package cyclonedx

import (
	"encoding/json"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbommap"
	"github.com/example/sbomb/internal/sbomwriter"
)

// TestTheSerialNumberIsTheSharedDocumentUUID: the reproducible serial is the
// document identity every format derives (sbommap.DocumentUUID), over the
// canonical BOM without its volatile fields. The goldens pin that no serial
// moved when the derivation was shared; this pins that it is shared.
func TestTheSerialNumberIsTheSharedDocumentUUID(t *testing.T) {
	bom := BOM{BomFormat: "CycloneDX", SpecVersion: Version16, Version: 1, Metadata: &Metadata{
		Timestamp: "2024-01-01T00:00:00Z",
		Tools:     []Tool{{Vendor: "sbomb", Name: "sbomb", Version: "1.0.0"}},
	}}
	canonical := bom
	metadata := *bom.Metadata
	metadata.Timestamp = ""
	canonical.Metadata = &metadata
	canonicalizeBOM(&canonical)
	body, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ReproducibleSerialNumber(bom), "urn:uuid:"+sbommap.DocumentUUID(body); got != want {
		t.Errorf("serial %s, want the shared identity %s", got, want)
	}
}

// TestAComponentCarriesTheSharedPropertySet: what the CycloneDX writer puts on
// a component is the set sbommap derives, which is what the SPDX writer puts
// on the element of the same identity.
func TestAComponentCarriesTheSharedPropertySet(t *testing.T) {
	document := sampleDocumentForSharedProperties()
	bom, err := (Writer{}).Build(document, emptyOptions())
	if err != nil {
		t.Fatal(err)
	}
	var component Component
	for _, candidate := range bom.Components {
		if candidate.BomRef == "component:zlib" {
			component = candidate
		}
	}
	want := sbommap.ComponentProperties(document.Components[0])
	want = append(want, sbommap.VCSProperties(document.Components[0].VCS)...)
	got := map[string]bool{}
	for _, property := range component.Properties {
		got[property.Name+"="+property.Value] = true
	}
	if len(got) != len(want) {
		t.Errorf("CycloneDX carries %d properties, the shared set has %d", len(got), len(want))
	}
	for _, property := range want {
		if !got[property.Name+"="+property.Value] {
			t.Errorf("missing %s=%s", property.Name, property.Value)
		}
	}
}

func emptyOptions() sbomwriter.Options { return sbomwriter.Options{SpecVersion: Version16} }

func sampleDocumentForSharedProperties() *sbomwriter.Document {
	return &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "app", Type: "application"},
		Components: []domain.Component{{
			ID: "component:zlib", Name: "zlib", Scope: "third-party", Originator: "Jean-loup Gailly",
			CVEExclusions: []domain.CVEExclusion{{CVE: "CVE-2022-37434", Reason: "inflateGetHeader is not used"}},
			VCS:           &domain.VCSRecord{URL: "https://github.com/madler/zlib", Commit: "abc", Dirty: true},
			Properties:    map[string][]string{"sbomb:component:detectedBy": {"fetchcontent"}, "finding": {"x"}},
		}},
		Relations: []sbomwriter.Relation{{From: "product", To: []string{"component:zlib"}}},
		Run:       sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", ToolVersion: "1.0.0"},
	}
}
