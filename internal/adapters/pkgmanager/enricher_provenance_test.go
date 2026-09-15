package pkgmanager

import (
	"path/filepath"
	"testing"
)

// A submodule whose directory holds no metadata manifest at all. No enricher
// contributed, so nothing may be re-attributed: the purl the adapter built
// stands, and no claim is filed against a source that never spoke.
func TestEnrichmentLeavesPurlAloneWhenNoEnricherSpoke(t *testing.T) {
	pkg := &Package{
		Name:    "tinyusb",
		Manager: "git-submodule",
		VCSURL:  "https://github.com/hathach/tinyusb",
		Commit:  "0af6b52",
		Version: Claim{Value: "0.15.0", Source: "cmake-config-version", Rank: RankInstallState},
		PURL: Claim{
			Value:  GenericPURL("tinyusb", "", "https://github.com/hathach/tinyusb", "0af6b52"),
			Source: "git-submodule",
			Rank:   RankDeclaredManifest,
		},
	}
	before := pkg.PURL

	applyEnrichment(pkg, ComponentRoot{Path: t.TempDir(), Name: "tinyusb"})

	if pkg.PURL != before {
		t.Errorf("purl changed although no enricher contributed: %+v", pkg.PURL)
	}
	for _, superseded := range pkg.Superseded {
		t.Errorf("a claim was filed against %q although no enricher contributed", superseded.Claim.Source)
	}
}

// The same package, with a bundled sbom.yml beside it. The version reaches
// the purl, and both carry the source that really supplied it.
func TestEnrichmentCarriesTheSourceThatSuppliedTheVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, bundledYAMLName), "name: tinyusb\nversion: 0.16.0\n")

	pkg := &Package{
		Name:    "tinyusb",
		Manager: "git-submodule",
		VCSURL:  "https://github.com/hathach/tinyusb",
		Commit:  "0af6b52",
		PURL: Claim{
			Value:  GenericPURL("tinyusb", "", "https://github.com/hathach/tinyusb", "0af6b52"),
			Source: "git-submodule",
			Rank:   RankDeclaredManifest,
		},
	}

	applyEnrichment(pkg, ComponentRoot{Path: dir, Name: "tinyusb"})

	if pkg.Version.Value != "0.16.0" {
		t.Fatalf("version = %q, want 0.16.0", pkg.Version.Value)
	}
	if pkg.PURL.Source != "bundled-sbom" {
		t.Errorf("purl source = %q, want bundled-sbom", pkg.PURL.Source)
	}
	if want := "pkg:generic/tinyusb@0.16.0"; len(pkg.PURL.Value) < len(want) || pkg.PURL.Value[:len(want)] != want {
		t.Errorf("purl = %q, want it to start %q", pkg.PURL.Value, want)
	}
}
