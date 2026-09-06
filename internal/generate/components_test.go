package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
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
