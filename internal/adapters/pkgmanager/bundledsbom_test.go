package pkgmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// cycloneDXDocument is a bundled CycloneDX SBOM as an upstream ships it: it
// describes itself in metadata.component and lists what it depends on in
// components[]. The dependency is part of the fixture on purpose -- it is the
// half that must never reach a document of ours.
const cycloneDXDocument = `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "version": 1,
  "metadata": {
    "component": {
      "type": "library",
      "name": "tinycbor",
      "version": "0.7.0",
      "purl": "pkg:generic/tinycbor@0.7.0",
      "supplier": {"name": "Example Ltd"},
      "licenses": [{"expression": "Apache-2.0"}]
    }
  },
  "components": [
    {"type": "library", "name": "zlib", "version": "1.3.1", "purl": "pkg:generic/zlib@1.3.1"}
  ]
}`

// spdxDocumentText is the same package as SPDX 2.3, described through
// documentDescribes and carrying a second package that is only a dependency.
const spdxDocumentText = `{
  "spdxVersion": "SPDX-2.3",
  "SPDXID": "SPDXRef-DOCUMENT",
  "documentDescribes": ["SPDXRef-Package-tinycbor"],
  "packages": [
    {"SPDXID": "SPDXRef-Package-zlib", "name": "zlib", "versionInfo": "1.3.1"},
    {"SPDXID": "SPDXRef-Package-tinycbor", "name": "tinycbor", "versionInfo": "0.7.0",
     "licenseDeclared": "Apache-2.0", "supplier": "Organization: Example Ltd",
     "externalRefs": [{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl",
                       "referenceLocator": "pkg:generic/tinycbor@0.7.0"}]}
  ]
}`

// conanWithDocument lays out a conan package whose root carries a bundled SBOM
// under the given name, and returns the build directory.
func conanWithDocument(t *testing.T, name, document string) string {
	t.Helper()
	build := t.TempDir()
	packageRoot := filepath.Join(t.TempDir(), "p")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if document != "" {
		writeTestFile(t, filepath.Join(packageRoot, name), document)
	}
	writeConan(t, build, "tinycbor", "0.6.1", packageRoot)
	return build
}

// The whole point of the reader: a document the upstream shipped states all
// four CRA fields at once, and it outranks what the manager that fetched the
// package recorded.
func TestABundledDocumentDescribesThePackageItLiesIn(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		document string
	}{
		{name: "CycloneDX", file: "sbom.cdx.json", document: cycloneDXDocument},
		{name: "SPDX", file: "sbom.spdx.json", document: spdxDocumentText},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			build := conanWithDocument(t, testCase.file, testCase.document)

			packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
			if len(packages) != 1 {
				t.Fatalf("packages = %#v (findings %#v)", packages, findings)
			}
			found := packages[0]
			for field, want := range map[Field]string{
				FieldVersion:  "0.7.0",
				FieldLicense:  "Apache-2.0",
				FieldSupplier: "Example Ltd",
				FieldPURL:     "pkg:generic/tinycbor@0.7.0",
			} {
				claim := claimsOf(found)[field]
				if claim.Value != want {
					t.Errorf("%s = %q, want %q from the bundled document", field, claim.Value, want)
				}
				if claim.Source != "bundled-sbom" {
					t.Errorf("%s came from %q, want the reader's own name", field, claim.Source)
				}
				if claim.Rank != RankBundledSBOM {
					t.Errorf("%s ranks %d, want rank 4", field, claim.Rank)
				}
			}
			if found.Version.Confidence != domain.ConfidenceHigh {
				t.Errorf("version confidence = %q, want high", found.Version.Confidence)
			}
			// The manager's version lost, and losing is not the same as being
			// forgotten: a disagreement nobody kept cannot be reported.
			var displaced bool
			for _, contribution := range found.Superseded {
				if contribution.Field == FieldVersion && contribution.Claim.Value == "0.6.1" {
					displaced = true
				}
			}
			if !displaced {
				t.Errorf("superseded = %#v, want conan's 0.6.1 kept", found.Superseded)
			}
			// The document named a dependency of its own. Nothing in this run
			// linked that dependency, so it is not part of the product.
			if len(packages) != 1 || found.Name != "tinycbor" {
				t.Errorf("packages = %#v, want only the package conan installed", packages)
			}
			assertNoOriginWithoutAValue(t, packages)
		})
	}
}

