package pkgmanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// mesonProject lays out a source tree with the given wrap files, and creates
// the subproject directories named. A wrap without its directory is what Meson
// leaves behind for a dependency it never had to fetch, so the two are given
// separately on purpose.
func mesonProject(t *testing.T, wraps map[string]string, directories ...string) string {
	t.Helper()
	source := t.TempDir()
	for name, content := range wraps {
		writeTestFile(t, filepath.Join(source, mesonSubprojectsDir, name), content)
	}
	for _, directory := range directories {
		if err := os.MkdirAll(filepath.Join(source, mesonSubprojectsDir, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return source
}

func mesonPackages(t *testing.T, source string) ([]Package, []domain.Finding) {
	t.Helper()
	return meson{}.Discover(Options{SourceDir: source, Context: context.Background()})
}

const gitWrap = `[wrap-git]
url = https://example.invalid/org/zlib.git
revision = v1.3.1
depth = 1
`

func TestAGitWrapNamesTheSubprojectItsRevisionAndItsRepository(t *testing.T) {
	source := mesonProject(t, map[string]string{"zlib.wrap": gitWrap}, "zlib")

	packages, findings := mesonPackages(t, source)

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	if len(packages) != 1 {
		t.Fatalf("packages = %#v, want the one the wrap declares", packages)
	}
	found := packages[0]
	if found.Name != "zlib" {
		t.Errorf("name = %q, want the wrap's own file name, which is how Meson addresses it", found.Name)
	}
	if want := filepath.Join(source, mesonSubprojectsDir, "zlib"); found.Root() != want {
		t.Errorf("root = %q, want %q", found.Root(), want)
	}
	if found.Version.Value != "1.3.1" || found.Version.Source != mesonSource {
		t.Errorf("version = %#v, want the revision with the tag convention's v trimmed", found.Version)
	}
	if found.Version.Rank != RankDeclaredManifest {
		t.Errorf("version ranks %d, want rank 2: a wrap declares what was asked for", found.Version.Rank)
	}
	if found.VCSURL != "https://example.invalid/org/zlib" {
		t.Errorf("vcs url = %q, want the normalized repository", found.VCSURL)
	}
	if !strings.HasPrefix(found.PURL.Value, "pkg:generic/zlib@1.3.1?vcs_url=") {
		t.Errorf("purl = %q, want the git-derived form of section 20.4", found.PURL.Value)
	}
	// A wrap records no file list, and inventing one would widen the used set.
	if len(found.Files) != 0 {
		t.Errorf("the package claims %d file(s); a wrap records none", len(found.Files))
	}
}

// A wrap that pins a commit states no version, exactly as a west manifest that
// does: the SHA travels in the purl and UNKNOWN_VERSION says the rest.
func TestAWrapPinnedToACommitStatesNoVersion(t *testing.T) {
	source := mesonProject(t, map[string]string{"zlib.wrap": "[wrap-git]\n" +
		"url = https://example.invalid/org/zlib.git\n" +
		"revision = 51b7f2abdade71cd9bb0e7a373ef2610ec6f9daf\n"}, "zlib")

	packages, findings := mesonPackages(t, source)

	if len(packages) != 1 {
		t.Fatalf("packages = %#v, want one", packages)
	}
	if packages[0].Version.Value != "" {
		t.Errorf("version = %q, want none: a commit is not a version (section 20.2)", packages[0].Version.Value)
	}
	if packages[0].Commit != "51b7f2abdade71cd9bb0e7a373ef2610ec6f9daf" {
		t.Errorf("commit = %q, want the revision the wrap pins", packages[0].Commit)
	}
	if len(findingsWithID(findings, "UNKNOWN_VERSION")) != 1 {
		t.Fatalf("findings = %#v, want one UNKNOWN_VERSION", findings)
	}
}

// A file wrap names a tarball. Its URL is not a repository and its file name is
// not a version, so the subproject is named and nothing more is claimed about
// it.
func TestAFileWrapClaimsNoVersionAndNoRepository(t *testing.T) {
	source := mesonProject(t, map[string]string{"libpng.wrap": `[wrap-file]
directory = libpng-1.6.40
source_url = https://example.invalid/libpng-1.6.40.tar.gz
source_filename = libpng-1.6.40.tar.gz
source_hash = 8f720b363aa08683c9bf2a563236f45313af2c55d542b5f5a4f3ac6bdda4b3a3
`}, "libpng-1.6.40")

	packages, findings := mesonPackages(t, source)

	if len(packages) != 1 {
		t.Fatalf("packages = %#v, want the one the wrap declares", packages)
	}
	found := packages[0]
	if found.Name != "libpng" {
		t.Errorf("name = %q, want the wrap's file name", found.Name)
	}
	if want := filepath.Join(source, mesonSubprojectsDir, "libpng-1.6.40"); found.Root() != want {
		t.Errorf("root = %q, want the directory the wrap names, %q", found.Root(), want)
	}
	if found.Version.Value != "" {
		t.Errorf("version = %q, want none: reading one out of a file name would be a guess", found.Version.Value)
	}
	if found.VCSURL != "" {
		t.Errorf("vcs url = %q, want none: a source_url is a tarball, not a repository", found.VCSURL)
	}
	if found.PURL.Value != "" {
		t.Errorf("purl = %q, want none for an archive nothing can identify", found.PURL.Value)
	}
	if len(findingsWithID(findings, "UNKNOWN_VERSION")) != 1 {
		t.Fatalf("findings = %#v, want one UNKNOWN_VERSION", findings)
	}
}

// Meson unpacks a subproject while it configures, so a wrap with no directory
// describes a dependency this build never obtained. Reporting one per such
// wrap would report the size of the subprojects directory instead of the state
// of the build.
func TestAWrapWithoutItsDirectoryIsSilence(t *testing.T) {
	source := mesonProject(t, map[string]string{"zlib.wrap": gitWrap})

	packages, findings := mesonPackages(t, source)

	if len(packages) != 0 || len(findings) != 0 {
		t.Fatalf("packages = %#v, findings = %#v, want silence", packages, findings)
	}
}

// Section 30.3: a wrap is somebody else's file, and a directory it states must
// not be able to point a component root out of the source tree.
func TestAWrapPointingOutOfTheSubprojectsDirectoryIsRefused(t *testing.T) {
	for _, directory := range []string{"../../etc", "/etc", "..", "."} {
		source := mesonProject(t, map[string]string{
			"evil.wrap": "[wrap-file]\ndirectory = " + directory + "\n",
		})

		packages, findings := mesonPackages(t, source)

		if len(packages) != 0 {
			t.Errorf("directory %q produced %#v, want no package", directory, packages)
		}
		if len(findingsWithID(findings, "EVIDENCE_UNREADABLE")) != 1 {
			t.Errorf("directory %q: findings = %#v, want one EVIDENCE_UNREADABLE", directory, findings)
		}
	}
}

// Two wraps naming one directory are two packages claiming one root. Discover
// keeps the first and reports the second rather than merging them, because the
// two disagree about who owns the subproject.
func TestTwoWrapsOnOneDirectoryAreReportedAsAConflict(t *testing.T) {
	source := mesonProject(t, map[string]string{
		"aaa.wrap": "[wrap-file]\ndirectory = shared\n",
		"zzz.wrap": "[wrap-file]\ndirectory = shared\n",
	}, "shared")

	packages, findings := Discover(Options{SourceDir: source, Context: context.Background()})

	if len(packages) != 1 || packages[0].Name != "aaa" {
		t.Fatalf("packages = %#v, want only the first wrap in glob order", packages)
	}
	if len(findingsWithID(findings, "COMPONENT_MAPPING_CONFLICT")) != 1 {
		t.Fatalf("findings = %#v, want one COMPONENT_MAPPING_CONFLICT", findings)
	}
}

// A wrap comes out of a tree this tool did not build, so its bytes are bounded
// like every other input of section 30.
func TestAWrapOverItsByteLimitIsReportedAndNotRead(t *testing.T) {
	source := mesonProject(t, map[string]string{
		"zlib.wrap": gitWrap + strings.Repeat("# padding\n", maxMesonWrapBytes/10+1),
	}, "zlib")

	packages, findings := mesonPackages(t, source)

	if len(packages) != 0 {
		t.Fatalf("packages = %#v, want none", packages)
	}
	limit := findingsWithID(findings, "INPUT_LIMIT_EXCEEDED")
	if len(limit) != 1 || limit[0].Subject.Kind != "evidence" {
		t.Fatalf("findings = %#v, want one INPUT_LIMIT_EXCEEDED against the file", findings)
	}
}

// A file with the extension that declares no wrap section is not a wrap, and
// saying so keeps the subproject traceable to the file.
func TestAFileWithNoWrapSectionIsReportedAndNotUsed(t *testing.T) {
	source := mesonProject(t, map[string]string{
		"zlib.wrap": "[provide]\nzlib = zlib_dep\n",
	}, "zlib")

	packages, findings := mesonPackages(t, source)

	if len(packages) != 0 {
		t.Fatalf("packages = %#v, want none", packages)
	}
	if len(findingsWithID(findings, "EVIDENCE_UNREADABLE")) != 1 {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
}

// The [provide] section maps dependency names onto Meson variables. It is not
// part of the wrap section and must not leak into the keys read from it.
func TestTheProvideSectionIsNotReadAsWrapKeys(t *testing.T) {
	source := mesonProject(t, map[string]string{
		"zlib.wrap": gitWrap + "\n[provide]\ndirectory = elsewhere\nrevision = 9.9.9\n",
	}, "zlib")

	packages, findings := mesonPackages(t, source)

	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	if len(packages) != 1 {
		t.Fatalf("packages = %#v, want one", packages)
	}
	if want := filepath.Join(source, mesonSubprojectsDir, "zlib"); packages[0].Root() != want {
		t.Errorf("root = %q, want %q: [provide] does not name a directory", packages[0].Root(), want)
	}
	if packages[0].Version.Value != "1.3.1" {
		t.Errorf("version = %q, want the wrap section's revision", packages[0].Version.Value)
	}
}

// A project with no subprojects directory has nothing missing about it, so the
// adapter opens nothing at all.
func TestAProjectWithoutSubprojectsIsNotTouched(t *testing.T) {
	packages, findings := mesonPackages(t, t.TempDir())

	if len(packages) != 0 || len(findings) != 0 {
		t.Fatalf("packages = %#v, findings = %#v, want silence", packages, findings)
	}
}

// A Meson subproject is very often a submodule as well, and the wrap is the
// stronger statement -- so the registry asks meson first and the submodule
// claim is rejected rather than merged.
func TestAWrapOutranksAGitmodulesEntryNamingTheSameDirectory(t *testing.T) {
	source := mesonProject(t, map[string]string{"zlib.wrap": gitWrap}, "zlib")
	writeTestFile(t, filepath.Join(source, ".gitmodules"),
		"[submodule \"subprojects/zlib\"]\n\tpath = subprojects/zlib\n\turl = git@example.invalid:org/zlib.git\n")

	packages, findings := Discover(Options{SourceDir: source, Context: context.Background()})

	if len(packages) != 1 {
		t.Fatalf("packages = %#v, want one: the two adapters claim one root", packages)
	}
	if packages[0].Manager != "meson" {
		t.Errorf("manager = %q, want meson, which states a revision where .gitmodules states none", packages[0].Manager)
	}
	if len(findingsWithID(findings, "COMPONENT_MAPPING_CONFLICT")) != 1 {
		t.Fatalf("findings = %#v, want one COMPONENT_MAPPING_CONFLICT naming both managers", findings)
	}
}

// A wrap is addressed by its file name, so the name has to be able to stand as
// one path segment and as a component name. A file called `...wrap` names the
// parent directory, and one called `.wrap` names nothing at all; both are
// refused rather than resolved, as espidf.go and west.go refuse a key out of
// somebody else's file for the same reason.
func TestAWrapWhoseNameCannotStandAsASegmentIsRefused(t *testing.T) {
	for _, file := range []string{"...wrap", ".wrap"} {
		source := mesonProject(t, map[string]string{file: gitWrap}, "zlib")

		packages, findings := mesonPackages(t, source)

		if len(packages) != 0 {
			t.Errorf("%q produced %#v, want no package", file, packages)
		}
		if len(findingsWithID(findings, "EVIDENCE_UNREADABLE")) != 1 {
			t.Errorf("%q: findings = %#v, want one EVIDENCE_UNREADABLE", file, findings)
		}
	}
}

// The number of wraps is bounded as well as the size of each one, because a
// tree this tool did not write can hold an absurd number of small files. Over
// the ceiling the whole directory is refused: reading half of it would
// attribute some subprojects and leave the rest looking as though the project
// never declared them.
func TestMoreWrapsThanTheLimitAreRefusedWhole(t *testing.T) {
	source := t.TempDir()
	subprojects := filepath.Join(source, mesonSubprojectsDir)
	if err := os.MkdirAll(filepath.Join(subprojects, "zlib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(subprojects, "zlib.wrap"), gitWrap)
	for index := 0; index < maxMesonWraps; index++ {
		if err := os.WriteFile(filepath.Join(subprojects, fmt.Sprintf("p%05d.wrap", index)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	packages, findings := mesonPackages(t, source)

	if len(packages) != 0 {
		t.Fatalf("packages = %#v, want none: half the wraps would describe half the build", packages)
	}
	limit := findingsWithID(findings, "INPUT_LIMIT_EXCEEDED")
	if len(limit) != 1 || limit[0].Subject.Ref != subprojects {
		t.Fatalf("findings = %#v, want one INPUT_LIMIT_EXCEEDED naming the subprojects directory", findings)
	}
}
