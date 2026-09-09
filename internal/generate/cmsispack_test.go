package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/pathmodel"
)

// The tests in this file run the whole pipeline, because the question they ask
// is not what the reader took out of a descriptor but what reading one does to
// the document this tool writes -- and for a CMSIS pack the interesting half is
// what stays the same.

// packDescriptorFile is a pack descriptor as an MCU vendor ships it. The
// second release and the licence path are part of the fixture on purpose: the
// first is what must not be published as the version, the second is what must
// not be published as a licence expression.
const packDescriptorFile = `<?xml version="1.0" encoding="UTF-8"?>
<package schemaVersion="1.7.7">
  <vendor>ARM</vendor>
  <name>CMSIS</name>
  <description>CMSIS (Common Microcontroller Software Interface Standard)</description>
  <license>LICENSE.txt</license>
  <releases>
    <release version="5.9.0" date="2022-05-02">Release notes for 5.9.0</release>
    <release version="5.8.0" date="2021-06-24">Release notes for 5.8.0</release>
  </releases>
</package>
`

// A vendor pack copied into the tree is found by the licence file beside it
// and carries nothing but its directory name. The descriptor lying in the same
// directory holds the version and the vendor, and this is what reading it is
// worth: the component gains two fields and the run gains nothing else.
func TestAPackDescriptorDescribesTheComponentItLiesIn(t *testing.T) {
	root := t.TempDir()
	packRoot := filepath.Join(root, "dep", "cmsis")
	header := filepath.Join(packRoot, "Include", "core_cm4.h")
	writeTreeFile(t, header, "#define __CORE_CM4_H 1\n")
	writeTreeFile(t, filepath.Join(packRoot, "LICENSE"), "SPDX-License-Identifier: Apache-2.0\n")
	cfg, buildDir := makeBuildTree(t, root, header)

	bare, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	writeTreeFile(t, filepath.Join(packRoot, "ARM.CMSIS.pdsc"), packDescriptorFile)

	described, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}

	// Without the descriptor the pack carries its directory name and nothing
	// else; if it already had a version there would be nothing to show here.
	if before, ok := componentByID(bare.Document, "component:cmsis"); !ok || before.Version != "" {
		t.Fatalf("the pack was already described as %#v before the descriptor was written", before)
	}
	component, ok := componentByID(described.Document, "component:cmsis")
	if !ok {
		t.Fatalf("the pack the header belongs to reached no component; components are %v",
			componentIDs(described.Document))
	}
	if component.Version != "5.9.0" || component.VersionSource != "cmsis-pack" {
		t.Errorf("version = %q from %q, want the newest release and the descriptor as its origin",
			component.Version, component.VersionSource)
	}
	if component.Supplier != "ARM" {
		t.Errorf("supplier = %q, want the vendor the descriptor states", component.Supplier)
	}
	// The licence element names a file, so the licence still comes from the
	// file the root already carries, and no purl is invented for a pack.
	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "Apache-2.0" {
		t.Errorf("license = %#v, want the one the licence file in the root states", component.Licenses)
	}
	if component.PURL != "" {
		t.Errorf("purl = %q, want none: no purl type is registered for CMSIS packs", component.PURL)
	}
	if len(findingsWithID(described.Findings, "UNKNOWN_PURL")) == 0 {
		t.Error("a component described only by a pack descriptor lost its UNKNOWN_PURL")
	}
	// Everything else: the used set and the components are the same, down to
	// their order. A descriptor describes; it adds no file and moves no
	// boundary.
	before, after := fileRefs(bare.Document), fileRefs(described.Document)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("the used files changed:\nbefore %v\nafter  %v", before, after)
	}
	for _, ref := range after {
		if strings.Contains(ref, ".pdsc") {
			t.Errorf("files = %v; the descriptor read itself into the used set", after)
		}
	}
	if got, want := componentIDs(described.Document), componentIDs(bare.Document); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("components = %v, want the %v of a run without the descriptor", got, want)
	}
}

// Describing a pack does not link it. A whole pack installed in the tree that
// nothing in the build ever used stays out of the document, and it is reported
// by nothing: PACKAGE_NOT_LINKED belongs to a manager that installed a
// dependency for this build, and this reader installs and enumerates nothing.
func TestAPackNothingLinkedIsAbsentAndUnreported(t *testing.T) {
	root := t.TempDir()
	packRoot := filepath.Join(root, "dep", "cmsis")
	writeTreeFile(t, filepath.Join(packRoot, "Include", "core_cm4.h"), "#define __CORE_CM4_H 1\n")
	writeTreeFile(t, filepath.Join(packRoot, "LICENSE"), "SPDX-License-Identifier: Apache-2.0\n")
	writeTreeFile(t, filepath.Join(packRoot, "ARM.CMSIS.pdsc"), packDescriptorFile)
	cfg, buildDir := makeBuildTree(t, root)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := componentByID(result.Document, "component:cmsis"); ok {
		t.Error("a pack nothing linked reached a component of its own")
	}
	for _, finding := range result.Findings {
		if finding.ID == "PACKAGE_NOT_LINKED" {
			t.Errorf("PACKAGE_NOT_LINKED came out of a pack descriptor: %s", finding.Subject.Ref)
		}
		if strings.Contains(finding.Subject.Ref, ".pdsc") {
			t.Errorf("a descriptor nothing reached was read: %s %s", finding.ID, finding.Subject.Ref)
		}
	}
	for _, ref := range fileRefs(result.Document) {
		if strings.Contains(ref, "cmsis") {
			t.Errorf("files = %v; an unlinked pack is in the document", fileRefs(result.Document))
		}
	}
}
