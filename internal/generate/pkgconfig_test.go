package generate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/policy"
	"github.com/example/sbomb/internal/sbomwriter"
)

// The tests in this file run the whole pipeline, because what they ask is not
// what a parser took out of a .pc file but what reading one does to the
// document this tool writes: which component a system library lands in, and --
// the half that matters more -- that nothing else about the run moves.

// The .pc file a distribution installs beside libfoo.so, in the shape
// libxcrypt.pc has.
const libfooPkgConfigFile = `prefix=/usr
exec_prefix=${prefix}
libdir=${prefix}/lib
includedir=${prefix}/include

Name: libfoo
Version: 1.2.3
Description: a library the distribution installed
Libs: -L${libdir} -lfoo
Cflags: -I${includedir}
`

// makeSysrootBuildTree lays out a Makefiles build tree that links one library
// out of a sysroot. The sysroot is named on the compile line, which is where
// the anchor model of section 7 looks for it, and the library is named with its
// absolute path in link.txt, which is what the linker really received.
func makeSysrootBuildTree(t *testing.T, root string, pkgConfigFiles map[string]string, headers ...string) (config.Config, string, string) {
	t.Helper()
	sysroot := filepath.Join(root, "sysroot")
	writeTreeFile(t, filepath.Join(sysroot, "usr", "lib", "libfoo.so"), "ELF shared object\n")
	for name, content := range pkgConfigFiles {
		writeTreeFile(t, filepath.Join(sysroot, filepath.FromSlash(name)), content)
	}

	cfg, buildDir := makeBuildTree(t, root, headers...)
	source := filepath.Join(root, "src", "main.c")
	writeTreeFile(t, filepath.Join(buildDir, "CMakeFiles", "app.dir", "link.txt"),
		"cc -o app CMakeFiles/app.dir/main.c.o "+filepath.Join(sysroot, "usr", "lib", "libfoo.so")+"\n")

	commands := []map[string]any{{
		"directory": buildDir,
		"file":      source,
		"arguments": []string{"cc", "--sysroot=" + sysroot, "-c", source, "-o", "CMakeFiles/app.dir/main.c.o"},
	}}
	encoded, err := json.Marshal(commands)
	if err != nil {
		t.Fatal(err)
	}
	writeTreeFile(t, filepath.Join(buildDir, "compile_commands.json"), string(encoded))
	return cfg, buildDir, sysroot
}

// systemLibraryPolicy is the host-linux answer of section 24.2: distribution
// libraries are in the document, under the build-environment component. Under
// the default policy they are not in it at all, which is what the last test
// below is about.
func systemLibraryPolicy() policy.Config {
	resolved := policy.DefaultConfig()
	resolved.SystemLibraries = "separate-component"
	return resolved
}

