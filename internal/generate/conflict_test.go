package generate

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/sbomwriter"
)

// The tests in this file run the whole pipeline, because the question they ask
// is not whether a conflict is found but what reporting one does to the
// document. A resolver test can state that two packages claimed one file; only
// a run can show that saying so out loud left every component, every file and
// every relation where it was.

// writeVcpkgPort writes what vcpkg leaves in a triplet tree for one port: the
// SPDX document that proves the port, its copyright file, and the list of the
// files it installed. Nothing is executed -- the files vcpkg wrote are the
// evidence (section 9.1), so the test writes exactly those.
func writeVcpkgPort(t *testing.T, buildDir, port, version string, entries []string) {
	t.Helper()
	share := filepath.Join(buildDir, "vcpkg_installed", "x64-linux", "share", port)
	writeTreeFile(t, filepath.Join(share, "vcpkg.spdx.json"),
		`{"packages":[{"name":"`+port+`","versionInfo":"`+version+`","licenseConcluded":"MIT",`+
			`"externalRefs":[{"referenceCategory":"PACKAGE-MANAGER","referenceType":"purl",`+
			`"referenceLocator":"pkg:vcpkg/`+port+`@`+version+`?triplet=x64-linux"}]}]}`)
	writeTreeFile(t, filepath.Join(share, "copyright"), "MIT\n")
	writeTreeFile(t, filepath.Join(buildDir, "vcpkg_installed", "vcpkg", "info",
		port+"_"+version+"_x64-linux.list"), strings.Join(entries, "\n")+"\n")
}

// fileRefs lists the identities the document carries, which is the used set as
// a reader of the document sees it.
func fileRefs(document *sbomwriter.Document) []string {
	refs := make([]string, 0, len(document.Files))
	for _, file := range document.Files {
		refs = append(refs, file.ID.Canonical())
	}
	sort.Strings(refs)
	return refs
}

// The rule at the head of internal/adapters/pkgmanager: an adapter improves
// what is known about a file the evidence chain already reached, and never puts
// a file into the used set. Reporting a disagreement must not become a way
// around it.
//
// Two ports claim one used header, and one of them also lists a header nothing
// ever included. The run has to end with the same files it would have had
// without the second port: a finding more, not a file more.
func TestReportingAContestedClaimAddsAFindingAndNotAFile(t *testing.T) {
	build := func(t *testing.T, contested bool) Result {
		t.Helper()
		root := t.TempDir()
		buildDir := filepath.Join(root, "build")
		installed := filepath.Join(buildDir, "vcpkg_installed", "x64-linux")
		header := filepath.Join(installed, "include", "shared.h")
		writeTreeFile(t, header, "#define SHARED 1\n")
		// A header the port installed and no translation unit ever read. A
		// package manager knows about it, because it is in the list; the
		// evidence chain never reached it, because it is in no dependency file.
		writeTreeFile(t, filepath.Join(installed, "include", "never-used.h"), "#define UNUSED 1\n")
		writeVcpkgPort(t, buildDir, "tinyfmt", "2.1.0", []string{
			"x64-linux/include/shared.h",
			"x64-linux/include/never-used.h",
		})
		if contested {
			writeVcpkgPort(t, buildDir, "tinylog", "1.4.0", []string{"x64-linux/include/shared.h"})
		}
		cfg, buildDir := makeBuildTree(t, root, header)
		result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	alone := build(t, false)
	disputed := build(t, true)

	// The disagreement is spoken, and the message names both ports, so that a
	// reviewer can go and look at the two of them.
	conflicts := findingsWithID(disputed.Findings, "COMPONENT_MAPPING_CONFLICT")
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want exactly one", conflicts)
	}
	if conflicts[0].Severity != domain.SeverityInfo {
		t.Errorf("severity = %s, want info: neither answer is wrong, there are two", conflicts[0].Severity)
	}
	if conflicts[0].Subject.Ref != "build:vcpkg_installed/x64-linux/include/shared.h" {
		t.Errorf("subject = %+v, want the contested header", conflicts[0].Subject)
	}
	for _, want := range []string{"tinyfmt", "tinylog"} {
		if !strings.Contains(conflicts[0].Message, want) {
			t.Errorf("message %q does not name %q", conflicts[0].Message, want)
		}
	}
	if len(findingsWithID(alone.Findings, "COMPONENT_MAPPING_CONFLICT")) != 0 {
		t.Error("one port claiming its own file is no disagreement")
	}

	// The used set is the same set. The second port changed who is said to own
	// the header, never which files the document has.
	before, after := fileRefs(alone.Document), fileRefs(disputed.Document)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("the files changed with the conflict:\n%v\n%v", before, after)
	}
	for _, refs := range [][]string{before, after} {
		for _, ref := range refs {
			if strings.Contains(ref, "never-used.h") {
				t.Errorf("files = %v; a header no translation unit read is in the document", refs)
			}
		}
	}
	// The resolution is untouched: the contested header keeps falling through
	// to the strategies below package metadata, which here is the project.
	header := "build:vcpkg_installed/x64-linux/include/shared.h"
	if owner := ownerOf(disputed.Document, header); owner != "component:project" {
		t.Errorf("the contested header belongs to %q, want component:project", owner)
	}
	if owner := ownerOf(alone.Document, header); owner != "component:tinyfmt" {
		t.Errorf("uncontested, the header belongs to %q, want component:tinyfmt", owner)
	}
}

