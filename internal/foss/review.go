package foss

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// renderReview produces foss-review.txt: the internal record, and one of the
// three files that do not ship with the product.
//
// It is deliberately without a verdict. There is no status column, no OK and
// no compliance judgement anywhere in it: everything the FOSS view finds is
// informational, and no exit code depends on licence content (section 32.6).
//
// Redaction reaches this file, because section 30.7 requires
// --redact-unanchored-paths to apply to the SBOM, the findings JSON and the
// review record equally. It reaches it by construction: every identity here
// comes out of the document, which was redacted during discovery.
func renderReview(m model, s style) string {
	var b strings.Builder
	s.title(&b, "FOSS REVIEW RECORD")
	b.WriteString("\n")
	s.prose(&b, "", "What sbomb saw about the licences of what this build produced. It states no "+
		"verdict: the manufacturer performs the assessment, and this record is the data it is "+
		"performed on. Of the four documents of this directory, THIRD-PARTY-NOTICES.txt is the "+
		"one that ships with the product; this file, foss-review.json and source-obligations.txt "+
		"are internal.")

	s.section(&b, "RUN")
	s.field(&b, "product", productName(m))
	s.field(&b, "policy profile", valueOrDash(m.run.profile))
	s.field(&b, "mode", valueOrDash(m.run.mode))
	s.field(&b, "licence view", "union")
	s.field(&b, "SBOM view", valueOrDash(m.run.headerEvidence))
	s.field(&b, "reproducible", fmt.Sprintf("%t", m.run.reproducible))

	s.section(&b, "DISTRIBUTED COMPONENTS")
	if len(m.distributed) == 0 {
		s.prose(&b, "", "No third-party component is contained in what is distributed.")
	}
	for _, candidate := range m.distributed {
		renderReviewEntry(&b, s, candidate)
	}

	s.section(&b, "BUILD-TIME-ONLY COMPONENTS")
	s.prose(&b, "", "These components are not inside what is distributed: every path from an "+
		"artifact to their files runs through a generator or toolchain edge. Whether their "+
		"licences impose anything at all depends on the individual licence's terms for build "+
		"tools, which is a question sbomb does not answer. They appear in neither "+
		"THIRD-PARTY-NOTICES.txt nor source-obligations.txt.")
	b.WriteString("\n")
	if len(m.buildTimeOnly) == 0 {
		s.prose(&b, "", "None.")
	}
	for _, candidate := range m.buildTimeOnly {
		s.bullet(&b, heading(candidate))
		s.field(&b, "  licence", licenseLine(candidate))
		s.field(&b, "  linkage form", listOrDash(candidate.linkage))
		s.field(&b, "  used files", fmt.Sprintf("%d", candidate.usedFiles))
	}

	renderRoleUnestablished(&b, s, m)
	renderArtifactBreakdown(&b, s, m)

	s.section(&b, "LICENCE VIEW")
	s.prose(&b, "", "The FOSS view is computed with headerEvidence=union whatever view the SBOM "+
		"uses, and this is not configurable: whether a header emitted code is not the licence "+
		"question, whether its interface was used is. The delta below is what the SBOM view "+
		"dropped and this view keeps.")
	b.WriteString("\n")
	s.field(&b, "headers narrowed away", fmt.Sprintf("%d", m.view.NarrowedTotal))
	if len(m.view.Deltas) == 0 {
		s.prose(&b, "", "The two views are identical for this build.")
	}
	for _, delta := range m.view.Deltas {
		s.bullet(&b, fmt.Sprintf("%s: %d header(s), %d additional copyright statement(s)",
			delta.Component, delta.Narrowed, len(delta.Copyrights)))
	}

	renderCompleteness(&b, s, m)
	renderFindings(&b, s, m)
	return b.String()
}

