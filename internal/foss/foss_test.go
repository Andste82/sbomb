package foss_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/foss"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/sbomwriter"
)

// The writer registry has to have the CycloneDX writer in it for
// foss-review.json, and importing the package is what registers it. The blank
// use keeps the import honest.
var _ = cyclonedx.Writer{}

func digestOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// artifact is one retained licence file, as section 22.9 retains it.
func artifact(kind, component, name, text string) domain.LicenseArtifact {
	return domain.LicenseArtifact{
		Kind:   kind,
		File:   domain.FileID{Anchor: "project", RelPath: "dep/" + component + "/" + name},
		SHA256: digestOf(text),
		Bytes:  []byte(text),
	}
}

func statement(text string) domain.CopyrightStatement {
	return domain.CopyrightStatement{Text: text, File: domain.FileID{Anchor: "project", RelPath: "dep/x/y.c"}}
}

// component is a distributed library with everything the documents print.
func component(name string, options ...func(*domain.Component)) domain.Component {
	c := domain.Component{
		ID:               "component:" + name,
		Name:             name,
		Type:             "library",
		DistributionRole: domain.RoleDistributed,
		LinkageForms:     []string{domain.LinkageStaticArchiveMember},
		Licenses:         []domain.LicenseFinding{{Expression: "MIT", Evidence: "file-level"}},
		Modification:     domain.ModificationRecord{Status: domain.ModificationUnknown},
	}
	for _, option := range options {
		option(&c)
	}
	return c
}

func documentOf(components ...domain.Component) *sbomwriter.Document {
	relations := make([]sbomwriter.Relation, 0, len(components))
	for _, c := range components {
		relations = append(relations, sbomwriter.Relation{From: c.ID, To: []string{"file:" + c.ID}})
	}
	return &sbomwriter.Document{
		Product:    domain.Component{ID: "product:app", Name: "app", Type: "application"},
		Components: components,
		Relations:  relations,
		Run:        sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", ToolVersion: "0.0.0-test", Reproducible: true},
	}
}

// render writes the four documents into a fresh directory and returns them by
// name, so that a test asserts over the bytes a user receives rather than over
// an internal string.
func render(t *testing.T, in foss.Input) map[string]string {
	t.Helper()
	directory := t.TempDir()
	if err := foss.Write(directory, in); err != nil {
		t.Fatalf("foss.Write: %v", err)
	}
	out := map[string]string{}
	for _, name := range foss.Files() {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		out[name] = string(data)
	}
	return out
}

// TestEveryClassifiedIdentifierIsARealSPDXIdentifier is the free test decision
// Q19 promised: the table is embedded for section 22.3 anyway, so a typo in the
// committed list is a compile-time-cheap failure rather than a silent
// misclassification.
func TestEveryClassifiedIdentifierIsARealSPDXIdentifier(t *testing.T) {
	known := license.KnownIdentifiers()
	identifiers := foss.Identifiers()
	if len(identifiers) == 0 {
		t.Fatal("the classification list is empty")
	}
	for _, identifier := range identifiers {
		if !known[identifier] {
			t.Errorf("%q is not an identifier the embedded SPDX table knows", identifier)
		}
	}
}

// TestAssessClassifiesAnExpressionRatherThanAnIdentifier pins the four cases
// the list has to get right: a disjunction, an exception, the one condition,
// and an identifier nobody classified.
func TestAssessClassifiesAnExpressionRatherThanAnIdentifier(t *testing.T) {
	static := []string{domain.LinkageStaticArchiveMember}
	dynamic := []string{domain.LinkageDynamic}

	if got := foss.Assess("MIT OR Apache-2.0", static); got.Owed() || len(got.Unclassified) > 0 {
		t.Errorf("MIT OR Apache-2.0 = %+v; want no obligation and nothing unclassified", got)
	}
	// The exception after WITH is not a licence. Reporting it as an
	// unclassified licence would be a false alarm on a very common
	// expression.
	withException := foss.Assess("GPL-2.0-only WITH Classpath-exception-2.0", static)
	if len(withException.Unclassified) != 0 {
		t.Errorf("unclassified = %v; want none", withException.Unclassified)
	}
	if !contains(withException.Obligations, foss.ObligationCorrespondingSource) {
		t.Errorf("obligations = %v; want the corresponding source", withException.Obligations)
	}
	// The one condition in the whole list.
	if got := foss.Assess("LGPL-2.1-only", static); !contains(got.Obligations, foss.ObligationRelinking) {
		t.Errorf("static LGPL obligations = %v; want relinking among them", got.Obligations)
	}
	if got := foss.Assess("LGPL-2.1-only", dynamic); contains(got.Obligations, foss.ObligationRelinking) {
		t.Errorf("dynamic LGPL obligations = %v; want no relinking", got.Obligations)
	}
	// Never assumed permissive.
	exotic := foss.Assess("Frobnicate-1.0", static)
	if len(exotic.Unclassified) != 1 || exotic.Owed() {
		t.Errorf("unknown identifier = %+v; want it reported as unclassified and nothing claimed", exotic)
	}
}

