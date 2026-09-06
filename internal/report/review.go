package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/sbomwriter"
)

// ReviewInput is everything the review report of section 34 describes.
type ReviewInput struct {
	Profile    string
	ExitCode   int
	ConfigPath string
	BuildDir   string
	Document   *sbomwriter.Document
	Graph      *evidence.Graph
	Findings   []domain.Finding
	Adapters   []string
	// Chains selects how much evidence is rendered: all, unresolved or none
	// (section 34 point 9).
	Chains string
}

// RenderReview produces the deterministic review report of section 34. The
// order of its nine sections is fixed so that two runs of the same build
// produce byte-identical output and a reviewer always finds the same thing in
// the same place.
func RenderReview(in ReviewInput) string {
	var b strings.Builder

	// 1. Run metadata.
	section(&b, "Run")
	line(&b, "tool", in.Document.Run.ToolName+" "+in.Document.Run.ToolVersion)
	line(&b, "policy profile", in.Profile)
	if in.ConfigPath != "" {
		line(&b, "configuration", in.ConfigPath)
	}
	line(&b, "build directory", in.BuildDir)
	if in.Document.Run.Generator != "" {
		line(&b, "generator", in.Document.Run.Generator)
	}
	if in.Document.Run.BuildConfig != "" {
		line(&b, "build configuration", in.Document.Run.BuildConfig)
	}
	if len(in.Adapters) > 0 {
		adapters := append([]string{}, in.Adapters...)
		sort.Strings(adapters)
		line(&b, "adapters", strings.Join(adapters, ", "))
	}
	line(&b, "reproducible", fmt.Sprintf("%t", in.Document.Run.Reproducible))

	// 2. Final deliverables.
	section(&b, "Deliverable")
	line(&b, "product", in.Document.Product.Name)
	if version := in.Document.Product.Version; version != "" {
		line(&b, "version", version)
	}
	if role := in.Document.Product.Properties["sbomb:artifact:role"]; len(role) > 0 {
		line(&b, "role", role[0])
	}
	for _, node := range in.Graph.Nodes() {
		if node.Kind != domain.NodeArtifact {
			continue
		}
		line(&b, "artifact", strings.TrimPrefix(string(node.ID), "artifact:"))
		if node.Attributes != nil && node.Attributes["discoveredBy"] != "" {
			line(&b, "discovered by", node.Attributes["discoveredBy"])
		}
	}

	// 3. Counts.
	section(&b, "Counts")
	line(&b, "components", fmt.Sprintf("%d", len(in.Document.Components)))
	line(&b, "files", fmt.Sprintf("%d", len(in.Document.Files)))
	for _, entry := range countBy(in.Document.Files, func(f domain.UsedFile) string { return string(f.Class) }) {
		line(&b, "  files "+entry.key, fmt.Sprintf("%d", entry.count))
	}
	edges := in.Graph.Edges()
	line(&b, "evidence edges", fmt.Sprintf("%d", len(edges)))
	for _, entry := range countEdges(edges, func(e domain.Edge) string { return string(e.Type) }) {
		line(&b, "  by type "+entry.key, fmt.Sprintf("%d", entry.count))
	}
	for _, entry := range countEdges(edges, func(e domain.Edge) string { return string(e.Strength) }) {
		line(&b, "  by strength "+entry.key, fmt.Sprintf("%d", entry.count))
	}

	// 4. Components.
	section(&b, "Components")
	fileCounts := map[string]int{}
	for _, relation := range in.Document.Relations {
		fileCounts[relation.From] = len(relation.To)
	}
	for _, component := range in.Document.Components {
		fmt.Fprintf(&b, "- %s\n", component.Name)
		line(&b, "  type", component.Type)
		line(&b, "  version", valueOrDash(component.Version)+versionSuffix(component))
		line(&b, "  supplier", valueOrDash(component.Supplier))
		line(&b, "  license", licenseSummary(component))
		line(&b, "  detected by", valueOrDash(component.DetectedBy))
		line(&b, "  files", fmt.Sprintf("%d", fileCounts[component.ID]))
	}

	// 5. Unresolved items.
	section(&b, "Unresolved")
	unresolved := map[string]int{}
	for _, finding := range in.Findings {
		switch finding.ID {
		case "UNKNOWN_COMPONENT", "UNKNOWN_VERSION", "UNKNOWN_LICENSE", "UNKNOWN_PURL",
			"MISSING_SUPPLIER", "MISSING_FILE_HASH", "MISSING_COMPONENT_HASH",
			"LINKED_OBJECT_SOURCE_UNRESOLVED", "UNANCHORED_FILE", "ARCHIVE_MEMBERS_UNRESOLVED",
			"MISSING_HEADER_DEPENDENCY_EVIDENCE":
			unresolved[finding.ID]++
		}
	}
	if len(unresolved) == 0 {
		line(&b, "none", "")
	}
	for _, key := range sortedKeys(unresolved) {
		line(&b, key, fmt.Sprintf("%d", unresolved[key]))
	}

	// 6. Stale evidence.
	section(&b, "Staleness")
	var stale int
	for _, finding := range in.Findings {
		if finding.ID == "STALE_BUILD_EVIDENCE" || finding.ID == "STALE_CMAKE_CONFIGURATION" {
			stale++
			fmt.Fprintf(&b, "- %s: %s\n", finding.Subject.Ref, finding.Message)
		}
	}
	if stale == 0 {
		line(&b, "none", "")
	}

	// 7. Findings by severity, waived separately.
	section(&b, "Findings")
	for _, severity := range []domain.Severity{domain.SeverityError, domain.SeverityWarning, domain.SeverityInfo} {
		var shown int
		for _, finding := range in.Findings {
			if finding.Severity != severity || finding.Waived {
				continue
			}
			if shown == 0 {
				fmt.Fprintf(&b, "%s\n", severity)
			}
			shown++
			fmt.Fprintf(&b, "- %s: %s\n", finding.ID, finding.Message)
			fmt.Fprintf(&b, "    subject: %s/%s\n", finding.Subject.Kind, finding.Subject.Ref)
			if finding.Remediation != "" {
				fmt.Fprintf(&b, "    remediation: %s\n", finding.Remediation)
			}
		}
	}
	var waived int
	for _, finding := range in.Findings {
		if !finding.Waived {
			continue
		}
		if waived == 0 {
			b.WriteString("waived\n")
		}
		waived++
		fmt.Fprintf(&b, "- %s: %s (%s)\n", finding.ID, finding.Subject.Ref, finding.WaiverReason)
	}

	// 8. Policy result.
	section(&b, "Result")
	line(&b, "policy profile", in.Profile)
	line(&b, "exit code", fmt.Sprintf("%d", in.ExitCode))
	if in.ExitCode == 0 {
		line(&b, "verdict", "pass")
	} else {
		line(&b, "verdict", "fail")
	}

	// 9. Evidence chains.
	section(&b, "Evidence chains")
	renderChains(&b, in)
	return b.String()
}

