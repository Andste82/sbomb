package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/adapters/pkgmanager"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
)

// Section 19.2: the component root is a resolved fact. The tests here are the
// acceptance criteria of that rule that the corpus cannot state -- a curated
// path against a package-manager root, a library inside a library, a licence
// file that the enclosing tree must not lend out. What the corpus can state is
// in cmd/sbomb/foss_fixture_test.go, against p14-foss.

// spdxLicenceFile is a licence file that resolves through technique 1 of
// section 22.3. The tests below observe which file was read, not how it was
// recognized, so the shortest thing that resolves is the right fixture.
func spdxLicenceFile(id string) string {
	return "SPDX-License-Identifier: " + id + "\n"
}

// licenceOf is what enrichment concluded about a component, as one string, so
// that a test can say "not the enclosing tree's licence" in one comparison.
func licenceOf(component *domain.Component) string {
	if len(component.Licenses) == 0 {
		return ""
	}
	if component.Licenses[0].Expression != "" {
		return component.Licenses[0].Expression
	}
	return component.Licenses[0].Name
}

// The mapping priority of section 19.2 settles the root, and each step of it
// has to outrank the one below. The defect this guards is the last line of
// that order becoming the first: a root taken from the used files moves with
// whatever the linker kept, and the licence read from it moves too.
func TestTheComponentRootFollowsTheMappingPriority(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "dep", "mit-lib")
	source := filepath.Join(depRoot, "src", "a.c")
	write(t, source, "void f(void){}\n")

	file := domain.UsedFile{ID: fileID("project", "dep/mit-lib/src/a.c")}
	physical := map[string]string{file.ID.Canonical(): source}
	component := func() *domain.Component {
		return &domain.Component{ID: "component:mit-lib", Name: "mit-lib", DetectedBy: "unresolved"}
	}

	// Nothing named a root, and no marker is there to find: the deepest common
	// directory of the used files is all that is left, and it is src/.
	plain := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)
	fallback := plain.resolveRoot(component(), []domain.UsedFile{file})
	if fallback.Source != rootSourceUsedFiles {
		t.Errorf("with nothing to go on: root came from %q, want %q", fallback.Source, rootSourceUsedFiles)
	}
	if fallback.ID.Canonical() != "project:dep/mit-lib/src" {
		t.Errorf("fallback root = %q, want the directory the used files share", fallback.ID.Canonical())
	}

	// A package manager stated where the package lives, which outranks the
	// files (strategy 2 against the last resort).
	managed := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)
	managed.setPackages([]pkgmanager.Package{{
		Name:      "mit-lib",
		Roots:     []string{depRoot},
		Manager:   "conan",
		AnchorKey: "pkg:conan/mit-lib",
	}}, identityFor(map[string]domain.FileID{depRoot: fileID("pkg:conan/mit-lib", "")}))
	byManager := managed.resolveRoot(component(), []domain.UsedFile{file})
	if byManager.Source != "package-manager" || byManager.ID.Canonical() != "pkg:conan/mit-lib:" {
		t.Errorf("root = %q from %q, want the package root the manager stated",
			byManager.ID.Canonical(), byManager.Source)
	}

	// And configuration outranks the manager: strategy 1 is the manufacturer
	// saying where the component begins, which is the one statement no reader
	// may overrule.
	cfg := config.Config{
		Project:    config.Project{Name: "firmware"},
		Components: []config.Component{{Path: "dep/mit-lib", Name: "mit-lib"}},
	}
	curated := newComponentResolver(cfg, physical, map[string]string{"project": root}, nil)
	curated.setPackages([]pkgmanager.Package{{
		Name:      "mit-lib",
		Roots:     []string{depRoot},
		Manager:   "conan",
		AnchorKey: "pkg:conan/mit-lib",
	}}, identityFor(map[string]domain.FileID{depRoot: fileID("pkg:conan/mit-lib", "")}))
	byConfig := curated.resolveRoot(component(), []domain.UsedFile{file})
	if byConfig.Source != "curated" || byConfig.ID.Canonical() != "project:dep/mit-lib" {
		t.Errorf("root = %q from %q, want the configured path", byConfig.ID.Canonical(), byConfig.Source)
	}
	if byConfig.Physical != depRoot {
		t.Errorf("physical root = %q, want %q", byConfig.Physical, depRoot)
	}
}

