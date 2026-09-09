package pkgmanager

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// The ESP-IDF component manager leaves two things behind in a project: the
// lock file it wrote when it resolved the dependency tree, and the components
// it unpacked into managed_components. The helpers below lay out exactly that,
// so the adapter is tested against the shape it really meets.

func writeIDFFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeIDFLock writes a lock file naming the given components. The idf
// pseudo-entry is always present, because a real lock file always carries it
// and skipping it is part of what is under test.
func writeIDFLock(t *testing.T, source string, entries ...string) {
	t.Helper()
	content := "dependencies:\n"
	for _, entry := range entries {
		content += entry
	}
	content += "  idf:\n    source:\n      type: idf\n    version: 5.1.2\n"
	content += "manifest_hash: 4f1e\ntarget: esp32\nversion: 1.0.0\n"
	writeIDFFile(t, filepath.Join(source, idfLockName), content)
}

// registryEntry is one resolved component of the registry, in the shape the
// component manager writes it.
func registryEntry(name, version string) string {
	return "  " + name + ":\n" +
		"    component_hash: 5c9d1f\n" +
		"    source:\n" +
		"      registry_url: https://components.espressif.com/\n" +
		"      type: service\n" +
		"    version: " + version + "\n"
}

// writeManagedComponent unpacks a component where the manager puts it.
func writeManagedComponent(t *testing.T, source, directory, manifest string) {
	t.Helper()
	root := filepath.Join(source, idfManagedDir, directory)
	if manifest == "" {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		return
	}
	writeIDFFile(t, filepath.Join(root, idfManifestName), manifest)
}

// ledStripManifest is a manifest in the shape components published to the
// registry really have, block sequence and all.
const ledStripManifest = `version: "2.5.3"
description: |
  Driver for addressable LEDs.
  It spans more than one line.
url: https://components.espressif.com/components/espressif/led_strip
repository: git@github.com:espressif/idf-extra-components.git
license: Apache-2.0
targets:
  - esp32
  - esp32s3
dependencies:
  idf: ">=4.4"
`

func idfPackages(t *testing.T, source string) ([]Package, []domain.Finding) {
	t.Helper()
	return espidf{}.Discover(Options{SourceDir: source, Context: context.Background()})
}

func findingIDs(findings []domain.Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.ID)
	}
	return ids
}

func TestAManagedComponentIsReadFromTheLockAndItsOwnManifest(t *testing.T) {
	source := t.TempDir()
	writeIDFLock(t, source, registryEntry("espressif/led_strip", "2.5.3"))
	writeManagedComponent(t, source, "espressif__led_strip", ledStripManifest)

	packages, findings := idfPackages(t, source)
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Name != "espressif/led_strip" {
		t.Errorf("name = %q, want the namespaced name the registry uses", found.Name)
	}
	if want := filepath.Join(source, idfManagedDir, "espressif__led_strip"); found.Root() != want {
		t.Errorf("root = %q, want %q", found.Root(), want)
	}
	if found.AnchorKey != "pkg:idf/espressif/led_strip" {
		t.Errorf("anchor key = %q", found.AnchorKey)
	}
	if found.Version.Value != "2.5.3" || found.Version.Source != idfVersionSource {
		t.Errorf("version = %q from %q", found.Version.Value, found.Version.Source)
	}
	if found.Version.Rank != RankInstallState || found.Version.Confidence != domain.ConfidenceHigh {
		t.Errorf("version rank/confidence = %v/%q, want the lock file's install state at high confidence",
			found.Version.Rank, found.Version.Confidence)
	}
	if found.License.Value != "Apache-2.0" || found.License.Rank != RankDeclaredManifest {
		t.Errorf("license = %q at rank %v, want the manifest's declaration", found.License.Value, found.License.Rank)
	}
	if found.PURL.Value != "pkg:idf/espressif/led_strip@2.5.3" {
		t.Errorf("purl = %q, want the form of section 20.4", found.PURL.Value)
	}
	if found.VCSURL != "https://github.com/espressif/idf-extra-components" {
		t.Errorf("vcs url = %q, want the repository the manifest declares, normalized", found.VCSURL)
	}
	if found.Supplier.Value != "" {
		t.Error("a supplier was invented; a registry namespace is not a supplier statement (section 20.5)")
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v, want none", findings)
	}
}

