package main

import (
	"bytes"
	"errors"
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
	case "validate":
		return handleValidate(subArgs[1:])
	case "evidence":
		return handleEvidence(subArgs[1:])
	default:
		return 1, "", "usage: sbomb [version|generate|validate|evidence|explain|schema]\n"
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
	for _, arg := range args {
		if arg == "--cyclonedx" {
			schema, err := cyclonedx.EmbeddedSchema()
			if err != nil {
				return 1, "", err.Error() + "\n"
			}
			return 0, schema, ""
		}
	}
	return 0, config.Schema() + "\n", ""
}

// handleValidate runs both validation layers over an existing document
// (section 32.5). Either layer failing is exit code 4.
func handleValidate(args []string) (int, string, string) {
	input := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--input":
			if i+1 >= len(args) {
				return 1, "", "missing value for --input\n"
			}
			input = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--input="):
			input = strings.TrimPrefix(args[i], "--input=")
		default:
			return 1, "", fmt.Sprintf("unexpected argument: %s\n", args[i])
		}
	}
	if input == "" {
		return 1, "", "--input is required\n"
	}
	if err := cyclonedx.ValidateFile(input); err != nil {
		return 4, "", err.Error() + "\n"
	}
	return 0, "valid CycloneDX 1.6 document: " + input + "\n", ""
}

// handleEvidence dumps the evidence graph without producing an SBOM, which is
// what section 32.1 offers for inspecting discovery on its own.
func handleEvidence(args []string) (int, string, string) {
	buildDir := ""
	output := ""
	cfgPath := ""
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
		case args[i] == "--config":
			if i+1 >= len(args) {
				return 1, "", "missing value for --config\n"
			}
			cfgPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--config="):
			cfgPath = strings.TrimPrefix(args[i], "--config=")
		default:
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
	generated, err := generate.RunWithOptions(loadedCfg, buildDir, true, generate.Options{PathFlavor: pathmodel.DefaultFlavor()})
	if err != nil && generated.Graph == nil {
		return exitCodeFor(err, 2), "", err.Error() + "\n"
	}
	var buffer bytes.Buffer
	if dumpErr := generated.Graph.Dump(&buffer); dumpErr != nil {
		return 2, "", dumpErr.Error() + "\n"
	}
	if output == "" {
		return 0, buffer.String(), ""
	}
	if writeErr := os.WriteFile(output, buffer.Bytes(), 0o600); writeErr != nil {
		return 2, "", writeErr.Error() + "\n"
	}
	return 0, "", ""
}

// reportBuildDir names the build the way the evidence does, so that a review
// report of the same build is byte-identical wherever it was produced.
func reportBuildDir(cfg config.Config, buildDir string) string {
	if cfg.Build.Dir != "" {
		return cfg.Build.Dir
	}
	return buildDir
}

// policyGateFlags maps a CLI flag onto the policy gate it sets. The flag names
// follow section 32.2; one table keeps flag, configuration key and field from
// drifting apart.
var policyGateFlags = map[string]string{
	"--fail-on-unknown-component":                "unknownComponent",
	"--fail-on-unknown-license":                  "unknownLicense",
	"--fail-on-unknown-version":                  "unknownVersion",
	"--fail-on-missing-supplier":                 "missingSupplier",
	"--fail-on-missing-hash":                     "missingHash",
	"--fail-on-missing-component-hash":           "missingComponentHash",
	"--fail-on-missing-source-for-linked-object": "missingSourceForLinkedObject",
	"--fail-on-stale-build-artifacts":            "staleBuildArtifacts",
	"--fail-on-review-required":                  "reviewRequired",
	"--fail-on-weak-evidence":                    "weakEvidence",
	"--fail-on-missing-header-evidence":          "missingHeaderEvidence",
	"--fail-on-unanchored-file":                  "unanchoredFile",
	"--allow-missing-link-evidence":              "allowMissingLinkEvidence",
	"--include-system-headers":                   "systemHeaders",
	"--include-linker-scripts":                   "linkerScripts",
	"--include-generated-intermediate-files":     "generatedIntermediateFiles",
	"--include-assets":                           "assets",
	"--include-transient-build-artifacts":        "transientBuildArtifacts",
	"--prebuilt-libraries-require-mapping":       "prebuiltLibrariesRequireMapping",
}

var policyScopeFlags = map[string]string{
	"--include-toolchain-runtime":  "includeToolchainRuntime",
	"--system-libraries":           "systemLibraries",
	"--pch-headers":                "pchHeaders",
	"--section-garbage-collection": "sectionGarbageCollection",
}

func flagName(arg string) string {
	name, _, _ := strings.Cut(arg, "=")
	return name
}

func isPolicyGateFlag(arg string) bool {
	_, known := policyGateFlags[flagName(arg)]
	return known
}

func isPolicyScopeFlag(arg string) bool {
	_, known := policyScopeFlags[flagName(arg)]
	return known
}

// parsePolicyGateFlag accepts "--fail-on-x" as true and "--fail-on-x=false"
// as an explicit override, which is what makes turning a profile gate off
// possible from the command line.
func parsePolicyGateFlag(arg string) (string, bool, error) {
	name, value, hasValue := strings.Cut(arg, "=")
	gate := policyGateFlags[name]
	if !hasValue {
		return gate, true, nil
	}
	switch strings.ToLower(value) {
	case "true", "1", "yes":
		return gate, true, nil
	case "false", "0", "no":
		return gate, false, nil
	}
	return "", false, fmt.Errorf("invalid value %q for %s; use true or false", value, name)
}

