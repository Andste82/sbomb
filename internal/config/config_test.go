package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValidFile(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "sbomb.json")
	if err := os.WriteFile(cfgPath, []byte(`{"project":{"name":"demo","root":"."},"build":{"dir":"build"},"mode":"single"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Project.Name != "demo" {
		t.Fatalf("Project.Name = %q", cfg.Project.Name)
	}
	if cfg.Build.Dir != "build" {
		t.Fatalf("Build.Dir = %q", cfg.Build.Dir)
	}
}

// build.dir is optional. It names what the build root is called, which the
// CMake File API already answers; --build-dir says where to read, which the
// CLI always supplies. Requiring it in the file forced every configuration to
// repeat whichever build directory happened to be current.
func TestLoadAcceptsAConfigurationWithoutBuildDir(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "sbomb.json")
	if err := os.WriteFile(cfgPath, []byte(`{"project":{"name":"demo","root":"."}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v, want a configuration without build.dir to load", err)
	}
	// And it stays empty rather than being filled in with a guess: an empty
	// value is what lets the File API's own build root anchor the document.
	// Defaulting it to the configuration file's directory would re-anchor
	// every file under the build root against a directory nobody built in.
	if cfg.Build.Dir != "" {
		t.Errorf("Build.Dir = %q, want it left empty for the File API to answer", cfg.Build.Dir)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "sbomb.json")
	if err := os.WriteFile(cfgPath, []byte(`{"notARealKey":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath); err == nil {
		t.Fatal("Load() accepted unknown key")
	}
}

// The keys below were accepted and then ignored: a configuration could ask for
// a hash algorithm, declare a generator's inputs or curate an upstream URL and
// nothing happened. Since an unknown key is an error, a reader reasonably
// concludes that an accepted one has an effect. They are gone (deviation D20),
// and loading has to say so rather than continue quietly.
func TestRemovedKeysAreRejectedRatherThanIgnored(t *testing.T) {
	for _, removed := range []struct{ name, body string }{
		{"generators", `{"project":{"name":"a"},"build":{"dir":"build"},"generators":[{"output":"x.h"}]}`},
		{"output.hashAlgorithms", `{"project":{"name":"a"},"build":{"dir":"build"},"output":{"hashAlgorithms":["sha512"]}}`},
		{"components[].cdxType", `{"project":{"name":"a"},"build":{"dir":"build"},"components":[{"path":"d","cdxType":"framework"}]}`},
		{"components[].upstream", `{"project":{"name":"a"},"build":{"dir":"build"},"components":[{"path":"d","upstream":{"url":"https://example.invalid"}}]}`},
	} {
		path := filepath.Join(t.TempDir(), "sbomb.json")
		if err := os.WriteFile(path, []byte(removed.body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Errorf("%s: loading succeeded; a removed key must be reported, not ignored", removed.name)
		}
	}
}

// The output settings admit a closed set of values each. Checking them is what
// keeps a configuration from being silently wrong.
func TestOutputSettingsAreChecked(t *testing.T) {
	for _, testCase := range []struct {
		name, body string
		wantError  bool
	}{
		{"the only format", `{"project":{"name":"a"},"build":{"dir":"build"},"output":{"format":"cyclonedx-json"}}`, false},
		{"another format", `{"project":{"name":"a"},"build":{"dir":"build"},"output":{"format":"spdx-json"}}`, true},
		{"the default version", `{"project":{"name":"a"},"build":{"dir":"build"},"output":{"specVersion":"1.6"}}`, false},
		{"the opt-in version", `{"project":{"name":"a"},"build":{"dir":"build"},"output":{"specVersion":"1.7"}}`, false},
		{"a version no writer emits", `{"project":{"name":"a"},"build":{"dir":"build"},"output":{"specVersion":"1.5"}}`, true},
		{"absent is fine", `{"project":{"name":"a"},"build":{"dir":"build"}}`, false},
	} {
		path := filepath.Join(t.TempDir(), "sbomb.json")
		if err := os.WriteFile(path, []byte(testCase.body), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path)
		if testCase.wantError && err == nil {
			t.Errorf("%s: expected an error", testCase.name)
		}
		if !testCase.wantError && err != nil {
			t.Errorf("%s: %v", testCase.name, err)
		}
	}
}

// Reproducibility is a project property, so the configuration can ask for it.
func TestReproducibleIsReadFromTheConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sbomb.json")
	if err := os.WriteFile(path, []byte(`{"project":{"name":"a"},"build":{"dir":"build"},"output":{"reproducible":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Output.Reproducible {
		t.Error("output.reproducible did not survive loading")
	}
}

// The promise the documentation makes -- a typo cannot silently disable a
// policy gate -- held only at the top level. "failOnMisingHash" and "profil"
// both loaded without complaint, so a gate somebody believed was on was off.
func TestATypoInsideAnObjectIsRefused(t *testing.T) {
	for _, typo := range []string{
		`{"project":{"name":"a"},"build":{"dir":"b"},"policy":{"failOnMisingHash":true}}`,
		`{"project":{"name":"a"},"build":{"dir":"b"},"policy":{"profil":"strict"}}`,
		`{"project":{"name":"a"},"build":{"dir":"b"},"project_name":"a"}`,
		`{"project":{"nam":"a"},"build":{"dir":"b"}}`,
		`{"project":{"name":"a"},"build":{"dir":"b"},"artifacts":[{"path":"x","rol":"library"}]}`,
		`{"project":{"name":"a"},"build":{"dir":"b"},"components":[{"path":"d","licence":"MIT"}]}`,
	} {
		path := filepath.Join(t.TempDir(), "sbomb.json")
		if err := os.WriteFile(path, []byte(typo), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Errorf("loaded without complaint: %s", typo)
		}
	}
}

// And a correct file still loads, at every level.
func TestAFullyPopulatedConfigurationStillLoads(t *testing.T) {
	body := `{
      "schemaVersion": 3,
      "project": {"name":"a","type":"firmware","root":".","version":"1","supplier":"s","license":"MIT"},
      "build": {"dir":"b","config":"Debug","introspection":{"git":true,"ninja":true}},
      "mode": "single",
      "artifacts": [{"path":"b/app","role":"application","map":"b/app.map","linkDepfile":"b/app.d"}],
      "anchors": [{"key":"sdk:x","path":"/opt/x"}],
      "discovery": {"excludeTargetPatterns":["*test*"]},
      "components": [{"path":"d","match":"d/**","name":"n","type":"library","version":"1","versionFrom":["git"],"license":"MIT","supplier":"s","purl":"pkg:generic/n@1"}],
      "manifests": ["d/conanfile.txt"],
      "policy": {"profile":"cra","profileOverlay":"host-linux","headerEvidence":"union","waiversFile":"w.json",
                 "failOnMissingHash":true,"includeAssets":true,"systemLibraries":"exclude",
                 "staleToleranceSeconds":5,"severityOverrides":{"UNKNOWN_LICENSE":"info"}},
      "output": {"format":"cyclonedx-json","specVersion":"1.6","reproducible":true}
    }`
	path := filepath.Join(t.TempDir(), "sbomb.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Output.Reproducible || cfg.Policy.Profile != "cra" || len(cfg.Components) != 1 {
		t.Errorf("configuration did not survive loading: %+v", cfg)
	}
}

// build.introspection.cmake was a switch that turned nothing on: the two cmake
// shapes behind it could only have regenerated a File API reply, and
// regenerating means configuring, which is a build command section 9.2
// forbids. A setting that changes nothing is worse than an absent one, because
// a reader believes it. The loader now refuses it like any other unknown
// field, and the published schema does not offer it either.
func TestTheCMakeIntrospectionGroupIsGone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sbomb.json")
	body := `{"project":{"name":"a"},"build":{"dir":"b","introspection":{"cmake":true}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("a group nothing can run was accepted as a configuration key")
	}
	if strings.Contains(Schema(), "cmake") {
		t.Error("the published schema still offers the group")
	}

	// The four groups that do have a caller keep working, so this is a
	// removal and not a regression of the block as a whole.
	body = `{"project":{"name":"a"},"build":{"dir":"b","introspection":{"ninja":true,"git":true,"osPackages":true,"compiler":true}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Build.Introspection.Ninja || !cfg.Build.Introspection.Git ||
		!cfg.Build.Introspection.OSPackages || !cfg.Build.Introspection.Compiler {
		t.Errorf("introspection block did not survive loading: %+v", cfg.Build.Introspection)
	}
}