func runSysrootBuild(t *testing.T, cfg config.Config, buildDir string) *sbomwriter.Document {
	t.Helper()
	result, err := RunWithOptions(cfg, buildDir, true, Options{
		PathFlavor: pathmodel.PosixFlavor{},
		Policy:     systemLibraryPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.Document
}

// The point of the change, seen from the document: a distribution library used
// to arrive as part of one component named after the sysroot, with no version
// anybody could state for it. It now arrives under the name pkg-config keeps
// its package under, with the version the .pc file states.
func TestAPkgConfigFileNamesASystemLibraryAndGivesItAVersion(t *testing.T) {
	root := t.TempDir()
	cfg, buildDir, _ := makeSysrootBuildTree(t, root, map[string]string{
		"usr/lib/pkgconfig/libfoo.pc": libfooPkgConfigFile,
	})
	document := runSysrootBuild(t, cfg, buildDir)

	component, ok := componentByID(document, "component:libfoo")
	if !ok {
		t.Fatalf("the system library reached no component of its own; components are %v", componentIDs(document))
	}
	if component.Version != "1.2.3" || component.VersionSource != "pkg-config" {
		t.Errorf("version = %q from %q, want 1.2.3 from pkg-config", component.Version, component.VersionSource)
	}
	if component.Scope != "system" {
		t.Errorf("scope = %q, want system; naming a distribution library does not make it a product dependency", component.Scope)
	}
	if got := component.Properties["sbomb:component:detectedBy"]; len(got) != 1 || got[0] != "pkg-config:libfoo.pc" {
		t.Errorf("detectedBy = %v, want the .pc file that answered", got)
	}
	// Section 24.1: it hangs under the synthetic build-environment component
	// and not under the product.
	if owner := relationFrom(document, "component:libfoo"); owner != buildEnvironmentID {
		t.Errorf("component:libfoo hangs under %q, want %q", owner, buildEnvironmentID)
	}
	// The licence stays open, and the finding that says so stays with it.
	if len(component.Licenses) != 1 || component.Licenses[0].Name != "NOASSERTION" {
		t.Errorf("licenses = %v, want NOASSERTION; a .pc file states no licence", component.Licenses)
	}
	if !hasFinding(document, "UNKNOWN_LICENSE", "component:libfoo") {
		t.Error("no UNKNOWN_LICENSE for a component whose licence nothing stated")
	}
	// A .pc file names no package ecosystem, so no purl is asserted.
	if component.PURL != "" {
		t.Errorf("purl = %q, want none; section 20.4 needs a package type nobody stated", component.PURL)
	}
	// The root is deliberately unresolved and deliberately unreported: /usr/lib
	// is where the package installed its libraries, not a directory this
	// component owns, and starting a licence search there would be wrong.
	if hasFinding(document, "COMPONENT_ROOT_UNRESOLVED", "component:libfoo") {
		t.Error("COMPONENT_ROOT_UNRESOLVED fired for a root nobody was looking for")
	}
}

// The rule of the package, seen from the document: reading pkg-config metadata
// improves what is known about a file the evidence chain already reached, and
// does nothing else. Nineteen packages installed in the sysroot and never
// linked add no component, no file and not one finding.
func TestPkgConfigFilesForLibrariesNothingLinkedAddNothing(t *testing.T) {
	root := t.TempDir()
	installed := map[string]string{}
	for index := 0; index < 19; index++ {
		name := fmt.Sprintf("libunused%d", index)
		installed["usr/lib/"+name+".so"] = "ELF shared object\n"
		installed["usr/lib/pkgconfig/"+name+".pc"] = strings.NewReplacer(
			"libfoo", name, "1.2.3", "9.9.9", "-lfoo", "-l"+strings.TrimPrefix(name, "lib"),
		).Replace(libfooPkgConfigFile)
	}
	cfg, buildDir, _ := makeSysrootBuildTree(t, root, installed)

	bare := runSysrootBuild(t, cfg, buildDir)
	writeTreeFile(t, filepath.Join(root, "sysroot", "usr", "lib", "pkgconfig", "libfoo.pc"), libfooPkgConfigFile)
	described := runSysrootBuild(t, cfg, buildDir)

	if _, ok := componentByID(described, "component:libfoo"); !ok {
		t.Fatalf("the linked library reached no component; components are %v", componentIDs(described))
	}
	for _, id := range componentIDs(described) {
		if strings.Contains(id, "unused") {
			t.Errorf("a package that was installed but never linked reached the document as %q", id)
		}
	}
	for _, finding := range described.Findings {
		if strings.Contains(finding.Subject.Ref, "unused") || strings.Contains(finding.Message, "unused") {
			t.Errorf("a package nothing linked was reported: %s %s", finding.ID, finding.Subject.Ref)
		}
		if finding.ID == "PACKAGE_NOT_LINKED" {
			t.Errorf("PACKAGE_NOT_LINKED came out of pkg-config metadata: %s", finding.Subject.Ref)
		}
	}
	// The used set is the same set, down to its order: the only thing that
	// moved is which component the library was grouped under.
	before, after := fileRefs(bare), fileRefs(described)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("the used files changed:\nbefore %v\nafter  %v", before, after)
	}
}

// Two .pc files that both verify against the library and state different things
// describe nothing: two statements are no statement (section 19.2). The library
// keeps the component its anchor gives it, and the disagreement is reported.
func TestTwoPkgConfigFilesThatDisagreeLeaveTheLibraryWhereItWas(t *testing.T) {
	root := t.TempDir()
	cfg, buildDir, _ := makeSysrootBuildTree(t, root, map[string]string{
		"usr/lib/pkgconfig/libfoo.pc": libfooPkgConfigFile,
		"usr/lib/pkgconfig/foo.pc":    strings.Replace(libfooPkgConfigFile, "1.2.3", "9.9.9", 1),
	})
	document := runSysrootBuild(t, cfg, buildDir)

	if _, ok := componentByID(document, "component:libfoo"); ok {
		t.Error("a component was named although two .pc files disagreed about it")
	}
	if _, ok := componentByID(document, "component:foo"); ok {
		t.Error("a component was named although two .pc files disagreed about it")
	}
	var reported bool
	for _, finding := range document.Findings {
		if finding.ID == "COMPONENT_MAPPING_CONFLICT" {
			reported = true
			for _, want := range []string{"usr/lib/pkgconfig/foo.pc", "usr/lib/pkgconfig/libfoo.pc"} {
				if !strings.Contains(finding.Message, want) {
					t.Errorf("the conflict does not name %q: %s", want, finding.Message)
				}
			}
		}
	}
	if !reported {
		t.Error("two .pc files disagreed and nothing was reported")
	}
}

// A broken .pc file is reported once and changes nothing else: the library
// falls back to the component its anchor gives it.
func TestABrokenPkgConfigFileIsReportedAndTheLibraryKeepsItsAnchorComponent(t *testing.T) {
	root := t.TempDir()
	cfg, buildDir, _ := makeSysrootBuildTree(t, root, map[string]string{
		"usr/lib/pkgconfig/libfoo.pc": "prefix=/usr\nthis line is neither a variable nor a key\n",
	})
	document := runSysrootBuild(t, cfg, buildDir)

	if _, ok := componentByID(document, "component:libfoo"); ok {
		t.Error("a component was named out of a file that could not be read")
	}
	reported := 0
	for _, finding := range document.Findings {
		if finding.ID == "EVIDENCE_UNREADABLE" && strings.HasSuffix(finding.Subject.Ref, "libfoo.pc") {
			reported++
			if strings.HasPrefix(finding.Subject.Ref, "/") {
				t.Errorf("the finding names the absolute path %q", finding.Subject.Ref)
			}
		}
	}
	if reported != 1 {
		t.Errorf("%d EVIDENCE_UNREADABLE findings for one broken file, want exactly one", reported)
	}
}

// Under the default policy a distribution library is not in the document at
// all (section 24.1), so there is nothing for this reader to describe and it
// reads nothing. The whole document is byte for byte what it was before .pc
// files existed.
func TestTheDefaultPolicyReadsNoPkgConfigFileAtAll(t *testing.T) {
	root := t.TempDir()
	cfg, buildDir, _ := makeSysrootBuildTree(t, root, map[string]string{
		"usr/lib/pkgconfig/libfoo.pc": libfooPkgConfigFile,
	})
	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range result.Document.Components {
		if component.VersionSource == "pkg-config" {
			t.Errorf("component %q was described by pkg-config under the default policy", component.ID)
		}
	}
	if _, ok := componentByID(result.Document, "component:libfoo"); ok {
		t.Error("a distribution library reached the document under the default policy")
	}
}

// A header is described by the package of the directory it sits in -- but only
// once a policy lets system headers into the document at all. Section 24.1
// keeps them out by default, and this reader does not change that: with the
// default policy there is no used file for it to describe.
func TestASystemHeaderIsDescribedOnlyOnceThePolicyIncludesIt(t *testing.T) {
	root := t.TempDir()
	header := filepath.Join(root, "sysroot", "usr", "include", "foo", "foo.h")
	writeTreeFile(t, header, "#pragma once\n")
	cfg, buildDir, _ := makeSysrootBuildTree(t, root, map[string]string{
		"usr/share/pkgconfig/foo.pc": `prefix=/usr
includedir=${prefix}/include

Name: foo
Version: 2.4
Libs:
Cflags: -I${includedir}/foo
`,
	}, header)

	excluded, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := componentByID(excluded.Document, "component:foo"); ok {
		t.Error("a system header reached the document under the default policy")
	}

	included := systemLibraryPolicy()
	included.IncludeSystemHeaders = true
	result, err := RunWithOptions(cfg, buildDir, true, Options{
		PathFlavor: pathmodel.PosixFlavor{},
		Policy:     included,
	})
	if err != nil {
		t.Fatal(err)
	}
	component, ok := componentByID(result.Document, "component:foo")
	if !ok {
		t.Fatalf("the header reached no component of its own; components are %v", componentIDs(result.Document))
	}
	if component.Version != "2.4" || component.VersionSource != "pkg-config" {
		t.Errorf("version = %q from %q, want 2.4 from pkg-config", component.Version, component.VersionSource)
	}
}

// Two runs over one tree produce the same document and the same findings. The
// candidate order is fixed, so a tree in which both candidate directories carry
// the same file answers the same way every time.
func TestTwoRunsOverOneSysrootAgree(t *testing.T) {
	root := t.TempDir()
	cfg, buildDir, _ := makeSysrootBuildTree(t, root, map[string]string{
		"usr/lib/pkgconfig/libfoo.pc":       libfooPkgConfigFile,
		"usr/lib/share/pkgconfig/libfoo.pc": libfooPkgConfigFile,
	})
	first, second := runSysrootBuild(t, cfg, buildDir), runSysrootBuild(t, cfg, buildDir)
	if strings.Join(componentIDs(first), "\n") != strings.Join(componentIDs(second), "\n") {
		t.Errorf("two runs disagreed about the components:\n%v\n%v", componentIDs(first), componentIDs(second))
	}
	component, ok := componentByID(first, "component:libfoo")
	if !ok {
		t.Fatal("the same file in both candidate directories is one answer, and it was not given")
	}
	if component.Version != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", component.Version)
	}
}

