package policy

import (
	"fmt"
	"strings"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
)

// Overrides are the values a CLI flag set explicitly. They win over
// everything else, per the precedence of section 32.2:
// CLI flag > policy profile > configuration file > built-in default.
type Overrides struct {
	Profile string
	Overlay string

	Gates  map[string]bool
	Scopes map[string]string

	HeaderEvidence string
	WaiversFile    string
}

// gateSetters connects a gate name -- as used by both the configuration file
// and the --fail-on-* flags -- to the field it controls. One table keeps the
// three input paths from drifting apart.
var gateSetters = map[string]func(*Config, bool){
	"unknownComponent":             func(c *Config, v bool) { c.FailOnUnknownComponent = v },
	"unknownLicense":               func(c *Config, v bool) { c.FailOnUnknownLicense = v },
	"unknownVersion":               func(c *Config, v bool) { c.FailOnUnknownVersion = v },
	"missingSupplier":              func(c *Config, v bool) { c.FailOnMissingSupplier = v },
	"missingHash":                  func(c *Config, v bool) { c.FailOnMissingHash = v },
	"missingComponentHash":         func(c *Config, v bool) { c.FailOnMissingComponentHash = v },
	"missingSourceForLinkedObject": func(c *Config, v bool) { c.FailOnMissingSourceForLinkedObject = v },
	"staleBuildArtifacts":          func(c *Config, v bool) { c.FailOnStaleBuildArtifacts = v },
	"reviewRequired":               func(c *Config, v bool) { c.FailOnReviewRequired = v },
	"weakEvidence":                 func(c *Config, v bool) { c.FailOnWeakEvidence = v },
	"missingHeaderEvidence":        func(c *Config, v bool) { c.FailOnMissingHeaderEvidence = v },
	"unanchoredFile":               func(c *Config, v bool) { c.FailOnUnanchoredFile = v },
	"allowMissingLinkEvidence":     func(c *Config, v bool) { c.AllowMissingLinkEvidence = v },
	"systemHeaders":                func(c *Config, v bool) { c.IncludeSystemHeaders = v },
	"linkerScripts":                func(c *Config, v bool) { c.IncludeLinkerScripts = v },
	"generatedIntermediateFiles":   func(c *Config, v bool) { c.IncludeGeneratedIntermediateFile = v },
	"assets":                       func(c *Config, v bool) { c.IncludeAssets = v },
	"transientBuildArtifacts":      func(c *Config, v bool) { c.IncludeTransientBuildArtifacts = v },
	"prebuiltLibrariesRequireMapping": func(c *Config, v bool) {
		c.PrebuiltLibrariesRequireMapping = v
	},
}

// GateNames lists every gate a flag or configuration key can set.
func GateNames() []string {
	names := make([]string, 0, len(gateSetters))
	for name := range gateSetters {
		names = append(names, name)
	}
	return names
}

var scopeSetters = map[string]func(*Config, string){
	"includeToolchainRuntime":  func(c *Config, v string) { c.IncludeToolchainRuntime = v },
	"systemLibraries":          func(c *Config, v string) { c.SystemLibraries = v },
	"pchHeaders":               func(c *Config, v string) { c.PCHHeaders = v },
	"sectionGarbageCollection": func(c *Config, v string) { c.SectionGarbageCollection = v },
}

// allowedScopeValues rejects a typo rather than letting it silently disable a
// scope rule, which is the same reasoning as the strict configuration keys.
var allowedScopeValues = map[string][]string{
	"includeToolchainRuntime":  {"exclude", "main-sbom", "separate-component", "report-only"},
	"systemLibraries":          {"exclude", "main-sbom", "separate-component", "report-only"},
	"pchHeaders":               {"include", "annotate-only", "exclude"},
	"sectionGarbageCollection": {"ignore", "annotate", "exclude"},
	"headerEvidence":           {"dwarf-preferred", "union", "depfiles"},
}