func renderChains(b *strings.Builder, in ReviewInput) {
	mode := in.Chains
	if mode == "" {
		mode = "unresolved"
	}
	if mode == "none" {
		line(b, "omitted", "--report-chains none")
		return
	}
	subjects := make([]string, 0)
	switch mode {
	case "all":
		for _, file := range in.Document.Files {
			subjects = append(subjects, file.ID.Canonical())
		}
	default:
		seen := map[string]bool{}
		for _, finding := range in.Findings {
			if finding.Subject.Kind != "file" || seen[finding.Subject.Ref] {
				continue
			}
			seen[finding.Subject.Ref] = true
			subjects = append(subjects, finding.Subject.Ref)
		}
	}
	sort.Strings(subjects)
	if len(subjects) == 0 {
		line(b, "none", "")
		return
	}
	for _, subject := range subjects {
		chains := in.Graph.Chains(domain.NodeID(subject), 1)
		if len(chains) == 0 {
			continue
		}
		fmt.Fprintf(b, "%s\n", subject)
		for _, edge := range chains[0] {
			fmt.Fprintf(b, "  <- [%s | %s | %s | %s] %s\n",
				edge.Type, edge.Strength, edge.Confidence, edge.Source, edge.From)
		}
	}
}

func section(b *strings.Builder, title string) {
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "== %s ==\n", title)
}

func line(b *strings.Builder, key, value string) {
	if value == "" {
		fmt.Fprintf(b, "%s\n", key)
		return
	}
	fmt.Fprintf(b, "%-22s %s\n", key+":", value)
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func versionSuffix(component domain.Component) string {
	if component.Version == "" || component.VersionSource == "" {
		return ""
	}
	return fmt.Sprintf(" (%s, confidence %s)", component.VersionSource, component.VersionConf)
}

func licenseSummary(component domain.Component) string {
	if len(component.Licenses) == 0 {
		return "-"
	}
	first := component.Licenses[0]
	value := first.Expression
	if value == "" {
		value = first.Name
	}
	if first.Evidence != "" {
		value += fmt.Sprintf(" (%s)", first.Evidence)
	}
	return value
}

type countEntry struct {
	key   string
	count int
}

func countBy(files []domain.UsedFile, key func(domain.UsedFile) string) []countEntry {
	counts := map[string]int{}
	for _, file := range files {
		counts[key(file)]++
	}
	return sortedEntries(counts)
}

func countEdges(edges []domain.Edge, key func(domain.Edge) string) []countEntry {
	counts := map[string]int{}
	for _, edge := range edges {
		counts[key(edge)]++
	}
	return sortedEntries(counts)
}

func sortedEntries(counts map[string]int) []countEntry {
	entries := make([]countEntry, 0, len(counts))
	for key, count := range counts {
		if key == "" {
			key = "(unset)"
		}
		entries = append(entries, countEntry{key: key, count: count})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	return entries
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
