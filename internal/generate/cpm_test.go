package generate

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/pathmodel"
)

// writeCPMLock writes the lock CPM.cmake leaves in the build directory, with
// the header it writes before any package has been added.
func writeCPMLock(t *testing.T, buildDir, blocks string) {
	t.Helper()
	writeTreeFile(t, filepath.Join(buildDir, "cpm-package-lock.cmake"),
		"# CPM Package Lock\n# This file should be committed to version control\n\n"+blocks)
}

// A header of a CPM dependency that the compiler really read must arrive in
// that dependency's component, carrying the version the lock recorded rather
// than the tag the populate script names.
func TestACPMDependencyReachesTheDocumentWithTheLockedVersion(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	header := filepath.Join(buildDir, "_deps", "tinylog-src", "tinylog.h")
	writeTreeFile(t, header, "#define TINYLOG 1\n")
	// The script says which revision was checked out; the lock says which
	// version that revision is.
	writeFetchContentPackage(t, buildDir, "tinylog", "v1.3.0")
	writeCPMLock(t, buildDir, "# tinylog\nCPMDeclarePackage(tinylog\n  NAME tinylog\n"+
		"  VERSION 1.2.0\n  GITHUB_REPOSITORY org/tinylog\n)\n")
	cfg, buildDir := makeBuildTree(t, root, header)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if owner := ownerOf(result.Document, "pkg:fetchcontent/tinylog:tinylog.h"); owner != "component:tinylog" {
		t.Fatalf("the header belongs to %q, want component:tinylog", owner)
	}
	component, ok := componentByID(result.Document, "component:tinylog")
	if !ok {
		t.Fatal("the dependency the header belongs to reached no component")
	}
	if component.Version != "1.2.0" {
		t.Errorf("version = %q, want the one the lock recorded", component.Version)
	}
	if component.VersionSource != "cpm" {
		t.Errorf("version source = %q, want the source string section 20.3 maps", component.VersionSource)
	}
	// The lock is the record of the manager that fetched the dependency.
	if component.DetectedBy != "cpm" {
		t.Errorf("detectedBy = %q, want the manager the project uses", component.DetectedBy)
	}
	wantPURL := "pkg:generic/tinylog@1.2.0?vcs_url=git%2Bhttps%3A%2F%2Fexample.invalid%2Forg%2Ftinylog"
	if component.PURL != wantPURL {
		t.Errorf("purl = %q, want the form of section 20.4 %q", component.PURL, wantPURL)
	}
	if reported := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED"); len(reported) != 0 {
		t.Errorf("findings = %+v, want none: a file of the dependency was used", reported)
	}

	// The version evidence must render as a manifest read out of a file, not as
	// the "other" every unmapped source falls to.
	for _, entry := range result.BOM.Components {
		if entry.PURL != wantPURL {
			continue
		}
		if entry.Evidence == nil || len(entry.Evidence.Identity) != 1 {
			t.Fatalf("evidence = %+v, want one identity for the version", entry.Evidence)
		}
		methods := entry.Evidence.Identity[0].Methods
		if len(methods) != 1 || methods[0].Technique != "manifest-analysis" || methods[0].Value != "cpm" {
			t.Fatalf("methods = %+v, want manifest-analysis with the exact source string", methods)
		}
	}
}

// A dependency CPM fetched that nothing links stays out of the document, and
// the finding that says so names CPM rather than the mechanism it drives.
func TestAnUnlinkedCPMDependencyIsReportedAgainstCPM(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	used := filepath.Join(root, "src", "own.h")
	writeTreeFile(t, used, "#define OWN 1\n")
	writeFetchContentPackage(t, buildDir, "tinylog", "v1.3.0")
	writeCPMLock(t, buildDir, "# tinylog\nCPMDeclarePackage(tinylog\n  NAME tinylog\n  VERSION 1.2.0\n)\n")
	cfg, buildDir := makeBuildTree(t, root, used)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := componentByID(result.Document, "component:tinylog"); ok {
		t.Error("a dependency no used file belongs to reached the document")
	}
	reported := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED")
	if len(reported) != 1 || reported[0].Subject.Ref != "tinylog" {
		t.Fatalf("findings = %+v, want one naming the dependency", reported)
	}
	if !strings.HasPrefix(reported[0].Message, "cpm ") {
		t.Errorf("message = %q, want the manager that fetched it named", reported[0].Message)
	}
}

