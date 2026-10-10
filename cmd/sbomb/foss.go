package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/foss"
	"github.com/example/sbomb/internal/generate"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/policy"
	"github.com/example/sbomb/internal/sbomwriter"
)

// fossRendering is everything the four documents of section 32.6 are rendered
// from. It exists so that `generate --foss-out` and `sbomb foss --out` produce
// byte-identical files: they are one renderer over one run, and the assertion
// is byte equality rather than a comparison of their component sets.
type fossRendering struct {
	Generated    generate.Result
	Findings     []domain.Finding
	Profile      string
	Mode         string
	HeaderView   string
	Format       string
	SpecVersion  string
	TLP          string
	Reproducible bool
}

// writeFOSSOutputs is the single call site of the FOSS writer. Both entry
// points go through it.
func writeFOSSOutputs(directory string, in fossRendering) error {
	mode := in.Mode
	if mode == "" {
		mode = "single"
	}
	view := in.HeaderView
	if view == "" {
		view = "dwarf-preferred"
	}
	// foss-review.json is a CycloneDX rendering whatever the SBOM is written
	// in (section 32.6). The configured version belongs to the SBOM's format,
	// so it is passed on only when that format is CycloneDX: "3.0.1" means
	// nothing to the CycloneDX writer, and the review rendering of an SPDX run
	// takes the CycloneDX default instead of failing after a full discovery.
	specVersion := in.SpecVersion
	if in.Generated.Writer != nil && in.Generated.Writer.ID() != sbomwriter.DefaultFormat {
		specVersion = ""
	}
	return foss.Write(directory, foss.Input{
		Document:       in.Generated.Document,
		Findings:       in.Findings,
		View:           fossView(in.Generated.FOSSView),
		Profile:        in.Profile,
		Mode:           mode,
		HeaderEvidence: view,
		Format:         in.Format,
		SpecVersion:    specVersion,
		TLP:            in.TLP,
		Reproducible:   in.Reproducible,
	})
}

// fossView converts the run's licence view into the renderer's own type. The
// renderer is a writer and does not import the pipeline (decision Q21), so the
// conversion happens here, exactly as it does for the review report's
// narrowing counts.
func fossView(view *generate.FOSSView) foss.View {
	if view == nil {
		return foss.View{}
	}
	out := foss.View{NarrowedTotal: view.NarrowedTotal}
	for _, delta := range view.Components {
		out.Deltas = append(out.Deltas, foss.ViewDelta{
			Component:   delta.Component,
			ComponentID: delta.ComponentID,
			Narrowed:    delta.Narrowed,
			Copyrights:  delta.Copyrights,
		})
	}
	return out
}