// The defect that made an attribution document depend on --gc-sections: the
// root was the deepest common directory of the files that survived the link,
// so a library of three sources with one member extracted had the root src/
// and its LICENSE one level up was never opened. Whether the linker kept one
// source or all three, the component begins in the same place and carries the
// same licence.
func TestTheLicenceDoesNotDependOnWhichFilesTheLinkerKept(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "dep", "mit-lib", "LICENSE"), mitText)

	physical := map[string]string{}
	all := make([]domain.UsedFile, 0, 3)
	for _, name := range []string{"a.c", "b.c", "c.c"} {
		file := domain.UsedFile{ID: fileID("project", "dep/mit-lib/src/"+name)}
		physical[file.ID.Canonical()] = filepath.Join(root, "dep", "mit-lib", "src", name)
		write(t, physical[file.ID.Canonical()], "void f(void){}\n")
		all = append(all, file)
	}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	for _, files := range [][]domain.UsedFile{all[:1], all} {
		component := &domain.Component{ID: "component:mit-lib", Name: "mit-lib", DetectedBy: "package-metadata:LICENSE"}
		resolver.enrichComponent(component, files)
		if component.Root == nil || component.Root.Canonical() != "project:dep/mit-lib" {
			t.Errorf("with %d used file(s): root = %v, want project:dep/mit-lib", len(files), component.Root)
		}
		if got := licenceOf(component); got != "MIT" {
			t.Errorf("with %d used file(s): licence = %q, want MIT", len(files), got)
		}
	}
}

// A component holding LICENSE-MIT and LICENSE-APACHE side by side holds no
// file called LICENSE at all. While the boundary markers were a list of exact
// names to stat, such a library was invisible: its sources dissolved into the
// enclosing project, and the attribution export named the manufacturer as the
// holder of both texts. Section 22.3 matches the -<id> form, and section 19.2
// now marks a boundary with whatever section 22.3 recognizes.
func TestALicenceIdFileMarksAComponentOfItsOwn(t *testing.T) {
	root := t.TempDir()
	componentRoot := filepath.Join(root, "dep", "multi-license")
	write(t, filepath.Join(componentRoot, "LICENSE-MIT"), mitText)
	write(t, filepath.Join(componentRoot, "LICENSE-APACHE"), spdxLicenceFile("Apache-2.0"))
	source := filepath.Join(componentRoot, "src", "multi.c")
	write(t, source, "int multi(void){return 0;}\n")

	file := domain.UsedFile{ID: fileID("project", "dep/multi-license/src/multi.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source}, map[string]string{"project": root}, nil)

	_, name, _, _, detectedBy := resolver.resolve(file)
	if name != "multi-license" {
		t.Fatalf("component name = %q, want multi-license; the library was absorbed into the project", name)
	}
	// Which of the two decided it is fixed by the order of section 22.3 rather
	// than by the order the filesystem returned the entries in.
	if detectedBy != "package-metadata:LICENSE-APACHE" {
		t.Errorf("detectedBy = %q, want the licence file that decided it", detectedBy)
	}

	// Both are recognized, which is what F4 retains and what the notices
	// document reproduces. One of them being the marker does not hide the
	// other.
	if got := licenseFilesIn(componentRoot); strings.Join(got, ",") != "LICENSE-APACHE,LICENSE-MIT" {
		t.Errorf("licence files of the root = %v, want both", got)
	}

	component := &domain.Component{ID: "component:multi-license", Name: "multi-license", DetectedBy: detectedBy}
	resolver.enrichComponent(component, []domain.UsedFile{file})
	if component.Root == nil || component.Root.Canonical() != "project:dep/multi-license" {
		t.Errorf("root = %v, want project:dep/multi-license", component.Root)
	}
}

// Attribution material is not a licence grant. A directory carrying only a
// COPYRIGHT is not thereby a separate work, and section 19.2 says so for the
// same reason it says it for NOTICE -- which matters more now that the marker
// is matched by rule rather than listed by name, because section 22.3
// recognizes all five names and only three of them grant anything.
func TestAttributionMaterialAloneMarksNoBoundary(t *testing.T) {
	for _, name := range []string{"NOTICE", "COPYRIGHT", "copyright.md"} {
		root := t.TempDir()
		write(t, filepath.Join(root, "src", "vendorbits", name), "Copyright (c) 2024 Example Holder\n")
		write(t, filepath.Join(root, "src", "vendorbits", "helper.c"), "void helper(void){}\n")

		file := domain.UsedFile{ID: fileID("project", "src/vendorbits/helper.c")}
		physical := map[string]string{file.ID.Canonical(): filepath.Join(root, "src", "vendorbits", "helper.c")}
		resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
			physical, map[string]string{"project": root}, nil)

		if _, got, _, _, _ := resolver.resolve(file); got != "firmware" {
			t.Errorf("a directory carrying only %s became component %q", name, got)
		}
	}
}

// A licence file is recognized the way section 22.3 recognizes one, so the
// spelling of the name is not what decides whether a library is seen. The
// fixed list of names this replaced matched LICENSE and missed license.
func TestABoundaryLicenceFileIsRecognizedWhateverItsCase(t *testing.T) {
	for _, name := range []string{"license", "License.txt", "COPYING.md", "licence"} {
		root := t.TempDir()
		write(t, filepath.Join(root, "dep", "tinylib", name), mitText)
		source := filepath.Join(root, "dep", "tinylib", "tinylib.c")
		write(t, source, "void tinylib(void){}\n")

		file := domain.UsedFile{ID: fileID("project", "dep/tinylib/tinylib.c")}
		resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
			map[string]string{file.ID.Canonical(): source}, map[string]string{"project": root}, nil)

		if _, got, _, _, _ := resolver.resolve(file); got != "tinylib" {
			t.Errorf("a library whose licence file is called %q became component %q", name, got)
		}
	}
}