// The order of the packages is part of the output, so two runs over one project
// must not differ in it -- nor in anything else the adapter returns.
func TestTheSameProjectYieldsTheSamePackagesTwice(t *testing.T) {
	source := t.TempDir()
	writeIDFLock(t, source,
		registryEntry("espressif/led_strip", "2.5.3"),
		registryEntry("espressif/mdns", "1.2.0"),
		registryEntry("someone/cbor", "0.9.1"),
		"  someone/from_git:\n    source:\n      git: https://example.invalid/o/r.git\n      type: git\n    version: 1.0.0\n",
	)
	for _, directory := range []string{"espressif__led_strip", "espressif__mdns", "someone__cbor", "someone__from_git"} {
		writeManagedComponent(t, source, directory, "version: \"9.9.9\"\nlicense: MIT\n")
	}

	first, firstFindings := idfPackages(t, source)
	second, secondFindings := idfPackages(t, source)
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstFindings, secondFindings) {
		t.Fatalf("two runs disagreed:\n%#v\n%#v", first, second)
	}
	if len(first) != 4 {
		t.Fatalf("packages = %#v, want all four managed components", first)
	}
	if first[0].Name != "espressif/led_strip" || first[3].Name != "someone/from_git" {
		t.Errorf("order = %q .. %q, want the components sorted by name", first[0].Name, first[3].Name)
	}
}

// A lock file is the better evidence, but its absence must not blank the run:
// the directories the manager filled still name the components, and each one's
// own manifest still states a version -- one rank weaker, and said out loud.
func TestWithoutALockFileTheUnpackedComponentsStillAnswer(t *testing.T) {
	source := t.TempDir()
	writeManagedComponent(t, source, "espressif__led_strip", ledStripManifest)
	writeManagedComponent(t, source, "someone__nameless", "description: no version here\n")

	packages, findings := idfPackages(t, source)
	if len(packages) != 2 {
		t.Fatalf("packages = %#v", packages)
	}
	if packages[0].Version.Value != "2.5.3" || packages[0].Version.Rank != RankDeclaredManifest {
		t.Errorf("version = %q at rank %v, want the manifest's own declaration",
			packages[0].Version.Value, packages[0].Version.Rank)
	}
	if packages[1].Version.Value != "" {
		t.Errorf("version = %q, want none: nothing stated one", packages[1].Version.Value)
	}
	if len(findings) != 1 || findings[0].ID != "UNKNOWN_VERSION" || findings[0].Subject.Ref != "someone/nameless" {
		t.Fatalf("findings = %#v, want one UNKNOWN_VERSION naming the component that states none", findings)
	}
}

// A project that does not use the component manager has nothing missing about
// it. Neither file exists, so nothing is opened and nothing is said.
func TestAProjectWithoutTheComponentManagerIsSilent(t *testing.T) {
	source := t.TempDir()
	writeIDFFile(t, filepath.Join(source, "main", "main.c"), "int main(void) { return 0; }\n")

	packages, findings := idfPackages(t, source)
	if len(packages) != 0 || len(findings) != 0 {
		t.Fatalf("packages = %#v, findings = %#v, want silence", packages, findings)
	}
}

// The IDF's own components are part of the framework, not packages the
// component manager resolved. Milestone 20 is about them; this adapter is not.
func TestTheFrameworksOwnComponentsAreNotManagedPackages(t *testing.T) {
	source := t.TempDir()
	writeIDFFile(t, filepath.Join(source, "components", "driver", idfManifestName), "version: 1.0.0\n")
	writeIDFLock(t, source, registryEntry("espressif/led_strip", "2.5.3"))
	writeManagedComponent(t, source, "espressif__led_strip", ledStripManifest)

	packages, _ := idfPackages(t, source)
	if len(packages) != 1 || packages[0].Name != "espressif/led_strip" {
		t.Fatalf("packages = %#v, want only the managed component", packages)
	}
}

