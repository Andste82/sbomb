package report

import (
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
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
