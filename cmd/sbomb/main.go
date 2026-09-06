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
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/generate"
	"github.com/example/sbomb/internal/limits"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/policy"
	"github.com/example/sbomb/internal/report"
	"github.com/example/sbomb/internal/sbomwriter"
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
	case "self":
		return handleSelf(subArgs[1:], verbosity)
	default:
		return 1, "", "usage: sbomb [version|generate|self|validate|evidence|explain|schema]\n"
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
	cyclone := false
	specVersion := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--cyclonedx":
			cyclone = true
		case args[i] == "--spec-version":
			if i+1 >= len(args) {
				return 1, "", "missing value for --spec-version\n"
			}
			specVersion = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--spec-version="):
			specVersion = strings.TrimPrefix(args[i], "--spec-version=")
		}
	}
	if !cyclone {
		if specVersion != "" {
			return 1, "", "--spec-version selects a CycloneDX schema and needs --cyclonedx\n"
		}
		return 0, config.Schema() + "\n", ""
	}
	// Which schema a document is checked against is the question this answers,
	// so it has to be answerable for either version.
	schema, err := cyclonedx.EmbeddedSchema(specVersion)
	if err != nil {
		return 1, "", err.Error() + "\n"
	}
	return 0, schema, ""
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
	data, err := os.ReadFile(input)
	if err != nil {
		return 4, "", err.Error() + "\n"
	}
	// The format is detected rather than assumed or asked for. Somebody
	// checking a file another tool sent them knows they have an SBOM, not
	// which serialization it is in, and the document says so itself.
	writer, specVersion, err := sbomwriter.DetectFormat(data)
	if err != nil {
		return 4, "", err.Error() + "\n"
	}
	if err := writer.Validate(bytes.NewReader(data)); err != nil {
		return 4, "", err.Error() + "\n"
	}
	return 0, fmt.Sprintf("valid %s %s document: %s\n", formatLabel(writer.ID()), specVersion, input), ""
}