// A lock entry records what was resolved. Without the directory the component
// owns no file, so it names a component no evidence chain can reach -- which is
// worth a warning and not a package.
func TestALockedComponentWithoutItsDirectoryIsReported(t *testing.T) {
	source := t.TempDir()
	writeIDFLock(t, source,
		registryEntry("espressif/led_strip", "2.5.3"),
		"  a_local_one:\n    source:\n      path: ../shared\n      type: local\n    version: 1.0.0\n",
	)
	if err := os.MkdirAll(filepath.Join(source, idfManagedDir), 0o755); err != nil {
		t.Fatal(err)
	}

	packages, findings := idfPackages(t, source)
	if len(packages) != 0 {
		t.Fatalf("packages = %#v, want none", packages)
	}
	if len(findings) != 1 || findings[0].ID != "MISSING_PACKAGE_EVIDENCE" {
		t.Fatalf("findings = %#v, want one MISSING_PACKAGE_EVIDENCE", findings)
	}
	if findings[0].Subject.Ref != "espressif/led_strip" {
		t.Errorf("subject = %q, want the component that was resolved", findings[0].Subject.Ref)
	}
	if !strings.Contains(findings[0].Message, filepath.Join(idfManagedDir, "espressif__led_strip")) {
		t.Errorf("message = %q, want it to name the directory that was looked for", findings[0].Message)
	}
}

// A lock file this reader does not fully understand contributes nothing at all
// rather than something partial. Where the manager's directories are there, the
// run degrades to them instead of going blank.
func TestALockFileThatCannotBeReadIsRefusedWhole(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
	}{
		{name: "a quoted scalar cut off mid line", content: "dependencies:\n  espressif/led_strip:\n    version: \"2.5.3\n"},
		{name: "a dedent to a column no mapping starts at", content: "dependencies:\n    a/b:\n  c/d:\n"},
		{name: "a flow collection left open", content: "dependencies: {espressif/led_strip:\n"},
		{name: "an anchor", content: "dependencies: &all\n  espressif/led_strip:\n    version: 1.0.0\n"},
		{name: "a merge key", content: "dependencies:\n  <<: other\n"},
		{name: "a tab in the indentation", content: "dependencies:\n\tespressif/led_strip:\n"},
		{name: "a duplicate key", content: "dependencies:\n  a/b:\n    version: 1\n  a/b:\n    version: 2\n"},
		{name: "not YAML at all", content: "\x00\x01 this is not a lock file\n"},
		{name: "a second document", content: "dependencies:\n  a/b:\n    version: 1\n---\ndependencies:\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			source := t.TempDir()
			writeIDFFile(t, filepath.Join(source, idfLockName), testCase.content)
			writeManagedComponent(t, source, "espressif__led_strip", ledStripManifest)

			packages, findings := idfPackages(t, source)
			unreadable := 0
			for _, finding := range findings {
				if finding.ID == "EVIDENCE_UNREADABLE" {
					unreadable++
					if finding.Subject.Ref != filepath.Join(source, idfLockName) {
						t.Errorf("subject = %q, want the lock file", finding.Subject.Ref)
					}
				}
			}
			if unreadable != 1 {
				t.Fatalf("findings = %v, want exactly one EVIDENCE_UNREADABLE", findingIDs(findings))
			}
			// The lock gave nothing, so the directory answered instead, and
			// with the weaker rank that says where the version came from.
			if len(packages) != 1 || packages[0].Version.Rank != RankDeclaredManifest {
				t.Fatalf("packages = %#v, want the component found from its directory alone", packages)
			}
		})
	}
}

// A lock key that is not a component name must never become a path. What is
// refused is named, so a reader of the findings can see what was skipped.
func TestALockKeyCanNotPointOutsideManagedComponents(t *testing.T) {
	source := t.TempDir()
	writeIDFLock(t, source,
		"  ../escape:\n    version: 1.0.0\n",
		"  /etc/passwd:\n    version: 1.0.0\n",
		"  espressif/led_strip/deeper:\n    version: 1.0.0\n",
		"  .hidden:\n    version: 1.0.0\n",
	)
	if err := os.MkdirAll(filepath.Join(source, idfManagedDir), 0o755); err != nil {
		t.Fatal(err)
	}

	packages, findings := idfPackages(t, source)
	if len(packages) != 0 {
		t.Fatalf("packages = %#v, want none: no key named a component", packages)
	}
	if len(findings) != 4 {
		t.Fatalf("findings = %v, want one per unusable key", findingIDs(findings))
	}
	for _, finding := range findings {
		if finding.ID != "EVIDENCE_UNREADABLE" || finding.Subject.Ref != filepath.Join(source, idfLockName) {
			t.Errorf("finding = %+v, want EVIDENCE_UNREADABLE naming the lock file", finding)
		}
	}
}

