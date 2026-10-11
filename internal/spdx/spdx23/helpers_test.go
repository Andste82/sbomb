package spdx23

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/example/sbomb/internal/sbommap"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// options23 are the mapping options the spdx-json writer uses for 2.3.
func options23(options sbomwriter.Options) mapping.Options {
	return mapping.Options{
		SpecVersion:              "2.3",
		LicenseText:              options.LicenseText,
		Reproducible:             options.Reproducible,
		Digests:                  []string{"SHA-1"},
		SelfContainedLicenseRefs: true,
	}
}

// modelOf maps a document as the writer does for 2.3.
func modelOf(t *testing.T, document *sbomwriter.Document, options sbomwriter.Options) *mapping.Model {
	t.Helper()
	model, err := mapping.Build(document, options23(options))
	if err != nil {
		t.Fatal(err)
	}
	return model
}

// renderModel renders a model under its reproducible namespace.
func renderModel(t *testing.T, model *mapping.Model) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Render(&out, model, sbommap.DocumentUUID(mapping.Digest(model))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// renderAllFields is the all-fields document at 2.3.
func renderAllFields(t *testing.T) []byte {
	t.Helper()
	data := renderModel(t, modelOf(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions()))
	if path := os.Getenv("SBOMB_SPDX23_DUMP"); path != "" {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return data
}

// decode reads a rendered document back.
func decode(t *testing.T, data []byte) document {
	t.Helper()
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func packageByLocal(doc document, local string) *pkg {
	for index := range doc.Packages {
		if doc.Packages[index].SPDXID == elementID(local) {
			return &doc.Packages[index]
		}
	}
	return nil
}

func fileByLocal(doc document, local string) *file {
	for index := range doc.Files {
		if doc.Files[index].SPDXID == elementID(local) {
			return &doc.Files[index]
		}
	}
	return nil
}

func hasRelationship(doc document, from, relationshipType, to string) bool {
	for _, rel := range doc.Relationships {
		if rel.SPDXElementID == from && rel.RelationshipType == relationshipType && rel.RelatedSPDXElement == to {
			return true
		}
	}
	return false
}
