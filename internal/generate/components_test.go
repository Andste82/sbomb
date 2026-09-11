package generate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/adapters/pkgmanager"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/pathmodel"
)

func fileID(anchor, rel string) domain.FileID {
	return domain.FileID{Anchor: domain.AnchorKey(anchor), RelPath: rel}
}

func TestCuratedConfigurationOutranksEveryOtherStrategy(t *testing.T) {
	root := t.TempDir()
	// A package manifest that strategy 6 would otherwise pick up.
	vendor := filepath.Join(root, "dep", "mbedtls")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendor, "conanfile.txt"), []byte("[requires]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := domain.UsedFile{ID: fileID("project", "dep/mbedtls/aes.c")}
	physical := map[string]string{file.ID.Canonical(): filepath.Join(vendor, "aes.c")}

	cfg := config.Config{
		Project:    config.Project{Name: "firmware"},
		Components: []config.Component{{Path: "dep/mbedtls", Name: "mbedtls", Type: "library"}},
	}
	resolver := newComponentResolver(cfg, physical, map[string]string{"project": root}, nil)

	id, name, _, _, detectedBy := resolver.resolve(file)
	if name != "mbedtls" || detectedBy != "curated" {
		t.Fatalf("resolve() = (%s, %s, %s), want the curated rule", id, name, detectedBy)
	}
}

func TestNearestPackageManifestNamesTheComponent(t *testing.T) {
	root := t.TempDir()
	vendor := filepath.Join(root, "dep", "tinycbor")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendor, "vcpkg.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := domain.UsedFile{ID: fileID("project", "dep/tinycbor/cbor.c")}
	physical := map[string]string{file.ID.Canonical(): filepath.Join(vendor, "cbor.c")}

	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	_, name, _, _, detectedBy := resolver.resolve(file)
	if name != "tinycbor" {
		t.Errorf("component name = %q, want tinycbor", name)
	}
	if detectedBy != "package-metadata:vcpkg.json" {
		t.Errorf("detectedBy = %q, want the manifest that decided it", detectedBy)
	}
}

