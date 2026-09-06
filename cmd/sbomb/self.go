package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/generate"
	"github.com/example/sbomb/internal/limits"
	"github.com/example/sbomb/internal/policy"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/selfsbom"
	"github.com/google/uuid"
)

// handleSelf writes an SBOM for a Go binary from the module evidence its
// linker recorded. A release runs it over the artifacts it is about to
// publish, so it takes the same policy and the same exit codes as generate: an
// SBOM that fails its own gates must not be attached to a release.
func handleSelf(args []string, verbosity int) (int, string, string) {
	options := selfOptions{verbosity: verbosity}
	if code, message := options.parse(args); code != 0 {
		return code, "", message
	}
	verbosity = options.verbosity

	var logBuf bytes.Buffer
	var logWriter io.Writer
	if verbosity > 0 {
		logWriter = &logBuf
	}
	logger := generate.NewLogger(verbosity, logWriter)

	policyConfig, err := policy.Resolve(config.Policy{}, policy.Overrides{Profile: options.policyName})
	if err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}

	logger.Info("Reading Go build information from '%s'", options.binaryPath)
	result, err := selfsbom.Build(options.binaryPath, selfsbom.Options{
		Version:      options.version,
		ModuleDir:    options.moduleDir,
		GOROOT:       options.goroot,
		Supplier:     options.supplier,
		Licenses:     options.licenses,
		Reproducible: options.reproducible,
		Timestamp:    runTimestamp(),
		Limits:       options.bounds,
	})
	if err != nil {
		return 2, logBuf.String(), err.Error() + "\n"
	}
	logger.Info("Recorded %d module(s) plus the standard library, toolchain %s",
		len(result.Binary.Deps), result.Binary.GoVersion)

	writer, specVersion, err := sbomwriter.Resolve("cyclonedx-json", options.specVersion)
	if err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}
	bom, err := writer.(cyclonedx.Writer).Build(result.Document, sbomwriter.Options{SpecVersion: specVersion, Reproducible: options.reproducible})
	if err != nil {
		return 4, logBuf.String(), err.Error() + "\n"
	}
	if options.reproducible {
		bom.SerialNumber = cyclonedx.ReproducibleSerialNumber(bom)
	} else {
		bom.SerialNumber = "urn:uuid:" + uuid.NewString()
	}
	logger.Info("Writing CycloneDX %s BOM to '%s'...", specVersion, options.output)
	if err := cyclonedx.WriteBOM(options.output, bom); err != nil {
		// Either validation layer failing is exit code 4 (section 32.5).
		return 4, logBuf.String(), err.Error() + "\n"
	}

	if options.evidencePath != "" {
		logger.Info("Writing evidence graph dump to '%s'...", options.evidencePath)
		if err := dumpGraph(result.Graph.Dump, options.evidencePath); err != nil {
			return 2, logBuf.String(), err.Error() + "\n"
		}
	}

	waivers, err := policy.LoadWaivers(policyConfig.WaiversFile)
	if err != nil {
		return 1, logBuf.String(), err.Error() + "\n"
	}
	evaluated := policy.Evaluate(result.Findings, policyConfig, waivers, runTimestamp())
	logger.Info("Policy evaluation complete: %d finding(s) (fail: %v, exit code: %d)",
		len(evaluated.Findings), evaluated.Fail, evaluated.ExitCode)
	if options.findingsJSONPath != "" {
		if err := policy.WriteFindingsJSON(options.findingsJSONPath, evaluated.Findings); err != nil {
			return 1, logBuf.String(), err.Error() + "\n"
		}
	}
	if evaluated.Fail {
		return 3, logBuf.String(), ""
	}
	return 0, logBuf.String(), ""
}

type selfOptions struct {
	binaryPath       string
	output           string
	version          string
	moduleDir        string
	goroot           string
	supplier         string
	policyName       string
	specVersion      string
	findingsJSONPath string
	evidencePath     string
	licenses         map[string]string
	reproducible     bool
	bounds           limits.Config
	verbosity        int
}

