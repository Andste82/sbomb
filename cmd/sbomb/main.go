package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/policy"
	"github.com/example/sbomb/internal/report"
)

func main() {
	code, out, errOut := execute(os.Args[1:])
	if out != "" {
		fmt.Print(out)
	}
	if errOut != "" {
		fmt.Fprint(os.Stderr, errOut)
	}
	os.Exit(code)
}

func execute(args []string) (int, string, string) {
	if len(args) == 0 {
		return 0, "sbomb 0.0.0-milestone13\n", ""
	}

	switch args[0] {
	case "version", "--version":
		return 0, "sbomb 0.0.0-milestone13\n", ""
	case "schema":
		return handleSchema(args[1:])
	case "generate":
		return handleGenerate(args[1:])
	case "explain":
		return handleExplain(args[1:])
	default:
		return 1, "", "usage: sbomb [version|generate|schema|explain]\n"
	}
}

func handleSchema(args []string) (int, string, string) {
	return 0, config.Schema() + "\n", ""
}

func handleGenerate(args []string) (int, string, string) {
	buildDir := ""
	output := ""
	repro := false
	cfgPath := ""
	policyName := "default"
	waiversPath := ""
	findingsJSONPath := ""
	reviewReportPath := ""
	reportFormat := "text"
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--build-dir":
			if i+1 >= len(args) {
				return 1, "", "missing value for --build-dir\n"
			}
			buildDir = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--build-dir="):
			buildDir = strings.TrimPrefix(args[i], "--build-dir=")
		case args[i] == "--output":
			if i+1 >= len(args) {
				return 1, "", "missing value for --output\n"
			}
			output = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--output="):
			output = strings.TrimPrefix(args[i], "--output=")
		case args[i] == "--reproducible":
			repro = true
		case args[i] == "--config":
			if i+1 >= len(args) {
				return 1, "", "missing value for --config\n"
			}
			cfgPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--config="):
			cfgPath = strings.TrimPrefix(args[i], "--config=")
		case args[i] == "--policy":
			if i+1 >= len(args) {
				return 1, "", "missing value for --policy\n"
			}
			policyName = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--policy="):
			policyName = strings.TrimPrefix(args[i], "--policy=")
		case args[i] == "--waivers":
			if i+1 >= len(args) {
				return 1, "", "missing value for --waivers\n"
			}
			waiversPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--waivers="):
			waiversPath = strings.TrimPrefix(args[i], "--waivers=")
		case args[i] == "--findings-json":
			if i+1 >= len(args) {
				return 1, "", "missing value for --findings-json\n"
			}
			findingsJSONPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--findings-json="):
			findingsJSONPath = strings.TrimPrefix(args[i], "--findings-json=")
		case args[i] == "--review-report":
			if i+1 >= len(args) {
				return 1, "", "missing value for --review-report\n"
			}
			reviewReportPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--review-report="):
			reviewReportPath = strings.TrimPrefix(args[i], "--review-report=")
		case args[i] == "--report-format":
			if i+1 >= len(args) {
				return 1, "", "missing value for --report-format\n"
			}
			reportFormat = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--report-format="):
			reportFormat = strings.TrimPrefix(args[i], "--report-format=")
		default:
			if strings.HasPrefix(args[i], "--") {
				return 1, "", fmt.Sprintf("unknown flag: %s\n", args[i])
			}
			return 1, "", fmt.Sprintf("unexpected argument: %s\n", args[i])
		}
	}
	if buildDir == "" {
		return 1, "", "--build-dir is required\n"
	}
	if cfgPath != "" {
		if _, err := config.Load(cfgPath); err != nil {
			return 1, "", err.Error() + "\n"
		}
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return 1, "", err.Error() + "\n"
	}
	if output == "" {
		output = "sbomb.cdx.json"
	}
	if err := cyclonedx.WriteEmpty(output, repro); err != nil {
		return 1, "", err.Error() + "\n"
	}

	cfg := policy.ResolveProfile(policyName)
	waivers, err := policy.LoadWaivers(waiversPath)
	if err != nil {
		return 1, "", err.Error() + "\n"
	}
	findings := []domain.Finding{}
	res := policy.Evaluate(findings, cfg, waivers, time.Now().UTC())
	if findingsJSONPath != "" {
		if err := policy.WriteFindingsJSON(findingsJSONPath, res.Findings); err != nil {
			return 1, "", err.Error() + "\n"
		}
	}
	if reviewReportPath != "" {
		text := report.RenderText(cfg.Profile, res.Findings, res.ExitCode)
		if strings.EqualFold(reportFormat, "markdown") {
			text = report.RenderMarkdown(cfg.Profile, res.Findings, res.ExitCode)
		}
		if err := os.WriteFile(reviewReportPath, []byte(text), 0o600); err != nil {
			return 1, "", err.Error() + "\n"
		}
	}
	if res.Fail {
		return 3, "", ""
	}
	return 0, "", ""
}

func handleExplain(args []string) (int, string, string) {
	fileRef := ""
	component := ""
	bomRef := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--file":
			if i+1 >= len(args) {
				return 1, "", "missing value for --file\n"
			}
			fileRef = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--file="):
			fileRef = strings.TrimPrefix(args[i], "--file=")
		case args[i] == "--component":
			if i+1 >= len(args) {
				return 1, "", "missing value for --component\n"
			}
			component = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--component="):
			component = strings.TrimPrefix(args[i], "--component=")
		case args[i] == "--bom-ref":
			if i+1 >= len(args) {
				return 1, "", "missing value for --bom-ref\n"
			}
			bomRef = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--bom-ref="):
			bomRef = strings.TrimPrefix(args[i], "--bom-ref=")
		default:
			if strings.HasPrefix(args[i], "--") {
				return 1, "", fmt.Sprintf("unknown flag: %s\n", args[i])
			}
			return 1, "", fmt.Sprintf("unexpected argument: %s\n", args[i])
		}
	}
	if fileRef == "" && component == "" && bomRef == "" {
		return 1, "", "one of --file, --component, or --bom-ref is required\n"
	}
	subject := fileRef
	if component != "" {
		subject = component
	}
	if bomRef != "" {
		subject = bomRef
	}
	msg := fmt.Sprintf("explain not yet implemented for %s; this is the milestone-13 CLI stub\n", subject)
	return 0, msg, ""
}