func TestPackageSearchStopsAtTheAnchorRoot(t *testing.T) {
	outer := t.TempDir()
	// A manifest above the anchor root must not be found: section 22.1 forbids
	// walking out of the component tree.
	if err := os.WriteFile(filepath.Join(outer, "Cargo.toml"), []byte("[package]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(outer, "project")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := domain.UsedFile{ID: fileID("project", "src/main.c")}
	physical := map[string]string{file.ID.Canonical(): filepath.Join(root, "src", "main.c")}

	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	_, name, _, _, detectedBy := resolver.resolve(file)
	if name != "firmware" || detectedBy != "anchor:project" {
		t.Errorf("resolve() = (%s, %s), want the project anchor; the search escaped its root", name, detectedBy)
	}
}

func TestFilesWithNoAnchorBecomeAnUnknownComponent(t *testing.T) {
	file := domain.UsedFile{ID: fileID("abs", "opt/vendor/blob.a")}
	resolver := newComponentResolver(config.Config{}, map[string]string{}, map[string]string{}, nil)

	_, name, _, scope, detectedBy := resolver.resolve(file)
	if name != "unknown:abs/opt" {
		t.Errorf("component name = %q, want unknown:abs/opt (section 19.3)", name)
	}
	if detectedBy != "unresolved" || scope != "unknown" {
		t.Errorf("detectedBy = %q, scope = %q", detectedBy, scope)
	}
}

func TestCuratedMetadataFillsTheCRAFields(t *testing.T) {
	cfg := config.Config{
		Components: []config.Component{{
			Path: "dep/mbedtls", Name: "mbedtls", Version: "3.5.0",
			Supplier: "Trusted Firmware", License: "Apache-2.0",
		}},
	}
	resolver := newComponentResolver(cfg, map[string]string{}, map[string]string{}, nil)
	component := domain.Component{ID: "component:mbedtls", Name: "mbedtls"}
	files := []domain.UsedFile{{ID: fileID("project", "dep/mbedtls/aes.c"), Hashes: map[string]string{"SHA-256": "x"}}}

	findings := resolver.enrichComponent(&component, files)

	if component.Version != "3.5.0" || component.VersionSource != "curated" {
		t.Errorf("version = %q from %q", component.Version, component.VersionSource)
	}
	if component.Supplier != "Trusted Firmware" {
		t.Errorf("supplier = %q", component.Supplier)
	}
	if len(component.Licenses) != 1 || component.Licenses[0].Expression != "Apache-2.0" {
		t.Errorf("licenses = %#v", component.Licenses)
	}
	for _, finding := range findings {
		switch finding.ID {
		case "UNKNOWN_VERSION", "MISSING_SUPPLIER", "UNKNOWN_LICENSE", "MISSING_COMPONENT_HASH":
			t.Errorf("a fully configured component still reported %s", finding.ID)
		}
	}
}

func TestMissingCRAFieldsAreReportedIndividually(t *testing.T) {
	resolver := newComponentResolver(config.Config{}, map[string]string{}, map[string]string{}, nil)
	component := domain.Component{ID: "component:mystery", Name: "mystery"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{{ID: fileID("project", "a.c")}})

	want := map[string]bool{
		"UNKNOWN_VERSION":        false,
		"MISSING_SUPPLIER":       false,
		"UNKNOWN_LICENSE":        false,
		"MISSING_COMPONENT_HASH": false,
	}
	for _, finding := range findings {
		if _, tracked := want[finding.ID]; tracked {
			want[finding.ID] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Errorf("%s was not reported for a component with no metadata", id)
		}
	}
}

func TestLicenseConflictKeepsTheCuratedValueAndRecordsTheOther(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "aes.c")
	if err := os.WriteFile(source, []byte("/* SPDX-License-Identifier: GPL-3.0-only */\nint aes(void){return 0;}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Components: []config.Component{{Path: "dep", Name: "mbedtls", License: "Apache-2.0"}}}
	file := domain.UsedFile{ID: fileID("project", "dep/aes.c")}
	resolver := newComponentResolver(cfg, map[string]string{file.ID.Canonical(): source}, map[string]string{}, nil)
	component := domain.Component{ID: "component:mbedtls", Name: "mbedtls"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{file})

	// Section 22.5: the curated value stays effective, the conflicting one is
	// recorded, and the disagreement is reported rather than resolved.
	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "Apache-2.0" {
		t.Errorf("effective license = %#v, want the curated Apache-2.0", component.Licenses)
	}
	conflicting := component.Properties["sbomb:license:conflictingValue"]
	if len(conflicting) != 1 || conflicting[0] != "GPL-3.0-only" {
		t.Errorf("conflicting value = %v, want GPL-3.0-only", conflicting)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "LICENSE_CONFLICT" {
			reported = true
		}
	}
	if !reported {
		t.Error("the conflict was resolved silently; section 22.5 forbids that")
	}
}

func TestLicenseComesFromTheComponentRootOnly(t *testing.T) {
	root := t.TempDir()
	// A license file above the component root must not be picked up.
	if err := os.WriteFile(filepath.Join(root, "LICENSE"), []byte("SPDX-License-Identifier: GPL-3.0-only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	componentRoot := filepath.Join(root, "dep", "tiny")
	if err := os.MkdirAll(componentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(componentRoot, "LICENSE"), []byte("SPDX-License-Identifier: MIT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(componentRoot, "tiny.c")
	if err := os.WriteFile(source, []byte("int tiny(void){return 0;}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := domain.UsedFile{ID: fileID("project", "dep/tiny/tiny.c")}
	resolver := newComponentResolver(config.Config{}, map[string]string{file.ID.Canonical(): source}, map[string]string{}, nil)
	component := domain.Component{ID: "component:tiny", Name: "tiny"}

	resolver.enrichComponent(&component, []domain.UsedFile{file})

	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "MIT" {
		t.Fatalf("license = %#v, want MIT from the component's own root", component.Licenses)
	}
}

func TestUnrecognizedLicenseTextIsNoAssertion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "LICENSE"), []byte("Do whatever you like.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "a.c")
	if err := os.WriteFile(source, []byte("int a(void){return 0;}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := domain.UsedFile{ID: fileID("project", "a.c")}
	resolver := newComponentResolver(config.Config{}, map[string]string{file.ID.Canonical(): source}, map[string]string{}, nil)
	component := domain.Component{ID: "component:a", Name: "a"}

	resolver.enrichComponent(&component, []domain.UsedFile{file})

	// Section 22.7: never invent a license from weak similarity.
	if len(component.Licenses) == 0 || component.Licenses[0].Name != "NOASSERTION" {
		t.Fatalf("license = %#v, want NOASSERTION", component.Licenses)
	}
	if len(component.Properties["sbomb:license:reason"]) == 0 {
		t.Error("NOASSERTION carries no reason code")
	}
}

func TestPurlIsAssertedOnlyFromAPackageAnchor(t *testing.T) {
	resolver := newComponentResolver(config.Config{}, map[string]string{}, map[string]string{}, nil)

	packaged := domain.Component{ID: "pkg:conan/mbedtls", Name: "mbedtls"}
	resolver.enrichComponent(&packaged, []domain.UsedFile{{ID: fileID("pkg:conan/mbedtls", "aes.c")}})
	if packaged.PURL == "" {
		t.Error("a package anchor should yield a purl")
	}
	// A purl without the scheme is not a purl, and the document validator
	// rejects it. This went unnoticed while no pkg: anchor was ever registered.
	if !strings.HasPrefix(packaged.PURL, "pkg:") {
		t.Errorf("purl = %q, want the pkg: scheme", packaged.PURL)
	}

	plain := domain.Component{ID: "component:project", Name: "project"}
	findings := resolver.enrichComponent(&plain, []domain.UsedFile{{ID: fileID("project", "a.c")}})
	if plain.PURL != "" {
		t.Errorf("purl = %q, want none: no package type can be asserted", plain.PURL)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "UNKNOWN_PURL" {
			reported = true
		}
	}
	if !reported {
		t.Error("UNKNOWN_PURL was not reported")
	}
}

// mitText is a complete MIT licence with a named holder, so that resolution
// succeeds and the tests below observe the root rather than a detection
// failure.
const mitText = `MIT License

Copyright (c) 2024 Example Holder

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`

// write creates a file and the directories above it.
func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A library copied into the source tree has no package manifest and a
// CMakeLists.txt nobody can read without interpreting CMake. Its licence file
// is the only marker it reliably carries, and without it the library is not a
// component at all -- it disappears into the manufacturer's own application.
func TestLicenceFileMarksAComponentThatHasNoManifest(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "third_party", "tinyjson", "LICENSE"), mitText)
	write(t, filepath.Join(root, "third_party", "tinyjson", "include", "tinyjson.h"), "#pragma once\n")

	file := domain.UsedFile{ID: fileID("project", "third_party/tinyjson/include/tinyjson.h")}
	physical := map[string]string{
		file.ID.Canonical(): filepath.Join(root, "third_party", "tinyjson", "include", "tinyjson.h"),
	}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	_, name, _, _, detectedBy := resolver.resolve(file)
	if name != "tinyjson" {
		t.Fatalf("component name = %q, want tinyjson; the library was absorbed into the project", name)
	}
	if detectedBy != "package-metadata:LICENSE" {
		t.Errorf("detectedBy = %q, want the licence file that decided it", detectedBy)
	}
}

// The project's own top-level licence describes the project. Treating it as a
// boundary would rename the project's component after its directory.
func TestTheProjectsOwnLicenceIsNotABoundary(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "LICENSE"), mitText)
	write(t, filepath.Join(root, "src", "main.c"), "int main(void){return 0;}\n")

	file := domain.UsedFile{ID: fileID("project", "src/main.c")}
	physical := map[string]string{file.ID.Canonical(): filepath.Join(root, "src", "main.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	_, name, _, _, detectedBy := resolver.resolve(file)
	if name != "firmware" || detectedBy != "anchor:project" {
		t.Errorf("resolve() = (%s, %s), want the project anchor", name, detectedBy)
	}
}

// A NOTICE is attribution material, not a licence grant. A directory carrying
// only one is not thereby a separate work.
func TestNoticeAloneIsNotABoundary(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "src", "vendorbits", "NOTICE"), "This product includes software.\n")
	write(t, filepath.Join(root, "src", "vendorbits", "helper.c"), "void helper(void){}\n")

	file := domain.UsedFile{ID: fileID("project", "src/vendorbits/helper.c")}
	physical := map[string]string{file.ID.Canonical(): filepath.Join(root, "src", "vendorbits", "helper.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	if _, name, _, _, _ := resolver.resolve(file); name != "firmware" {
		t.Errorf("component name = %q, want firmware; a NOTICE must not define a component", name)
	}
}

// bundledDocument is a CycloneDX SBOM as a dependency ships it, describing
// itself in metadata.component.
const bundledDocument = `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,
  "metadata":{"component":{"type":"library","name":"tinyjson","version":"0.7.0",
    "purl":"pkg:generic/tinyjson@0.7.0","supplier":{"name":"Example Ltd"},
    "licenses":[{"expression":"Apache-2.0"}]}}}`

// A dependency that ships its own SBOM says with it that it is a separate
// piece of software, exactly as a licence file does -- and some of them ship
// nothing else. Without the marker the library disappears into the
// manufacturer's own application.
func TestABundledSBOMMarksAComponentThatHasNoLicenceFile(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "third_party", "tinyjson", "sbom.cdx.json"), bundledDocument)
	write(t, filepath.Join(root, "third_party", "tinyjson", "include", "tinyjson.h"), "#pragma once\n")

	file := domain.UsedFile{ID: fileID("project", "third_party/tinyjson/include/tinyjson.h")}
	physical := map[string]string{
		file.ID.Canonical(): filepath.Join(root, "third_party", "tinyjson", "include", "tinyjson.h"),
	}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	_, name, _, _, detectedBy := resolver.resolve(file)
	// Named after its directory and never after the document: the name settles
	// before the files are grouped, and a reader that changed it would be
	// moving a boundary rather than describing one.
	if name != "tinyjson" {
		t.Fatalf("component name = %q, want tinyjson; the library was absorbed into the project", name)
	}
	if detectedBy != "package-metadata:sbom.cdx.json" {
		t.Errorf("detectedBy = %q, want the document that decided it", detectedBy)
	}
}

// The project's own SBOM at its own root describes the project. Treating it as
// a boundary would rename the project's component after its source directory,
// which is what happens to anyone who keeps this tool's own output there.
func TestTheProjectsOwnSBOMIsNotABoundary(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "sbom.cdx.json"), bundledDocument)
	write(t, filepath.Join(root, "src", "main.c"), "int main(void){return 0;}\n")

	file := domain.UsedFile{ID: fileID("project", "src/main.c")}
	physical := map[string]string{file.ID.Canonical(): filepath.Join(root, "src", "main.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	_, name, _, _, detectedBy := resolver.resolve(file)
	if name != "firmware" || detectedBy != "anchor:project" {
		t.Errorf("resolve() = (%s, %s), want the project anchor", name, detectedBy)
	}
}

// The two halves together, through the real reader rather than a stub: the
// document bounds the component and then describes it, and the four fields a
// copied-in library used to be missing are all there.
func TestAMarkerRootIsDescribedByTheDocumentThatMarkedIt(t *testing.T) {
	root := t.TempDir()
	componentRoot := filepath.Join(root, "third_party", "tinyjson")
	write(t, filepath.Join(componentRoot, "sbom.cdx.json"), bundledDocument)
	source := filepath.Join(componentRoot, "tinyjson.c")
	write(t, source, "int tinyjson(void){return 0;}\n")

	file := domain.UsedFile{ID: fileID("project", "third_party/tinyjson/tinyjson.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source}, map[string]string{"project": root}, nil)
	component := domain.Component{ID: "component:tinyjson", Name: "tinyjson"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{file})

	if component.Version != "0.7.0" || component.VersionSource != "bundled-sbom" {
		t.Errorf("version = %q from %q, want the document's answer and its origin",
			component.Version, component.VersionSource)
	}
	if component.Supplier != "Example Ltd" || component.PURL != "pkg:generic/tinyjson@0.7.0" {
		t.Errorf("supplier/purl = %q/%q, want the document's", component.Supplier, component.PURL)
	}
	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "Apache-2.0" {
		t.Errorf("license = %#v, want the declared Apache-2.0", component.Licenses)
	}
	for _, finding := range findings {
		switch finding.ID {
		case "UNKNOWN_VERSION", "MISSING_SUPPLIER", "UNKNOWN_PURL", "UNKNOWN_LICENSE", "EVIDENCE_UNREADABLE":
			t.Errorf("%s was reported for a component the document described", finding.ID)
		}
	}
	if component.Name != "tinyjson" || component.Root == nil || component.Root.RelPath != "third_party/tinyjson" {
		t.Errorf("component = %q at %#v, want the identity and the root it was grouped under",
			component.Name, component.Root)
	}
}

// The root is where the component begins, not where the surviving files
// happen to sit. This is the defect that made a licence text depend on
// --gc-sections.
func TestComponentRootDoesNotFollowTheUsedFiles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "dep", "mit-lib", "LICENSE"), mitText)
	for _, name := range []string{"a.c", "b.c", "c.c"} {
		write(t, filepath.Join(root, "dep", "mit-lib", "src", name), "void f(void){}\n")
	}

	physical := map[string]string{}
	all := []domain.UsedFile{}
	for _, name := range []string{"a.c", "b.c", "c.c"} {
		file := domain.UsedFile{ID: fileID("project", "dep/mit-lib/src/"+name)}
		physical[file.ID.Canonical()] = filepath.Join(root, "dep", "mit-lib", "src", name)
		all = append(all, file)
	}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	// The linker keeping one source or all three must not move the root.
	for _, files := range [][]domain.UsedFile{all[:1], all} {
		component := &domain.Component{ID: "component:mit-lib", Name: "mit-lib", DetectedBy: "package-metadata:LICENSE"}
		got := resolver.resolveRoot(component, files)
		want := filepath.Join(root, "dep", "mit-lib")
		if got.Physical != want {
			t.Errorf("with %d used file(s): root = %q, want %q", len(files), got.Physical, want)
		}
		if got.ID.Canonical() != "project:dep/mit-lib" {
			t.Errorf("with %d used file(s): root identity = %q", len(files), got.ID.Canonical())
		}
		if got.Source == rootSourceUsedFiles {
			t.Errorf("with %d used file(s): the root was guessed from the used files", len(files))
		}
	}
}

// A licence at the component root resolves for a library whose sources sit one
// level down. Before the root became a resolved fact this was NOASSERTION.
func TestLicenceResolvesFromTheComponentRootNotTheSourceDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "dep", "mit-lib", "LICENSE"), mitText)
	write(t, filepath.Join(root, "dep", "mit-lib", "src", "a.c"), "void f(void){}\n")

	file := domain.UsedFile{ID: fileID("project", "dep/mit-lib/src/a.c")}
	physical := map[string]string{file.ID.Canonical(): filepath.Join(root, "dep", "mit-lib", "src", "a.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	component := &domain.Component{ID: "component:mit-lib", Name: "mit-lib", DetectedBy: "package-metadata:LICENSE"}
	resolver.enrichComponent(component, []domain.UsedFile{file})

	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "MIT" {
		t.Fatalf("licences = %+v, want MIT read from the component root", component.Licenses)
	}
	if got := component.Properties["sbomb:component:root"]; len(got) != 1 || got[0] != "project:dep/mit-lib" {
		t.Errorf("sbomb:component:root = %v, want [project:dep/mit-lib]", got)
	}
}

// When nothing named a root, the fallback is used and says so. Silence would
// leave a guessed root indistinguishable from a resolved one.
func TestGuessedRootIsReported(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "src", "main.c"), "int main(void){return 0;}\n")

	file := domain.UsedFile{ID: fileID("abs", "opt/vendor/blob.c")}
	physical := map[string]string{file.ID.Canonical(): filepath.Join(root, "src", "main.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		physical, map[string]string{"project": root}, nil)

	component := &domain.Component{ID: "unknown:abs/opt", Name: "unknown:abs/opt", DetectedBy: "unresolved"}
	findings := resolver.enrichComponent(component, []domain.UsedFile{file})

	var reported bool
	for _, finding := range findings {
		if finding.ID == "COMPONENT_ROOT_UNRESOLVED" {
			reported = true
		}
	}
	if !reported {
		t.Errorf("findings = %+v, want COMPONENT_ROOT_UNRESOLVED", findings)
	}
}

// Strategy 5: the configuration names a CMake target, and the File API says
// which sources that target owns. Nothing is inferred from where the files sit.
func TestConfiguredCMakeTargetMapsItsSources(t *testing.T) {
	file := domain.UsedFile{ID: fileID("project", "libs/crypto/aes.c")}
	cfg := config.Config{
		Project:    config.Project{Name: "firmware"},
		Components: []config.Component{{Targets: config.StringList{"crypto"}, Type: "library"}},
	}
	resolver := newComponentResolver(cfg, map[string]string{}, map[string]string{}, nil)
	resolver.setTargets(map[string]string{file.ID.Canonical(): "crypto"})

	_, name, componentType, _, detectedBy := resolver.resolve(file)
	if name != "crypto" || componentType != "library" {
		t.Fatalf("resolve() = (%s, %s), want the component the target maps to", name, componentType)
	}
	if detectedBy != "cmake-target:crypto" {
		t.Errorf("detectedBy = %q, want cmake-target:crypto", detectedBy)
	}
}

// A curated path still outranks a target: section 19.2 puts configuration
// first, and both are configuration, so the more specific statement wins.
func TestCuratedPathOutranksATarget(t *testing.T) {
	file := domain.UsedFile{ID: fileID("project", "libs/crypto/aes.c")}
	cfg := config.Config{
		Project: config.Project{Name: "firmware"},
		Components: []config.Component{
			{Targets: config.StringList{"crypto"}, Name: "by-target"},
			{Path: "libs/crypto", Name: "by-path"},
		},
	}
	resolver := newComponentResolver(cfg, map[string]string{}, map[string]string{}, nil)
	resolver.setTargets(map[string]string{file.ID.Canonical(): "crypto"})

	if _, name, _, _, _ := resolver.resolve(file); name != "by-path" {
		t.Errorf("component name = %q, want by-path", name)
	}
}

// A source two targets compile is left unmapped. The build system said two
// things, and choosing one of them would be the guess this tool refuses.
func TestASourceTwoTargetsShareIsNotMapped(t *testing.T) {
	result, err := anchors.Assemble(anchors.Options{
		Flavor: pathmodel.DefaultFlavor(), ProjectRoot: "/src", BuildRoot: "/bd",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := newBuilder(evidence.New(), result, "/bd", "/bd", NewLogger(0, nil))
	model := &cmakeapi.Model{
		SourceRoot: "/src",
		Configurations: []cmakeapi.Configuration{{
			Name: "Debug",
			Targets: []cmakeapi.Target{
				{Name: "app", Sources: []cmakeapi.Source{{Path: "shared.c"}, {Path: "main.c"}}},
				{Name: "tests", Sources: []cmakeapi.Source{{Path: "shared.c"}}},
			},
		}},
	}

	byFile, findings := targetsByFile(model, b)
	if owner, mapped := byFile["project:shared.c"]; mapped {
		t.Errorf("shared.c maps to %q; a contested source must stay unmapped", owner)
	}
	if byFile["project:main.c"] != "app" {
		t.Errorf("main.c maps to %q, want app", byFile["project:main.c"])
	}
	// Dropping it is a decision, and a decision nobody is told about is the
	// silence this report exists to end.
	if len(findings) != 1 || findings[0].ID != "COMPONENT_MAPPING_CONFLICT" {
		t.Fatalf("findings = %+v, want one COMPONENT_MAPPING_CONFLICT", findings)
	}
	if findings[0].Severity != domain.SeverityInfo {
		t.Errorf("severity = %s, want info", findings[0].Severity)
	}
	if findings[0].Subject.Ref != "project:shared.c" {
		t.Errorf("subject = %+v, want the contested source", findings[0].Subject)
	}
	for _, want := range []string{"app", "tests"} {
		if !strings.Contains(findings[0].Message, want) {
			t.Errorf("message %q does not name target %q", findings[0].Message, want)
		}
	}
}

// The same source listed by three targets has three sides, and a message built
// from a map has to be sorted or two runs over one build disagree.
func TestEveryTargetThatClaimedASourceIsNamedOnce(t *testing.T) {
	result, err := anchors.Assemble(anchors.Options{
		Flavor: pathmodel.DefaultFlavor(), ProjectRoot: "/src", BuildRoot: "/bd",
	})
	if err != nil {
		t.Fatal(err)
	}
	model := &cmakeapi.Model{
		SourceRoot: "/src",
		Configurations: []cmakeapi.Configuration{
			{Name: "Debug", Targets: []cmakeapi.Target{
				{Name: "tests", Sources: []cmakeapi.Source{{Path: "shared.c"}}},
				{Name: "app", Sources: []cmakeapi.Source{{Path: "shared.c"}}},
				{Name: "bench", Sources: []cmakeapi.Source{{Path: "shared.c"}}},
			}},
			// A second configuration repeats the same claims; a repetition is
			// not a further side.
			{Name: "Release", Targets: []cmakeapi.Target{
				{Name: "app", Sources: []cmakeapi.Source{{Path: "shared.c"}}},
			}},
		},
	}

	var messages []string
	for run := 0; run < 2; run++ {
		b := newBuilder(evidence.New(), result, "/bd", "/bd", NewLogger(0, nil))
		_, findings := targetsByFile(model, b)
		if len(findings) != 1 {
			t.Fatalf("findings = %+v, want exactly one", findings)
		}
		messages = append(messages, findings[0].Message)
	}
	if messages[0] != messages[1] {
		t.Errorf("two runs disagree:\n%s\n%s", messages[0], messages[1])
	}
	if got := strings.Count(messages[0], `"app"`); got != 1 {
		t.Errorf("target app named %d times in %q, want once", got, messages[0])
	}
	for _, want := range []string{"app", "bench", "tests"} {
		if !strings.Contains(messages[0], want) {
			t.Errorf("message %q does not name target %q", messages[0], want)
		}
	}
}

// One target listing a source in two configurations is not a disagreement.
// Only a second target is.
func TestOneTargetInTwoConfigurationsIsNoConflict(t *testing.T) {
	result, err := anchors.Assemble(anchors.Options{
		Flavor: pathmodel.DefaultFlavor(), ProjectRoot: "/src", BuildRoot: "/bd",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := newBuilder(evidence.New(), result, "/bd", "/bd", NewLogger(0, nil))
	model := &cmakeapi.Model{
		SourceRoot: "/src",
		Configurations: []cmakeapi.Configuration{
			{Name: "Debug", Targets: []cmakeapi.Target{
				{Name: "app", Sources: []cmakeapi.Source{{Path: "shared.c"}}},
			}},
			{Name: "Release", Targets: []cmakeapi.Target{
				{Name: "app", Sources: []cmakeapi.Source{{Path: "shared.c"}}},
			}},
		},
	}

	byFile, findings := targetsByFile(model, b)
	if byFile["project:shared.c"] != "app" {
		t.Errorf("shared.c maps to %q, want app", byFile["project:shared.c"])
	}
	if len(findings) != 0 {
		t.Errorf("findings = %+v, want none", findings)
	}
}

// A source outside the project and the build tree still has an identity: the
// registry anchors it absolutely rather than giving up on it. So it is a file
// like any other, and two targets claiming it are reported like any others.
// The empty-identity check in targetsByFile is a guard, not a quiet exit.
func TestAContestedSourceOutsideEveryTreeIsStillReported(t *testing.T) {
	result, err := anchors.Assemble(anchors.Options{
		Flavor: pathmodel.DefaultFlavor(), ProjectRoot: "/src", BuildRoot: "/bd",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := newBuilder(evidence.New(), result, "/bd", "/bd", NewLogger(0, nil))
	model := &cmakeapi.Model{
		SourceRoot: "/src",
		Configurations: []cmakeapi.Configuration{
			{Name: "Debug", Targets: []cmakeapi.Target{
				{Name: "app", Sources: []cmakeapi.Source{{Path: "/elsewhere/stray.c"}}},
				{Name: "tests", Sources: []cmakeapi.Source{{Path: "/elsewhere/stray.c"}}},
			}},
		},
	}

	byFile, findings := targetsByFile(model, b)
	if len(byFile) != 0 {
		t.Errorf("byFile = %+v, want nothing: a contested source is mapped by neither target", byFile)
	}
	if len(findings) != 1 || findings[0].Subject.Ref != "abs:elsewhere/stray.c" {
		t.Fatalf("findings = %+v, want one naming the absolute identity", findings)
	}
	for _, want := range []string{"app", "tests"} {
		if !strings.Contains(findings[0].Message, want) {
			t.Errorf("message %q does not name target %q", findings[0].Message, want)
		}
	}
}

// TestVersionFromReachesTheDocument nails down the whole chain: the rule list
// is read from the configuration, applied at the component root, and the
// result becomes the component's version. Before the rule list was passed to
// version.Resolve explicitly it never was -- the field it was read from was
// never filled -- so versionFrom had no effect at all.
func TestVersionFromReachesTheDocument(t *testing.T) {
	root := t.TempDir()
	dep := filepath.Join(root, "dep")
	if err := os.MkdirAll(filepath.Join(dep, "include"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "include", "version.h"),
		[]byte("#define MBEDTLS_VERSION_STRING \"3.5.0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dep, "aes.c")
	if err := os.WriteFile(source, []byte("int aes(void){return 0;}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{Components: []config.Component{{
		Path: "dep", Name: "mbedtls",
		VersionFrom: config.StringList{"header:include/version.h:MBEDTLS_VERSION_STRING"},
	}}}
	file := domain.UsedFile{ID: fileID("project", "dep/aes.c")}
	resolver := newComponentResolver(cfg, map[string]string{file.ID.Canonical(): source}, map[string]string{}, nil)
	component := domain.Component{ID: "component:mbedtls", Name: "mbedtls"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{file})

	if component.Version != "3.5.0" || component.VersionSource != "header" {
		t.Fatalf("version = %q from %q, want 3.5.0 from the header rule", component.Version, component.VersionSource)
	}
	for _, finding := range findings {
		if finding.ID == "UNKNOWN_VERSION" {
			t.Errorf("a component whose versionFrom resolved still reported UNKNOWN_VERSION")
		}
	}
}

// TestVersionFromGitWithoutIntrospectionSaysWhatIsMissing pins the honest
// failure: no runner means no version, and the finding names the permission
// rather than leaving the reader to guess why the rule did nothing.
func TestVersionFromGitWithoutIntrospectionSaysWhatIsMissing(t *testing.T) {
	cfg := config.Config{Components: []config.Component{{
		Path: "dep", Name: "mbedtls", VersionFrom: config.StringList{"git"},
	}}}
	resolver := newComponentResolver(cfg, map[string]string{}, map[string]string{}, nil)
	component := domain.Component{ID: "component:mbedtls", Name: "mbedtls"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{{ID: fileID("project", "dep/aes.c")}})

	if component.Version != "" {
		t.Fatalf("version = %q; without introspection the git rule must produce nothing", component.Version)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID != "UNKNOWN_VERSION" {
			continue
		}
		reported = true
		if !strings.Contains(finding.Message, "--allow-introspection=git") {
			t.Errorf("UNKNOWN_VERSION message = %q, want the missing permission named", finding.Message)
		}
	}
	if !reported {
		t.Error("no UNKNOWN_VERSION finding for a component whose only version rule could not be tried")
	}
}

// TestVersionFromGitBlamesThePermissionOnlyWhenItIsMissing keeps that finding
// truthful in the other direction: with the git group enabled, a rule that
// still produced nothing -- here because the component root lies outside every
// registered anchor -- must not tell the reader to enable what is already on.
func TestVersionFromGitBlamesThePermissionOnlyWhenItIsMissing(t *testing.T) {
	cfg := config.Config{Components: []config.Component{{
		Path: "dep", Name: "mbedtls", VersionFrom: config.StringList{"git"},
	}}}
	resolver := newComponentResolver(cfg, map[string]string{}, map[string]string{}, nil)
	resolver.setIntrospection(&exec.Runner{Features: exec.Features{Git: true}}, context.Background())
	component := domain.Component{ID: "component:mbedtls", Name: "mbedtls"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{{ID: fileID("project", "dep/aes.c")}})

	if component.Version != "" {
		t.Fatalf("version = %q; git answered nothing, so nothing may be published", component.Version)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID != "UNKNOWN_VERSION" {
			continue
		}
		reported = true
		if strings.Contains(finding.Message, "--allow-introspection=git") {
			t.Errorf("UNKNOWN_VERSION message = %q, but git introspection was enabled", finding.Message)
		}
	}
	if !reported {
		t.Error("no UNKNOWN_VERSION finding for a component that ended up without a version")
	}
}

// The osPackages group promised to name the distribution package behind a
// system library, and nothing implemented it (deviation D30). What a reader
// gets instead has to be the honest gap rather than silence, so this pins what
// a system library actually reports: no version, no supplier, no purl, and a
// finding for each. It is also the measure the group would have to beat before
// it comes back -- `dpkg -S` answers a name and an architecture, so a component
// built from it would still raise two of these three.
func TestASystemLibrarySaysWhatIsMissingAboutIt(t *testing.T) {
	resolver := newComponentResolver(config.Config{}, map[string]string{}, map[string]string{}, nil)
	component := domain.Component{ID: "anchor:sysroot", Name: "sysroot"}

	findings := resolver.enrichComponent(&component,
		[]domain.UsedFile{{ID: fileID("sysroot", "usr/lib/x86_64-linux-gnu/libssl.so.3")}})

	if component.Version != "" || component.Supplier != "" || component.PURL != "" {
		t.Fatalf("version = %q, supplier = %q, purl = %q; nothing may be invented for a system library",
			component.Version, component.Supplier, component.PURL)
	}
	reported := map[string]bool{}
	for _, finding := range findings {
		reported[finding.ID] = true
	}
	for _, id := range []string{"UNKNOWN_VERSION", "MISSING_SUPPLIER", "UNKNOWN_PURL"} {
		if !reported[id] {
			t.Errorf("a system library reached the document without %s; the gap has to be named", id)
		}
	}
}

// identityFor is the callback setPackages uses to turn a path into an
// identity. The tests state the mapping outright instead of going through the
// anchor registry, so that what is under test is the resolver and not the path
// model. Registering a root and looking a listed file up differ only in what
// they record, which is nothing here, so both sides get the same table.
func identityFor(byPath map[string]domain.FileID) packagePaths {
	resolve := func(path string) domain.FileID { return byPath[path] }
	return packagePaths{register: resolve, lookup: resolve}
}

// FetchContent puts the checkout in _deps/<name>-src and everything CMake
// generated for the package -- a configure_file header, the libraries built
// from it -- in _deps/<name>-build. While only the checkout was a root, such a
// generated header fell through to the project component, which reports it as
// the manufacturer's own code.
func TestAFileUnderASecondPackageRootBelongsToThePackage(t *testing.T) {
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{}, map[string]string{}, nil)
	resolver.setPackages([]pkgmanager.Package{{
		Name:      "tinylog",
		Roots:     []string{"/bd/_deps/tinylog-src", "/bd/_deps/tinylog-build"},
		Manager:   "fetchcontent",
		AnchorKey: "pkg:fetchcontent/tinylog",
	}}, identityFor(map[string]domain.FileID{
		"/bd/_deps/tinylog-src":   fileID("pkg:fetchcontent/tinylog", ""),
		"/bd/_deps/tinylog-build": fileID("build", "_deps/tinylog-build"),
	}))

	generated := domain.UsedFile{ID: fileID("build", "_deps/tinylog-build/tinylog_config.h")}
	_, name, _, _, detectedBy := resolver.resolve(generated)
	if name != "tinylog" || detectedBy != "fetchcontent" {
		t.Fatalf("resolve() = (%s, %s), want the package the build tree belongs to", name, detectedBy)
	}

	// The component still begins at the checkout: that is where the licence
	// and the version are, and a root on the build tree would move both.
	component := domain.Component{ID: "component:tinylog", Name: "tinylog"}
	root := resolver.resolveRoot(&component, []domain.UsedFile{generated})
	if root.Source != "package-manager" || root.ID.Canonical() != "pkg:fetchcontent/tinylog:" {
		t.Errorf("root = %q from %q, want the checkout", root.ID.Canonical(), root.Source)
	}
}

// A vcpkg triplet tree merges every port into one include and one lib
// directory, so no root can separate them -- but vcpkg listed each file it
// installed, and a list beats a prefix.
func TestAnExactFileClaimBeatsALongerRootOfAnotherPackage(t *testing.T) {
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{}, map[string]string{}, nil)
	resolver.setPackages([]pkgmanager.Package{
		{
			Name:    "tinyfmt",
			Roots:   []string{"/bd/vcpkg_installed/x64-linux/share/tinyfmt"},
			Files:   []string{"/bd/vcpkg_installed/x64-linux/include/tinyfmt.h"},
			Manager: "vcpkg",
		},
		{
			// A package whose root is a longer prefix of the same file. Only
			// the exact claim keeps the header with the port that installed it.
			Name:    "umbrella",
			Roots:   []string{"/bd/vcpkg_installed/x64-linux/include"},
			Manager: "vcpkg",
		},
	}, identityFor(map[string]domain.FileID{
		"/bd/vcpkg_installed/x64-linux/share/tinyfmt":     fileID("build", "vcpkg_installed/x64-linux/share/tinyfmt"),
		"/bd/vcpkg_installed/x64-linux/include":           fileID("build", "vcpkg_installed/x64-linux/include"),
		"/bd/vcpkg_installed/x64-linux/include/tinyfmt.h": fileID("build", "vcpkg_installed/x64-linux/include/tinyfmt.h"),
	}))

	header := domain.UsedFile{ID: fileID("build", "vcpkg_installed/x64-linux/include/tinyfmt.h")}
	if _, name, _, _, detectedBy := resolver.resolve(header); name != "tinyfmt" || detectedBy != "vcpkg" {
		t.Errorf("resolve() = (%s, %s), want the port that installed the header", name, detectedBy)
	}
}

// A package's file list names everything the manager installed, of which the
// build used a few. Resolving those paths must not record them: the callback
// that registers a path also remembers where its bytes are, and a triplet tree
// of a hundred thousand files would then be carried around for the sake of the
// three that were used (section 31).
func TestListedFilesAreLookedUpButNotRegistered(t *testing.T) {
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{}, map[string]string{}, nil)
	byPath := map[string]domain.FileID{
		"/bd/vcpkg_installed/x64-linux/share/tinyfmt":     fileID("build", "vcpkg_installed/x64-linux/share/tinyfmt"),
		"/bd/vcpkg_installed/x64-linux/include/tinyfmt.h": fileID("build", "vcpkg_installed/x64-linux/include/tinyfmt.h"),
	}
	registered := []string{}
	resolver.setPackages([]pkgmanager.Package{{
		Name:    "tinyfmt",
		Roots:   []string{"/bd/vcpkg_installed/x64-linux/share/tinyfmt"},
		Files:   []string{"/bd/vcpkg_installed/x64-linux/include/tinyfmt.h"},
		Manager: "vcpkg",
	}}, packagePaths{
		register: func(path string) domain.FileID {
			registered = append(registered, path)
			return byPath[path]
		},
		lookup: func(path string) domain.FileID { return byPath[path] },
	})

	if len(registered) != 1 || registered[0] != "/bd/vcpkg_installed/x64-linux/share/tinyfmt" {
		t.Errorf("registered = %v, want the root alone", registered)
	}
	// The claim still has to work: the header is looked up, only not recorded.
	header := domain.UsedFile{ID: fileID("build", "vcpkg_installed/x64-linux/include/tinyfmt.h")}
	if _, name, _, _, _ := resolver.resolve(header); name != "tinyfmt" {
		t.Errorf("resolve() = %s, want the port that installed the header", name)
	}
}

// Two managers claiming one file said two things, and two statements are no
// statement. The file falls through to the strategies below rather than being
// awarded to whichever package was read first.
func TestAFileTwoPackagesClaimIsMappedByNeither(t *testing.T) {
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{}, map[string]string{}, nil)
	findings := resolver.setPackages([]pkgmanager.Package{
		{Name: "left", Roots: []string{"/bd/left"}, Files: []string{"/bd/shared/util.h"}, Manager: "vcpkg"},
		{Name: "right", Roots: []string{"/bd/right"}, Files: []string{"/bd/shared/util.h"}, Manager: "conan"},
	}, identityFor(map[string]domain.FileID{
		"/bd/left":          fileID("build", "left"),
		"/bd/right":         fileID("build", "right"),
		"/bd/shared/util.h": fileID("build", "shared/util.h"),
	}))

	contested := domain.UsedFile{ID: fileID("build", "shared/util.h")}
	if _, name, _, _, detectedBy := resolver.resolve(contested); name != "firmware" {
		t.Errorf("resolve() = (%s, %s), want the file to fall through to the anchor", name, detectedBy)
	}
	// Falling through is a decision. Debug logging is off in a normal run, so
	// only a finding reaches the person who has to judge the mapping.
	if len(findings) != 1 || findings[0].ID != "COMPONENT_MAPPING_CONFLICT" {
		t.Fatalf("findings = %+v, want one COMPONENT_MAPPING_CONFLICT", findings)
	}
	if findings[0].Subject.Ref != "build:shared/util.h" {
		t.Errorf("subject = %+v, want the contested file", findings[0].Subject)
	}
	for _, want := range []string{"left", "right", "vcpkg", "conan"} {
		if !strings.Contains(findings[0].Message, want) {
			t.Errorf("message %q does not name %q", findings[0].Message, want)
		}
	}
}

// One package listing a file under two of its own roots is not a disagreement
// with anybody, and a package that claims a file nobody else claims is not one
// either.
func TestOnePackageClaimingItsOwnFileIsNoConflict(t *testing.T) {
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{}, map[string]string{}, nil)
	findings := resolver.setPackages([]pkgmanager.Package{
		{Name: "left", Roots: []string{"/bd/left-src", "/bd/left-build"},
			Files: []string{"/bd/shared/util.h"}, Manager: "fetchcontent"},
	}, identityFor(map[string]domain.FileID{
		"/bd/left-src":      fileID("build", "left-src"),
		"/bd/left-build":    fileID("build", "left-build"),
		"/bd/shared/util.h": fileID("build", "shared/util.h"),
	}))
	if len(findings) != 0 {
		t.Errorf("findings = %+v, want none", findings)
	}
	file := domain.UsedFile{ID: fileID("build", "shared/util.h")}
	if _, name, _, _, _ := resolver.resolve(file); name != "left" {
		t.Errorf("resolve() = %s, want the package that listed the file", name)
	}
}

// A listed file that lies under no anchor is not part of this build's identity
// space, so two packages naming it dispute nothing the document could show.
// It was passed over before and it is passed over now, without a word.
func TestAListedFileUnderNoAnchorIsSkippedAndNotReported(t *testing.T) {
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{}, map[string]string{}, nil)
	findings := resolver.setPackages([]pkgmanager.Package{
		{Name: "left", Roots: []string{"/bd/left"}, Files: []string{"/elsewhere/stray.h"}, Manager: "vcpkg"},
		{Name: "right", Roots: []string{"/bd/right"}, Files: []string{"/elsewhere/stray.h"}, Manager: "conan"},
	}, identityFor(map[string]domain.FileID{
		"/bd/left":  fileID("build", "left"),
		"/bd/right": fileID("build", "right"),
	}))
	if len(findings) != 0 {
		t.Errorf("findings = %+v, want none: the file has no identity to dispute", findings)
	}
}

// Section 19.2: the longest prefix wins, so a package nested inside another
// keeps its own files. Several roots per package make equal-length prefixes
// ordinary, which is why the order is total and not merely longest-first.
func TestTheInnerOfTwoNestedPackagesStillWins(t *testing.T) {
	build := func() *componentResolver {
		resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
			map[string]string{}, map[string]string{}, nil)
		resolver.setPackages([]pkgmanager.Package{
			{Name: "outer", Roots: []string{"/src/dir"}, Manager: "submodule"},
			{Name: "inner", Roots: []string{"/src/dir/inner"}, Manager: "submodule"},
		}, identityFor(map[string]domain.FileID{
			"/src/dir":       fileID("project", "dir"),
			"/src/dir/inner": fileID("project", "dir/inner"),
		}))
		return resolver
	}
	file := domain.UsedFile{ID: fileID("project", "dir/inner/inner.c")}
	first, _, _, _, _ := build().resolve(file)
	second, _, _, _, _ := build().resolve(file)
	if first != "component:inner" || second != first {
		t.Errorf("resolve() = %q then %q, want component:inner both times", first, second)
	}
}

// A dependency that was installed but never linked is correctly absent from
// the document. Until now it was absent without a word, and "why is the
// library I installed not in the SBOM" had no answer in the findings.
func TestAPackageNoUsedFileBelongsToIsReported(t *testing.T) {
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{}, map[string]string{}, nil)
	resolver.setPackages([]pkgmanager.Package{
		{Name: "linked", Roots: []string{"/bd/linked"}, Manager: "conan"},
		{Name: "installed-only", Roots: []string{"/bd/installed-only"}, Manager: "conan"},
	}, identityFor(map[string]domain.FileID{
		"/bd/linked":         fileID("build", "linked"),
		"/bd/installed-only": fileID("build", "installed-only"),
	}))

	findings := resolver.unusedPackageFindings([]domain.UsedFile{
		{ID: fileID("build", "linked/aes.c")},
	})
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one", findings)
	}
	if findings[0].ID != "PACKAGE_NOT_LINKED" || findings[0].Subject.Ref != "installed-only" {
		t.Errorf("finding = %+v, want PACKAGE_NOT_LINKED for installed-only", findings[0])
	}
	if findings[0].Severity != domain.SeverityInfo {
		t.Errorf("severity = %q, want info: leaving the package out is correct", findings[0].Severity)
	}
}

// A package whose files the configuration assigned to some other component is
// linked all the same. Deciding this from resolve() would call it unused,
// because curated configuration answers before the package manager does.
func TestACuratedlyMappedPackageIsNotReportedAsUnlinked(t *testing.T) {
	cfg := config.Config{
		Project:    config.Project{Name: "firmware"},
		Components: []config.Component{{Path: "vendor", Name: "vendor-blob"}},
	}
	resolver := newComponentResolver(cfg, map[string]string{}, map[string]string{}, nil)
	resolver.setPackages([]pkgmanager.Package{
		{Name: "tinycbor", Roots: []string{"/src/vendor/tinycbor"}, Manager: "conan"},
	}, identityFor(map[string]domain.FileID{
		"/src/vendor/tinycbor": fileID("project", "vendor/tinycbor"),
	}))

	file := domain.UsedFile{ID: fileID("project", "vendor/tinycbor/cbor.c")}
	if _, name, _, _, _ := resolver.resolve(file); name != "vendor-blob" {
		t.Fatalf("component name = %q, want the curated one", name)
	}
	if findings := resolver.unusedPackageFindings([]domain.UsedFile{file}); len(findings) != 0 {
		t.Errorf("findings = %+v, want none: the package's file was used", findings)
	}
}

// A vcpkg port keeps a directory of its own for its copyright file and for
// nothing else; every header and library it installed sits in the shared
// triplet tree. Whether such a port was linked is therefore only answerable
// from the list it wrote, and asking its roots alone would report a port as
// unused while one of its headers is in the document.
func TestAPackageLinkedOnlyThroughItsFileListIsNotReportedAsUnlinked(t *testing.T) {
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{}, map[string]string{}, nil)
	resolver.setPackages([]pkgmanager.Package{{
		Name:    "tinyfmt",
		Roots:   []string{"/bd/vcpkg_installed/x64-linux/share/tinyfmt"},
		Files:   []string{"/bd/vcpkg_installed/x64-linux/include/tinyfmt.h"},
		Manager: "vcpkg",
	}}, identityFor(map[string]domain.FileID{
		"/bd/vcpkg_installed/x64-linux/share/tinyfmt":     fileID("build", "vcpkg_installed/x64-linux/share/tinyfmt"),
		"/bd/vcpkg_installed/x64-linux/include/tinyfmt.h": fileID("build", "vcpkg_installed/x64-linux/include/tinyfmt.h"),
	}))

	header := domain.UsedFile{ID: fileID("build", "vcpkg_installed/x64-linux/include/tinyfmt.h")}
	if findings := resolver.unusedPackageFindings([]domain.UsedFile{header}); len(findings) != 0 {
		t.Errorf("findings = %+v, want none: the port's own list names the used header", findings)
	}
}

