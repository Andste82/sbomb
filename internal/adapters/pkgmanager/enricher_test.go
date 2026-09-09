package pkgmanager

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// stubEnricher stands in for the readers this interface was built for. None
// exists yet, so what is under test is the wiring and the ranking around it,
// not any file format.
type stubEnricher struct {
	name          string
	contributions []Contribution
	findings      []domain.Finding
	// seen records every root the enricher was offered, so that a test can
	// prove which directories were read and which were left alone.
	seen *[]ComponentRoot
}

func (s stubEnricher) Source() string { return s.name }

func (s stubEnricher) Enrich(root ComponentRoot) ([]Contribution, []domain.Finding) {
	if s.seen != nil {
		*s.seen = append(*s.seen, root)
	}
	return s.contributions, s.findings
}

// withEnrichers puts a registry in place for one test and takes it out again.
// The registry is package state, and a test that left its own reader behind
// would change the result of every test after it.
func withEnrichers(t *testing.T, readers ...Enricher) {
	t.Helper()
	previous := enrichers
	enrichers = readers
	t.Cleanup(func() { enrichers = previous })
}

// The point of the whole interface: a manager that never states a supplier is
// not thereby a package without one, and something lying beside the package
// can say so.
func TestAnEnricherFillsAFieldTheManagerLeftEmpty(t *testing.T) {
	var seen []ComponentRoot
	withEnrichers(t, stubEnricher{
		name: "stub-manifest",
		contributions: []Contribution{
			{Field: FieldSupplier, Claim: Claim{Value: "Example Ltd", Rank: RankDeclaredManifest}},
		},
		seen: &seen,
	})
	build := t.TempDir()
	packageRoot := filepath.Join(t.TempDir(), "p")
	writeConan(t, build, "tinycbor", "0.6.1", packageRoot)

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v (findings %#v)", packages, findings)
	}
	found := packages[0]
	if found.Supplier.Value != "Example Ltd" {
		t.Errorf("supplier = %q, want the one the enricher stated", found.Supplier.Value)
	}
	// A claim that names no origin is published under the enricher's name, so
	// that no value reaches the document with nothing to attribute it to.
	if found.Supplier.Source != "stub-manifest" {
		t.Errorf("supplier source = %q, want the enricher's name", found.Supplier.Source)
	}
	// Everything the manager itself proved has to survive being described.
	if found.Version.Value != "0.6.1" || found.Version.Source != "conan" {
		t.Errorf("version = %q from %q, want conan's own answer",
			found.Version.Value, found.Version.Source)
	}
	if len(seen) != 1 || seen[0].Path != packageRoot || seen[0].Name != "tinycbor" {
		t.Errorf("roots offered = %#v, want the identity root of tinycbor", seen)
	}
}

// A further root is the build tree CMake filled beside a checkout. Reading a
// manifest there would describe generated output, so it is never offered.
func TestOnlyTheIdentityRootIsDescribed(t *testing.T) {
	var seen []ComponentRoot
	withEnrichers(t, stubEnricher{name: "stub-manifest", seen: &seen})
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.com/tinylog.git", "v1.4.0")
	buildTree := filepath.Join(build, "_deps", "tinylog-build")
	if err := os.MkdirAll(buildTree, 0o755); err != nil {
		t.Fatal(err)
	}

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || len(packages[0].Roots) != 2 {
		t.Fatalf("packages = %#v, want one package with two roots", packages)
	}
	if len(seen) != 1 {
		t.Fatalf("roots offered = %#v, want only the identity root", seen)
	}
	if seen[0].Path != packages[0].Root() {
		t.Errorf("root offered = %q, want the checkout %q", seen[0].Path, packages[0].Root())
	}
}