// TestNoRetainedTextProducesTheIncompletenessMarker is requirement R10 at the
// point where the entry would have been, and decision Q11 in the same test: a
// waiver annotates the finding and changes nothing in the notices document.
func TestNoRetainedTextProducesTheIncompletenessMarker(t *testing.T) {
	nolicense := component("nolicense", func(c *domain.Component) {
		c.Licenses = nil
		c.LicenseArtifacts = nil
	})
	waived := domain.Finding{
		ID: "FOSS_LICENSE_TEXT_MISSING", Severity: domain.SeverityInfo,
		Subject: domain.Subject{Kind: "component", Ref: nolicense.ID},
		Message: "no licence text was retained", Waived: true,
		WaiverReason: "Vendor confirmed proprietary; ticket SEC-1234.",
	}
	for _, findings := range [][]domain.Finding{nil, {waived}} {
		files := render(t, foss.Input{Document: documentOf(nolicense), Findings: findings})
		notices := files[foss.NoticesFile]
		if !strings.Contains(notices, "nolicense") {
			t.Fatal("the component with no licence text is not in the notices document")
		}
		if !strings.Contains(notices, "[licence text not found in component - attribution incomplete]") {
			t.Fatalf("the incompleteness marker is missing:\n%s", notices)
		}
	}
	// And the waiver is visible where it belongs, with its reason.
	files := render(t, foss.Input{Document: documentOf(nolicense), Findings: []domain.Finding{waived}})
	if !strings.Contains(files[foss.ReviewTextFile], "Vendor confirmed proprietary") {
		t.Error("the waived finding's reason is not in the review record")
	}
}

// TestASharedTextIsPrintedOnceAndTwoHoldersTwice is decision Q13 and the
// reason the component's own file is retained rather than a canonical one.
func TestASharedTextIsPrintedOnceAndTwoHoldersTwice(t *testing.T) {
	const shared = "Apache License 2.0 - the very same bytes\n"
	const mitA = "MIT License\n\nCopyright (c) 2026 Holder A\n"
	const mitB = "MIT License\n\nCopyright (c) 2026 Holder B\n"
	first := component("alpha", func(c *domain.Component) {
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "alpha", "LICENSE", shared),
		}
	})
	second := component("beta", func(c *domain.Component) {
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "beta", "LICENSE", shared),
		}
	})
	holderA := component("gamma", func(c *domain.Component) {
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "gamma", "LICENSE", mitA),
		}
	})
	holderB := component("delta", func(c *domain.Component) {
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "delta", "LICENSE", mitB),
		}
	})
	notices := render(t, foss.Input{Document: documentOf(first, second, holderA, holderB)})[foss.NoticesFile]

	if count := strings.Count(notices, shared); count != 1 {
		t.Errorf("the shared text appears %d time(s); want exactly one", count)
	}
	if !strings.Contains(notices, "[also the licence text of: beta]") {
		t.Errorf("the components sharing the text are not listed under it:\n%s", notices)
	}
	if !strings.Contains(notices, "byte-identical to LICENSE reproduced under \"alpha\"") {
		t.Error("the second carrier does not point at the text printed once")
	}
	// Two MIT texts naming two holders are two texts, and neither holder is
	// lost: that is the whole point of retaining the component's own file.
	if !strings.Contains(notices, "Copyright (c) 2026 Holder A") || !strings.Contains(notices, "Copyright (c) 2026 Holder B") {
		t.Error("one of the two holders was dropped")
	}
}

