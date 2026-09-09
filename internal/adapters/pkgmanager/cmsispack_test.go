package pkgmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// packDescriptor is a CMSIS pack descriptor in the shape a vendor ships it:
// the vendor and the pack name, a licence that is a *path* into the pack, and
// a release history in the descending order the schema requires. The second
// release is part of the fixture on purpose -- it is the half that must never
// be read as the pack's version.
const packDescriptor = `<?xml version="1.0" encoding="UTF-8"?>
<package schemaVersion="1.7.7" xmlns:xs="http://www.w3.org/2001/XMLSchema-instance">
  <vendor>ARM</vendor>
  <name>CMSIS</name>
  <description>CMSIS (Common Microcontroller Software Interface Standard)</description>
  <license>LICENSE.txt</license>
  <releases>
    <release version="5.9.0" date="2022-05-02">Release notes for 5.9.0</release>
    <release version="5.8.0" date="2021-06-24">Release notes for 5.8.0</release>
  </releases>
</package>
`

// writePack lays a descriptor into a fresh directory and returns the root, so
// a case can say what it is testing rather than how a temporary tree is made.
func writePack(t *testing.T, name, content string) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, name), content)
	return root
}

// contributionFor is the claim a reader made about one field, and whether it
// made one at all.
func contributionFor(contributions []Contribution, field Field) (Claim, bool) {
	for _, contribution := range contributions {
		if contribution.Field == field {
			return contribution.Claim, true
		}
	}
	return Claim{}, false
}

// The whole point of the reader: for a vendor pack the descriptor is the only
// place a version and a supplier exist at all.
func TestAPackDescriptorStatesTheVersionAndTheVendor(t *testing.T) {
	root := writePack(t, "ARM.CMSIS.pdsc", packDescriptor)

	contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none: the descriptor was readable", findings)
	}
	if len(contributions) != 2 {
		t.Fatalf("contributions = %#v, want exactly a version and a supplier", contributions)
	}
	version, _ := contributionFor(contributions, FieldVersion)
	if version.Value != "5.9.0" {
		t.Errorf("version = %q, want the newest release the history states first", version.Value)
	}
	if version.Rank != RankDeclaredManifest {
		t.Errorf("version ranks %d, want rank 2: a descriptor is the manifest the vendor declares", version.Rank)
	}
	if version.Confidence != domain.ConfidenceHigh {
		t.Errorf("version confidence = %q, want high (section 20.3)", version.Confidence)
	}
	supplier, _ := contributionFor(contributions, FieldSupplier)
	if supplier.Value != "ARM" || supplier.Rank != RankDeclaredManifest {
		t.Errorf("supplier = %#v, want the vendor the descriptor states, at rank 2", supplier)
	}
	// The licence element names a file inside the pack rather than an SPDX
	// expression, so publishing it would put a filename where a consumer
	// expects SPDX. And no purl type is registered for CMSIS packs, so there
	// is none to build.
	if claim, made := contributionFor(contributions, FieldLicense); made {
		t.Errorf("licence = %#v, want none: <license> names a file, not an expression", claim)
	}
	if claim, made := contributionFor(contributions, FieldPURL); made {
		t.Errorf("purl = %#v, want none: no purl type is registered for CMSIS packs", claim)
	}
}

// A reader that took the element text instead of the attribute would publish
// the release note as the version.
func TestTheVersionIsTheReleaseAttributeAndNotTheReleaseNote(t *testing.T) {
	root := writePack(t, "ARM.CMSIS.pdsc", packDescriptor)

	contributions, _ := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

	version, made := contributionFor(contributions, FieldVersion)
	if !made || strings.Contains(version.Value, "Release notes") {
		t.Errorf("version = %#v, want the version attribute of the first release", version)
	}
}

