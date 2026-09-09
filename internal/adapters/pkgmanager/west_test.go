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

// The manifest a Zephyr workspace declares, in the shape a real one has: a
// default remote and revision, a remote with a URL base, and projects that
// state a path, a revision and either their own URL or the remote's. Nothing in
// this repository documents that format, so this file is also the written
// statement of what this tool believes a west manifest to be. It is not a
// guess: west 1.5.0's own resolver was run over exactly this manifest in the
// development container, and it produces the same three paths, URLs and
// revisions this test asserts (deviation D39).
const westTestManifest = `manifest:
  defaults:
    remote: upstream
    revision: main
  remotes:
    - name: upstream
      url-base: https://github.com/zephyrproject-rtos
  projects:
    - name: hal_nordic
      path: modules/hal/nordic
      revision: v3.5.0
      remote: upstream
    - name: mcuboot
      path: bootloader/mcuboot
      url: https://github.com/mcu-tools/mcuboot.git
      revision: 0123456789abcdef0123456789abcdef01234567
    - name: cmsis
      path: modules/hal/cmsis
  self:
    path: zephyr
`

// writeWestFile writes one file of a workspace, creating what lies above it.
func writeWestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeWorkspace lays out what `west init` and `west update` leave behind: the
// config naming the manifest repository, the manifest in it, the directories
// the projects were cloned into, and the application beside them, which is the
// source root of the build being read. It answers the workspace root and that
// application.
func writeWorkspace(t *testing.T, manifest string, projectDirs ...string) (topdir, source string) {
	t.Helper()
	topdir = t.TempDir()
	writeWestFile(t, filepath.Join(topdir, westDir, westConfigName), "[manifest]\npath = zephyr\nfile = west.yml\n")
	writeWestFile(t, filepath.Join(topdir, "zephyr", westManifestFile), manifest)
	if len(projectDirs) == 0 {
		projectDirs = []string{"modules/hal/nordic", "bootloader/mcuboot", "modules/hal/cmsis"}
	}
	for _, dir := range projectDirs {
		if err := os.MkdirAll(filepath.Join(topdir, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	source = filepath.Join(topdir, "app")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	return topdir, source
}

// discoverWest runs the adapter alone, so that what is asserted is this
// adapter's answer and not the registry's order.
func discoverWest(source string) ([]Package, []domain.Finding) {
	return west{}.Discover(Options{SourceDir: source, Context: context.Background()})
}

func packageNamed(packages []Package, name string) (Package, bool) {
	for _, entry := range packages {
		if entry.Name == name {
			return entry, true
		}
	}
	return Package{}, false
}

// The happy path: every project the manifest states, at the directory it states,
// with the revision it asked for and the repository it named.
func TestAWestManifestNamesItsProjects(t *testing.T) {
	topdir, source := writeWorkspace(t, westTestManifest)

	packages, findings := discoverWest(source)
	if len(packages) != 3 {
		t.Fatalf("packages = %#v, want the three projects the manifest states", packages)
	}
	// The order is the manifest's, because a sequence has no key to sort by and
	// two runs must publish the same document.
	if packages[0].Name != "hal_nordic" || packages[1].Name != "mcuboot" || packages[2].Name != "cmsis" {
		t.Errorf("names = %q, want the order the manifest states them in",
			[]string{packages[0].Name, packages[1].Name, packages[2].Name})
	}

	nordic, _ := packageNamed(packages, "hal_nordic")
	if nordic.Root() != filepath.Join(topdir, "modules", "hal", "nordic") {
		t.Errorf("root = %q, want the path the manifest states, resolved against the workspace root", nordic.Root())
	}
	if nordic.Version.Value != "3.5.0" || nordic.Version.Source != "west" || nordic.Version.Rank != RankDeclaredManifest {
		t.Errorf("version = %#v, want the manifest's revision at the rank of a declared manifest", nordic.Version)
	}
	if nordic.Version.Confidence != domain.ConfidenceHigh {
		t.Errorf("confidence = %q, want the high of section 20.3 for a manager's exact declaration", nordic.Version.Confidence)
	}
	// The remote's URL base and the project's name, which is what west joins
	// where a project states no URL of its own.
	if nordic.VCSURL != "https://github.com/zephyrproject-rtos/hal_nordic" {
		t.Errorf("vcs url = %q, want the remote's base joined with the project name", nordic.VCSURL)
	}
	if nordic.PURL.Value != "pkg:generic/hal_nordic@3.5.0?vcs_url=git%2Bhttps%3A%2F%2Fgithub.com%2Fzephyrproject-rtos%2Fhal_nordic" {
		t.Errorf("purl = %q, want the git-derived purl of section 20.4", nordic.PURL.Value)
	}
	if nordic.Manager != "west" || nordic.AnchorKey != "pkg:west/hal_nordic" {
		t.Errorf("manager = %q, anchor = %q", nordic.Manager, nordic.AnchorKey)
	}

	// A project that states no revision takes the manifest's default, and one
	// that states no URL takes the default remote's.
	cmsis, _ := packageNamed(packages, "cmsis")
	if cmsis.Version.Value != "main" {
		t.Errorf("version = %q, want the revision the manifest defaults to", cmsis.Version.Value)
	}
	if cmsis.VCSURL != "https://github.com/zephyrproject-rtos/cmsis" {
		t.Errorf("vcs url = %q, want the default remote's base", cmsis.VCSURL)
	}

	// The rule of pkgmanager.go, at the level this adapter can prove it: it
	// never records a file as belonging to a package, so it can never widen the
	// used set.
	for _, entry := range packages {
		if len(entry.Files) != 0 {
			t.Errorf("%s lists %d file(s); west records no file list and this adapter must invent none", entry.Name, len(entry.Files))
		}
	}
	// mcuboot is pinned to a commit and therefore has no version; nothing else
	// is missing anything.
	for _, finding := range findings {
		if finding.ID != "UNKNOWN_VERSION" || finding.Subject.Ref != "mcuboot" {
			t.Errorf("finding = %+v, want nothing but the missing version of the commit-pinned project", finding)
		}
	}
}

// Section 20.2 point 5: a commit is a version only where the configuration asks
// for it. A manifest that pins a project to a SHA therefore publishes no
// version -- the SHA reaches the document through the purl and the vcsCommit
// property, and UNKNOWN_VERSION says the rest out loud.
func TestACommitPinnedProjectPublishesNoVersion(t *testing.T) {
	_, source := writeWorkspace(t, westTestManifest)

	packages, findings := discoverWest(source)
	mcuboot, ok := packageNamed(packages, "mcuboot")
	if !ok {
		t.Fatal("the project pinned to a commit was not discovered")
	}
	if mcuboot.Version.Value != "" {
		t.Errorf("version = %q, want none: a commit is not a version", mcuboot.Version.Value)
	}
	if mcuboot.Commit != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("commit = %q, want the revision the manifest pins", mcuboot.Commit)
	}
	// The URL keeps neither its credentials nor its .git suffix (section 19.4),
	// and the commit qualifies it inside the purl.
	want := "pkg:generic/mcuboot?vcs_url=git%2Bhttps%3A%2F%2Fgithub.com%2Fmcu-tools%2Fmcuboot%400123456789abcdef0123456789abcdef01234567"
	if mcuboot.PURL.Value != want {
		t.Errorf("purl = %q, want %q", mcuboot.PURL.Value, want)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "UNKNOWN_VERSION" && finding.Subject.Ref == "mcuboot" {
			reported = true
			if !strings.Contains(finding.Message, "commit") {
				t.Errorf("message = %q, want it to say why there is no version", finding.Message)
			}
		}
	}
	if !reported {
		t.Error("a project with no publishable version passed in silence")
	}
}

// A revision that only looks like a hash is not one. A tag made of digits --
// the date convention -- is the only version such a manifest states, and
// calling it a commit would throw it away.
func TestARevisionIsACommitOnlyWhereItReallyIsOne(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		revision string
		commit   bool
	}{
		{name: "a full SHA-1", revision: "0123456789abcdef0123456789abcdef01234567", commit: true},
		{name: "a full SHA-1 of digits alone", revision: strings.Repeat("1", 40), commit: true},
		{name: "an abbreviated SHA", revision: "5a6e18a", commit: true},
		{name: "a date tag", revision: "20240612", commit: false},
		{name: "a release tag", revision: "v3.5.0", commit: false},
		{name: "a branch", revision: "main", commit: false},
		{name: "a short hex word", revision: "beef", commit: false},
		{name: "nothing at all", revision: "", commit: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := westIsCommit(testCase.revision); got != testCase.commit {
				t.Errorf("westIsCommit(%q) = %v, want %v", testCase.revision, got, testCase.commit)
			}
		})
	}
	// The v of a tag is convention and not part of the version; a word that
	// merely starts with one keeps it, because trimming a letter off a branch
	// name would invent a version.
	if got := westVersionOf("v3.5.0"); got != "3.5.0" {
		t.Errorf("westVersionOf(\"v3.5.0\") = %q", got)
	}
	if got := westVersionOf("vendor-branch"); got != "vendor-branch" {
		t.Errorf("westVersionOf(\"vendor-branch\") = %q, want the branch name untouched", got)
	}
}

