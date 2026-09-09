package generate

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/sbomwriter"
)

// The tests in this file run the whole pipeline, because the question they ask
// is not what a reader took out of an image manifest but what reading one does
// to the document this tool writes. A unit test can show that two values
// arrived; only a run can show that the 798 packages beside them did not.

// writeVendoredLibrary writes a library that was simply copied into the source
// tree: a header and the licence file that bounds it. Section 19.2 strategy 6
// finds such a component and names it after its directory, and until now that
// was all anyone learnt about it.
func writeVendoredLibrary(t *testing.T, root, name string) string {
	t.Helper()
	header := filepath.Join(root, "vendor", name, name+".h")
	writeTreeFile(t, header, "#define "+strings.ToUpper(name)+" 1\n")
	writeTreeFile(t, filepath.Join(root, "vendor", name, "LICENSE"), "Copyright somebody.\n")
	return header
}

// writeImageManifest writes a Buildroot legal-info manifest for a whole image:
// the packages named here plus the several hundred others such an image holds,
// none of which this build has anything to do with.
func writeImageManifest(t *testing.T, described map[string]string) string {
	t.Helper()
	var manifest strings.Builder
	manifest.WriteString("\"PACKAGE\",\"VERSION\",\"LICENSE\",\"LICENSE FILES\",\"SOURCE\",\"SOURCE SITE\"\n")
	names := make([]string, 0, len(described))
	for name := range described {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&manifest, "%q,%q,%q,\"LICENSE\",\"%s.tar.gz\",\"https://example.invalid\"\n",
			name, described[name], "BSD-3-Clause", name)
	}
	for index := 0; index < 800-len(described); index++ {
		fmt.Fprintf(&manifest, "\"image-package-%d\",\"1.0\",\"MIT\",\"COPYING\",\"p.tar.gz\",\"https://example.invalid\"\n", index)
	}
	// Outside every build tree, as a deploy directory is.
	path := filepath.Join(t.TempDir(), "manifest.csv")
	writeTreeFile(t, path, manifest.String())
	return path
}

// relationRefs renders the relations of a document, which is where the used
// files and the components meet: a component that gained a file, or a file that
// changed hands, shows up here and nowhere else.
func relationRefs(document *sbomwriter.Document) []string {
	refs := make([]string, 0, len(document.Relations))
	for _, relation := range document.Relations {
		refs = append(refs, relation.From+" -> "+strings.Join(relation.To, ","))
	}
	sort.Strings(refs)
	return refs
}

// THE test of this feature. An image manifest describes an image, not this
// build: it may improve what is known about the components the evidence chain
// reached, and it may do nothing else. An 800-package manifest against a build
// that links three libraries must produce three described components and not
// one component, file, relation or finding more.
func TestAnImageManifestDescribesWhatIsThereAndAddsNothing(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	vendored := writeVendoredLibrary(t, root, "tinylog")
	undescribed := writeVendoredLibrary(t, root, "tinyhash")
	port := filepath.Join(buildDir, "vcpkg_installed", "x64-linux", "include", "tinyfmt.h")
	writeTreeFile(t, port, "#define TINYFMT 1\n")
	writeVcpkgPort(t, buildDir, "tinyfmt", "2.1.0", []string{"x64-linux/include/tinyfmt.h"})
	cfg, buildDir := makeBuildTree(t, root, vendored, undescribed, port)

	bare, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}

	manifest := writeImageManifest(t, map[string]string{"tinylog": "1.9.0", "tinyfmt": "2.0.0"})
	cfg.DistroManifests = []string{manifest}
	described, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}

	// What the manifest is for: a library copied into the tree carried nothing
	// but its directory name, and the image build knows its version and its
	// licence.
	component, ok := componentByID(described.Document, "component:tinylog")
	if !ok {
		t.Fatal("the vendored library reached no component")
	}
	if component.Version != "1.9.0" || component.VersionSource != "buildroot" {
		t.Errorf("version = %q from %q, want the image manifest's answer and its origin",
			component.Version, component.VersionSource)
	}
	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "BSD-3-Clause" {
		t.Errorf("licenses = %+v, want the expression the image manifest declared", component.Licenses)
	}
	// A component the manifest does not mention is left exactly as it was.
	before, _ := componentByID(bare.Document, "component:tinyhash")
	after, _ := componentByID(described.Document, "component:tinyhash")
	if before.Version != after.Version || before.VersionSource != after.VersionSource {
		t.Errorf("tinyhash = %q/%q, want the %q/%q of a run without the manifest",
			after.Version, after.VersionSource, before.Version, before.VersionSource)
	}
	// Equal rank: the manager that installed the port keeps the field, because
	// this reader is asked after the adapter (section 21.1).
	installed, _ := componentByID(described.Document, "component:tinyfmt")
	if installed.Version != "2.1.0" || installed.VersionSource != "vcpkg" {
		t.Errorf("tinyfmt = %q from %q, want vcpkg's own answer at equal rank",
			installed.Version, installed.VersionSource)
	}

	// And now the half that matters more: everything else is where it was.
	if got, want := componentIDs(described.Document), componentIDs(bare.Document); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("components = %v, want the %v of a run without the manifest", got, want)
	}
	if got, want := fileRefs(described.Document), fileRefs(bare.Document); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the files changed with the manifest:\n%v\n%v", want, got)
	}
	if got, want := relationRefs(described.Document), relationRefs(bare.Document); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the relations changed with the manifest:\n%v\n%v", want, got)
	}
	for _, ref := range fileRefs(described.Document) {
		if strings.Contains(ref, "manifest.csv") {
			t.Errorf("files = %v; the manifest read itself into the used set", fileRefs(described.Document))
		}
	}
	// No entry of an image manifest becomes a package, so none of them can be
	// reported as installed-but-unlinked either.
	if notLinked := findingsWithID(described.Findings, "PACKAGE_NOT_LINKED"); len(notLinked) != 0 {
		t.Errorf("findings = %+v, want no PACKAGE_NOT_LINKED for an image manifest", notLinked)
	}
	// Reading the manifest may only remove findings -- the version it supplied
	// is one UNKNOWN_VERSION fewer -- and never add one.
	if len(described.Findings) > len(bare.Findings) {
		t.Errorf("findings grew from %d to %d with the manifest", len(bare.Findings), len(described.Findings))
	}
	// Not one of the 798 packages the image holds and this build never touched
	// is mentioned anywhere.
	for _, finding := range described.Findings {
		if strings.Contains(finding.Subject.Ref, "image-package-") || strings.Contains(finding.Message, "image-package-") {
			t.Errorf("finding %s mentions an entry nothing matched: %+v", finding.ID, finding)
		}
	}
	for _, id := range componentIDs(described.Document) {
		if strings.Contains(id, "image-package-") {
			t.Errorf("components = %v; an image manifest created one", componentIDs(described.Document))
		}
	}
}

