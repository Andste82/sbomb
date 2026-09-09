package pkgmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// The tests in this file ask one question of each of the four readers: what it
// takes out of a file it understands, and what it does with everything else.
// The "everything else" half is the larger one on purpose -- all four read
// files written in languages this tool does not interpret, so the shapes they
// must refuse are what keeps them honest.

// rootWith lays the given files into a fresh directory and returns it, so a
// case can say what it is testing rather than how a temporary tree is made.
func rootWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		writeTestFile(t, filepath.Join(root, name), content)
	}
	return root
}

// versionFrom is the version claim a reader made, and whether it made one.
func versionFrom(contributions []Contribution) (Claim, bool) {
	return contributionFor(contributions, FieldVersion)
}

// packageVersionFile is what write_basic_package_version_file generates,
// shortened to the shape that matters: one literal assignment of
// PACKAGE_VERSION, and the comparison logic below it that must not be read as
// a second one.
const packageVersionFile = `# This is a basic version file for the Config-mode of find_package().
set(PACKAGE_VERSION "3.4.1")

if(PACKAGE_VERSION VERSION_LESS PACKAGE_FIND_VERSION)
  set(PACKAGE_VERSION_COMPATIBLE FALSE)
else()
  set(PACKAGE_VERSION_COMPATIBLE TRUE)
  if(PACKAGE_FIND_VERSION STREQUAL PACKAGE_VERSION)
    set(PACKAGE_VERSION_EXACT TRUE)
  endif()
endif()
`