// A Zephyr module states its own name, and that is the name the build system
// and the other modules know it by. Where it does, the manifest's handle for
// the repository gives way to it.
func TestTheModuleDescriptorNamesTheComponent(t *testing.T) {
	topdir, source := writeWorkspace(t, westTestManifest)
	writeWestFile(t, filepath.Join(topdir, "modules", "hal", "nordic", westModuleDir, westModuleName),
		"name: hal_nordic_module\nbuild:\n  cmake: .\n  kconfig: Kconfig\n")

	packages, _ := discoverWest(source)
	found, ok := packageNamed(packages, "hal_nordic_module")
	if !ok {
		t.Fatalf("packages = %#v, want the name the module states about itself", packages)
	}
	if found.AnchorKey != "pkg:west/hal_nordic_module" {
		t.Errorf("anchor = %q, want the module's own name in it", found.AnchorKey)
	}
	if found.Root() != filepath.Join(topdir, "modules", "hal", "nordic") {
		t.Errorf("root = %q, want the directory the manifest states", found.Root())
	}
}

// Without a workspace nothing is read, and a west.yml lying beside the sources
// changes nothing: the manifest states paths relative to a workspace root, and
// there is none to resolve them against.
func TestWithoutAWorkspaceNothingIsReadAndNothingIsReported(t *testing.T) {
	source := t.TempDir()
	writeWestFile(t, filepath.Join(source, westManifestFile), westTestManifest)

	packages, findings := discoverWest(source)
	if len(packages) != 0 || len(findings) != 0 {
		t.Fatalf("packages = %#v, findings = %#v, want silence", packages, findings)
	}
}

