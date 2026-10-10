package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/licenselist"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
	"github.com/example/sbomb/internal/testutil"
)

// The SPDX goldens, and the test that keeps them honest.
//
// A golden only says that the output did not change; it does not say that the
// output was right when it was captured. Each SPDX golden is therefore taken
// from exactly the run its CycloneDX sibling of the same name is taken from,
// and TestTheSpdxDocumentLosesNothingTheCycloneDXDocumentCarries holds the
// two renderings of that one run against each other: whatever the CycloneDX
// document states about a component, the SPDX document must state about the
// element of the same identity. The CycloneDX writer is the older one and
// carries the larger test suite, so it is the reference; the SPDX document may
// say more, never less (section 28.11).

// spdxFixture is one build the goldens are captured from.
type spdxFixture struct {
	// name is the golden's base name: <name>.spdx.json, and <name>.cdx.json
	// where a CycloneDX golden exists.
	name string
	// cdx says how the CycloneDX golden of the same name relates to this run:
	// cdxNone, there is none; cdxExisting, it is committed and was captured
	// from exactly these arguments, so this run must reproduce it byte for
	// byte -- the proof that the SPDX golden describes the same build; or
	// cdxNew, it is captured here, beside the SPDX one.
	cdx cdxGoldenKind
	// assembly marks the fixtures whose product is a set of deliverables.
	assembly bool
	// prepare builds what the run reads and returns the generate arguments
	// that describe it, without --format, --spec-version and --output.
	prepare func(t *testing.T) []string
}

type cdxGoldenKind int

const (
	cdxNone cdxGoldenKind = iota
	cdxExisting
	cdxNew
)

// spdxFixtures is the golden table of the design: every fixture a CycloneDX
// golden is taken from, the two assembly fixtures, three fixtures that each
// exercise one edge of the relationship mapping (a header-only component, a
// generated source, a package-manager component), and one curated build that
// sets every field a configuration or a bundled sbom.yml can set.
var spdxFixtures = []spdxFixture{
	{name: "gcc-make-p02", cdx: cdxExisting, prepare: func(t *testing.T) []string {
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "gcc-make", "p02-static"), "--policy", "lenient"}
	}},
	{name: "gcc-ninja-p10-fetchcontent-declared", cdx: cdxExisting, prepare: prepareDeclaredRevision},
	{name: "gcc-ninja-p14-foss", cdx: cdxExisting, prepare: func(t *testing.T) []string {
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"), "--policy", "lenient",
			"--source-dir", testutil.CorpusSourceTree(t)}
	}},
	{name: "gcc-ninja-p14-foss-licensetext", cdx: cdxExisting, prepare: func(t *testing.T) []string {
		configuration := filepath.Join(t.TempDir(), "licencetext.json")
		if err := os.WriteFile(configuration, []byte(licenceTextConfiguration), 0o600); err != nil {
			t.Fatal(err)
		}
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"), "--policy", "lenient",
			"--source-dir", testutil.CorpusSourceTree(t), "--config", configuration}
	}},
	{name: "gcc-ninja-p14-foss-modified", cdx: cdxExisting, prepare: func(t *testing.T) []string {
		requireGit(t)
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
			"--source-dir", materializeFossRepositories(t), "--allow-introspection=git", "--policy", "lenient"}
	}},
	{name: "msvc-ninja-p02", cdx: cdxExisting, prepare: func(t *testing.T) []string {
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "msvc-ninja", "p02-static"),
			"--path-flavor", "windows", "--policy", "lenient"}
	}},
	{name: "msvc-nmake-p02", cdx: cdxExisting, prepare: func(t *testing.T) []string {
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "msvc-nmake", "p02-static"),
			"--path-flavor", "windows", "--policy", "lenient"}
	}},
	{name: "msvc-vs17-p02", cdx: cdxExisting, prepare: func(t *testing.T) []string {
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "msvc-vs17", "p02-static"),
			"--config-name", "Debug", "--path-flavor", "windows", "--policy", "lenient"}
	}},
	{name: "gcc-ninja-p15-shared", assembly: true, prepare: func(t *testing.T) []string {
		root := testutil.RepoRoot(t)
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p15-shared"),
			"--config", filepath.Join(root, "testdata", "config", "p15-shared.json"),
			"--source-dir", filepath.Join(root, "tools", "fixtures", "projects", "p15-shared")}
	}},
	{name: "gcc-ninja-p12-assets", assembly: true, prepare: func(t *testing.T) []string {
		root := testutil.RepoRoot(t)
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p12-assets"),
			"--config", filepath.Join(root, "testdata", "config", "p12-assets.json"),
			"--source-dir", filepath.Join(root, "tools", "fixtures", "projects", "p12-assets")}
	}},
	// These three run under the lenient profile like every other golden: the
	// default profile fails them on findings that have nothing to do with the
	// document (exit 3), and a golden of a failed run would be no document.
	{name: "gcc-ninja-p05-headeronly", prepare: func(t *testing.T) []string {
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p05-headeronly"), "--policy", "lenient"}
	}},
	{name: "gcc-ninja-p04-generated", prepare: func(t *testing.T) []string {
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p04-generated"), "--policy", "lenient"}
	}},
	{name: "clang-ninja-p11-conan", prepare: func(t *testing.T) []string {
		return []string{"--build-dir", testutil.CorpusBuildDir(t, "clang-ninja", "p11-conan"), "--policy", "lenient"}
	}},
	{name: "gcc-ninja-p14-foss-curated", cdx: cdxNew, prepare: prepareCurated},
}

// requireGit skips a fixture that builds repositories when git is not there,
// as the CycloneDX tests of the same fixtures do.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required to build the fixture repositories")
	}
}

// prepareDeclaredRevision is the setup of the "standing on the declared
// revision" case of TestModificationStatusAgainstADeclaredRevision: tinylog's
// checkout as a repository whose HEAD is the tag the build declared.
func prepareDeclaredRevision(t *testing.T) []string {
	requireGit(t)
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p10-fetchcontent")
	checkout := filepath.Join(buildDir, "_deps", "tinylog-src")
	fixtureGit(t, checkout, "init", "-q", "-b", "main")
	fixtureGit(t, checkout, "add", "-A")
	fixtureGit(t, checkout, "commit", "-qm", "tinylog v1.2.0", "--no-gpg-sign")
	fixtureGit(t, checkout, "tag", "-f", "v1.2.0")
	return []string{"--build-dir", buildDir, "--policy", "lenient", "--allow-introspection=git"}
}