func parsePolicyScopeFlag(arg string) (string, string, error) {
	name, value, hasValue := strings.Cut(arg, "=")
	if !hasValue || value == "" {
		return "", "", fmt.Errorf("%s requires a value, for example %s=exclude", name, name)
	}
	return policyScopeFlags[name], value, nil
}

// exitCodeFor honours the exit-code precedence of section 32.4 by letting a
// finding carry its own code.
func exitCodeFor(err error, fallback int) int {
	var exit *generate.ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return fallback
}

func handleGenerate(args []string, verbosity int) (int, string, string) {
	buildDir := ""
	output := ""
	repro := false
	cfgPath := ""
	policyName := ""
	profileOverlay := ""
	gateOverrides := map[string]bool{}
	scopeOverrides := map[string]string{}
	headerEvidence := ""
	waiversPath := ""
	findingsJSONPath := ""
	reviewReportPath := ""
	reportFormat := "text"
	reportChains := ""
	pathFlavor := ""
	redactUnanchored := false
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
		case args[i] == "--redact-unanchored-paths":
			redactUnanchored = true
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
		case args[i] == "--profile-overlay":
			if i+1 >= len(args) {
				return 1, "", "missing value for --profile-overlay\n"
			}
			profileOverlay = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--profile-overlay="):
			profileOverlay = strings.TrimPrefix(args[i], "--profile-overlay=")
		case strings.HasPrefix(args[i], "--header-evidence="):
			headerEvidence = strings.TrimPrefix(args[i], "--header-evidence=")
		case isPolicyGateFlag(args[i]):
			name, value, err := parsePolicyGateFlag(args[i])
			if err != nil {
				return 1, "", err.Error() + "\n"
			}
			gateOverrides[name] = value
		case isPolicyScopeFlag(args[i]):
			name, value, err := parsePolicyScopeFlag(args[i])
			if err != nil {
				return 1, "", err.Error() + "\n"
			}
			scopeOverrides[name] = value
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
		case strings.HasPrefix(args[i], "--report-chains="):
			reportChains = strings.TrimPrefix(args[i], "--report-chains=")
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
	// The scope options of section 33.1 are discovery settings, so the policy
	// has to be resolved before generation, not after it.
	policyConfig, err := policy.Resolve(loadedCfg.Policy, policy.Overrides{
		Profile:        policyName,
		Overlay:        profileOverlay,
		Gates:          gateOverrides,
		Scopes:         scopeOverrides,
		HeaderEvidence: headerEvidence,
		WaiversFile:    waiversPath,
	})
	if err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}
	cliLogger.Info("Policy profile '%s' resolved", policyConfig.Profile)

	generated, err := generate.RunWithOptions(loadedCfg, buildDir, repro, generate.Options{
		PathFlavor:            flavor,
		Verbosity:             verbosity,
		LogWriter:             logWriter,
		RedactUnanchoredPaths: redactUnanchored,
		Policy:                policyConfig,
	})
	if err != nil {
		return exitCodeFor(err, 2), logBuf.String(), err.Error() + "\n"
	}
	cliLogger.Info("Writing CycloneDX BOM to '%s'...", output)
	if err := cyclonedx.WriteBOM(output, generated.BOM); err != nil {
		// Either validation layer failing is exit code 4 (section 32.5).
		return 4, logBuf.String(), err.Error() + "\n"
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

	waivers, err := policy.LoadWaivers(policyConfig.WaiversFile)
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
	res := policy.Evaluate(findings, policyConfig, waivers, now)
	cliLogger.Info("Policy evaluation complete: %d finding(s) (fail: %v, exit code: %d)", len(res.Findings), res.Fail, res.ExitCode)

	if findingsJSONPath != "" {
		cliLogger.Info("Writing findings JSON to '%s'...", findingsJSONPath)
		if err := policy.WriteFindingsJSON(findingsJSONPath, res.Findings); err != nil {
			return 1, logBuf.String(), err.Error() + "\n"
		}
	}
	if reviewReportPath != "" {
		cliLogger.Info("Writing review report (%s format) to '%s'...", reportFormat, reviewReportPath)
		text := report.RenderReview(report.ReviewInput{
			Profile:    policyConfig.Profile,
			ExitCode:   res.ExitCode,
			ConfigPath: cfgPath,
			BuildDir:   reportBuildDir(loadedCfg, buildDir),
			Document:   generated.Document,
			Graph:      generated.Graph,
			Findings:   res.Findings,
			Adapters:   generated.Adapters,
			Chains:     reportChains,

			HeaderNarrowing: narrowingForReport(generated.HeaderNarrowing),
		})
		if strings.EqualFold(reportFormat, "markdown") {
			text = report.RenderMarkdown(policyConfig.Profile, res.Findings, res.ExitCode)
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

// narrowingForReport carries the DWARF narrowing counts across the package
// boundary; the report package must not depend on the generator.
func narrowingForReport(counts []generate.NarrowingCount) []report.Narrowing {
	out := make([]report.Narrowing, 0, len(counts))
	for _, entry := range counts {
		out = append(out, report.Narrowing{Component: entry.Component, Count: entry.Count, Headers: entry.Headers})
	}
	return out
}