// The workspace is found by walking up, because an application inside a
// workspace is a subdirectory of it. That is the one upward step this adapter
// takes, and it stats one fixed path per level.
func TestTheWorkspaceIsFoundAboveTheSourceRoot(t *testing.T) {
	topdir, _ := writeWorkspace(t, westTestManifest)
	deeper := filepath.Join(topdir, "app", "subsystem", "drivers")
	if err := os.MkdirAll(deeper, 0o755); err != nil {
		t.Fatal(err)
	}

	packages, _ := discoverWest(deeper)
	if len(packages) != 3 {
		t.Fatalf("packages = %#v, want the workspace above the source root to have been found", packages)
	}
}

// Everything the manifest or the config states about a path is somebody else's
// statement about this machine's filesystem, and section 30.3 answers it with
// refusal rather than with a component root somewhere else.
func TestAPathOutOfTheManifestNeverLeavesTheWorkspace(t *testing.T) {
	t.Run("a project climbing out of the workspace is skipped and the rest are kept", func(t *testing.T) {
		manifest := "manifest:\n  projects:\n" +
			"    - name: escape\n      path: ../../etc\n      revision: v1.0.0\n" +
			"    - name: hal_nordic\n      path: modules/hal/nordic\n      revision: v3.5.0\n"
		_, source := writeWorkspace(t, manifest)

		packages, findings := discoverWest(source)
		if len(packages) != 1 || packages[0].Name != "hal_nordic" {
			t.Fatalf("packages = %#v, want only the project inside the workspace", packages)
		}
		var reported bool
		for _, finding := range findings {
			if finding.ID == "EVIDENCE_UNREADABLE" && strings.Contains(finding.Message, "escape") {
				reported = true
			}
		}
		if !reported {
			t.Errorf("findings = %v, want the refused project named", findingIDs(findings))
		}
	})

	t.Run("a workspace config pointing out of the workspace reads nothing", func(t *testing.T) {
		topdir, source := writeWorkspace(t, westTestManifest)
		writeWestFile(t, filepath.Join(topdir, westDir, westConfigName), "[manifest]\npath = ../elsewhere\nfile = west.yml\n")

		packages, findings := discoverWest(source)
		if len(packages) != 0 {
			t.Fatalf("packages = %#v, want none", packages)
		}
		if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
			t.Fatalf("findings = %+v, want the config reported as unreadable", findings)
		}
	})

	t.Run("a project named as a path is skipped", func(t *testing.T) {
		manifest := "manifest:\n  projects:\n    - name: ../etc\n      revision: v1.0.0\n"
		_, source := writeWorkspace(t, manifest)

		packages, findings := discoverWest(source)
		if len(packages) != 0 {
			t.Fatalf("packages = %#v, want none", packages)
		}
		if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
			t.Fatalf("findings = %+v, want the project reported as unreadable", findings)
		}
	})
}

// A manifest entry with no directory is the ordinary case rather than missing
// evidence: west clones only the projects whose groups are enabled, and a
// warning for each of the others would report the size of the manifest instead
// of the state of the build.
func TestAProjectThatWasNeverClonedIsSilence(t *testing.T) {
	_, source := writeWorkspace(t, westTestManifest, "modules/hal/nordic")

	packages, findings := discoverWest(source)
	if len(packages) != 1 || packages[0].Name != "hal_nordic" {
		t.Fatalf("packages = %#v, want the one project that is on disk", packages)
	}
	for _, finding := range findings {
		if finding.ID == "MISSING_PACKAGE_EVIDENCE" {
			t.Errorf("finding = %+v, want no complaint about a project west deliberately did not clone", finding)
		}
	}
}

// A project whose directory is the source root, or holds it, would rename the
// project's own component after a manifest entry. Section 19.2 refuses a marker
// file at the anchor root itself for the same reason.
func TestAProjectCoveringTheSourceRootIsDropped(t *testing.T) {
	manifest := "manifest:\n  projects:\n" +
		"    - name: application\n      path: app\n      revision: v1.0.0\n" +
		"    - name: workspace\n      path: .\n      revision: v1.0.0\n" +
		"    - name: hal_nordic\n      path: modules/hal/nordic\n      revision: v3.5.0\n"
	_, source := writeWorkspace(t, manifest, "modules/hal/nordic")

	packages, _ := discoverWest(source)
	if len(packages) != 1 || packages[0].Name != "hal_nordic" {
		t.Fatalf("packages = %#v, want neither the source root nor the workspace root as a package", packages)
	}
}