// renderReviewEntry is one component's row: the three attributes of section
// 24.5, what was retained, and the licence-view delta.
func renderReviewEntry(b *strings.Builder, s style, candidate entry) {
	s.bullet(b, heading(candidate))
	s.field(b, "  bom-ref", valueOrDash(candidate.bomRef))
	s.field(b, "  version", valueOrDash(candidate.version))
	s.field(b, "  licence", licenseLine(candidate)+classSuffix(candidate))
	s.field(b, "  origin", valueOrDash(candidate.origin))
	s.field(b, "  distribution role", valueOrDash(candidate.role))
	s.field(b, "  linkage form", listOrDash(candidate.linkage))
	s.field(b, "  modified", candidate.modified+signalSuffix(candidate))
	s.field(b, "  used files", fmt.Sprintf("%d", candidate.usedFiles))
	s.field(b, "  narrowing delta", fmt.Sprintf("%d header(s), %d additional copyright statement(s)",
		candidate.narrowed, candidate.addedCopyrights))
	s.field(b, "  copyright statements", fmt.Sprintf("%d", len(candidate.copyrights)))
	s.field(b, "  source obligation", listOrDash(candidate.obligations))
	if len(candidate.artifacts) == 0 {
		s.field(b, "  retained texts", "none - "+missingTextMarker)
		return
	}
	for _, artifact := range candidate.artifacts {
		// The canonical identity and the digest, which is what makes an entry
		// traceable back to the bytes sbomb read (requirement R11). This file
		// is internal, so an identity belongs in it; the notices document
		// carries none.
		s.field(b, "  retained "+kindLabel(artifact),
			fmt.Sprintf("%s (%s, sha256:%s)", path.Base(artifact.File.RelPath),
				artifact.File.Canonical(), artifact.SHA256))
	}
}

// renderRoleUnestablished names the components the role derivation did not
// answer for.
//
// The section exists so that no component can leave the FOSS outputs without
// being named. THIRD-PARTY-NOTICES.txt lists the distributed components
// (section 32.6) and the section above lists the build-time-only ones; a
// component in neither would otherwise be in no document at all. The case is
// section 24.2's synthetic build-environment grouping, which has no files of
// its own and whose role therefore nothing derives -- and it is exactly the
// component that must not be presented to a customer as a third-party
// component under an unknown licence. Where the entry is not synthetic it is
// a component mapping to look at, which is why the names are here.
//
// It is written only when there is something in it: in the normal run every
// mapped component has a role, and an empty heading would suggest a question
// that was not raised.
func renderRoleUnestablished(b *strings.Builder, s style, m model) {
	if len(m.roleUnestablished) == 0 {
		return
	}
	s.section(b, "COMPONENTS WITH NO DISTRIBUTION ROLE")
	s.prose(b, "", "No chain from an artifact decided whether these components are inside what "+
		"is distributed, so section 24.5 answered neither role for them. They are in neither "+
		"THIRD-PARTY-NOTICES.txt nor source-obligations.txt: an undecided role is not a licence "+
		"obligation. A synthetic grouping of sbomb's own -- build-environment (section 24.2) -- "+
		"is the expected entry here; any other name is a component mapping worth looking at.")
	b.WriteString("\n")
	for _, candidate := range m.roleUnestablished {
		// The name, the identity and what was read about it, and no used-file
		// count: a grouping whose relation names other components rather than
		// files has none, and "1 used file" for a node that groups one
		// component would be a wrong number rather than a missing one.
		s.bullet(b, heading(candidate))
		s.field(b, "  bom-ref", valueOrDash(candidate.bomRef))
		s.field(b, "  licence", licenseLine(candidate))
		s.field(b, "  linkage form", listOrDash(candidate.linkage))
	}
}

// renderArtifactBreakdown answers "which artifact pulled in the LGPL
// component" (decision Q10). There is one notices document per run whatever
// the mode, and the per-artifact detail goes here, because that is the
// question that actually gets asked.
//
// The section is written only in assembly mode: in single-artifact mode it
// would restate the component list under one heading.
func renderArtifactBreakdown(b *strings.Builder, s style, m model) {
	if len(m.byArtifact) == 0 {
		return
	}
	s.section(b, "COMPONENTS PER ARTIFACT")
	s.prose(b, "", "One product, several deliverables. A component several artifacts share "+
		"appears once above and under each artifact here, and its linkage form may differ "+
		"between them -- static in one, header-only in another.")
	b.WriteString("\n")
	for _, artifact := range sortedArtifacts(m.byArtifact) {
		s.bullet(b, artifact)
		for _, name := range m.byArtifact[artifact] {
			s.field(b, "  component", name)
		}
	}
}