func TestACMakePackageVersionFileStatesTheVersion(t *testing.T) {
	root := rootWith(t, map[string]string{"mbedtlsConfigVersion.cmake": packageVersionFile})

	contributions, findings := cmakeConfigVersion{}.Enrich(ComponentRoot{Path: root, Name: "mbedtls"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none: the file was readable", findings)
	}
	if len(contributions) != 1 {
		t.Fatalf("contributions = %#v, want exactly a version", contributions)
	}
	version, _ := versionFrom(contributions)
	if version.Value != "3.4.1" {
		t.Errorf("version = %q, want the literal the file assigns", version.Value)
	}
	if version.Rank != RankDeclaredManifest {
		t.Errorf("version ranks %d, want rank 2, so that any installation state outranks it", version.Rank)
	}
	if version.Confidence != domain.ConfidenceHigh {
		t.Errorf("version confidence = %q, want high (section 20.3)", version.Confidence)
	}
	// The wiring publishes the claim under the reader's Source(), so a reader
	// that filled it in itself would spell the origin in two places.
	if version.Source != "" {
		t.Errorf("version source = %q, want none: applyEnrichment fills it from Source()", version.Source)
	}
}

// The other spelling of the same file, which is the one Conan's CMakeDeps
// generator writes.
func TestTheHyphenatedSpellingOfTheVersionFileIsReadToo(t *testing.T) {
	root := rootWith(t, map[string]string{"mbedtls-config-version.cmake": packageVersionFile})

	contributions, findings := cmakeConfigVersion{}.Enrich(ComponentRoot{Path: root, Name: "mbedtls"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	if version, made := versionFrom(contributions); !made || version.Value != "3.4.1" {
		t.Errorf("version = %#v, want the one the file assigns", version)
	}
}

// The latent bug of conan.go's own pattern, which this reader must not inherit:
// `[^"\s)]+` matches `${PROJECT_VERSION` and would publish that fragment as a
// version nobody stated.
func TestAPackageVersionFileThatAssignsAVariableIsRefused(t *testing.T) {
	root := rootWith(t, map[string]string{
		"fooConfigVersion.cmake": "set(PACKAGE_VERSION ${PROJECT_VERSION})\n",
	})

	contributions, findings := cmakeConfigVersion{}.Enrich(ComponentRoot{Path: root, Name: "foo"})

	if len(contributions) != 0 {
		t.Fatalf("contributions = %#v, want none: the value is a variable reference", contributions)
	}
	if len(findingsWithID(findings, "EVIDENCE_UNREADABLE")) != 1 {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
}

func TestTwoPackageVersionFilesInOneRootAreRefusedTogether(t *testing.T) {
	root := rootWith(t, map[string]string{
		"fooConfigVersion.cmake": `set(PACKAGE_VERSION "1.0")` + "\n",
		"barConfigVersion.cmake": `set(PACKAGE_VERSION "2.0")` + "\n",
	})

	contributions, findings := cmakeConfigVersion{}.Enrich(ComponentRoot{Path: root})

	if len(contributions) != 0 {
		t.Fatalf("contributions = %#v, want none: two files describe two packages", contributions)
	}
	if len(findingsWithID(findings, "EVIDENCE_UNREADABLE")) != 1 {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE naming the root", findings)
	}
}

func TestAPackageVersionFileAssigningTwoDifferentVersionsIsRefused(t *testing.T) {
	root := rootWith(t, map[string]string{
		"fooConfigVersion.cmake": "set(PACKAGE_VERSION \"1.0\")\nset(PACKAGE_VERSION \"2.0\")\n",
	})

	contributions, findings := cmakeConfigVersion{}.Enrich(ComponentRoot{Path: root})

	if len(contributions) != 0 {
		t.Fatalf("contributions = %#v, want none", contributions)
	}
	if len(findingsWithID(findings, "EVIDENCE_UNREADABLE")) != 1 {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
}

// A commented-out assignment is not a declaration, and a file that holds only
// one states no version at all.
func TestACommentedOutPackageVersionIsNotRead(t *testing.T) {
	root := rootWith(t, map[string]string{
		"fooConfigVersion.cmake": "# set(PACKAGE_VERSION \"9.9\")\n",
	})

	contributions, findings := cmakeConfigVersion{}.Enrich(ComponentRoot{Path: root})

	if len(contributions) != 0 || len(findings) != 0 {
		t.Fatalf("contributions = %#v, findings = %#v, want silence", contributions, findings)
	}
}

// The ordinary build2 package manifest: the format line, then the fields.
const build2Package = `: 1
name: libhello
version: 1.0.2
summary: A hello library
license: MIT
`

func TestABuild2ManifestStatesTheVersionAndTheLicence(t *testing.T) {
	root := rootWith(t, map[string]string{build2ManifestName: build2Package})

	contributions, findings := build2Manifest{}.Enrich(ComponentRoot{Path: root, Name: "libhello"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	if len(contributions) != 2 {
		t.Fatalf("contributions = %#v, want exactly a version and a licence", contributions)
	}
	version, _ := versionFrom(contributions)
	if version.Value != "1.0.2" || version.Rank != RankDeclaredManifest {
		t.Errorf("version = %#v, want 1.0.2 at rank 2", version)
	}
	license, _ := contributionFor(contributions, FieldLicense)
	if license.Value != "MIT" || license.Rank != RankDeclaredManifest {
		t.Errorf("licence = %#v, want MIT at rank 2", license)
	}
	// The name and the summary are read past, not published: Contribution has
	// no field for either, and D33 keeps the name that was already settled.
	for _, contribution := range contributions {
		if contribution.Claim.Value == "libhello" || strings.Contains(contribution.Claim.Value, "hello library") {
			t.Errorf("a name or a summary reached the document: %#v", contribution)
		}
	}
}

// The whole reason the format line is checked: `manifest` is a file name half
// the world uses, and claiming every one of them would attribute a stranger's
// data to a component. A Yocto image manifest is exactly such a file.
func TestAFileCalledManifestThatIsNotBuild2IsPassedOverInSilence(t *testing.T) {
	root := rootWith(t, map[string]string{
		build2ManifestName: "PACKAGE NAME: busybox\nPACKAGE VERSION: 1.36.1\nLICENSE: GPL-2.0-only\n",
	})

	contributions, findings := build2Manifest{}.Enrich(ComponentRoot{Path: root})

	if len(contributions) != 0 || len(findings) != 0 {
		t.Fatalf("contributions = %#v, findings = %#v, want silence for a file that is no build2 manifest",
			contributions, findings)
	}
}

// build2's multi-line value syntax is not read. That is a real limit rather
// than an oversight (D40): a value taken out of a file whose remainder this
// reader cannot follow would be published with nothing behind it.
func TestABuild2ManifestThisReaderCannotFollowIsRefusedWhole(t *testing.T) {
	root := rootWith(t, map[string]string{
		build2ManifestName: ": 1\nname: libhello\nversion: 1.0.2\ndescription:\n\\\nlong text\n\\\n",
	})

	contributions, findings := build2Manifest{}.Enrich(ComponentRoot{Path: root})

	if len(contributions) != 0 {
		t.Fatalf("contributions = %#v, want none: half a manifest is an invented one", contributions)
	}
	if len(findingsWithID(findings, "EVIDENCE_UNREADABLE")) != 1 {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
}

// A licence that is not an SPDX expression is dropped in silence: the file was
// read, it simply states nothing licenses[].expression has room for.
func TestABuild2LicenceThatIsNotAnSPDXExpressionIsNotPublished(t *testing.T) {
	for _, license := range []string{"other: see the COPYING file", "All rights reserved", "MIT AND (Apache-2.0"} {
		root := rootWith(t, map[string]string{
			build2ManifestName: ": 1\nname: libhello\nversion: 1.0.2\nlicense: " + license + "\n",
		})

		contributions, findings := build2Manifest{}.Enrich(ComponentRoot{Path: root})

		if len(findings) != 0 {
			t.Errorf("license %q: findings = %#v, want none", license, findings)
		}
		if _, made := contributionFor(contributions, FieldLicense); made {
			t.Errorf("license %q was published as an SPDX expression", license)
		}
		if version, made := versionFrom(contributions); !made || version.Value != "1.0.2" {
			t.Errorf("license %q: version = %#v, want the manifest's own", license, version)
		}
	}
}

// A compound expression is one, and must survive.
func TestABuild2CompoundLicenceIsPublished(t *testing.T) {
	root := rootWith(t, map[string]string{
		build2ManifestName: ": 1\nname: libhello\nversion: 1\nlicense: Apache-2.0 WITH LLVM-exception OR MIT\n",
	})

	contributions, _ := build2Manifest{}.Enrich(ComponentRoot{Path: root})

	license, made := contributionFor(contributions, FieldLicense)
	if !made || license.Value != "Apache-2.0 WITH LLVM-exception OR MIT" {
		t.Errorf("licence = %#v, want the compound expression the manifest states", license)
	}
}

func TestAnXmakeDescriptionStatesTheVersion(t *testing.T) {
	root := rootWith(t, map[string]string{xmakeManifestName: `
set_project("hello")
set_version("2.1.0")

target("hello")
    set_kind("static")
    add_files("src/*.c")
`})

	contributions, findings := xmakeManifest{}.Enrich(ComponentRoot{Path: root, Name: "hello"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	if len(contributions) != 1 {
		t.Fatalf("contributions = %#v, want exactly a version", contributions)
	}
	version, _ := versionFrom(contributions)
	if version.Value != "2.1.0" || version.Rank != RankDeclaredManifest {
		t.Errorf("version = %#v, want 2.1.0 at rank 2", version)
	}
	// The project name is read past: it has no home in Contribution, and the
	// component keeps the name settled before its files were grouped.
	for _, contribution := range contributions {
		if contribution.Claim.Value == "hello" {
			t.Errorf("the project name reached the document: %#v", contribution)
		}
	}
}

// set_version takes options after the version, and they must not confuse the
// reader into taking the whole argument list.
func TestTheXmakeVersionIsTheFirstArgumentAndNotTheOptions(t *testing.T) {
	root := rootWith(t, map[string]string{
		xmakeManifestName: `set_version("1.4.2", {build = "%Y%m%d%H%M"})` + "\n",
	})

	contributions, _ := xmakeManifest{}.Enrich(ComponentRoot{Path: root})

	if version, made := versionFrom(contributions); !made || version.Value != "1.4.2" {
		t.Errorf("version = %#v, want the first argument alone", version)
	}
}

func TestAnXmakeDescriptionWithTwoVersionsIsRefused(t *testing.T) {
	root := rootWith(t, map[string]string{
		xmakeManifestName: "if is_plat(\"windows\") then\n    set_version(\"1.0\")\nelse\n    set_version(\"2.0\")\nend\n",
	})

	contributions, findings := xmakeManifest{}.Enrich(ComponentRoot{Path: root})

	if len(contributions) != 0 {
		t.Fatalf("contributions = %#v, want none: choosing a branch would be interpreting Lua", contributions)
	}
	if len(findingsWithID(findings, "EVIDENCE_UNREADABLE")) != 1 {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
}

func TestACommentedOutXmakeVersionIsNotRead(t *testing.T) {
	root := rootWith(t, map[string]string{
		xmakeManifestName: "-- set_version(\"9.9\")\nset_project(\"hello\")\n",
	})

	contributions, findings := xmakeManifest{}.Enrich(ComponentRoot{Path: root})

	if len(contributions) != 0 || len(findings) != 0 {
		t.Fatalf("contributions = %#v, findings = %#v, want silence", contributions, findings)
	}
}

func TestABazelModuleStatesTheVersion(t *testing.T) {
	root := rootWith(t, map[string]string{bazelModuleName: `module(
    name = "abseil-cpp",
    version = "20240116.2",
    compatibility_level = 1,
)

bazel_dep(name = "platforms", version = "0.0.8")
bazel_dep(name = "rules_cc", version = "0.0.9")
`})

	contributions, findings := bazelModule{}.Enrich(ComponentRoot{Path: root, Name: "abseil-cpp"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	if len(contributions) != 1 {
		t.Fatalf("contributions = %#v, want exactly a version", contributions)
	}
	version, _ := versionFrom(contributions)
	if version.Value != "20240116.2" {
		t.Errorf("version = %q, want the module's own and not a dependency's", version.Value)
	}
	if version.Rank != RankDeclaredManifest {
		t.Errorf("version ranks %d, want rank 2", version.Rank)
	}
}

// The bazel_dep() calls are the trap: they name versions of packages nothing
// has proven this build linked, and reading one would publish a dependency's
// version as the module's.
func TestAModuleWithoutAVersionDoesNotBorrowADependencysVersion(t *testing.T) {
	root := rootWith(t, map[string]string{
		bazelModuleName: "module(name = \"mine\")\nbazel_dep(name = \"platforms\", version = \"0.0.8\")\n",
	})

	contributions, findings := bazelModule{}.Enrich(ComponentRoot{Path: root})

	if len(contributions) != 0 || len(findings) != 0 {
		t.Fatalf("contributions = %#v, findings = %#v, want silence: the module states no version",
			contributions, findings)
	}
}

func TestAnUnclosedBazelModuleCallIsRefused(t *testing.T) {
	root := rootWith(t, map[string]string{
		bazelModuleName: "module(\n    name = \"mine\",\n    version = \"1.0\",\n",
	})

	contributions, findings := bazelModule{}.Enrich(ComponentRoot{Path: root})

	if len(contributions) != 0 {
		t.Fatalf("contributions = %#v, want none", contributions)
	}
	if len(findingsWithID(findings, "EVIDENCE_UNREADABLE")) != 1 {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
}

func TestTwoBazelModuleDeclarationsAreRefused(t *testing.T) {
	root := rootWith(t, map[string]string{
		bazelModuleName: "module(name = \"a\", version = \"1.0\")\nmodule(name = \"b\", version = \"2.0\")\n",
	})

	contributions, findings := bazelModule{}.Enrich(ComponentRoot{Path: root})

	if len(contributions) != 0 {
		t.Fatalf("contributions = %#v, want none", contributions)
	}
	if len(findingsWithID(findings, "EVIDENCE_UNREADABLE")) != 1 {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
}

// A root that carries none of the four files is the ordinary case, and silence
// is the contract: there is nothing missing about a component that has no
// manifest.
func TestARootWithoutAnyOfTheseFilesIsSilence(t *testing.T) {
	root := t.TempDir()
	for _, enricher := range []Enricher{cmakeConfigVersion{}, build2Manifest{}, xmakeManifest{}, bazelModule{}} {
		contributions, findings := enricher.Enrich(ComponentRoot{Path: root, Name: "dep"})
		if len(contributions) != 0 || len(findings) != 0 {
			t.Errorf("%s: contributions = %#v, findings = %#v, want silence",
				enricher.Source(), contributions, findings)
		}
	}
}

// A directory that happens to carry the name of one of these files is not one:
// a glob and a stat both answer with names, and only a readable file is
// evidence somebody meant to leave here.
func TestADirectoryNamedLikeAManifestIsNotRead(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{build2ManifestName, xmakeManifestName, bazelModuleName, "fooConfigVersion.cmake"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, enricher := range []Enricher{cmakeConfigVersion{}, build2Manifest{}, xmakeManifest{}, bazelModule{}} {
		contributions, findings := enricher.Enrich(ComponentRoot{Path: root, Name: "dep"})
		if len(contributions) != 0 || len(findings) != 0 {
			t.Errorf("%s: contributions = %#v, findings = %#v, want silence",
				enricher.Source(), contributions, findings)
		}
	}
}

// Every one of the four bounds its own file, and the bound is asked of the
// size before the file is opened.
func TestAManifestOverItsByteLimitIsReportedAndNotRead(t *testing.T) {
	for _, tc := range []struct {
		name     string
		file     string
		bound    int
		enricher Enricher
	}{
		{"CMake package-version file", "fooConfigVersion.cmake", maxCMakeConfigVersionBytes, cmakeConfigVersion{}},
		{"build2 manifest", build2ManifestName, maxBuild2ManifestBytes, build2Manifest{}},
		{"xmake description", xmakeManifestName, maxXmakeBytes, xmakeManifest{}},
		{"Bazel module file", bazelModuleName, maxBazelModuleBytes, bazelModule{}},
	} {
		root := t.TempDir()
		path := filepath.Join(root, tc.file)
		// A file just over the ceiling, whose first line would otherwise be
		// read as a perfectly good declaration.
		content := ": 1\nversion: 1.0\nset(PACKAGE_VERSION \"1.0\")\nset_version(\"1.0\")\nmodule(version = \"1.0\")\n"
		content += strings.Repeat("x", tc.bound+1-len(content))
		writeTestFile(t, path, content)

		contributions, findings := tc.enricher.Enrich(ComponentRoot{Path: root, Name: "dep"})

		if len(contributions) != 0 {
			t.Errorf("%s: contributions = %#v, want none", tc.name, contributions)
		}
		limit := findingsWithID(findings, "INPUT_LIMIT_EXCEEDED")
		if len(limit) != 1 {
			t.Fatalf("%s: findings = %#v, want one INPUT_LIMIT_EXCEEDED", tc.name, findings)
		}
		if limit[0].Subject.Kind != "evidence" || limit[0].Subject.Ref != path {
			t.Errorf("%s: subject = %#v, want the file that was refused", tc.name, limit[0].Subject)
		}
	}
}

// The point of ranking all four at rank 2: what a manager recorded while
// installing is rank 3, so the manager wins and the manifest's answer is kept
// as the origin that lost. Without that, a stale checked-in declaration would
// overrule the version that is really on disk.
func TestAManagersInstallStateOutranksEveryOneOfTheseReaders(t *testing.T) {
	for _, tc := range []struct {
		source string
		files  map[string]string
	}{
		{cmakeConfigVersionSource, map[string]string{"fooConfigVersion.cmake": `set(PACKAGE_VERSION "1.0")` + "\n"}},
		{build2Source, map[string]string{build2ManifestName: ": 1\nname: foo\nversion: 1.0\n"}},
		{xmakeSource, map[string]string{xmakeManifestName: `set_version("1.0")` + "\n"}},
		{bazelSource, map[string]string{bazelModuleName: `module(name = "foo", version = "1.0")` + "\n"}},
	} {
		root := rootWith(t, tc.files)
		installed := Package{Name: "foo", Roots: []string{root}}
		installed.Take(FieldVersion, Claim{Value: "2.0", Source: "conan", Rank: RankInstallState})

		findings := applyEnrichment(&installed, ComponentRoot{Path: root, Name: "foo"})

		if len(findings) != 0 {
			t.Errorf("%s: findings = %#v, want none", tc.source, findings)
		}
		if installed.Version.Value != "2.0" || installed.Version.Source != "conan" {
			t.Errorf("%s: version = %#v, want what the manager installed", tc.source, installed.Version)
		}
		superseded, found := Claim{}, false
		for _, contribution := range installed.Superseded {
			if contribution.Field == FieldVersion && contribution.Claim.Source == tc.source {
				superseded, found = contribution.Claim, true
			}
		}
		if !found || superseded.Value != "1.0" {
			t.Errorf("%s: superseded = %#v, want the manifest's 1.0 kept for the report", tc.source, installed.Superseded)
		}
	}
}

// Two of the four in one root rank alike, so the order of the registry decides
// and the loser is kept. That is deterministic but it is behaviour, which is
// why the registry says so in a comment: re-sorting it changes what a document
// publishes.
func TestTwoEquallyRankedManifestsAreDecidedByTheRegistryOrder(t *testing.T) {
	root := rootWith(t, map[string]string{
		xmakeManifestName: `set_version("2.0")` + "\n",
		bazelModuleName:   `module(name = "foo", version = "1.0")` + "\n",
	})

	var first, second Package
	if findings := applyEnrichment(&first, ComponentRoot{Path: root, Name: "foo"}); len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	applyEnrichment(&second, ComponentRoot{Path: root, Name: "foo"})

	if first.Version.Value != "2.0" || first.Version.Source != xmakeSource {
		t.Errorf("version = %#v, want the reader that stands first in the registry", first.Version)
	}
	if second.Version != first.Version {
		t.Errorf("two runs over one root disagreed: %#v and %#v", first.Version, second.Version)
	}
	kept := false
	for _, contribution := range first.Superseded {
		if contribution.Claim.Source == bazelSource && contribution.Claim.Value == "1.0" {
			kept = true
		}
	}
	if !kept {
		t.Errorf("superseded = %#v, want the losing manifest kept for the report", first.Superseded)
	}
}

// Nothing these readers take out of a file is ever used as a path. Stated as a
// test so that a later reader which does open something it read cannot slip in
// without this failing.
func TestNothingTheseReadersReadIsUsedAsAPath(t *testing.T) {
	root := rootWith(t, map[string]string{
		"fooConfigVersion.cmake": `set(PACKAGE_VERSION "../../etc/passwd")` + "\n",
		build2ManifestName:       ": 1\nname: ../../etc\nversion: /etc/passwd\n",
		xmakeManifestName:        `set_version("../../../etc/shadow")` + "\n",
		bazelModuleName:          `module(name = "..", version = "/etc/passwd")` + "\n",
	})

	var described Package
	findings := applyEnrichment(&described, ComponentRoot{Path: root, Name: "foo"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none: these are values, not paths", findings)
	}
	// The values are published verbatim as versions, which is all they are.
	// The point is the absence of anything else: no root, no file, no anchor.
	if len(described.Roots) != 0 || len(described.Files) != 0 || described.LicenseFile != "" {
		t.Errorf("a reader produced a path: %#v", described)
	}
}

// Bytes are not the only way a file can be too large: a file that is small in
// bytes can still be a million lines, and every line costs a scan. Each format
// bounds both, and the line bound is reported like the byte bound rather than
// quietly truncating the file at the ceiling.
func TestAManifestOverItsLineLimitIsReportedAndNotRead(t *testing.T) {
	for _, tc := range []struct {
		name     string
		file     string
		lines    int
		enricher Enricher
	}{
		{"CMake package-version file", "fooConfigVersion.cmake", maxCMakeConfigVersionLines, cmakeConfigVersion{}},
		{"build2 manifest", build2ManifestName, maxBuild2ManifestLines, build2Manifest{}},
		{"xmake description", xmakeManifestName, maxXmakeLines, xmakeManifest{}},
		{"Bazel module file", bazelModuleName, maxBazelModuleLines, bazelModule{}},
	} {
		root := t.TempDir()
		path := filepath.Join(root, tc.file)
		// A file whose first lines would be read as a perfectly good
		// declaration, followed by one line more than the ceiling allows.
		declaration := ": 1\nversion: 1.0\nset(PACKAGE_VERSION \"1.0\")\nset_version(\"1.0\")\nmodule(version = \"1.0\")\n"
		writeTestFile(t, path, declaration+strings.Repeat("x\n", tc.lines+1))

		contributions, findings := tc.enricher.Enrich(ComponentRoot{Path: root, Name: "dep"})

		if len(contributions) != 0 {
			t.Errorf("%s: contributions = %#v, want none", tc.name, contributions)
		}
		limit := findingsWithID(findings, "INPUT_LIMIT_EXCEEDED")
		if len(limit) != 1 {
			t.Fatalf("%s: findings = %#v, want one INPUT_LIMIT_EXCEEDED", tc.name, findings)
		}
		if limit[0].Subject.Kind != "evidence" || limit[0].Subject.Ref != path {
			t.Errorf("%s: subject = %#v, want the file that was refused", tc.name, limit[0].Subject)
		}
	}
}

// The third bound of section 30 is the length of one line, and it belongs to
// the shared line reader rather than to any one format. Only the two formats
// whose byte ceiling stands above limits.MaxLine can reach it -- for the other
// two the file is over its own ceiling long before a single line is.
func TestAManifestWithALineOverTheParserLimitIsReported(t *testing.T) {
	for _, tc := range []struct {
		name     string
		file     string
		enricher Enricher
	}{
		{"xmake description", xmakeManifestName, xmakeManifest{}},
		{"Bazel module file", bazelModuleName, bazelModule{}},
	} {
		root := t.TempDir()
		path := filepath.Join(root, tc.file)
		writeTestFile(t, path, strings.Repeat("x", limits.MaxLine+1)+"\n")

		contributions, findings := tc.enricher.Enrich(ComponentRoot{Path: root, Name: "dep"})

		if len(contributions) != 0 {
			t.Errorf("%s: contributions = %#v, want none", tc.name, contributions)
		}
		if len(findingsWithID(findings, "INPUT_LIMIT_EXCEEDED")) != 1 {
			t.Fatalf("%s: findings = %#v, want one INPUT_LIMIT_EXCEEDED", tc.name, findings)
		}
	}
}

// Lua's block comment is the one shape in which a version nobody meant to
// state could reach the document: the line itself carries no comment marker,
// so a reader that only skips `--` lines would publish the version somebody
// commented out. It is not a declaration and must not be read as one.
func TestAVersionInsideAnXmakeBlockCommentIsNotRead(t *testing.T) {
	for _, description := range []string{
		"--[[\nset_version(\"9.9\")\n]]\n",
		"--[==[\nset_version(\"9.9\")\n]==]\n",
		"--[[ an older release\nset_version(\"9.9\")\n]]\nset_project(\"hello\")\n",
	} {
		root := rootWith(t, map[string]string{xmakeManifestName: description})

		contributions, findings := xmakeManifest{}.Enrich(ComponentRoot{Path: root})

		if len(contributions) != 0 || len(findings) != 0 {
			t.Errorf("description %q: contributions = %#v, findings = %#v, want silence",
				description, contributions, findings)
		}
	}
}

// A commented-out version does not hide the one beside it: only the block is
// passed over, and the declaration after it is still the file's own statement.
func TestTheXmakeVersionAfterABlockCommentIsStillRead(t *testing.T) {
	root := rootWith(t, map[string]string{
		xmakeManifestName: "--[[\nset_version(\"9.9\")\n]]\nset_version(\"2.0\")\n",
	})

	contributions, findings := xmakeManifest{}.Enrich(ComponentRoot{Path: root})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	if version, made := versionFrom(contributions); !made || version.Value != "2.0" {
		t.Errorf("version = %#v, want the declaration outside the comment", version)
	}
}

// The module() call is followed by counting parentheses, and a parenthesis
// inside a string argument is not one of them. Counting it would lose the
// version of an ordinary file -- to a warning rather than to a wrong value,
// which is why this is stated as a test rather than left to chance.
func TestABazelVersionSurvivesAParenthesisInsideAStringArgument(t *testing.T) {
	root := rootWith(t, map[string]string{
		bazelModuleName: "module(\n    name = \"mine\",\n    version = \"1.0 (rc1\",\n)\n",
	})

	contributions, findings := bazelModule{}.Enrich(ComponentRoot{Path: root})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none: the parenthesis is inside a string", findings)
	}
	if version, made := versionFrom(contributions); !made || version.Value != "1.0 (rc1" {
		t.Errorf("version = %#v, want the literal the call states", version)
	}
}

// A reader nobody registered reads nothing. The registry is the whole wiring
// between these four and the pipeline, so a reader added without a line there
// would pass every test above and still describe no component at all.
func TestAllFourReadersAreRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, enricher := range enrichers {
		registered[enricher.Source()] = true
	}
	for _, source := range []string{cmakeConfigVersionSource, build2Source, xmakeSource, bazelSource} {
		if !registered[source] {
			t.Errorf("%q is not in the enricher registry, so nothing would ever call it", source)
		}
	}
}

// The structural half of the rule of this package, at the entry point strategy
// 6 uses: what comes back describes a root and owns nothing. No name, no root,
// no file, no manager -- there is nowhere in the answer to put one, so no
// reader in this file can widen the used set or move a boundary however much a
// later one might want to.
func TestEnrichDescribesARootAndClaimsNoFile(t *testing.T) {
	root := rootWith(t, map[string]string{
		xmakeManifestName:  `set_version("2.4.0")` + "\n",
		build2ManifestName: ": 1\nname: libhello\nversion: 2.4.0\nlicense: MIT\n",
	})

	described, findings := Enrich(ComponentRoot{Path: root, Name: "foo"})

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	if described.Version.Value == "" {
		t.Fatal("the carrier holds no version, so nothing was described at all")
	}
	if described.Name != "" || described.Manager != "" {
		t.Errorf("the carrier names a package: %q from %q", described.Name, described.Manager)
	}
	if len(described.Roots) != 0 || len(described.Files) != 0 {
		t.Errorf("the carrier owns roots %v and files %v; it may own neither", described.Roots, described.Files)
	}
}
