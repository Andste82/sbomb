package cyclonedx

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

// The bytes a component carries, as section 22.9 retains them. CRLF and a
// trailing blank line are in there on purpose: a reproduced notice that was
// re-wrapped is not the notice the licence said to reproduce, and base64 is
// the only way a JSON document can promise that.
const retainedMIT = "MIT License\r\n\r\nCopyright (c) 2026 Example Holder\r\n\r\nPermission is hereby granted...\r\n"

func licenseTextDocument() *sbomwriter.Document {
	return &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "app", Type: "application"},
		Components: []domain.Component{
			{
				ID: "component:mit-lib", Name: "mit-lib", Type: "library",
				Licenses: []domain.LicenseFinding{{Expression: "MIT", SPDXID: "MIT", Evidence: "component-level"}},
				LicenseArtifacts: []domain.LicenseArtifact{
					{
						Kind:       domain.LicenseArtifactLicense,
						File:       domain.FileID{Anchor: "project", RelPath: "dep/mit-lib/LICENSE"},
						SHA256:     strings.Repeat("b", 64),
						Bytes:      []byte(retainedMIT),
						DetectedID: "MIT",
						Technique:  "spdx-digest",
					},
					{
						Kind:   domain.LicenseArtifactNotice,
						File:   domain.FileID{Anchor: "project", RelPath: "dep/mit-lib/NOTICE"},
						SHA256: strings.Repeat("c", 64),
						Bytes:  []byte("This product includes software written by somebody else.\n"),
					},
				},
			},
		},
		Relations: []sbomwriter.Relation{{From: "product", To: []string{"component:mit-lib"}}},
		Run:       sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", ToolVersion: "0.0.0-test"},
	}
}

