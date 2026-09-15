package pkgmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// bundledYAML reads an sbom.yml an upstream ships at the root of a component
// checkout. It is the YAML counterpart of bundledSBOM, which reads the same
// kind of statement out of *.cdx.json and *.spdx.json, and it enriches a root
// already established by another adapter: it never discovers files or creates
// a root.
//
// Espressif's esp-idf-sbom writes this file for every ESP-IDF component, which
// is where the shape below comes from, but nothing here is ESP-IDF's: a flat
// map of the metadata one component states about itself is what any upstream
// would write. The reader is therefore named for the file and not for the tool,
// and claims the source bundled-sbom -- the technique vocabulary of section
// 20.3 already has that entry, and it describes this exactly: a document the
// upstream shipped inside the package.
type bundledYAML struct{}

func (bundledYAML) Source() string { return "bundled-sbom" }

const bundledYAMLName = "sbom.yml"

// bundledYAMLFields are the keys that make a document one of these. sbom.yml is
// a name other tools use as well -- syft and SPDX-YAML among them -- and their
// documents describe a whole dependency tree under keys of their own
// (spdxVersion, packages, artifacts) rather than one component in a flat map.
//
// A name and one field about it is the least that distinguishes the two. Asking
// for all of them would refuse a component that simply has no supplier, and the
// claims below already treat every field as optional.
var bundledYAMLFields = []string{"version", "cpe", "supplier", "originator", "description"}

func (bundledYAML) Enrich(root ComponentRoot) ([]Contribution, []domain.Finding) {
	path := filepath.Join(root.Path, bundledYAMLName)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, nil
	}

	// Nothing is reported until the document is known to be one of ours. A
	// file of this name that another tool wrote is not broken evidence, it is
	// somebody else's, and a warning about it would fail a strict run over a
	// file this reader has no business with. Whatever reads that format can
	// try its luck; the enrichers do not take turns.
	quiet := make([]domain.Finding, 0)
	manifest, ok := (&espidf{}).readYAMLFile(path, maxIDFManifestBytes, bundledYAMLName, &quiet)
	if !ok {
		return nil, nil
	}
	name := manifest.scalarAt("name")
	if name == "" {
		return nil, nil
	}
	described := false
	for _, field := range bundledYAMLFields {
		if manifest.scalarAt(field) != "" {
			described = true
			break
		}
	}
	if !described && len(manifest.child("cve-exclude-list").itemsOf()) == 0 {
		return nil, nil
	}

	// From here the document is ours, so a disagreement about it is worth
	// reporting: the file describes a component other than the one it lies in,
	// and taking its metadata would attribute one component's claims to
	// another.
	findings := make([]domain.Finding, 0)
	if root.Name != "" && name != root.Name {
		findings = append(findings, domain.Finding{
			ID: "COMPONENT_METADATA_MISMATCH", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "evidence", Ref: path},
			Message: fmt.Sprintf("%s names %q, but the component root is %q", bundledYAMLName, name, root.Name),
		})
		return nil, findings
	}

	claim := func(field Field, value string) Contribution {
		return Contribution{Field: field, Claim: Claim{Value: value, Rank: RankBundledSBOM, Confidence: domain.ConfidenceHigh}}
	}
	contributions := make([]Contribution, 0, 5)
	contributions = append(contributions,
		claim(FieldVersion, manifest.scalarAt("version")),
		claim(FieldCPE, manifest.scalarAt("cpe")),
		claim(FieldSupplier, organizationValue(manifest.scalarAt("supplier"))),
		claim(FieldOriginator, organizationValue(manifest.scalarAt("originator"))),
		claim(FieldDescription, manifest.scalarAt("description")),
	)
	// cve-exclude-list is esp-idf-sbom's spelling and no standard, so it is
	// read where it appears and required nowhere.
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

// organizationValue strips the SPDX role prefix an upstream writes in front of
// a supplier or originator, leaving the party itself.
func organizationValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "Organization: ")
	value = strings.TrimPrefix(value, "Person: ")
	return strings.TrimSpace(value)
}
