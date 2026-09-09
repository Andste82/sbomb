package pkgmanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// writeCPMLock writes the file CPM.cmake leaves in the build directory. The
// header is the one CPM writes when it is included, before any package has
// been added, so every fixture here starts the way a real one does.
func writeCPMLock(t *testing.T, buildDir, blocks string) {
	t.Helper()
	content := "# CPM Package Lock\n# This file should be committed to version control\n\n" + blocks
	if err := os.WriteFile(filepath.Join(buildDir, cpmLockName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeCheckout creates the directory FetchContent puts a dependency's sources
// in, and nothing else. Without a populate script the lock is the only thing
// that can say anything about the package, which is what most of these tests
// want to see.
func writeCheckout(t *testing.T, buildDir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(buildDir, "_deps", name+"-src"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// cpmBlock is one entry as cpm_add_to_package_lock writes it: a comment naming
// the package, the call, one key per line and the closing parenthesis alone.
func cpmBlock(name string, keys ...string) string {
	block := "# " + name + "\nCPMDeclarePackage(" + name + "\n"
	for _, key := range keys {
		block += "  " + key + "\n"
	}
	return block + ")\n"
}

// The lock states a version and a repository for a package the FetchContent
// layout already located, and both reach the package.
func TestTheCPMLockNamesTheVersionAndTheRepository(t *testing.T) {
	build := t.TempDir()
	writeCheckout(t, build, "fmt")
	writeCPMLock(t, build, cpmBlock("fmt", "NAME fmt", "VERSION 9.1.0", "GITHUB_REPOSITORY fmtlib/fmt"))

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Name != "fmt" || found.Version.Value != "9.1.0" {
		t.Errorf("name/version = %q/%q", found.Name, found.Version.Value)
	}
	if found.Version.Source != cpmVersionSource || found.Version.Confidence != domain.ConfidenceHigh {
		t.Errorf("version source = %q, confidence = %q", found.Version.Source, found.Version.Confidence)
	}
	if found.Version.Rank != RankInstallState {
		t.Errorf("version rank = %d, want the lock to count as install state", found.Version.Rank)
	}
	if found.VCSURL != "https://github.com/fmtlib/fmt" {
		t.Errorf("vcs url = %q, want the expanded shorthand without the .git suffix", found.VCSURL)
	}
	want := "pkg:generic/fmt@9.1.0?vcs_url=git%2Bhttps%3A%2F%2Fgithub.com%2Ffmtlib%2Ffmt"
	if found.PURL.Value != want {
		t.Errorf("purl = %q, want %q", found.PURL.Value, want)
	}
	// The lock is the record of the manager that fetched the dependency, so it
	// is the one detectedBy names.
	if found.Manager != cpmManager {
		t.Errorf("manager = %q, want the manager the lock belongs to", found.Manager)
	}
	// The anchor is about the path identity, and _deps/fmt-src is the directory
	// FetchContent made.
	if found.AnchorKey != "pkg:fetchcontent/fmt" {
		t.Errorf("anchor key = %q, want the one naming the directory layout", found.AnchorKey)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v", findings)
	}
}

// The whole point of reading the lock inside the FetchContent adapter: CPM and
// FetchContent describe one package, not two, so nothing is duplicated and
// nobody is turned away.
func TestTheCPMLockAddsNoSecondPackageAndNoConflict(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.4.0")
	if err := os.MkdirAll(filepath.Join(build, "_deps", "tinylog-build"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCPMLock(t, build, cpmBlock("tinylog", "NAME tinylog", "VERSION 1.4.0",
		"GIT_REPOSITORY https://example.invalid/org/tinylog.git"))

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v, want the one dependency both of them describe", packages)
	}
	if len(packages[0].Roots) != 2 {
		t.Errorf("roots = %q, want the checkout and the build tree", packages[0].Roots)
	}
	for _, finding := range findings {
		if finding.ID == "COMPONENT_MAPPING_CONFLICT" {
			t.Errorf("finding = %#v: CPM calls FetchContent, so the two do not dispute the root", finding)
		}
	}
}

// CPM lower-cases the name for the _deps directories while the lock keeps the
// name as it was written, so the join between the two has to lower-case as
// well. Without this the common case -- GTest, Catch2, Boost -- would silently
// find nothing.
func TestTheLockIsFoundForAPackageWhoseNameIsNotLowerCase(t *testing.T) {
	build := t.TempDir()
	writeCheckout(t, build, "gtest")
	writeCPMLock(t, build, cpmBlock("GTest", "NAME GTest", "VERSION 1.14.0",
		"GITHUB_REPOSITORY google/googletest"))

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if packages[0].Version.Value != "1.14.0" || packages[0].Version.Source != cpmVersionSource {
		t.Errorf("version = %q from %q, want the locked one", packages[0].Version.Value, packages[0].Version.Source)
	}
}

// Each of the repository keys CPM accepts expands to the URL CPM itself would
// have built out of it, and every one of them is normalized by section 19.4.
func TestTheRepositoryKeysExpandTheWayCPMExpandsThem(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{name: "github", key: "GITHUB_REPOSITORY org/repo", want: "https://github.com/org/repo"},
		{name: "gitlab", key: "GITLAB_REPOSITORY org/repo", want: "https://gitlab.com/org/repo"},
		{name: "bitbucket", key: "BITBUCKET_REPOSITORY org/repo", want: "https://bitbucket.org/org/repo"},
		{name: "explicit", key: "GIT_REPOSITORY https://example.invalid/org/repo.git", want: "https://example.invalid/org/repo"},
		{name: "scp form", key: "GIT_REPOSITORY git@example.invalid:org/repo.git", want: "https://example.invalid/org/repo"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			build := t.TempDir()
			writeCheckout(t, build, "repo")
			writeCPMLock(t, build, cpmBlock("repo", "NAME repo", "VERSION 1.0", testCase.key))

			packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
			if len(packages) != 1 {
				t.Fatalf("packages = %#v", packages)
			}
			if packages[0].VCSURL != testCase.want {
				t.Errorf("vcs url = %q, want %q", packages[0].VCSURL, testCase.want)
			}
		})
	}
}

// Both origins are install state and rank equally, so the order of the two Take
// calls decides. It is the lock that has to win: it names a version, while the
// script names whichever revision was checked out.
func TestTheLockWinsOverThePopulateScriptTag(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.3.0")
	writeCPMLock(t, build, cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0"))

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Version.Value != "1.2.0" || found.Version.Source != cpmVersionSource {
		t.Errorf("version = %q from %q, want the locked one", found.Version.Value, found.Version.Source)
	}
	// The script's answer is not thrown away; a disagreement between two
	// origins is worth reporting and cannot be reported once it is gone.
	if !supersededHas(found, FieldVersion, "fetchcontent", "1.3.0") {
		t.Errorf("superseded = %#v, want the populate script's tag kept", found.Superseded)
	}
	// The purl restates the claim that won.
	if !strings.Contains(found.PURL.Value, "@1.2.0") {
		t.Errorf("purl = %q, want the version that won in it", found.PURL.Value)
	}
}

// The case that makes the order matter in practice: a package pinned to a
// commit has a forty-character hash where the tag would be, and publishing that
// as the version is what the lock prevents.
func TestACommitPinnedPackageStillPublishesTheLockedVersion(t *testing.T) {
	build := t.TempDir()
	commit := strings.Repeat("a", 40)
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", commit)
	writeCPMLock(t, build, cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0"))

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if packages[0].Version.Value != "1.2.0" {
		t.Errorf("version = %q, want the locked version rather than the pinned hash", packages[0].Version.Value)
	}
}

// The lock is a declaration and the checkout is the thing itself, so
// introspection still outranks it. Wiring a second declared origin in must not
// hide the strongest one.
func TestTheCheckoutOutranksTheLock(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.3.0")
	writeCPMLock(t, build, cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0"))
	runner := standInGit(t, "v1.5.0", "")
	runner.Anchors = []string{build}

	packages, _ := Discover(Options{BuildDir: build, Runner: runner, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Version.Value != "1.5.0" || found.Version.Source != "git-describe" {
		t.Errorf("version = %q from %q, want what the checkout answered", found.Version.Value, found.Version.Source)
	}
	if !supersededHas(found, FieldVersion, cpmVersionSource, "1.2.0") {
		t.Errorf("superseded = %#v, want the locked version kept", found.Superseded)
	}
}

// supersededHas reports whether a claim that lost is still on record with the
// field, origin and value it had.
func supersededHas(found Package, field Field, source, value string) bool {
	for _, contribution := range found.Superseded {
		if contribution.Field == field && contribution.Claim.Source == source &&
			contribution.Claim.Value == value {
			return true
		}
	}
	return false
}

// A project that does not use CPM has nothing missing about it, and the header
// CPM writes before any package is added is the shape of a project that uses
// CPM without a package lock. Neither may make a sound.
func TestAnAbsentOrEmptyLockChangesNothing(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		present bool
	}{{name: "absent"}, {name: "header only", present: true}} {
		t.Run(testCase.name, func(t *testing.T) {
			build := t.TempDir()
			writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.4.0")
			if testCase.present {
				writeCPMLock(t, build, "")
			}

			packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
			if len(packages) != 1 || packages[0].Version.Value != "1.4.0" {
				t.Fatalf("packages = %#v, want the FetchContent evidence unchanged", packages)
			}
			if packages[0].Manager != "fetchcontent" {
				t.Errorf("manager = %q, want the adapter that found it", packages[0].Manager)
			}
			if len(findings) != 0 {
				t.Errorf("findings = %#v, want none", findings)
			}
		})
	}
}

// A package CPM could not version is written into the lock as a fully
// commented-out block. A comment is not a claim, and it is not a broken file
// either, so it contributes nothing and reports nothing.
func TestACommentedOutBlockContributesNothing(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.4.0")
	writeCPMLock(t, build, "# tinylog (unversioned)\n# CPMDeclarePackage(tinylog\n"+
		"#  NAME tinylog\n#  GIT_TAG main\n#)\n")

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if packages[0].Version.Value != "1.4.0" || packages[0].Version.Source != "fetchcontent" {
		t.Errorf("version = %q from %q, want the populate script's",
			packages[0].Version.Value, packages[0].Version.Source)
	}
	if packages[0].Manager != "fetchcontent" {
		t.Errorf("manager = %q, want the adapter that found it: the lock claimed nothing", packages[0].Manager)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v, want none: a comment is not a damaged file", findings)
	}
}

// A block that never closes means the reader no longer knows what it is
// reading, so the file is refused whole -- reported once, with the FetchContent
// evidence carrying on untouched.
func TestABrokenLockIsRefusedWholeAndReportedOnce(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.4.0")
	writeCPMLock(t, build, cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0")+
		"garbage line\nCPMDeclarePackage(other\n  NAME other\n")

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if packages[0].Version.Value != "1.4.0" || packages[0].Version.Source != "fetchcontent" {
		t.Errorf("version = %q from %q, want nothing taken out of the broken file",
			packages[0].Version.Value, packages[0].Version.Source)
	}
	assertOneEvidenceFinding(t, findings, "EVIDENCE_UNREADABLE", filepath.Join(build, cpmLockName))
}

// The byte bound of section 30 refuses the file whole rather than in part: half
// a lock would version some packages out of it and leave the rest looking as
// though CPM had said nothing about them.
func TestALockOverTheByteBoundIsRefusedWhole(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.4.0")
	padding := "# " + strings.Repeat("x", maxCPMLockBytes) + "\n"
	writeCPMLock(t, build, cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0")+padding)

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || packages[0].Version.Value != "1.4.0" {
		t.Fatalf("packages = %#v, want no version out of the refused file", packages)
	}
	assertOneEvidenceFinding(t, findings, "INPUT_LIMIT_EXCEEDED", filepath.Join(build, cpmLockName))
}

// A file that is small in bytes can still name an absurd number of packages,
// which is why the entry count is bounded on its own -- and refused whole for
// the same reason as the byte bound.
func TestALockOverTheEntryBoundIsRefusedWhole(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.4.0")
	blocks := strings.Builder{}
	blocks.WriteString(cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0"))
	for i := 0; i <= maxCPMLockEntries; i++ {
		blocks.WriteString(cpmBlock(fmt.Sprintf("p%d", i), "VERSION 1.0"))
	}
	writeCPMLock(t, build, blocks.String())

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || packages[0].Version.Value != "1.4.0" {
		t.Fatalf("packages = %#v, want no version out of the refused file", packages)
	}
	assertOneEvidenceFinding(t, findings, "INPUT_LIMIT_EXCEEDED", filepath.Join(build, cpmLockName))
}

// A line longer than the scanner bound of section 30 stops the read, and a file
// that was not read to its end supplies nothing. It reaches the reader only
// through the byte bound in practice -- a line over limits.MaxLine makes the
// file larger than maxCPMLockBytes -- so the parser is asked directly here.
func TestALineOverTheScannerBoundStopsTheRead(t *testing.T) {
	lock := cpmBlock("tinylog", "NAME tinylog", "VERSION "+strings.Repeat("9", limits.MaxLine+1))
	if _, err := parseCPMLock(strings.NewReader(lock)); err == nil {
		t.Fatal("a line over the scanner bound was read as though it were whole")
	}
}

// Two blocks naming one package differently are not two statements but none.
// Keeping the first would make the published version depend on the order CPM
// happened to configure in.
func TestTwoBlocksThatDisagreeContributeNothing(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.4.0")
	writeCPMLock(t, build, cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0")+
		cpmBlock("tinylog", "NAME tinylog", "VERSION 1.3.0"))

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if packages[0].Version.Value != "1.4.0" || packages[0].Version.Source != "fetchcontent" {
		t.Errorf("version = %q from %q, want the populate script's",
			packages[0].Version.Value, packages[0].Version.Source)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v, want none: the file was read, it simply says two things", findings)
	}
}

// Two identical blocks say one thing twice, which is no contradiction at all.
func TestTwoIdenticalBlocksStillContribute(t *testing.T) {
	build := t.TempDir()
	writeCheckout(t, build, "tinylog")
	block := cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0")
	writeCPMLock(t, build, block+block)

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || packages[0].Version.Value != "1.2.0" {
		t.Fatalf("packages = %#v, want the version both blocks state", packages)
	}
}

// The reader improves what is known about packages the evidence already
// reached; it never discovers one. A lock entry with no directory beneath
// _deps names nothing this tool could attribute a file to.
func TestALockEntryWithoutACheckoutCreatesNoPackage(t *testing.T) {
	build := t.TempDir()
	writeCheckout(t, build, "tinylog")
	writeCPMLock(t, build, cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0")+
		cpmBlock("absent", "NAME absent", "VERSION 2.0.0"))

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || packages[0].Name != "tinylog" {
		t.Fatalf("packages = %#v, want only the dependency that is on disk", packages)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v, want none: the lock is not a list of what must exist", findings)
	}
}

// Nothing in the lock ever becomes a path. A SOURCE_DIR pointing out of the
// build tree is not even a key this reader knows, and the roots stay where the
// directory layout put them.
func TestNothingInTheLockBecomesAPath(t *testing.T) {
	build := t.TempDir()
	writeCheckout(t, build, "tinylog")
	writeCPMLock(t, build, cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0",
		"SOURCE_DIR ../../etc/passwd", "GIT_REPOSITORY /etc/passwd"))

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	for _, root := range packages[0].Roots {
		if !strings.HasPrefix(root, filepath.Join(build, "_deps")+string(filepath.Separator)) {
			t.Errorf("root = %q, which lies outside the _deps directory", root)
		}
	}
	// The file list is the one place a reader could put a path of its own
	// choosing in front of the resolver, which would let the lock decide that a
	// file belongs to a package rather than only what that package is called.
	// The CPM reader never writes to it: it holds no paths at all.
	if len(packages[0].Files) != 0 {
		t.Errorf("files = %q, want none: the lock states no file ownership", packages[0].Files)
	}
}

// Section 19.4: a repository URL with a password in it must never reach the
// document, through the URL or through the purl that restates it.
func TestCredentialsInALockedRepositoryAreStripped(t *testing.T) {
	build := t.TempDir()
	writeCheckout(t, build, "tinylog")
	writeCPMLock(t, build, cpmBlock("tinylog", "NAME tinylog", "VERSION 1.2.0",
		"GIT_REPOSITORY https://user:secret@example.invalid/org/tinylog.git"))

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if packages[0].VCSURL != "https://example.invalid/org/tinylog" {
		t.Errorf("vcs url = %q, want the credentials gone", packages[0].VCSURL)
	}
	for _, value := range []string{packages[0].VCSURL, packages[0].PURL.Value} {
		if strings.Contains(value, "secret") {
			t.Errorf("%q carries the password out of the lock file", value)
		}
	}
	for _, finding := range findings {
		if strings.Contains(finding.Message, "secret") {
			t.Errorf("finding = %#v carries the password out of the lock file", finding)
		}
	}
}

// The lock is looked up and never iterated, so the order its blocks are written
// in cannot reach the output. Two runs over two orderings must agree.
func TestTheOrderOfTheBlocksDoesNotReachTheOutput(t *testing.T) {
	first := cpmBlock("alpha", "NAME alpha", "VERSION 1.0")
	second := cpmBlock("beta", "NAME beta", "VERSION 2.0")

	describe := func(blocks string) string {
		build := t.TempDir()
		writeCheckout(t, build, "alpha")
		writeCheckout(t, build, "beta")
		writeCPMLock(t, build, blocks)
		packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
		rendered := make([]string, 0, len(packages))
		for _, found := range packages {
			rendered = append(rendered, fmt.Sprintf("%s %s %s %s",
				found.Name, found.Version.Value, found.Version.Source, found.PURL.Value))
		}
		return strings.Join(rendered, "\n")
	}
	if forwards, backwards := describe(first+second), describe(second+first); forwards != backwards {
		t.Errorf("the two orderings gave\n%s\nand\n%s", forwards, backwards)
	}
}

// assertOneEvidenceFinding states that a refused lock is reported exactly once,
// against the file itself rather than against a component.
func assertOneEvidenceFinding(t *testing.T, findings []domain.Finding, id, path string) {
	t.Helper()
	matching := make([]domain.Finding, 0, 1)
	for _, finding := range findings {
		if finding.ID == id {
			matching = append(matching, finding)
		}
	}
	if len(matching) != 1 {
		t.Fatalf("findings = %#v, want exactly one %s", findings, id)
	}
	if matching[0].Subject.Kind != "evidence" || matching[0].Subject.Ref != path {
		t.Errorf("subject = %#v, want the lock file itself", matching[0].Subject)
	}
	if matching[0].Severity != domain.SeverityWarning {
		t.Errorf("severity = %q, want warning", matching[0].Severity)
	}
}