// A real vendor descriptor is mostly device, condition and component
// definitions this tool has no use for. They must neither be read nor get in
// the way of the four elements that are.
func TestTheSectionsThisToolHasNoUseForContributeNothing(t *testing.T) {
	var builder strings.Builder
	builder.WriteString("<package schemaVersion=\"1.7.7\">\n  <vendor>Keil</vendor>\n  <name>STM32F4xx_DFP</name>\n")
	builder.WriteString("  <devices>\n")
	for i := 0; i < 500; i++ {
		// The nested <vendor> and <name> are the trap: only /package/vendor
		// and /package/name may be read, never a namesake further down.
		builder.WriteString("    <family Dfamily=\"STM32F4\"><vendor>Nobody</vendor><name>Wrong</name>" +
			"<description>a device this build never mentioned</description></family>\n")
	}
	builder.WriteString("  </devices>\n  <conditions><condition id=\"c\"><require Tcompiler=\"GCC\"/></condition></conditions>\n")
	builder.WriteString("  <releases>\n    <release version=\"2.17.1\" date=\"2023-11-08\">notes</release>\n  </releases>\n")
	builder.WriteString("  <components><component Cclass=\"Device\" Cversion=\"1.0.0\"/></components>\n</package>\n")
	root := writePack(t, "Keil.STM32F4xx_DFP.pdsc", builder.String())

	contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "STM32F4xx_DFP"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	version, _ := contributionFor(contributions, FieldVersion)
	supplier, _ := contributionFor(contributions, FieldSupplier)
	if version.Value != "2.17.1" || supplier.Value != "Keil" {
		t.Errorf("version/supplier = %q/%q, want the pack's own answers", version.Value, supplier.Value)
	}
	if len(contributions) != 2 {
		t.Errorf("contributions = %#v, want exactly two: nothing in the ignored sections is a claim", contributions)
	}
}

// Go reports local names, so a descriptor written with a namespace prefix says
// the same thing as one without. A reader that matched qualified names would
// go silent on a document that is no less valid.
func TestAPrefixedDescriptorReadsTheSame(t *testing.T) {
	prefixed := `<?xml version="1.0" encoding="UTF-8"?>
<p:package xmlns:p="http://www.keil.com/pack/1.7.7" schemaVersion="1.7.7">
  <p:vendor>ARM</p:vendor>
  <p:name>CMSIS</p:name>
  <p:releases><p:release version="5.9.0" date="2022-05-02">notes</p:release></p:releases>
</p:package>
`
	root := writePack(t, "ARM.CMSIS.pdsc", prefixed)

	contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	version, _ := contributionFor(contributions, FieldVersion)
	supplier, _ := contributionFor(contributions, FieldSupplier)
	if version.Value != "5.9.0" || supplier.Value != "ARM" {
		t.Errorf("version/supplier = %q/%q, want a prefixed document read like any other",
			version.Value, supplier.Value)
	}
}

// The pack installer writes the version into the layout, and what is on disk
// outranks what the release history declares. The loser is kept, because a
// disagreement nobody recorded cannot be reported.
func TestTheInstalledLayoutOutranksTheReleaseHistory(t *testing.T) {
	// The descriptor's newest release is 5.9.0; the pack lies in a 5.9.1
	// directory, which is what somebody actually installed.
	root := filepath.Join(t.TempDir(), "ARM", "CMSIS", "5.9.1")
	writeTestFile(t, filepath.Join(root, "ARM.CMSIS.pdsc"), packDescriptor)

	contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	var installed, declared bool
	for _, contribution := range contributions {
		if contribution.Field != FieldVersion {
			continue
		}
		switch contribution.Claim.Rank {
		case RankInstallState:
			installed = contribution.Claim.Value == "5.9.1"
		case RankDeclaredManifest:
			declared = contribution.Claim.Value == "5.9.0"
		}
	}
	if !installed || !declared {
		t.Fatalf("contributions = %#v, want 5.9.1 as install state and 5.9.0 kept as the declaration",
			contributions)
	}
	// And the ranking really settles it, rather than the order of the two
	// claims: what a package publishes is the installed version.
	var described Package
	for _, contribution := range contributions {
		described.Take(contribution.Field, contribution.Claim)
	}
	if described.Version.Value != "5.9.1" {
		t.Errorf("version = %q, want the one the layout states", described.Version.Value)
	}
	if len(described.Superseded) != 1 || described.Superseded[0].Claim.Value != "5.9.0" {
		t.Errorf("superseded = %#v, want the declared version kept", described.Superseded)
	}
}

// An origin agreeing with itself is not a disagreement, so nothing is
// superseded and one claim carries the stronger of the two origins.
func TestALayoutThatAgreesWithTheHistoryRecordsNoDisagreement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ARM", "CMSIS", "5.9.0")
	writeTestFile(t, filepath.Join(root, "ARM.CMSIS.pdsc"), packDescriptor)

	contributions, _ := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

	versions := 0
	for _, contribution := range contributions {
		if contribution.Field == FieldVersion {
			versions++
			if contribution.Claim.Value != "5.9.0" || contribution.Claim.Rank != RankInstallState {
				t.Errorf("version = %#v, want 5.9.0 at the stronger of the two origins", contribution.Claim)
			}
		}
	}
	if versions != 1 {
		t.Errorf("contributions = %#v, want one version claim where both origins agree", contributions)
	}
	var described Package
	for _, contribution := range contributions {
		described.Take(contribution.Field, contribution.Claim)
	}
	if len(described.Superseded) != 0 {
		t.Errorf("superseded = %#v, want nothing: the two origins agreed", described.Superseded)
	}
}