// The other side of the comparison above: two files of one module that were
// described the same way have said one thing twice. A package whose libraries
// lie in two directories -- and a package whose hundreds of headers all derive
// the same candidate -- must not look like a disagreement with itself.
func TestTwoFilesOfOneModuleThatAgreeAreOneAnswer(t *testing.T) {
	root := t.TempDir()
	stated := strings.Replace(libfooPkgConfigFile, "-L${libdir} -lfoo", "-L${libdir} -L${libdir}/foo -lfoo", 1)
	cfg, buildDir, sysroot := makeSysrootBuildTree(t, root, map[string]string{
		"usr/lib/pkgconfig/libfoo.pc":     stated,
		"usr/lib/foo/pkgconfig/libfoo.pc": stated,
	})
	other := filepath.Join(sysroot, "usr", "lib", "foo", "libfoo.so")
	writeTreeFile(t, other, "ELF shared object\n")
	writeTreeFile(t, filepath.Join(buildDir, "CMakeFiles", "app.dir", "link.txt"),
		"cc -o app CMakeFiles/app.dir/main.c.o "+
			filepath.Join(sysroot, "usr", "lib", "libfoo.so")+" "+other+"\n")
	document := runSysrootBuild(t, cfg, buildDir)

	component, ok := componentByID(document, "component:libfoo")
	if !ok {
		t.Fatalf("the libraries reached no component of their own; components are %v", componentIDs(document))
	}
	if component.Version != "1.2.3" || component.VersionSource != "pkg-config" {
		t.Errorf("version = %q from %q, want 1.2.3 from pkg-config", component.Version, component.VersionSource)
	}
	for _, finding := range document.Findings {
		if finding.ID == "COMPONENT_MAPPING_CONFLICT" {
			t.Errorf("a package was reported as disagreeing with itself: %s", finding.Message)
		}
	}
	// Both libraries are in that component, and nothing else came with them.
	for _, path := range []string{"usr/lib/libfoo.so", "usr/lib/foo/libfoo.so"} {
		if owner := ownerOf(document, "sysroot:sysroot:"+path); owner != "component:libfoo" {
			t.Errorf("%s hangs under %q, want component:libfoo", path, owner)
		}
	}
}

