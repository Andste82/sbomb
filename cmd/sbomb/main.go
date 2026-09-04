package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/generate"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/policy"
	"github.com/example/sbomb/internal/report"
)

var version = "0.0.0-milestone16"

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
		return 0, "sbomb " + version + "\n", ""
	}

	switch args[0] {
	case "version", "--version":
		return 0, "sbomb " + version + "\n", ""
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
	pathFlavor := ""
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
		case args[i] == "--path-flavor":
			if i+1 >= len(args) {
				return 1, "", "missing value for --path-flavor\n"
			}
			pathFlavor = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--path-flavor="):
			pathFlavor = strings.TrimPrefix(args[i], "--path-flavor=")
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
	loadedCfg := config.Config{}
	if cfgPath != "" {
		var err error
		loadedCfg, err = config.Load(cfgPath)
		if err != nil {
			return 1, "", err.Error() + "\n"
		}
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return 1, "", err.Error() + "\n"
	}
	if output == "" {
		output = "sbomb.cdx.json"
	}
	flavor := pathmodel.DefaultFlavor()
	switch strings.ToLower(pathFlavor) {
	case "", "default":
	case "posix":
		flavor = pathmodel.PosixFlavor{}
	case "windows":
		flavor = pathmodel.WindowsFlavor{}
	default:
		return 1, "", "invalid value for --path-flavor: " + pathFlavor + "\n"
	}
	generated, err := generate.RunWithOptions(loadedCfg, buildDir, repro, generate.Options{PathFlavor: flavor})
	if err != nil {
		return 2, "", err.Error() + "\n"
	}
	if err := cyclonedx.WriteBOM(output, generated.BOM); err != nil {
		return 2, "", err.Error() + "\n"
	}
	evidencePath := filepath.Join(buildDir, "evidence.json")
	evidenceFile, err := os.Create(evidencePath)
	if err != nil {
		return 2, "", err.Error() + "\n"
	}
	if err := generated.Graph.Dump(evidenceFile); err != nil {
		evidenceFile.Close()
		return 2, "", err.Error() + "\n"
	}
	if err := evidenceFile.Close(); err != nil {
		return 2, "", err.Error() + "\n"
	}

	cfg := policy.ResolveProfile(policyName)
	waivers, err := policy.LoadWaivers(waiversPath)
	if err != nil {
		return 1, "", err.Error() + "\n"
	}
	findings := generated.Findings
	now := time.Now().UTC()
	if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
		if seconds, parseErr := strconv.ParseInt(epoch, 10, 64); parseErr == nil {
			now = time.Unix(seconds, 0).UTC()
		}
	}
	res := policy.Evaluate(findings, cfg, waivers, now)
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
	buildDir := ""
	fileRef := ""
	component := ""
	bomRef := ""
	format := "text"
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
		case args[i] == "--format":
			if i+1 >= len(args) {
				return 1, "", "missing value for --format\n"
			}
			format = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--format="):
			format = strings.TrimPrefix(args[i], "--format=")
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
	if buildDir == "" {
		return 1, "", "--build-dir is required\n"
	}
	g, err := loadEvidenceGraph(buildDir)
	if err != nil {
		return 1, "", err.Error() + "\n"
	}

	var text string
	if strings.EqualFold(format, "json") {
		text, err = report.RenderExplainJSON(g, subject)
	} else if strings.EqualFold(format, "text") {
		text, err = report.RenderExplain(g, subject)
	} else {
		return 1, "", fmt.Sprintf("unsupported explain format: %s\n", format)
	}
	if err != nil {
		return 1, "", err.Error() + "\n"
	}
	if strings.EqualFold(format, "text") && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return 0, text, ""
}

func loadEvidenceGraph(buildDir string) (*evidence.Graph, error) {
	for _, name := range []string{"evidence.json", ".sbomb/evidence.json"} {
		path := filepath.Join(buildDir, name)
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		graph, loadErr := evidence.LoadDump(file)
		_ = file.Close()
		if loadErr != nil {
			return nil, fmt.Errorf("load evidence graph %s: %w", path, loadErr)
		}
		return graph, nil
	}
	return nil, fmt.Errorf("no evidence graph found in %s", buildDir)
}