// Whole or not at all. A manifest this reader cannot read publishes nothing --
// a value taken out of a file whose remainder could not be read would be
// published with nothing behind it -- and says which file was in the way.
func TestAManifestThatCannotBeReadYieldsNothing(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		manifest string
		says     string
	}{
		{name: "a construct the reader refuses", manifest: "manifest: &all\n", says: "reserved indicator"},
		{name: "no manifest mapping", manifest: "projects:\n  - name: a\n", says: "no manifest mapping"},
		{name: "no projects sequence", manifest: "manifest:\n  defaults:\n    revision: main\n", says: "no projects sequence"},
		{name: "projects that are not a sequence", manifest: "manifest:\n  projects: none\n", says: "no projects sequence"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, source := writeWorkspace(t, testCase.manifest)

			packages, findings := discoverWest(source)
			if len(packages) != 0 {
				t.Fatalf("packages = %#v, want none out of a file that could not be read whole", packages)
			}
			if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
				t.Fatalf("findings = %+v, want one EVIDENCE_UNREADABLE", findings)
			}
			if !strings.Contains(findings[0].Message, testCase.says) {
				t.Errorf("message = %q, want it to name what was in the way (%q)", findings[0].Message, testCase.says)
			}
		})
	}
}

// A project entry that states no name names nothing, so it is skipped and said
// out loud -- and the projects around it are still read, because one broken
// entry is not a broken file.
func TestAProjectWithoutANameIsSkippedAndReported(t *testing.T) {
	manifest := "manifest:\n  projects:\n" +
		"    - path: modules/hal/nordic\n      revision: v3.5.0\n" +
		"    - name: hal_nordic\n      path: modules/hal/nordic\n      revision: v3.5.0\n"
	_, source := writeWorkspace(t, manifest)

	packages, findings := discoverWest(source)
	if len(packages) != 1 || packages[0].Name != "hal_nordic" {
		t.Fatalf("packages = %#v, want the named project alone", packages)
	}
	if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
		t.Fatalf("findings = %+v, want the nameless entry reported", findings)
	}
}

// The workspace config names the manifest, so a manifest that is not there is
// evidence the workspace itself promised and did not deliver.
func TestAManifestTheConfigNamesButDoesNotExistIsReported(t *testing.T) {
	topdir, source := writeWorkspace(t, westTestManifest)
	if err := os.Remove(filepath.Join(topdir, "zephyr", westManifestFile)); err != nil {
		t.Fatal(err)
	}

	packages, findings := discoverWest(source)
	if len(packages) != 0 {
		t.Fatalf("packages = %#v, want none", packages)
	}
	if len(findings) != 1 || findings[0].ID != "MISSING_PACKAGE_EVIDENCE" {
		t.Fatalf("findings = %+v, want the missing manifest reported", findings)
	}
	if findings[0].Subject.Ref != filepath.Join(topdir, "zephyr", westManifestFile) {
		t.Errorf("subject = %q, want the file the config named", findings[0].Subject.Ref)
	}
}

// Section 30: every file this adapter reads is bounded in bytes, and a file
// over its bound is refused whole rather than in part.
func TestAFileOverItsBoundIsRefusedWhole(t *testing.T) {
	padding := strings.Repeat("#", maxWestManifestBytes) + "\n"

	t.Run("the manifest", func(t *testing.T) {
		topdir, source := writeWorkspace(t, westTestManifest)
		writeWestFile(t, filepath.Join(topdir, "zephyr", westManifestFile), padding+westTestManifest)

		packages, findings := discoverWest(source)
		if len(packages) != 0 {
			t.Fatalf("packages = %#v, want none out of a file past the bound", packages)
		}
		if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
			t.Fatalf("findings = %+v, want the manifest reported over the limit", findings)
		}
	})

	t.Run("the workspace config", func(t *testing.T) {
		topdir, source := writeWorkspace(t, westTestManifest)
		writeWestFile(t, filepath.Join(topdir, westDir, westConfigName),
			strings.Repeat("#", maxWestConfigBytes)+"\n[manifest]\npath = zephyr\n")

		packages, findings := discoverWest(source)
		if len(packages) != 0 {
			t.Fatalf("packages = %#v, want none", packages)
		}
		if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
			t.Fatalf("findings = %+v, want the config reported over the limit", findings)
		}
	})

	t.Run("the module descriptor, which leaves the project with the manifest's name", func(t *testing.T) {
		topdir, source := writeWorkspace(t, westTestManifest)
		writeWestFile(t, filepath.Join(topdir, "modules", "hal", "nordic", westModuleDir, westModuleName),
			strings.Repeat("#", maxWestModuleBytes)+"\nname: hal_nordic_module\n")

		packages, findings := discoverWest(source)
		if _, ok := packageNamed(packages, "hal_nordic"); !ok {
			t.Fatalf("packages = %#v, want the project under the name the manifest states", packages)
		}
		var reported bool
		for _, finding := range findings {
			if finding.ID == "INPUT_LIMIT_EXCEEDED" {
				reported = true
			}
		}
		if !reported {
			t.Errorf("findings = %v, want the descriptor reported over the limit", findingIDs(findings))
		}
	})

	t.Run("a manifest naming more projects than the bound", func(t *testing.T) {
		var builder strings.Builder
		builder.WriteString("manifest:\n  projects:\n")
		for index := 0; index <= maxWestProjects; index++ {
			builder.WriteString(fmt.Sprintf("    - name: p%d\n", index))
		}
		_, source := writeWorkspace(t, builder.String())

		packages, findings := discoverWest(source)
		if len(packages) != 0 {
			t.Fatalf("packages = %d, want none", len(packages))
		}
		if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
			t.Fatalf("findings = %+v, want the manifest reported over the limit", findings)
		}
	})
}

