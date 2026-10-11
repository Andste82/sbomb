package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
)

// The SPDX 2.3 goldens, and the test that keeps them honest.
//
// Each 2.3 golden is taken from exactly the run its 3.0.1 sibling of the same
// name is taken from (spdx_golden_test.go), and
// TestSpdx23DocumentStatesWhatThe301DocumentStates holds the two renderings
// of that one run against each other. The 3.0.1 document is itself held to
// the CycloneDX one, so the chain CycloneDX -> 3.0.1 -> 2.3 means a 2.3
// golden states what the build stated, except for what deviation D57
// records as beyond 2.3. The 2.3 goldens live in their own directory so that
// the 3.0.1 globs of the conformance job do not read them as 3.0.1.

// spdx23Golden is the golden path of a 2.3 document, relative to
// testdata/golden.
func spdx23Golden(name string) string {
	return path.Join("spdx-2.3", name+".spdx.json")
}

// TestSpdx23GoldenDocuments captures one SPDX 2.3 document per marked
// fixture. Beside the golden: the document passes both tiers of the writer's
// checks, describes exactly one package, the product, and never states a
// file without a SHA-1 as a File (clause 8.4).
func TestSpdx23GoldenDocuments(t *testing.T) {
	marked := 0
	for _, fixture := range spdxFixtures {
		if !fixture.spdx23 {
			continue
		}
		marked++
		t.Run(fixture.name, func(t *testing.T) {
			renders := renderFixture(t, fixture)
			holdToBothTiers(t, fixture.name, "2.3", renders.spdx23)
			doc := parseSpdx23(t, renders.spdx23)
			var described []string
			for _, rel := range doc.relationships {
				if rel.from == "SPDXRef-DOCUMENT" && rel.kind == "DESCRIBES" {
					described = append(described, rel.to)
				}
			}
			if len(described) != 1 || !strings.HasPrefix(doc.local(described[0]), "product:") {
				t.Errorf("the document describes %v, want the product alone", described)
			}
			for _, f := range doc.files {
				if !hasChecksum(f, "SHA1") {
					t.Errorf("%s is a File without a SHA-1", f["SPDXID"])
				}
			}
			if fixture.assembly {
				product := described[0]
				var artifacts int
				for _, rel := range doc.relationships {
					if rel.from == product && rel.kind == "CONTAINS" && strings.HasPrefix(doc.local(rel.to), "artifact:") {
						artifacts++
					}
				}
				if artifacts < 2 {
					t.Errorf("an assembly whose product contains %d deliverables", artifacts)
				}
			}
			assertGolden(t, spdx23Golden(fixture.name), renders.spdx23)
		})
	}
	if marked == 0 {
		t.Fatal("no fixture is marked for an SPDX 2.3 golden")
	}
}

