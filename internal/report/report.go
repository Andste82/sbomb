package report

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

func RenderText(profile string, findings []domain.Finding, exitCode int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Policy profile: %s\n", profile)
	fmt.Fprintf(&b, "Exit code: %d\n\n", exitCode)
	if len(findings) == 0 {
		b.WriteString("No findings.\n")
		return b.String()
	}
	for _, f := range findings {
		status := ""
		if f.Waived {
			status = " [waived]"
		}
		fmt.Fprintf(&b, "- %s [%s]%s: %s\n", f.ID, f.Severity, status, f.Message)
		if f.Subject.Ref != "" {
			fmt.Fprintf(&b, "  subject: %s/%s\n", f.Subject.Kind, f.Subject.Ref)
		}
	}
	return b.String()
}

func RenderMarkdown(profile string, findings []domain.Finding, exitCode int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Policy report\n\n")
	fmt.Fprintf(&b, "- Profile: %s\n", profile)
	fmt.Fprintf(&b, "- Exit code: %d\n", exitCode)
	if len(findings) == 0 {
		b.WriteString("\nNo findings.\n")
		return b.String()
	}
	b.WriteString("\n| ID | Severity | Subject | Message |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, f := range findings {
		fmt.Fprintf(&b, "| %s | %s | %s/%s | %s |\n", f.ID, f.Severity, f.Subject.Kind, f.Subject.Ref, f.Message)
	}
	return b.String()
}

func RenderExplain(graph *evidence.Graph, target string) (string, error) {
	if graph == nil {
		return "", fmt.Errorf("no evidence graph available")
	}
	chains := graph.Chains(domain.NodeID(target), 10)
	if len(chains) == 0 {
		return "", fmt.Errorf("no evidence chain for %s", target)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", target)
	b.WriteString("  used because:\n")
	for _, chain := range chains {
		for j := len(chain) - 1; j >= 0; j-- {
			edge := chain[j]
			fmt.Fprintf(&b, "    %s\n", edge.To)
			fmt.Fprintf(&b, "      <- [%s | %s | %s | %s] %s\n", edge.Type, edge.Strength, edge.Confidence, edge.Source, edge.From)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// RenderExplainJSON returns the deterministic machine-readable form of an evidence explanation.
func RenderExplainJSON(graph *evidence.Graph, target string) (string, error) {
	if graph == nil {
		return "", fmt.Errorf("no evidence graph available")
	}
	chains := graph.Chains(domain.NodeID(target), 10)
	if len(chains) == 0 {
		return "", fmt.Errorf("no evidence chain for %s", target)
	}

	type edge struct {
		From       domain.NodeID       `json:"from"`
		To         domain.NodeID       `json:"to"`
		Type       domain.EvidenceType `json:"type"`
		Strength   domain.Strength     `json:"strength"`
		Confidence domain.Confidence   `json:"confidence"`
		Source     string              `json:"source"`
	}
	result := struct {
		Subject string   `json:"subject"`
		Chains  [][]edge `json:"chains"`
	}{Subject: target, Chains: make([][]edge, 0, len(chains))}
	for _, chain := range chains {
		out := make([]edge, 0, len(chain))
		for _, item := range chain {
			out = append(out, edge{From: item.From, To: item.To, Type: item.Type, Strength: item.Strength, Confidence: item.Confidence, Source: item.Source})
		}
		result.Chains = append(result.Chains, out)
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}
