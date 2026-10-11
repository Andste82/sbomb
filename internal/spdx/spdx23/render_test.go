package spdx23

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbommap"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// The all-fields document rendered at 2.3 passes both tiers.
func TestTheAllFieldsDocumentPassesBothTiers(t *testing.T) {
	data := renderAllFields(t)
	if err := Validate(data); err != nil {
		t.Fatalf("tier a: %v", err)
	}
	if err := CheckOwnOutput(data); err != nil {
		t.Fatalf("tier b: %v", err)
	}
	if err := ValidateOwnOutput(data); err != nil {
		t.Fatalf("both: %v", err)
	}
}

// Rendering is deterministic: the same model twice is the same bytes.
func TestRenderingTwiceGivesTheSameBytes(t *testing.T) {
	if first, second := renderAllFields(t), renderAllFields(t); !bytes.Equal(first, second) {
		t.Fatal("two renderings of one document differ")
	}
}

// The reproducible identity of a 2.3 document is not the one of the 3.0.1
// document of the same run: the version is part of the model's digest.
func TestTheNamespaceDiffersFromThe301DocumentOfTheSameRun(t *testing.T) {
	model23 := modelOf(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions())
	model301, err := mapping.Build(sbomwritertest.AllFields(), mapping.Options{
		SpecVersion: "3.0.1", LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true, CustomAdditions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ns23, ns301 := sbommap.DocumentUUID(mapping.Digest(model23)), sbommap.DocumentUUID(mapping.Digest(model301))
	if ns23 == ns301 {
		t.Fatal("the 2.3 and the 3.0.1 document share a namespace")
	}
	doc := decode(t, renderAllFields(t))
	if doc.DocumentNamespace != "urn:uuid:"+ns23 {
		t.Errorf("documentNamespace = %q, want urn:uuid:%s", doc.DocumentNamespace, ns23)
	}
}

// minimalModel is a product and one component, for the relationship tests.
func minimalModel() *mapping.Model {
	return &mapping.Model{
		SpecVersion: "2.3",
		Created:     "2023-11-14T22:13:20Z",
		Creator:     "Acme",
		Run:         mapping.Run{ToolName: "sbomb", ToolVersion: "1.0"},
		Packages: []mapping.Package{
			{LocalID: "product:app", Role: mapping.RoleProduct, Name: "app", StatedType: "application"},
			{LocalID: "component:lib", Role: mapping.RoleComponent, Name: "lib", StatedType: "library"},
		},
		Agents: []string{"Acme"},
	}
}

// Every edge kind of the model is a 2.3 relationship type, stated from the
// end 2.3 names it from.
func TestEachEdgeKindHasItsTypeAndDirection(t *testing.T) {
	app, lib := elementID("product:app"), elementID("component:lib")
	for kind, want := range map[mapping.EdgeKind]relationship{
		mapping.EdgeContains:           {SPDXElementID: app, RelationshipType: "CONTAINS", RelatedSPDXElement: lib},
		mapping.EdgeEmbeds:             {SPDXElementID: app, RelationshipType: "CONTAINS", RelatedSPDXElement: lib},
		mapping.EdgeStaticLink:         {SPDXElementID: app, RelationshipType: "STATIC_LINK", RelatedSPDXElement: lib},
		mapping.EdgeDynamicLink:        {SPDXElementID: app, RelationshipType: "DYNAMIC_LINK", RelatedSPDXElement: lib},
		mapping.EdgeProvidedDependency: {SPDXElementID: lib, RelationshipType: "PROVIDED_DEPENDENCY_OF", RelatedSPDXElement: app},
		mapping.EdgeTool:               {SPDXElementID: lib, RelationshipType: "BUILD_TOOL_OF", RelatedSPDXElement: app},
		mapping.EdgeDependsOn:          {SPDXElementID: app, RelationshipType: "DEPENDS_ON", RelatedSPDXElement: lib},
	} {
		model := minimalModel()
		model.Edges = []mapping.Edge{{From: "product:app", Kind: kind, To: "component:lib"}}
		data := renderModel(t, model)
		doc := decode(t, data)
		if !hasRelationship(doc, want.SPDXElementID, want.RelationshipType, want.RelatedSPDXElement) {
			t.Errorf("%s: no %s %s %s in %+v", kind, want.SPDXElementID, want.RelationshipType, want.RelatedSPDXElement, doc.Relationships)
		}
		if err := ValidateOwnOutput(data); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	if len(edgeSpellings) != 7 {
		t.Errorf("the model has 7 edge kinds and the table spells %d", len(edgeSpellings))
	}
}

// The document describes the product, and only by relationship.
func TestTheDocumentDescribesTheProduct(t *testing.T) {
	doc := decode(t, renderAllFields(t))
	product := elementID("product:sentinel-product")
	if !hasRelationship(doc, documentID, "DESCRIBES", product) {
		t.Error("no SPDXRef-DOCUMENT DESCRIBES the product")
	}
	if doc.Packages[0].SPDXID != product {
		t.Errorf("the first package is %s", doc.Packages[0].SPDXID)
	}
	if len(doc.DocumentDescribes) != 0 {
		t.Error("documentDescribes is written")
	}
}

// A package or file the document knows to depend on nothing says so, as
// CycloneDX does with an empty dependsOn and 3.0.1 with NoneElement.
func TestAKnownLeafDependsOnNone(t *testing.T) {
	doc := decode(t, renderAllFields(t))
	for _, local := range []string{"component:type-file", "file:build:_deps/kitchen-src/kitchen.c", "file:project:hdr/hdr.h"} {
		if !hasRelationship(doc, elementID(local), "DEPENDS_ON", none) {
			t.Errorf("%s does not DEPENDS_ON NONE", local)
		}
	}
	if hasRelationship(doc, elementID("component:kitchen"), "DEPENDS_ON", none) {
		t.Error("kitchen, which contains files, is stated to depend on nothing")
	}
}

// A file with a SHA-1 is a file; one without -- missing, a patch, or one the
// producer gave no SHA-1 -- is a package of one file, with no digest invented.
func TestAFileWithoutASHA1IsAPackageOfOneFile(t *testing.T) {
	doc := decode(t, renderAllFields(t))

	source := fileByLocal(doc, "file:build:_deps/kitchen-src/kitchen.c")
	if source == nil {
		t.Fatal("kitchen.c, which has a SHA-1, is not a file")
	}
	want := []checksum{{"SHA1", sbomwritertest.HashKitchenSHA1}, {"SHA256", sbomwritertest.HashKitchenSource}}
	if !reflect.DeepEqual(source.Checksums, want) {
		t.Errorf("kitchen.c checksums = %+v", source.Checksums)
	}
	if source.FileName != "build:_deps/kitchen-src/kitchen.c" {
		t.Errorf("fileName = %q, want the canonical path", source.FileName)
	}

	for local, comment := range map[string]string{
		"file:project:hdr/hdr.h":                 missingFileComment,
		"patch:component:kitchen/1":              patchFileComment,
		"file:build:lib/libkitchen.a":            noSHA1FileComment,
		"file:build:_deps/kitchen-src/kitchen.h": noSHA1FileComment,
	} {
		if fileByLocal(doc, local) != nil {
			t.Errorf("%s has no SHA-1 and is a file", local)
			continue
		}
		p := packageByLocal(doc, local)
		if p == nil {
			t.Errorf("%s is dropped", local)
			continue
		}
		if p.FilesAnalyzed == nil || *p.FilesAnalyzed || p.PrimaryPackagePurpose != "FILE" || p.DownloadLocation != noAssertion {
			t.Errorf("%s: filesAnalyzed %v, purpose %q, downloadLocation %q", local, p.FilesAnalyzed, p.PrimaryPackagePurpose, p.DownloadLocation)
		}
		if !strings.HasPrefix(p.Comment, comment) {
			t.Errorf("%s comment = %q, want it to start %q", local, p.Comment, comment)
		}
		for _, sum := range p.Checksums {
			if sum.Algorithm == "SHA1" {
				t.Errorf("%s states a SHA-1 it does not have", local)
			}
		}
	}
	if p := packageByLocal(doc, "file:project:hdr/hdr.h"); p != nil && p.PackageFileName != "project:hdr/hdr.h" {
		t.Errorf("packageFileName = %q", p.PackageFileName)
	}
	if p := packageByLocal(doc, "patch:component:kitchen/1"); p != nil && (p.PackageFileName != "" || p.Name != "0002-sentinel.patch" || p.Description != "sentinel patch description B") {
		t.Errorf("patch package = %+v", p)
	}
	if !hasRelationship(doc, elementID("patch:component:kitchen/1"), "PATCH_APPLIED", elementID("component:kitchen")) {
		t.Error("the patch is not PATCH_APPLIED to its component")
	}
	// The digest without a 2.3 algorithm is in the comment, not lost.
	if p := packageByLocal(doc, "file:build:lib/libkitchen.a"); p == nil || !strings.Contains(p.Comment, "SENTINEL-UNKNOWN-KEY:abcdef0123") {
		t.Error("libkitchen.a's digest without a 2.3 algorithm is lost")
	}
}

// A retained licence file gets its SHA-1 from the bytes the document already
// holds; a used file keeps the SHA-1 its own read computed.
func TestARetainedLicenceFileGetsItsSHA1FromItsBytes(t *testing.T) {
	doc := decode(t, renderAllFields(t))
	sum := sha1.Sum([]byte("sentinel licence text of MIT\n"))
	license := fileByLocal(doc, "file:build:_deps/kitchen-src/LICENSE")
	if license == nil {
		t.Fatal("LICENSE is not a file")
	}
	if license.Checksums[0] != (checksum{"SHA1", hex.EncodeToString(sum[:])}) {
		t.Errorf("LICENSE checksums = %+v", license.Checksums)
	}
	if license.NoticeText != "sentinel licence text of MIT\n" {
		t.Errorf("noticeText = %q", license.NoticeText)
	}
	if !reflect.DeepEqual(license.FileTypes, []string{"DOCUMENTATION", "OTHER", "TEXT"}) {
		t.Errorf("fileTypes = %v", license.FileTypes)
	}
	if !reflect.DeepEqual(license.LicenseInfoInFiles, []string{"MIT"}) {
		t.Errorf("licenseInfoInFiles = %v", license.LicenseInfoInFiles)
	}
	custom := fileByLocal(doc, "file:build:_deps/kitchen-src/COPYING.custom")
	if custom == nil || custom.NoticeText != "" || !strings.Contains(custom.Comment, nonUTF8TextComment) {
		t.Errorf("COPYING.custom, whose text is not UTF-8 = %+v", custom)
	}
	if !hasRelationship(doc, elementID("file:build:_deps/kitchen-src/LICENSE"), "DOCUMENTATION_OF", elementID("component:kitchen")) {
		t.Error("LICENSE is not DOCUMENTATION_OF kitchen")
	}
	if !hasRelationship(doc, elementID("component:kitchen"), "OTHER", elementID("file:build:_deps/kitchen-src/kitchen.h")) {
		t.Error("kitchen.h, a copyright statement's file, is not OTHER evidence of kitchen")
	}
}

func TestPurposesAndFileTypes(t *testing.T) {
	for stated, want := range map[string]string{
		"application": "APPLICATION", "library": "LIBRARY", "framework": "FRAMEWORK", "firmware": "FIRMWARE",
		"container": "CONTAINER", "device": "DEVICE", "file": "FILE", "operating-system": "OPERATING_SYSTEM",
		"platform": "OTHER", "data": "OTHER", "device-driver": "OTHER", "machine-learning-model": "OTHER",
		"cryptographic-asset": "OTHER", "": "OTHER",
	} {
		if got := packagePurpose(mapping.Package{StatedType: stated}); got != want {
			t.Errorf("purpose of %q = %q, want %q", stated, got, want)
		}
	}
	for _, test := range []struct {
		file mapping.File
		want []string
	}{
		{mapping.File{Class: string(domain.FileClassSource)}, []string{"SOURCE"}},
		{mapping.File{Class: string(domain.FileClassHeader)}, []string{"SOURCE"}},
		{mapping.File{Class: string(domain.FileClassGeneratedSource)}, []string{"SOURCE"}},
		{mapping.File{Class: string(domain.FileClassGeneratedHeader)}, []string{"SOURCE"}},
		{mapping.File{Class: string(domain.FileClassObject)}, []string{"BINARY"}},
		{mapping.File{Class: string(domain.FileClassSharedLibrary)}, []string{"BINARY"}},
		{mapping.File{Class: string(domain.FileClassArchive)}, []string{"ARCHIVE"}},
		{mapping.File{Class: string(domain.FileClassAsset)}, []string{"OTHER"}},
		{mapping.File{Class: string(domain.FileClassUnknown)}, nil},
		{mapping.File{Origin: mapping.OriginEvidence | mapping.OriginLicenseFile}, []string{"DOCUMENTATION", "TEXT"}},
		{mapping.File{Origin: mapping.OriginEvidence}, nil},
	} {
		if got := fileTypes(test.file); !reflect.DeepEqual(got, test.want) {
			t.Errorf("fileTypes(%+v) = %v, want %v", test.file, got, test.want)
		}
	}
	for key, want := range map[string]string{"SHA-1": "SHA1", "sha256": "SHA256", "BLAKE2B-256": "BLAKE2b-256", "Adler32": "ADLER32"} {
		if got, known := checksumAlgorithm(key); !known || got != want {
			t.Errorf("checksumAlgorithm(%q) = %q, %v", key, got, known)
		}
	}
	for _, key := range []string{"SHA3-224", "SENTINEL-UNKNOWN-KEY"} {
		if _, known := checksumAlgorithm(key); known {
			t.Errorf("%s has a 2.3 algorithm", key)
		}
	}
}

func TestLicenceFieldsJoinDistinctExpressions(t *testing.T) {
	use := func(kind mapping.LicenseKind, expression string) mapping.LicenseUse {
		return mapping.LicenseUse{License: mapping.License{Kind: kind, Expression: expression}}
	}
	expr := mapping.LicenseExpression
	for _, test := range []struct {
		uses []mapping.LicenseUse
		want string
	}{
		{nil, ""},
		{[]mapping.LicenseUse{use(mapping.LicenseNone, "")}, none},
		{[]mapping.LicenseUse{use(mapping.LicenseNone, ""), use(mapping.LicenseNoAssertion, "")}, noAssertion},
		{[]mapping.LicenseUse{use(mapping.LicenseNoAssertion, ""), use(expr, "MIT")}, "MIT"},
		{[]mapping.LicenseUse{use(expr, "MIT OR Apache-2.0")}, "MIT OR Apache-2.0"},
		{[]mapping.LicenseUse{use(expr, "Zlib"), use(expr, "MIT OR Apache-2.0"), use(expr, "Zlib")}, "(MIT OR Apache-2.0) AND Zlib"},
	} {
		if got := licenseField(test.uses); got != test.want {
			t.Errorf("licenseField(%+v) = %q, want %q", test.uses, got, test.want)
		}
	}
	doc := decode(t, renderAllFields(t))
	if p := packageByLocal(doc, "component:type-untyped"); p == nil || p.LicenseConcluded != "Zlib" {
		t.Errorf("a concluded licence is not licenseConcluded: %+v", p)
	}
	if p := packageByLocal(doc, "artifact:build:bin/app"); p == nil || p.LicenseConcluded != noAssertion || p.LicenseDeclared != "" {
		t.Errorf("a package with no licence: %+v", p)
	}
}

// The licences sbomb defines carry the retained text, or the observed name
// and a comment that says no text was retained; a text that is not UTF-8 is
// named by the file it is in.
func TestExtractedLicencesNeverInventAText(t *testing.T) {
	byID := func(doc document) map[string]extractedLicense {
		out := map[string]extractedLicense{}
		for _, info := range doc.HasExtractedLicensingInfos {
			out[info.LicenseID] = info
		}
		return out
	}
	withText := byID(decode(t, renderAllFields(t)))
	var retained, nonUTF8 extractedLicense
	for id, info := range withText {
		switch {
		case strings.HasPrefix(id, "LicenseRef-sbomb-Sentinel-Retained-Licence-"):
			retained = info
		case strings.HasPrefix(id, "LicenseRef-sbomb-Sentinel-Custom-Licence-"):
			nonUTF8 = info
		}
	}
	if retained.ExtractedText != "sentinel retained custom licence text\n" || retained.Comment != "" {
		t.Errorf("a retained text: %+v", retained)
	}
	if nonUTF8.ExtractedText != "Sentinel Custom Licence" || !strings.HasSuffix(nonUTF8.Comment, "build:_deps/kitchen-src/COPYING.custom") {
		t.Errorf("a text that is not UTF-8: %+v", nonUTF8)
	}
	if info := withText["LicenseRef-sbomb-Sentinel-Unlisted-1.0"]; info.ExtractedText != "Sentinel-Unlisted-1.0" || info.Comment != noTextComment {
		t.Errorf("a name alone: %+v", info)
	}

	// licenseTextInSBOM off: the retained licence is a name too.
	options := sbomwritertest.AllFieldsOptions()
	options.LicenseText = ""
	data := renderModel(t, modelOf(t, sbomwritertest.AllFields(), options))
	if err := ValidateOwnOutput(data); err != nil {
		t.Fatal(err)
	}
	for id, info := range byID(decode(t, data)) {
		if info.Comment != noTextComment {
			t.Errorf("without licence texts, %s = %+v", id, info)
		}
	}
}

// A LicenseRef the build stated is defined by its identifier, and one in
// another document is replaced by a reference sbomb mints, so that the
// document is self-contained.
func TestForeignLicenceReferencesAreDefinedInTheDocument(t *testing.T) {
	document := sbomwritertest.AllFields()
	document.Components[0].Licenses = []domain.LicenseFinding{{Expression: "LicenseRef-acme OR DocumentRef-other:LicenseRef-beta"}}
	data := renderModel(t, modelOf(t, document, sbomwritertest.AllFieldsOptions()))
	if err := ValidateOwnOutput(data); err != nil {
		t.Fatal(err)
	}
	doc := decode(t, data)
	kitchen := packageByLocal(doc, "component:kitchen")
	if kitchen == nil || !strings.HasPrefix(kitchen.LicenseConcluded, "LicenseRef-acme OR LicenseRef-sbomb-DocumentRef-other-LicenseRef-beta-") {
		t.Fatalf("licenseConcluded = %+v", kitchen)
	}
	found := false
	for _, info := range doc.HasExtractedLicensingInfos {
		if info.LicenseID == "LicenseRef-acme" {
			found = true
			if info.ExtractedText != "LicenseRef-acme" || info.Comment != statedRefComment {
				t.Errorf("LicenseRef-acme = %+v", info)
			}
		}
		if strings.HasPrefix(info.LicenseID, "LicenseRef-sbomb-DocumentRef-") && info.Name != "DocumentRef-other:LicenseRef-beta" {
			t.Errorf("the minted reference = %+v", info)
		}
	}
	if !found {
		t.Error("LicenseRef-acme is not defined")
	}
}

func TestDownloadLocation(t *testing.T) {
	commit := []mapping.Property{{Name: "sbomb:component:vcsCommit", Value: "abc123"}}
	for _, test := range []struct {
		pkg  mapping.Package
		want string
	}{
		{mapping.Package{}, noAssertion},
		{mapping.Package{VCSURL: "https://github.com/madler/zlib.git"}, "git+https://github.com/madler/zlib.git"},
		{mapping.Package{VCSURL: "https://github.com/madler/zlib.git", Properties: commit}, "git+https://github.com/madler/zlib.git@abc123"},
		{mapping.Package{VCSURL: "git+ssh://git@github.com/a/b.git", Properties: commit}, "git+ssh://git@github.com/a/b.git@abc123"},
		{mapping.Package{VCSURL: "https://github.com/a/b.git", Properties: append(commit, mapping.Property{Name: "sbomb:component:vcsCommit", Value: "def"})}, "git+https://github.com/a/b.git"},
		{mapping.Package{VCSURL: "/home/me/src/zlib"}, noAssertion},
		{mapping.Package{VCSURL: "file:///home/me/src/zlib"}, noAssertion},
		{mapping.Package{VCSURL: "https://gitserver/zlib.git"}, noAssertion},
		{mapping.Package{VCSURL: "https://github.com/a b.git"}, noAssertion},
	} {
		if got := downloadLocation(test.pkg); got != test.want {
			t.Errorf("downloadLocation(%q, %v) = %q, want %q", test.pkg.VCSURL, test.pkg.Properties, got, test.want)
		}
	}
}

// Properties travel as annotations, one name=value line each, and read back
// to the same properties; a value's line breaks cannot break a line.
func TestPropertiesTravelAsAnnotationsAndReadBack(t *testing.T) {
	properties := []mapping.Property{
		{Name: "sbomb:a", Value: "plain"},
		{Name: "sbomb:b", Value: "two\nlines\r\nand a \\ backslash"},
		{Name: "sbomb:c", Value: ""},
	}
	lines := propertyLines(properties)
	for _, line := range lines {
		if strings.ContainsAny(line, "\n\r") {
			t.Errorf("line %q breaks", line)
		}
	}
	back, err := ParseProperties(lines)
	if err != nil || !reflect.DeepEqual(back, properties) {
		t.Fatalf("read back %+v, %v", back, err)
	}
	for _, bad := range []string{"no-equals", "other:x=y", `sbomb:x=bad\q`, `sbomb:x=trailing\`} {
		if _, err := ParseProperties([]string{bad}); err == nil {
			t.Errorf("%q was read as a property", bad)
		}
	}
	for comment, want := range map[string]string{
		"sbomb:a=1\nsbomb:b=2":                           "properties",
		"licenseConcluded: MIT\nsbomb:license:x=1":       "statement",
		"licenseInfoInFiles: MIT\nsbomb:license:x=1":     "statement",
		"not affected: CVE-2024-1: the upstream says so": "not-affected",
	} {
		if got, err := annotationKind(comment); err != nil || got != want {
			t.Errorf("annotationKind(%q) = %q, %v", comment, got, err)
		}
	}
	for _, bad := range []string{"licenseConcluded: MIT", "not affected: CVE-1", "free text"} {
		if _, err := annotationKind(bad); err == nil {
			t.Errorf("annotationKind(%q) accepted it", bad)
		}
	}

	doc := decode(t, renderAllFields(t))
	kitchen := packageByLocal(doc, "component:kitchen")
	var notAffected []string
	statementSeen := false
	for _, a := range kitchen.Annotations {
		if a.AnnotationType != "OTHER" || a.Annotator != "Tool: sbomb-0.0.0-sentinel" || a.AnnotationDate != "2023-11-14T22:13:20Z" {
			t.Errorf("annotation %+v", a)
		}
		if strings.HasPrefix(a.Comment, notAffectedPrefix) {
			notAffected = append(notAffected, a.Comment)
		}
		if strings.HasPrefix(a.Comment, "licenseConcluded: MIT OR Apache-2.0\n") && strings.Contains(a.Comment, "sbomb:license:technique=spdx-identifier") {
			statementSeen = true
		}
	}
	wantNotAffected := []string{
		"not affected: CVE-2024-0001: sentinel reason one",
		"not affected: CVE-2024-0001: sentinel reason two",
		"not affected: CVE-2024-0002: the upstream lists this vulnerability as not applying to the version it shipped",
		"not affected: GHSA-sent-inel-0001: sentinel reason of an advisory",
	}
	if !reflect.DeepEqual(notAffected, wantNotAffected) {
		t.Errorf("CVE exclusions = %q", notAffected)
	}
	if !statementSeen {
		t.Error("the qualified concluded licence has no statement annotation")
	}
}

// What a package states natively: the version, supplier, originator, the
// purl, the CPE and the VCS location, the copyright notices and the prose
// that decided the modification status.
func TestAPackageStatesWhat23HasFieldsFor(t *testing.T) {
	doc := decode(t, renderAllFields(t))
	kitchen := packageByLocal(doc, "component:kitchen")
	if kitchen.VersionInfo != "1.2.3-sentinel" || kitchen.Supplier != "Organization: Sentinel Supplier Org" ||
		kitchen.Originator != "Organization: Sentinel Originator Org" || kitchen.SourceInfo != "sentinel signal prose" ||
		kitchen.CopyrightText != "Copyright sentinel curated notice\nCopyright sentinel holder C, of no file" ||
		kitchen.Description != "sentinel description of kitchen" || kitchen.PrimaryPackagePurpose != "LIBRARY" {
		t.Errorf("kitchen = %+v", kitchen)
	}
	want := []externalRef{
		{"PACKAGE-MANAGER", "purl", "pkg:generic/kitchen@1.2.3-sentinel"},
		{"SECURITY", "cpe23Type", "cpe:2.3:a:sentinel:kitchen:1.2.3:*:*:*:*:*:*:*"},
		{"OTHER", "vcs", "https://sentinel.example/kitchen.git"},
	}
	if !reflect.DeepEqual(kitchen.ExternalRefs, want) {
		t.Errorf("externalRefs = %+v", kitchen.ExternalRefs)
	}
	if doc.CreationInfo.LicenseListVersion == "" || strings.Count(doc.CreationInfo.LicenseListVersion, ".") != 1 {
		t.Errorf("licenseListVersion = %q, want M.N", doc.CreationInfo.LicenseListVersion)
	}
	if !reflect.DeepEqual(doc.CreationInfo.Creators, []string{"Organization: Sentinel Vendor Org", "Tool: sbomb-0.0.0-sentinel"}) {
		t.Errorf("creators = %v", doc.CreationInfo.Creators)
	}
}
