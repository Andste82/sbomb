package generate

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/pathmodel"
)

// writeWestWorkspace lays out what west leaves in a workspace: the config that
// names the manifest repository, the manifest itself, and the projects it
// declares. The application being built is the workspace root here, which is
// the one layout in which the runner would also be allowed to ask a project's
// checkout anything -- see the header of west.go for why that is not the usual
// one.
func writeWestWorkspace(t *testing.T, root string) {
	t.Helper()
	writeTreeFile(t, filepath.Join(root, ".west", "config"), "[manifest]\npath = zephyr\nfile = west.yml\n")
	writeTreeFile(t, filepath.Join(root, "zephyr", "west.yml"),
		"manifest:\n"+
			"  defaults:\n    remote: upstream\n"+
			"  remotes:\n    - name: upstream\n      url-base: https://example.invalid/zephyrproject-rtos\n"+
			"  projects:\n"+
			"    - name: hal_nordic\n      path: modules/hal/nordic\n      revision: v3.5.0\n"+
			"    - name: mcuboot\n      path: bootloader/mcuboot\n      revision: v1.10.0\n"+
			"  self:\n    path: zephyr\n")
	writeTreeFile(t, filepath.Join(root, "bootloader", "mcuboot", "boot.h"), "#define BOOT 1\n")
}

// A header of a west project that the compiler really read must arrive in that
// project's component -- with the revision the manifest asked for, the
// repository the remote names, and the purl of section 20.4.
func TestAWestProjectReachesTheDocument(t *testing.T) {
	root := t.TempDir()
	header := filepath.Join(root, "modules", "hal", "nordic", "hal.h")
	writeWestWorkspace(t, root)
	writeTreeFile(t, header, "#define HAL 1\n")
	cfg, buildDir := makeBuildTree(t, root, header)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if owner := ownerOf(result.Document, "pkg:west/hal_nordic:hal.h"); owner != "component:hal_nordic" {
		t.Fatalf("the header belongs to %q, want the west project", owner)
	}
	component, ok := componentByID(result.Document, "component:hal_nordic")
	if !ok {
		t.Fatal("the west project the header belongs to reached no component")
	}
	if component.Version != "3.5.0" {
		t.Errorf("version = %q, want the revision the manifest states, without the tag's v", component.Version)
	}
	want := "pkg:generic/hal_nordic@3.5.0?vcs_url=git%2Bhttps%3A%2F%2Fexample.invalid%2Fzephyrproject-rtos%2Fhal_nordic"
	if component.PURL != want {
		t.Errorf("purl = %q, want %q", component.PURL, want)
	}
	if component.DetectedBy != "west" {
		t.Errorf("detectedBy = %q, want the adapter that named the component", component.DetectedBy)
	}
	if component.VersionSource != "west" {
		t.Errorf("version source = %q, want the source string section 20.3 maps", component.VersionSource)
	}

	// The version evidence must render as a manifest read out of a file, not as
	// the "other" every unmapped source falls to.
	for _, entry := range result.BOM.Components {
		if entry.PURL != want {
			continue
		}
		if entry.Evidence == nil || len(entry.Evidence.Identity) != 1 {
			t.Fatalf("evidence = %+v, want one identity for the version", entry.Evidence)
		}
		methods := entry.Evidence.Identity[0].Methods
		if len(methods) != 1 || methods[0].Technique != "manifest-analysis" || methods[0].Value != "west" {
			t.Fatalf("methods = %+v, want manifest-analysis with the exact source string", methods)
		}
		return
	}
	t.Fatal("the west project did not reach the CycloneDX document")
}

// A project west cloned but nothing linked is not part of the product, so it is
// absent -- and its absence is said out loud rather than passing in silence.
func TestAWestProjectNothingLinkedIsAbsentAndReported(t *testing.T) {
	root := t.TempDir()
	header := filepath.Join(root, "modules", "hal", "nordic", "hal.h")
	writeWestWorkspace(t, root)
	writeTreeFile(t, header, "#define HAL 1\n")
	cfg, buildDir := makeBuildTree(t, root, header)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	// The build read the header of one project and never touched the other.
	if _, ok := componentByID(result.Document, "component:mcuboot"); ok {
		t.Error("a project nothing linked reached the document")
	}
	reported := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED")
	if len(reported) != 1 || reported[0].Subject.Ref != "mcuboot" {
		t.Fatalf("findings = %+v, want one PACKAGE_NOT_LINKED naming the unlinked project", reported)
	}
}

