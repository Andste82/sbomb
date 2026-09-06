package selfsbom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/adapters/gobin"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/sbomwriter"
)

// mitText is the SPDX MIT text with its copyright statement filled in, which
// is what a real licence file looks like. Normalization drops the statement,
// so this is recognized by exact text match.
const mitText = `MIT License

Copyright (c) 2026 Somebody

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`

func linked(deps ...gobin.Module) *gobin.Binary {
	return &gobin.Binary{
		GoVersion:   "go1.22.2",
		MainPackage: "example.com/app/cmd/app",
		Main:        gobin.Module{Path: "example.com/app", Version: "(devel)"},
		Deps:        deps,
		Settings: map[string]string{
			"GOOS":         "linux",
			"GOARCH":       "amd64",
			"vcs.revision": "0123456789abcdef0123456789abcdef01234567",
		},
	}
}

// module writes a vendor tree holding one module at one version, so that the
// agreement check between the binary and the sources on disk can be exercised
// both ways.
func vendorTree(t *testing.T, modulePath, version, licenceText string) string {
	t.Helper()
	root := t.TempDir()
	vendor := filepath.Join(root, "vendor")
	if err := os.MkdirAll(filepath.Join(vendor, filepath.FromSlash(modulePath)), 0o755); err != nil {
		t.Fatal(err)
	}
	modulesTxt := "# " + modulePath + " " + version + "\n## explicit\n" + modulePath + "\n"
	if err := os.WriteFile(filepath.Join(vendor, "modules.txt"), []byte(modulesTxt), 0o644); err != nil {
		t.Fatal(err)
	}
	if licenceText != "" {
		path := filepath.Join(vendor, filepath.FromSlash(modulePath), "LICENSE")
		if err := os.WriteFile(path, []byte(licenceText), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func componentByID(t *testing.T, document *sbomwriter.Document, id string) domain.Component {
	t.Helper()
	for _, component := range document.Components {
		if component.ID == id {
			return component
		}
	}
	t.Fatalf("no component %q in %v", id, componentIDs(document))
	return domain.Component{}
}

func componentIDs(document *sbomwriter.Document) []string {
	ids := make([]string, 0, len(document.Components))
	for _, component := range document.Components {
		ids = append(ids, component.ID)
	}
	return ids
}

func findingIDs(findings []domain.Finding, subject string) []string {
	ids := []string{}
	for _, finding := range findings {
		if subject == "" || finding.Subject.Ref == subject {
			ids = append(ids, finding.ID)
		}
	}
	return ids
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestEveryLinkedModuleBecomesAComponentUnderTheProduct(t *testing.T) {
	result, err := Assemble(linked(
		gobin.Module{Path: "example.com/one", Version: "v1.0.0", Sum: "h1:one"},
		gobin.Module{Path: "example.com/two", Version: "v2.0.0", Sum: "h1:two"},
	), "app", Options{Version: "1.2.3", Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}

	// Two dependencies plus the standard library, which is linked into the
	// binary exactly like any other library and so is a component of the
	// product rather than of the build environment.
	want := []string{"component:go/example.com/one", "component:go/example.com/two", stdComponent}
	got := componentIDs(result.Document)
	if len(got) != len(want) {
		t.Fatalf("components = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("component %d = %q, want %q", index, got[index], want[index])
		}
	}

	if len(result.Document.Relations) != 1 || result.Document.Relations[0].From != productID {
		t.Fatalf("relations = %+v", result.Document.Relations)
	}
	if len(result.Document.Relations[0].To) != 3 {
		t.Errorf("the product depends on %v, want all three components", result.Document.Relations[0].To)
	}
	if result.Document.Product.Version != "1.2.3" {
		t.Errorf("product version = %q, want the supplied 1.2.3", result.Document.Product.Version)
	}
	if result.Document.Product.PURL != "pkg:golang/example.com/app@1.2.3" {
		t.Errorf("product purl = %q", result.Document.Product.PURL)
	}
}

// A golang purl carries the module path as a namespace, so its slashes are
// separators and must survive. Percent-encoding them produces a purl no
// consumer resolves.
func TestModulePathSlashesSurviveInThePURL(t *testing.T) {
	result, err := Assemble(linked(gobin.Module{Path: "github.com/google/uuid", Version: "v1.6.0"}), "app", Options{Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	component := componentByID(t, result.Document, "component:go/github.com/google/uuid")
	if component.PURL != "pkg:golang/github.com/google/uuid@v1.6.0" {
		t.Errorf("purl = %q", component.PURL)
	}
}

// The module sum hashes a module's whole file tree. It is not the digest of
// any artifact this document names, so it is recorded as what it is and the
// missing component hash is still reported.
func TestTheModuleSumIsRecordedAsAPropertyAndNotAsAHash(t *testing.T) {
	result, err := Assemble(linked(
		gobin.Module{Path: "example.com/one", Version: "v1.0.0", Sum: "h1:abcdef"},
	), "app", Options{Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	component := componentByID(t, result.Document, "component:go/example.com/one")
	if got := component.Properties["sbomb:go:moduleSum"]; len(got) != 1 || got[0] != "h1:abcdef" {
		t.Errorf("sbomb:go:moduleSum = %v", got)
	}
	if !contains(findingIDs(result.Findings, component.ID), "MISSING_COMPONENT_HASH") {
		t.Error("a component with no hashable file should report MISSING_COMPONENT_HASH")
	}
}

func TestAReplacedModuleNamesWhatWasCompiledAndWhatWasAskedFor(t *testing.T) {
	replacement := gobin.Module{Path: "example.com/fork", Version: "v1.2.3"}
	result, err := Assemble(linked(
		gobin.Module{Path: "example.com/original", Version: "v1.0.0", ReplacedBy: &replacement},
	), "app", Options{Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	component := componentByID(t, result.Document, "component:go/example.com/fork")
	if component.Version != "v1.2.3" {
		t.Errorf("version = %q, want the replacement's", component.Version)
	}
	if got := component.Properties["sbomb:go:replaces"]; len(got) != 1 || got[0] != "example.com/original@v1.0.0" {
		t.Errorf("sbomb:go:replaces = %v", got)
	}
}

func TestAVendoredLicenceIsUsedWhenTheVersionsAgree(t *testing.T) {
	root := vendorTree(t, "example.com/one", "v1.0.0", mitText)
	result, err := Assemble(linked(
		gobin.Module{Path: "example.com/one", Version: "v1.0.0"},
	), "app", Options{ModuleDir: root, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	component := componentByID(t, result.Document, "component:go/example.com/one")
	if len(component.Licenses) != 1 || component.Licenses[0].Expression != "MIT" {
		t.Fatalf("licences = %+v, want MIT", component.Licenses)
	}
	if source := component.Properties["sbomb:license:source"]; len(source) != 1 || strings.HasPrefix(source[0], "/") {
		// Section 7.5: a host path must not reach the document.
		t.Errorf("licence source = %v, want a path relative to the module root", source)
	}
}

// A vendor directory left over from another commit holds the right module at
// the wrong version. Its licence must not be attributed to what was linked,
// because it would look exactly as confident as a correct answer.
func TestAVendoredLicenceIsRefusedWhenTheVersionsDisagree(t *testing.T) {
	root := vendorTree(t, "example.com/one", "v0.9.0", mitText)
	result, err := Assemble(linked(
		gobin.Module{Path: "example.com/one", Version: "v1.0.0"},
	), "app", Options{ModuleDir: root, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	component := componentByID(t, result.Document, "component:go/example.com/one")
	if component.Licenses[0].Expression != "" {
		t.Errorf("licence = %+v, want none", component.Licenses[0])
	}
	ids := findingIDs(result.Findings, component.ID)
	if !contains(ids, "STALE_BUILD_EVIDENCE") {
		t.Errorf("findings = %v, want STALE_BUILD_EVIDENCE", ids)
	}
}

func TestCurationFillsInALicenceAndIsMarkedAsCurated(t *testing.T) {
	result, err := Assemble(linked(
		gobin.Module{Path: "example.com/one", Version: "v1.0.0"},
	), "app", Options{
		Licenses:     map[string]string{"example.com/one": "BSD-3-Clause"},
		Reproducible: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	component := componentByID(t, result.Document, "component:go/example.com/one")
	if component.Licenses[0].Expression != "BSD-3-Clause" {
		t.Fatalf("licence = %+v", component.Licenses[0])
	}
	if got := component.Properties["sbomb:license:evidenceClass"]; len(got) != 1 || got[0] != "curated" {
		t.Errorf("evidence class = %v, want curated: a curated value must not borrow a file's confidence", got)
	}
	if contains(findingIDs(result.Findings, component.ID), "UNKNOWN_LICENSE") {
		t.Error("a curated licence should not also be reported as unknown")
	}
}

func TestCurationThatContradictsTheLicenceTextIsAConflict(t *testing.T) {
	root := vendorTree(t, "example.com/one", "v1.0.0", mitText)
	result, err := Assemble(linked(
		gobin.Module{Path: "example.com/one", Version: "v1.0.0"},
	), "app", Options{
		ModuleDir:    root,
		Licenses:     map[string]string{"example.com/one": "Apache-2.0"},
		Reproducible: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	component := componentByID(t, result.Document, "component:go/example.com/one")
	ids := findingIDs(result.Findings, component.ID)
	if !contains(ids, "LICENSE_CONFLICT") {
		t.Errorf("findings = %v, want LICENSE_CONFLICT", ids)
	}
}

func TestADirtyWorkingTreeAndAMissingVersionAreReported(t *testing.T) {
	binary := linked()
	binary.Settings["vcs.modified"] = "true"
	result, err := Assemble(binary, "app", Options{Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	ids := findingIDs(result.Findings, productID)
	for _, want := range []string{"VCS_DIRTY", "UNKNOWN_VERSION", "MISSING_SUPPLIER"} {
		if !contains(ids, want) {
			t.Errorf("findings = %v, want %s", ids, want)
		}
	}
	if result.Document.Product.Version != "" {
		t.Errorf("product version = %q, want empty: (devel) is not a version", result.Document.Product.Version)
	}
}

// The document has to survive both validation layers, because a release
// attaches it to an artifact and a consumer will run a schema check on it.
func TestTheDocumentPassesBothValidationLayers(t *testing.T) {
	root := vendorTree(t, "example.com/one", "v1.0.0", mitText)
	result, err := Assemble(linked(
		gobin.Module{Path: "example.com/one", Version: "v1.0.0", Sum: "h1:one"},
	), "app", Options{ModuleDir: root, Version: "1.0.0", Supplier: "Example", Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	data, err := cyclonedx.MarshalDocument(result.Document, sbomwriter.Options{SpecVersion: "1.6", Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := cyclonedx.Validate(data); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// The graph is what the document is derived from, and its invariants are the
// tool's own definition of a well-formed answer.
func TestTheEvidenceGraphHoldsItsInvariants(t *testing.T) {
	result, err := Assemble(linked(
		gobin.Module{Path: "example.com/one", Version: "v1.0.0"},
	), "app", Options{Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Graph.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
	if len(result.Graph.Edges()) != 2 {
		t.Errorf("edges = %d, want one per component", len(result.Graph.Edges()))
	}
}

func TestAssembleRefusesEmptyEvidence(t *testing.T) {
	if _, err := Assemble(nil, "app", Options{}); err == nil {
		t.Fatal("assembling nothing should be an error")
	}
}

// iscText is the ISC licence with its copyright filled in, used as the second
// licence of a dual-licensed file.
const iscText = `ISC License

Copyright (c) 2026 Somebody

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.
`

// A dual-licensed module: two complete licence texts in one file. What is in
// the file can be stated; how the two relate cannot, because that is written
// in the sentence between them.
func TestADualLicensedModuleReportsBothAsEvidenceAndConcludesNothing(t *testing.T) {
	dual := "This module is dual licensed. Use it under either licence below.\n\n" +
		mitText + "\n\n=== or ===\n\n" + iscText
	root := vendorTree(t, "example.com/one", "v1.0.0", dual)

	result, err := Assemble(linked(
		gobin.Module{Path: "example.com/one", Version: "v1.0.0"},
	), "app", Options{ModuleDir: root, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	component := componentByID(t, result.Document, "component:go/example.com/one")

	if got := component.Licenses[0].Expression; got != "" {
		t.Errorf("concluded %q from a file holding two licences", got)
	}
	if component.Licenses[0].Reason != license.ReasonLicenseCompositionUnresolved {
		t.Errorf("reason = %q, want %q", component.Licenses[0].Reason, license.ReasonLicenseCompositionUnresolved)
	}
	observed := make([]string, 0, len(component.LicenseEvidence))
	for _, finding := range component.LicenseEvidence {
		observed = append(observed, finding.Expression)
	}
	if len(observed) != 2 || observed[0] != "MIT" || observed[1] != "ISC" {
		t.Fatalf("evidence = %v, want [MIT ISC]", observed)
	}

	// The finding has to name them, or a reviewer has to open the file to
	// learn what the question even is.
	var message string
	for _, finding := range result.Findings {
		if finding.ID == "UNKNOWN_LICENSE" && finding.Subject.Ref == component.ID {
			message = finding.Message
		}
	}
	if !strings.Contains(message, "MIT") || !strings.Contains(message, "ISC") {
		t.Errorf("the finding does not name both licences: %q", message)
	}
}