// The checkout outranks the manifest, because a manifest says what was asked
// for and the checkout says what is there. The loser is kept, so the report can
// say the two disagreed.
func TestTheCheckoutSupersedesTheManifest(t *testing.T) {
	topdir, source := writeWorkspace(t, westTestManifest)
	runner := standInGit(t, "v3.6.0", "abc123")
	runner.Anchors = []string{topdir}

	packages, _ := west{}.Discover(Options{SourceDir: source, Runner: runner, Context: context.Background()})
	nordic, ok := packageNamed(packages, "hal_nordic")
	if !ok {
		t.Fatal("the project was not discovered")
	}
	if nordic.Version.Value != "3.6.0" || nordic.Version.Source != "git-describe" || nordic.Version.Rank != RankObservedCheckout {
		t.Errorf("version = %#v, want what the checkout answered", nordic.Version)
	}
	if nordic.Commit != "abc123" {
		t.Errorf("commit = %q, want the one the checkout answered", nordic.Commit)
	}
	var displaced bool
	for _, contribution := range nordic.Superseded {
		if contribution.Field == FieldVersion && contribution.Claim.Value == "3.5.0" {
			displaced = true
		}
	}
	if !displaced {
		t.Errorf("superseded = %#v, want the manifest's revision kept as the origin that lost", nordic.Superseded)
	}
}

// A dirty checkout answers with a version that does not identify its content,
// which section 20.3 marks down and findings.md reports.
func TestADirtyCheckoutIsReported(t *testing.T) {
	topdir, source := writeWorkspace(t, westTestManifest)
	runner := standInGit(t, "v3.6.0-dirty", "abc123")
	runner.Anchors = []string{topdir}

	packages, findings := west{}.Discover(Options{SourceDir: source, Runner: runner, Context: context.Background()})
	nordic, _ := packageNamed(packages, "hal_nordic")
	if !nordic.Dirty || nordic.Version.Confidence != domain.ConfidenceMedium {
		t.Errorf("package = %#v, want a dirty checkout of medium confidence", nordic)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "VCS_DIRTY" && finding.Subject.Ref == "hal_nordic" {
			reported = true
			if finding.Severity != domain.SeverityInfo {
				t.Errorf("severity = %q, want info", finding.Severity)
			}
		}
	}
	if !reported {
		t.Error("a dirty checkout passed in silence")
	}
}

// Introspection is off by default, and an adapter must work without it: nothing
// is executed, and the manifest's own claims stand.
func TestWithoutIntrospectionNoCommandRuns(t *testing.T) {
	_, source := writeWorkspace(t, westTestManifest)
	runner := standInGit(t, "v3.6.0", "abc123")
	runner.Features.Git = false

	packages, _ := west{}.Discover(Options{SourceDir: source, Runner: runner, Context: context.Background()})
	nordic, _ := packageNamed(packages, "hal_nordic")
	if nordic.Version.Source != "west" {
		t.Errorf("version = %#v, want the manifest's, with git never asked", nordic.Version)
	}
	if records := runner.Records(); len(records) != 0 {
		t.Errorf("commands ran = %v, want none with the group off", records)
	}
}

// The honest limit of point 4 of this work: the runner refuses a path outside
// the anchors of the run, and in the usual Zephyr layout the workspace lies
// above the source tree. The refusal is silent -- no process is created, so
// nothing failed -- and the manifest's claims stand alone.
func TestAProjectOutsideTheAnchorsCannotBeAskedAndSaysSo(t *testing.T) {
	_, source := writeWorkspace(t, westTestManifest)
	runner := standInGit(t, "v3.6.0", "abc123")
	// The anchors of a run are the project and build trees; the workspace is
	// above the first and holds none of the projects.
	runner.Anchors = []string{source}

	packages, findings := west{}.Discover(Options{SourceDir: source, Runner: runner, Context: context.Background()})
	nordic, _ := packageNamed(packages, "hal_nordic")
	if nordic.Version.Value != "3.5.0" || nordic.Version.Source != "west" {
		t.Errorf("version = %#v, want the manifest's, because the checkout could not be asked", nordic.Version)
	}
	if nordic.Commit != "" {
		t.Errorf("commit = %q, want none: no command answered", nordic.Commit)
	}
	for _, finding := range findings {
		if finding.ID != "UNKNOWN_VERSION" {
			t.Errorf("finding = %+v, want no complaint: a refused path is not a failure", finding)
		}
	}
}

// Byte-identical output from identical evidence (section 6): two runs over one
// workspace must produce the same packages in the same order, with the same
// claims.
func TestTwoRunsOverOneWorkspaceAgree(t *testing.T) {
	_, source := writeWorkspace(t, westTestManifest)

	first, firstFindings := discoverWest(source)
	second, secondFindings := discoverWest(source)
	if len(first) != len(second) {
		t.Fatalf("two runs found %d and %d packages", len(first), len(second))
	}
	for index := range first {
		if first[index].Name != second[index].Name || first[index].Root() != second[index].Root() ||
			first[index].Version != second[index].Version || first[index].PURL != second[index].PURL {
			t.Errorf("package %d differs between two runs: %#v and %#v", index, first[index], second[index])
		}
	}
	if strings.Join(findingIDs(firstFindings), ",") != strings.Join(findingIDs(secondFindings), ",") {
		t.Errorf("findings differ between two runs: %v and %v", findingIDs(firstFindings), findingIDs(secondFindings))
	}
}

