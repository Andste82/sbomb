package pkgmanager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

func TestESPIDSbomEnricherReadsMetadata(t *testing.T) {
	root := t.TempDir()
	manifest := `name: 'freertos'
version: '10.5.1'
cpe: cpe:2.3:o:amazon:freertos:{}:*:*:*:*:*:*:*
supplier: 'Organization: Espressif Systems (Shanghai) CO LTD'
originator: 'Organization: Amazon Web Services'
description: An open-source RTOS with Espressif patches.
cve-exclude-list:
  - cve: CVE-2024-28115
    reason: MPU ports are not enabled
`
	if err := os.WriteFile(filepath.Join(root, idfSBOMName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	contributions, findings := (espidfsbom{}).Enrich(ComponentRoot{Path: root, Name: "freertos"})
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
	values := map[Field]Claim{}
	var exclusions []CVEExclusion
	for _, contribution := range contributions {
		if contribution.Field != "" {
			values[contribution.Field] = contribution.Claim
		}
		exclusions = append(exclusions, contribution.CVEExclusions...)
	}
	if values[FieldVersion].Value != "10.5.1" || values[FieldVersion].Rank != RankBundledSBOM {
		t.Errorf("version = %#v", values[FieldVersion])
	}
	if values[FieldCPE].Value != "cpe:2.3:o:amazon:freertos:{}:*:*:*:*:*:*:*" {
		t.Errorf("cpe = %#v", values[FieldCPE])
	}
	if values[FieldSupplier].Value != "Espressif Systems (Shanghai) CO LTD" {
		t.Errorf("supplier = %#v", values[FieldSupplier])
	}
	if values[FieldOriginator].Value != "Amazon Web Services" {
		t.Errorf("originator = %#v", values[FieldOriginator])
	}
	if values[FieldDescription].Value == "" {
		t.Error("description is empty")
	}
	if len(exclusions) != 1 || exclusions[0].CVE != "CVE-2024-28115" || exclusions[0].Reason != "MPU ports are not enabled" {
		t.Errorf("cve exclusions = %#v", exclusions)
	}
}

func TestESPIDSbomEnricherRejectsMismatchedName(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, idfSBOMName), []byte("name: other\nversion: 1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contributions, findings := (espidfsbom{}).Enrich(ComponentRoot{Path: root, Name: "freertos"})
	if len(contributions) != 0 {
		t.Fatalf("contributions = %#v", contributions)
	}
	if len(findings) != 1 || findings[0].ID != "COMPONENT_METADATA_MISMATCH" || findings[0].Severity != domain.SeverityWarning {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestESPIDSbomEnricherVersionsGenericSubmodulePURL(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, idfSBOMName), []byte("name: lwip\nversion: 2.1.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg := Package{
		Name:    "lwip",
		Manager: "git-submodule",
		VCSURL:  "https://github.com/espressif/esp-lwip",
		Commit:  "f79221431fa9042b3572d271d687de66da7560c4",
		PURL:    Claim{Value: "pkg:generic/lwip?vcs_url=git%2Bhttps%3A%2F%2Fgithub.com%2Fespressif%2Fesp-lwip%40f79221431fa9042b3572d271d687de66da7560c4", Rank: RankDeclaredManifest},
	}
	if findings := applyEnrichment(&pkg, ComponentRoot{Path: root, Name: "lwip"}); len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
	want := "pkg:generic/lwip@2.1.3?vcs_url=git%2Bhttps%3A%2F%2Fgithub.com%2Fespressif%2Fesp-lwip%40f79221431fa9042b3572d271d687de66da7560c4"
	if pkg.PURL.Value != want {
		t.Errorf("purl = %q, want %q", pkg.PURL.Value, want)
	}
}
