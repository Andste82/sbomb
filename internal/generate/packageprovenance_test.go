package generate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/sbomwriter"
)

// The tests in this file ask what the provenance of section 21.1 changed about
// the document, and the first answer has to be: nothing about which files are
// in it. Knowing better where a value came from may never turn into knowing
// more files.

// standInGitOnPath puts a program called git on PATH that answers the
// allowlisted questions of section 9.2 with fixed strings. Introspection is
// what makes two origins meet at one field, and a test of that meeting must
// not depend on the git a machine happens to carry -- the e2e suite drives a
// real one.
func standInGitOnPath(t *testing.T, described string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"# $1 $2 are -C <dir>; $3 is the subcommand.\n" +
		"case \"$3\" in\n" +
		"  describe) printf '%s\\n' '" + described + "' ;;\n" +
		"  *) exit 1 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// mentionsFile reports whether a file reached the document at all -- as a file
// entry or through a relation. A package that was never linked must appear in
// neither.
func mentionsFile(document *sbomwriter.Document, canonical string) bool {
	for _, file := range document.Files {
		if file.ID.Canonical() == canonical {
			return true
		}
	}
	for _, relation := range document.Relations {
		for _, ref := range relation.To {
			if ref == canonical {
				return true
			}
		}
	}
	return false
}

// A package that states its version, licence, supplier and purl outright is
// the best evidence any manager produces, and it still buys the package
// nothing: no used file belongs to it, so it is not part of the product. This
// is the rule the whole package hangs on -- an adapter improves what is known
// about files the evidence chain already reached, and never widens that reach.
func TestARichlyDescribedPackageStillReachesNoFileTheChainDidNot(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")

	// A vcpkg port with everything stated, its copyright file, its installed
	// header, and the list that attributes that header to it.
	installed := filepath.Join(buildDir, "vcpkg_installed", "x64-linux")
	writeTreeFile(t, filepath.Join(installed, "share", "tinyfmt", "vcpkg.spdx.json"),
		`{"packages":[{"name":"tinyfmt","versionInfo":"2.1.0","licenseDeclared":"MIT",`+
			`"supplier":"Organization: Example Org","externalRefs":[{"referenceCategory":"PACKAGE-MANAGER",`+
			`"referenceType":"purl","referenceLocator":"pkg:vcpkg/tinyfmt@2.1.0?triplet=x64-linux"}]}]}`)
	writeTreeFile(t, filepath.Join(installed, "share", "tinyfmt", "copyright"), "MIT\n")
	unusedHeader := filepath.Join(installed, "include", "tinyfmt.h")
	writeTreeFile(t, unusedHeader, "#define TINYFMT 1\n")
	writeTreeFile(t, filepath.Join(buildDir, "vcpkg_installed", "vcpkg", "info",
		"tinyfmt_2.1.0_x64-linux.list"), "x64-linux/\nx64-linux/include/\nx64-linux/include/tinyfmt.h\n")

	// A FetchContent dependency that was populated and never compiled against.
	writeFetchContentPackage(t, buildDir, "tinylog", "v1.4.0")
	writeTreeFile(t, filepath.Join(buildDir, "_deps", "tinylog-src", "log.c"), "int l(void){return 0;}\n")

	// The build itself reads none of it.
	cfg, buildDir := makeBuildTree(t, root)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"component:tinyfmt", "component:tinylog"} {
		if _, ok := componentByID(result.Document, id); ok {
			t.Errorf("%s is in the document although no used file belongs to it", id)
		}
	}
	for _, canonical := range []string{
		"build:vcpkg_installed/x64-linux/include/tinyfmt.h",
		"build:_deps/tinylog-src/log.c",
	} {
		if mentionsFile(result.Document, canonical) {
			t.Errorf("%s reached the document; a package may not add a file to the used set", canonical)
		}
	}
	// The packages are not silently dropped either: an installed dependency
	// that nothing linked is reported, once per package.
	notLinked := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED")
	if len(notLinked) != 2 {
		t.Fatalf("findings = %+v, want one PACKAGE_NOT_LINKED per installed package", notLinked)
	}
	for _, finding := range notLinked {
		if finding.Severity != "info" {
			t.Errorf("%s is %q; a dependency that was installed but not linked is not a defect",
				finding.Subject.Ref, finding.Severity)
		}
	}
}

// Where two origins describe one component the document publishes the one that
// won, and says so: the version, its source and the purl all name the checkout
// rather than the tag that was asked for, while detectedBy keeps naming the
// manager that mapped the files. The tag that lost is kept inside the package
// for a later report and must reach no part of the document.
func TestOnlyTheOriginThatWonIsPublished(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	writeFetchContentPackage(t, buildDir, "tinylog", "v1.2.0")
	header := filepath.Join(buildDir, "_deps", "tinylog-src", "tinylog.h")
	writeTreeFile(t, header, "#define TINYLOG 1\n")
	cfg, buildDir := makeBuildTree(t, root, header)
	// The project's own version must not come from the stand-in as well, or the
	// assertion below could not tell the two apart.
	cfg.Project.Version = "9.9.9"
	standInGitOnPath(t, "v1.3.0")

	result, err := RunWithOptions(cfg, buildDir, true, Options{
		PathFlavor:    pathmodel.PosixFlavor{},
		Introspection: exec.Features{Git: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	component, ok := componentByID(result.Document, "component:tinylog")
	if !ok {
		t.Fatal("the dependency whose header was used reached no component")
	}
	if component.Version != "1.3.0" || component.VersionSource != "git-describe" {
		t.Errorf("version = %q from %q, want the checkout's answer",
			component.Version, component.VersionSource)
	}
	if component.DetectedBy != "fetchcontent" {
		t.Errorf("detectedBy = %q, want the manager that mapped the files", component.DetectedBy)
	}
	if !strings.Contains(component.PURL, "@1.3.0") {
		t.Errorf("purl = %q, want the version that won in it", component.PURL)
	}
	// Nothing anywhere in the document may carry the displaced tag: the losing
	// contribution is kept for a report that does not exist yet, and until it
	// does it is invisible.
	if found := valuesMentioning(result.Document, "1.2.0"); len(found) != 0 {
		t.Errorf("the displaced tag reached the document as %v", found)
	}
}

// valuesMentioning collects every published field of every component that
// carries a value, so a test can state that a losing claim reached none of
// them -- the product and the artifacts included.
func valuesMentioning(document *sbomwriter.Document, value string) []string {
	components := []domain.Component{document.Product}
	components = append(components, document.Artifacts...)
	components = append(components, document.Components...)

	found := make([]string, 0)
	for _, component := range components {
		for label, field := range map[string]string{
			"version":       component.Version,
			"versionSource": component.VersionSource,
			"purl":          component.PURL,
			"cpe":           component.CPE,
		} {
			if field != "" && strings.Contains(field, value) {
				found = append(found, component.ID+" "+label+"="+field)
			}
		}
		for name, values := range component.Properties {
			for _, entry := range values {
				if strings.Contains(entry, value) {
					found = append(found, component.ID+" "+name+"="+entry)
				}
			}
		}
	}
	return found
}
