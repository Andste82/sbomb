package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// licenceTextConfiguration is the one setting this run differs by. It is a
// policy setting and not a flag on a side output, because section 28.7 makes
// the document a function of its inputs alone: an SBOM that grew because
// somebody asked for a notices file would not be one.
const licenceTextConfiguration = `{
  "schemaVersion": 1,
  "project": {},
  "policy": { "licenseTextInSBOM": "evidence" }
}
`

// generateFOSS runs the fixture with its source tree relocated (section 7.9),
// which is the only way the committed corpus can be read: the evidence names
// /__fixture_src__ and the harvested sources live in testdata/fixtures.
func generateFOSS(t *testing.T, extra ...string) []byte {
	t.Helper()
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	output := filepath.Join(t.TempDir(), "foss.cdx.json")
	args := append([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--source-dir", testutil.CorpusSourceTree(t), "--output", output, "--reproducible"}, extra...)
	code, _, stderr := execute(args)
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fossDocument is the shape of the document these tests read.
type fossDocument struct {
	Components []struct {
		Name       string `json:"name"`
		BomRef     string `json:"bom-ref"`
		Properties []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"properties"`
		Evidence struct {
			Licenses []struct {
				License *struct {
					ID              string `json:"id"`
					Name            string `json:"name"`
					Acknowledgement string `json:"acknowledgement"`
					Text            *struct {
						ContentType string `json:"contentType"`
						Encoding    string `json:"encoding"`
						Content     string `json:"content"`
					} `json:"text"`
				} `json:"license"`
			} `json:"licenses"`
		} `json:"evidence"`
	} `json:"components"`
}

func parseFOSS(t *testing.T, data []byte) fossDocument {
	t.Helper()
	var document fossDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

// TestFOSSLicenceTextGolden is the second regression document of the fixture:
// the same run with licenseTextInSBOM=evidence. It exists beside the default
// one so that the size of what the setting adds is visible in a diff rather
// than argued about.
func TestFOSSLicenceTextGolden(t *testing.T) {
	configuration := filepath.Join(t.TempDir(), "licencetext.json")
	if err := os.WriteFile(configuration, []byte(licenceTextConfiguration), 0o600); err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "gcc-ninja-p14-foss-licensetext.cdx.json",
		generateFOSS(t, "--config", configuration))
}

// The end-to-end round trip, on real fixture bytes: what the document carries
// decodes to the file the component ships, with the holder line inside it,
// under a digest the test computes from the file itself.
func TestTheDocumentCarriesTheComponentsOwnLicenceBytes(t *testing.T) {
	configuration := filepath.Join(t.TempDir(), "licencetext.json")
	if err := os.WriteFile(configuration, []byte(licenceTextConfiguration), 0o600); err != nil {
		t.Fatal(err)
	}
	document := parseFOSS(t, generateFOSS(t, "--config", configuration))

	type retained struct{ id, acknowledgement, text string }
	byComponent := map[string][]retained{}
	for _, component := range document.Components {
		for _, entry := range component.Evidence.Licenses {
			if entry.License == nil || entry.License.Text == nil {
				continue
			}
			if entry.License.Text.ContentType != "text/plain" || entry.License.Text.Encoding != "base64" {
				t.Errorf("%s: attachment = %#v, want text/plain + base64", component.Name, entry.License.Text)
			}
			decoded, err := base64.StdEncoding.DecodeString(entry.License.Text.Content)
			if err != nil {
				t.Fatalf("%s: content is not base64: %v", component.Name, err)
			}
			identifier := entry.License.ID
			if identifier == "" {
				identifier = entry.License.Name
			}
			byComponent[component.Name] = append(byComponent[component.Name],
				retained{id: identifier, acknowledgement: entry.License.Acknowledgement, text: string(decoded)})
		}
	}

	mit := byComponent["mit-lib"]
	// `concluded`, not `declared`: the fixture's LICENSE is the MIT text with
	// its holder filled in and no SPDX-License-Identifier line, so technique 4
	// recognized it -- sbomb compared the bytes against the SPDX templates and
	// worked the identifier out. CycloneDX reserves `declared` for what the
	// authors of a component state about it.
	if len(mit) != 1 || mit[0].id != "MIT" || mit[0].acknowledgement != "concluded" {
		t.Fatalf("mit-lib carries %#v, want one concluded MIT text", mit)
	}
	onDisk, err := os.ReadFile(filepath.Join(testutil.CorpusSourceTree(t), "dep", "mit-lib", "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if mit[0].text != string(onDisk) {
		t.Error("the text in the document is not the text of the component's file")
	}
	if !strings.Contains(mit[0].text, "Copyright (c)") {
		t.Error("the retained MIT text carries no copyright line, which is what MIT requires to be reproduced")
	}

	// Both texts of the dual-licensed component, not the first one that
	// resolved -- the defect this milestone closes.
	var dual []string
	for _, entry := range byComponent["multi-license"] {
		dual = append(dual, entry.id)
	}
	sort.Strings(dual)
	if strings.Join(dual, ",") != "Apache-2.0,MIT" {
		t.Errorf("multi-license carries %v, want both texts", dual)
	}

	// A NOTICE is retained for reproduction and never as licence evidence
	// (requirement R4).
	notice, err := os.ReadFile(filepath.Join(testutil.CorpusSourceTree(t), "dep", "apache-lib", "NOTICE"))
	if err != nil {
		t.Fatal(err)
	}
	for name, entries := range byComponent {
		for _, entry := range entries {
			if entry.text == string(notice) {
				t.Errorf("%s: the NOTICE was written as licence evidence", name)
			}
		}
	}
}

// The property beside the field: the bytes are in evidence.licenses, and where
// they came from is in a property, because the field cannot say it. Both are
// written whether or not the text is (section 28.7), so the default document
// still says which file was retained and what its digest is.
func TestRetainedArtifactsAreNamedWithTheirPathAndDigest(t *testing.T) {
	document := parseFOSS(t, generateFOSS(t))

	want := map[string]string{
		"apache-lib":    "sbomb:component:licenseFile=project:dep/apache-lib/LICENSE",
		"bsd-hdr":       "sbomb:component:licenseFile=project:dep/bsd-hdr/LICENSE",
		"lgpl-lib":      "sbomb:component:licenseFile=project:dep/lgpl-lib/LICENSE",
		"mit-lib":       "sbomb:component:licenseFile=project:dep/mit-lib/LICENSE",
		"multi-license": "sbomb:component:licenseFile=project:dep/multi-license/LICENSE-APACHE",
		"nocopyright":   "sbomb:component:licenseFile=project:dep/nocopyright/LICENSE",
	}
	sourceTree := testutil.CorpusSourceTree(t)
	seen := map[string]bool{}
	for _, component := range document.Components {
		for _, property := range component.Properties {
			if property.Name != "sbomb:component:licenseFile" && property.Name != "sbomb:component:noticeFile" {
				continue
			}
			canonical, digest, found := strings.Cut(property.Value, "@sha256:")
			if !found {
				t.Fatalf("%s: property value %q is not <canonicalPath>@sha256:<hex>", component.Name, property.Value)
			}
			if strings.Contains(canonical, sourceTree) || filepath.IsAbs(canonical) {
				t.Errorf("%s: %q is not a canonical path", component.Name, canonical)
			}
			// The digest is checked against the file it names, read here.
			relative := strings.TrimPrefix(canonical, "project:")
			bytesOnDisk, err := os.ReadFile(filepath.Join(sourceTree, filepath.FromSlash(relative)))
			if err != nil {
				t.Fatalf("%s: the property names %q, which cannot be read: %v", component.Name, canonical, err)
			}
			sum := sha256.Sum256(bytesOnDisk)
			if digest != hex.EncodeToString(sum[:]) {
				t.Errorf("%s: digest of %s = %s, want %s", component.Name, canonical, digest, hex.EncodeToString(sum[:]))
			}
			seen[component.Name+":"+property.Name+"="+canonical] = true
		}
	}

	for name, property := range want {
		if !seen[name+":"+property] {
			t.Errorf("%s does not name %s", name, property)
		}
	}
	// The Apache dependency ships both kinds, and they are distinguished.
	if !seen["apache-lib:sbomb:component:noticeFile=project:dep/apache-lib/NOTICE"] {
		t.Error("apache-lib does not name its NOTICE")
	}
	// The manufacturer's own application carries no licence file in its root,
	// so nothing is named for it and FOSS_LICENSE_TEXT_MISSING is what says so.
	for key := range seen {
		if strings.HasPrefix(key, "project:") {
			t.Errorf("the project component named a retained artifact: %s", key)
		}
	}
}

// FOSS_LICENSE_TEXT_MISSING on the corpus: the fixture's own application has
// an SPDX identifier in its source and no licence file of its own, which is
// precisely the case where attribution cannot be satisfied from what sbomb
// saw. The finding is informational and gates nothing.
func TestALicenceWithoutATextIsReportedOnTheFixture(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	directory := t.TempDir()
	findingsPath := filepath.Join(directory, "findings.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--source-dir", testutil.CorpusSourceTree(t),
		"--output", filepath.Join(directory, "out.cdx.json"),
		"--findings-json", findingsPath, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(findingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Findings []struct {
			ID       string `json:"id"`
			Severity string `json:"severity"`
			Subject  struct {
				Ref string `json:"ref"`
			} `json:"subject"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, finding := range report.Findings {
		if finding.ID != "FOSS_LICENSE_TEXT_MISSING" {
			continue
		}
		if finding.Severity != "info" {
			t.Errorf("FOSS_LICENSE_TEXT_MISSING severity = %q, want info", finding.Severity)
		}
		subjects = append(subjects, finding.Subject.Ref)
	}
	sort.Strings(subjects)
	if strings.Join(subjects, ",") != "component:project" {
		t.Errorf("FOSS_LICENSE_TEXT_MISSING reported for %v, want the project component alone", subjects)
	}
}