// A manifest that cannot be read costs the licence and nothing else: the
// component was resolved by the lock, and that statement stands on its own.
func TestAnUnreadableManifestLeavesTheComponentWithItsLockedVersion(t *testing.T) {
	source := t.TempDir()
	writeIDFLock(t, source, registryEntry("espressif/led_strip", "2.5.3"))
	writeManagedComponent(t, source, "espressif__led_strip", "license: [MIT\n")

	packages, findings := idfPackages(t, source)
	if len(packages) != 1 || packages[0].Version.Value != "2.5.3" {
		t.Fatalf("packages = %#v, want the component with the version the lock recorded", packages)
	}
	if packages[0].License.Value != "" {
		t.Errorf("license = %q, want none: no licence may be invented out of a file that was refused",
			packages[0].License.Value)
	}
	if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
	if findings[0].Subject.Ref != filepath.Join(source, idfManagedDir, "espressif__led_strip", idfManifestName) {
		t.Errorf("subject = %q, want the manifest that could not be read", findings[0].Subject.Ref)
	}
}

// Every bound of section 30 refuses its file whole. Half a lock file would
// attribute some components and leave the rest looking as though the project
// never depended on them.
func TestAnInputOverItsBoundIsRefusedWhole(t *testing.T) {
	t.Run("a lock file over the byte bound", func(t *testing.T) {
		source := t.TempDir()
		writeIDFFile(t, filepath.Join(source, idfLockName),
			"dependencies:\n  espressif/led_strip:\n    version: 2.5.3\n# "+
				strings.Repeat("x", maxIDFLockBytes)+"\n")

		_, findings := idfPackages(t, source)
		if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
			t.Fatalf("findings = %#v, want one INPUT_LIMIT_EXCEEDED", findings)
		}
		if findings[0].Subject.Ref != filepath.Join(source, idfLockName) {
			t.Errorf("subject = %q, want the lock file", findings[0].Subject.Ref)
		}
	})

	t.Run("a manifest over the byte bound", func(t *testing.T) {
		source := t.TempDir()
		writeIDFLock(t, source, registryEntry("espressif/led_strip", "2.5.3"))
		writeManagedComponent(t, source, "espressif__led_strip",
			"license: Apache-2.0\n# "+strings.Repeat("x", maxIDFManifestBytes)+"\n")

		packages, findings := idfPackages(t, source)
		if len(packages) != 1 || packages[0].License.Value != "" {
			t.Fatalf("packages = %#v, want the component without a licence from a refused file", packages)
		}
		if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
			t.Fatalf("findings = %#v, want one INPUT_LIMIT_EXCEEDED", findings)
		}
	})

	t.Run("a lock file naming more components than the bound", func(t *testing.T) {
		source := t.TempDir()
		var builder strings.Builder
		builder.WriteString("dependencies:\n")
		for index := 0; index <= maxIDFComponents; index++ {
			builder.WriteString("  ns/c")
			builder.WriteString(strconv.Itoa(index))
			builder.WriteString(":\n    version: 1.0.0\n")
		}
		writeIDFFile(t, filepath.Join(source, idfLockName), builder.String())

		packages, findings := idfPackages(t, source)
		if len(packages) != 0 {
			t.Fatalf("packages = %#v, want none from a refused lock file", packages)
		}
		if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
			t.Fatalf("findings = %#v, want one INPUT_LIMIT_EXCEEDED", findings)
		}
	})
}

// Two origins describing one component are ranked, not averaged, and the loser
// is kept: a disagreement is something to report, not something to drop.
func TestTheLockOutranksTheComponentsOwnManifest(t *testing.T) {
	source := t.TempDir()
	writeIDFLock(t, source, registryEntry("espressif/led_strip", "2.5.3"))
	writeManagedComponent(t, source, "espressif__led_strip", "version: 2.4.0\nlicense: Apache-2.0\n")

	packages, _ := idfPackages(t, source)
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Version.Value != "2.5.3" || found.Version.Rank != RankInstallState {
		t.Errorf("version = %q at rank %v, want what the lock recorded", found.Version.Value, found.Version.Rank)
	}
	if found.PURL.Value != "pkg:idf/espressif/led_strip@2.5.3" {
		t.Errorf("purl = %q, want the version that won", found.PURL.Value)
	}
	superseded := 0
	for _, contribution := range found.Superseded {
		if contribution.Field == FieldVersion && contribution.Claim.Value == "2.4.0" {
			superseded++
		}
	}
	if superseded != 1 {
		t.Errorf("superseded = %#v, want the manifest's version kept as the losing claim", found.Superseded)
	}
}