// Two packages can carry the same module name in one sysroot -- a distribution
// library in /usr/lib and a hand-built one in /opt/lib both called libfoo --
// and then two verified .pc files describe one component. Each mapping is right
// on its own, so both files belong there; what nobody can state is which of the
// two versions the component has. It gets neither, and the disagreement is
// reported: publishing one of them would be the guess this reader exists to
// avoid.
func TestTwoPackagesOfOneModuleNameLeaveTheComponentWithoutAVersion(t *testing.T) {
	root := t.TempDir()
	cfg, buildDir, sysroot := makeSysrootBuildTree(t, root, map[string]string{
		"usr/lib/pkgconfig/libfoo.pc": strings.Replace(libfooPkgConfigFile, "1.2.3", "1.0.0", 1),
		"opt/lib/pkgconfig/libfoo.pc": strings.NewReplacer(
			"1.2.3", "2.0.0", "${prefix}/lib", "/opt/lib",
		).Replace(libfooPkgConfigFile),
	})
	other := filepath.Join(sysroot, "opt", "lib", "libfoo.so")
	writeTreeFile(t, other, "ELF shared object\n")
	writeTreeFile(t, filepath.Join(buildDir, "CMakeFiles", "app.dir", "link.txt"),
		"cc -o app CMakeFiles/app.dir/main.c.o "+
			filepath.Join(sysroot, "usr", "lib", "libfoo.so")+" "+other+"\n")
	document := runSysrootBuild(t, cfg, buildDir)

	component, ok := componentByID(document, "component:libfoo")
	if !ok {
		t.Fatalf("the libraries reached no component of their own; components are %v", componentIDs(document))
	}
	if component.Version != "" || component.VersionSource != "" {
		t.Errorf("version = %q from %q, want none: two packages of this name stated different versions",
			component.Version, component.VersionSource)
	}
	var reported bool
	for _, finding := range document.Findings {
		if finding.ID != "COMPONENT_MAPPING_CONFLICT" || finding.Subject.Ref != "component:libfoo" {
			continue
		}
		reported = true
		for _, want := range []string{"1.0.0", "2.0.0"} {
			if !strings.Contains(finding.Message, want) {
				t.Errorf("the conflict does not name the version %q: %s", want, finding.Message)
			}
		}
	}
	if !reported {
		t.Error("two packages of one module name stated different versions and nothing was reported")
	}
}

// relationFrom names the component a component hangs under.
func relationFrom(document *sbomwriter.Document, id string) string {
	for _, relation := range document.Relations {
		for _, ref := range relation.To {
			if ref == id {
				return relation.From
			}
		}
	}
	return ""
}

func hasFinding(document *sbomwriter.Document, id, subject string) bool {
	for _, finding := range document.Findings {
		if finding.ID == id && finding.Subject.Ref == subject {
			return true
		}
	}
	return false
}