// The rule this whole change is measured against: a manifest may improve what
// is known about a package the evidence chain already reached, and it may never
// enlarge what the document contains. The same tree is run twice, once with the
// lock and once without, and everything except the metadata has to match -- the
// files, the components they were grouped under, and the findings.
//
// The tree is laid out to make a failure visible rather than merely possible:
// the checkout holds a source no translation unit ever included, and the lock
// names a second package that has no directory under _deps at all. If reading a
// manifest could pull a file or a component in, one of those two would arrive.
func TestTheLockChangesWhatIsKnownAndNeverWhatIsIncluded(t *testing.T) {
	run := func(withLock bool) (files []string, components []string, findings []string) {
		t.Helper()
		root := t.TempDir()
		buildDir := filepath.Join(root, "build")
		header := filepath.Join(buildDir, "_deps", "tinylog-src", "tinylog.h")
		writeTreeFile(t, header, "#define TINYLOG 1\n")
		// A file of the dependency that nothing read. It lies inside the
		// package's own root, so only the evidence chain keeps it out.
		writeTreeFile(t, filepath.Join(buildDir, "_deps", "tinylog-src", "unread.c"),
			"int unread(void) { return 0; }\n")
		writeFetchContentPackage(t, buildDir, "tinylog", "v1.3.0")
		if withLock {
			writeCPMLock(t, buildDir, "# tinylog\nCPMDeclarePackage(tinylog\n  NAME tinylog\n"+
				"  VERSION 1.2.0\n)\n# ghost\nCPMDeclarePackage(ghost\n  NAME ghost\n"+
				"  VERSION 4.0.0\n  GITHUB_REPOSITORY org/ghost\n)\n")
		}
		cfg, buildDir := makeBuildTree(t, root, header)

		result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range result.Document.Files {
			files = append(files, file.ID.Canonical()+" -> "+file.ComponentID)
		}
		for _, component := range result.Document.Components {
			components = append(components, component.ID)
		}
		for _, finding := range result.Findings {
			// The two runs are two temporary trees, so a subject naming the
			// build directory names a different absolute path in each. Only
			// that difference is normalized away; a subject that names a
			// component or a file is compared as it stands, because inventing
			// one is exactly what this test is watching for.
			ref := strings.ReplaceAll(finding.Subject.Ref, root, "<tree>")
			findings = append(findings, finding.ID+" "+finding.Subject.Kind+" "+ref)
		}
		sort.Strings(files)
		sort.Strings(components)
		sort.Strings(findings)
		return files, components, findings
	}

	withoutFiles, withoutComponents, withoutFindings := run(false)
	withFiles, withComponents, withFindings := run(true)

	if !slices.Equal(withFiles, withoutFiles) {
		t.Errorf("files with the lock = %q, without it = %q: the lock may not move a file in or out",
			withFiles, withoutFiles)
	}
	if !slices.Equal(withComponents, withoutComponents) {
		t.Errorf("components with the lock = %q, without it = %q: the lock may not invent a component",
			withComponents, withoutComponents)
	}
	if !slices.Equal(withFindings, withoutFindings) {
		t.Errorf("findings with the lock = %q, without it = %q: a lock that was read cleanly reports nothing",
			withFindings, withoutFindings)
	}
	// The comparison would hold just as well if the lock had been ignored
	// altogether, so the test states that the runs it compared reached a file.
	if len(withFiles) == 0 {
		t.Fatal("no file reached the document, so the comparison proves nothing")
	}
}