// syntheticRender23 is the synthetic document of TestTheSyntheticDocumentGolden
// written as SPDX 2.3, through the registered writer.
func syntheticRender23(t *testing.T) []byte {
	t.Helper()
	writer, err := sbomwriter.Get("spdx-json", "2.3")
	if err != nil {
		t.Fatal(err)
	}
	options := sbomwritertest.AllFieldsOptions()
	options.SpecVersion = "2.3"
	var buffer bytes.Buffer
	if err := writer.Write(&buffer, sbomwritertest.AllFields(), options); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// TestSpdx23SyntheticDocumentGolden is the 2.3 rendering of the document in
// which every field of the format-neutral document is set: where the shapes
// 2.3 has to work around -- a missing file and patches as one-file packages,
// a digest with no 2.3 algorithm, a licence text that is not UTF-8, VEX
// statements as annotations, foreign and unlisted licence references -- are
// visible in a review.
func TestSpdx23SyntheticDocumentGolden(t *testing.T) {
	data := syntheticRender23(t)
	holdToBothTiers(t, "synthetic-all-fields", "2.3", data)
	assertGolden(t, spdx23Golden("synthetic-all-fields"), data)
}

// choiceInAFile is a build whose one third-party component states a choice of
// licences in a file of its own: a LICENSE that reads "SPDX-License-Identifier:
// MIT OR Apache-2.0".
func choiceInAFile() *sbomwriter.Document {
	header := domain.FileID{Anchor: "build", RelPath: "_deps/dual/dual.h"}
	licence := domain.FileID{Anchor: "build", RelPath: "_deps/dual/LICENSE"}
	const statement = "SPDX-License-Identifier: MIT OR Apache-2.0\n"
	return &sbomwriter.Document{
		Product: domain.Component{
			ID: "product", Name: "choice", Version: "1.0.0", VersionSource: "curated", Type: "application",
			Licenses: []domain.LicenseFinding{{Expression: "MIT", Evidence: "curated"}},
		},
		Artifacts: []domain.Component{{ID: "build:bin/app", Name: "app", Type: "application"}},
		Components: []domain.Component{{
			ID: "component:dual", Name: "dual", Version: "2.0.0", Type: "library", Scope: "third-party",
			LinkageForms: []string{domain.LinkageHeaderOnly}, DistributionRole: domain.RoleDistributed,
			Licenses: []domain.LicenseFinding{{
				Expression: "MIT OR Apache-2.0", Evidence: "file-level", Confidence: domain.ConfidenceHigh,
				Source: licence.Canonical(), Technique: "spdx-identifier",
			}},
			LicenseArtifacts: []domain.LicenseArtifact{{
				Kind: domain.LicenseArtifactLicense, File: licence,
				SHA256:     "e1011a8d249fe5463b86287ce92f141abc29f64b20e5fd9ca16e158385eb669e",
				Bytes:      []byte(statement),
				DetectedID: "MIT OR Apache-2.0", Technique: "spdx-identifier",
			}},
		}},
		Files: []domain.UsedFile{{
			ID: header, Class: domain.FileClassHeader,
			Hashes: map[string]string{
				"SHA-256": "1f2e3d4c5b6a79881f2e3d4c5b6a79881f2e3d4c5b6a79881f2e3d4c5b6a7988",
				"SHA-1":   "0a1b2c3d4e5f60718293a4b5c6d7e8f901234567",
			},
		}},
		Relations: []sbomwriter.Relation{
			{From: "component:dual", To: []string{header.Canonical()}},
			{From: "build:bin/app", To: []string{"component:dual"}},
			{From: "product", To: []string{"build:bin/app"}},
		},
		Run: sbomwriter.RunMetadata{
			ToolName: "sbomb", ToolVendor: "Sentinel Vendor Org", ToolVersion: "0.0.0-sentinel",
			Timestamp: "2023-11-14T22:13:20Z", Reproducible: true,
		},
	}
}

// TestSpdx23ChoiceInAFileGolden: a file that offers a choice of licences
// states the whole expression as one licenseInfoInFiles entry, not one entry
// per option as clause 8.6.1 suggests (deviation D57), so that the choice
// itself is not lost; the 3.0.1 document of the same build states the same
// expression.
func TestSpdx23ChoiceInAFileGolden(t *testing.T) {
	render := func(version string) []byte { return renderSynthetic(t, choiceInAFile(), version) }
	data := render("2.3")
	holdToBothTiers(t, "synthetic-choice-in-file", "2.3", data)
	doc := parseSpdx23(t, data)
	var entries []any
	for _, f := range doc.files {
		if strings.HasSuffix(f["fileName"].(string), "/LICENSE") {
			entries, _ = f["licenseInfoInFiles"].([]any)
		}
	}
	if len(entries) != 1 || entries[0] != "MIT OR Apache-2.0" {
		t.Errorf("the LICENSE states licenseInfoInFiles %v, want the whole choice as one entry", entries)
	}
	assertNoProblems(t, "synthetic-choice-in-file", crossCheck23(t, render("3.0.1"), data))
	assertGolden(t, spdx23Golden("synthetic-choice-in-file"), data)
}

// renderSynthetic writes a synthetic document in the given SPDX version
// through the registered writer, with the all-fields options.
func renderSynthetic(t *testing.T, document *sbomwriter.Document, version string) []byte {
	t.Helper()
	writer, err := sbomwriter.Get("spdx-json", version)
	if err != nil {
		t.Fatal(err)
	}
	options := sbomwritertest.AllFieldsOptions()
	options.SpecVersion = version
	var buffer bytes.Buffer
	if err := writer.Write(&buffer, document, options); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// TestSpdx23LicenceReferencesGolden: licence references the build stated --
// a LicenseRef, a LicenseRef of another document, an exception off the SPDX
// list -- are written so that every reference the 2.3 document uses is
// defined in it and no AdditionRef appears (section 28.11.11, deviation D57).
// The golden is in the directory the conformance job hands to pyspdxtools.
func TestSpdx23LicenceReferencesGolden(t *testing.T) {
	data := renderSynthetic(t, sbomwritertest.LicenceReferences(), "2.3")
	holdToBothTiers(t, "synthetic-licence-refs", "2.3", data)
	if bytes.Contains(data, []byte("AdditionRef")) {
		t.Error("the 2.3 document names an AdditionRef")
	}
	if !bytes.Contains(data, []byte(`"licenseId": "LicenseRef-acme"`)) {
		t.Error("LicenseRef-acme, which the build stated, is not defined in the document")
	}
	assertNoProblems(t, "synthetic-licence-refs", crossCheck23(t, renderSynthetic(t, sbomwritertest.LicenceReferences(), "3.0.1"), data))
	assertGolden(t, spdx23Golden("synthetic-licence-refs"), data)
}

// TestSpdx23DocumentStatesWhatThe301DocumentStates is the completeness
// cross-check of the 2.3 renderer, run over every 2.3 fixture and the
// synthetic document: the same build written as SPDX 3.0.1 and SPDX 2.3, and
// every statement of the 3.0.1 document looked up in the 2.3 one on the
// element of the same local identity.
func TestSpdx23DocumentStatesWhatThe301DocumentStates(t *testing.T) {
	for _, fixture := range spdxFixtures {
		if !fixture.spdx23 {
			continue
		}
		t.Run(fixture.name, func(t *testing.T) {
			renders := renderFixture(t, fixture)
			assertNoProblems(t, fixture.name, crossCheck23(t, renders.spdx, renders.spdx23))
		})
	}
	t.Run("synthetic-all-fields", func(t *testing.T) {
		assertNoProblems(t, "synthetic-all-fields", crossCheck23(t, syntheticRenders(t).spdx, syntheticRender23(t)))
	})
}

// TestSpdx23CrossCheckCanFail: a cross-check that finds everything proves
// nothing. Dropping one relationship, one checksum and one annotation line
// from the 2.3 document must each be reported.
func TestSpdx23CrossCheckCanFail(t *testing.T) {
	spdx3 := syntheticRenders(t).spdx
	untouched := syntheticRender23(t)
	if problems := crossCheck23(t, spdx3, untouched); len(problems) != 0 {
		t.Fatalf("the untouched synthetic document already reports losses: %v", problems)
	}
	edits := map[string]func(map[string]any){
		"a relationship": func(doc map[string]any) {
			rels := doc["relationships"].([]any)
			for i, rel := range rels {
				if rel.(map[string]any)["relationshipType"] == "STATIC_LINK" {
					doc["relationships"] = append(rels[:i:i], rels[i+1:]...)
					return
				}
			}
			t.Fatal("no STATIC_LINK to drop")
		},
		"a SHA-256": func(doc map[string]any) {
			for _, f := range doc["files"].([]any) {
				f := f.(map[string]any)
				sums := f["checksums"].([]any)
				for i, sum := range sums {
					if sum.(map[string]any)["algorithm"] == "SHA256" {
						f["checksums"] = append(sums[:i:i], sums[i+1:]...)
						return
					}
				}
			}
			t.Fatal("no SHA-256 to drop")
		},
		"a concluded licence": func(doc map[string]any) {
			for _, p := range doc["packages"].([]any) {
				p := p.(map[string]any)
				if p["licenseConcluded"] == "Zlib" {
					p["licenseConcluded"] = "MIT"
					return
				}
			}
			t.Fatal("no package concludes Zlib")
		},
		"an annotation line": func(doc map[string]any) {
			for _, p := range doc["packages"].([]any) {
				annotations, _ := p.(map[string]any)["annotations"].([]any)
				for _, a := range annotations {
					a := a.(map[string]any)
					comment := a["comment"].(string)
					if lines := strings.Split(comment, "\n"); len(lines) > 1 && strings.HasPrefix(lines[0], "sbomb:") {
						a["comment"] = strings.Join(lines[1:], "\n")
						return
					}
				}
			}
			t.Fatal("no annotation line to drop")
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(untouched, &doc); err != nil {
				t.Fatal(err)
			}
			edit(doc)
			edited, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if problems := crossCheck23(t, spdx3, edited); len(problems) == 0 {
				t.Errorf("%s was dropped from the 2.3 document, and the cross-check reported nothing", name)
			}
		})
	}
}

func assertNoProblems(t *testing.T, name string, problems []string) {
	t.Helper()
	if len(problems) > 0 {
		t.Errorf("%s: the SPDX 2.3 document lost %d statement(s) of the 3.0.1 document:\n  %s",
			name, len(problems), strings.Join(problems, "\n  "))
	}
}

// --- the SPDX 2.3 side ---

type spdx23Relationship struct {
	from, kind, to, comment string
}

type spdx23Doc struct {
	raw           map[string]any
	elements      map[string]map[string]any // local identity -> package or file
	files         []map[string]any
	relationships []spdx23Relationship
	set           map[spdx23Relationship]bool
}

func parseSpdx23(t *testing.T, data []byte) *spdx23Doc {
	t.Helper()
	doc := &spdx23Doc{elements: map[string]map[string]any{}, set: map[spdx23Relationship]bool{}}
	if err := json.Unmarshal(data, &doc.raw); err != nil {
		t.Fatalf("not an SPDX 2.3 JSON document: %v", err)
	}
	for _, list := range []string{"packages", "files"} {
		entries, _ := doc.raw[list].([]any)
		for _, entry := range entries {
			element := entry.(map[string]any)
			id, _ := element["SPDXID"].(string)
			local := localIdentity(id)
			if local == id {
				t.Fatalf("%s does not decode to a local identity", id)
			}
			if _, duplicate := doc.elements[local]; duplicate {
				t.Fatalf("%s is stated twice", id)
			}
			doc.elements[local] = element
			if list == "files" {
				doc.files = append(doc.files, element)
			}
		}
	}
	entries, _ := doc.raw["relationships"].([]any)
	for _, entry := range entries {
		fields := entry.(map[string]any)
		rel := spdx23Relationship{}
		rel.from, _ = fields["spdxElementId"].(string)
		rel.kind, _ = fields["relationshipType"].(string)
		rel.to, _ = fields["relatedSpdxElement"].(string)
		rel.comment, _ = fields["comment"].(string)
		doc.relationships = append(doc.relationships, rel)
		doc.set[spdx23Relationship{from: localIdentity(rel.from), kind: rel.kind, to: localIdentity(rel.to)}] = true
	}
	return doc
}

func (d *spdx23Doc) local(id string) string { return localIdentity(id) }

// has reports a relationship between two local identities ("NONE" for the
// 2.3 NONE).
func (d *spdx23Doc) has(from, kind, to string) bool {
	return d.set[spdx23Relationship{from: from, kind: kind, to: to}]
}

// annotationLines are the lines of every annotation of an element, decoded
// as sbomb writes them: "\\" for a backslash, "\n" and "\r" for the line
// ends a value held.
func annotationLines(element map[string]any) map[string]bool {
	out := map[string]bool{}
	annotations, _ := element["annotations"].([]any)
	for _, entry := range annotations {
		comment, _ := entry.(map[string]any)["comment"].(string)
		for _, line := range strings.Split(comment, "\n") {
			out[unescapeAnnotation(line)] = true
		}
	}
	return out
}

func unescapeAnnotation(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) {
			switch line[i+1] {
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case 'r':
				b.WriteByte('\r')
				i++
				continue
			}
		}
		b.WriteByte(line[i])
	}
	return b.String()
}