// NOASSERTION is the document saying it does not know, so the next field it
// did fill in answers instead.
func TestASPDXDocumentFallsBackFromNoAssertion(t *testing.T) {
	document := `{"spdxVersion":"SPDX-2.2","documentDescribes":["SPDXRef-P"],
	  "packages":[{"SPDXID":"SPDXRef-P","name":"tinycbor","versionInfo":"0.7.0",
	  "licenseDeclared":"NOASSERTION","licenseConcluded":"MIT","supplier":"NOASSERTION"}]}`
	build := conanWithDocument(t, "sbom.spdx.json", document)

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.License.Value != "MIT" {
		t.Errorf("license = %q, want the concluded MIT", found.License.Value)
	}
	if found.Supplier.Value != "" {
		t.Errorf("supplier = %q, want none: NOASSERTION is not a supplier", found.Supplier.Value)
	}
	assertNoOriginWithoutAValue(t, packages)
}

// A document with one package and nothing said about relationships describes
// that package and no other. It is the only inference this reader makes.
func TestASPDXDocumentWithASinglePackageDescribesIt(t *testing.T) {
	document := `{"spdxVersion":"SPDX-2.3",
	  "packages":[{"SPDXID":"SPDXRef-P","name":"tinycbor","versionInfo":"0.7.0"}]}`
	build := conanWithDocument(t, "sbom.spdx.json", document)

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || packages[0].Version.Value != "0.7.0" {
		t.Fatalf("packages = %#v, want the single package read", packages)
	}
}

// The DESCRIBES relationship is the other way an SPDX document says what it is
// about, and both spellings have to answer the same.
func TestASPDXDescribesRelationshipNamesThePackage(t *testing.T) {
	document := `{"spdxVersion":"SPDX-2.3",
	  "relationships":[{"spdxElementId":"SPDXRef-DOCUMENT","relationshipType":"DESCRIBES",
	                    "relatedSpdxElement":"SPDXRef-P"}],
	  "packages":[{"SPDXID":"SPDXRef-O","name":"other","versionInfo":"9.9.9"},
	              {"SPDXID":"SPDXRef-P","name":"tinycbor","versionInfo":"0.7.0"}]}`
	build := conanWithDocument(t, "sbom.spdx.json", document)

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || packages[0].Version.Value != "0.7.0" {
		t.Fatalf("packages = %#v, want the version of the package DESCRIBES named", packages)
	}
}

// The regression that matters most. vcpkg writes vcpkg.spdx.json itself, so it
// is that manager's install state and not something an upstream shipped;
// reading it again here would let a generic reader outrank, at rank 4, the very
// adapter that installed the package.
func TestVcpkgsOwnDocumentIsNotReadAsABundledSBOM(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v (findings %#v)", packages, findings)
	}
	found := packages[0]
	for field, claim := range claimsOf(found) {
		if claim.Value == "" {
			continue
		}
		if claim.Source != "vcpkg" || claim.Rank != RankInstallState {
			t.Errorf("%s = %q from %q at rank %d, want vcpkg's own claim at rank 3",
				field, claim.Value, claim.Source, claim.Rank)
		}
	}
	if len(found.Superseded) != 0 {
		t.Errorf("superseded = %#v, want nothing: only one origin read this document", found.Superseded)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v, want none", findings)
	}
}