// renderCompleteness is the block section 32.6 shows, in absolute numbers.
//
// Percentages are deliberately absent: "86% complete" is a number nobody can
// act on, and "6 of 9" names the three to look at.
func renderCompleteness(b *strings.Builder, s style, m model) {
	s.section(b, "COMPLETENESS")
	total := len(m.distributed)
	withLicense, withText, withCopyright, withVersion, unknownModification := 0, 0, 0, 0, 0
	for _, candidate := range m.distributed {
		if candidate.license != "" {
			withLicense++
		}
		if len(candidate.grants()) > 0 {
			withText++
		}
		if len(candidate.copyrights) > 0 {
			withCopyright++
		}
		if candidate.version != "" {
			withVersion++
		}
		if candidate.modified == string(domain.ModificationUnknown) {
			unknownModification++
		}
	}
	s.countHeader(b)
	s.count(b, "components", fmt.Sprintf("%d", total))
	s.count(b, "with resolved licence", fmt.Sprintf("%d / %d", withLicense, total))
	s.count(b, "with retained licence text", fmt.Sprintf("%d / %d", withText, total))
	s.count(b, "with copyright statements", fmt.Sprintf("%d / %d", withCopyright, total))
	s.count(b, "with resolved version", fmt.Sprintf("%d / %d", withVersion, total))
	s.count(b, "modification status unknown", fmt.Sprintf("%d", unknownModification))
	s.count(b, "licence view vs. SBOM view", fmt.Sprintf("+%d files", m.view.NarrowedTotal))
	s.count(b, "build-time-only components", fmt.Sprintf("%d", len(m.buildTimeOnly)))
	if len(m.roleUnestablished) > 0 {
		// Only when there is one: a zero line here would raise a question the
		// run did not raise. Where it is not zero it belongs in the block,
		// because the block is what a reviewer counts components in.
		s.count(b, "no distribution role", fmt.Sprintf("%d", len(m.roleUnestablished)))
	}
	s.count(b, "unique retained texts", fmt.Sprintf("%d", len(m.texts)))
}

// renderFindings lists what the FOSS view reported, waived findings in a
// section of their own.
//
// A waived finding keeps its reason here and stops failing the build, and it
// changes nothing in THIRD-PARTY-NOTICES.txt (section 26.3). The approver and
// the expiry stay in the waiver file: the finding carries the reason alone.
func renderFindings(b *strings.Builder, s style, m model) {
	s.section(b, "FINDINGS")
	var open, waived int
	for _, finding := range m.findings {
		if finding.Waived {
			waived++
			continue
		}
		open++
		s.bullet(b, fmt.Sprintf("%s [%s] %s: %s", finding.ID, finding.Severity,
			finding.Subject.Ref, finding.Message))
	}
	if open == 0 {
		s.prose(b, "", "None.")
	}
	if waived == 0 {
		return
	}
	b.WriteString("\n")
	s.prose(b, "", "Waived. A waiver annotates rather than deletes: these findings no longer "+
		"fail the build, and THIRD-PARTY-NOTICES.txt is unchanged by them.")
	b.WriteString("\n")
	for _, finding := range m.findings {
		if !finding.Waived {
			continue
		}
		s.bullet(b, fmt.Sprintf("%s [%s] %s: %s (waived: %s)", finding.ID, finding.Severity,
			finding.Subject.Ref, finding.Message, waiverSummary(finding.Waiver)))
	}
}

func classSuffix(candidate entry) string {
	if candidate.licenseClass == "" {
		return ""
	}
	return " (" + candidate.licenseClass + ")"
}

func signalSuffix(candidate entry) string {
	if candidate.modifiedSignal == "" {
		return ""
	}
	return " (" + candidate.modifiedSignal + ")"
}

// sortedArtifacts is the deliverables of the breakdown, in a fixed order.
func sortedArtifacts(byArtifact map[string][]string) []string {
	out := make([]string, 0, len(byArtifact))
	for artifact := range byArtifact {
		out = append(out, artifact)
	}
	sort.Strings(out)
	return out
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func listOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	sorted := append([]string{}, values...)
	sort.Strings(sorted)
	return strings.Join(sorted, ", ")
}

// waiverSummary renders what the waiver said: the reason, then who accepted it
// and until when where the waiver states them. This record is the one an
// auditor reads, so the approver belongs in it -- a reason is an assertion and
// the approver is the person answering for it (section 26.1).
func waiverSummary(record *domain.WaiverRecord) string {
	if record == nil {
		return "-"
	}
	parts := make([]string, 0, 3)
	if record.Reason != "" {
		parts = append(parts, record.Reason)
	}
	if record.ApprovedBy != "" {
		parts = append(parts, "approved by "+record.ApprovedBy)
	}
	if record.Expires != "" {
		parts = append(parts, "expires "+record.Expires)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "; ")
}