// A `projects:` that holds plain words rather than mappings parses -- the
// reader records the sequence and reads nothing out of it -- and silence there
// would be indistinguishable from a workspace that declares nothing. A file
// this tool cannot read is refused whole and says so, like every other shape
// that is not what was expected.
func TestAProjectsListHoldingNoProjectIsRefused(t *testing.T) {
	_, source := writeWorkspace(t, "manifest:\n  projects:\n    - hal_nordic\n    - mcuboot\n")

	packages, findings := discoverWest(source)
	if len(packages) != 0 {
		t.Fatalf("packages = %#v, want none out of a list of bare words", packages)
	}
	if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
		t.Fatalf("findings = %+v, want one EVIDENCE_UNREADABLE", findings)
	}
	if !strings.Contains(findings[0].Message, "no project this reader could read") {
		t.Errorf("message = %q, want it to say the projects could not be read", findings[0].Message)
	}
}

// The workspace config is the only statement there is about where the manifest
// lives, so a config this tool cannot use ends the adapter rather than sending
// it looking for a manifest somewhere it was never told about.
func TestAWorkspaceConfigThatCannotBeUsedReadsNothing(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		config string
		says   string
	}{
		{
			name:   "no manifest section at all",
			config: "[zephyr]\nbase = zephyr\n",
			says:   "no manifest section",
		},
		{
			// Exactly the section, not a name that merely begins with it: the
			// other sections of this file are somebody else's settings.
			name:   "a section that only begins like the manifest's",
			config: "[manifestation]\npath = zephyr\n",
			says:   "no manifest section",
		},
		{
			name:   "a manifest section stating no path",
			config: "[manifest]\nfile = west.yml\n",
			says:   "not a path inside the workspace",
		},
		{
			name:   "an absolute manifest repository",
			config: "[manifest]\npath = /etc\nfile = west.yml\n",
			says:   "not a path inside the workspace",
		},
		{
			// filepath.IsAbs answers for the platform this is compiled for, so
			// the Windows shapes are refused explicitly and on every platform.
			name:   "a manifest repository behind a drive letter",
			config: "[manifest]\npath = C:\\zephyr\nfile = west.yml\n",
			says:   "not a path inside the workspace",
		},
		{
			name:   "a manifest repository behind a backslash",
			config: "[manifest]\npath = \\zephyr\nfile = west.yml\n",
			says:   "not a path inside the workspace",
		},
		{
			name:   "a manifest file climbing out of its repository",
			config: "[manifest]\npath = zephyr\nfile = ../../etc/passwd\n",
			says:   "not a path inside the workspace",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			topdir, source := writeWorkspace(t, westTestManifest)
			writeWestFile(t, filepath.Join(topdir, westDir, westConfigName), testCase.config)

			packages, findings := discoverWest(source)
			if len(packages) != 0 {
				t.Fatalf("packages = %#v, want none: the workspace never said where its manifest is", packages)
			}
			if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
				t.Fatalf("findings = %+v, want one EVIDENCE_UNREADABLE", findings)
			}
			if !strings.Contains(findings[0].Message, testCase.says) {
				t.Errorf("message = %q, want it to name what was in the way (%q)", findings[0].Message, testCase.says)
			}
		})
	}

	t.Run("a config that cannot be read to its end", func(t *testing.T) {
		topdir, source := writeWorkspace(t, westTestManifest)
		// Inside the byte bound and past the line bound: limits.Scanner refuses
		// rather than growing without one, which is the second half of section
		// 30 and the only way a file this small stops being readable.
		writeWestFile(t, filepath.Join(topdir, westDir, westConfigName),
			strings.Repeat("x", limits.MaxLine))

		packages, findings := discoverWest(source)
		if len(packages) != 0 {
			t.Fatalf("packages = %#v, want none", packages)
		}
		if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
			t.Fatalf("findings = %+v, want the config reported as unreadable", findings)
		}
		if !strings.Contains(findings[0].Message, "could not be read to its end") {
			t.Errorf("message = %q, want it to say the file was not read whole", findings[0].Message)
		}
	})

	t.Run("a directory where the config belongs is no workspace at all", func(t *testing.T) {
		topdir, source := writeWorkspace(t, westTestManifest)
		if err := os.Remove(filepath.Join(topdir, westDir, westConfigName)); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(topdir, westDir, westConfigName), 0o755); err != nil {
			t.Fatal(err)
		}

		// A workspace is a directory holding that file, and a directory of the
		// same name is not one. Nothing is opened and nothing is missing.
		packages, findings := discoverWest(source)
		if len(packages) != 0 || len(findings) != 0 {
			t.Fatalf("packages = %#v, findings = %#v, want silence", packages, findings)
		}
	})

	t.Run("a config naming no file takes west's own default", func(t *testing.T) {
		topdir, source := writeWorkspace(t, westTestManifest)
		writeWestFile(t, filepath.Join(topdir, westDir, westConfigName), "[manifest]\npath = zephyr\n")

		packages, _ := discoverWest(source)
		if len(packages) != 3 {
			t.Fatalf("packages = %#v, want the manifest found at the default file name", packages)
		}
	})
}