// The rank is 4 and not 1: a document beats every manifest and every install
// state, and it still loses to the checkout itself, because a declaration says
// what was shipped and the checkout says what is there.
func TestTheCheckoutOutranksABundledDocument(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.2.0")
	writeTestFile(t, filepath.Join(build, "_deps", "tinylog-src", "sbom.cdx.json"), cycloneDXDocument)
	runner := standInGit(t, "v1.3.0", "037797e856cb1ec53c1545741d08fa758b6f0edb")
	runner.Anchors = []string{build}

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background(), Runner: runner})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Version.Value != "1.3.0" || found.Version.Source != "git-describe" {
		t.Errorf("version = %q from %q, want the checkout's answer",
			found.Version.Value, found.Version.Source)
	}
	// Only the version has a stronger origin. Everything the checkout cannot
	// answer still comes from the document.
	if found.Supplier.Value != "Example Ltd" || found.Supplier.Source != "bundled-sbom" {
		t.Errorf("supplier = %q from %q, want the document's", found.Supplier.Value, found.Supplier.Source)
	}
	if found.License.Value != "Apache-2.0" {
		t.Errorf("license = %q, want the document's", found.License.Value)
	}
}

// A component root with no document is the ordinary case: nothing is claimed
// and nothing is reported, because a package that shipped no SBOM has nothing
// missing about it.
func TestARootWithoutADocumentIsSilent(t *testing.T) {
	build := conanWithDocument(t, "", "")
	// A file that is neither format and a directory that merely looks like one.
	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	writeTestFile(t, filepath.Join(packages[0].Root(), "README.md"), "nothing here\n")
	if err := os.MkdirAll(filepath.Join(packages[0].Root(), "notes.cdx.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	described, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(described) != 1 {
		t.Fatalf("packages = %#v", described)
	}
	if described[0].Version.Value != "0.6.1" || described[0].Version.Source != "conan" {
		t.Errorf("version = %q from %q, want conan's answer untouched",
			described[0].Version.Value, described[0].Version.Source)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v, want none", findings)
	}
	if len(described[0].Superseded) != 0 {
		t.Errorf("superseded = %#v, want none", described[0].Superseded)
	}
}

// A document that cannot be trusted end to end states nothing at all. Half a
// document is not a weaker answer, it is an invented one -- and every refusal
// says which file it was, or the missing value is untraceable.
func TestAnUnusableDocumentPublishesNothingAndIsReported(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		document string
	}{{
		name: "a truncated document", file: "sbom.cdx.json", document: "{",
	}, {
		name: "a document that is not an object", file: "sbom.cdx.json", document: `["tinycbor"]`,
	}, {
		name: "a document declaring another format", file: "sbom.cdx.json",
		document: `{"bomFormat":"SPDX","specVersion":"1.6","metadata":{"component":{"name":"tinycbor","version":"0.7.0"}}}`,
	}, {
		name: "a CycloneDX document describing nothing", file: "sbom.cdx.json",
		document: `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,
		  "components":[{"name":"zlib","version":"1.3.1"}]}`,
	}, {
		name: "a CycloneDX root component without a name", file: "sbom.cdx.json",
		document: `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,
		  "metadata":{"component":{"version":"0.7.0"}}}`,
	}, {
		name: "an SPDX version this tool does not read", file: "sbom.spdx.json",
		document: `{"spdxVersion":"SPDX-3.0.1","packages":[{"SPDXID":"SPDXRef-P","name":"tinycbor","versionInfo":"0.7.0"}]}`,
	}, {
		name: "an SPDX document describing several packages", file: "sbom.spdx.json",
		document: `{"spdxVersion":"SPDX-2.3","documentDescribes":["SPDXRef-A","SPDXRef-B"],
		  "packages":[{"SPDXID":"SPDXRef-A","name":"a","versionInfo":"1.0"},
		              {"SPDXID":"SPDXRef-B","name":"b","versionInfo":"2.0"}]}`,
	}, {
		name: "an SPDX document that describes none of its packages", file: "sbom.spdx.json",
		document: `{"spdxVersion":"SPDX-2.3",
		  "packages":[{"SPDXID":"SPDXRef-A","name":"a","versionInfo":"1.0"},
		              {"SPDXID":"SPDXRef-B","name":"b","versionInfo":"2.0"}]}`,
	}, {
		name: "an SPDX document naming an element that is not one of its packages", file: "sbom.spdx.json",
		document: `{"spdxVersion":"SPDX-2.3","documentDescribes":["SPDXRef-Missing"],
		  "packages":[{"SPDXID":"SPDXRef-A","name":"a","versionInfo":"1.0"}]}`,
	}}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			build := conanWithDocument(t, testCase.file, testCase.document)

			packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
			if len(packages) != 1 {
				t.Fatalf("packages = %#v", packages)
			}
			found := packages[0]
			if found.Version.Value != "0.6.1" || found.Version.Source != "conan" {
				t.Errorf("version = %q from %q, want conan's answer left standing",
					found.Version.Value, found.Version.Source)
			}
			for _, field := range []Field{FieldLicense, FieldSupplier} {
				if claim := claimsOf(found)[field]; claim.Value != "" {
					t.Errorf("%s = %q, want none: nothing readable stated one", field, claim.Value)
				}
			}
			if len(found.Superseded) != 0 {
				t.Errorf("superseded = %#v, want none: no claim was ever made", found.Superseded)
			}
			reported := findingsWithID(findings, "EVIDENCE_UNREADABLE")
			if len(reported) != 1 {
				t.Fatalf("findings = %#v, want exactly one EVIDENCE_UNREADABLE", findings)
			}
			if reported[0].Subject.Kind != "evidence" ||
				filepath.Base(reported[0].Subject.Ref) != testCase.file {
				t.Errorf("subject = %#v, want the document itself", reported[0].Subject)
			}
			assertNoOriginWithoutAValue(t, packages)
		})
	}
}