func hasChecksum(element map[string]any, algorithm string) bool {
	return checksumValue(element, algorithm) != ""
}

func checksumValue(element map[string]any, algorithm string) string {
	sums, _ := element["checksums"].([]any)
	for _, entry := range sums {
		fields := entry.(map[string]any)
		if fields["algorithm"] == algorithm {
			value, _ := fields["checksumValue"].(string)
			return value
		}
	}
	return ""
}

func hasExternalRef23(element map[string]any, kind, locator string) bool {
	refs, _ := element["externalRefs"].([]any)
	for _, entry := range refs {
		fields := entry.(map[string]any)
		if fields["referenceType"] == kind && fields["referenceLocator"] == locator {
			return true
		}
	}
	return false
}

// --- the cross-check ---

// crossCheck23 is every statement of the 3.0.1 rendering the 2.3 rendering
// of the same build does not carry, sorted.
//
// Element by element: the element exists (as a File, or as a one-file
// package when it has no SHA-1); its name, version, package URL, CPEs,
// supplier and originator are the same; every SHA-256 is the same; every
// extension property is a line of one of its annotations. Relationship by
// relationship: every structural relationship has its 2.3 counterpart --
// contains and its embeds as CONTAINS, hasStaticLink as STATIC_LINK,
// hasDynamicLink as DYNAMIC_LINK, hasProvidedDependency and usesTool from
// the other end as PROVIDED_DEPENDENCY_OF and BUILD_TOOL_OF, dependsOn as
// DEPENDS_ON (NoneElement as NONE), patchedBy as PATCH_APPLIED, hasEvidence
// as DOCUMENTATION_OF or OTHER. Licences: every expression the 3.0.1 element
// concludes or declares is named in a 2.3 licence field of the element (as
// written, or, for a licence 2.3 cannot reference, as a reference sbomb
// defined). VEX: every not-affected statement is an annotation of the
// component.
func crossCheck23(t *testing.T, spdx3Data, spdx23Data []byte) []string {
	t.Helper()
	graph := parseSpdxGraph(t, spdx3Data)
	doc := parseSpdx23(t, spdx23Data)
	var problems []string
	report := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	nameOf := func(ref string) string {
		name, _ := graph.nodes[localIdentity(ref)]["name"].(string)
		return name
	}
	for local, node := range graph.nodes {
		kind, _ := node["type"].(string)
		if kind != "software_Package" && kind != "software_File" {
			continue
		}
		element, found := doc.elements[local]
		if !found {
			report("%s: no package or file in the 2.3 document", local)
			continue
		}
		_, isFile := element["fileName"]
		name, _ := node["name"].(string)
		switch {
		case isFile && element["fileName"] != name:
			report("%s: fileName %v, want %q", local, element["fileName"], name)
		case !isFile && element["name"] != name:
			report("%s: name %v, want %q", local, element["name"], name)
		}
		if kind == "software_File" && !isFile && element["primaryPackagePurpose"] != "FILE" {
			report("%s: a file stated as a package that is not of purpose FILE", local)
		}
		if version, _ := node["software_packageVersion"].(string); version != "" && element["versionInfo"] != version {
			report("%s: versionInfo %v, want %q", local, element["versionInfo"], version)
		}
		if purl, _ := node["software_packageUrl"].(string); purl != "" && !hasExternalRef23(element, "purl", purl) {
			report("%s: no purl externalRef %q", local, purl)
		}
		identifiers, _ := node["externalIdentifier"].([]any)
		for _, entry := range identifiers {
			fields := entry.(map[string]any)
			identifier, _ := fields["identifier"].(string)
			switch fields["externalIdentifierType"] {
			case "cpe23":
				if !hasExternalRef23(element, "cpe23Type", identifier) {
					report("%s: no cpe23Type externalRef %q", local, identifier)
				}
			case "cpe22":
				if !hasExternalRef23(element, "cpe22Type", identifier) {
					report("%s: no cpe22Type externalRef %q", local, identifier)
				}
			}
		}
		if supplier, _ := node["suppliedBy"].(string); supplier != "" && element["supplier"] != "Organization: "+nameOf(supplier) {
			report("%s: supplier %v, want Organization: %s", local, element["supplier"], nameOf(supplier))
		}
		for _, originator := range anyStrings(node["originatedBy"]) {
			if element["originator"] != "Organization: "+nameOf(originator) {
				report("%s: originator %v, want Organization: %s", local, element["originator"], nameOf(originator))
			}
		}
		hashes, _ := node["verifiedUsing"].([]any)
		for _, entry := range hashes {
			fields := entry.(map[string]any)
			if fields["algorithm"] != "sha256" {
				continue
			}
			if value, _ := fields["hashValue"].(string); checksumValue(element, "SHA256") != value {
				report("%s: SHA256 %q, want %q", local, checksumValue(element, "SHA256"), value)
			}
		}
		lines := annotationLines(element)
		for property := range extensionOf(node) {
			if !lines[property] {
				report("%s: no annotation line %q", local, property)
			}
		}
	}

	// licenceField is the 2.3 field a 3.0.1 licence relationship becomes: a
	// conclusion is licenseConcluded; a declaration is licenseInfoInFiles on
	// a File and licenseDeclared on a package.
	licenceField := func(element map[string]any, relationshipType string) string {
		if relationshipType == "hasConcludedLicense" {
			value, _ := element["licenseConcluded"].(string)
			return value
		}
		if _, isFile := element["fileName"]; isFile {
			return strings.Join(anyStrings(element["licenseInfoInFiles"]), " ")
		}
		value, _ := element["licenseDeclared"].(string)
		return value
	}
	for _, rel := range graph.relationships {
		from := rel.from
		for _, to := range rel.to {
			switch rel.relationshipType {
			case "contains":
				if !doc.has(from, "CONTAINS", to) {
					report("%s contains %s: no CONTAINS", from, to)
				}
			case "hasStaticLink":
				if !doc.has(from, "STATIC_LINK", to) {
					report("%s hasStaticLink %s: no STATIC_LINK", from, to)
				}
			case "hasDynamicLink":
				if !doc.has(from, "DYNAMIC_LINK", to) {
					report("%s hasDynamicLink %s: no DYNAMIC_LINK", from, to)
				}
			case "hasProvidedDependency":
				if !doc.has(to, "PROVIDED_DEPENDENCY_OF", from) {
					report("%s hasProvidedDependency %s: no PROVIDED_DEPENDENCY_OF", from, to)
				}
			case "usesTool":
				if !doc.has(to, "BUILD_TOOL_OF", from) {
					report("%s usesTool %s: no BUILD_TOOL_OF", from, to)
				}
			case "dependsOn":
				target := to
				if to == "NoneElement" {
					target = "NONE"
				}
				if !doc.has(from, "DEPENDS_ON", target) {
					report("%s dependsOn %s: no DEPENDS_ON", from, to)
				}
			case "patchedBy":
				if !doc.has(to, "PATCH_APPLIED", from) {
					report("%s patchedBy %s: no PATCH_APPLIED", from, to)
				}
			case "hasEvidence":
				if !doc.has(to, "DOCUMENTATION_OF", from) && !doc.has(from, "OTHER", to) {
					report("%s hasEvidence %s: no DOCUMENTATION_OF or OTHER", from, to)
				}
			case "hasConcludedLicense", "hasDeclaredLicense":
				element, found := doc.elements[from]
				if !found {
					continue
				}
				field := licenceField(element, rel.relationshipType)
				expression, _ := graph.nodes[to]["simplelicensing_licenseExpression"].(string)
				// The two individuals of the licensing profile are no node of the
				// document; they are named by their IRI.
				switch {
				case strings.HasSuffix(to, "NoneLicense"):
					expression = "NONE"
				case strings.HasSuffix(to, "NoAssertionLicense"):
					expression = "NOASSERTION"
				}
				switch {
				case expression == "":
					report("%s %s %s: a licence the cross-check cannot read", from, rel.relationshipType, to)
				case expression == "NONE" || expression == "NOASSERTION":
					// Stated only when nothing else is: 2.3 has one field.
					if len(graph.targets(from, rel.relationshipType)) == 1 && field != expression {
						report("%s %s %s: the 2.3 field says %q", from, rel.relationshipType, expression, field)
					}
				case !licenceNamed(field, expression, doc, graph.mintedName):
					report("%s %s %q: not in its 2.3 field %q", from, rel.relationshipType, expression, field)
				}
			case "doesNotAffect":
				// VEX statements: security_VexNotAffectedVulnAssessmentRelationship
				// from the vulnerability to the component.
				id := strings.TrimPrefix(from, "vulnerability:")
				element := doc.elements[to]
				statement, _ := rel.node["security_impactStatement"].(string)
				switch {
				case element == nil || !annotationHasPrefix(element, "not affected: "+id+": "):
					report("%s does not affect %s: no not-affected annotation", id, to)
				case statement != "" && !annotationLines(element)["not affected: "+id+": "+statement]:
					report("%s does not affect %s: the annotation does not say %q", id, to, statement)
				}
			}
		}
	}
	sort.Strings(problems)
	return problems
}