// A directory name is only evidence where the layout really is the pack
// installer's. Anywhere else it is a name somebody chose, and reading it would
// be the guess this tool refuses.
func TestTheLayoutIsReadOnlyWhereItIsThePackInstallersOwn(t *testing.T) {
	cases := []struct {
		name string
		dir  []string
		file string
	}{
		{name: "the directories name another pack", dir: []string{"Keil", "STM32F4xx_DFP", "2.17.1"}, file: "ARM.CMSIS.pdsc"},
		{name: "the descriptor is not named after the pack", dir: []string{"ARM", "CMSIS", "5.9.1"}, file: "pack.pdsc"},
		{name: "the pack was unpacked somewhere flat", dir: []string{"third_party", "cmsis"}, file: "ARM.CMSIS.pdsc"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := filepath.Join(append([]string{t.TempDir()}, testCase.dir...)...)
			writeTestFile(t, filepath.Join(root, testCase.file), packDescriptor)

			contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

			if len(findings) != 0 {
				t.Fatalf("findings = %#v, want none", findings)
			}
			for _, contribution := range contributions {
				if contribution.Field == FieldVersion && contribution.Claim.Rank != RankDeclaredManifest {
					t.Errorf("version = %#v, want only what the descriptor declares", contribution.Claim)
				}
			}
		})
	}
}

// The ordinary case, and the contract: most component roots are no CMSIS pack,
// and there is nothing missing about a component that ships no descriptor.
func TestARootWithNoDescriptorIsSilent(t *testing.T) {
	contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: t.TempDir(), Name: "tinylog"})

	if len(contributions) != 0 || len(findings) != 0 {
		t.Errorf("contributions = %#v, findings = %#v, want both empty", contributions, findings)
	}
}

// A document that breaks part of the way through states nothing. The
// zero-contribution assertion is the important half: a vendor read before the
// break would be published with a document nobody could parse behind it.
func TestABrokenDescriptorStatesNothing(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "truncated mid-document", content: strings.Split(packDescriptor, "<releases>")[0] + "  <rele"},
		{name: "an unclosed tag", content: "<package><vendor>ARM</vendor><name>CMSIS</name>"},
		// An element the document never closes, which is the shape
		// Decoder.AutoClose exists to tidy away. Setting that field was tried
		// here and it cannot leak a value either way: an element closed the
		// moment it opens captures no text at all, so the document is still
		// refused. The case is kept for the malformed document itself.
		{name: "an element nothing closes", content: "<package><vendor>ARM<name>CMSIS</name></package>"},
		{name: "a mismatched end tag", content: "<package><vendor>ARM</name></package>"},
		{name: "not XML at all", content: "vendor: ARM\nname: CMSIS\nversion: 5.9.0\n"},
		{name: "an empty file", content: ""},
		{name: "well-formed but no pack", content: "<catalogue><entry name=\"a\"/></catalogue>\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := writePack(t, "ARM.CMSIS.pdsc", testCase.content)

			contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

			if len(contributions) != 0 {
				t.Errorf("contributions = %#v, want none out of a document that was not read whole", contributions)
			}
			if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
				t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
			}
			if findings[0].Subject.Ref != filepath.Join(root, "ARM.CMSIS.pdsc") {
				t.Errorf("subject = %q, want the descriptor itself", findings[0].Subject.Ref)
			}
		})
	}
}