// TestTheProjectsOwnCodeIsNotAThirdParty is decision Q12: the criterion is the
// component type, not the anchor scope. A library copied into the source tree
// carries scope=project and must still be in the document.
func TestTheProjectsOwnCodeIsNotAThirdParty(t *testing.T) {
	own := component("project", func(c *domain.Component) {
		c.Type = "application"
		c.Scope = "project"
	})
	copied := component("copied-lib", func(c *domain.Component) {
		// Copied into the tree, so the anchor scope says "project" -- which
		// is exactly why the scope is the wrong criterion.
		c.Scope = "project"
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "copied-lib", "LICENSE", "MIT License\n"),
		}
	})
	notices := render(t, foss.Input{Document: documentOf(own, copied)})[foss.NoticesFile]
	if strings.Contains(notices, "project") {
		t.Errorf("the project's own component is in the notices document:\n%s", notices)
	}
	if !strings.Contains(notices, "copied-lib") {
		t.Error("a library copied into the source tree is not in the notices document")
	}
}

// TestAnEmbeddedAssetIsCarriedLikeAnyOtherComponent is decision Q18. A binary
// carries no SPDX identifier, so an asset gets its licence from a file beside
// it -- and one without such a file gets the marker rather than a guess.
func TestAnEmbeddedAssetIsCarriedLikeAnyOtherComponent(t *testing.T) {
	const ofl = "SIL Open Font License 1.1\n\nCopyright (c) 2026 The Font Authors\n"
	withText := component("nice-font", func(c *domain.Component) {
		c.LinkageForms = []string{domain.LinkageEmbeddedAsset}
		c.Licenses = []domain.LicenseFinding{{Expression: "OFL-1.1", Evidence: "component-level"}}
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "nice-font", "LICENSE", ofl),
		}
		c.Copyrights = []domain.CopyrightStatement{statement("Copyright (c) 2026 The Font Authors")}
	})
	withoutText := component("bare-font", func(c *domain.Component) {
		c.LinkageForms = []string{domain.LinkageEmbeddedAsset}
		c.Licenses = nil
		c.LicenseArtifacts = nil
	})
	notices := render(t, foss.Input{Document: documentOf(withText, withoutText)})[foss.NoticesFile]
	if !strings.Contains(notices, ofl) {
		t.Error("the font's own licence text is not reproduced")
	}
	if !strings.Contains(notices, "bare-font") ||
		!strings.Contains(notices, "[licence text not found in component - attribution incomplete]") {
		t.Errorf("the font without a licence file is not reported as incomplete:\n%s", notices)
	}
}

// TestABuildTimeOnlyComponentIsOnlyInTheReviewRecord is requirement R1 in the
// over-inclusion direction: naming a GPL component the product does not
// contain invites an obligation that was never triggered.
func TestABuildTimeOnlyComponentIsOnlyInTheReviewRecord(t *testing.T) {
	generator := component("gpl-gen", func(c *domain.Component) {
		c.DistributionRole = domain.RoleBuildTimeOnly
		c.LinkageForms = []string{domain.LinkageBuildTool}
		c.Licenses = []domain.LicenseFinding{{Expression: "GPL-2.0-only", Evidence: "file-level"}}
	})
	files := render(t, foss.Input{Document: documentOf(generator)})
	if strings.Contains(files[foss.NoticesFile], "gpl-gen") {
		t.Error("a build-time-only component is in the notices document")
	}
	if strings.Contains(files[foss.ObligationsFile], "gpl-gen") {
		t.Error("a build-time-only component is in source-obligations.txt")
	}
	review := files[foss.ReviewTextFile]
	if !strings.Contains(review, "BUILD-TIME-ONLY COMPONENTS") || !strings.Contains(review, "gpl-gen") {
		t.Errorf("the build-time-only section does not name it:\n%s", review)
	}
}