// The rule of internal/adapters/pkgmanager/pkgmanager.go, proven where it is
// visible: an adapter improves what is known about the files the evidence chain
// already reached, and never puts one there. Two runs over one workspace --
// alike but for the manifest west wrote -- must therefore carry exactly the
// same files.
func TestTheWestManifestAddsNoFileToTheDocument(t *testing.T) {
	filesOf := func(t *testing.T, withManifest bool) []string {
		t.Helper()
		root := t.TempDir()
		project := filepath.Join(root, "modules", "hal", "nordic")
		header := filepath.Join(project, "hal.h")
		writeTreeFile(t, header, "#define HAL 1\n")
		// Files of the project the compiler never read. They exist, west put
		// them there, and no evidence chain reaches them -- so the document
		// must not know about them either.
		writeTreeFile(t, filepath.Join(project, "hal.c"), "int hal;\n")
		writeTreeFile(t, filepath.Join(project, "include", "unused.h"), "#define UNUSED 1\n")
		writeTreeFile(t, filepath.Join(project, "LICENSE"), "Apache-2.0\n")
		if withManifest {
			writeWestWorkspace(t, root)
		}
		cfg, buildDir := makeBuildTree(t, root, header)

		result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
		if err != nil {
			t.Fatal(err)
		}
		// The anchor a file is expressed against does change once the project
		// is known -- that is the whole point of the adapter -- so the files
		// are compared by the name they carry on disk, which does not.
		names := make([]string, 0, len(result.Document.Files))
		for _, file := range result.Document.Files {
			names = append(names, filepath.Base(file.ID.RelPath))
		}
		sort.Strings(names)
		return names
	}

	without := filesOf(t, false)
	with := filesOf(t, true)
	if !reflect.DeepEqual(without, with) {
		t.Fatalf("the used set changed with the manifest:\nwithout %v\nwith    %v", without, with)
	}
	for _, name := range with {
		if name == "hal.c" || name == "unused.h" || name == "LICENSE" {
			t.Errorf("%q reached the document, but nothing linked it", name)
		}
	}
	if len(with) != 2 {
		t.Fatalf("files = %v, want exactly the source and the header the build read", with)
	}
}

// Section 20.2 point 5, end to end: a manifest that pins a project to a commit
// states no version, so the component carries none -- the SHA reaches the
// document as the commit inside the purl and as the vcsCommit property, and
// UNKNOWN_VERSION says out loud that there is no version rather than letting a
// hash stand in for one.
func TestACommitPinnedWestProjectPublishesNoVersion(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	root := t.TempDir()
	header := filepath.Join(root, "modules", "hal", "nordic", "hal.h")
	writeTreeFile(t, filepath.Join(root, ".west", "config"), "[manifest]\npath = zephyr\nfile = west.yml\n")
	writeTreeFile(t, filepath.Join(root, "zephyr", "west.yml"),
		"manifest:\n  projects:\n"+
			"    - name: hal_nordic\n      path: modules/hal/nordic\n"+
			"      url: https://example.invalid/zephyrproject-rtos/hal_nordic.git\n"+
			"      revision: "+commit+"\n")
	writeTreeFile(t, header, "#define HAL 1\n")
	cfg, buildDir := makeBuildTree(t, root, header)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	component, ok := componentByID(result.Document, "component:hal_nordic")
	if !ok {
		t.Fatal("the west project the header belongs to reached no component")
	}
	if component.Version != "" {
		t.Errorf("version = %q, want none: a commit is not a version", component.Version)
	}
	want := "pkg:generic/hal_nordic?vcs_url=git%2Bhttps%3A%2F%2Fexample.invalid%2Fzephyrproject-rtos%2Fhal_nordic%40" + commit
	if component.PURL != want {
		t.Errorf("purl = %q, want %q", component.PURL, want)
	}
	// The adapter says why there is no version, and the pipeline says that
	// nothing supplied one; both are about this project and both are honest.
	var explained bool
	for _, finding := range findingsWithID(result.Findings, "UNKNOWN_VERSION") {
		if finding.Subject.Ref == "hal_nordic" && strings.Contains(finding.Message, "commit") {
			explained = true
		}
	}
	if !explained {
		t.Fatalf("findings = %+v, want the commit-pinned project's missing version explained",
			findingsWithID(result.Findings, "UNKNOWN_VERSION"))
	}

	// The commit itself is published as evidence about the checkout, wherever
	// the spec version puts it -- on the component at 1.6, on the reference it
	// qualifies at 1.7.
	var stated bool
	for _, entry := range result.BOM.Components {
		if entry.PURL != want {
			continue
		}
		for _, property := range entry.Properties {
			if property.Name == "sbomb:component:vcsCommit" && property.Value == commit {
				stated = true
			}
		}
		for _, reference := range entry.ExternalReferences {
			for _, property := range reference.Properties {
				if property.Name == "sbomb:component:vcsCommit" && property.Value == commit {
					stated = true
				}
			}
		}
	}
	if !stated {
		t.Error("the commit the manifest pins reached the document nowhere")
	}
}
