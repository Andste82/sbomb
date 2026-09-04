package report

import (
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

func TestRenderTextIncludesPolicyAndFindings(t *testing.T) {
	findings := []domain.Finding{{
		ID:       "UNKNOWN_LICENSE",
		Severity: domain.SeverityWarning,
		Subject:  domain.Subject{Kind: "file", Ref: "project:src/main.c"},
		Message:  "No component license could be determined.",
	}}

	text := RenderText("strict", findings, 3)
	if !strings.Contains(text, "Policy profile: strict") {
		t.Fatalf("report missing policy name: %q", text)
	}
	if !strings.Contains(text, "UNKNOWN_LICENSE") {
		t.Fatalf("report missing finding ID: %q", text)
	}
	if !strings.Contains(text, "Exit code: 3") {
		t.Fatalf("report missing exit code: %q", text)
	}
}

func TestRenderExplainIncludesEvidenceChain(t *testing.T) {
	g := evidence.New()
	g.AddNode(domain.Node{ID: "product:demo", Kind: domain.NodeProduct})
	g.AddNode(domain.Node{ID: "artifact:build/demo.elf", Kind: domain.NodeArtifact})
	g.AddNode(domain.Node{ID: "build:demo.o", Kind: domain.NodeObject})
	g.AddNode(domain.Node{ID: "project:src/main.c", Kind: domain.NodeSource})
	g.AddEdge(domain.Edge{From: "product:demo", To: "artifact:build/demo.elf", Type: "product", Strength: "linked", Confidence: domain.ConfidenceHigh, Source: "spec", Adapter: "test"})
	g.AddEdge(domain.Edge{From: "artifact:build/demo.elf", To: "build:demo.o", Type: "link", Strength: "linked", Confidence: domain.ConfidenceHigh, Source: "spec", Adapter: "test"})
	g.AddEdge(domain.Edge{From: "build:demo.o", To: "project:src/main.c", Type: "compile", Strength: "derived", Confidence: domain.ConfidenceHigh, Source: "spec", Adapter: "test"})

	text, err := RenderExplain(g, "project:src/main.c")
	if err != nil {
		t.Fatalf("RenderExplain returned error: %v", err)
	}
	if !strings.Contains(text, "used because:") {
		t.Fatalf("explain output missing chain header: %q", text)
	}
	if !strings.Contains(text, "artifact:build/demo.elf") {
		t.Fatalf("explain output missing artifact in chain: %q", text)
	}
	if !strings.Contains(text, "compile") {
		t.Fatalf("explain output missing compile edge: %q", text)
	}
}

func TestRenderExplainJSONIsStructuredAndDeterministic(t *testing.T) {
	g := evidence.New()
	g.AddNode(domain.Node{ID: "artifact:build/demo.elf", Kind: domain.NodeArtifact})
	g.AddNode(domain.Node{ID: "build:demo.o", Kind: domain.NodeObject})
	g.AddNode(domain.Node{ID: "project:src/main.c", Kind: domain.NodeSource})
	g.AddEdge(domain.Edge{From: "artifact:build/demo.elf", To: "build:demo.o", Type: "link", Strength: "linked", Confidence: domain.ConfidenceHigh, Source: "linker", Adapter: "linker"})
	g.AddEdge(domain.Edge{From: "build:demo.o", To: "project:src/main.c", Type: "compile", Strength: "derived", Confidence: domain.ConfidenceHigh, Source: "compiledb", Adapter: "compiledb"})

	first, err := RenderExplainJSON(g, "project:src/main.c")
	if err != nil {
		t.Fatalf("RenderExplainJSON returned error: %v", err)
	}
	second, err := RenderExplainJSON(g, "project:src/main.c")
	if err != nil {
		t.Fatalf("second RenderExplainJSON returned error: %v", err)
	}
	if first != second {
		t.Fatalf("JSON explain output changed between runs:\n%s\n---\n%s", first, second)
	}
	if !strings.Contains(first, `"subject": "project:src/main.c"`) || !strings.Contains(first, `"type": "compile"`) {
		t.Fatalf("JSON explain output missing expected fields: %q", first)
	}
}