// The round trip, at both specification versions, through the schema
// validation the writer performs in process: what a recipient decodes is what
// the component carried, byte for byte.
func TestARetainedLicenceTextSurvivesTheDocumentUnchanged(t *testing.T) {
	for _, specVersion := range []string{Version16, Version17} {
		t.Run(specVersion, func(t *testing.T) {
			data, err := MarshalDocument(licenseTextDocument(), sbomwriter.Options{
				SpecVersion: specVersion, LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			// license.text and license.acknowledgement exist at 1.6 and at
			// 1.7, so this is not a 1.7 feature and is not gated on one.
			if err := Validate(data); err != nil {
				t.Fatalf("document is not valid at %s: %v", specVersion, err)
			}
			component := componentNamed(t, data, "mit-lib")
			if component.Evidence == nil || len(component.Evidence.Licenses) != 1 {
				t.Fatalf("evidence.licenses = %#v, want the one retained grant", component.Evidence)
			}
			entry := component.Evidence.Licenses[0].License
			if entry == nil || entry.ID != "MIT" {
				t.Fatalf("licence entry = %#v, want the identifier beside the text", entry)
			}
			if entry.Text == nil {
				t.Fatal("the retained text is not in the document")
			}
			if entry.Text.ContentType != "text/plain" || entry.Text.Encoding != "base64" {
				t.Errorf("attachment = %#v, want text/plain + base64", entry.Text)
			}
			decoded, err := base64.StdEncoding.DecodeString(entry.Text.Content)
			if err != nil {
				t.Fatalf("the content is not base64: %v", err)
			}
			if string(decoded) != retainedMIT {
				t.Errorf("decoded text = %q, want the retained bytes", decoded)
			}
			// The text came out of the component's own file, so the claim is
			// declared rather than concluded.
			if entry.Acknowledgement != "declared" {
				t.Errorf("acknowledgement = %q, want declared", entry.Acknowledgement)
			}
			// A NOTICE is retained for reproduction and is not licence
			// evidence; requirement R4 turns on the two not being confused.
			if strings.Contains(string(data), base64.StdEncoding.EncodeToString([]byte("This product includes"))) {
				t.Error("the NOTICE reached evidence.licenses")
			}
		})
	}
}

// Section 28.7: with the setting off -- which is the default -- the document
// keeps the size it has today. Base64 inflates a licence by a third, and an
// SBOM must not grow because somebody asked for an attribution document on
// the side.
func TestTheDocumentCarriesNoLicenceTextUnlessAsked(t *testing.T) {
	for _, licenseText := range []string{"", sbomwriter.LicenseTextOff} {
		data, err := MarshalDocument(licenseTextDocument(), sbomwriter.Options{
			SpecVersion: Version16, LicenseText: licenseText, Reproducible: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "\"text\"") || strings.Contains(string(data), "acknowledgement") {
			t.Errorf("licenseTextInSBOM=%q wrote a licence text into the document", licenseText)
		}
		if component := componentNamed(t, data, "mit-lib"); component.Evidence != nil {
			t.Errorf("evidence = %#v, want none", component.Evidence)
		}
	}
}

// A curated value is somebody's conclusion even when the bytes beside it are
// the component's own, and CycloneDX has a word for the difference.
func TestACuratedIdentifierIsAcknowledgedAsConcluded(t *testing.T) {
	document := licenseTextDocument()
	document.Components[0].Licenses = []domain.LicenseFinding{{
		Expression: "MIT", SPDXID: "MIT", Evidence: "component-level", Source: "curated",
	}}
	data, err := MarshalDocument(document, sbomwriter.Options{
		SpecVersion: Version17, LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(data); err != nil {
		t.Fatalf("document is not valid: %v", err)
	}
	entry := componentNamed(t, data, "mit-lib").Evidence.Licenses[0].License
	if entry.Acknowledgement != "concluded" {
		t.Errorf("acknowledgement = %q, want concluded", entry.Acknowledgement)
	}
}

// A text none of the techniques of section 22.3 recognized is still the
// deliverable, and no canonical SPDX text is ever substituted for it. It is
// written under the marker section 28.7 uses for "no reliable assertion", so
// that the document says both things at once: here are the bytes, and nobody
// knows which licence they are.
func TestAnUnrecognizedTextIsCarriedUnderNoAssertion(t *testing.T) {
	document := licenseTextDocument()
	document.Components[0].LicenseArtifacts = []domain.LicenseArtifact{{
		Kind:   domain.LicenseArtifactLicense,
		File:   domain.FileID{Anchor: "project", RelPath: "dep/mit-lib/LICENSE"},
		SHA256: strings.Repeat("d", 64),
		Bytes:  []byte("Terms nobody has catalogued.\n"),
	}}
	data, err := MarshalDocument(document, sbomwriter.Options{
		SpecVersion: Version16, LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(data); err != nil {
		t.Fatalf("document is not valid: %v", err)
	}
	entry := componentNamed(t, data, "mit-lib").Evidence.Licenses[0].License
	if entry.Name != "NOASSERTION" || entry.ID != "" {
		t.Errorf("licence entry = %#v, want NOASSERTION with no identifier", entry)
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Text.Content)
	if err != nil || string(decoded) != "Terms nobody has catalogued.\n" {
		t.Errorf("decoded text = %q (%v), want the retained bytes", decoded, err)
	}
}

// An SPDX-License-Identifier line in somebody's file may name anything at all,
// including a LicenseRef. `license.id` is an enum in both schemas, so writing
// such a value there would make the document invalid -- the run would fail
// rather than the licence be reported. It goes in the free-text field instead,
// and the text goes with it.
func TestAnIdentifierTheSPDXListDoesNotCarryGoesInTheNameField(t *testing.T) {
	for _, detected := range []string{"LicenseRef-acme", "MIT OR Apache-2.0"} {
		document := licenseTextDocument()
		document.Components[0].LicenseArtifacts = []domain.LicenseArtifact{{
			Kind:       domain.LicenseArtifactLicense,
			File:       domain.FileID{Anchor: "project", RelPath: "dep/mit-lib/LICENSE"},
			SHA256:     strings.Repeat("e", 64),
			Bytes:      []byte("Terms of " + detected + ".\n"),
			DetectedID: detected,
			Technique:  "spdx-identifier",
		}}
		data, err := MarshalDocument(document, sbomwriter.Options{
			SpecVersion: Version16, LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true,
		})
		if err != nil {
			t.Fatalf("%s: %v", detected, err)
		}
		if err := Validate(data); err != nil {
			t.Fatalf("%s: document is not valid: %v", detected, err)
		}
		entry := componentNamed(t, data, "mit-lib").Evidence.Licenses[0].License
		if entry.ID != "" || entry.Name != detected {
			t.Errorf("%s: licence entry = %#v, want it under name", detected, entry)
		}
		if entry.Text == nil {
			t.Errorf("%s: the text was dropped with the identifier", detected)
		}
	}
}

// An observation of a licence and a retained text of that same licence are one
// statement about one file. The text attaches to the observation rather than
// adding a second entry claiming the licence twice.
func TestARetainedTextAttachesToAnObservationOfTheSameLicence(t *testing.T) {
	document := licenseTextDocument()
	document.Components[0].LicenseEvidence = []domain.LicenseFinding{
		{Expression: "MIT", SPDXID: "MIT", Evidence: "component-level"},
	}
	data, err := MarshalDocument(document, sbomwriter.Options{
		SpecVersion: Version16, LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(data); err != nil {
		t.Fatalf("document is not valid: %v", err)
	}
	licenses := componentNamed(t, data, "mit-lib").Evidence.Licenses
	if len(licenses) != 1 {
		t.Fatalf("evidence.licenses = %#v, want one entry", licenses)
	}
	if licenses[0].License.Text == nil {
		t.Error("the observation did not receive the retained text")
	}
}