// Section 30: a document past the bound is refused whole and reported, never
// read up to the limit and applied in part.
func TestADocumentPastTheBoundsOfSectionThirtyIsRefusedWhole(t *testing.T) {
	padding := strings.Repeat(" ", maxSPDXBytes)
	document := `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,` + padding +
		`"metadata":{"component":{"name":"tinycbor","version":"0.7.0"}}}`
	build := conanWithDocument(t, "sbom.cdx.json", document)

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if packages[0].Version.Value != "0.6.1" {
		t.Errorf("version = %q, want conan's, with nothing read from the oversized document",
			packages[0].Version.Value)
	}
	reported := findingsWithID(findings, "INPUT_LIMIT_EXCEEDED")
	if len(reported) != 1 || filepath.Base(reported[0].Subject.Ref) != "sbom.cdx.json" {
		t.Fatalf("findings = %#v, want one INPUT_LIMIT_EXCEEDED naming the document", findings)
	}
	if reported[0].Subject.Kind != "evidence" {
		t.Errorf("subject kind = %q, want the document to be the subject", reported[0].Subject.Kind)
	}
}

// More documents than the bound of section 30 allows: the first few by name are
// read, the rest is reported rather than parsed, and the report names the
// directory because no single file is at fault.
func TestMoreDocumentsThanTheBoundAreReported(t *testing.T) {
	build := conanWithDocument(t, "a.cdx.json", cycloneDXDocument)
	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	root := packages[0].Root()
	for _, name := range []string{"b.cdx.json", "c.cdx.json", "d.cdx.json", "e.cdx.json"} {
		writeTestFile(t, filepath.Join(root, name), cycloneDXDocument)
	}

	described, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(described) != 1 || described[0].Version.Value != "0.7.0" {
		t.Fatalf("packages = %#v, want the first documents still read", described)
	}
	reported := findingsWithID(findings, "INPUT_LIMIT_EXCEEDED")
	if len(reported) != 1 || reported[0].Subject.Ref != root {
		t.Errorf("findings = %#v, want one INPUT_LIMIT_EXCEEDED naming the root %q", findings, root)
	}
}