// A module descriptor is a better name for a component, not a condition for
// having one: where it cannot be read, or does not name a single directory
// segment, the project keeps the name the manifest gave it. Losing the module
// name is not losing the project.
func TestAModuleDescriptorThatCannotBeUsedLeavesTheManifestsName(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		descriptor string
		unreadable bool
	}{
		{name: "a descriptor the reader refuses", descriptor: "name: &anchor\n", unreadable: true},
		{name: "a descriptor stating no name", descriptor: "build:\n  cmake: .\n"},
		{name: "a name that is not one path segment", descriptor: "name: hal/nordic\n"},
		{name: "a name that is a climb out of the tree", descriptor: "name: ..\n"},
		{name: "an empty name", descriptor: "name: \"\"\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			topdir, source := writeWorkspace(t, westTestManifest)
			writeWestFile(t, filepath.Join(topdir, "modules", "hal", "nordic", westModuleDir, westModuleName), testCase.descriptor)

			packages, findings := discoverWest(source)
			found, ok := packageNamed(packages, "hal_nordic")
			if !ok {
				t.Fatalf("packages = %#v, want the project under the name the manifest states", packages)
			}
			if found.AnchorKey != "pkg:west/hal_nordic" || found.Root() != filepath.Join(topdir, "modules", "hal", "nordic") {
				t.Errorf("package = %#v, want the manifest's name and the manifest's directory", found)
			}
			var reported bool
			for _, finding := range findings {
				if finding.ID == "EVIDENCE_UNREADABLE" {
					reported = true
				}
			}
			if reported != testCase.unreadable {
				t.Errorf("findings = %v, want a descriptor that could not be read reported (%v) and a merely unusable one passed over",
					findingIDs(findings), testCase.unreadable)
			}
		})
	}

	t.Run("the other spelling of the extension is read too", func(t *testing.T) {
		topdir, source := writeWorkspace(t, westTestManifest)
		writeWestFile(t, filepath.Join(topdir, "modules", "hal", "nordic", westModuleDir, westModuleAltName),
			"name: hal_nordic_module\n")

		packages, _ := discoverWest(source)
		if _, ok := packageNamed(packages, "hal_nordic_module"); !ok {
			t.Fatalf("packages = %#v, want the name the descriptor states", packages)
		}
	})
}

// Where a project came from is what the manifest says and nothing more: its own
// URL if it states one, otherwise the remote's base joined with the repository
// path -- which defaults to the project's name. A remote nobody declared leaves
// no repository at all rather than a guessed one.
func TestWhereAProjectSaysItCameFrom(t *testing.T) {
	const remotes = "manifest:\n  defaults:\n    remote: upstream\n" +
		"  remotes:\n    - name: upstream\n      url-base: https://example.invalid/zephyrproject-rtos\n" +
		"    - name: mirror\n      url-base: https://mirror.invalid/z/\n  projects:\n"
	for _, testCase := range []struct {
		name    string
		project string
		want    string
		purl    string
	}{
		{
			name:    "a project stating its own URL is not using the remote",
			project: "    - name: hal_nordic\n      path: modules/hal/nordic\n      revision: v3.5.0\n      url: https://example.invalid/own/hal_nordic.git\n      remote: upstream\n",
			want:    "https://example.invalid/own/hal_nordic",
			purl:    "pkg:generic/hal_nordic@3.5.0?vcs_url=git%2Bhttps%3A%2F%2Fexample.invalid%2Fown%2Fhal_nordic",
		},
		{
			name:    "a project naming a remote of its own",
			project: "    - name: hal_nordic\n      path: modules/hal/nordic\n      revision: v3.5.0\n      remote: mirror\n",
			// A base that ends in a separator does not produce a doubled one.
			want: "https://mirror.invalid/z/hal_nordic",
			purl: "pkg:generic/hal_nordic@3.5.0?vcs_url=git%2Bhttps%3A%2F%2Fmirror.invalid%2Fz%2Fhal_nordic",
		},
		{
			name:    "a repository path that is not the project's name",
			project: "    - name: hal_nordic\n      path: modules/hal/nordic\n      revision: v3.5.0\n      repo-path: hal/nordic-hal\n",
			want:    "https://example.invalid/zephyrproject-rtos/hal/nordic-hal",
			purl:    "pkg:generic/hal_nordic@3.5.0?vcs_url=git%2Bhttps%3A%2F%2Fexample.invalid%2Fzephyrproject-rtos%2Fhal%2Fnordic-hal",
		},
		{
			name:    "a remote nobody declared leaves no repository",
			project: "    - name: hal_nordic\n      path: modules/hal/nordic\n      revision: v3.5.0\n      remote: nowhere\n",
			want:    "",
			purl:    "pkg:generic/hal_nordic@3.5.0",
		},
		{
			name:    "credentials in a URL are not published",
			project: "    - name: hal_nordic\n      path: modules/hal/nordic\n      revision: v3.5.0\n      url: https://token:secret@example.invalid/own/hal_nordic.git\n",
			want:    "https://example.invalid/own/hal_nordic",
			purl:    "pkg:generic/hal_nordic@3.5.0?vcs_url=git%2Bhttps%3A%2F%2Fexample.invalid%2Fown%2Fhal_nordic",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, source := writeWorkspace(t, remotes+testCase.project, "modules/hal/nordic")

			packages, _ := discoverWest(source)
			if len(packages) != 1 {
				t.Fatalf("packages = %#v, want the one project", packages)
			}
			if packages[0].VCSURL != testCase.want {
				t.Errorf("vcs url = %q, want %q", packages[0].VCSURL, testCase.want)
			}
			if packages[0].PURL.Value != testCase.purl {
				t.Errorf("purl = %q, want %q", packages[0].PURL.Value, testCase.purl)
			}
		})
	}
}