// licenceNamed reports whether a 3.0.1 licence expression is named in the
// joined 2.3 licence fields of an element: every identifier of it, compared
// as a token. An identifier 2.3 cannot reference -- an AdditionRef, a
// reference into another document -- is named through the LicenseRef sbomb
// defined for it, whose name is the original. A reference sbomb minted in
// 3.0.1 (an AdditionRef-sbomb-* for an exception off the list) is looked for by
// the name minted gives it.
func licenceNamed(fields, expression string, doc *spdx23Doc, minted func(string) string) bool {
	tokens := map[string]bool{}
	for _, token := range strings.FieldsFunc(fields, licenceSeparator) {
		tokens[token] = true
	}
	defined := map[string]string{}
	infos, _ := doc.raw["hasExtractedLicensingInfos"].([]any)
	for _, entry := range infos {
		fields := entry.(map[string]any)
		id, _ := fields["licenseId"].(string)
		name, _ := fields["name"].(string)
		defined[id] = name
	}
	for _, token := range strings.FieldsFunc(expression, licenceSeparator) {
		switch strings.ToUpper(token) {
		case "AND", "OR", "WITH":
			continue
		}
		if tokens[token] {
			continue
		}
		needle := token
		if name := minted(token); name != "" {
			needle = name
		}
		found := false
		for id, name := range defined {
			if tokens[id] && (name == needle || strings.Contains(name, needle)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func licenceSeparator(r rune) bool { return r == ' ' || r == '(' || r == ')' }

func annotationHasPrefix(element map[string]any, prefix string) bool {
	annotations, _ := element["annotations"].([]any)
	for _, entry := range annotations {
		comment, _ := entry.(map[string]any)["comment"].(string)
		if strings.HasPrefix(comment, prefix) {
			return true
		}
	}
	return false
}

// mintedName is the name of a licence or exception reference sbomb minted in a
// 3.0.1 document -- what was observed -- or "" for any other identifier.
func (g *spdxGraph) mintedName(ref string) string {
	if !strings.HasPrefix(ref, "LicenseRef-sbomb-") && !strings.HasPrefix(ref, "AdditionRef-sbomb-") {
		return ""
	}
	name, _ := g.nodes["license-text:"+ref]["name"].(string)
	return name
}