// TestAComponentWithNoDistributionRoleIsInNoShippableDocument is the other
// half of the membership rule, tested positively rather than by exclusion.
//
// Section 32.6 asks for distribution role `distributed`, and the empty string
// is not a third value of section 24.5 but the absence of a derivation:
// section 24.2's synthetic build-environment grouping has no files of its own,
// so no chain from an artifact reaches it and nothing decides its role.
// Treating "not build-time-only" as "distributed" would put sbomb's own
// bookkeeping node into the customer's notices document as a third-party
// component under an unknown licence -- which is what this asserts cannot
// happen. It is named in the review record instead, because a component in
// none of the three lists would otherwise leave the FOSS outputs silently.
func TestAComponentWithNoDistributionRoleIsInNoShippableDocument(t *testing.T) {
	synthetic := domain.Component{
		ID:         "component:build-environment",
		Name:       "build-environment",
		Type:       "framework",
		Scope:      "toolchain",
		Properties: map[string][]string{"sbomb:component:detectedBy": {"synthetic"}},
	}
	if foss.Reported(synthetic) {
		t.Error("a component with no derived distribution role is reported as distributed")
	}
	files := render(t, foss.Input{Document: documentOf(component("mit-lib"), synthetic)})
	for _, name := range []string{foss.NoticesFile, foss.ObligationsFile} {
		if strings.Contains(files[name], "build-environment") {
			t.Errorf("%s presents a component with no distribution role as a component:\n%s",
				name, files[name])
		}
	}
	review := files[foss.ReviewTextFile]
	if !strings.Contains(review, "COMPONENTS WITH NO DISTRIBUTION ROLE") ||
		!strings.Contains(review, "- build-environment") {
		t.Errorf("the review record does not name it:\n%s", review)
	}
	if !strings.Contains(review, "no distribution role           1") {
		t.Errorf("the completeness block does not count it:\n%s", review)
	}
	// And it is counted nowhere else: the distributed count is the one
	// library, not two.
	if !strings.Contains(review, "components                     1") {
		t.Errorf("the component count includes a component with no role:\n%s", review)
	}
}

// TestTheOutputIsNotASourceOffer is the structural guarantee of section 32.6,
// machine-checked. An evidence-derived subset of sources would look like a
// source offer while being materially incomplete, which turns the tool's
// precision into a compliance defect (requirement R6).
func TestTheOutputIsNotASourceOffer(t *testing.T) {
	lib := component("lgpl-lib", func(c *domain.Component) {
		c.Licenses = []domain.LicenseFinding{{Expression: "LGPL-2.1-only", Evidence: "file-level"}}
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "lgpl-lib", "LICENSE", "GNU LGPL 2.1\n"),
		}
	})
	directory := t.TempDir()
	if err := foss.Write(directory, foss.Input{Document: documentOf(lib)}); err != nil {
		t.Fatal(err)
	}
	forbidden := []string{".c", ".h", ".cpp", ".hpp", ".s", ".tar", ".tar.gz", ".tgz", ".zip", ".patch"}
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		lower := strings.ToLower(entry.Name())
		for _, suffix := range forbidden {
			if strings.HasSuffix(lower, suffix) || strings.Contains(lower, ".tar") {
				t.Errorf("%s was written into the output directory", entry.Name())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// And the guarantee is a property of the code, not of its callers: the
	// one function that creates a file here refuses anything else.
	if err := foss.WriteDocumentForTest(directory, "mbedtls.tar.gz", "payload"); err == nil {
		t.Error("the writer accepted a name outside the four documents")
	}
}

// TestOutOverwritesTheFourNamesAndLeavesEverythingElse is decision Q14. The
// draft refused a non-empty directory; that was invented, and it breaks a CI
// job whose output directory already exists.
func TestOutOverwritesTheFourNamesAndLeavesEverythingElse(t *testing.T) {
	directory := t.TempDir()
	stale := filepath.Join(directory, foss.NoticesFile)
	keep := filepath.Join(directory, "notes-from-the-reviewer.md")
	if err := os.WriteFile(stale, []byte("a stale notices file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := foss.Write(directory, foss.Input{Document: documentOf(component("alpha"))}); err != nil {
		t.Fatalf("a directory that already holds the four files must not be refused: %v", err)
	}
	notices, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(notices), "a stale notices file") {
		t.Error("the stale notices file survived the run")
	}
	other, err := os.ReadFile(keep)
	if err != nil || string(other) != "keep me\n" {
		t.Errorf("an unrelated file in the output directory was touched: %q, %v", other, err)
	}
}

// TestTheNoticesDocumentCarriesNoPath is decision Q8: section 30.7 needs no
// redaction rule for this file because it contains no filesystem path at all,
// and that is asserted rather than promised.
func TestTheNoticesDocumentCarriesNoPath(t *testing.T) {
	lib := component("alpha", func(c *domain.Component) {
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "alpha", "LICENSE", "MIT License\n"),
			artifact(domain.LicenseArtifactNotice, "alpha", "NOTICE", "Notices\n"),
		}
		c.VCS = &domain.VCSRecord{URL: "https://example.invalid/alpha", Commit: "a3f19c2"}
	})
	files := render(t, foss.Input{Document: documentOf(lib)})
	notices := files[foss.NoticesFile]
	for _, path := range []string{"project:dep/alpha/LICENSE", "project:dep/alpha/NOTICE", "dep/alpha"} {
		if strings.Contains(notices, path) {
			t.Errorf("the notices document contains the path %q", path)
		}
	}
	// The origin URL is not a path and is the one locator the document does
	// carry (requirement R9 permits it as a convenience, never as a
	// substitute for the text).
	if !strings.Contains(notices, "https://example.invalid/alpha @ a3f19c2") {
		t.Error("the origin is missing")
	}
	// The review record is internal and does carry the identity, which is
	// what makes an entry traceable (requirement R11).
	if !strings.Contains(files[foss.ReviewTextFile], "project:dep/alpha/LICENSE") {
		t.Error("the review record does not name the file a text was retained from")
	}
}

