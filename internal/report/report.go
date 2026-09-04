package report

import (
	"fmt"
	"strings"

	"github.com/example/sbomb/internal/domain"
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
