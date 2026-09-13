package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/foss"
	"github.com/example/sbomb/internal/testutil"
)

// The FOSS outputs of section 32.6, over the p14-foss fixture: the only
// project in the corpus whose sources are committed, because a licence text
// cannot be read out of build evidence.

// runFOSS produces the four documents from one generate run and returns them
// by name.
//
// The lenient profile is the corpus convention for p14-foss and predates this
// milestone: every golden of the fixture is captured under it, so a FOSS
// golden under a second profile would describe a build no other golden
// describes. It is not the whole coverage -- the default profile is what a
// user gets, and it reports the toolchain runtime as a component, which is a
// materially different notices document -- so runFOSSUnderTheDefaultProfile
// below exercises that one.
func runFOSS(t *testing.T, extra ...string) (map[string]string, string) {
	t.Helper()
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	directory := t.TempDir()
	outDir := filepath.Join(directory, "foss")
	sbomPath := filepath.Join(directory, "app.cdx.json")
	args := append([]string{"generate",
		"--build-dir", buildDir,
		"--policy", "lenient",
		"--source-dir", testutil.CorpusSourceTree(t),
		"--output", sbomPath,
		"--foss-out", outDir,
		"--reproducible"}, extra...)
	code, _, stderr := execute(args)
	if code != 0 || stderr != "" {
		t.Fatalf("generate --foss-out = code %d, stderr %q", code, stderr)
	}
	return readFOSS(t, outDir), sbomPath
}

// runFOSSUnderTheDefaultProfile is the same documents with no --policy at
// all: the profile a user who passes no flags gets, whose
// includeToolchainRuntime is separate-component (section 33.1) and which
// therefore carries two components the lenient profile does not -- the
// toolchain runtime gnu-13.3.0 and section 24.2's synthetic build-environment
// grouping.
//
// It goes through `sbomb foss` rather than through `generate --foss-out`, for
// a harness reason that is worth stating. The corpus build directory is copied
// per test, and the copy does not preserve modification times: a used file
// that lands after the artifact makes the artifact look stale, which the
// default profile's failOnStaleBuildArtifacts gate turns into exit 3. That is
// a property of the copy and not of the build, and it is why every golden of
// this fixture is captured under the lenient profile. `foss` evaluates no
// policy gate at all (section 32.6), so it renders the default profile's
// documents without the flake -- which is the same point from the other side:
// no exit code of these outputs depends on licence content.
func runFOSSUnderTheDefaultProfile(t *testing.T, extra ...string) map[string]string {
	t.Helper()
	outDir := filepath.Join(t.TempDir(), "foss")
	args := append([]string{"foss",
		"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
		"--source-dir", testutil.CorpusSourceTree(t),
		"--out", outDir,
		"--reproducible"}, extra...)
	code, _, stderr := execute(args)
	if code != 0 || stderr != "" {
		t.Fatalf("foss under the default profile = code %d, stderr %q", code, stderr)
	}
	return readFOSS(t, outDir)
}

