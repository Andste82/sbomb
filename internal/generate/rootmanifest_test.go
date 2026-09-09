package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/pathmodel"
)

// The tests in this file run the whole pipeline, because the question they ask
// is not what a reader took out of an xmake.lua but what reading one does to
// the document -- and specifically whether it does anything to the *shape* of
// it. The answer has to be no: a manifest beside a component describes it and
// moves nothing.

// A library copied into the tree is found by the licence file beside it
// (section 19.2 strategy 6) and carries nothing but its directory name. This
// is the case the whole enricher mechanism exists for, and until now nothing
// but a bundled SBOM or a CMSIS descriptor could answer it. An xmake.lua lying
// in the same directory is a version, and this shows it arriving -- with
// nothing else about the run changing.
func TestAnXmakeDescriptionGivesACopiedInLibraryItsFirstVersion(t *testing.T) {
	root := t.TempDir()
	libraryRoot := filepath.Join(root, "dep", "foo")
	header := filepath.Join(libraryRoot, "include", "foo.h")
	writeTreeFile(t, header, "#define FOO 1\n")
	writeTreeFile(t, filepath.Join(libraryRoot, "LICENSE"), "SPDX-License-Identifier: MIT\n")
	cfg, buildDir := makeBuildTree(t, root, header)

	bare, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	writeTreeFile(t, filepath.Join(libraryRoot, "xmake.lua"),
		"set_project(\"foo\")\nset_version(\"2.4.0\")\n\ntarget(\"foo\")\n    set_kind(\"headeronly\")\n")

	described, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}

	// Without the description the library has its directory name and nothing
	// else; if it already had a version there would be nothing to show here.
	if before, ok := componentByID(bare.Document, "component:foo"); !ok || before.Version != "" {
		t.Fatalf("the library was already described as %#v before the xmake.lua was written", before)
	}
	component, ok := componentByID(described.Document, "component:foo")
	if !ok {
		t.Fatalf("the library the header belongs to reached no component; components are %v",
			componentIDs(described.Document))
	}
	if component.Version != "2.4.0" || component.VersionSource != "xmake" {
		t.Errorf("version = %q from %q, want the set_version line and xmake as its origin",
			component.Version, component.VersionSource)
	}
	// The licence still comes from the file that drew the boundary, and no
	// purl is invented out of a build description.
	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "MIT" {
		t.Errorf("license = %#v, want the one the licence file in the root states", component.Licenses)
	}
	if component.PURL != "" {
		t.Errorf("purl = %q, want none: an xmake.lua names no package ecosystem", component.PURL)
	}
	if len(findingsWithID(described.Findings, "UNKNOWN_PURL")) == 0 {
		t.Error("a component described only by an xmake.lua lost its UNKNOWN_PURL")
	}
	// Everything else is the same, down to the order: the used files, the
	// components and their identities. A manifest describes; it adds no file
	// and moves no boundary.
	before, after := fileRefs(bare.Document), fileRefs(described.Document)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("the used files changed:\nbefore %v\nafter  %v", before, after)
	}
	for _, ref := range after {
		if strings.Contains(ref, "xmake.lua") {
			t.Errorf("files = %v; the description read itself into the used set", after)
		}
	}
	if got, want := componentIDs(described.Document), componentIDs(bare.Document); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("components = %v, want the %v of a run without the description", got, want)
	}
}

// The counter-test, and the reason the one above needs a licence file at all:
// none of these manifests is a boundary marker. A library with an xmake.lua and
// nothing else gets no component of its own, because the marker lists of
// section 19.2 were not touched -- widening them moves component boundaries and
// costs a stat per ancestor directory per used file, which is a change of its
// own and not a side effect of adding a reader.
func TestAManifestAloneDoesNotMakeAComponent(t *testing.T) {
	root := t.TempDir()
	libraryRoot := filepath.Join(root, "dep", "foo")
	header := filepath.Join(libraryRoot, "include", "foo.h")
	writeTreeFile(t, header, "#define FOO 1\n")
	for name, content := range map[string]string{
		"xmake.lua":              "set_version(\"2.4.0\")\n",
		"MODULE.bazel":           "module(name = \"foo\", version = \"2.4.0\")\n",
		"manifest":               ": 1\nname: foo\nversion: 2.4.0\n",
		"fooConfigVersion.cmake": "set(PACKAGE_VERSION \"2.4.0\")\n",
	} {
		writeTreeFile(t, filepath.Join(libraryRoot, name), content)
	}
	cfg, buildDir := makeBuildTree(t, root, header)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := componentByID(result.Document, "component:foo"); ok {
		t.Errorf("a manifest drew a component boundary; components are %v", componentIDs(result.Document))
	}
	for _, ref := range fileRefs(result.Document) {
		for _, name := range []string{"xmake.lua", "MODULE.bazel", "ConfigVersion.cmake"} {
			if strings.Contains(ref, name) {
				t.Errorf("files = %v; a manifest read itself into the used set", fileRefs(result.Document))
			}
		}
	}
}