// A package is one dependency however many roots it has, so it is worth one
// finding at most. One per root would say the same thing about the same
// missing component twice.
func TestAPackageWithSeveralRootsIsReportedOnce(t *testing.T) {
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{}, map[string]string{}, nil)
	resolver.setPackages([]pkgmanager.Package{{
		Name:    "tinylog",
		Roots:   []string{"/bd/_deps/tinylog-src", "/bd/_deps/tinylog-build"},
		Manager: "fetchcontent",
	}}, identityFor(map[string]domain.FileID{
		"/bd/_deps/tinylog-src":   fileID("build", "_deps/tinylog-src"),
		"/bd/_deps/tinylog-build": fileID("build", "_deps/tinylog-build"),
	}))

	findings := resolver.unusedPackageFindings([]domain.UsedFile{{ID: fileID("project", "src/main.c")}})
	if len(findings) != 1 || findings[0].Subject.Ref != "tinylog" {
		t.Errorf("findings = %+v, want a single PACKAGE_NOT_LINKED for tinylog", findings)
	}
}

// stubEnrichment stands in for the readers pkgmanager.Enrich will call. It is
// installed in the resolver's own field, so the production code needs no test
// hook of its own -- the same arrangement setPackages uses for packagePaths.
func stubEnrichment(described pkgmanager.Package, findings []domain.Finding, seen *[]pkgmanager.ComponentRoot) func(pkgmanager.ComponentRoot) (pkgmanager.Package, []domain.Finding) {
	return func(root pkgmanager.ComponentRoot) (pkgmanager.Package, []domain.Finding) {
		if seen != nil {
			*seen = append(*seen, root)
		}
		return described, findings
	}
}

