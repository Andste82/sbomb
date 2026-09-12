package cyclonedx

import (
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

func copyrightDocument() *sbomwriter.Document {
	return &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "app", Type: "application"},
		Components: []domain.Component{
			{
				ID: "component:mit-lib", Name: "mit-lib", Type: "library",
				Licenses: []domain.LicenseFinding{{Expression: "MIT", SPDXID: "MIT", Evidence: "component-level"}},
				// Handed over unsorted on purpose: section 29 is the
				// document's promise, not the caller's.
				Copyrights: []domain.CopyrightStatement{
					{Text: "Copyright (c) 2026 Zebra Holder", File: domain.FileID{Anchor: "project", RelPath: "dep/mit-lib/z.c"}},
					{Text: "Copyright (c) 2019-2026 Acme Inc.", File: domain.FileID{Anchor: "project", RelPath: "dep/mit-lib/LICENSE"}},
				},
			},
			{
				ID: "component:curated", Name: "curated", Type: "library",
				Copyright: "Copyright (c) 2026 Reviewed By Hand",
			},
		},
		Relations: []sbomwriter.Relation{{From: "product", To: []string{"component:mit-lib", "component:curated"}}},
		Run:       sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", ToolVersion: "0.0.0-test"},
	}
}

// evidence.copyright[] and component.copyright exist at 1.6 and at 1.7, so
// neither is gated on a version. The statements are ordered by text (section
// 29) and carried unchanged, and the concluded field stays empty for the
// component nobody concluded anything about.
func TestCopyrightIsWrittenAsEvidenceAndOrderedByText(t *testing.T) {
	for _, specVersion := range []string{Version16, Version17} {
		t.Run(specVersion, func(t *testing.T) {
			data, err := MarshalDocument(copyrightDocument(), sbomwriter.Options{
				SpecVersion: specVersion, Reproducible: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(data); err != nil {
				t.Fatalf("document is not valid at %s: %v", specVersion, err)
			}

			observed := componentNamed(t, data, "mit-lib")
			if observed.Evidence == nil || len(observed.Evidence.Copyright) != 2 {
				t.Fatalf("evidence.copyright = %#v, want two statements", observed.Evidence)
			}
			if observed.Evidence.Copyright[0].Text != "Copyright (c) 2019-2026 Acme Inc." ||
				observed.Evidence.Copyright[1].Text != "Copyright (c) 2026 Zebra Holder" {
				t.Errorf("evidence.copyright = %#v, want them ordered by text", observed.Evidence.Copyright)
			}
			// An observation is never a conclusion: section 22.4, in the two
			// places CycloneDX provides for it.
			if observed.Copyright != "" {
				t.Errorf("component.copyright = %q, and nothing was curated", observed.Copyright)
			}

			curated := componentNamed(t, data, "curated")
			if curated.Copyright != "Copyright (c) 2026 Reviewed By Hand" {
				t.Errorf("component.copyright = %q, want the curated value", curated.Copyright)
			}
			if curated.Evidence != nil && len(curated.Evidence.Copyright) != 0 {
				t.Errorf("the curated value became evidence: %#v", curated.Evidence.Copyright)
			}
		})
	}
}

// A component that states nothing carries no empty array and no empty string:
// an absent field is how CycloneDX says nothing was found, and a document full
// of empty structures is harder to read for no gain.
func TestAComponentWithoutStatementsCarriesNoCopyrightAtAll(t *testing.T) {
	document := copyrightDocument()
	document.Components = []domain.Component{{ID: "component:bare", Name: "bare", Type: "library"}}
	document.Relations = []sbomwriter.Relation{{From: "product", To: []string{"component:bare"}}}

	data, err := MarshalDocument(document, sbomwriter.Options{SpecVersion: Version16, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	bare := componentNamed(t, data, "bare")
	if bare.Copyright != "" || (bare.Evidence != nil && len(bare.Evidence.Copyright) != 0) {
		t.Errorf("component = %#v, want no copyright fields", bare)
	}
	if string(data) == "" {
		t.Fatal("no document")
	}
}