// Resolve builds the effective policy from all four sources in precedence
// order (section 32.2).
func Resolve(cfgPolicy config.Policy, overrides Overrides) (Config, error) {
	profile := overrides.Profile
	if profile == "" {
		profile = cfgPolicy.Profile
	}
	resolved := ResolveProfile(profile)

	// 3. Configuration file.
	applyBool := func(value *bool, name string) {
		if value != nil {
			if setter, known := gateSetters[name]; known {
				setter(&resolved, *value)
			}
		}
	}
	applyBool(cfgPolicy.FailOnUnknownComponent, "unknownComponent")
	applyBool(cfgPolicy.FailOnUnknownLicense, "unknownLicense")
	applyBool(cfgPolicy.FailOnUnknownVersion, "unknownVersion")
	applyBool(cfgPolicy.FailOnMissingSupplier, "missingSupplier")
	applyBool(cfgPolicy.FailOnMissingHash, "missingHash")
	applyBool(cfgPolicy.FailOnMissingComponentHash, "missingComponentHash")
	applyBool(cfgPolicy.FailOnMissingSourceForLinkedObject, "missingSourceForLinkedObject")
	applyBool(cfgPolicy.FailOnStaleBuildArtifacts, "staleBuildArtifacts")
	applyBool(cfgPolicy.FailOnReviewRequired, "reviewRequired")
	applyBool(cfgPolicy.FailOnWeakEvidence, "weakEvidence")
	applyBool(cfgPolicy.FailOnMissingHeaderEvidence, "missingHeaderEvidence")
	applyBool(cfgPolicy.FailOnUnanchoredFile, "unanchoredFile")
	applyBool(cfgPolicy.AllowMissingLinkEvidence, "allowMissingLinkEvidence")
	applyBool(cfgPolicy.IncludeSystemHeaders, "systemHeaders")
	applyBool(cfgPolicy.IncludeLinkerScripts, "linkerScripts")
	applyBool(cfgPolicy.IncludeGeneratedIntermediateFile, "generatedIntermediateFiles")
	applyBool(cfgPolicy.IncludeAssets, "assets")
	applyBool(cfgPolicy.IncludeTransientBuildArtifacts, "transientBuildArtifacts")
	applyBool(cfgPolicy.PrebuiltLibrariesRequireMapping, "prebuiltLibrariesRequireMapping")

	for name, value := range map[string]string{
		"includeToolchainRuntime":  cfgPolicy.IncludeToolchainRuntime,
		"systemLibraries":          cfgPolicy.SystemLibraries,
		"pchHeaders":               cfgPolicy.PCHHeaders,
		"sectionGarbageCollection": cfgPolicy.SectionGarbageCollection,
	} {
		if value == "" {
			continue
		}
		if err := checkScopeValue(name, value); err != nil {
			return Config{}, err
		}
		scopeSetters[name](&resolved, value)
	}
	if cfgPolicy.HeaderEvidence != "" {
		if err := checkScopeValue("headerEvidence", cfgPolicy.HeaderEvidence); err != nil {
			return Config{}, err
		}
		resolved.HeaderEvidence = cfgPolicy.HeaderEvidence
	}
	if cfgPolicy.StaleToleranceSeconds != nil {
		resolved.StaleToleranceSeconds = *cfgPolicy.StaleToleranceSeconds
	}
	if cfgPolicy.WaiversFile != "" {
		resolved.WaiversFile = cfgPolicy.WaiversFile
	}
	for id, severity := range cfgPolicy.SeverityOverrides {
		switch domain.Severity(severity) {
		case domain.SeverityError, domain.SeverityWarning, domain.SeverityInfo:
			resolved.SeverityOverrides[id] = domain.Severity(severity)
		default:
			return Config{}, fmt.Errorf("invalid severity %q for finding %s", severity, id)
		}
	}

	// 2b. The overlay, which refines scope on top of the chosen profile.
	overlayName := overrides.Overlay
	if overlayName == "" {
		overlayName = cfgPolicy.Overlay
	}
	overlay, err := Overlay(overlayName)
	if err != nil {
		return Config{}, err
	}
	overlay(&resolved)

	// 1. CLI flags win over everything.
	for name, value := range overrides.Gates {
		setter, known := gateSetters[name]
		if !known {
			return Config{}, fmt.Errorf("unknown policy gate %q", name)
		}
		setter(&resolved, value)
	}
	for name, value := range overrides.Scopes {
		if err := checkScopeValue(name, value); err != nil {
			return Config{}, err
		}
		scopeSetters[name](&resolved, value)
	}
	if overrides.HeaderEvidence != "" {
		if err := checkScopeValue("headerEvidence", overrides.HeaderEvidence); err != nil {
			return Config{}, err
		}
		resolved.HeaderEvidence = overrides.HeaderEvidence
	}
	if overrides.WaiversFile != "" {
		resolved.WaiversFile = overrides.WaiversFile
	}
	return resolved, nil
}

func checkScopeValue(name, value string) error {
	allowed, known := allowedScopeValues[name]
	if !known {
		return fmt.Errorf("unknown policy option %q", name)
	}
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("invalid value %q for %s; allowed: %s", value, name, strings.Join(allowed, ", "))
}