// prepareCurated is the kitchen-sink build: the FOSS fixture with a bundled
// sbom.yml in one component root (supplier, originator, description, cpe,
// version, and an upstream CVE exclusion with and without a reason) and a
// configuration that curates a copyright, a purl, a compound expression, a
// licence that is only a name, and a licence that contradicts the
// component's own files. None of the corpus builds sets these, so without it
// the native SPDX fields they map to would be in no golden at all.
func prepareCurated(t *testing.T) []string {
	t.Helper()
	root := testutil.RepoRoot(t)
	source := testutil.CorpusSourceTreeCopy(t)
	manifest, err := os.ReadFile(filepath.Join(root, "testdata", "config", "curated", "sbom.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "dep", "mit-lib", "sbom.yml"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
		"--config", filepath.Join(root, "testdata", "config", "curated.json"), "--source-dir", source}
}

// fixtureRenders is one fixture's build written three ways from one prepared
// input: SPDX 3.0.1, CycloneDX 1.6 and CycloneDX 1.7.
type fixtureRenders struct {
	spdx, cdx16, cdx17 []byte
}

// renderCache keeps the renders of each fixture for the rest of the test
// binary: the golden test and the cross-check read the same three documents,
// and generating them twice would double the cost of both for nothing. The
// tests of this file do not run in parallel, so a plain map is enough.
var renderCache = map[string]fixtureRenders{}

// renderFixture prepares a fixture once and generates it in each format, with
// SOURCE_DATE_EPOCH pinned so the SPDX document is reproducible (section 29).
func renderFixture(t *testing.T, fixture spdxFixture) fixtureRenders {
	t.Helper()
	if cached, found := renderCache[fixture.name]; found {
		return cached
	}
	t.Setenv("SOURCE_DATE_EPOCH", pinnedEpoch)
	args := fixture.prepare(t)
	run := func(format, version, extension string) []byte {
		output := filepath.Join(t.TempDir(), "out"+extension)
		code, _, stderr := execute(append(append([]string{"generate"}, args...),
			"--format", format, "--spec-version", version, "--output", output, "--reproducible"))
		if code != 0 || stderr != "" {
			t.Fatalf("%s: generate --format %s --spec-version %s = code %d, stderr %q",
				fixture.name, format, version, code, stderr)
		}
		return readFile(t, output)
	}
	renders := fixtureRenders{
		spdx:  run("spdx-json", "3.0.1", ".spdx.json"),
		cdx16: run("cyclonedx-json", "1.6", ".cdx.json"),
		cdx17: run("cyclonedx-json", "1.7", ".cdx.json"),
	}
	renderCache[fixture.name] = renders
	return renders
}

// holdToBothTiers runs the writer's own checks on a document: conformance
// (tier a) and the invariants of sbomb's output (tier b). Every golden must
// pass both, or the golden would preserve a document sbomb refuses to write.
func holdToBothTiers(t *testing.T, name string, data []byte) {
	t.Helper()
	writer, version, err := sbomwriter.DetectFormat(data)
	if err != nil || writer.ID() != "spdx-json" || version != "3.0.1" {
		t.Fatalf("%s is not an SPDX 3.0.1 document (%v)", name, err)
	}
	if err := writer.Validate(bytes.NewReader(data)); err != nil {
		t.Errorf("%s is not conformant: %v", name, err)
	}
	checker, ok := writer.(sbomwriter.OutputChecker)
	if !ok {
		t.Fatal("the SPDX writer no longer checks its own output")
	}
	if err := checker.CheckOutput(data); err != nil {
		t.Errorf("%s breaks an invariant of sbomb's own output: %v", name, err)
	}
}

// TestSpdxGoldenDocuments captures one SPDX 3.0.1 document per fixture. What
// a golden cannot say for itself is asserted beside it: that the CycloneDX
// golden of the same name is reproduced by the same arguments (so both
// describe one build), that the document passes both tiers of checks, that no
// relationship says `generates` (section 28.11.4: a generated source adds no
// edge), that the extension profile is not claimed (deviation D49b), and, for
// an assembly, that the product contains at least two deliverables, each a
// package of its own.
func TestSpdxGoldenDocuments(t *testing.T) {
	for _, fixture := range spdxFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			renders := renderFixture(t, fixture)
			switch fixture.cdx {
			case cdxExisting:
				committed := readFile(t, goldenPath(t, fixture.name+".cdx.json"))
				if !bytes.Equal(renders.cdx16, committed) {
					t.Fatalf("these arguments no longer reproduce %s.cdx.json, so the SPDX golden would describe another build", fixture.name)
				}
			case cdxNew:
				assertGolden(t, fixture.name+".cdx.json", renders.cdx16)
			}
			holdToBothTiers(t, fixture.name, renders.spdx)
			graph := parseSpdxGraph(t, renders.spdx)
			for _, relationship := range graph.relationships {
				if relationship.relationshipType == "generates" {
					t.Errorf("%s: %s generates %v; a generated source adds no relationship", fixture.name, relationship.from, relationship.to)
				}
			}
			for _, profile := range anyStrings(graph.document["profileConformance"]) {
				if profile == "extension" {
					t.Errorf("%s claims the extension profile", fixture.name)
				}
			}
			if fixture.assembly {
				assertAssembly(t, graph)
			}
			assertGolden(t, fixture.name+".spdx.json", renders.spdx)
		})
	}
}

// assertAssembly is the assembly-mode check of the goldens: the product is
// the root, it contains the deliverables, and each deliverable is a package
// that links or embeds components of its own.
func assertAssembly(t *testing.T, graph *spdxGraph) {
	t.Helper()
	sbom := graph.nodes["sbom"]
	roots, _ := sbom["rootElement"].([]any)
	if len(roots) != 1 {
		t.Fatalf("the Sbom has %d root elements, want the product alone", len(roots))
	}
	product := localIdentity(roots[0].(string))
	var artifacts []string
	for local, node := range graph.nodes {
		if strings.HasPrefix(local, "artifact:") && node["type"] == "software_Package" {
			artifacts = append(artifacts, local)
		}
	}
	sort.Strings(artifacts)
	if len(artifacts) < 2 {
		t.Fatalf("an assembly with %d deliverable packages (%v)", len(artifacts), artifacts)
	}
	contained := graph.targets(product, "contains")
	for _, artifact := range artifacts {
		if !contained[artifact] {
			t.Errorf("the product %s does not contain %s", product, artifact)
		}
		reached := false
		for _, relationship := range graph.from[artifact] {
			if len(relationship.to) > 0 {
				reached = true
			}
		}
		if !reached {
			t.Errorf("%s reaches no component", artifact)
		}
	}
}

// TestTheSyntheticDocumentGolden is the one golden no build produces: the
// synthetic document in which every field of the format-neutral document is
// set. It is where the rare shapes -- a missing file, a digest of an unknown
// algorithm, a licence text that is not UTF-8, patches, VEX statements, every
// component type and file class -- are visible in a review at all, because no
// fixture of the corpus has them.
func TestTheSyntheticDocumentGolden(t *testing.T) {
	writer, err := sbomwriter.Get("spdx-json", "3.0.1")
	if err != nil {
		t.Fatal(err)
	}
	options := sbomwritertest.AllFieldsOptions()
	options.SpecVersion = "3.0.1"
	var buffer bytes.Buffer
	if err := writer.Write(&buffer, sbomwritertest.AllFields(), options); err != nil {
		t.Fatal(err)
	}
	holdToBothTiers(t, "synthetic-all-fields", buffer.Bytes())
	assertGolden(t, "synthetic-all-fields.spdx.json", buffer.Bytes())
}