// Both describe the same package, and where neither origin outranks the other
// the manager that installed it is the one to believe.
func TestAnEnricherOfEqualRankLosesToTheOwningManager(t *testing.T) {
	withEnrichers(t, stubEnricher{
		name: "stub-manifest",
		contributions: []Contribution{
			{Field: FieldVersion, Claim: Claim{Value: "9.9.9", Rank: RankInstallState}},
		},
	})
	build := t.TempDir()
	writeConan(t, build, "tinycbor", "0.6.1", filepath.Join(t.TempDir(), "p"))

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Version.Value != "0.6.1" || found.Version.Source != "conan" {
		t.Errorf("version = %q from %q, want conan to keep the field",
			found.Version.Value, found.Version.Source)
	}
	// The loser is kept, because a disagreement that was never recorded cannot
	// be reported later.
	var superseded bool
	for _, contribution := range found.Superseded {
		if contribution.Field == FieldVersion && contribution.Claim.Value == "9.9.9" {
			superseded = true
		}
	}
	if !superseded {
		t.Errorf("superseded = %#v, want the enricher's version kept", found.Superseded)
	}
}

// Rank and not order decides wherever the ranks differ: an SBOM the upstream
// shipped knows the package better than the manager that fetched it.
func TestAStrongerEnricherWinsAndTheDocumentSaysSo(t *testing.T) {
	withEnrichers(t, stubEnricher{
		name: "stub-sbom",
		contributions: []Contribution{
			{Field: FieldVersion, Claim: Claim{Value: "0.7.0", Rank: RankBundledSBOM, Confidence: domain.ConfidenceHigh}},
		},
	})
	build := t.TempDir()
	writeConan(t, build, "tinycbor", "0.6.1", filepath.Join(t.TempDir(), "p"))

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Version.Value != "0.7.0" || found.Version.Source != "stub-sbom" {
		t.Errorf("version = %q from %q, want the bundled SBOM to win and be named",
			found.Version.Value, found.Version.Source)
	}
	if len(found.Superseded) != 1 || found.Superseded[0].Claim.Value != "0.6.1" {
		t.Errorf("superseded = %#v, want conan's version kept", found.Superseded)
	}
}

// Where two readers rank equally the registry decides, so the order in
// enricher.go is behaviour: two runs over one tree publish the same value only
// because that order is fixed.
func TestTheRegistryOrderDecidesBetweenTwoEqualEnrichers(t *testing.T) {
	first := stubEnricher{
		name:          "stub-a",
		contributions: []Contribution{{Field: FieldSupplier, Claim: Claim{Value: "A", Rank: RankDeclaredManifest}}},
	}
	second := stubEnricher{
		name:          "stub-b",
		contributions: []Contribution{{Field: FieldSupplier, Claim: Claim{Value: "B", Rank: RankDeclaredManifest}}},
	}
	for _, order := range [][]Enricher{{first, second}, {second, first}} {
		withEnrichers(t, order...)
		build := t.TempDir()
		writeConan(t, build, "tinycbor", "0.6.1", filepath.Join(t.TempDir(), "p"))

		packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
		if len(packages) != 1 {
			t.Fatalf("packages = %#v", packages)
		}
		want := order[0].(stubEnricher).contributions[0].Claim.Value
		if packages[0].Supplier.Value != want {
			t.Errorf("supplier = %q, want %q from the first reader in the registry",
				packages[0].Supplier.Value, want)
		}
	}
}

// The ordinary case. Most component roots hold nothing an enricher knows, and
// a component without a manifest is not thereby a component with a problem.
func TestARootWithNothingToReadIsSilent(t *testing.T) {
	build := t.TempDir()
	packageRoot := filepath.Join(t.TempDir(), "p")
	writeConan(t, build, "tinycbor", "0.6.1", packageRoot)
	bare, bareFindings := Discover(Options{BuildDir: build, Context: context.Background()})

	withEnrichers(t, stubEnricher{name: "stub-manifest"})
	described, findings := Discover(Options{BuildDir: build, Context: context.Background()})

	if len(findings) != len(bareFindings) {
		t.Errorf("findings = %#v, want the %d a run without readers produces",
			findings, len(bareFindings))
	}
	if len(described) != 1 || len(bare) != 1 {
		t.Fatalf("packages = %#v / %#v", described, bare)
	}
	if described[0].Version != bare[0].Version || described[0].Supplier != bare[0].Supplier {
		t.Errorf("package = %#v, want it unchanged by a reader that said nothing", described[0])
	}
	if len(described[0].Superseded) != 0 {
		t.Errorf("superseded = %#v, want nothing recorded", described[0].Superseded)
	}
}