// A component vendored as a submodule and downloaded by the component manager
// is one directory with two owners. The adapter order decides, and the loser is
// reported rather than merged into the winner.
func TestAManagedComponentThatIsAlsoASubmoduleIsReportedOnce(t *testing.T) {
	source := t.TempDir()
	writeIDFLock(t, source, registryEntry("espressif/led_strip", "2.5.3"))
	writeManagedComponent(t, source, "espressif__led_strip", ledStripManifest)
	writeIDFFile(t, filepath.Join(source, ".gitmodules"),
		"[submodule \"led_strip\"]\n\tpath = managed_components/espressif__led_strip\n"+
			"\turl = https://example.invalid/o/led_strip.git\n")

	packages, findings := Discover(Options{SourceDir: source, Context: context.Background()})
	if len(packages) != 1 || packages[0].Manager != (espidf{}).Manager() {
		t.Fatalf("packages = %#v, want the one the component manager installed", packages)
	}
	conflicts := 0
	for _, finding := range findings {
		if finding.ID == "COMPONENT_MAPPING_CONFLICT" {
			conflicts++
		}
	}
	if conflicts != 1 {
		t.Fatalf("findings = %v, want one COMPONENT_MAPPING_CONFLICT", findingIDs(findings))
	}
}

// The rule of the package comment, at the level where this adapter could break
// it: Files is the one field that puts a path into the resolver's map of files
// a package owns outright, and this adapter must never fill it. What it returns
// is a root -- an offer to name files that something else has already reached.
func TestTheAdapterNamesNoFileOfItsOwn(t *testing.T) {
	source := t.TempDir()
	writeIDFLock(t, source,
		registryEntry("espressif/led_strip", "2.5.3"),
		registryEntry("espressif/mdns", "1.2.0"),
	)
	writeManagedComponent(t, source, "espressif__led_strip", ledStripManifest)
	writeManagedComponent(t, source, "espressif__mdns", "version: 1.2.0\nlicense: Apache-2.0\n")
	// Files of the components that no compiler ever read.
	writeIDFFile(t, filepath.Join(source, idfManagedDir, "espressif__led_strip", "led_strip.c"), "int led;\n")
	writeIDFFile(t, filepath.Join(source, idfManagedDir, "espressif__mdns", "include", "mdns.h"), "#define MDNS 1\n")

	packages, _ := idfPackages(t, source)
	if len(packages) != 2 {
		t.Fatalf("packages = %#v", packages)
	}
	for _, found := range packages {
		if len(found.Files) != 0 {
			t.Errorf("%s claims the files %v; an adapter may never put a file into the used set",
				found.Name, found.Files)
		}
		// One root per component, and it is the directory the manager filled.
		if len(found.Roots) != 1 {
			t.Errorf("%s has the roots %v, want the one directory it was unpacked into", found.Name, found.Roots)
		}
	}
}

func TestTheIDFPURLEscapesEachSegmentOnItsOwn(t *testing.T) {
	if got := idfPURL("espressif", "led_strip", "2.5.3"); got != "pkg:idf/espressif/led_strip@2.5.3" {
		t.Errorf("purl = %q", got)
	}
	// The slash between namespace and name is structure, not a character in a
	// name, so it must never become %2F.
	if got := idfPURL("es pressif", "led strip", "1.0 rc1"); got != "pkg:idf/es%20pressif/led%20strip@1.0%20rc1" {
		t.Errorf("purl = %q, want each segment escaped on its own", got)
	}
	if got := idfPURL("", "cbor", "1.0"); got != "pkg:idf/cbor@1.0" {
		t.Errorf("purl = %q, want no namespace invented", got)
	}
	if got := idfPURL("", "", "1.0"); got != "" {
		t.Errorf("purl = %q, want none without a name", got)
	}
}