// formatLabel names a format the way a person writes it, rather than by the
// identifier the registry keys on.
func formatLabel(id string) string {
	if id == "cyclonedx-json" {
		return "CycloneDX"
	}
	return id
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
	allowIntrospection := false
	sourceDir := ""
	mode := ""
	specVersion := ""
	configName := ""
	mapPath := ""
	linkDepfile := ""
	imageManifests := []string{}
	// The evidence dump has always been written to <build-dir>/evidence.json,
	// which is where "explain" looks for it. The path is selectable now, and
	// "off" suppresses it, so a run can leave the build directory untouched.
	evidenceDump := ""
	var introspectionGroups []string
	var bounds limits.Config
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
		case args[i] == "--strict-symlinks":
			bounds.StrictSymlinks = true
		case strings.HasPrefix(args[i], "--max-input-size="):
			value := strings.TrimPrefix(args[i], "--max-input-size=")
			size, parseErr := parseSize(value)
			if parseErr != nil {
				return 1, "", fmt.Sprintf("invalid --max-input-size %q: %v\n", value, parseErr)
			}
			bounds.MaxInput = size
		case args[i] == "--source-dir":
			if i+1 >= len(args) {
				return 1, "", "missing value for --source-dir\n"
			}
			sourceDir = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--source-dir="):
			sourceDir = strings.TrimPrefix(args[i], "--source-dir=")
		case args[i] == "--mode":
			if i+1 >= len(args) {
				return 1, "", "missing value for --mode\n"
			}
			mode = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--mode="):
			mode = strings.TrimPrefix(args[i], "--mode=")
		case args[i] == "--spec-version":
			if i+1 >= len(args) {
				return 1, "", "missing value for --spec-version\n"
			}
			specVersion = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--spec-version="):
			specVersion = strings.TrimPrefix(args[i], "--spec-version=")
		case args[i] == "--config-name":
			if i+1 >= len(args) {
				return 1, "", "missing value for --config-name\n"
			}
			configName = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--config-name="):
			configName = strings.TrimPrefix(args[i], "--config-name=")
		case args[i] == "--map":
			if i+1 >= len(args) {
				return 1, "", "missing value for --map\n"
			}
			mapPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--map="):
			mapPath = strings.TrimPrefix(args[i], "--map=")
		case args[i] == "--link-depfile":
			if i+1 >= len(args) {
				return 1, "", "missing value for --link-depfile\n"
			}
			linkDepfile = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--link-depfile="):
			linkDepfile = strings.TrimPrefix(args[i], "--link-depfile=")
		case args[i] == "--image-manifest":
			if i+1 >= len(args) {
				return 1, "", "missing value for --image-manifest\n"
			}
			imageManifests = append(imageManifests, args[i+1])
			i++
		case strings.HasPrefix(args[i], "--image-manifest="):
			imageManifests = append(imageManifests, strings.TrimPrefix(args[i], "--image-manifest="))
		case args[i] == "--evidence-dump":
			if i+1 >= len(args) {
				return 1, "", "missing value for --evidence-dump\n"
			}
			evidenceDump = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--evidence-dump="):
			evidenceDump = strings.TrimPrefix(args[i], "--evidence-dump=")
		case args[i] == "--allow-introspection":
			allowIntrospection = true
		case strings.HasPrefix(args[i], "--allow-introspection="):
			value := strings.TrimPrefix(args[i], "--allow-introspection=")
			if value == "false" {
				introspectionGroups, allowIntrospection = nil, false
				break
			}
			introspectionGroups = strings.Split(value, ",")
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
	// A version no writer emits is a usage error, refused here rather than at
	// the write: nothing should be read, created or discovered on the strength
	// of an argument that cannot be honoured.
	if specVersion != "" {
		if _, _, err := sbomwriter.Resolve("cyclonedx-json", specVersion); err != nil {
			return 1, "", err.Error() + "\n"
		}
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
	// Command line over configuration, per section 32.2.
	if sourceDir != "" {
		loadedCfg.Project.Root = sourceDir
	}
	if mode != "" {
		if mode != "single" && mode != "assembly" {
			return 1, logBuf.String(), "invalid value for --mode: " + mode + "\n"
		}
		loadedCfg.Mode = mode
	}
	if configName != "" {
		loadedCfg.Build.Config = configName
	}
	// output.specVersion is read from the configuration, and the flag overrides
	// it, so a project that always wants 1.7 says so once. The configuration's
	// own value was checked by the loader.
	if specVersion != "" {
		loadedCfg.Output.SpecVersion = specVersion
	}
	loadedCfg.Manifests = append(loadedCfg.Manifests, imageManifests...)

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

	// A project that wants comparable SBOMs wants them from every run, not
	// only from the ones where somebody passed the flag, so the configuration
	// can ask for it. The flag still forces it on for a single run; neither
	// can turn the other off.
	repro = repro || loadedCfg.Output.Reproducible

	introspection, err := resolveIntrospection(loadedCfg, allowIntrospection, introspectionGroups)
	if err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}
	if introspection.Enabled() {
		cliLogger.Info("Introspection enabled: %s", strings.Join(exec.Allowlist(), "; "))
	}

	generated, err := generate.RunWithOptions(loadedCfg, buildDir, repro, generate.Options{
		PathFlavor:            flavor,
		Verbosity:             verbosity,
		LogWriter:             logWriter,
		RedactUnanchoredPaths: redactUnanchored,
		Policy:                policyConfig,
		Introspection:         introspection,
		Limits:                bounds,
		MapPath:               mapPath,
		LinkDepfilePath:       linkDepfile,
	})
	if err != nil {
		return exitCodeFor(err, 2), logBuf.String(), err.Error() + "\n"
	}
	cliLogger.Info("Writing CycloneDX BOM to '%s'...", output)
	if err := cyclonedx.WriteBOM(output, generated.BOM); err != nil {
		// Either validation layer failing is exit code 4 (section 32.5).
		return 4, logBuf.String(), err.Error() + "\n"
	}
	// The default is <build-dir>/evidence.json because that is where "explain"
	// looks for it; --evidence-dump moves it, and "off" leaves the build
	// directory untouched.
	evidencePath := filepath.Join(buildDir, "evidence.json")
	if evidenceDump != "" {
		evidencePath = evidenceDump
	}
	if evidenceDump == "off" {
		cliLogger.Info("Evidence graph dump suppressed by --evidence-dump=off")
	} else {
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

// resolveIntrospection combines the configuration's build.introspection block
// with --allow-introspection. Section 9.2 makes the default off, so a group is
// enabled only when something says so explicitly; the bare flag enables all of
// them, and a comma-separated value enables the named ones.
func resolveIntrospection(cfg config.Config, allowAll bool, groups []string) (exec.Features, error) {
	features := exec.Features{
		CMake:      cfg.Build.Introspection.CMake,
		Ninja:      cfg.Build.Introspection.Ninja,
		Git:        cfg.Build.Introspection.Git,
		OSPackages: cfg.Build.Introspection.OSPackages,
		Compiler:   cfg.Build.Introspection.Compiler,
	}
	if allowAll {
		return exec.Features{CMake: true, Ninja: true, Git: true, OSPackages: true, Compiler: true}, nil
	}
	for _, group := range groups {
		switch strings.TrimSpace(group) {
		case "":
		case "cmake":
			features.CMake = true
		case "ninja":
			features.Ninja = true
		case "git":
			features.Git = true
		case "osPackages", "os-packages":
			features.OSPackages = true
		case "compiler":
			features.Compiler = true
		default:
			return exec.Features{}, fmt.Errorf("unknown introspection group %q (cmake, ninja, git, osPackages, compiler)", group)
		}
	}
	return features, nil
}

// parseSize reads a byte count, with the suffixes a person writing a limit on
// a command line reaches for. Section 30 states --max-input-size in bytes; the
// suffixes are a convenience, not a second syntax.
func parseSize(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("empty")
	}
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(trimmed, "GiB"), strings.HasSuffix(trimmed, "G"):
		multiplier = 1 << 30
	case strings.HasSuffix(trimmed, "MiB"), strings.HasSuffix(trimmed, "M"):
		multiplier = 1 << 20
	case strings.HasSuffix(trimmed, "KiB"), strings.HasSuffix(trimmed, "K"):
		multiplier = 1 << 10
	}
	digits := strings.TrimRight(trimmed, "GiBMK")
	number, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, err
	}
	if number <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return number * multiplier, nil
}