// handleFOSS is the thin front end of section 32.6, for the case "I want the
// notices, not an SBOM on disk".
//
// It is the same code path as `generate --foss-out`: one discovery, one
// renderer. What it does not do is evaluate policy gates -- no exit code of
// this command depends on licence content, and everything the FOSS view finds
// is informational.
func handleFOSS(args []string, verbosity int) (int, string, string) {
	buildDir, sourceDir, out := "", "", ""
	// Section 32.6: the FOSS outputs are a second rendering of one discovery,
	// so the discovery has to be the same one -- and a run that compares paths
	// one way for the document and another way for the notices can describe
	// two different component sets from one build.
	pathFlavor := ""
	format, specVersion, configName, mode := "", "", "", ""
	cfgPath, policyName, waiversPath := "", "", ""
	repro, redactUnanchored := false, false
	allowIntrospection := false
	var introspectionGroups []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--verbose" || args[i] == "-v":
			verbosity++
		case strings.HasPrefix(args[i], "--verbose=") || strings.HasPrefix(args[i], "-v="):
			if level, ok := verbosityFlag(args[i], verbosity); ok {
				verbosity = level
			}
		case args[i] == "--build-dir":
			if i+1 >= len(args) {
				return 1, "", "missing value for --build-dir\n"
			}
			buildDir = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--build-dir="):
			buildDir = strings.TrimPrefix(args[i], "--build-dir=")
		case args[i] == "--source-dir":
			if i+1 >= len(args) {
				return 1, "", "missing value for --source-dir\n"
			}
			sourceDir = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--source-dir="):
			sourceDir = strings.TrimPrefix(args[i], "--source-dir=")
		case args[i] == "--path-flavor":
			if i+1 >= len(args) {
				return 1, "", "missing value for --path-flavor\n"
			}
			pathFlavor = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--path-flavor="):
			pathFlavor = strings.TrimPrefix(args[i], "--path-flavor=")
		case args[i] == "--out":
			if i+1 >= len(args) {
				return 1, "", "missing value for --out\n"
			}
			out = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--out="):
			out = strings.TrimPrefix(args[i], "--out=")
		case args[i] == "--format":
			if i+1 >= len(args) {
				return 1, "", "missing value for --format\n"
			}
			format = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--format="):
			format = strings.TrimPrefix(args[i], "--format=")
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
		case args[i] == "--config-name":
			if i+1 >= len(args) {
				return 1, "", "missing value for --config-name\n"
			}
			configName = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--config-name="):
			configName = strings.TrimPrefix(args[i], "--config-name=")
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
		case args[i] == "--reproducible":
			repro = true
		case args[i] == "--redact-unanchored-paths":
			redactUnanchored = true
		case args[i] == "--allow-introspection":
			allowIntrospection = true
		case strings.HasPrefix(args[i], "--allow-introspection="):
			value := strings.TrimPrefix(args[i], "--allow-introspection=")
			if value == "false" {
				introspectionGroups, allowIntrospection = nil, false
				break
			}
			introspectionGroups = strings.Split(value, ",")
		default:
			if strings.HasPrefix(args[i], "--") {
				return 1, "", fmt.Sprintf("unknown flag: %s\n", args[i])
			}
			return 1, "", fmt.Sprintf("unexpected argument: %s\n", args[i])
		}
	}

	// Usage errors first, per the precedence of section 32.4: nothing is read
	// or discovered on the strength of an argument that cannot be honoured.
	if buildDir == "" {
		return 1, "", "--build-dir is required\n"
	}
	if out == "" {
		return 1, "", "--out is required\n"
	}
	if format != "" && format != foss.FormatText && format != foss.FormatMarkdown {
		return 1, "", fmt.Sprintf("invalid value for --format: %s; use %s or %s\n",
			format, foss.FormatText, foss.FormatMarkdown)
	}
	if mode != "" && mode != "single" && mode != "assembly" {
		return 1, "", "invalid value for --mode: " + mode + "\n"
	}

	var logBuf bytes.Buffer
	var logWriter io.Writer
	if verbosity > 0 {
		logWriter = &logBuf
	}
	cliLogger := generate.NewLogger(verbosity, logWriter)

	loadedCfg := config.Config{}
	if cfgPath != "" {
		loaded, err := config.Load(cfgPath)
		if err != nil {
			return 1, logBuf.String(), err.Error() + "\n"
		}
		loadedCfg = loaded
	}
	if sourceDir != "" {
		loadedCfg.Project.Root = sourceDir
	}
	if configName != "" {
		loadedCfg.Build.Config = configName
	}
	if mode != "" {
		loadedCfg.Mode = mode
	}
	if specVersion != "" {
		loadedCfg.Output.SpecVersion = specVersion
	}
	repro = repro || loadedCfg.Output.Reproducible
	// The SBOM format and version the run is configured for, and whether the
	// writer can honour them, are usage questions and are answered before the
	// build directory is looked at (section 32.4): a version no writer emits,
	// or a configured TLP the chosen version cannot carry, is refused here
	// rather than after a full discovery run. Reproducibility is not part of
	// the question. This command writes no SBOM, and nothing it writes states
	// a creation time, so what the configured format would demand of a
	// reproducible document -- SPDX's SOURCE_DATE_EPOCH -- is not demanded of
	// a run that never writes one.
	if _, err := resolveOutput(loadedCfg.Output.Format, loadedCfg.Output.SpecVersion, sbomwriter.Options{
		TLP: loadedCfg.Output.TLP,
	}); err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}
	// For the same reason the run is rendered as CycloneDX whatever the
	// configuration names: foss-review.json, the one machine-readable record
	// this command writes, is CycloneDX in every case (section 32.6), and the
	// SPDX version belongs to the SPDX document. Rendering the configured
	// format would only render a document nobody receives -- and, under
	// --reproducible without SOURCE_DATE_EPOCH, fail on it.
	if sbomwriter.FormatOrDefault(loadedCfg.Output.Format) != sbomwriter.DefaultFormat {
		loadedCfg.Output.Format = sbomwriter.DefaultFormat
		loadedCfg.Output.SpecVersion = ""
	}
	// A build directory that is not there is a discovery error and not a
	// usage error (section 32.4 code 2): no evidence can be collected from
	// it. `foss` reads a build tree and never creates one, which is the
	// difference from `generate`.
	if info, err := os.Stat(buildDir); err != nil || !info.IsDir() {
		return 2, logBuf.String(), fmt.Sprintf("the build directory %s cannot be read: no evidence can be collected\n", buildDir)
	}

	policyConfig, err := policy.Resolve(loadedCfg.Policy, policy.Overrides{
		Profile:     policyName,
		WaiversFile: waiversPath,
	})
	if err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}
	introspection, err := resolveIntrospection(loadedCfg, allowIntrospection, introspectionGroups)
	if err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}
	if introspection.Enabled() {
		cliLogger.Info("Introspection enabled: %s", strings.Join(exec.AllowlistFor(introspection), "; "))
	}

	flavor := pathmodel.DefaultFlavor()
	switch pathFlavor {
	case "", "auto":
	case "posix":
		flavor = pathmodel.PosixFlavor{}
	case "windows":
		flavor = pathmodel.WindowsFlavor{}
	default:
		return 1, logBuf.String(), "invalid value for --path-flavor: " + pathFlavor + "\n"
	}

	generated, err := generate.RunWithOptions(loadedCfg, buildDir, repro, generate.Options{
		PathFlavor:            flavor,
		Verbosity:             verbosity,
		LogWriter:             logWriter,
		RedactUnanchoredPaths: redactUnanchored,
		Policy:                policyConfig,
		Introspection:         introspection,
		FOSSView:              true,
	})
	if err != nil {
		return exitCodeFor(err, 2), logBuf.String(), err.Error() + "\n"
	}

	// Waivers are evaluated for their annotation and for nothing else. A
	// waived FOSS finding keeps its reason in the review record and stops
	// failing a build -- and this command fails no build over licence
	// content, so the result's exit code is deliberately discarded.
	waivers, err := policy.LoadWaivers(policyConfig.WaiversFile)
	if err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}
	annotated := policy.Evaluate(generated.Findings, policyConfig, waivers, runTimestamp())

	cliLogger.Info("Writing the FOSS documents to '%s'...", out)
	if err := writeFOSSOutputs(out, fossRendering{
		Generated:    generated,
		Findings:     annotated.Findings,
		Profile:      policyConfig.Profile,
		Mode:         loadedCfg.Mode,
		HeaderView:   policyConfig.HeaderEvidence,
		Format:       format,
		SpecVersion:  loadedCfg.Output.SpecVersion,
		TLP:          loadedCfg.Output.TLP,
		Reproducible: repro,
	}); err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}
	return 0, logBuf.String(), ""
}