func readFOSS(t *testing.T, directory string) map[string]string {
	t.Helper()
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

// TestFOSSOutputsGolden is the regression document for all four files. The
// notices format is house style, so nothing but a golden can keep it from
// drifting unnoticed.
func TestFOSSOutputsGolden(t *testing.T) {
	files, _ := runFOSS(t)
	for _, name := range foss.Files() {
		assertGolden(t, filepath.Join("foss", "gcc-ninja-p14-foss", name), []byte(files[name]))
	}
}

// TestBothEntryPointsProduceTheSameBytes replaces the consistency test an
// earlier draft had between the two commands. They are one code path, so the
// assertion is byte equality and not that their component sets happen to
// match.
func TestBothEntryPointsProduceTheSameBytes(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	sourceDir := testutil.CorpusSourceTree(t)
	directory := t.TempDir()
	viaGenerate := filepath.Join(directory, "a")
	viaFOSS := filepath.Join(directory, "b")

	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--source-dir", sourceDir, "--output", filepath.Join(directory, "app.cdx.json"),
		"--foss-out", viaGenerate, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate --foss-out = code %d, stderr %q", code, stderr)
	}
	code, _, stderr = execute([]string{"foss", "--build-dir", buildDir, "--policy", "lenient",
		"--source-dir", sourceDir, "--out", viaFOSS, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("foss --out = code %d, stderr %q", code, stderr)
	}
	first, second := readFOSS(t, viaGenerate), readFOSS(t, viaFOSS)
	for _, name := range foss.Files() {
		if first[name] != second[name] {
			t.Errorf("%s differs between generate --foss-out and foss --out", name)
		}
	}
}

// TestFOSSOutputDoesNotChangeTheDocument is decision B1, machine-checked:
// --foss-out is an output selector and never a content switch. Otherwise an
// SBOM would depend on which side outputs somebody asked for.
func TestFOSSOutputDoesNotChangeTheDocument(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	sourceDir := testutil.CorpusSourceTree(t)
	directory := t.TempDir()
	with := filepath.Join(directory, "with.cdx.json")
	without := filepath.Join(directory, "without.cdx.json")

	base := []string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--source-dir", sourceDir, "--reproducible"}
	code, _, stderr := execute(append(append([]string{}, base...), "--output", with,
		"--foss-out", filepath.Join(directory, "foss")))
	if code != 0 || stderr != "" {
		t.Fatalf("with --foss-out = code %d, stderr %q", code, stderr)
	}
	code, _, stderr = execute(append(append([]string{}, base...), "--output", without))
	if code != 0 || stderr != "" {
		t.Fatalf("without --foss-out = code %d, stderr %q", code, stderr)
	}
	left, err := os.ReadFile(with)
	if err != nil {
		t.Fatal(err)
	}
	right, err := os.ReadFile(without)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatal("--foss-out changed the document")
	}
}