// Unreadable evidence is reported and nothing is invented from it, exactly as
// an adapter that cannot read a file it expected reports rather than guesses.
func TestAnEnricherFindingReachesTheRun(t *testing.T) {
	withEnrichers(t, stubEnricher{
		name: "stub-manifest",
		findings: []domain.Finding{{
			ID: "INPUT_LIMIT_EXCEEDED", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "evidence", Ref: "stub"},
			Message: "the manifest is larger than the limit of section 30",
		}},
	})
	build := t.TempDir()
	writeConan(t, build, "tinycbor", "0.6.1", filepath.Join(t.TempDir(), "p"))

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "INPUT_LIMIT_EXCEEDED" {
			reported = true
		}
	}
	if !reported {
		t.Error("an enricher's finding did not reach the run")
	}
	if packages[0].Version.Value != "0.6.1" {
		t.Errorf("version = %q, want the manager's answer left standing",
			packages[0].Version.Value)
	}
}

// The rule of this package, pinned: describing a package must not change what
// it covers. A reader has no way to say otherwise, and this is what that is
// worth.
func TestEnrichmentLeavesTheFilesAndRootsOfAPackageAlone(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")
	writeVcpkgFileList(t, build, "x64-linux", "tinyfmt", "2.1.0", []string{
		"x64-linux/include/tinyfmt.h",
		"x64-linux/lib/libtinyfmt.a",
	})
	bare, _ := Discover(Options{BuildDir: build, Context: context.Background()})

	withEnrichers(t, stubEnricher{
		name: "stub-manifest",
		contributions: []Contribution{
			{Field: FieldSupplier, Claim: Claim{Value: "Example Ltd", Rank: RankBundledSBOM}},
		},
	})
	described, _ := Discover(Options{BuildDir: build, Context: context.Background()})

	if len(described) != 1 || len(bare) != 1 {
		t.Fatalf("packages = %#v / %#v", described, bare)
	}
	if described[0].Supplier.Value != "Example Ltd" {
		t.Fatalf("the reader said nothing; there is no boundary to check")
	}
	if len(described[0].Files) != len(bare[0].Files) {
		t.Errorf("files = %q, want the %q the manager listed", described[0].Files, bare[0].Files)
	}
	for i, path := range bare[0].Files {
		if described[0].Files[i] != path {
			t.Errorf("file %d = %q, want %q", i, described[0].Files[i], path)
		}
	}
	if len(described[0].Roots) != len(bare[0].Roots) || described[0].Root() != bare[0].Root() {
		t.Errorf("roots = %q, want %q", described[0].Roots, bare[0].Roots)
	}
	if described[0].Name != bare[0].Name {
		t.Errorf("name = %q, want %q", described[0].Name, bare[0].Name)
	}
}

// Enrich is the entry point for a root no manager owns. What it returns
// carries claims and nothing else: a package list records what was installed,
// and nothing was installed here.
func TestEnrichCarriesClaimsAndNothingElse(t *testing.T) {
	withEnrichers(t, stubEnricher{
		name: "stub-manifest",
		contributions: []Contribution{
			{Field: FieldVersion, Claim: Claim{Value: "3.0.1", Rank: RankDeclaredManifest, Confidence: domain.ConfidenceHigh}},
			{Field: FieldLicense, Claim: Claim{Value: "MIT", Rank: RankDeclaredManifest}},
		},
	})

	described, findings := Enrich(ComponentRoot{Path: t.TempDir(), Name: "tiny"})

	if described.Version.Value != "3.0.1" || described.License.Value != "MIT" {
		t.Errorf("claims = %#v / %#v", described.Version, described.License)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v, want none", findings)
	}
	if described.Name != "" || len(described.Roots) != 0 || len(described.Files) != 0 || described.Manager != "" {
		t.Errorf("package = %#v, want a carrier of claims and nothing more", described)
	}
}