// TestReviewJSONIsARenderingOfTheDocument pins section 36.1: the machine
// readable record is the writer registry's output with the retained texts in
// it, not a schema of sbomb's own.
func TestReviewJSONIsARenderingOfTheDocument(t *testing.T) {
	lib := component("alpha", func(c *domain.Component) {
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "alpha", "LICENSE", "MIT License\n"),
		}
	})
	files := render(t, foss.Input{Document: documentOf(lib), SpecVersion: "1.6"})
	data := files[foss.ReviewJSONFile]
	if !strings.Contains(data, "\"bomFormat\": \"CycloneDX\"") {
		t.Fatalf("foss-review.json is not a CycloneDX document:\n%s", data)
	}
	if err := cyclonedx.Validate([]byte(data)); err != nil {
		t.Fatalf("foss-review.json is not a valid CycloneDX document: %v", err)
	}
	// The retained text always reaches the FOSS outputs and reaches the SBOM
	// only when the policy says so (decision B1).
	if !strings.Contains(data, "\"contentType\": \"text/plain\"") {
		t.Error("the retained licence text did not reach foss-review.json")
	}
}

// TestMarkdownIsTheSameContentInAnotherRendering keeps --format honest: the
// four names never change, and the licence text is still verbatim.
func TestMarkdownIsTheSameContentInAnotherRendering(t *testing.T) {
	const text = "MIT License\n\nCopyright (c) 2026 Holder\n"
	lib := component("alpha", func(c *domain.Component) {
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "alpha", "LICENSE", text),
		}
	})
	files := render(t, foss.Input{Document: documentOf(lib), Format: foss.FormatMarkdown})
	notices := files[foss.NoticesFile]
	if !strings.HasPrefix(notices, "# THIRD-PARTY NOTICES") {
		t.Errorf("the markdown rendering has no markdown title:\n%s", notices)
	}
	if !strings.Contains(notices, text) {
		t.Error("the retained bytes were rewritten by the markdown rendering")
	}
}

// TestAnInvalidFormatIsRefused: a typo must not silently select the default.
func TestAnInvalidFormatIsRefused(t *testing.T) {
	err := foss.Write(t.TempDir(), foss.Input{Document: documentOf(component("alpha")), Format: "html"})
	if err == nil {
		t.Fatal("an unknown format was accepted")
	}
}