// A Meson subproject the build really linked is a component of its own, named
// and versioned by the wrap. This is the adapter half of the same work, and it
// is here rather than in the unit tests because only a run shows that the files
// the compiler read arrive in the component the wrap named.
func TestAMesonSubprojectIsNamedAndVersionedByItsWrap(t *testing.T) {
	root := t.TempDir()
	subproject := filepath.Join(root, "subprojects", "zlib")
	header := filepath.Join(subproject, "zlib.h")
	writeTreeFile(t, header, "#define ZLIB_VERSION \"1.3.1\"\n")
	writeTreeFile(t, filepath.Join(root, "subprojects", "zlib.wrap"),
		"[wrap-git]\nurl = https://example.invalid/org/zlib.git\nrevision = v1.3.1\n")
	cfg, buildDir := makeBuildTree(t, root, header)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}

	component, ok := componentByID(result.Document, "component:zlib")
	if !ok {
		t.Fatalf("the subproject the header belongs to reached no component; components are %v",
			componentIDs(result.Document))
	}
	if component.Version != "1.3.1" || component.VersionSource != "meson" {
		t.Errorf("version = %q from %q, want the wrap's revision and meson as its origin",
			component.Version, component.VersionSource)
	}
	for _, ref := range fileRefs(result.Document) {
		if strings.Contains(ref, ".wrap") {
			t.Errorf("files = %v; the wrap read itself into the used set", fileRefs(result.Document))
		}
	}
}

// A subproject that was unpacked and never linked is not part of the product,
// so it is not in the document -- and its absence is reported rather than
// passing in silence, because "the SBOM does not list the subproject I
// vendored" has to be answerable from the findings.
func TestAMesonSubprojectNothingLinkedIsAbsentAndReported(t *testing.T) {
	root := t.TempDir()
	writeTreeFile(t, filepath.Join(root, "subprojects", "zlib", "zlib.h"), "#define ZLIB 1\n")
	writeTreeFile(t, filepath.Join(root, "subprojects", "zlib.wrap"),
		"[wrap-git]\nurl = https://example.invalid/org/zlib.git\nrevision = v1.3.1\n")
	cfg, buildDir := makeBuildTree(t, root)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := componentByID(result.Document, "component:zlib"); ok {
		t.Error("a subproject nothing linked reached a component of its own")
	}
	unlinked := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED")
	if len(unlinked) != 1 || unlinked[0].Subject.Ref != "zlib" {
		t.Fatalf("findings = %#v, want one PACKAGE_NOT_LINKED naming the subproject", unlinked)
	}
}