// A component the upstream describes itself is described by the upstream. The
// bundled document ranks 4 and the image manifest 3, so the document wins and
// the manifest's claim is the one that loses.
func TestABundledDocumentOutranksAnImageManifest(t *testing.T) {
	root := t.TempDir()
	header := writeVendoredLibrary(t, root, "tinylog")
	writeTreeFile(t, filepath.Join(root, "vendor", "tinylog", "sbom.cdx.json"), upstreamDocument)
	cfg, buildDir := makeBuildTree(t, root, header)
	cfg.DistroManifests = []string{writeImageManifest(t, map[string]string{"tinylog": "1.9.0"})}

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	component, ok := componentByID(result.Document, "component:tinylog")
	if !ok {
		t.Fatal("the vendored library reached no component")
	}
	if component.Version != "1.9.0-upstream" || component.VersionSource != "bundled-sbom" {
		t.Errorf("version = %q from %q, want the document the upstream shipped",
			component.Version, component.VersionSource)
	}
}

// A package of the image that this build never linked stays out of the
// document, in silence. Hundreds of unmatched entries are what an image
// manifest normally holds, and a finding per entry would drown the report.
func TestAnImageManifestEntryNothingLinkedIsSilent(t *testing.T) {
	root := t.TempDir()
	header := writeVendoredLibrary(t, root, "tinylog")
	cfg, buildDir := makeBuildTree(t, root, header)
	cfg.DistroManifests = []string{writeImageManifest(t, map[string]string{"tinylog": "1.9.0", "openssl": "3.0.12"})}

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := componentByID(result.Document, "component:openssl"); present {
		t.Error("openssl reached a component; being in an image is no evidence that this build linked it")
	}
	for _, finding := range result.Findings {
		if strings.Contains(finding.Subject.Ref, "openssl") || strings.Contains(finding.Message, "openssl") {
			t.Errorf("finding %s mentions a package nothing linked: %+v", finding.ID, finding)
		}
	}
}

// A configured path that is not there is reported and the run goes on: the
// manifest improves a document that is correct without it, so a typo is a
// warning rather than the end of the run.
func TestAConfiguredImageManifestThatIsNotThereIsReportedAndTheRunGoesOn(t *testing.T) {
	root := t.TempDir()
	header := writeVendoredLibrary(t, root, "tinylog")
	cfg, buildDir := makeBuildTree(t, root, header)
	missing := filepath.Join(root, "deploy", "licenses", "image", "license.manifest")
	cfg.DistroManifests = []string{missing}

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	reported := findingsWithID(result.Findings, "MISSING_PACKAGE_EVIDENCE")
	if len(reported) != 1 || reported[0].Subject.Ref != missing {
		t.Fatalf("findings = %+v, want one MISSING_PACKAGE_EVIDENCE naming the configured path", reported)
	}
	if _, ok := componentByID(result.Document, "component:tinylog"); !ok {
		t.Error("the component vanished with the manifest that was not there")
	}
}

