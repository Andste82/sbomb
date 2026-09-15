package pkgmanager

import (
	"path/filepath"
	"strings"
	"testing"
)

// sbom.yml is a name other tools use too. A document this reader cannot
// recognise as one of its own is somebody else's file, not broken evidence, so
// it is passed over without a word -- a warning would fail a strict run over a
// file the reader has no business with.
func TestBundledYAMLIsSilentAboutFilesThatAreNotItsOwn(t *testing.T) {
	for _, c := range []struct {
		name    string
		content string
	}{
		{"syft document", "artifacts:\n  - name: openssl\n    version: 3.0.1\nsource:\n  type: directory\n"},
		{"spdx document", "spdxVersion: SPDX-2.3\nSPDXID: SPDXRef-DOCUMENT\nname: myproject-1.0\npackages: []\n"},
		{"past the size bound", strings.Repeat("x", maxIDFManifestBytes+1)},
		{"not yaml this reader parses", "version: [a, b]\n"},
		{"a name and nothing else", "name: mylib\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, bundledYAMLName), c.content)

			contributions, findings := bundledYAML{}.Enrich(ComponentRoot{Path: dir, Name: "mylib"})

			if len(findings) != 0 {
				t.Errorf("reported %d findings about a file that is not its own: %+v", len(findings), findings)
			}
			if len(contributions) != 0 {
				t.Errorf("took %d claims out of a file that is not its own", len(contributions))
			}
		})
	}
}

// A document that is recognisably one of its own, describing a component other
// than the one it lies in, is worth reporting: taking its metadata would
// attribute one component's claims to another.
func TestBundledYAMLReportsAMismatchOnlyForItsOwnDocuments(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, bundledYAMLName), "name: freertos\nversion: 10.5.1\n")

	contributions, findings := bundledYAML{}.Enrich(ComponentRoot{Path: dir, Name: "lwip"})

	if len(contributions) != 0 {
		t.Errorf("took %d claims although the document describes another component", len(contributions))
	}
	if len(findings) != 1 || findings[0].ID != "COMPONENT_METADATA_MISMATCH" {
		t.Fatalf("findings = %+v, want one COMPONENT_METADATA_MISMATCH", findings)
	}
	if strings.Contains(findings[0].Message, "ESP-IDF") {
		t.Errorf("a generic reader names ESP-IDF in its message: %q", findings[0].Message)
	}
}
