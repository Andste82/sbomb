package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/example/sbomb/internal/buildinfo"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/generate"
	"github.com/example/sbomb/internal/pathmodel"
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
		return 0, buildinfo.Name + " " + buildinfo.Version + "\n", ""
	}

	// Verbosity may be given before the subcommand. It is consumed here rather
	// than reinjected into the subcommand's arguments, so that subcommands
	// which take no verbosity flag do not reject it as an unknown argument.
	subArgs := args
	verbosity := 0
	for len(subArgs) > 0 {
		level, ok := verbosityFlag(subArgs[0], verbosity)
		if !ok {
			break
		}
		verbosity = level
		subArgs = subArgs[1:]
	}
	if len(subArgs) == 0 {
		return 0, buildinfo.Name + " " + buildinfo.Version + "\n", ""
	}

	switch subArgs[0] {
	case "version", "--version":
		return 0, buildinfo.Name + " " + buildinfo.Version + "\n", ""
	case "schema":
		return handleSchema(subArgs[1:])
	case "generate":
		return handleGenerate(subArgs[1:], verbosity)
	case "explain":
		return handleExplain(subArgs[1:])
	default:
		return 1, "", "usage: sbomb [version|generate|schema|explain]\n"
	}
}

// verbosityFlag reports the new verbosity level for a single verbosity flag.
// It accepts -v, -vvv, -v=N, --verbose and --verbose=N; the second return
// value is false when arg is not a verbosity flag at all.
func verbosityFlag(arg string, current int) (int, bool) {
	switch {
	case arg == "-v" || arg == "--verbose":
		if current == 0 {
			return 1, true
		}
		return current + 1, true
	case strings.HasPrefix(arg, "--verbose="):
		level, err := strconv.Atoi(strings.TrimPrefix(arg, "--verbose="))
		if err != nil {
			return current, false
		}
		return level, true
	case strings.HasPrefix(arg, "-v="):
		level, err := strconv.Atoi(strings.TrimPrefix(arg, "-v="))
		if err != nil {
			return current, false
		}
		return level, true
	case isAllV(arg):
		return current + len(arg) - 1, true
	default:
		return current, false
	}
}

func isInteger(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func isAllV(s string) bool {
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	for _, r := range s[1:] {
		if r != 'v' {
			return false
		}
	}
	return true
}

func handleSchema(args []string) (int, string, string) {
	return 0, config.Schema() + "\n", ""
}

func handleGenerate(args []string, verbosity int) (int, string, string) {
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
		case args[i] == "--verbose" || args[i] == "-v":
			if i+1 < len(args) && isInteger(args[i+1]) {
				v, _ := strconv.Atoi(args[i+1])
				verbosity = v
				i++
			} else if verbosity == 0 {
				verbosity = 1
			} else {
				verbosity++
			}
		case strings.HasPrefix(args[i], "--verbose="):
			v, err := strconv.Atoi(strings.TrimPrefix(args[i], "--verbose="))
			if err == nil {
				verbosity = v
			}
		case strings.HasPrefix(args[i], "-v="):
			v, err := strconv.Atoi(strings.TrimPrefix(args[i], "-v="))
			if err == nil {
				verbosity = v
			}
		case strings.HasPrefix(args[i], "-v") && isAllV(args[i]):
			verbosity += len(args[i]) - 1
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
	var logBuf bytes.Buffer
	var logWriter io.Writer
	if verbosity > 0 {
		logWriter = &logBuf
	}
	cliLogger := generate.NewLogger(verbosity, logWriter)

	cliLogger.Info("Loading configuration (config file: '%s')...", cfgPath)
	loadedCfg := config.Config{}
	if cfgPath != "" {
		var err error
		loadedCfg, err = config.Load(cfgPath)
		if err != nil {
			return 1, logBuf.String(), err.Error() + "\n"
		}
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
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
		return 1, logBuf.String(), "invalid value for --path-flavor: " + pathFlavor + "\n"
	}
	generated, err := generate.RunWithOptions(loadedCfg, buildDir, repro, generate.Options{
		PathFlavor: flavor,
		Verbosity:  verbosity,
		LogWriter:  logWriter,
	})
	if err != nil {
		return 2, logBuf.String(), err.Error() + "\n"
	}
	cliLogger.Info("Writing CycloneDX BOM to '%s'...", output)
	if err := cyclonedx.WriteBOM(output, generated.BOM); err != nil {
		return 2, logBuf.String(), err.Error() + "\n"
	}
	evidencePath := filepath.Join(buildDir, "evidence.json")
	cliLogger.Info("Writing evidence graph dump to '%s'...", evidencePath)
	evidenceFile, err := os.Create(evidencePath)
	if err != nil {
		return 2, logBuf.String(), err.Error() + "\n"
	}
	if err := generated.Graph.Dump(evidenceFile); err != nil {
		evidenceFile.Close()
		return 2, logBuf.String(), err.Error() + "\n"
	}
	if err := evidenceFile.Close(); err != nil {
		return 2, logBuf.String(), err.Error() + "\n"
	}

	cliLogger.Info("Evaluating policy profile '%s'...", policyName)
	cfg := policy.ResolveProfile(policyName)
	waivers, err := policy.LoadWaivers(waiversPath)
	if err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}
	findings := generated.Findings
	now := time.Now().UTC()
	if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
		if seconds, parseErr := strconv.ParseInt(epoch, 10, 64); parseErr == nil {
			now = time.Unix(seconds, 0).UTC()
		}
	}
	res := policy.Evaluate(findings, cfg, waivers, now)
	cliLogger.Info("Policy evaluation complete: %d finding(s) (fail: %v, exit code: %d)", len(res.Findings), res.Fail, res.ExitCode)

	if findingsJSONPath != "" {
		cliLogger.Info("Writing findings JSON to '%s'...", findingsJSONPath)
		if err := policy.WriteFindingsJSON(findingsJSONPath, res.Findings); err != nil {
			return 1, logBuf.String(), err.Error() + "\n"
		}
	}
	if reviewReportPath != "" {
		cliLogger.Info("Writing review report (%s format) to '%s'...", reportFormat, reviewReportPath)
		text := report.RenderText(cfg.Profile, res.Findings, res.ExitCode)
		if strings.EqualFold(reportFormat, "markdown") {
			text = report.RenderMarkdown(cfg.Profile, res.Findings, res.ExitCode)
		}
		if err := os.WriteFile(reviewReportPath, []byte(text), 0o600); err != nil {
			return 1, logBuf.String(), err.Error() + "\n"
		}
	}
	outStr := logBuf.String()
	if res.Fail {
		return 3, outStr, ""
	}
	return 0, outStr, ""
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