// A list that breached a bound of section 30 was refused whole, so the port it
// belonged to said nothing about any file. Nothing is not a second answer, and
// a report of a disagreement with it would invent the opponent.
func TestARefusedFileListIsNoSideOfAConflict(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	installed := filepath.Join(buildDir, "vcpkg_installed", "x64-linux")
	header := filepath.Join(installed, "include", "shared.h")
	writeTreeFile(t, header, "#define SHARED 1\n")
	writeVcpkgPort(t, buildDir, "tinyfmt", "2.1.0", []string{"x64-linux/include/shared.h"})
	// One entry past the bound of section 30, and the whole list is refused.
	entries := make([]string, 0, 100_001)
	for i := 0; i <= 100_000; i++ {
		entries = append(entries, "x64-linux/include/shared.h"+strconv.Itoa(i))
	}
	writeVcpkgPort(t, buildDir, "tinylog", "1.4.0", entries)
	cfg, buildDir := makeBuildTree(t, root, header)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if limits := findingsWithID(result.Findings, "INPUT_LIMIT_EXCEEDED"); len(limits) != 1 {
		t.Fatalf("findings = %+v, want the refused list reported once", limits)
	}
	if conflicts := findingsWithID(result.Findings, "COMPONENT_MAPPING_CONFLICT"); len(conflicts) != 0 {
		t.Errorf("conflicts = %+v, want none: an unread list claims nothing", conflicts)
	}
	// The port that did prove its claim keeps the header, exactly as it would
	// have without the unreadable list beside it.
	if owner := ownerOf(result.Document, "build:vcpkg_installed/x64-linux/include/shared.h"); owner != "component:tinyfmt" {
		t.Errorf("the header belongs to %q, want component:tinyfmt", owner)
	}
}

// A port whose files nothing ever included is absent from the document, and
// that absence has its own report already. It is not a disagreement: nobody
// contradicted the port, the build simply never used it.
func TestAnInstalledButUnlinkedPortIsNoConflict(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	installed := filepath.Join(buildDir, "vcpkg_installed", "x64-linux")
	writeTreeFile(t, filepath.Join(installed, "include", "tinyfmt.h"), "#define TINYFMT 1\n")
	writeVcpkgPort(t, buildDir, "tinyfmt", "2.1.0", []string{"x64-linux/include/tinyfmt.h"})
	cfg, buildDir := makeBuildTree(t, root)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	notLinked := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED")
	if len(notLinked) != 1 || notLinked[0].Subject.Ref != "tinyfmt" {
		t.Fatalf("findings = %+v, want one PACKAGE_NOT_LINKED naming tinyfmt", notLinked)
	}
	if conflicts := findingsWithID(result.Findings, "COMPONENT_MAPPING_CONFLICT"); len(conflicts) != 0 {
		t.Errorf("conflicts = %+v, want none", conflicts)
	}
	if _, ok := componentByID(result.Document, "component:tinyfmt"); ok {
		t.Error("a port nothing linked reached a component of its own")
	}
	for _, ref := range fileRefs(result.Document) {
		if strings.Contains(ref, "tinyfmt.h") {
			t.Errorf("files = %v; a header nothing included is in the document", fileRefs(result.Document))
		}
	}
}