// The same proof for the other three readers, in one table, because what has
// to hold is the same for all four: the version arrives under the origin that
// read it, and nothing else about the run moves. Only xmake gets a case of its
// own above, and only because it is the example the whole strategy-6 argument
// is written around.
func TestEveryRootManifestDescribesACopiedInLibraryAndMovesNothing(t *testing.T) {
	for _, tc := range []struct {
		file    string
		content string
		version string
		source  string
	}{
		{
			file:    "manifest",
			content: ": 1\nname: foo\nversion: 3.2.1\nsummary: a copied-in library\n",
			version: "3.2.1",
			source:  "build2",
		},
		{
			file:    "MODULE.bazel",
			content: "module(\n    name = \"foo\",\n    version = \"9.9.9\",\n)\n\nbazel_dep(name = \"rules_cc\", version = \"0.0.9\")\n",
			version: "9.9.9",
			source:  "bazel",
		},
		{
			file:    "fooConfigVersion.cmake",
			content: "set(PACKAGE_VERSION \"7.1\")\nif(PACKAGE_VERSION VERSION_LESS PACKAGE_FIND_VERSION)\nendif()\n",
			version: "7.1",
			source:  "cmake-config-version",
		},
	} {
		root := t.TempDir()
		libraryRoot := filepath.Join(root, "dep", "foo")
		header := filepath.Join(libraryRoot, "include", "foo.h")
		writeTreeFile(t, header, "#define FOO 1\n")
		writeTreeFile(t, filepath.Join(libraryRoot, "LICENSE"), "SPDX-License-Identifier: MIT\n")
		cfg, buildDir := makeBuildTree(t, root, header)

		bare, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
		if err != nil {
			t.Fatal(err)
		}
		if before, ok := componentByID(bare.Document, "component:foo"); !ok || before.Version != "" {
			t.Fatalf("%s: the library was already described as %#v", tc.file, before)
		}
		writeTreeFile(t, filepath.Join(libraryRoot, tc.file), tc.content)

		described, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
		if err != nil {
			t.Fatal(err)
		}

		component, ok := componentByID(described.Document, "component:foo")
		if !ok {
			t.Fatalf("%s: the library reached no component; components are %v",
				tc.file, componentIDs(described.Document))
		}
		if component.Version != tc.version || component.VersionSource != tc.source {
			t.Errorf("%s: version = %q from %q, want %q from %q",
				tc.file, component.Version, component.VersionSource, tc.version, tc.source)
		}
		// The licence still comes from the file that drew the boundary, so
		// reading a manifest replaced nothing that was already known.
		if len(component.Licenses) == 0 || component.Licenses[0].Expression != "MIT" {
			t.Errorf("%s: license = %#v, want the licence file's own", tc.file, component.Licenses)
		}
		// The used files and the components are the same set in the same
		// order, and the manifest is not among the files: a reader describes,
		// and describing adds nothing to what the evidence chain reached.
		before, after := fileRefs(bare.Document), fileRefs(described.Document)
		if strings.Join(before, "\n") != strings.Join(after, "\n") {
			t.Errorf("%s: the used files changed:\nbefore %v\nafter  %v", tc.file, before, after)
		}
		for _, ref := range after {
			if strings.Contains(ref, tc.file) {
				t.Errorf("%s: files = %v; the manifest read itself into the used set", tc.file, after)
			}
		}
		if got, want := componentIDs(described.Document), componentIDs(bare.Document); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: components = %v, want the %v of a run without the manifest", tc.file, got, want)
		}
	}
}

// A manifest nobody can read leaves the component exactly as it was, and says
// so. This is the whole-or-nothing rule at the level a user sees it: an
// xmake.lua stating two versions publishes neither, and the run reports the
// file rather than picking one of them.
func TestAManifestThatCannotBeReadLeavesTheComponentUndescribed(t *testing.T) {
	root := t.TempDir()
	libraryRoot := filepath.Join(root, "dep", "foo")
	header := filepath.Join(libraryRoot, "include", "foo.h")
	writeTreeFile(t, header, "#define FOO 1\n")
	writeTreeFile(t, filepath.Join(libraryRoot, "LICENSE"), "SPDX-License-Identifier: MIT\n")
	writeTreeFile(t, filepath.Join(libraryRoot, "xmake.lua"),
		"if is_plat(\"windows\") then\n    set_version(\"1.0\")\nelse\n    set_version(\"2.0\")\nend\n")
	cfg, buildDir := makeBuildTree(t, root, header)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}

	component, ok := componentByID(result.Document, "component:foo")
	if !ok {
		t.Fatalf("the library reached no component; components are %v", componentIDs(result.Document))
	}
	if component.Version != "" {
		t.Errorf("version = %q, want none: the description states two and neither is the one",
			component.Version)
	}
	unreadable := findingsWithID(result.Findings, "EVIDENCE_UNREADABLE")
	if len(unreadable) != 1 || !strings.Contains(unreadable[0].Subject.Ref, "xmake.lua") {
		t.Fatalf("findings = %#v, want one EVIDENCE_UNREADABLE naming the description", unreadable)
	}
}
