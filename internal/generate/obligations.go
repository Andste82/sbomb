package generate

import (
	"fmt"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/foss"
)

// resolveSourceObligations is section 32.6 for one component: what the
// committed list of internal/foss says its licence asks for.
//
// It runs here rather than in the renderer because the answer belongs in the
// document. Section 28.1's rule is that a specified field wins over a
// property, and CycloneDX has no field for an obligation at all, so the datum
// reaches a consumer as `sbomb:component:sourceObligation` or not at all --
// and a notices document is house style that Dependency-Track, sbomqs and ORT
// do not read.
//
// Two restrictions, both deliberate:
//
//   - Distributed components only (section 24.5). A build-time-only code
//     generator distributes nothing, so no obligation of its licence was
//     triggered by this build, and naming one would invite a source request
//     the manufacturer cannot answer (requirement R1).
//   - A resolved licence only. NOASSERTION is already reported by
//     UNKNOWN_LICENSE, and calling it unclassified as well would say the same
//     thing twice in two vocabularies.
func resolveSourceObligations(component *domain.Component) []domain.Finding {
	if component.DistributionRole != domain.RoleDistributed {
		return nil
	}
	expression := licenseExpressionOf(component)
	if expression == "" {
		return nil
	}
	assessment := foss.Assess(expression, component.LinkageForms)
	component.SourceObligations = assessment.Obligations
	for _, obligation := range assessment.Obligations {
		component.Properties = addProperty(component.Properties, "sbomb:component:sourceObligation", obligation)
	}

	findings := []domain.Finding{}
	if assessment.Owed() {
		findings = append(findings, componentFinding("FOSS_SOURCE_OBLIGATION", domain.SeverityInfo, component,
			fmt.Sprintf("the licence %s triggers a source obligation (%s); sbomb names it and does not produce the material",
				expression, strings.Join(assessment.Obligations, ", ")),
			"See source-obligations.txt of --foss-out for what is owed and why."))
	}
	if len(assessment.Unclassified) > 0 {
		findings = append(findings, componentFinding("FOSS_LICENSE_UNCLASSIFIED", domain.SeverityInfo, component,
			fmt.Sprintf("%s is on neither obligation list, so nothing is claimed about it and nothing is ruled out",
				strings.Join(assessment.Unclassified, ", ")),
			"Assess the licence by hand; a permissive classification is not assumed."))
	}
	return findings
}

// licenseExpressionOf is the component's licence as one string, or empty when
// none was resolved. NOASSERTION is "none was resolved" spelled the way
// section 22.7 spells it, and is answered with the empty string here so that
// no caller has to know the spelling.
func licenseExpressionOf(component *domain.Component) string {
	for _, finding := range component.Licenses {
		value := finding.Expression
		if value == "" {
			value = finding.SPDXID
		}
		if value == "" {
			value = finding.Name
		}
		value = strings.TrimSpace(value)
		if value == "" || value == "NOASSERTION" {
			continue
		}
		return value
	}
	return ""
}