// A relative path is anchored at the project root, exactly as the native
// manifest list of section 32.2 is, so one configuration works from any working
// directory.
func TestARelativeImageManifestPathIsAnchoredAtTheProjectRoot(t *testing.T) {
	root := t.TempDir()
	header := writeVendoredLibrary(t, root, "tinylog")
	cfg, buildDir := makeBuildTree(t, root, header)
	writeTreeFile(t, filepath.Join(root, "deploy", "manifest.csv"),
		"\"PACKAGE\",\"VERSION\",\"LICENSE\"\n\"tinylog\",\"1.9.0\",\"MIT\"\n")
	cfg.DistroManifests = []string{filepath.Join("deploy", "manifest.csv")}

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	component, _ := componentByID(result.Document, "component:tinylog")
	if component.Version != "1.9.0" {
		t.Errorf("version = %q, want the one the manifest under the project root states", component.Version)
	}
}

// Two runs over one build and one manifest write the same document and report
// the same findings in the same order (section 11.1).
func TestTwoRunsWithAnImageManifestAgree(t *testing.T) {
	root := t.TempDir()
	header := writeVendoredLibrary(t, root, "tinylog")
	cfg, buildDir := makeBuildTree(t, root, header)
	cfg.DistroManifests = []string{writeImageManifest(t, map[string]string{"tinylog": "1.9.0"})}

	render := func() string {
		result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
		if err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		for _, component := range result.Document.Components {
			fmt.Fprintf(&out, "%s %s %s %+v\n", component.ID, component.Version, component.VersionSource, component.Licenses)
		}
		for _, finding := range result.Findings {
			fmt.Fprintf(&out, "%s %s %s\n", finding.ID, finding.Subject.Ref, finding.Message)
		}
		return out.String()
	}
	if first, second := render(), render(); first != second {
		t.Errorf("two runs disagreed:\n%s\n%s", first, second)
	}
}

// A configuration that names no image manifest opens no file and behaves as it
// did before this reader existed.
func TestWithoutAConfiguredManifestNothingIsRead(t *testing.T) {
	root := t.TempDir()
	header := writeVendoredLibrary(t, root, "tinylog")
	cfg, buildDir := makeBuildTree(t, root, header)
	if len(cfg.DistroManifests) != 0 {
		t.Fatal("the test tree configured a manifest")
	}
	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	component, ok := componentByID(result.Document, "component:tinylog")
	if !ok {
		t.Fatal("the vendored library reached no component")
	}
	if component.VersionSource == "buildroot" || component.VersionSource == "yocto" {
		t.Errorf("version source = %q without a configured manifest", component.VersionSource)
	}
}

// A configuration is loaded from JSON, so the key has to be spelled the way the
// documentation spells it.
func TestTheDistroManifestKeyIsLoadedFromAConfigurationFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sbomb.json")
	writeTreeFile(t, path, `{"project":{"name":"p"},"distroManifests":["deploy/manifest.csv"]}`)
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.DistroManifests) != 1 || loaded.DistroManifests[0] != "deploy/manifest.csv" {
		t.Errorf("distroManifests = %v, want the configured path", loaded.DistroManifests)
	}
}

// A port vcpkg installed and nothing ever included is reported as unlinked,
// with or without an image manifest beside it. The manifest must neither
// silence that report -- describing a package is no evidence that anything
// linked it -- nor add one of its own for the hundreds of image packages this
// build has nothing to do with. The count is one, and it is the port's.
func TestAnUnlinkedPortIsReportedOnceWithAnImageManifestBeside(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	writeTreeFile(t, filepath.Join(buildDir, "vcpkg_installed", "x64-linux", "include", "tinyfmt.h"),
		"#define TINYFMT 1\n")
	writeVcpkgPort(t, buildDir, "tinyfmt", "2.1.0", []string{"x64-linux/include/tinyfmt.h"})
	header := writeVendoredLibrary(t, root, "tinylog")
	cfg, buildDir := makeBuildTree(t, root, header)
	cfg.DistroManifests = []string{writeImageManifest(t, map[string]string{
		"tinylog": "1.9.0", "tinyfmt": "2.0.0",
	})}

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	notLinked := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED")
	if len(notLinked) != 1 || notLinked[0].Subject.Ref != "tinyfmt" {
		t.Fatalf("findings = %+v, want exactly one PACKAGE_NOT_LINKED, naming the port", notLinked)
	}
	// The port stays out of the document, described or not: a version from an
	// image manifest is not a link.
	if _, present := componentByID(result.Document, "component:tinyfmt"); present {
		t.Error("a port nothing linked reached a component because a manifest named it")
	}
	for _, ref := range fileRefs(result.Document) {
		if strings.Contains(ref, "tinyfmt.h") {
			t.Errorf("files = %v; a header nothing included is in the document", fileRefs(result.Document))
		}
	}
	// The library the chain did reach is described, which is the whole point.
	if component, ok := componentByID(result.Document, "component:tinylog"); !ok || component.Version != "1.9.0" {
		t.Errorf("tinylog = %+v, want the version the image build recorded", component)
	}
}