const selfUsage = "usage: sbomb self <binary> [--output <path>] [--version <v>] " +
	"[--module-dir <dir>] [--goroot <dir>] [--supplier <name>] [--policy <profile>] " +
	"[--spec-version <v>] [--license <module>=<SPDX>]... [--findings-json <path>] " +
	"[--evidence <path>] [--reproducible]\n"

// parse reads the arguments. It returns an exit code and a message, both zero
// valued when the arguments are good.
func (o *selfOptions) parse(args []string) (int, string) {
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if level, isVerbosity := verbosityFlag(argument, o.verbosity); isVerbosity {
			o.verbosity = level
			continue
		}
		if !strings.HasPrefix(argument, "-") {
			if o.binaryPath != "" {
				return 1, "unexpected argument: " + argument + "\n"
			}
			o.binaryPath = argument
			continue
		}
		// The value may be attached with "=" or be the next argument. Cutting
		// only the flag itself keeps a path that contains "=" intact.
		name, attached, hasAttached := strings.Cut(argument, "=")
		take := func() (string, bool) {
			if hasAttached {
				return attached, true
			}
			if index+1 >= len(args) {
				return "", false
			}
			index++
			return args[index], true
		}
		var value string
		var ok bool
		switch name {
		case "--binary":
			value, ok = take()
			o.binaryPath = value
		case "--output":
			value, ok = take()
			o.output = value
		case "--version":
			value, ok = take()
			o.version = value
		case "--module-dir":
			value, ok = take()
			o.moduleDir = value
		case "--goroot":
			value, ok = take()
			o.goroot = value
		case "--supplier":
			value, ok = take()
			o.supplier = value
		case "--license":
			value, ok = take()
			if ok {
				module, expression, split := strings.Cut(value, "=")
				if !split || strings.TrimSpace(module) == "" || strings.TrimSpace(expression) == "" {
					return 1, "--license expects <module path>=<SPDX expression>, got " + value + "\n"
				}
				if o.licenses == nil {
					o.licenses = map[string]string{}
				}
				o.licenses[strings.TrimSpace(module)] = strings.TrimSpace(expression)
			}
		case "--policy":
			value, ok = take()
			o.policyName = value
		case "--spec-version":
			value, ok = take()
			o.specVersion = value
		case "--findings-json":
			value, ok = take()
			o.findingsJSONPath = value
		case "--evidence":
			value, ok = take()
			o.evidencePath = value
		case "--max-input-size":
			value, ok = take()
			if ok {
				size, err := parseSize(value)
				if err != nil {
					return 1, fmt.Sprintf("invalid --max-input-size %q: %v\n", value, err)
				}
				o.bounds.MaxInput = size
			}
		case "--reproducible":
			o.reproducible, ok = true, true
		case "--strict-symlinks":
			o.bounds.StrictSymlinks, ok = true, true
		case "--help", "-h":
			return 1, selfUsage
		default:
			return 1, "unknown option: " + name + "\n"
		}
		if !ok {
			return 1, "missing value for " + name + "\n"
		}
	}

	if o.binaryPath == "" {
		return 1, selfUsage
	}
	if o.output == "" {
		o.output = o.binaryPath + ".cdx.json"
	}
	// The module root is where the vendored licences are. Finding it from the
	// working directory is a convenience; it is never inferred from the binary,
	// which may sit anywhere.
	if o.moduleDir == "" {
		if found, located := selfsbom.SourceModuleDir("."); located {
			o.moduleDir = found
		}
	}
	return 0, ""
}

func dumpGraph(dump func(io.Writer) error, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := dump(file); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// runTimestamp honours SOURCE_DATE_EPOCH, so that a build which pins it gets
// the same document twice (section 29).
func runTimestamp() time.Time {
	if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
		if seconds, err := strconv.ParseInt(epoch, 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC()
		}
	}
	return time.Now().UTC()
}