// Two documents in one directory are settled by name and by nothing else, so
// that the same tree always produces the same document.
func TestTwoDocumentsInOneRootAreSettledByName(t *testing.T) {
	build := conanWithDocument(t, "a-sbom.cdx.json", cycloneDXDocument)
	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	other := strings.Replace(cycloneDXDocument, "0.7.0", "0.9.9", -1)
	writeTestFile(t, filepath.Join(packages[0].Root(), "z-sbom.cdx.json"), other)

	first, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	second, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("packages = %#v / %#v", first, second)
	}
	if first[0].Version.Value != "0.7.0" {
		t.Errorf("version = %q, want the first document by name", first[0].Version.Value)
	}
	if second[0].Version.Value != first[0].Version.Value {
		t.Errorf("two runs disagreed: %q and %q", first[0].Version.Value, second[0].Version.Value)
	}
	var displaced bool
	for _, contribution := range first[0].Superseded {
		if contribution.Field == FieldVersion && contribution.Claim.Value == "0.9.9" {
			displaced = true
		}
	}
	if !displaced {
		t.Errorf("superseded = %#v, want the losing document kept", first[0].Superseded)
	}
}

// The rule of this package, from the outside: a document listing dependencies
// adds none of them. Nothing was linked because a file said so.
func TestTheDependenciesInADocumentAddNoPackage(t *testing.T) {
	build := t.TempDir()
	packageRoot := filepath.Join(t.TempDir(), "p")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConan(t, build, "tinycbor", "0.6.1", packageRoot)
	bare, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	writeTestFile(t, filepath.Join(packageRoot, "sbom.cdx.json"), cycloneDXDocument)

	described, _ := Discover(Options{BuildDir: build, Context: context.Background()})

	if len(described) != len(bare) {
		t.Fatalf("packages = %d, want the %d the manager recorded", len(described), len(bare))
	}
	for i := range bare {
		if described[i].Name != bare[i].Name {
			t.Errorf("package %d = %q, want %q", i, described[i].Name, bare[i].Name)
		}
		if len(described[i].Files) != len(bare[i].Files) || len(described[i].Roots) != len(bare[i].Roots) {
			t.Errorf("package %d covers %q / %q, want the files and roots of %q / %q",
				i, described[i].Files, described[i].Roots, bare[i].Files, bare[i].Roots)
		}
	}
	if described[0].Version.Value != "0.7.0" {
		t.Fatal("the document was not read at all; there is nothing to check here")
	}
}

// A licence array without an expression can say something this tool will not
// interpret. Several licence objects do not say whether they apply together or
// the recipient chooses, and deciding that by inspection is forbidden; a bare
// `name` is the licence CycloneDX defines as having no SPDX identifier, and
// publishing it would state free text where the document promises SPDX.
func TestALicenceArrayIsReadOnlyWhereItIsUnambiguous(t *testing.T) {
	cases := []struct {
		name     string
		licenses string
		want     string
	}{
		{name: "an expression", licenses: `[{"expression":"MIT OR Apache-2.0"}]`, want: "MIT OR Apache-2.0"},
		{name: "a single identifier", licenses: `[{"license":{"id":"MIT"}}]`, want: "MIT"},
		{name: "a single non-SPDX name", licenses: `[{"license":{"name":"Example Proprietary Licence v3, see LEGAL.txt"}}]`, want: ""},
		{name: "two objects and no expression", licenses: `[{"license":{"id":"MIT"}},{"license":{"id":"BSD-3-Clause"}}]`, want: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			document := `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"metadata":{"component":
			  {"name":"tinycbor","version":"0.7.0","licenses":` + testCase.licenses + `}}}`
			build := conanWithDocument(t, "sbom.cdx.json", document)

			packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
			if len(packages) != 1 {
				t.Fatalf("packages = %#v", packages)
			}
			if packages[0].License.Value != testCase.want {
				t.Errorf("license = %q, want %q", packages[0].License.Value, testCase.want)
			}
			// Refusing to interpret is not the same as failing to read: the
			// version came through and nothing was reported.
			if packages[0].Version.Value != "0.7.0" || len(findings) != 0 {
				t.Errorf("version = %q, findings = %#v, want the rest of the document read",
					packages[0].Version.Value, findings)
			}
		})
	}
}