// A library inside a library. mbedtls carries its own LICENSE and vendors
// everest under 3rdparty/, which carries another: the nearer marker wins, so
// everest is a component of its own with a root of its own, and mbedtls's
// licence is never attributed to it. This is the case the parent walk that
// bee9a03 removed got wrong -- it ascended to the filesystem root and printed
// a stranger's licence under this component's name.
func TestTheNearerLicenceFileSplitsTheInnerLibraryOut(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "dep", "mbedtls")
	inner := filepath.Join(outer, "3rdparty", "everest")
	write(t, filepath.Join(outer, "LICENSE"), mitText)
	write(t, filepath.Join(inner, "LICENSE"), spdxLicenceFile("Apache-2.0"))

	outerSource := filepath.Join(outer, "library", "ssl_tls.c")
	innerSource := filepath.Join(inner, "library", "everest.c")
	write(t, outerSource, "void ssl_tls(void){}\n")
	write(t, innerSource, "void everest(void){}\n")

	outerFile := domain.UsedFile{ID: fileID("project", "dep/mbedtls/library/ssl_tls.c")}
	innerFile := domain.UsedFile{ID: fileID("project", "dep/mbedtls/3rdparty/everest/library/everest.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{
			outerFile.ID.Canonical(): outerSource,
			innerFile.ID.Canonical(): innerSource,
		}, map[string]string{"project": root}, nil)

	// The longest matching prefix wins, so the inner file never lands in the
	// outer component (section 19.2).
	_, outerName, _, _, _ := resolver.resolve(outerFile)
	_, innerName, _, _, _ := resolver.resolve(innerFile)
	if outerName != "mbedtls" || innerName != "everest" {
		t.Fatalf("components = (%s, %s), want (mbedtls, everest)", outerName, innerName)
	}

	outerComponent := &domain.Component{ID: "component:mbedtls", Name: "mbedtls", DetectedBy: "package-metadata:LICENSE"}
	resolver.enrichComponent(outerComponent, []domain.UsedFile{outerFile})
	innerComponent := &domain.Component{ID: "component:everest", Name: "everest", DetectedBy: "package-metadata:LICENSE"}
	resolver.enrichComponent(innerComponent, []domain.UsedFile{innerFile})

	if outerComponent.Root == nil || outerComponent.Root.Canonical() != "project:dep/mbedtls" {
		t.Errorf("mbedtls root = %v, want project:dep/mbedtls", outerComponent.Root)
	}
	if innerComponent.Root == nil || innerComponent.Root.Canonical() != "project:dep/mbedtls/3rdparty/everest" {
		t.Errorf("everest root = %v, want the inner directory", innerComponent.Root)
	}
	if got := licenceOf(outerComponent); got != "MIT" {
		t.Errorf("mbedtls licence = %q, want MIT", got)
	}
	if got := licenceOf(innerComponent); got != "Apache-2.0" {
		t.Errorf("everest licence = %q, want Apache-2.0 from its own LICENSE and not the enclosing one", got)
	}
}