// describedPackage is what a reader hands back: claims, each with its origin
// and its rank, and nothing that could widen the used set.
func describedPackage(t *testing.T, values map[pkgmanager.Field]string, rank pkgmanager.Rank) pkgmanager.Package {
	t.Helper()
	var described pkgmanager.Package
	// Written through Take, because that is the only way a claim is ever
	// recorded and the test should not be able to assign past the ranking.
	for _, field := range []pkgmanager.Field{
		pkgmanager.FieldVersion, pkgmanager.FieldSupplier, pkgmanager.FieldPURL, pkgmanager.FieldLicense,
	} {
		value, stated := values[field]
		if !stated {
			continue
		}
		described.Take(field, pkgmanager.Claim{
			Value: value, Source: "stub-manifest", Rank: rank, Confidence: domain.ConfidenceHigh,
		})
	}
	return described
}

// A library copied into the tree is found by the licence file beside it and
// then carries nothing but its directory name. Whatever else lies in that
// directory is read here, which is the gap this interface exists to close.
func TestAMarkerRootIsDescribedByAReader(t *testing.T) {
	root := t.TempDir()
	componentRoot := filepath.Join(root, "dep", "tiny")
	if err := os.MkdirAll(componentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(componentRoot, "LICENSE"), []byte("SPDX-License-Identifier: MIT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(componentRoot, "tiny.c")
	if err := os.WriteFile(source, []byte("int tiny(void){return 0;}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := domain.UsedFile{ID: fileID("project", "dep/tiny/tiny.c")}
	resolver := newComponentResolver(config.Config{}, map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, nil)
	var seen []pkgmanager.ComponentRoot
	resolver.enrich = stubEnrichment(describedPackage(t, map[pkgmanager.Field]string{
		pkgmanager.FieldVersion:  "3.0.1",
		pkgmanager.FieldSupplier: "Example Ltd",
		pkgmanager.FieldPURL:     "pkg:generic/tiny@3.0.1",
		pkgmanager.FieldLicense:  "Apache-2.0",
	}, pkgmanager.RankDeclaredManifest), nil, &seen)
	component := domain.Component{ID: "component:tiny", Name: "tiny"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{file})

	if len(seen) != 1 || seen[0].Path != componentRoot || seen[0].Name != "tiny" {
		t.Fatalf("roots offered = %#v, want the marker directory %q", seen, componentRoot)
	}
	if component.Version != "3.0.1" || component.VersionSource != "stub-manifest" {
		t.Errorf("version = %q from %q, want the reader's answer and its origin",
			component.Version, component.VersionSource)
	}
	if component.Supplier != "Example Ltd" {
		t.Errorf("supplier = %q", component.Supplier)
	}
	if component.PURL != "pkg:generic/tiny@3.0.1" {
		t.Errorf("purl = %q", component.PURL)
	}
	// Section 22.2 puts explicit component metadata above a licence file found
	// in the root, so the declaration wins over the LICENSE beside it.
	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "Apache-2.0" {
		t.Errorf("license = %#v, want the declared Apache-2.0", component.Licenses)
	}
	for _, finding := range findings {
		switch finding.ID {
		case "UNKNOWN_VERSION", "MISSING_SUPPLIER", "UNKNOWN_PURL", "UNKNOWN_LICENSE":
			t.Errorf("%s was reported for a component that was described", finding.ID)
		}
	}
	// A reader describes; it does not move a boundary. The name and the
	// identity settled before the files were grouped and must not have moved.
	if component.Name != "tiny" || component.ID != "component:tiny" {
		t.Errorf("component = %q/%q, want the identity it was grouped under", component.ID, component.Name)
	}
	if component.Root == nil || component.Root.RelPath != "dep/tiny" {
		t.Errorf("root = %#v, want the marker directory", component.Root)
	}
}

// Curated configuration is above every other source (section 20.2), and a
// reader that overtook it would make the file the user wrote advisory.
func TestCuratedMetadataOutranksAReader(t *testing.T) {
	root := t.TempDir()
	componentRoot := filepath.Join(root, "dep", "tiny")
	if err := os.MkdirAll(componentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(componentRoot, "LICENSE"), []byte("SPDX-License-Identifier: MIT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(componentRoot, "tiny.c")
	if err := os.WriteFile(source, []byte("int tiny(void){return 0;}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Matched by glob rather than by path, so the root still comes from the
	// marker and the reader is reached at all.
	cfg := config.Config{Components: []config.Component{{
		Match: "dep/tiny/**", Name: "tiny",
		Version: "9.9.9", Supplier: "Curated Ltd", PURL: "pkg:generic/tiny@9.9.9",
	}}}
	file := domain.UsedFile{ID: fileID("project", "dep/tiny/tiny.c")}
	resolver := newComponentResolver(cfg, map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, nil)
	resolver.enrich = stubEnrichment(describedPackage(t, map[pkgmanager.Field]string{
		pkgmanager.FieldVersion:  "3.0.1",
		pkgmanager.FieldSupplier: "Example Ltd",
		pkgmanager.FieldPURL:     "pkg:generic/tiny@3.0.1",
	}, pkgmanager.RankBundledSBOM), nil, nil)
	component := domain.Component{ID: "component:tiny", Name: "tiny"}

	resolver.enrichComponent(&component, []domain.UsedFile{file})

	if component.Version != "9.9.9" || component.VersionSource != "curated" {
		t.Errorf("version = %q from %q, want the curated value", component.Version, component.VersionSource)
	}
	if component.Supplier != "Curated Ltd" || component.PURL != "pkg:generic/tiny@9.9.9" {
		t.Errorf("supplier/purl = %q/%q, want the curated values", component.Supplier, component.PURL)
	}
}

// versionFrom is the user saying where this component's version is to be read
// from. A reader that answered when the rule found nothing would quietly
// replace an instruction with a guess of its own.
func TestACuratedVersionFromKeepsAReaderOutOfTheVersion(t *testing.T) {
	root := t.TempDir()
	componentRoot := filepath.Join(root, "dep", "tiny")
	if err := os.MkdirAll(componentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(componentRoot, "LICENSE"), []byte("SPDX-License-Identifier: MIT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(componentRoot, "tiny.c")
	if err := os.WriteFile(source, []byte("int tiny(void){return 0;}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Components: []config.Component{{
		Match: "dep/tiny/**", Name: "tiny",
		// A header that is not there: the rule is explicit and answers nothing.
		VersionFrom: config.StringList{"header:version.h:TINY_VERSION"},
	}}}
	file := domain.UsedFile{ID: fileID("project", "dep/tiny/tiny.c")}
	resolver := newComponentResolver(cfg, map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, nil)
	resolver.enrich = stubEnrichment(describedPackage(t, map[pkgmanager.Field]string{
		pkgmanager.FieldVersion: "3.0.1",
	}, pkgmanager.RankBundledSBOM), nil, nil)
	component := domain.Component{ID: "component:tiny", Name: "tiny"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{file})

	if component.Version != "" {
		t.Errorf("version = %q, want none: the user said where to read it and it was not there", component.Version)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "UNKNOWN_VERSION" {
			reported = true
		}
	}
	if !reported {
		t.Error("the version stayed empty without UNKNOWN_VERSION being reported")
	}
}

// A root a manager owns was already described while it was discovered, where
// its claims were ranked against the manager's own. Describing it a second
// time here would rank them against nothing.
func TestARootAPackageManagerOwnsIsNotDescribedAgain(t *testing.T) {
	root := t.TempDir()
	packageRoot := filepath.Join(root, "dep", "tiny")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(packageRoot, "tiny.c")
	if err := os.WriteFile(source, []byte("int tiny(void){return 0;}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := domain.UsedFile{ID: fileID("project", "dep/tiny/tiny.c")}
	resolver := newComponentResolver(config.Config{}, map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, nil)
	var described pkgmanager.Package
	described.Take(pkgmanager.FieldVersion, pkgmanager.Claim{
		Value: "2.0.0", Source: "conan", Rank: pkgmanager.RankInstallState, Confidence: domain.ConfidenceHigh,
	})
	described.Name = "tiny"
	described.Manager = "conan"
	described.Roots = []string{packageRoot}
	resolver.setPackages([]pkgmanager.Package{described},
		identityFor(map[string]domain.FileID{packageRoot: fileID("project", "dep/tiny")}))
	var seen []pkgmanager.ComponentRoot
	resolver.enrich = stubEnrichment(pkgmanager.Package{}, nil, &seen)
	component := domain.Component{ID: "component:tiny", Name: "tiny"}

	resolver.enrichComponent(&component, []domain.UsedFile{file})

	if len(seen) != 0 {
		t.Errorf("roots offered = %#v, want none: the manager's root was described during discovery", seen)
	}
	if component.Version != "2.0.0" {
		t.Errorf("version = %q, want conan's", component.Version)
	}
}

// The important boundary: a root nothing named is the deepest common directory
// of the files that were used, which is a guess. Reading a file there would
// turn that guess into a source.
func TestAGuessedRootIsNeverRead(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src", "a.c")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("int a(void){return 0;}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := domain.UsedFile{ID: fileID("project", "src/a.c")}
	resolver := newComponentResolver(config.Config{}, map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, nil)
	var seen []pkgmanager.ComponentRoot
	resolver.enrich = stubEnrichment(describedPackage(t, map[pkgmanager.Field]string{
		pkgmanager.FieldVersion: "3.0.1",
	}, pkgmanager.RankBundledSBOM), nil, &seen)
	component := domain.Component{ID: "component:a", Name: "a"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{file})

	if len(seen) != 0 {
		t.Errorf("roots offered = %#v, want none: nothing named this root", seen)
	}
	if component.Version != "" {
		t.Errorf("version = %q, want none", component.Version)
	}
	var unresolved bool
	for _, finding := range findings {
		if finding.ID == "COMPONENT_ROOT_UNRESOLVED" {
			unresolved = true
		}
	}
	if !unresolved {
		t.Error("a guessed root stopped being reported as one")
	}
}

// The same boundary with the real reader behind it. A document whose name is
// none of the fixed marker names does not bound a component, so the directory
// stays a guess -- and a guess is never read, however completely the file in it
// would have answered.
func TestAGuessedRootIsNeverReadByTheRealReader(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src", "a.c")
	write(t, source, "int a(void){return 0;}\n")
	write(t, filepath.Join(root, "src", "tinyjson-1.2.cdx.json"), bundledDocument)

	file := domain.UsedFile{ID: fileID("project", "src/a.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source}, map[string]string{"project": root}, nil)
	component := domain.Component{ID: "component:a", Name: "a"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{file})

	if component.Version != "" || component.Supplier != "" {
		t.Errorf("component = %q/%q, want nothing: no marker named this root",
			component.Version, component.Supplier)
	}
	var unresolved bool
	for _, finding := range findings {
		if finding.ID == "COMPONENT_ROOT_UNRESOLVED" {
			unresolved = true
		}
	}
	if !unresolved {
		t.Error("a guessed root stopped being reported as one")
	}
}

// Evidence that could not be read is reported and nothing is invented from it.
func TestAReaderFindingReachesTheReport(t *testing.T) {
	root := t.TempDir()
	componentRoot := filepath.Join(root, "dep", "tiny")
	if err := os.MkdirAll(componentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(componentRoot, "LICENSE"), []byte("SPDX-License-Identifier: MIT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(componentRoot, "tiny.c")
	if err := os.WriteFile(source, []byte("int tiny(void){return 0;}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := domain.UsedFile{ID: fileID("project", "dep/tiny/tiny.c")}
	resolver := newComponentResolver(config.Config{}, map[string]string{file.ID.Canonical(): source},
		map[string]string{"project": root}, nil)
	resolver.enrich = stubEnrichment(pkgmanager.Package{}, []domain.Finding{{
		ID: "INPUT_LIMIT_EXCEEDED", Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: filepath.Join(componentRoot, "manifest")},
		Message: "the manifest is larger than the limit of section 30",
	}}, nil)
	component := domain.Component{ID: "component:tiny", Name: "tiny"}

	findings := resolver.enrichComponent(&component, []domain.UsedFile{file})

	var reported bool
	for _, finding := range findings {
		if finding.ID == "INPUT_LIMIT_EXCEEDED" {
			reported = true
		}
	}
	if !reported {
		t.Error("a reader's finding did not reach the report")
	}
	// Nothing was read, so the licence file beside it still answers and the
	// version is still missing rather than made up.
	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "MIT" {
		t.Errorf("license = %#v, want the MIT of the licence file", component.Licenses)
	}
	if component.Version != "" {
		t.Errorf("version = %q, want none", component.Version)
	}
}

// Describing a package does not link it. A dependency that was installed but
// never linked stays out of the document however well it is described.
func TestADescribedPackageThatNoFileReachesIsStillNotLinked(t *testing.T) {
	root := t.TempDir()
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{}, map[string]string{"project": root}, nil)
	var described pkgmanager.Package
	described.Name = "tinyfmt"
	described.Manager = "vcpkg"
	described.Roots = []string{filepath.Join(root, "dep", "tinyfmt")}
	described.Take(pkgmanager.FieldSupplier, pkgmanager.Claim{
		Value: "Example Ltd", Source: "stub-manifest", Rank: pkgmanager.RankDeclaredManifest,
	})
	resolver.setPackages([]pkgmanager.Package{described}, identityFor(map[string]domain.FileID{
		filepath.Join(root, "dep", "tinyfmt"): fileID("project", "dep/tinyfmt"),
	}))
	elsewhere := domain.UsedFile{ID: fileID("project", "src/main.c")}

	findings := resolver.unusedPackageFindings([]domain.UsedFile{elsewhere})

	var reported bool
	for _, finding := range findings {
		if finding.ID == "PACKAGE_NOT_LINKED" && finding.Subject.Ref == "tinyfmt" {
			reported = true
		}
	}
	if !reported {
		t.Error("a described but unlinked package stopped being reported")
	}
	if _, mapped := resolver.packageFor(elsewhere); mapped {
		t.Error("a described package claimed a file it never listed")
	}
}

// The document is made of the files the evidence chain reached, and describing
// a root does not change which files those are. A library sitting in the tree
// with its licence beside it, that nothing linked, stays absent however
// willingly a reader would describe it -- and it is never even offered, because
// no used file walks up into it.
func TestDescribingRootsAddsNoComponentToTheDocument(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) string {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// One component of the manufacturer's own and one copied-in library, both
	// reached by a used file.
	main := write("src/main.c", "int main(void){return 0;}\n")
	tiny := write("dep/tiny/tiny.c", "int tiny(void){return 0;}\n")
	write("dep/tiny/LICENSE", "SPDX-License-Identifier: MIT\n")
	// And a library nothing linked, laid out exactly like the one that was.
	write("dep/ghost/ghost.c", "int ghost(void){return 0;}\n")
	write("dep/ghost/LICENSE", "SPDX-License-Identifier: MIT\n")

	mainFile := domain.UsedFile{ID: fileID("project", "src/main.c")}
	tinyFile := domain.UsedFile{ID: fileID("project", "dep/tiny/tiny.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{
			mainFile.ID.Canonical(): main,
			tinyFile.ID.Canonical(): tiny,
		}, map[string]string{"project": root}, nil)
	var seen []pkgmanager.ComponentRoot
	resolver.enrich = stubEnrichment(describedPackage(t, map[pkgmanager.Field]string{
		pkgmanager.FieldVersion:  "3.0.1",
		pkgmanager.FieldSupplier: "Example Ltd",
	}, pkgmanager.RankBundledSBOM), nil, &seen)

	groups, _ := groupFilesByComponent(resolver, []domain.UsedFile{mainFile, tinyFile})

	if len(groups) != 2 {
		t.Fatalf("components = %#v, want the two the used files reached", groups)
	}
	for _, group := range groups {
		if strings.Contains(group.component.Name, "ghost") {
			t.Errorf("component %q entered the document without a file reaching it", group.component.Name)
		}
		for _, file := range group.files {
			if strings.Contains(file.ID.RelPath, "ghost") {
				t.Errorf("file %q entered the document without the evidence chain reaching it", file.ID.RelPath)
			}
		}
	}
	// Only the marker root was offered. The manufacturer's own component has a
	// root nothing named, and reading there would turn a guess into a source.
	if len(seen) != 1 || seen[0].Path != filepath.Join(root, "dep", "tiny") {
		t.Errorf("roots offered = %#v, want only the marker root of tiny", seen)
	}
	for _, group := range groups {
		if group.component.Name == "firmware" && group.component.Version == "3.0.1" {
			t.Error("the reader's answer reached a component whose root nothing named")
		}
	}
}

// Section 22.3 recognizes its file names case-insensitively, with an optional
// .txt or .md extension, and recognizes LICENSE-<id>. The code used to carry a
// hand-written list of eleven exact names instead, so a component whose licence
// sat in license.txt or in LICENSE-MIT resolved to NOASSERTION.
func TestALicenceFileIsRecognizedWhateverItsCaseAndExtension(t *testing.T) {
	recognized := []string{
		"LICENSE", "license", "License.txt", "LICENSE.MD",
		"LICENCE", "licence.md",
		"COPYING", "copying.TXT",
		"NOTICE", "COPYRIGHT", "copyright.md",
		"LICENSE-MIT", "license-Apache-2.0.txt",
	}
	for _, name := range recognized {
		if _, ok := recognizedLicenseFile(name); !ok {
			t.Errorf("%q is a licence file of section 22.3 and was not recognized", name)
		}
	}

	// Everything else is a file that happens to sit beside a licence. Reading
	// one would be the keyword heuristic section 22.3 forbids, one directory
	// further out.
	notRecognized := []string{
		"LICENSE-", "LICENSES", "MIT-LICENSE", "LICENSE.rst", "LICENSE.old",
		"NOTICE.c", "copyleft", "licensing.md",
	}
	for _, name := range notRecognized {
		if _, ok := recognizedLicenseFile(name); ok {
			t.Errorf("%q is not a licence file of section 22.3 and was recognized", name)
		}
	}
}

// A directory is read in no defined order, and section 29 requires one
// document per evidence. The order the candidates are consulted in is
// therefore the code's, not the filesystem's.
func TestTheLicenceFilesOfARootAreConsultedInAFixedOrder(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"NOTICE", "COPYING", "LICENSE-MIT", "LICENSE", "README.md", "LICENSE-APACHE"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A project that keeps its texts in a directory of that name states
	// nothing by the name alone, and there is nothing there to read.
	if err := os.Mkdir(filepath.Join(root, "LICENCE"), 0o755); err != nil {
		t.Fatal(err)
	}

	want := []string{"LICENSE", "LICENSE-APACHE", "LICENSE-MIT", "COPYING", "NOTICE"}
	got := licenseFilesIn(root)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("licenceFilesIn = %v, want %v", got, want)
	}
}

// The defect this guards: the component's licence is in the file, and the file
// was skipped because its name was spelled in a way the list did not carry.
func TestAComponentRootResolvesALicenceThroughAnyRecognizedName(t *testing.T) {
	for _, name := range []string{"license.txt", "LICENSE-MIT", "Licence.md"} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, name), []byte("SPDX-License-Identifier: MIT\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		resolver := &componentResolver{}
		found, ok := resolver.licenseFromComponentRoot(root)
		if !ok || found.Expression != "MIT" {
			t.Errorf("licence in %q resolved to %#v, want MIT", name, found)
		}
		if found.Source != name {
			t.Errorf("licence in %q names source %q", name, found.Source)
		}
	}
}
