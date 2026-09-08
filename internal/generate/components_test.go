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
	resolver.setPackages([]pkgmanager.Package{
		{Name: "left", Roots: []string{"/bd/left"}, Files: []string{"/bd/shared/util.h"}, Manager: "vcpkg"},
		{Name: "right", Roots: []string{"/bd/right"}, Files: []string{"/bd/shared/util.h"}, Manager: "vcpkg"},
	}, identityFor(map[string]domain.FileID{
		"/bd/left":          fileID("build", "left"),
		"/bd/right":         fileID("build", "right"),
		"/bd/shared/util.h": fileID("build", "shared/util.h"),
	}))

	contested := domain.UsedFile{ID: fileID("build", "shared/util.h")}
	if _, name, _, _, detectedBy := resolver.resolve(contested); name != "firmware" {
		t.Errorf("resolve() = (%s, %s), want the file to fall through to the anchor", name, detectedBy)
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
