package policy

import (
	"strings"
	"testing"

	"github.com/example/sbomb/internal/config"
)

func boolPtr(v bool) *bool { return &v }

// TestPrecedenceIsCLIThenProfileThenConfiguration pins the order of section
// 32.2. Without it a configuration file could silently override a flag, or a
// profile could override a deliberate configuration choice.
func TestPrecedenceIsCLIThenProfileThenConfiguration(t *testing.T) {
	cfgPolicy := config.Policy{
		Profile:              "strict",
		FailOnUnknownLicense: boolPtr(false),
		FailOnWeakEvidence:   boolPtr(false),
	}

	// Configuration turns two strict gates off.
	resolved, err := Resolve(cfgPolicy, Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.FailOnUnknownLicense || resolved.FailOnWeakEvidence {
		t.Error("configuration could not turn a profile gate off")
	}
	if !resolved.FailOnUnknownVersion {
		t.Error("configuration silently changed a gate it did not mention")
	}

	// A flag turns one of them back on.
	resolved, err = Resolve(cfgPolicy, Overrides{Gates: map[string]bool{"unknownLicense": true}})
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.FailOnUnknownLicense {
		t.Error("a CLI flag did not win over the configuration file")
	}
	if resolved.FailOnWeakEvidence {
		t.Error("a CLI flag changed a gate it did not mention")
	}

	// A flag also selects the profile.
	resolved, err = Resolve(cfgPolicy, Overrides{Profile: "lenient"})
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.AllowMissingLinkEvidence {
		t.Errorf("profile = %q, want the lenient profile chosen by the flag", resolved.Profile)
	}
}

func TestHostLinuxOverlayOnlyChangesScope(t *testing.T) {
	base, err := ResolveProfile("cra")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(config.Policy{Profile: "cra"}, Overrides{Overlay: "host-linux"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SystemLibraries != "separate-component" {
		t.Errorf("systemLibraries = %q, want separate-component", resolved.SystemLibraries)
	}
	// The overlay is a scope profile; it must not touch the gates.
	if resolved.FailOnUnknownVersion != base.FailOnUnknownVersion ||
		resolved.FailOnMissingSupplier != base.FailOnMissingSupplier {
		t.Error("the host-linux overlay changed a gate; it may only change scope")
	}
}

func TestInvalidValuesAreRejectedRatherThanIgnored(t *testing.T) {
	if _, err := Resolve(config.Policy{}, Overrides{Overlay: "nonsense"}); err == nil {
		t.Error("an unknown overlay was accepted")
	}
	if _, err := Resolve(config.Policy{SystemLibraries: "sometimes"}, Overrides{}); err == nil {
		t.Error("an invalid systemLibraries value was accepted")
	}
	if _, err := Resolve(config.Policy{}, Overrides{HeaderEvidence: "guess"}); err == nil {
		t.Error("an invalid headerEvidence value was accepted")
	}
	if _, err := Resolve(config.Policy{SeverityOverrides: map[string]string{"X": "loud"}}, Overrides{}); err == nil {
		t.Error("an invalid severity was accepted")
	}
}

// An unknown profile name used to fall back to the default gates, so a typo
// such as "strcit" ran silently under weaker gates than the user asked for.
// The overlay was already checked; the profile has to be too, from the flag
// and from the configuration file alike.
func TestUnknownProfileIsRejectedRatherThanDefaulted(t *testing.T) {
	for _, source := range []struct {
		name      string
		cfgPolicy config.Policy
		overrides Overrides
	}{
		{"flag", config.Policy{}, Overrides{Profile: "strcit"}},
		{"configuration", config.Policy{Profile: "bogus"}, Overrides{}},
	} {
		_, err := Resolve(source.cfgPolicy, source.overrides)
		if err == nil {
			t.Errorf("%s: an unknown profile was accepted", source.name)
			continue
		}
		for _, profile := range []string{"default", "lenient", "cra", "strict"} {
			if !strings.Contains(err.Error(), profile) {
				t.Errorf("%s: error %q does not name the profile %q", source.name, err, profile)
			}
		}
	}

	// Every spelling accepted so far still resolves.
	for name, want := range map[string]string{"STRICT": "strict", "Cra": "cra", "Lenient": "lenient", "": "default"} {
		resolved, err := Resolve(config.Policy{}, Overrides{Profile: name})
		if err != nil {
			t.Errorf("profile %q: %v", name, err)
			continue
		}
		if resolved.Profile != want {
			t.Errorf("profile %q resolved to %q, want %q", name, resolved.Profile, want)
		}
	}
}

func TestSeverityOverridesReachTheEvaluator(t *testing.T) {
	resolved, err := Resolve(config.Policy{
		SeverityOverrides: map[string]string{"UNKNOWN_VERSION": "info"},
	}, Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SeverityOverrides["UNKNOWN_VERSION"] != "info" {
		t.Errorf("severity overrides = %v", resolved.SeverityOverrides)
	}
}

func TestEveryGateNameHasASetter(t *testing.T) {
	// A gate that no name reaches could never be turned off from a
	// configuration file or a flag.
	if len(GateNames()) < 15 {
		t.Errorf("only %d gate names are wired", len(GateNames()))
	}
	for _, name := range GateNames() {
		if _, err := Resolve(config.Policy{}, Overrides{Gates: map[string]bool{name: true}}); err != nil {
			t.Errorf("gate %q: %v", name, err)
		}
	}
}