// TestTheSpdxDocumentLosesNothingTheCycloneDXDocumentCarries is the
// completeness cross-check of section 28.11, run over every golden fixture:
// the same prepared build is written as CycloneDX 1.6, CycloneDX 1.7 and SPDX
// 3.0.1, and every statement of the CycloneDX documents is looked up in the
// SPDX one, on the element whose local identity is the CycloneDX bom-ref.
// The value-level coverage test in internal/spdx/mapping proves that each
// field of the neutral document reaches SPDX; this proves it of real builds,
// where the interesting cases are combinations no synthetic document thought
// of.
func TestTheSpdxDocumentLosesNothingTheCycloneDXDocumentCarries(t *testing.T) {
	for _, fixture := range spdxFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			crossCheck(t, fixture.name, renderFixture(t, fixture))
		})
	}
}

// TestTheSyntheticSpdxDocumentLosesNothingTheCycloneDXDocumentCarries runs the
// same cross-check over the synthetic document, rendered by both writers from
// one format-neutral document. It holds the shapes no fixture of the corpus
// has to the same standard: among them a licence expression with lower-case
// operators and an identifier the licence list does not carry, read from an
// SPDX-License-Identifier line, which CycloneDX carries as written and SPDX
// must carry with its structure rather than as its first identifier.
func TestTheSyntheticSpdxDocumentLosesNothingTheCycloneDXDocumentCarries(t *testing.T) {
	crossCheck(t, "synthetic-all-fields", syntheticRenders(t))
}

// TestTheCrossCheckReportsAnSpdxLicenceThatSaysMore: the cross-check is only
// as strong as its matching. A licence that is a name in CycloneDX becomes a
// LicenseRef sbomb minted for it in SPDX; an SPDX expression that merely
// contains that reference -- "LicenseRef-sbomb-X AND <anything>" -- states a
// different licence, with obligations CycloneDX does not state, and must be
// reported as a loss rather than found.
func TestTheCrossCheckReportsAnSpdxLicenceThatSaysMore(t *testing.T) {
	renders := syntheticRenders(t)
	if problems := crossCheckProblems(t, renders); len(problems) != 0 {
		t.Fatalf("the untouched synthetic document already reports losses: %v", problems)
	}
	edited := 0
	renders.spdx = editSpdxGraph(t, renders.spdx, func(graph []any) []any {
		for _, node := range graph {
			node := node.(map[string]any)
			expression, _ := node["simplelicensing_licenseExpression"].(string)
			if strings.HasPrefix(expression, "LicenseRef-sbomb-") && !strings.Contains(expression, " ") {
				node["simplelicensing_licenseExpression"] = expression + " AND LicenseRef-not-in-the-cyclonedx-document"
				edited++
			}
		}
		return graph
	})
	if edited == 0 {
		t.Fatal("the synthetic document has no licence that is one minted reference; the test checks nothing")
	}
	if problems := crossCheckProblems(t, renders); len(problems) == 0 {
		t.Errorf("%d licence expressions were widened by an extra AND operand, and the cross-check found every one", edited)
	}
}

// TestTheCrossCheckReportsAVersionConfidenceThatSaysOtherwise: CycloneDX
// states how sure sbomb is of a version as a number, SPDX as the level it was
// computed from. An SPDX document that names another level than the number
// stands for contradicts the CycloneDX document; the property being present
// is not enough.
func TestTheCrossCheckReportsAVersionConfidenceThatSaysOtherwise(t *testing.T) {
	renders := syntheticRenders(t)
	edited := 0
	renders.spdx = editSpdxGraph(t, renders.spdx, func(graph []any) []any {
		for _, node := range graph {
			extensions, _ := node.(map[string]any)["extension"].([]any)
			for _, extension := range extensions {
				entries, _ := extension.(map[string]any)["extension_cdxProperty"].([]any)
				for _, entry := range entries {
					fields := entry.(map[string]any)
					if fields["extension_cdxPropName"] == "sbomb:version:confidence" && fields["extension_cdxPropValue"] != "low" {
						fields["extension_cdxPropValue"] = "low"
						edited++
					}
				}
			}
		}
		return graph
	})
	if edited == 0 {
		t.Fatal("the synthetic document states no version confidence other than low; the test checks nothing")
	}
	problems := crossCheckProblems(t, renders)
	if len(problems) < edited {
		t.Errorf("%d version confidences were changed to low, and the cross-check reported %d problem(s): %v", edited, len(problems), problems)
	}
	for _, problem := range problems {
		if !strings.Contains(problem, "sbomb:version:confidence") {
			t.Errorf("unexpected problem %q", problem)
		}
	}
}

// TestTheCrossCheckReportsAFileThatBecameAPackage: a file element states no
// sbomb:component:type, so its class is where its type lives. An SPDX
// document that calls every source and object file a library package has
// lost what CycloneDX's type "file" says, and must be reported.
func TestTheCrossCheckReportsAFileThatBecameAPackage(t *testing.T) {
	renders := syntheticRenders(t)
	edited := 0
	renders.spdx = editSpdxGraph(t, renders.spdx, func(graph []any) []any {
		for _, node := range graph {
			node := node.(map[string]any)
			if node["type"] == "software_File" {
				node["type"] = "software_Package"
				node["software_primaryPurpose"] = "library"
				edited++
			}
		}
		return graph
	})
	if edited == 0 {
		t.Fatal("the synthetic document has no file; the test checks nothing")
	}
	problems := crossCheckProblems(t, renders)
	reported := 0
	for _, problem := range problems {
		if strings.Contains(problem, "want a software_File") {
			reported++
		}
	}
	if reported == 0 {
		t.Errorf("%d files were turned into library packages, and the cross-check reported none of them: %v", edited, problems)
	}
}

// TestTheCrossCheckReportsARenamedComponentOfTypeFile: a component of type
// file that is no used file has no canonical path, and is named in SPDX as
// CycloneDX names it. Its name is checked like any other component's.
func TestTheCrossCheckReportsARenamedComponentOfTypeFile(t *testing.T) {
	renders := syntheticRenders(t)
	edited := false
	renders.spdx = editSpdxGraph(t, renders.spdx, func(graph []any) []any {
		for _, node := range graph {
			node := node.(map[string]any)
			if id, _ := node["spdxId"].(string); localIdentity(id) == "component:type-file" {
				node["name"] = "a-different-name"
				edited = true
			}
		}
		return graph
	})
	if !edited {
		t.Fatal("the synthetic document has no component:type-file; the test checks nothing")
	}
	problems := crossCheckProblems(t, renders)
	if !slices.Contains(problems, `component:type-file: name "a-different-name", want "type-file"`) {
		t.Errorf("the component of type file was renamed, and the cross-check reported: %v", problems)
	}
}