// A root that is not a directory on disk is no root at all, and a reader that
// was called with one could only guess.
func TestEnrichWithoutARootReadsNothing(t *testing.T) {
	var seen []ComponentRoot
	withEnrichers(t, stubEnricher{name: "stub-manifest", seen: &seen})

	described, findings := Enrich(ComponentRoot{Name: "tiny"})

	if len(seen) != 0 {
		t.Errorf("roots offered = %#v, want none", seen)
	}
	if described.Version.Value != "" || len(findings) != 0 {
		t.Errorf("package = %#v, findings = %#v, want nothing at all", described, findings)
	}
}

// The rule of this package at its sharpest. A reader describes packages; it
// does not produce them. A directory can look every bit like a dependency --
// a licence, a manifest, headers -- and until a manager records it, nothing
// installed it and no file of it was linked. Describing such a directory has
// to leave the result empty, and the directory must not even be offered,
// because nothing has settled it as a component root.
func TestNoEnricherCanBringAPackageIntoExistence(t *testing.T) {
	var seen []ComponentRoot
	withEnrichers(t, stubEnricher{
		name: "stub-manifest",
		contributions: []Contribution{
			{Field: FieldVersion, Claim: Claim{Value: "3.0.1", Rank: RankBundledSBOM}},
			{Field: FieldSupplier, Claim: Claim{Value: "Example Ltd", Rank: RankBundledSBOM}},
			{Field: FieldPURL, Claim: Claim{Value: "pkg:generic/tiny@3.0.1", Rank: RankBundledSBOM}},
		},
		seen: &seen,
	})
	build := t.TempDir()
	library := filepath.Join(build, "third_party", "tiny")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"LICENSE":        "SPDX-License-Identifier: MIT\n",
		"vcpkg.json":     "{\"name\":\"tiny\",\"version\":\"3.0.1\"}\n",
		"tiny.h":         "int tiny(void);\n",
		"CMakeLists.txt": "add_library(tiny tiny.c)\n",
	} {
		if err := os.WriteFile(filepath.Join(library, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})

	if len(packages) != 0 {
		t.Errorf("packages = %#v, want none: no manager installed anything here", packages)
	}
	if len(seen) != 0 {
		t.Errorf("roots offered = %#v, want none: nothing had settled a component root", seen)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v, want none: a build tree without dependencies is not a problem", findings)
	}
}

// The count is part of the rule too: describing the packages a manager did
// record must not add a further one, however much the readers state.
func TestDescribingPackagesDoesNotAddAny(t *testing.T) {
	build := t.TempDir()
	writeConan(t, build, "tinycbor", "0.6.1", filepath.Join(t.TempDir(), "p"))
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.2.0")
	bare, _ := Discover(Options{BuildDir: build, Context: context.Background()})

	withEnrichers(t, stubEnricher{
		name: "stub-manifest",
		contributions: []Contribution{
			{Field: FieldSupplier, Claim: Claim{Value: "Example Ltd", Rank: RankBundledSBOM}},
		},
	})
	described, _ := Discover(Options{BuildDir: build, Context: context.Background()})

	if len(described) != len(bare) {
		t.Fatalf("packages = %d, want the %d the managers recorded", len(described), len(bare))
	}
	for i := range bare {
		if described[i].Name != bare[i].Name || described[i].Manager != bare[i].Manager {
			t.Errorf("package %d = %q from %q, want %q from %q", i,
				described[i].Name, described[i].Manager, bare[i].Name, bare[i].Manager)
		}
		if described[i].Supplier.Value != "Example Ltd" {
			t.Errorf("package %d was not described at all; there is nothing to check here", i)
		}
	}
}

