package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
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

	byFile := targetsByFile(model, b)
	if owner, mapped := byFile["project:shared.c"]; mapped {
		t.Errorf("shared.c maps to %q; a contested source must stay unmapped", owner)
	}
	if byFile["project:main.c"] != "app" {
		t.Errorf("main.c maps to %q, want app", byFile["project:main.c"])
	}
}