// TestFOSSOutputsAreDeterministic is section 29 for the new outputs: two runs
// over independent copies of the same evidence and of the same source tree
// must produce the same bytes, or the documents cannot be reviewed or diffed.
func TestFOSSOutputsAreDeterministic(t *testing.T) {
	first, _ := runFOSS(t)
	directory := t.TempDir()
	outDir := filepath.Join(directory, "second")
	code, _, stderr := execute([]string{"foss",
		"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
		"--policy", "lenient",
		"--source-dir", testutil.CorpusSourceTreeCopy(t),
		"--out", outDir, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("second run = code %d, stderr %q", code, stderr)
	}
	second := readFOSS(t, outDir)
	for _, name := range foss.Files() {
		if first[name] != second[name] {
			t.Errorf("%s changed between two runs over the same evidence", name)
		}
	}
}

// TestMitLibCarriesItsOwnText is requirement R2: the MIT text names the rights
// holder inside the text, so the generic SPDX text would ship a template where
// a notice was required. The bytes must be the component's own file.
func TestMitLibCarriesItsOwnText(t *testing.T) {
	files, _ := runFOSS(t)
	notices := files[foss.NoticesFile]
	retained, err := os.ReadFile(filepath.Join(testutil.CorpusSourceTree(t), "dep", "mit-lib", "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notices, string(retained)) {
		t.Fatal("mit-lib's entry does not contain the bytes of the component's own licence file")
	}
	if !strings.Contains(notices, "Copyright (c) 2026 Fixture MIT Library Authors") {
		t.Error("the holder line from the component's own text is missing")
	}
	// Two MIT texts with different holders are two texts: the fixture's
	// dual-licensed dependency carries its own LICENSE-MIT.
	other, err := os.ReadFile(filepath.Join(testutil.CorpusSourceTree(t), "dep", "multi-license", "LICENSE-MIT"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notices, string(other)) {
		t.Error("the second MIT text was replaced by the first")
	}
}

// TestASharedTextIsPrintedOnceOnTheFixture: apache-lib's LICENSE and
// multi-license's LICENSE-APACHE are the same bytes, which is the case
// decision Q13 is about.
func TestASharedTextIsPrintedOnceOnTheFixture(t *testing.T) {
	files, _ := runFOSS(t)
	notices := files[foss.NoticesFile]
	apache, err := os.ReadFile(filepath.Join(testutil.CorpusSourceTree(t), "dep", "apache-lib", "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(notices, string(apache)); count != 1 {
		t.Errorf("the shared Apache text appears %d time(s); want one", count)
	}
	if !strings.Contains(notices, "[also the licence text of: multi-license]") {
		t.Error("the components sharing the text are not listed under it")
	}
}

// TestGPLGeneratorIsOnlyInTheBuildTimeOnlySection is requirement R1: the code
// generator ran during the build and was linked into nothing, so naming it in
// a notices document would invite a source request nobody owes.
func TestGPLGeneratorIsOnlyInTheBuildTimeOnlySection(t *testing.T) {
	files, _ := runFOSS(t)
	if strings.Contains(files[foss.NoticesFile], "gpl-gen") {
		t.Error("gpl-gen is in THIRD-PARTY-NOTICES.txt")
	}
	if strings.Contains(files[foss.ObligationsFile], "gpl-gen") {
		t.Error("gpl-gen is in source-obligations.txt")
	}
	review := files[foss.ReviewTextFile]
	section := review[strings.Index(review, "BUILD-TIME-ONLY COMPONENTS"):]
	if !strings.Contains(section, "gpl-gen") {
		t.Errorf("gpl-gen is not in the build-time-only section:\n%s", review)
	}
}

// TestLgplLibNamesStaticLinkage is the one condition the obligation list has:
// static linkage against an LGPL library implicates the relinking provision.
func TestLgplLibNamesStaticLinkage(t *testing.T) {
	files, _ := runFOSS(t)
	obligations := files[foss.ObligationsFile]
	if !strings.Contains(obligations, "lgpl-lib - LGPL-2.1-only - static-archive-member") {
		t.Fatalf("lgpl-lib's entry does not name its linkage:\n%s", obligations)
	}
	if !strings.Contains(obligations, "relinking provision") {
		t.Error("the relinking provision is not named")
	}
	if !strings.Contains(obligations, "sbomb does not and cannot produce this material.") {
		t.Error("the document does not say that the material is out of scope")
	}
}

// TestTheProjectIsNotInTheNoticesOnTheFixture is decision Q12 on real build
// evidence: the manufacturer's own application is type application and stays
// out, and eight libraries copied into the source tree -- all of them
// scope=project -- stay in.
func TestTheProjectIsNotInTheNoticesOnTheFixture(t *testing.T) {
	files, _ := runFOSS(t)
	notices := files[foss.NoticesFile]
	if strings.Contains(notices, "\nproject\n") {
		t.Error("the project's own component is in the notices document")
	}
	for _, name := range []string{"apache-lib", "bsd-hdr", "lgpl-lib", "mit-lib", "multi-license", "nocopyright"} {
		if !strings.Contains(notices, name) {
			t.Errorf("%s, a library copied into the source tree, is not in the notices document", name)
		}
	}
}

// TestTheDefaultProfileKeepsSbombsOwnGroupingOutOfTheNotices is the
// membership rule of section 32.6 under the profile a user gets when they pass
// no flags, which is a materially different document from the lenient one the
// goldens are captured under: includeToolchainRuntime=separate-component adds
// the toolchain runtime as a component, and with it section 24.2's synthetic
// build-environment grouping.
//
// The toolchain runtime belongs in the notices document -- it is inside the
// artifact, so its role is distributed and it owes attribution like any other
// component. The synthetic grouping does not: it has no files of its own, no
// chain from an artifact reaches it, section 24.5 derives no role for it, and
// presenting sbomb's own bookkeeping node to a customer as a third-party
// component under an unknown licence would be a defect in the one document
// that ships.
func TestTheDefaultProfileKeepsSbombsOwnGroupingOutOfTheNotices(t *testing.T) {
	files := runFOSSUnderTheDefaultProfile(t)
	notices := files[foss.NoticesFile]
	if !strings.Contains(notices, "gnu-13.3.0") {
		t.Error("the toolchain runtime is inside the artifact and is not in the notices document")
	}
	for _, name := range []string{foss.NoticesFile, foss.ObligationsFile} {
		if strings.Contains(files[name], "build-environment") {
			t.Errorf("%s presents sbomb's own synthetic grouping as a component", name)
		}
	}
	// And it is named rather than dropped: a component in none of the three
	// lists would otherwise leave the FOSS outputs without a word.
	review := files[foss.ReviewTextFile]
	section := review[strings.Index(review, "COMPONENTS WITH NO DISTRIBUTION ROLE"):]
	if !strings.Contains(section[:strings.Index(section, "LICENCE VIEW")], "- build-environment") {
		t.Errorf("build-environment is not named in the review record:\n%s", review)
	}
	for _, line := range []string{"components                     8", "no distribution role           1"} {
		if !strings.Contains(review, line) {
			t.Errorf("the completeness block does not state %q:\n%s", line, review)
		}
	}
}

// TestTheIncompletenessMarkersReachTheNoticesDocumentOnTheFixture is
// requirement R10 end to end: where the text or the statement could not be
// established, the document says so at the point the entry would have been,
// and no canonical SPDX text is substituted for either.
//
// Two components carry one marker each, and which marker is the point. The
// toolchain runtime has no retained licence text -- no source tree of GCC was
// read -- but it does have copyright statements, because the narrowed headers
// of section 32.6 are its own headers and they carry the FSF's notices. The
// fixture's `nocopyright` dependency is the other way round: a licence file
// and no notice anywhere in it. A marker that stood in both places would say
// nothing about which of the two things was missing.
func TestTheIncompletenessMarkersReachTheNoticesDocumentOnTheFixture(t *testing.T) {
	files := runFOSSUnderTheDefaultProfile(t)
	notices := files[foss.NoticesFile]
	entryOf := func(name string) string {
		start := strings.Index(notices, name)
		if start < 0 {
			t.Fatalf("the notices document has no entry for %s", name)
		}
		entry := notices[start:]
		// The last entry has no separator after it.
		if end := strings.Index(entry, "================"); end >= 0 {
			entry = entry[:end]
		}
		return entry
	}

	toolchain := entryOf("gnu-13.3.0")
	if !strings.Contains(toolchain, "[licence text not found in component - attribution incomplete]") {
		t.Errorf("the missing-text marker is not in the toolchain entry:\n%s", toolchain)
	}
	if !strings.Contains(toolchain, "Licence: not established (NOASSERTION)") {
		t.Errorf("a licence was asserted for a component that resolved to none:\n%s", toolchain)
	}
	// The statements the licence view reads out of the narrowed headers. They
	// reach the shipped document, which is what makes the missing-copyright
	// marker absent here rather than unreported.
	if !strings.Contains(toolchain, "Free Software Foundation") {
		t.Errorf("the toolchain entry carries no copyright statement, and its own headers state one:\n%s", toolchain)
	}
	if strings.Contains(toolchain, "[no copyright statement found in component") {
		t.Errorf("the missing-copyright marker stands although statements were found:\n%s", toolchain)
	}

	bare := entryOf("nocopyright")
	if !strings.Contains(bare, "[no copyright statement found in component - attribution incomplete]") {
		t.Errorf("the missing-copyright marker is not in the entry that exists to have none:\n%s", bare)
	}
}

// TestAWaiverDoesNotChangeTheNoticesDocument is decision Q11 over real build
// evidence, and the half of the milestone's waiver test the corpus can carry:
// dep/nolicense is no mapped component of p14-foss, but nocopyright is, it
// reports FOSS_COPYRIGHT_MISSING, and the question the decision answers is the
// same one -- does a waiver change the shippable document.
//
// It does not. The waiver annotates: the finding keeps its reason in the
// review record and stops failing the build, and THIRD-PARTY-NOTICES.txt is
// byte-identical with and without it, incompleteness marker included. That
// document reports what sbomb saw, not what somebody decided about it.
func TestAWaiverDoesNotChangeTheNoticesDocument(t *testing.T) {
	waivers := filepath.Join(t.TempDir(), "waivers.json")
	if err := os.WriteFile(waivers, []byte(`{
	  "waivers": [
	    {
	      "id": "FOSS_COPYRIGHT_MISSING",
	      "subject": "component:nocopyright",
	      "reason": "upstream carries no notice, confirmed by review",
	      "approvedBy": "a.steinbart",
	      "expires": "2099-01-31"
	    }
	  ]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plain, _ := runFOSS(t)
	waived, _ := runFOSS(t, "--waivers", waivers)
	if plain[foss.NoticesFile] != waived[foss.NoticesFile] {
		t.Error("a waiver changed THIRD-PARTY-NOTICES.txt")
	}
	if !strings.Contains(waived[foss.NoticesFile],
		"[no copyright statement found in component - attribution incomplete]") {
		t.Error("the incompleteness marker was removed by a waiver")
	}
	review := waived[foss.ReviewTextFile]
	if !strings.Contains(review, "FOSS_COPYRIGHT_MISSING") ||
		!strings.Contains(review, "(waived: upstream carries no notice, confirmed by review)") {
		t.Errorf("the waived finding is not in the review record with its reason:\n%s", review)
	}
}

// TestTheNoticesDocumentHasNoPathInAFixtureRun is decision Q8 on the fixture:
// no canonical identity, no anchor, no host directory. The origin URL and the
// URLs inside a licence text are not filesystem paths.
func TestTheNoticesDocumentHasNoPathInAFixtureRun(t *testing.T) {
	files, _ := runFOSS(t)
	notices := files[foss.NoticesFile]
	for _, forbidden := range []string{
		"__fixture_", testutil.CorpusSourceTree(t), "project:dep/", "build:", "dep/mit-lib",
	} {
		if strings.Contains(notices, forbidden) {
			t.Errorf("THIRD-PARTY-NOTICES.txt contains %q", forbidden)
		}
	}
	// And the same for source-obligations.txt, which carries a name, a
	// licence and a linkage form and nothing else.
	for _, forbidden := range []string{"__fixture_", "project:dep/", testutil.CorpusSourceTree(t)} {
		if strings.Contains(files[foss.ObligationsFile], forbidden) {
			t.Errorf("source-obligations.txt contains %q", forbidden)
		}
	}
}

// TestTheNarrowingDeltaIsNonZeroAndCounted pins the licence view against a
// hand-counted number: the fixture's twenty-five toolchain headers are in the
// dependency files and in no emitted compilation unit, so the SBOM view drops
// them and the licence view counts them.
func TestTheNarrowingDeltaIsNonZeroAndCounted(t *testing.T) {
	files, _ := runFOSS(t)
	review := files[foss.ReviewTextFile]
	if !strings.Contains(review, "headers narrowed away:       25") {
		t.Errorf("the narrowing delta is not 25:\n%s", review)
	}
	if !strings.Contains(review, "licence view vs. SBOM view     +25 files") {
		t.Error("the completeness block does not state the delta")
	}
	if !strings.Contains(review, "- gnu-13.3.0: 25 header(s)") {
		t.Error("the delta is not attributed to a component")
	}
}

// TestFOSSOutputIsNotASourceOffer is the structural guarantee of section 32.6
// over a real run: nothing that could be mistaken for corresponding source is
// in the output directory.
func TestFOSSOutputIsNotASourceOffer(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	directory := t.TempDir()
	outDir := filepath.Join(directory, "foss")
	code, _, stderr := execute([]string{"foss", "--build-dir", buildDir, "--policy", "lenient",
		"--source-dir", testutil.CorpusSourceTree(t), "--out", outDir, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("foss = code %d, stderr %q", code, stderr)
	}
	forbidden := []string{".c", ".h", ".cpp", ".hpp", ".s", ".zip", ".patch"}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(foss.Files()) {
		t.Fatalf("the output directory holds %d file(s); want the four documents", len(entries))
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if strings.Contains(name, ".tar") {
			t.Errorf("%s is an archive", entry.Name())
		}
		for _, suffix := range forbidden {
			if strings.HasSuffix(name, suffix) {
				t.Errorf("%s looks like source or a patch", entry.Name())
			}
		}
	}
}

// TestFOSSExitCodes is section 32.6: usage errors first, a build directory
// that cannot be read as a discovery error, and no exit code that depends on
// licence content.
func TestFOSSExitCodes(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no build dir", []string{"foss", "--out", "/tmp/does-not-matter"}, 1},
		{"no out", []string{"foss", "--build-dir", buildDir}, 1},
		{"unknown flag", []string{"foss", "--build-dir", buildDir, "--out", "/tmp/x", "--nope"}, 1},
		{"invalid format", []string{"foss", "--build-dir", buildDir, "--out", "/tmp/x", "--format", "html"}, 1},
		{"missing build dir", []string{"foss", "--build-dir", filepath.Join(t.TempDir(), "gone"), "--out", "/tmp/x"}, 2},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			code, _, stderr := execute(testCase.args)
			if code != testCase.want {
				t.Fatalf("exit code = %d, want %d (stderr %q)", code, testCase.want, stderr)
			}
			if stderr == "" {
				t.Error("a refusal must say why")
			}
		})
	}
}

// TestFOSSRunsCleanOnAnIncompleteFixture is section 32.6's promise that
// nothing here fails a build: the fixture has a component with no copyright
// statement and one with no licence text, and the command still exits 0.
func TestFOSSRunsCleanOnAnIncompleteFixture(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	outDir := filepath.Join(t.TempDir(), "foss")
	code, _, stderr := execute([]string{"foss", "--build-dir", buildDir, "--policy", "strict",
		"--source-dir", testutil.CorpusSourceTree(t), "--out", outDir, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("foss under the strict profile = code %d, stderr %q; no exit code may depend on licence content",
			code, stderr)
	}
}

// TestOutOverwritesTheFourFilesOnly is decision Q14 through the CLI.
func TestOutOverwritesTheFourFilesOnly(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	outDir := t.TempDir()
	stale := filepath.Join(outDir, foss.NoticesFile)
	unrelated := filepath.Join(outDir, "reviewer-notes.md")
	if err := os.WriteFile(stale, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execute([]string{"foss", "--build-dir", buildDir, "--policy", "lenient",
		"--source-dir", testutil.CorpusSourceTree(t), "--out", outDir, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("a non-empty output directory must not be refused: code %d, stderr %q", code, stderr)
	}
	notices, err := os.ReadFile(stale)
	if err != nil || strings.Contains(string(notices), "stale") {
		t.Error("the stale notices file survived the run")
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep\n" {
		t.Errorf("an unrelated file was touched: %q, %v", data, err)
	}
}

// TestFOSSMarkdownFormat: --format changes how the documents read and not
// which four names they have.
func TestFOSSMarkdownFormat(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	outDir := filepath.Join(t.TempDir(), "foss")
	code, _, stderr := execute([]string{"foss", "--build-dir", buildDir, "--policy", "lenient",
		"--source-dir", testutil.CorpusSourceTree(t), "--out", outDir,
		"--format", "markdown", "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("foss --format markdown = code %d, stderr %q", code, stderr)
	}
	files := readFOSS(t, outDir)
	if !strings.HasPrefix(files[foss.NoticesFile], "# THIRD-PARTY NOTICES") {
		t.Error("the markdown rendering did not reach the notices document")
	}
}

// TestFOSSRefusesAnInvalidFormatBeforeDiscovering: a typo in the rendering
// flag must be refused before a full discovery runs, not after it, and
// nothing may be created on the strength of the refused argument.
//
// The rendering flag exists on `foss` alone. `generate --foss-out` writes the
// text rendering: the milestone's flag surface is --foss-out on generate and
// --format on foss, and an equivalent of --format on generate is surface
// nobody asked for (open-questions.md).
func TestFOSSRefusesAnInvalidFormatBeforeDiscovering(t *testing.T) {
	directory := t.TempDir()
	outDir := filepath.Join(directory, "foss")
	code, _, stderr := execute([]string{"foss",
		"--build-dir", filepath.Join(directory, "build"),
		"--out", outDir, "--format", "html"})
	if code != 1 || !strings.Contains(stderr, "--format") {
		t.Fatalf("exit code = %d, stderr %q; want a usage error naming the flag", code, stderr)
	}
	if _, err := os.Stat(outDir); err == nil {
		t.Error("the output directory was created although the run was refused")
	}
	// And `generate` has no rendering flag of its own to typo.
	code, _, stderr = execute([]string{"generate", "--build-dir", directory,
		"--output", filepath.Join(directory, "out.cdx.json"),
		"--foss-out", filepath.Join(directory, "generated"), "--foss-format", "markdown"})
	if code != 1 || !strings.Contains(stderr, "--foss-format") {
		t.Fatalf("generate --foss-format = code %d, stderr %q; want an unknown-flag error", code, stderr)
	}
}

// Section 32.6: the two entry points are one discovery, so they have to
// compare paths the same way. `sbomb foss` rejected --path-flavor outright, so
// a Windows runner with `path-flavor: posix` pinned produced an SBOM under one
// comparison and notices under another -- from one build, describing component
// sets that can differ.
func TestFOSSTakesThePathFlavor(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	out := t.TempDir()
	code, _, stderr := execute([]string{"foss", "--build-dir", buildDir,
		"--source-dir", testutil.CorpusSourceTree(t), "--path-flavor", "posix", "--out", out})
	if code != 0 || stderr != "" {
		t.Fatalf("foss --path-flavor posix = code %d, stderr %q", code, stderr)
	}
	// And it is checked rather than passed through.
	code, _, stderr = execute([]string{"foss", "--build-dir", buildDir, "--path-flavor", "nonsense", "--out", out})
	if code == 0 || !strings.Contains(stderr, "invalid value for --path-flavor") {
		t.Errorf("an invalid flavor = code %d, stderr %q", code, stderr)
	}
}