// The safety of this reader is four decoder fields that are never assigned, so
// the tests have to assert the refusal rather than the values: an edit that
// sets Decoder.Entity or Decoder.Strict to "support" such a document must fail
// here rather than in a memory exhaustion somewhere else.
func TestAnEntityBombIsRefusedWithoutExpanding(t *testing.T) {
	bomb := `<?xml version="1.0"?>
<!DOCTYPE package [
  <!ENTITY lol "lol">
  <!ENTITY lol1 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">
  <!ENTITY lol2 "&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;">
  <!ENTITY lol3 "&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;">
  <!ENTITY lol4 "&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;">
  <!ENTITY lol5 "&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;">
  <!ENTITY lol6 "&lol5;&lol5;&lol5;&lol5;&lol5;&lol5;&lol5;&lol5;&lol5;&lol5;">
  <!ENTITY lol7 "&lol6;&lol6;&lol6;&lol6;&lol6;&lol6;&lol6;&lol6;&lol6;&lol6;">
  <!ENTITY lol8 "&lol7;&lol7;&lol7;&lol7;&lol7;&lol7;&lol7;&lol7;&lol7;&lol7;">
  <!ENTITY lol9 "&lol8;&lol8;&lol8;&lol8;&lol8;&lol8;&lol8;&lol8;&lol8;&lol8;">
]>
<package>
  <vendor>&lol9;</vendor>
  <name>CMSIS</name>
  <releases><release version="5.9.0">notes</release></releases>
</package>
`
	root := writePack(t, "ARM.CMSIS.pdsc", bomb)

	contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

	if len(contributions) != 0 {
		t.Errorf("contributions = %#v, want none out of a document that declares its own entities", contributions)
	}
	if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
}

// The same refusal, and the same reason: an entity the document declares is an
// undefined entity here, so a SYSTEM entity never becomes a file read.
func TestAnExternalEntityIsRefusedAndNoFileIsRead(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.txt")
	writeTestFile(t, secret, "a-secret-nobody-asked-for\n")
	xxe := "<?xml version=\"1.0\"?>\n<!DOCTYPE package [\n  <!ENTITY xxe SYSTEM \"file://" + secret + "\">\n]>\n" +
		"<package>\n  <vendor>&xxe;</vendor>\n  <name>CMSIS</name>\n" +
		"  <releases><release version=\"5.9.0\">notes</release></releases>\n</package>\n"
	root := writePack(t, "ARM.CMSIS.pdsc", xxe)

	contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

	if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
	for _, contribution := range contributions {
		if strings.Contains(contribution.Claim.Value, "a-secret") {
			t.Fatalf("the content of another file reached a claim: %#v", contribution)
		}
	}
	if len(contributions) != 0 {
		t.Errorf("contributions = %#v, want none", contributions)
	}
}

// Reinterpreting bytes under an encoding this tool cannot decode would be a
// guess. CharsetReader stays nil precisely so that such a document is refused.
func TestADescriptorInAForeignEncodingIsRefused(t *testing.T) {
	root := writePack(t, "ARM.CMSIS.pdsc", strings.Replace(packDescriptor, "UTF-8", "ISO-8859-1", 1))

	contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

	if len(contributions) != 0 {
		t.Errorf("contributions = %#v, want none out of a document this tool cannot decode", contributions)
	}
	if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
}

// The three bounds of section 30, each with the shape of excess it exists for.
// Nothing is taken from a document that breached one: a bound that still let a
// value through would bound nothing.
func TestADescriptorThatBreachesABoundIsRefusedWhole(t *testing.T) {
	deep := "<package>" + strings.Repeat("<a>", maxPDSCDepth+8) + strings.Repeat("</a>", maxPDSCDepth+8) + "</package>"
	many := "<package><vendor>ARM</vendor>" + strings.Repeat("<a/>", maxPDSCElements+1) + "</package>"
	large := "<package><vendor>ARM</vendor><description>" +
		strings.Repeat("x", maxPDSCBytes) + "</description></package>"
	for _, testCase := range []struct{ name, content string }{
		{name: "nested deeper than section 30 allows", content: deep},
		{name: "more elements than section 30 allows", content: many},
		{name: "larger than section 30 allows", content: large},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := writePack(t, "ARM.CMSIS.pdsc", testCase.content)

			contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

			if len(contributions) != 0 {
				t.Errorf("contributions = %#v, want none out of a document that breached a bound", contributions)
			}
			if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
				t.Fatalf("findings = %#v, want one INPUT_LIMIT_EXCEEDED", findings)
			}
			if findings[0].Subject.Ref != filepath.Join(root, "ARM.CMSIS.pdsc") {
				t.Errorf("subject = %q, want the descriptor rather than the component", findings[0].Subject.Ref)
			}
		})
	}
}

