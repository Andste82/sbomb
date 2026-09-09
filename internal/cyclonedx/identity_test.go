package cyclonedx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

func documentWithVersion(source string, confidence domain.Confidence) *sbomwriter.Document {
	return &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "app", Type: "application"},
		Components: []domain.Component{{
			ID:            "component:dep",
			Name:          "dep",
			Type:          "library",
			Version:       "3.5.0",
			VersionSource: source,
			VersionConf:   confidence,
		}},
		Relations: []sbomwriter.Relation{{From: "product", To: []string{"component:dep"}}},
		Run:       sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", ToolVersion: "0.0.0-test"},
	}
}

// TestWhereTheVersionCameFromIsPublished is the point of this evidence: a bare
// version string cannot be weighed. sbomb has always known the source and the
// confidence and kept both to itself.
func TestWhereTheVersionCameFromIsPublished(t *testing.T) {
	// Both versions, because evidence.identity predates 1.6 and section 28.1
	// gates only what a version alone can express.
	for _, specVersion := range supportedVersions {
		t.Run(specVersion, func(t *testing.T) {
			data, err := MarshalDocument(documentWithVersion("conan", domain.ConfidenceHigh),
				sbomwriter.Options{SpecVersion: specVersion, Reproducible: true})
			if err != nil {
				t.Fatal(err)
			}
			// The schema layer is what catches the two field-name bugs this
			// structure carried while nothing filled it in.
			if err := Validate(data); err != nil {
				t.Fatalf("document is not valid: %v", err)
			}
			component := componentNamed(t, data, "dep")
			if component.Evidence == nil || len(component.Evidence.Identity) != 1 {
				t.Fatalf("evidence = %+v, want one identity entry", component.Evidence)
			}
			identity := component.Evidence.Identity[0]
			if identity.Field != "version" || identity.ConcludedValue != "3.5.0" {
				t.Errorf("identity = %+v", identity)
			}
			if identity.Confidence != 0.9 {
				t.Errorf("confidence = %v, want 0.9", identity.Confidence)
			}
			if len(identity.Methods) != 1 {
				t.Fatalf("methods = %+v, want one", identity.Methods)
			}
			if identity.Methods[0].Technique != "manifest-analysis" {
				t.Errorf("technique = %q", identity.Methods[0].Technique)
			}
			// The coarse vocabulary loses which manifest; the value keeps it.
			if identity.Methods[0].Value != "conan" {
				t.Errorf("method value = %q, want the exact source", identity.Methods[0].Value)
			}
			if !strings.Contains(string(data), `"concludedValue"`) {
				t.Error(`the document does not use "concludedValue"`)
			}
		})
	}
}

// TestAVersionWithoutASourceClaimsNothing: evidence about how a value was
// established is worth less than nothing when it is invented.
func TestAVersionWithoutASourceClaimsNothing(t *testing.T) {
	data, err := MarshalDocument(documentWithVersion("", domain.ConfidenceUnknown),
		sbomwriter.Options{SpecVersion: Version17, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	if component := componentNamed(t, data, "dep"); component.Evidence != nil {
		t.Errorf("evidence was invented for a version with no recorded source: %+v", component.Evidence)
	}
}

func TestVersionSourcesMapOntoTheClosedTechniqueVocabulary(t *testing.T) {
	// The vocabulary CycloneDX permits; anything outside it fails the schema.
	permitted := map[string]bool{
		"source-code-analysis": true, "binary-analysis": true, "manifest-analysis": true,
		"ast-fingerprint": true, "hash-comparison": true, "instrumentation": true,
		"dynamic-analysis": true, "filename": true, "attestation": true, "other": true,
	}
	for source, want := range map[string]string{
		"curated":      "attestation",
		"conan":        "manifest-analysis",
		"vcpkg":        "manifest-analysis",
		"fetchcontent": "manifest-analysis",
		// The .pc file a distribution installs beside a system library: a
		// declaration read out of a file, like every manifest above it.
		"pkg-config": "manifest-analysis",
		// The .pdsc descriptor an MCU vendor ships inside a CMSIS-Pack: the
		// vendor's own declaration, read out of a file in the same sense.
		"cmsis-pack":    "manifest-analysis",
		"header":        "source-code-analysis",
		"go-build-info": "binary-analysis",
		"git":           "other",
		"git-describe":  "other",
		"git-commit":    "other",
		// A source nobody has mapped yet must not be guessed at.
		"something-new": "other",
	} {
		got := techniqueForVersionSource(source)
		if got != want {
			t.Errorf("techniqueForVersionSource(%q) = %q, want %q", source, got, want)
		}
		if !permitted[got] {
			t.Errorf("technique %q is not in the CycloneDX vocabulary", got)
		}
	}
}

// TestAMethodKeepsAZeroConfidence pins the second of the two bugs the unused
// struct carried: confidence is required on a method, so omitting it at zero
// would produce an invalid document rather than a modest one.
func TestAMethodKeepsAZeroConfidence(t *testing.T) {
	encoded, err := json.Marshal(Method{Technique: "other", Confidence: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"confidence":0`) {
		t.Errorf("a zero confidence was dropped: %s", encoded)
	}
}
