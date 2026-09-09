package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/sbomwriter"
)

// The tests in this file run the whole pipeline, because the question they ask
// is not what a reader took out of a document but what reading one does to the
// document this tool writes. A unit test can show that four values arrived;
// only a run can show that the dependencies beside them did not.

// upstreamDocument is a CycloneDX SBOM as an upstream ships it inside its
// package: it describes itself in metadata.component and lists what it depends
// on in components[]. The dependencies are the half that must never reach a
// document of ours -- a file saying that zlib exists is no evidence that
// anything in this build linked it.
const upstreamDocument = `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "version": 1,
  "metadata": {
    "component": {
      "type": "library",
      "name": "tinylog",
      "version": "1.9.0-upstream",
      "purl": "pkg:github/example/tinylog@1.9.0",
      "supplier": {"name": "Example Upstream Ltd"},
      "licenses": [{"expression": "Apache-2.0"}]
    }
  },
  "components": [
    {"type": "library", "name": "zlib", "version": "1.3.1", "purl": "pkg:generic/zlib@1.3.1"},
    {"type": "library", "name": "openssl", "version": "3.0.13", "purl": "pkg:generic/openssl@3.0.13"}
  ]
}`

// The rule of this package, seen from the document: a dependency that ships
// its own SBOM is described better, and nothing else about the run moves. The
// files are the same files, the components are the same components, and the
// two libraries the document names are absent, because no link, no object and
// no header ever reached them.
func TestABundledDocumentDescribesAPackageAndAddsNothingToTheDocument(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	header := filepath.Join(buildDir, "_deps", "tinylog-src", "tinylog.h")
	writeTreeFile(t, header, "#define TINYLOG 1\n")
	writeFetchContentPackage(t, buildDir, "tinylog", "v1.4.0")
	cfg, buildDir := makeBuildTree(t, root, header)

	bare, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	writeTreeFile(t, filepath.Join(buildDir, "_deps", "tinylog-src", "sbom.cdx.json"), upstreamDocument)

	described, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}

	// The point of the change: the populate script could only say which tag
	// was asked for, the document says what the upstream shipped.
	component, ok := componentByID(described.Document, "component:tinylog")
	if !ok {
		t.Fatal("the package the header belongs to reached no component")
	}
	if component.Version != "1.9.0-upstream" || component.VersionSource != "bundled-sbom" {
		t.Errorf("version = %q from %q, want the document's answer and its origin",
			component.Version, component.VersionSource)
	}
	if component.Supplier != "Example Upstream Ltd" || component.PURL != "pkg:github/example/tinylog@1.9.0" {
		t.Errorf("supplier/purl = %q/%q, want the document's", component.Supplier, component.PURL)
	}
	// Everything else: the used set is the same set, down to its order.
	before, after := fileRefs(bare.Document), fileRefs(described.Document)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("the files changed with the document:\n%v\n%v", before, after)
	}
	for _, ref := range after {
		if strings.Contains(ref, "sbom.cdx.json") {
			t.Errorf("files = %v; the document read itself into the used set", after)
		}
	}
	if got, want := componentIDs(described.Document), componentIDs(bare.Document); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("components = %v, want the %v of a run without the document", got, want)
	}
	for _, dependency := range []string{"zlib", "openssl"} {
		if _, present := componentByID(described.Document, "component:"+dependency); present {
			t.Errorf("%s reached a component of its own; a document is no evidence that it was linked", dependency)
		}
	}
}

// Describing a package does not link it. A port installed in the tree with a
// flawless SBOM beside it, that nothing in the build ever used, stays out of
// the document and keeps the report that says so.
func TestAnUnlinkedPackageWithABundledSBOMStaysOutOfTheDocument(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	writeTreeFile(t, filepath.Join(buildDir, "vcpkg_installed", "x64-linux", "include", "tinyfmt.h"),
		"#define TINYFMT 1\n")
	writeVcpkgPort(t, buildDir, "tinyfmt", "2.1.0", []string{"x64-linux/include/tinyfmt.h"})
	// The document lies in the directory vcpkg gave the port, which is exactly
	// the root a reader is offered.
	writeTreeFile(t, filepath.Join(buildDir, "vcpkg_installed", "x64-linux", "share", "tinyfmt", "sbom.cdx.json"),
		strings.ReplaceAll(upstreamDocument, "tinylog", "tinyfmt"))
	cfg, buildDir := makeBuildTree(t, root)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	notLinked := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED")
	if len(notLinked) != 1 || notLinked[0].Subject.Ref != "tinyfmt" {
		t.Fatalf("findings = %+v, want one PACKAGE_NOT_LINKED naming tinyfmt", notLinked)
	}
	if _, ok := componentByID(result.Document, "component:tinyfmt"); ok {
		t.Error("a package nothing linked reached a component of its own")
	}
	for _, ref := range fileRefs(result.Document) {
		if strings.Contains(ref, "tinyfmt") || strings.Contains(ref, "sbom.cdx.json") {
			t.Errorf("files = %v; a described but unlinked package is in the document", fileRefs(result.Document))
		}
	}
}

// componentIDs lists the components a document carries, in the order it
// carries them.
func componentIDs(document *sbomwriter.Document) []string {
	ids := make([]string, 0, len(document.Components))
	for _, component := range document.Components {
		ids = append(ids, component.ID)
	}
	return ids
}