// A project that states neither a revision nor anywhere to have come from is
// still a directory the build may have read, so it is published under its own
// name -- with no version invented for it, and with the missing one said out
// loud.
func TestAProjectStatingNothingIsStillNamedAndSaysWhatIsMissing(t *testing.T) {
	_, source := writeWorkspace(t, "manifest:\n  projects:\n    - name: hal_nordic\n      path: modules/hal/nordic\n", "modules/hal/nordic")

	packages, findings := discoverWest(source)
	if len(packages) != 1 {
		t.Fatalf("packages = %#v, want the one project", packages)
	}
	if packages[0].Version.Value != "" || packages[0].Version.Rank != RankNone {
		t.Errorf("version = %#v, want none: the manifest states none and nothing else was asked", packages[0].Version)
	}
	if packages[0].PURL.Value != "pkg:generic/hal_nordic" {
		t.Errorf("purl = %q, want the project named without a version or a repository", packages[0].PURL.Value)
	}
	if len(findings) != 1 || findings[0].ID != "UNKNOWN_VERSION" || findings[0].Subject.Ref != "hal_nordic" {
		t.Fatalf("findings = %+v, want the missing version reported against the component", findings)
	}
	if findings[0].Severity != domain.SeverityWarning {
		t.Errorf("severity = %q, want warning", findings[0].Severity)
	}
}

// The manifest repository describes itself under `self:`, and west gives it no
// name of its own -- it calls it "manifest". Publishing a component out of that
// would name a dependency after a keyword, so nothing is read from it.
func TestTheManifestRepositoryItselfIsNotAProject(t *testing.T) {
	topdir, source := writeWorkspace(t, westTestManifest)

	packages, _ := discoverWest(source)
	for _, entry := range packages {
		if entry.Root() == filepath.Join(topdir, "zephyr") {
			t.Errorf("the manifest repository was published as the package %q", entry.Name)
		}
		if entry.Name == "manifest" || entry.Name == "self" {
			t.Errorf("a package was named after a keyword of the manifest: %q", entry.Name)
		}
	}
}

// One anchor key per package: a key registered twice aborts the run, so two
// entries resolving to one name are a contradiction the manifest has to lose.
// The first is kept, because the first is what every other reader here keeps
// and because two runs must agree.
func TestTwoProjectsOfOneNameKeepTheFirst(t *testing.T) {
	topdir, source := writeWorkspace(t, "manifest:\n  projects:\n"+
		"    - name: hal_nordic\n      path: modules/hal/nordic\n      revision: v3.5.0\n"+
		"    - name: hal_nordic\n      path: bootloader/mcuboot\n      revision: v1.0.0\n")

	packages, _ := discoverWest(source)
	if len(packages) != 1 {
		t.Fatalf("packages = %#v, want one package for one name", packages)
	}
	if packages[0].Root() != filepath.Join(topdir, "modules", "hal", "nordic") || packages[0].Version.Value != "3.5.0" {
		t.Errorf("package = %#v, want the first of the two entries", packages[0])
	}
}

// The position of west in the registry is behaviour and not taste: the first
// adapter to claim a root keeps it, and a directory west cloned is described by
// the manifest that asked for it -- with a name, a revision and a repository --
// while the .gitmodules line naming the same path states a path and a URL and
// nothing more. The adapter that lost is named rather than dropped in silence.
func TestAWestProjectIsDescribedByWestAndNotByGitmodules(t *testing.T) {
	topdir, _ := writeWorkspace(t, westTestManifest)
	writeWestFile(t, filepath.Join(topdir, ".gitmodules"),
		"[submodule \"nordic\"]\n\tpath = modules/hal/nordic\n\turl = https://example.invalid/other/nordic.git\n")

	// The workspace root is the source root here, which is the one layout in
	// which both adapters see the same directory at all.
	packages, findings := Discover(Options{SourceDir: topdir, Context: context.Background()})
	root := filepath.Join(topdir, "modules", "hal", "nordic")
	var owner string
	for _, entry := range packages {
		if entry.Root() == root {
			owner = entry.Manager
		}
	}
	if owner != "west" {
		t.Fatalf("the directory west cloned is described by %q, want west", owner)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "COMPONENT_MAPPING_CONFLICT" && strings.Contains(finding.Message, "west") {
			reported = true
			if finding.Severity != domain.SeverityInfo {
				t.Errorf("severity = %q, want info", finding.Severity)
			}
		}
	}
	if !reported {
		t.Errorf("findings = %v, want the adapter that lost the root named", findingIDs(findings))
	}
}