// TestTheCompletenessBlockCountsInAbsoluteNumbers: "86% complete" is a number
// nobody can act on, and "1 / 2" names the one to look at.
func TestTheCompletenessBlockCountsInAbsoluteNumbers(t *testing.T) {
	complete := component("alpha", func(c *domain.Component) {
		c.Version = "1.2.3"
		c.LicenseArtifacts = []domain.LicenseArtifact{
			artifact(domain.LicenseArtifactLicense, "alpha", "LICENSE", "MIT License\n"),
		}
		c.Copyrights = []domain.CopyrightStatement{statement("Copyright (c) 2026 Holder")}
	})
	bare := component("beta", func(c *domain.Component) {
		c.Licenses = nil
	})
	review := render(t, foss.Input{
		Document: documentOf(complete, bare),
		View:     foss.View{NarrowedTotal: 14},
	})[foss.ReviewTextFile]
	for _, want := range []string{
		"components                     2",
		"with resolved licence          1 / 2",
		"with retained licence text     1 / 2",
		"with copyright statements      1 / 2",
		"with resolved version          1 / 2",
		"modification status unknown    2",
		"licence view vs. SBOM view     +14 files",
	} {
		if !strings.Contains(review, want) {
			t.Errorf("the completeness block is missing %q:\n%s", want, review)
		}
	}
	if strings.Contains(review, "%") {
		t.Error("the review record states a percentage")
	}
}

// TestAssemblyModeBreaksTheComponentsDownPerArtifact is decision Q10: one
// notices document per run, and "which artifact pulled in the LGPL component"
// answered in the review record.
func TestAssemblyModeBreaksTheComponentsDownPerArtifact(t *testing.T) {
	shared := component("shared-lib")
	document := documentOf(shared)
	document.Artifacts = []domain.Component{
		{ID: "artifact:bootloader", Name: "bootloader", Type: "application"},
		{ID: "artifact:application", Name: "application", Type: "application"},
	}
	document.Files = []domain.UsedFile{{
		ID: domain.FileID{Anchor: "project", RelPath: "dep/shared-lib/src/a.c"},
		Properties: map[string][]string{
			"sbomb:evidence:artifacts": {"artifact:application", "artifact:bootloader"},
		},
	}}
	document.Relations = []sbomwriter.Relation{{From: shared.ID, To: []string{"project:dep/shared-lib/src/a.c"}}}
	review := render(t, foss.Input{Document: document, Mode: "assembly"})[foss.ReviewTextFile]
	if !strings.Contains(review, "COMPONENTS PER ARTIFACT") {
		t.Fatalf("no per-artifact breakdown in assembly mode:\n%s", review)
	}
	for _, artifact := range []string{"artifact:bootloader", "artifact:application"} {
		if !strings.Contains(review, artifact) {
			t.Errorf("%s is not in the breakdown", artifact)
		}
	}
	// One notices document per run, whatever the mode.
	if strings.Count(review, "shared-lib") < 3 {
		t.Error("the shared component is not listed under both artifacts")
	}
}

// TestSingleArtifactModeHasNoBreakdown: in single-artifact mode the section
// would restate the component list under one heading.
func TestSingleArtifactModeHasNoBreakdown(t *testing.T) {
	review := render(t, foss.Input{Document: documentOf(component("alpha")), Mode: "single"})[foss.ReviewTextFile]
	if strings.Contains(review, "COMPONENTS PER ARTIFACT") {
		t.Error("single-artifact mode wrote a per-artifact breakdown")
	}
}

// TestTheUnionViewAddsCopyrightStatements is the one place the two views of
// section 32.6 differ: a header DWARF narrowing dropped is not in the
// document, and its copyright notice is still owed.
func TestTheUnionViewAddsCopyrightStatements(t *testing.T) {
	lib := component("alpha", func(c *domain.Component) {
		c.Copyrights = []domain.CopyrightStatement{statement("Copyright (c) 2026 Holder A")}
	})
	files := render(t, foss.Input{
		Document: documentOf(lib),
		View: foss.View{
			NarrowedTotal: 3,
			Deltas: []foss.ViewDelta{{
				Component:  "alpha",
				Narrowed:   3,
				Copyrights: []domain.CopyrightStatement{statement("Copyright (c) 2026 Holder B")},
			}},
		},
	})
	if !strings.Contains(files[foss.NoticesFile], "Copyright (c) 2026 Holder B") {
		t.Error("a statement from a narrowed header did not reach the notices document")
	}
	if !strings.Contains(files[foss.ReviewTextFile], "3 header(s), 1 additional copyright statement(s)") {
		t.Errorf("the narrowing delta is not reported per component:\n%s", files[foss.ReviewTextFile])
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