// A directory holding two descriptors is a pack index or a cache, not a pack.
// Picking one of them would attribute somebody else's pack to this component.
func TestTwoDescriptorsInOneRootAreRefused(t *testing.T) {
	root := writePack(t, "ARM.CMSIS.pdsc", packDescriptor)
	writeTestFile(t, filepath.Join(root, "Keil.STM32F4xx_DFP.pdsc"),
		strings.Replace(packDescriptor, "5.9.0", "2.17.1", 1))

	contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})

	if len(contributions) != 0 {
		t.Errorf("contributions = %#v, want none: neither descriptor was read", contributions)
	}
	if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
	if findings[0].Subject.Ref != root {
		t.Errorf("subject = %q, want the root that holds both", findings[0].Subject.Ref)
	}
}

// The registry order is behaviour now that two readers can claim the same
// field: an SBOM the upstream shipped is rank 4 and has to keep the version,
// while the descriptor still supplies the supplier the SBOM did not state.
func TestABundledSBOMOutranksAPackDescriptor(t *testing.T) {
	build := t.TempDir()
	packageRoot := filepath.Join(t.TempDir(), "p")
	writeTestFile(t, filepath.Join(packageRoot, "ARM.CMSIS.pdsc"), packDescriptor)
	writeTestFile(t, filepath.Join(packageRoot, "sbom.cdx.json"), `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "version": 1,
  "metadata": {"component": {"type": "library", "name": "CMSIS", "version": "6.0.0"}}
}`)
	writeConan(t, build, "CMSIS", "0.0.1", packageRoot)

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Version.Value != "6.0.0" || found.Version.Source != "bundled-sbom" {
		t.Errorf("version = %q from %q, want the bundled document to keep the field",
			found.Version.Value, found.Version.Source)
	}
	if found.Supplier.Value != "ARM" || found.Supplier.Source != "cmsis-pack" {
		t.Errorf("supplier = %q from %q, want the vendor only the descriptor states",
			found.Supplier.Value, found.Supplier.Source)
	}
	var kept bool
	for _, contribution := range found.Superseded {
		if contribution.Field == FieldVersion && contribution.Claim.Value == "5.9.0" {
			kept = true
		}
	}
	if !kept {
		t.Errorf("superseded = %#v, want the pack's declared version kept", found.Superseded)
	}
}

// Both describe the same package, and where neither origin outranks the other
// the manager that installed it is the one to believe.
func TestTheOwningManagerKeepsTheVersionAPackDescriptorAlsoStates(t *testing.T) {
	build := t.TempDir()
	packageRoot := filepath.Join(t.TempDir(), "p")
	writeTestFile(t, filepath.Join(packageRoot, "ARM.CMSIS.pdsc"), packDescriptor)
	writeConan(t, build, "CMSIS", "0.0.1", packageRoot)

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Version.Value != "0.0.1" || found.Version.Source != "conan" {
		t.Errorf("version = %q from %q, want the manager's own answer",
			found.Version.Value, found.Version.Source)
	}
	if found.Supplier.Value != "ARM" {
		t.Errorf("supplier = %q, want the one the descriptor stated", found.Supplier.Value)
	}
}

// The rule of this package: describing a package must not change what it
// covers, and a descriptor lying in a tree no manager owns brings no package
// into existence.
func TestAPackDescriptorAddsNoPackageAndNoFile(t *testing.T) {
	build := t.TempDir()
	packageRoot := filepath.Join(t.TempDir(), "p")
	writeConan(t, build, "CMSIS", "0.0.1", packageRoot)
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	bare, _ := Discover(Options{BuildDir: build, Context: context.Background()})

	// A second pack, in a directory nothing settled as a component root.
	writeTestFile(t, filepath.Join(build, "packs", "Keil", "STM32F4xx_DFP", "2.17.1", "Keil.STM32F4xx_DFP.pdsc"),
		packDescriptor)
	writeTestFile(t, filepath.Join(packageRoot, "ARM.CMSIS.pdsc"), packDescriptor)
	described, _ := Discover(Options{BuildDir: build, Context: context.Background()})

	if len(described) != len(bare) || len(described) != 1 {
		t.Fatalf("packages = %#v, want the %d the manager recorded", described, len(bare))
	}
	if described[0].Supplier.Value != "ARM" {
		t.Fatalf("the descriptor was not read at all; there is nothing to check here")
	}
	if len(described[0].Files) != len(bare[0].Files) || len(described[0].Roots) != len(bare[0].Roots) {
		t.Errorf("files/roots = %#v / %#v, want the manager's own", described[0].Files, described[0].Roots)
	}
	if described[0].Name != bare[0].Name {
		t.Errorf("name = %q, want %q: a reader does not rename a component", described[0].Name, bare[0].Name)
	}
}