// TestTheCrossCheckReportsLicenceTextsOnTheWrongFiles: CycloneDX binds each
// retained text to the licence it is the text of. Swapping the texts of two
// licence files of one component, each of which states its own licence,
// leaves every text on some evidence file and every licence stated, and
// still makes each file state the other's licence for its bytes.
func TestTheCrossCheckReportsLicenceTextsOnTheWrongFiles(t *testing.T) {
	renders := syntheticRenders(t)
	first, second := "file:build:_deps/kitchen-src/LICENSE", "file:build:_deps/kitchen-src/LICENSE.retained"
	swapped := false
	renders.spdx = editSpdxGraph(t, renders.spdx, func(graph []any) []any {
		nodes := map[string]map[string]any{}
		for _, node := range graph {
			node := node.(map[string]any)
			if id, _ := node["spdxId"].(string); localIdentity(id) == first || localIdentity(id) == second {
				nodes[localIdentity(id)] = node
			}
		}
		if nodes[first]["externalRef"] != nil && nodes[second]["externalRef"] != nil {
			nodes[first]["externalRef"], nodes[second]["externalRef"] = nodes[second]["externalRef"], nodes[first]["externalRef"]
			swapped = true
		}
		return graph
	})
	if !swapped {
		t.Fatalf("the synthetic document has no licence texts on %s and %s; the test checks nothing", first, second)
	}
	problems := crossCheckProblems(t, renders)
	reported := 0
	for _, problem := range problems {
		if strings.Contains(problem, "none of which states that licence") {
			reported++
		}
	}
	if reported == 0 {
		t.Errorf("two licence texts were swapped between their files, and the cross-check reported: %v", problems)
	}
}