// The upward walk is bounded by the anchor, and that bound is what keeps a
// dependency from being given the project's own licence. A component whose
// root carries no licence file has no licence evidence -- which is an answer,
// stated as NOASSERTION with a reason and a finding, and never the licence of
// the tree that happens to enclose it.
func TestAComponentWithNoLicenceFileNeverGetsTheProjectsOwn(t *testing.T) {
	root := t.TempDir()
	// The project's own top-level licence, one directory above the dependency.
	write(t, filepath.Join(root, "LICENSE"), mitText)
	write(t, filepath.Join(root, "dep", "vendored", "vcpkg.json"),
		"{\"name\":\"vendored\",\"version\":\"1.0\"}\n")
	source := filepath.Join(root, "dep", "vendored", "src", "x.c")
	write(t, source, "void x(void){}\n")

	file := domain.UsedFile{ID: fileID("project", "dep/vendored/src/x.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source}, map[string]string{"project": root}, nil)

	component := &domain.Component{ID: "component:vendored", Name: "vendored", DetectedBy: "package-metadata:vcpkg.json"}
	findings := resolver.enrichComponent(component, []domain.UsedFile{file})

	if component.Root == nil || component.Root.Canonical() != "project:dep/vendored" {
		t.Fatalf("root = %v, want project:dep/vendored", component.Root)
	}
	if got := licenceOf(component); got != "NOASSERTION" {
		t.Errorf("licence = %q, want NOASSERTION; the project's own licence is not this component's", got)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "UNKNOWN_LICENSE" {
			reported = true
		}
	}
	if !reported {
		t.Errorf("findings = %+v, want UNKNOWN_LICENSE: the gap has to be named", findings)
	}
	// And the walk stopped where the anchor does rather than one level higher,
	// so the project's licence was never even a candidate.
	if _, ok := resolver.licenseBoundaries[root]; ok {
		t.Error("the walk asked about the anchor root, which is not a boundary the search may cross")
	}
}

// Section 31: the boundary markers are consulted once per used file per
// ancestor directory, so the licence match -- which needs the directory listed
// rather than a name stat-ed -- is answered from a memo. A run over a tree of
// files pays for each directory once.
func TestTheLicenceBoundaryOfADirectoryIsReadOnce(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "dep", "tinylib", "LICENSE"), mitText)

	physical := map[string]string{}
	files := make([]domain.UsedFile, 0, 4)
	for _, name := range []string{"a.c", "b.c", "c.c", "d.c"} {
		file := domain.UsedFile{ID: fileID("project", "dep/tinylib/src/"+name)}
		physical[file.ID.Canonical()] = filepath.Join(root, "dep", "tinylib", "src", name)
		write(t, physical[file.ID.Canonical()], "void f(void){}\n")
		files = append(files, file)
	}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	for _, file := range files {
		resolver.nearestPackageRoot(file)
	}
	// src/ carries no licence and tinylib/ does; the walk stops there, so
	// those two directories are the only ones an answer was kept for -- one
	// entry each, however many files walked through them.
	want := map[string]string{
		filepath.Join(root, "dep", "tinylib", "src"): "",
		filepath.Join(root, "dep", "tinylib"):        "LICENSE",
	}
	if len(resolver.licenseBoundaries) != len(want) {
		t.Fatalf("memo = %v, want one entry per directory walked: %v", resolver.licenseBoundaries, want)
	}
	for dir, marker := range want {
		if got, known := resolver.licenseBoundaries[dir]; !known || got != marker {
			t.Errorf("memo for %q = %q (known %v), want %q", dir, got, known, marker)
		}
	}
}