// A build directory missing the evidence a conflict would be found in reports
// what is missing and invents no dispute. This is where a report about two
// answers is most tempting and most wrong: there are none.
func TestAnAbsentAndABrokenPackageEvidenceReportNoConflict(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		write func(t *testing.T, buildDir string)
	}{
		{
			name:  "no package evidence at all",
			write: func(*testing.T, string) {},
		},
		{
			name: "an SPDX document that is not JSON",
			write: func(t *testing.T, buildDir string) {
				writeTreeFile(t, filepath.Join(buildDir, "vcpkg_installed", "x64-linux",
					"share", "tinyfmt", "vcpkg.spdx.json"), "{ this is not JSON")
			},
		},
		{
			name: "a file list for a port that proved nothing",
			write: func(t *testing.T, buildDir string) {
				writeTreeFile(t, filepath.Join(buildDir, "vcpkg_installed", "vcpkg", "info",
					"tinyfmt_2.1.0_x64-linux.list"), "x64-linux/include/shared.h\n")
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			buildDir := filepath.Join(root, "build")
			header := filepath.Join(buildDir, "vcpkg_installed", "x64-linux", "include", "shared.h")
			writeTreeFile(t, header, "#define SHARED 1\n")
			testCase.write(t, buildDir)
			cfg, buildDir := makeBuildTree(t, root, header)

			result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
			if err != nil {
				t.Fatal(err)
			}
			if conflicts := findingsWithID(result.Findings, "COMPONENT_MAPPING_CONFLICT"); len(conflicts) != 0 {
				t.Errorf("conflicts = %+v, want none: there was no second answer", conflicts)
			}
			// The header the compiler read is in the document either way; what
			// is missing is only the name of whoever installed it.
			if owner := ownerOf(result.Document, "build:vcpkg_installed/x64-linux/include/shared.h"); owner != "component:project" {
				t.Errorf("the header belongs to %q, want component:project", owner)
			}
		})
	}
}

// Two runs over one build produce the same findings byte for byte (section
// 26.2). The conflict reports are built from maps, whose order Go deliberately
// varies between runs, so this is the test that catches a forgotten sort.
func TestConflictFindingsAreTheSameOnEveryRun(t *testing.T) {
	run := func(t *testing.T) []string {
		t.Helper()
		root := t.TempDir()
		buildDir := filepath.Join(root, "build")
		installed := filepath.Join(buildDir, "vcpkg_installed", "x64-linux")
		first := filepath.Join(installed, "include", "one.h")
		second := filepath.Join(installed, "include", "two.h")
		writeTreeFile(t, first, "#define ONE 1\n")
		writeTreeFile(t, second, "#define TWO 1\n")
		for _, port := range []struct{ name, version string }{
			{"tinyfmt", "2.1.0"}, {"tinylog", "1.4.0"}, {"tinycbor", "0.6.0"},
		} {
			writeVcpkgPort(t, buildDir, port.name, port.version, []string{
				"x64-linux/include/one.h", "x64-linux/include/two.h",
			})
		}
		cfg, buildDir := makeBuildTree(t, root, first, second)
		result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
		if err != nil {
			t.Fatal(err)
		}
		messages := make([]string, 0, 2)
		for _, finding := range findingsWithID(result.Findings, "COMPONENT_MAPPING_CONFLICT") {
			messages = append(messages, finding.Subject.Ref+" "+finding.Message)
		}
		return messages
	}

	first, second := run(t), run(t)
	if len(first) != 2 {
		t.Fatalf("conflicts = %v, want one per contested header", first)
	}
	if strings.Join(first, "\n") != strings.Join(second, "\n") {
		t.Errorf("two runs disagree:\n%s\n%s", strings.Join(first, "\n"), strings.Join(second, "\n"))
	}
	// Every port that claimed the header is a side, and each is named once.
	for _, port := range []string{"tinyfmt", "tinylog", "tinycbor"} {
		if got := strings.Count(first[0], `"`+port+`"`); got != 1 {
			t.Errorf("port %s named %d times in %q, want once", port, got, first[0])
		}
	}
}