// syntheticRenders writes the synthetic document in the three renders the
// cross-check compares, through the registered writers.
func syntheticRenders(t *testing.T) fixtureRenders {
	t.Helper()
	render := func(id, version string) []byte {
		t.Helper()
		writer, err := sbomwriter.Get(id, version)
		if err != nil {
			t.Fatal(err)
		}
		options := sbomwritertest.AllFieldsOptions()
		options.SpecVersion = version
		var buffer bytes.Buffer
		if err := writer.Write(&buffer, sbomwritertest.AllFields(), options); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	return fixtureRenders{
		spdx:  render("spdx-json", "3.0.1"),
		cdx16: render("cyclonedx-json", "1.6"),
		cdx17: render("cyclonedx-json", "1.7"),
	}
}

// crossCheck looks up every statement of the CycloneDX renders in the SPDX one.
func crossCheck(t *testing.T, name string, renders fixtureRenders) {
	t.Helper()
	if problems := crossCheckProblems(t, renders); len(problems) > 0 {
		t.Errorf("%s: the SPDX document lost %d statement(s) of the CycloneDX document:\n  %s",
			name, len(problems), strings.Join(problems, "\n  "))
	}
}

// crossCheckProblems is every statement of the CycloneDX renders the SPDX one
// does not carry, sorted.
func crossCheckProblems(t *testing.T, renders fixtureRenders) []string {
	t.Helper()
	graph := parseSpdxGraph(t, renders.spdx)
	cdx := parseCycloneDX(t, renders.cdx16)
	cdx17 := parseCycloneDX(t, renders.cdx17)

	var problems []string
	report := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	for _, component := range cdx.allComponents() {
		compareComponent(graph, component, report)
	}
	for _, component := range cdx17.allComponents() {
		if component.IsExternal && !graph.hasRelationship("", "hasProvidedDependency", component.BomRef, "runtime") {
			report("%s: CycloneDX 1.7 says the environment provides it; no hasProvidedDependency with scope runtime names it", component.BomRef)
		}
	}
	compareDependencies(graph, cdx, report)
	compareMetadata(graph, cdx, report)
	sort.Strings(problems)
	return problems
}

// compareComponent looks up every statement of one CycloneDX component.
func compareComponent(graph *spdxGraph, component cdxComponent, report func(string, ...any)) {
	ref := component.BomRef
	node, found := graph.nodes[ref]
	if !found {
		report("%s: no SPDX element has this identity", ref)
		return
	}
	text := func(key string) string { value, _ := node[key].(string); return value }
	properties := extensionOf(node)

	// Every property, byte for byte: section 28.11.7 makes the SPDX
	// extension carry the property set the CycloneDX 1.6 writer emits.
	for _, property := range component.Properties {
		if !properties[property.Name+"="+property.Value] {
			report("%s: property %s=%q is not in the extension", ref, property.Name, property.Value)
		}
	}

	if canonical := component.property("sbomb:path:canonical"); component.Type == "file" && canonical != "" {
		// A used file is a software_File named by its canonical path in SPDX
		// (section 28.11.3), where CycloneDX names it by its base name. A
		// file element has no sbomb:component:type, so its class is the one
		// place its type is stated: a used file that became a package would
		// lose it.
		if node["type"] != "software_File" {
			report("%s: a file is a %v, want a software_File", ref, node["type"])
		}
		if text("name") != canonical {
			report("%s: name %q, want the canonical path %q", ref, text("name"), canonical)
		}
	} else {
		// Everything else -- a component of type file among them, which has
		// no path and is no used file -- is a package named as CycloneDX
		// names it, whose type the checks below recover.
		if text("name") != component.Name {
			report("%s: name %q, want %q", ref, text("name"), component.Name)
		}
		// The type must be recoverable: exactly, from sbomb:component:type,
		// which the mapping writes for every typed component; or, for a
		// component whose type was never set and that CycloneDX defaults,
		// from the purpose, which defaults the same way (section 28.11.3).
		if typed := propertyValues(properties, "sbomb:component:type"); len(typed) > 0 {
			if len(typed) != 1 || typed[0] != component.Type {
				report("%s: sbomb:component:type %v, want %q", ref, typed, component.Type)
			}
		} else if (component.Type != "library" && component.Type != "application") || text("software_primaryPurpose") != component.Type {
			report("%s: the type %q is not recoverable: no sbomb:component:type, and the purpose is %q", ref, component.Type, text("software_primaryPurpose"))
		}
	}
	if component.Version != "" && text("software_packageVersion") != component.Version {
		report("%s: version %q, want %q", ref, text("software_packageVersion"), component.Version)
	}
	if component.Purl != "" {
		if text("software_packageUrl") != component.Purl {
			report("%s: packageUrl %q, want %q", ref, text("software_packageUrl"), component.Purl)
		}
		if !hasExternalIdentifier(node, "packageUrl", component.Purl) {
			report("%s: no packageUrl external identifier %q", ref, component.Purl)
		}
	}
	if component.Cpe != "" {
		kind := "cpe23"
		if strings.HasPrefix(component.Cpe, "cpe:/") {
			kind = "cpe22"
		}
		if !hasExternalIdentifier(node, kind, component.Cpe) {
			report("%s: no %s external identifier %q", ref, kind, component.Cpe)
		}
	}
	if component.Supplier != nil && component.Supplier.Name != "" {
		supplier, _ := node["suppliedBy"].(string)
		if graph.name(localIdentity(supplier)) != component.Supplier.Name {
			report("%s: suppliedBy %q, want an Organization named %q", ref, supplier, component.Supplier.Name)
		}
	}
	if originator := component.property("sbomb:component:originator"); originator != "" {
		named := false
		for _, agent := range anyStrings(node["originatedBy"]) {
			named = named || graph.name(localIdentity(agent)) == originator
		}
		if !named {
			report("%s: no originatedBy Organization named %q", ref, originator)
		}
	}
	if component.Description != "" && text("description") != component.Description {
		report("%s: description %q, want %q", ref, text("description"), component.Description)
	}
	// The curated notice opens the package's copyright text; what follows it
	// are the statements read from no file (section 28.11.3).
	if copyright := text("software_copyrightText"); component.Copyright != "" &&
		copyright != component.Copyright && !strings.HasPrefix(copyright, component.Copyright+"\n") {
		report("%s: copyrightText %q, want the curated %q first", ref, copyright, component.Copyright)
	}
	for _, hash := range component.Hashes {
		if !hasHash(node, spdxAlgorithm(hash.Alg), hash.Content) {
			report("%s: no %s hash %s", ref, hash.Alg, hash.Content)
		}
	}
	for _, reference := range component.ExternalReferences {
		if !hasExternalRef(node, reference.Type, reference.URL) {
			report("%s: no %s external reference %q", ref, reference.Type, reference.URL)
		}
	}

	// The concluded licence is the component's own statement.
	for _, choice := range component.Licenses {
		if !graph.licenceStated([]string{ref}, "hasConcludedLicense", choice.text()) {
			report("%s: no hasConcludedLicense to %q", ref, choice.text())
		}
	}
	// Observed licences, and their texts, are statements of the component
	// or of the evidence files it was read from (section 28.11.5). CycloneDX's
	// "declared" -- the authors wrote the identifier into the file -- is
	// hasDeclaredLicense in SPDX as well. Its "concluded" is not one SPDX
	// verb: the 3.0.1 vocabulary calls a licence a tool found in a file
	// declared, and only a curated licence concluded, which CycloneDX does not
	// tell apart. So a concluded evidence licence is owed a licence
	// relationship of either type; which one is pinned by the mapping's own
	// tests and the goldens.
	evidence := graph.targets(ref, "hasEvidence")
	holders := append([]string{ref}, keys(evidence)...)
	if component.Evidence != nil {
		for _, choice := range component.Evidence.Licenses {
			relationshipType := ""
			if choice.License != nil {
				switch choice.License.Acknowledgement {
				case "declared":
					relationshipType = "hasDeclaredLicense"
				}
			}
			// A retained text nobody identified is CycloneDX's NOASSERTION
			// without an acknowledgement. SPDX 3.0.1 says the same by
			// stating no licence relationship at all -- an absent
			// relationship is no assertion -- so only its text is owed.
			unidentified := (choice.License == nil || choice.License.Acknowledgement == "") && choice.text() == "NOASSERTION"
			if !unidentified && !graph.licenceStated(holders, relationshipType, choice.text()) {
				report("%s: evidence licence %q (%s) has no %s relationship from the component or its evidence files",
					ref, choice.text(), choice.acknowledgement(), relationshipOrAny(relationshipType))
			}
			// CycloneDX binds a retained text to one licence: these bytes are
			// MIT. SPDX says the same only when the file that carries the bytes
			// is the file that states the licence; a text on one file and the
			// licence on another would make each file state the other's
			// licence. An unidentified text is bound to no licence, and its
			// file states none.
			if choice.License != nil && choice.License.Text != nil && choice.License.Text.Content != "" {
				carriers := graph.filesCarryingText(keys(evidence), choice.License.Text.Content, choice.License.Text.Encoding)
				if len(carriers) == 0 {
					report("%s: the text of evidence licence %q is attached to none of its evidence files", ref, choice.text())
				}
				bound := false
				for _, file := range carriers {
					if unidentified {
						bound = bound || !graph.licenceStated([]string{file}, "", "")
					} else {
						bound = bound || graph.licenceStated([]string{file}, relationshipType, choice.text())
					}
				}
				if len(carriers) > 0 && !bound {
					report("%s: the text of evidence licence %q is on %v, none of which states that licence", ref, choice.text(), carriers)
				}
			}
		}
		// A statement read from no file is in the package's own copyright
		// text: there is no file for it to sit on (section 28.11.3).
		statements := graph.copyrightLines(append(append(keys(evidence), keys(graph.targets(ref, "contains"))...), ref))
		for _, copyright := range component.Evidence.Copyright {
			if !statements[copyright.Text] {
				report("%s: evidence copyright %q is on none of its files", ref, copyright.Text)
			}
		}
		for _, identity := range component.Evidence.Identity {
			if identity.Field != "version" {
				continue
			}
			for _, method := range identity.Methods {
				if method.Value != "" && !properties["sbomb:version:source="+method.Value] {
					report("%s: the version's source %q is not sbomb:version:source", ref, method.Value)
				}
			}
			if identity.ConcludedValue != "" && text("software_packageVersion") != identity.ConcludedValue {
				report("%s: the version's concluded value %q is not the packageVersion %q", ref, identity.ConcludedValue, text("software_packageVersion"))
			}
			// CycloneDX states the confidence as a number, sbomb's own scale
			// mapped through domain.Confidence.Float; SPDX states the level
			// itself. The two must name the same level, not merely both be
			// present. The lowest number is also what a component without a
			// recorded level gets, and SPDX then states no level at all.
			stated := propertyValues(properties, "sbomb:version:confidence")
			level, known := confidenceLevel(identity.Confidence)
			switch {
			case !known:
				report("%s: the version's confidence %v is on no level of sbomb's scale", ref, identity.Confidence)
			case level == domain.ConfidenceUnknown && len(stated) == 0:
			case len(stated) != 1 || stated[0] != string(level):
				report("%s: sbomb:version:confidence %v, want %q for the confidence %v", ref, stated, level, identity.Confidence)
			}
		}
	}

	if component.Pedigree != nil {
		for _, commit := range component.Pedigree.Commits {
			if !properties["sbomb:component:vcsCommit="+commit.UID] {
				report("%s: pedigree commit %s is not sbomb:component:vcsCommit", ref, commit.UID)
			}
		}
		patches := graph.targets(ref, "patchedBy")
		// CycloneDX writes the modification signal and, in parentheses, each
		// patch as "file: description" into the notes, because its patch
		// object has no field for either. SPDX states the signal as the
		// package's sourceInfo and each patch as a file element named after
		// the patch file, with the description as its own. So the notes must
		// be the sourceInfo followed by labels of the patch files -- an exact
		// duplicate patch is one file, and its label may appear twice.
		if notes := component.Pedigree.Notes; notes != "" && notes != text("software_sourceInfo") {
			if !notesArePatchLabels(notes, text("software_sourceInfo"), graph, patches) {
				report("%s: sourceInfo %q and the patch files do not account for the pedigree notes %q", ref, text("software_sourceInfo"), notes)
			}
		}
		types := map[string]bool{}
		for _, patch := range component.Pedigree.Patches {
			types[patch.Type] = true
		}
		if len(patches) < len(types) {
			report("%s: %d patch files, want at least one for each of the %d patch types of the pedigree", ref, len(patches), len(types))
		}
		for _, patch := range component.Pedigree.Patches {
			typed := false
			for file := range patches {
				typed = typed || extensionOf(graph.nodes[file])["sbomb:patch:type="+patch.Type]
			}
			if !typed {
				report("%s: no patch file of type %q", ref, patch.Type)
			}
		}
	}

	if component.Scope == "excluded" && !graph.hasRelationship("", "usesTool", ref, "build") {
		report("%s: excluded from the deliverable, but no usesTool with scope build names it", ref)
	}

	// An upstream CVE exclusion is a VEX statement from the vulnerability
	// to the component (section 28.11.4).
	for _, property := range component.Properties {
		if property.Name != "sbomb:component:cveExclusion" {
			continue
		}
		cve, reason, _ := strings.Cut(property.Value, ": ")
		if !graph.hasVex(cve, ref, reason) {
			report("%s: the exclusion of %s (%q) is no doesNotAffect statement", ref, cve, reason)
		}
	}
}

// compareDependencies holds the SPDX relationships to the CycloneDX
// dependency graph. A non-empty dependsOn is some relationship between the
// same two identities -- which one depends on the linkage form, and that is
// the business of the mapping tests. An empty dependsOn is CycloneDX's
// positive statement that the element depends on nothing, which SPDX says
// with a complete relationship to NoneElement (section 28.11.4).
func compareDependencies(graph *spdxGraph, cdx *cdxDocument, report func(string, ...any)) {
	for _, dependency := range cdx.Dependencies {
		if len(dependency.DependsOn) == 0 {
			if !graph.knownLeaf(dependency.Ref) {
				report("%s: CycloneDX says it depends on nothing; no complete dependsOn NoneElement says so", dependency.Ref)
			}
			continue
		}
		for _, target := range dependency.DependsOn {
			if !graph.hasRelationship(dependency.Ref, "", target, "") {
				report("%s -> %s: no relationship between the two", dependency.Ref, target)
			}
		}
	}
}

// compareMetadata holds the document level: the product, the tool that wrote
// the document and its vendor, and the run properties.
func compareMetadata(graph *spdxGraph, cdx *cdxDocument, report func(string, ...any)) {
	roots := anyStrings(graph.nodes["sbom"]["rootElement"])
	if cdx.Metadata.Component != nil && (len(roots) != 1 || localIdentity(roots[0]) != cdx.Metadata.Component.BomRef) {
		report("the Sbom's root is %v, want the product %s", roots, cdx.Metadata.Component.BomRef)
	}
	createdUsing := map[string]bool{}
	for _, tool := range anyStrings(graph.creationInfo["createdUsing"]) {
		createdUsing[graph.name(localIdentity(tool))] = true
	}
	createdBy := map[string]bool{}
	for _, agent := range anyStrings(graph.creationInfo["createdBy"]) {
		createdBy[graph.name(localIdentity(agent))] = true
	}
	for _, tool := range cdx.Metadata.Tools {
		if !createdUsing[tool.Name] {
			report("tool %q is not in createdUsing", tool.Name)
		}
		if tool.Vendor != "" && !createdBy[tool.Vendor] {
			report("tool vendor %q is not in createdBy", tool.Vendor)
		}
		if tool.Version != "" && !extensionOf(graph.nodes["tool:"+tool.Name])["sbomb:run:toolVersion="+tool.Version] {
			report("tool %q carries no sbomb:run:toolVersion=%s", tool.Name, tool.Version)
		}
	}
	document := extensionOf(graph.document)
	for _, property := range cdx.Metadata.Properties {
		// The one run property whose value is the format's own version.
		if property.Name == "sbomb:run:specVersion" {
			property.Value = "3.0.1"
		}
		if !document[property.Name+"="+property.Value] {
			report("run property %s=%q is not on the SpdxDocument", property.Name, property.Value)
		}
	}
}

// --- the CycloneDX side, as far as the cross-check reads it ---

type cdxDocument struct {
	Metadata struct {
		Tools []struct {
			Vendor  string `json:"vendor"`
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"tools"`
		Component  *cdxComponent `json:"component"`
		Properties []cdxProperty `json:"properties"`
	} `json:"metadata"`
	Components   []cdxComponent `json:"components"`
	Dependencies []struct {
		Ref       string   `json:"ref"`
		DependsOn []string `json:"dependsOn"`
	} `json:"dependencies"`
}

type cdxProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type cdxComponent struct {
	BomRef      string `json:"bom-ref"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Purl        string `json:"purl"`
	Cpe         string `json:"cpe"`
	Copyright   string `json:"copyright"`
	Scope       string `json:"scope"`
	IsExternal  bool   `json:"isExternal"`
	Supplier    *struct {
		Name string `json:"name"`
	} `json:"supplier"`
	Hashes []struct {
		Alg     string `json:"alg"`
		Content string `json:"content"`
	} `json:"hashes"`
	Licenses           []cdxLicenseChoice `json:"licenses"`
	ExternalReferences []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"externalReferences"`
	Properties []cdxProperty `json:"properties"`
	Evidence   *struct {
		Licenses  []cdxLicenseChoice `json:"licenses"`
		Copyright []struct {
			Text string `json:"text"`
		} `json:"copyright"`
		Identity []struct {
			Field          string  `json:"field"`
			ConcludedValue string  `json:"concludedValue"`
			Confidence     float64 `json:"confidence"`
			Methods        []struct {
				Value string `json:"value"`
			} `json:"methods"`
		} `json:"identity"`
	} `json:"evidence"`
	Pedigree *struct {
		Commits []struct {
			UID string `json:"uid"`
		} `json:"commits"`
		Notes   string `json:"notes"`
		Patches []struct {
			Type string `json:"type"`
		} `json:"patches"`
	} `json:"pedigree"`
	Components []cdxComponent `json:"components"`
}

type cdxLicenseChoice struct {
	Expression string `json:"expression"`
	License    *struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Acknowledgement string `json:"acknowledgement"`
		Text            *struct {
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
		} `json:"text"`
	} `json:"license"`
}

// text is what the choice states: an expression, an id or a name.
func (c cdxLicenseChoice) text() string {
	switch {
	case c.Expression != "":
		return c.Expression
	case c.License == nil:
		return ""
	case c.License.ID != "":
		return c.License.ID
	default:
		return c.License.Name
	}
}

func (c cdxLicenseChoice) acknowledgement() string {
	if c.License == nil || c.License.Acknowledgement == "" {
		return "no acknowledgement"
	}
	return c.License.Acknowledgement
}

func (c cdxComponent) property(name string) string {
	for _, property := range c.Properties {
		if property.Name == name {
			return property.Value
		}
	}
	return ""
}

func parseCycloneDX(t *testing.T, data []byte) *cdxDocument {
	t.Helper()
	var document cdxDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("not a CycloneDX document: %v", err)
	}
	return &document
}

// allComponents is the product and every component, nested ones included.
func (d *cdxDocument) allComponents() []cdxComponent {
	var out []cdxComponent
	var walk func([]cdxComponent)
	walk = func(components []cdxComponent) {
		for _, component := range components {
			out = append(out, component)
			walk(component.Components)
		}
	}
	if d.Metadata.Component != nil {
		walk([]cdxComponent{*d.Metadata.Component})
	}
	walk(d.Components)
	return out
}

// --- the SPDX side ---

type spdxRelationship struct {
	class, relationshipType, from, scope, completeness string
	to                                                 []string
	node                                               map[string]any
}

type spdxGraph struct {
	nodes         map[string]map[string]any
	document      map[string]any
	creationInfo  map[string]any
	relationships []spdxRelationship
	from          map[string][]spdxRelationship
}

func parseSpdxGraph(t *testing.T, data []byte) *spdxGraph {
	t.Helper()
	var root struct {
		Graph []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("not an SPDX JSON-LD document: %v", err)
	}
	graph := &spdxGraph{nodes: map[string]map[string]any{}, from: map[string][]spdxRelationship{}}
	for _, node := range root.Graph {
		if node["type"] == "CreationInfo" {
			graph.creationInfo = node
			continue
		}
		id, _ := node["spdxId"].(string)
		local := localIdentity(id)
		graph.nodes[local] = node
		if node["type"] == "SpdxDocument" {
			graph.document = node
		}
		if _, isRelationship := node["relationshipType"]; isRelationship {
			relationship := spdxRelationship{node: node}
			relationship.class, _ = node["type"].(string)
			relationship.relationshipType, _ = node["relationshipType"].(string)
			from, _ := node["from"].(string)
			relationship.from = localIdentity(from)
			relationship.scope, _ = node["scope"].(string)
			relationship.completeness, _ = node["completeness"].(string)
			for _, to := range anyStrings(node["to"]) {
				relationship.to = append(relationship.to, localIdentity(to))
			}
			graph.relationships = append(graph.relationships, relationship)
			graph.from[relationship.from] = append(graph.from[relationship.from], relationship)
		}
	}
	if graph.document == nil || graph.creationInfo == nil {
		t.Fatal("the document has no SpdxDocument or no CreationInfo")
	}
	return graph
}

// targets is the set of elements the relationships of one type from an
// element point to.
func (g *spdxGraph) targets(from, relationshipType string) map[string]bool {
	out := map[string]bool{}
	for _, relationship := range g.from[from] {
		if relationship.relationshipType == relationshipType {
			for _, to := range relationship.to {
				out[to] = true
			}
		}
	}
	return out
}

// hasRelationship reports a relationship to `to`; an empty from, type or
// scope matches any.
func (g *spdxGraph) hasRelationship(from, relationshipType, to, scope string) bool {
	for _, relationship := range g.relationships {
		if (from != "" && relationship.from != from) ||
			(relationshipType != "" && relationship.relationshipType != relationshipType) ||
			(scope != "" && relationship.scope != scope) {
			continue
		}
		for _, target := range relationship.to {
			if target == to {
				return true
			}
		}
	}
	return false
}

func (g *spdxGraph) knownLeaf(from string) bool {
	for _, relationship := range g.from[from] {
		if relationship.relationshipType == "dependsOn" && relationship.completeness == "complete" &&
			len(relationship.to) == 1 && relationship.to[0] == "NoneElement" {
			return true
		}
	}
	return false
}

func (g *spdxGraph) name(local string) string {
	name, _ := g.nodes[local]["name"].(string)
	return name
}

// licenceStated reports a licence relationship of the given type (any
// licence type when empty) from one of the holders to a target that states
// the CycloneDX licence text, or, when licence is empty, to any target.
func (g *spdxGraph) licenceStated(holders []string, relationshipType, licence string) bool {
	for _, holder := range holders {
		for _, relationship := range g.from[holder] {
			if relationshipType != "" && relationship.relationshipType != relationshipType {
				continue
			}
			if relationship.relationshipType != "hasConcludedLicense" && relationship.relationshipType != "hasDeclaredLicense" {
				continue
			}
			for _, target := range relationship.to {
				if licence == "" || g.licenceMatches(target, licence) {
					return true
				}
			}
		}
	}
	return false
}

// licenceMatches is how one CycloneDX licence statement is found again: as
// the same expression, as the predefined individual for NOASSERTION or NONE,
// or -- for a name that is no SPDX identifier -- as the LicenseRef sbomb
// minted for it, whose definition carries the name (section 28.11.5).
//
// The whole SPDX expression has to say what the CycloneDX statement says. An
// expression that merely contains a reference minted for the CycloneDX text --
// "LicenseRef-sbomb-X AND <anything>" -- is a different licence, and is not a
// match.
func (g *spdxGraph) licenceMatches(target, licence string) bool {
	switch licence {
	case "NOASSERTION":
		return target == "expandedlicensing_NoAssertionLicense"
	case "NONE":
		return target == "expandedlicensing_NoneLicense"
	}
	node := g.nodes[target]
	if node == nil {
		return false
	}
	expression, _ := node["simplelicensing_licenseExpression"].(string)
	if expression == licence {
		return true
	}
	entries, _ := node["simplelicensing_customIdToUri"].([]any)
	minted := map[string]string{}
	for _, entry := range entries {
		key, _ := entry.(map[string]any)["key"].(string)
		value, _ := entry.(map[string]any)["value"].(string)
		minted[key] = g.name(localIdentity(value))
	}
	// A name, or a text that is no expression, is one reference minted for
	// the whole of it: the expression is that reference and nothing else.
	if name, found := minted[expression]; found && name == licence {
		return true
	}
	// An expression that names an identifier the list does not carry keeps
	// its structure in SPDX, with that operand replaced by a reference whose
	// definition is named after it, lower-case operators upper-cased and a
	// listed identifier in the list's spelling (deviation D55). It is the same
	// expression as CycloneDX's when, read back through those definitions, it
	// says the same thing -- which a document that kept only the first
	// identifier, or added an operand, does not.
	return slices.Equal(expressionTokens(expression, minted), expressionTokens(licence, nil))
}

// expressionTokens splits a licence expression into its tokens, with the
// operators upper-cased, every listed identifier in the list's spelling (a
// reader matches identifiers without case, SPDX 3.0.1 annex B), and every
// reference in minted replaced by the operand it was minted for.
func expressionTokens(expression string, minted map[string]string) []string {
	expression = strings.NewReplacer("(", " ( ", ")", " ) ").Replace(expression)
	tokens := strings.Fields(expression)
	afterWith := false
	for index, token := range tokens {
		switch upper := strings.ToUpper(token); upper {
		case "AND", "OR", "WITH":
			tokens[index] = upper
			afterWith = upper == "WITH"
			continue
		}
		lookup := licenselist.Canonical
		if afterWith {
			lookup = licenselist.CanonicalException
		}
		afterWith = false
		if operand, found := minted[token]; found {
			tokens[index] = operand
		} else if canonical, listed := lookup(token); listed {
			tokens[index] = canonical
		}
	}
	return tokens
}

// filesCarryingText is every file among `files` that carries the licence
// text as a "license" external reference with an embedded data: URL of its bytes --
// with the UTF-8 character set stated where the bytes are UTF-8 -- which is
// where SPDX 3.0.1 can attach a complete licence text to the file it was read
// from (section 28.11.3).
func (g *spdxGraph) filesCarryingText(files []string, content, encoding string) []string {
	decoded := content
	if encoding == "base64" {
		raw, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil
		}
		decoded = string(raw)
	}
	payload := base64.StdEncoding.EncodeToString([]byte(decoded))
	var carriers []string
	for _, file := range files {
		carries := false
		references, _ := g.nodes[file]["externalRef"].([]any)
		for _, reference := range references {
			object := reference.(map[string]any)
			if object["externalRefType"] != "license" {
				continue
			}
			for _, locator := range anyStrings(object["locator"]) {
				carries = carries || locator == "data:text/plain;base64,"+payload || locator == "data:text/plain;charset=utf-8;base64,"+payload
			}
		}
		if carries {
			carriers = append(carriers, file)
		}
	}
	return carriers
}

func (g *spdxGraph) copyrightLines(files []string) map[string]bool {
	out := map[string]bool{}
	for _, file := range files {
		text, _ := g.nodes[file]["software_copyrightText"].(string)
		for _, line := range strings.Split(text, "\n") {
			out[line] = true
		}
	}
	return out
}

// hasVex reports a not-affected statement from the CVE's vulnerability to
// the component, with the upstream's reason as its impact statement when the
// upstream gave one.
func (g *spdxGraph) hasVex(cve, component, reason string) bool {
	for _, relationship := range g.from["vulnerability:"+cve] {
		if relationship.class != "security_VexNotAffectedVulnAssessmentRelationship" ||
			relationship.relationshipType != "doesNotAffect" {
			continue
		}
		statement, _ := relationship.node["security_impactStatement"].(string)
		if reason != "" && statement != reason {
			continue
		}
		for _, to := range relationship.to {
			if to == component {
				return true
			}
		}
	}
	return false
}

// extensionOf is the element's sbomb properties as a set of "name=value".
func extensionOf(node map[string]any) map[string]bool {
	out := map[string]bool{}
	extensions, _ := node["extension"].([]any)
	for _, extension := range extensions {
		entries, _ := extension.(map[string]any)["extension_cdxProperty"].([]any)
		for _, entry := range entries {
			fields := entry.(map[string]any)
			name, _ := fields["extension_cdxPropName"].(string)
			value, _ := fields["extension_cdxPropValue"].(string)
			out[name+"="+value] = true
		}
	}
	return out
}

// propertyValues are the values of one property name, sorted.
func propertyValues(properties map[string]bool, name string) []string {
	var values []string
	for entry := range properties {
		if value, found := strings.CutPrefix(entry, name+"="); found {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	return values
}

// confidenceLevel is the reverse of domain.Confidence.Float: the level a
// CycloneDX confidence number was written from.
func confidenceLevel(number float64) (domain.Confidence, bool) {
	for _, level := range []domain.Confidence{domain.ConfidenceHigh, domain.ConfidenceMedium, domain.ConfidenceLow, domain.ConfidenceUnknown} {
		if level.Float() == number {
			return level, true
		}
	}
	return "", false
}

func hasExternalIdentifier(node map[string]any, kind, identifier string) bool {
	identifiers, _ := node["externalIdentifier"].([]any)
	for _, entry := range identifiers {
		fields := entry.(map[string]any)
		if fields["externalIdentifierType"] == kind && fields["identifier"] == identifier {
			return true
		}
	}
	return false
}

func hasExternalRef(node map[string]any, kind, locator string) bool {
	references, _ := node["externalRef"].([]any)
	for _, entry := range references {
		fields := entry.(map[string]any)
		if fields["externalRefType"] != kind {
			continue
		}
		for _, value := range anyStrings(fields["locator"]) {
			if value == locator {
				return true
			}
		}
	}
	return false
}

func hasHash(node map[string]any, algorithm, value string) bool {
	hashes, _ := node["verifiedUsing"].([]any)
	for _, entry := range hashes {
		fields := entry.(map[string]any)
		if fields["algorithm"] == algorithm && fields["hashValue"] == value {
			return true
		}
	}
	return false
}

// spdxAlgorithm is the SPDX spelling of a CycloneDX hash algorithm.
func spdxAlgorithm(alg string) string {
	switch alg {
	case "SHA-1":
		return "sha1"
	case "SHA-256":
		return "sha256"
	case "SHA-384":
		return "sha384"
	case "SHA-512":
		return "sha512"
	case "MD5":
		return "md5"
	case "SHA3-256":
		return "sha3_256"
	case "SHA3-384":
		return "sha3_384"
	case "SHA3-512":
		return "sha3_512"
	}
	return "other"
}

// notesArePatchLabels reports whether CycloneDX pedigree notes are exactly
// the signal followed by labels of the given patch files, in the form the
// CycloneDX writer builds them. Labels are removed longest first, so that one
// that is the prefix of another ("description A" and "description A2") does
// not leave the rest of the longer one behind.
func notesArePatchLabels(notes, signal string, graph *spdxGraph, patches map[string]bool) bool {
	rest, found := strings.CutPrefix(notes, strings.TrimSpace(signal+" ("))
	if !found || !strings.HasSuffix(rest, ")") {
		return false
	}
	rest = strings.TrimSuffix(rest, ")")
	var labels []string
	for file := range patches {
		node := graph.nodes[file]
		name, _ := node["name"].(string)
		description, _ := node["description"].(string)
		name, description = strings.Join(strings.Fields(name), " "), strings.Join(strings.Fields(description), " ")
		// A patch the metadata names no file for has a stated name in SPDX,
		// which requires one of every File, and none in the notes.
		if strings.HasSuffix(name, "(the metadata names no patch file)") {
			name = ""
		}
		switch {
		case name != "" && description != "":
			labels = append(labels, name+": "+description)
		case name != "":
			labels = append(labels, name)
		case description != "":
			labels = append(labels, description)
		}
	}
	sort.Slice(labels, func(i, j int) bool { return len(labels[i]) > len(labels[j]) })
	for _, label := range labels {
		rest = strings.ReplaceAll(rest, label, "")
	}
	return strings.Trim(rest, ", ") == ""
}

func relationshipOrAny(relationshipType string) string {
	if relationshipType == "" {
		return "licence"
	}
	return relationshipType
}

func anyStrings(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		out := make([]string, 0, len(typed))
		for _, entry := range typed {
			if text, ok := entry.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