// Evidence a reader could not use publishes nothing at all rather than
// something partial: half a manifest is not a weaker answer, it is an invented
// one. The finding travels on unchanged, in the shape section 30 gives it --
// the shape vcpkg reports a file list over the limit with.
func TestUnusableEvidencePublishesNothingHalfway(t *testing.T) {
	cases := []struct {
		name          string
		contributions []Contribution
		findings      []domain.Finding
	}{{
		name: "a source over the limit of section 30 is dropped whole",
		findings: []domain.Finding{{
			ID: "INPUT_LIMIT_EXCEEDED", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "evidence", Ref: "/build/third_party/tiny/sbom.json"},
			Message: "bundled SBOM exceeds the size limit of section 30; not read",
		}},
	}, {
		name: "a file that could not be parsed states nothing",
		contributions: []Contribution{
			{Field: FieldVersion, Claim: Claim{Value: "", Rank: RankBundledSBOM}},
			{Field: FieldSupplier, Claim: Claim{Value: "", Rank: RankBundledSBOM}},
		},
		findings: []domain.Finding{{
			ID: "EVIDENCE_UNREADABLE", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "evidence", Ref: "/build/third_party/tiny/sbom.json"},
			Message: "bundled SBOM is not valid JSON",
		}},
	}, {
		name: "a value with no origin behind it is no claim",
		contributions: []Contribution{
			{Field: FieldVersion, Claim: Claim{Value: "9.9.9"}},
			{Field: FieldSupplier, Claim: Claim{Value: "Example Ltd"}},
		},
	}}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			withEnrichers(t, stubEnricher{
				name:          "stub-sbom",
				contributions: testCase.contributions,
				findings:      testCase.findings,
			})
			build := t.TempDir()
			writeConan(t, build, "tinycbor", "0.6.1", filepath.Join(t.TempDir(), "p"))

			packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
			if len(packages) != 1 {
				t.Fatalf("packages = %#v", packages)
			}
			found := packages[0]
			if found.Version.Value != "0.6.1" || found.Version.Source != "conan" {
				t.Errorf("version = %q from %q, want conan's answer left standing",
					found.Version.Value, found.Version.Source)
			}
			if found.Supplier.Value != "" {
				t.Errorf("supplier = %q, want none: nothing readable stated one", found.Supplier.Value)
			}
			if len(found.Superseded) != 0 {
				t.Errorf("superseded = %#v, want nothing: no claim was ever made", found.Superseded)
			}
			if len(findings) != len(testCase.findings) {
				t.Fatalf("findings = %#v, want the %d the reader reported", findings, len(testCase.findings))
			}
			for i, finding := range findings {
				if !reflect.DeepEqual(finding, testCase.findings[i]) {
					t.Errorf("finding = %#v, want it passed on as %#v", finding, testCase.findings[i])
				}
			}
		})
	}
}

// Byte-identical output for the same evidence is the promise the whole tool
// rests on, and a second kind of reader is a second chance to break it. Two
// runs over one tree have to agree down to the claims that lost.
func TestDescribingATreeTwiceGivesTheSameAnswer(t *testing.T) {
	withEnrichers(t,
		stubEnricher{
			name: "stub-manifest",
			contributions: []Contribution{
				{Field: FieldVersion, Claim: Claim{Value: "0.7.0", Rank: RankBundledSBOM}},
				{Field: FieldSupplier, Claim: Claim{Value: "Example Ltd", Rank: RankDeclaredManifest}},
			},
		},
		stubEnricher{
			name: "stub-sbom",
			contributions: []Contribution{
				{Field: FieldVersion, Claim: Claim{Value: "0.8.0", Rank: RankBundledSBOM}},
				{Field: FieldSupplier, Claim: Claim{Value: "Other Ltd", Rank: RankDeclaredManifest}},
			},
		},
	)
	build := t.TempDir()
	writeConan(t, build, "tinycbor", "0.6.1", filepath.Join(t.TempDir(), "p"))

	first, firstFindings := Discover(Options{BuildDir: build, Context: context.Background()})
	second, secondFindings := Discover(Options{BuildDir: build, Context: context.Background()})

	if !reflect.DeepEqual(first, second) {
		t.Errorf("two runs disagreed:\n%#v\n%#v", first, second)
	}
	if !reflect.DeepEqual(firstFindings, secondFindings) {
		t.Errorf("two runs reported differently:\n%#v\n%#v", firstFindings, secondFindings)
	}
	if len(first) != 1 || len(first[0].Superseded) == 0 {
		t.Fatalf("packages = %#v, want one package whose readers disagreed", first)
	}
	// The disagreement really was decided: by rank where the ranks differ, and
	// by registry order where they do not.
	if first[0].Version.Value != "0.7.0" || first[0].Supplier.Value != "Example Ltd" {
		t.Errorf("version/supplier = %q/%q, want what the first reader in the registry said",
			first[0].Version.Value, first[0].Supplier.Value)
	}
}
