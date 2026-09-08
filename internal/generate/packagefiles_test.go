package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/sbomwriter"
)

// The tests in this file run the whole pipeline, because what they ask -- which
// component a used file ends up in -- is decided only after the evidence chain
// has reached that file. A resolver test can state that a package claims a
// path; only a run can show that the file the compiler really read arrives
// there.

// makeBuildTree lays out a Makefiles build tree with one translation unit that
// depends on the given headers. Nothing is executed: for a Makefiles generator
// the files CMake wrote are the evidence (section 9.1), so the test writes
// exactly those.
func makeBuildTree(t *testing.T, root string, headers ...string) (config.Config, string) {
	t.Helper()
	buildDir := filepath.Join(root, "build")
	targetDir := filepath.Join(buildDir, "CMakeFiles", "app.dir")
	source := filepath.Join(root, "src", "main.c")
	for _, dir := range []string{targetDir, filepath.Dir(source)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTreeFile(t, source, "int main(void) { return 0; }\n")

	depends := "CMakeFiles/app.dir/main.c.o: " + source
	for _, header := range headers {
		depends += " " + header
	}
	writeTreeFile(t, filepath.Join(targetDir, "build.make"), "CMakeFiles/app.dir/main.c.o: "+source+"\n")
	writeTreeFile(t, filepath.Join(targetDir, "link.txt"), "cc -o app CMakeFiles/app.dir/main.c.o\n")
	writeTreeFile(t, filepath.Join(targetDir, "compiler_depend.make"), depends+"\n")
	writeTreeFile(t, filepath.Join(buildDir, "app"), "binary\n")
	// A Makefile at the build root is what says this is a Makefiles build tree.
	writeTreeFile(t, filepath.Join(buildDir, "Makefile"), "all:\n")

	return config.Config{
		Project:   config.Project{Root: root},
		Artifacts: []config.Artifact{{Path: "app", Role: "application"}},
	}, buildDir
}

func writeTreeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ownerOf names the component a used file was grouped under, which the
// document expresses as the relation from the component to its files.
func ownerOf(document *sbomwriter.Document, file string) string {
	for _, relation := range document.Relations {
		for _, ref := range relation.To {
			if ref == file {
				return relation.From
			}
		}
	}
	return ""
}

func componentByID(document *sbomwriter.Document, id string) (domain.Component, bool) {
	for _, component := range document.Components {
		if component.ID == id {
			return component, true
		}
	}
	return domain.Component{}, false
}

// writeFetchContentPackage writes what CMake leaves behind for a FetchContent
// dependency: the script that names the repository and the tag, the checkout,
// and the tree CMake fills beside it.
func writeFetchContentPackage(t *testing.T, buildDir, name, tag string) {
	t.Helper()
	script := `
execute_process(
  COMMAND "/usr/bin/git" clone --no-checkout --config "advice.detachedHead=false" "https://example.invalid/org/` + name + `.git" "` + name + `-src"
  WORKING_DIRECTORY "/build/_deps"
)
execute_process(
  COMMAND "/usr/bin/git" checkout "` + tag + `" --
  WORKING_DIRECTORY "/build/_deps/` + name + `-src"
)
`
	writeTreeFile(t, filepath.Join(buildDir, "_deps", name+"-subbuild", name+"-populate-prefix",
		"tmp", name+"-populate-gitclone.cmake"), script)
	if err := os.MkdirAll(filepath.Join(buildDir, "_deps", name+"-src"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A header configure_file wrote exists only in the build tree CMake filled for
// the dependency, never in its checkout. While the checkout was the package's
// only root, such a header belonged to no package and was reported as the
// manufacturer's own code, which is what a component of scope project means.
func TestAGeneratedHeaderInTheBuildTreeReachesItsPackage(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	generated := filepath.Join(buildDir, "_deps", "tinylog-build", "tinylog_config.h")
	writeTreeFile(t, generated, "#define TINYLOG_LEVEL 2\n")
	writeFetchContentPackage(t, buildDir, "tinylog", "v1.4.0")
	cfg, buildDir := makeBuildTree(t, root, generated)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if owner := ownerOf(result.Document, "build:_deps/tinylog-build/tinylog_config.h"); owner != "component:tinylog" {
		t.Fatalf("the generated header belongs to %q, want component:tinylog", owner)
	}
	component, ok := componentByID(result.Document, "component:tinylog")
	if !ok {
		t.Fatal("the package the header belongs to reached no component")
	}
	if component.DetectedBy != "fetchcontent" || component.Version != "1.4.0" {
		t.Errorf("component = %+v, want the FetchContent dependency with its tag", component)
	}
	// The second root holds files of the package but is not where it begins.
	// The component still starts at the checkout -- the root the package
	// anchor was registered on, which is why it is named by that anchor -- so
	// the licence and the version keep being read from there and not from the
	// tree CMake wrote beside it.
	if got := component.Properties["sbomb:component:root"]; len(got) != 1 || got[0] != "pkg:fetchcontent/tinylog:" {
		t.Errorf("sbomb:component:root = %v, want the checkout [pkg:fetchcontent/tinylog:]", got)
	}
	if reported := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED"); len(reported) != 0 {
		t.Errorf("findings = %+v, want none: a file of the package was used", reported)
	}
}

// vcpkg merges every port into one triplet tree, so the include directory a
// header sits in says nothing about which port installed it. The list vcpkg
// wrote when it installed the port does, and with it the port's version, purl
// and licence reach the header instead of stopping at the package entry.
func TestAVcpkgHeaderReachesItsPortThroughTheInstalledFileList(t *testing.T) {
	for _, testCase := range []struct {
		name string
		list bool
	}{
		{name: "with the list vcpkg wrote", list: true},
		{name: "without it", list: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			buildDir := filepath.Join(root, "build")
			installed := filepath.Join(buildDir, "vcpkg_installed", "x64-linux")
			writeTreeFile(t, filepath.Join(installed, "share", "tinyfmt", "vcpkg.spdx.json"),
				`{"packages":[{"name":"tinyfmt","versionInfo":"2.1.0","licenseConcluded":"MIT",`+
					`"externalRefs":[{"referenceCategory":"PACKAGE-MANAGER","referenceType":"purl",`+
					`"referenceLocator":"pkg:vcpkg/tinyfmt@2.1.0?triplet=x64-linux"}]}]}`)
			writeTreeFile(t, filepath.Join(installed, "share", "tinyfmt", "copyright"), "MIT\n")
			header := filepath.Join(installed, "include", "tinyfmt.h")
			writeTreeFile(t, header, "#define TINYFMT 1\n")
			if testCase.list {
				writeTreeFile(t, filepath.Join(buildDir, "vcpkg_installed", "vcpkg", "info",
					"tinyfmt_2.1.0_x64-linux.list"),
					"x64-linux/\nx64-linux/include/\nx64-linux/include/tinyfmt.h\n")
			}
			cfg, buildDir := makeBuildTree(t, root, header)

			result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
			if err != nil {
				t.Fatal(err)
			}
			owner := ownerOf(result.Document, "build:vcpkg_installed/x64-linux/include/tinyfmt.h")
			notLinked := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED")
			if !testCase.list {
				// Without the list nothing about the document changes: the
				// header keeps falling through to the project, exactly as
				// before. What is new is that the port no longer disappears
				// without a word.
				if owner != "component:project" {
					t.Errorf("the header belongs to %q; with no list to look it up in it stays with the project", owner)
				}
				if len(notLinked) != 1 || notLinked[0].Subject.Ref != "tinyfmt" {
					t.Errorf("findings = %+v, want one PACKAGE_NOT_LINKED naming tinyfmt", notLinked)
				}
				return
			}
			if owner != "component:tinyfmt" {
				t.Fatalf("the header belongs to %q, want component:tinyfmt", owner)
			}
			component, ok := componentByID(result.Document, "component:tinyfmt")
			if !ok {
				t.Fatal("the port that installed the header reached no component")
			}
			if component.Version != "2.1.0" || component.PURL != "pkg:vcpkg/tinyfmt@2.1.0?triplet=x64-linux" {
				t.Errorf("component = %+v, want the version and purl vcpkg stated", component)
			}
			if component.Scope != string(anchors.ScopeThirdParty) || component.DetectedBy != "vcpkg" {
				t.Errorf("scope/detectedBy = %q/%q, want a third-party component from vcpkg",
					component.Scope, component.DetectedBy)
			}
			if len(component.Licenses) != 1 || component.Licenses[0].Expression != "MIT" {
				t.Errorf("licenses = %+v, want the MIT vcpkg declared", component.Licenses)
			}
			if len(notLinked) != 0 {
				t.Errorf("findings = %+v, want none: a file of the port was used", notLinked)
			}
		})
	}
}
