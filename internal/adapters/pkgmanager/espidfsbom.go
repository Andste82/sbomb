package pkgmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// espidfsbom reads the metadata manifest Espressif ships at the root of an
// ESP-IDF component checkout. It enriches a root already established by the
// package or submodule adapter; it never discovers files or creates a root.
type espidfsbom struct{}

func (espidfsbom) Source() string { return "idf-sbom" }

const idfSBOMName = "sbom.yml"

func (espidfsbom) Enrich(root ComponentRoot) ([]Contribution, []domain.Finding) {
	path := filepath.Join(root.Path, idfSBOMName)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, nil
	}
	findings := make([]domain.Finding, 0)
	manifest, ok := (&espidf{}).readYAMLFile(path, maxIDFManifestBytes, idfSBOMName, &findings)
	if !ok {
		return nil, findings
	}
	name := manifest.scalarAt("name")
	if root.Name != "" && name != "" && name != root.Name {
		findings = append(findings, domain.Finding{
			ID: "COMPONENT_METADATA_MISMATCH", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "evidence", Ref: path},
			Message: fmt.Sprintf("ESP-IDF sbom.yml names %q, but the component root is %q", name, root.Name),
		})
		return nil, findings
	}

	claim := func(field Field, value string) Contribution {
		return Contribution{Field: field, Claim: Claim{Value: value, Source: "idf-sbom", Rank: RankBundledSBOM, Confidence: domain.ConfidenceHigh}}
	}
	contributions := make([]Contribution, 0, 5)
	contributions = append(contributions,
		claim(FieldVersion, manifest.scalarAt("version")),
		claim(FieldCPE, manifest.scalarAt("cpe")),
		claim(FieldSupplier, organizationValue(manifest.scalarAt("supplier"))),
		claim(FieldOriginator, organizationValue(manifest.scalarAt("originator"))),
		claim(FieldDescription, manifest.scalarAt("description")),
	)
	for _, item := range manifest.child("cve-exclude-list").itemsOf() {
		cve := item.scalarAt("cve")
		if cve != "" {
			contributions = append(contributions, Contribution{
				CVEExclusions: []CVEExclusion{{CVE: cve, Reason: item.scalarAt("reason")}},
			})
		}
	}
	return contributions, findings
}

func organizationValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "Organization: ")
	value = strings.TrimPrefix(value, "Person: ")
	return strings.TrimSpace(value)
}
