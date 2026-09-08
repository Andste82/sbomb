package report

import (
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/sbomwriter"
)

func reviewInput(commands []string) ReviewInput {
	return ReviewInput{
		Profile:  "default",
		BuildDir: "/build",
		Document: &sbomwriter.Document{
			Run:     sbomwriter.RunMetadata{ToolName: "sbomb", ToolVersion: "0.0.0-test"},
			Product: domain.Component{Name: "demo"},
		},
		Graph:         evidence.New(),
		Introspection: commands,
	}
}

// Section 9.2 requires every executed command to be visible. The report is
// where a reviewer sees which programs the run consulted.
func TestReviewReportNamesTheCommandsThatRan(t *testing.T) {
	text := RenderReview(reviewInput([]string{
		"git -C /src rev-parse HEAD",
		"ninja -C /build -t deps",
	}))
	for _, command := range []string{"git -C /src rev-parse HEAD", "ninja -C /build -t deps"} {
		if !strings.Contains(text, command) {
			t.Errorf("report does not name %q:\n%s", command, text)
		}
	}
}

// A run that read nothing but files says so, rather than leaving the reader to
// infer it from an absent line.
func TestReviewReportSaysWhenNothingRan(t *testing.T) {
	text := RenderReview(reviewInput(nil))
	if !strings.Contains(text, "introspection:") || !strings.Contains(text, "no command was executed") {
		t.Errorf("report is silent about introspection:\n%s", text)
	}
}

// The report has to be byte-identical between two runs over the same
// evidence, so nothing time-dependent may reach it from a command record.
func TestReviewReportStaysByteIdentical(t *testing.T) {
	input := reviewInput([]string{"git -C /src describe --tags --always --dirty"})
	first := RenderReview(input)
	if second := RenderReview(input); first != second {
		t.Error("two renderings of the same input differ")
	}
	for _, unit := range []string{"ms", "µs", "duration", "elapsed"} {
		if strings.Contains(first, unit) {
			t.Errorf("the report carries %q, which changes between runs:\n%s", unit, first)
		}
	}
}
