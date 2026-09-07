package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func compiledSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	document, err := jsonschema.UnmarshalJSON(strings.NewReader(Schema()))
	if err != nil {
		t.Fatalf("the published schema is not JSON: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("sbomb.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("sbomb.json")
	if err != nil {
		t.Fatalf("the published schema does not compile: %v", err)
	}
	return schema
}

func checkAgainstSchema(t *testing.T, schema *jsonschema.Schema, body string) error {
	t.Helper()
	value, err := jsonschema.UnmarshalJSON(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return schema.Validate(value)
}

// The published schema and the loader have to agree about what a valid file
// is. They disagreed for a long time: the schema named five of the eleven
// sections the loader accepts and set additionalProperties false, so every
// documented example failed against the document the tool hands out.
func TestTheSchemaAcceptsEveryCommittedConfiguration(t *testing.T) {
	schema := compiledSchema(t)
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "config", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no committed configurations found: %v", err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkAgainstSchema(t, schema, string(data)); err != nil {
			t.Errorf("%s: rejected by the schema the tool publishes:\n%v", filepath.Base(path), err)
		}
		// And the loader has to agree.
		if _, err := Load(path); err != nil {
			t.Errorf("%s: rejected by the loader: %v", filepath.Base(path), err)
		}
	}
}

// A configuration exercising every section, as the documentation shows it.
func TestTheSchemaAcceptsAFullyPopulatedConfiguration(t *testing.T) {
	schema := compiledSchema(t)
	body := `{
      "schemaVersion": 3,
      "project": {"name":"a","type":"firmware","root":".","version":"1","supplier":"s","license":"MIT"},
      "build": {"dir":"b","config":"Debug","introspection":{"git":true,"ninja":true}},
      "mode": "single",
      "artifacts": [{"path":"b/app","role":"image","map":"b/app.map","linkDepfile":"b/app.d"}],
      "anchors": [{"key":"sdk:x","path":"/opt/x"}],
      "discovery": {"excludeTargetPatterns":["*test*"]},
      "components": [{"path":"d","name":"n","type":"library","version":"1","versionFrom":["git"],
                      "license":"MIT","supplier":"s","purl":"pkg:generic/n@1"}],
      "manifests": ["d/conanfile.txt"],
      "policy": {"profile":"cra","headerEvidence":"union","failOnMissingHash":true,
                 "includeAssets":true,"systemLibraries":"exclude","staleToleranceSeconds":5,
                 "severityOverrides":{"UNKNOWN_LICENSE":"info"}},
      "output": {"format":"cyclonedx-json","specVersion":"1.6","reproducible":true}
    }`
	if err := checkAgainstSchema(t, schema, body); err != nil {
		t.Fatalf("rejected: %v", err)
	}
	// A single string is valid where a list is, which is what StringList means.
	if err := checkAgainstSchema(t, schema, `{"project":{"name":"a"},"components":[{"path":"d","versionFrom":"git"}]}`); err != nil {
		t.Errorf("a bare string for versionFrom was rejected: %v", err)
	}
}

// The schema must refuse what the loader refuses, or it is the more permissive
// of two checks and tells a reader nothing.
func TestTheSchemaRefusesWhatTheLoaderRefuses(t *testing.T) {
	schema := compiledSchema(t)
	for _, bad := range []struct{ why, body string }{
		{"a typo in a policy gate", `{"project":{"name":"a"},"policy":{"failOnMisingHash":true}}`},
		{"a typo in a section name", `{"project":{"name":"a"},"policys":{}}`},
		{"a removed key", `{"project":{"name":"a"},"generators":[{"output":"x"}]}`},
		{"an unknown mode", `{"project":{"name":"a"},"mode":"cluster"}`},
		{"an unknown artifact role", `{"project":{"name":"a"},"artifacts":[{"path":"p","role":"widget"}]}`},
		{"an unknown output format", `{"project":{"name":"a"},"output":{"format":"spdx-json"}}`},
		{"no project at all", `{"build":{"dir":"b"}}`},
	} {
		if err := checkAgainstSchema(t, schema, bad.body); err == nil {
			t.Errorf("%s: accepted by the schema", bad.why)
		}
	}

	// And it must accept what the loader accepts. project.name stopped being
	// required when the build system became a source for it: CMake states it,
	// and section 6.1 makes the deliverable the root of a single-artifact
	// document anyway. A schema still demanding it would send a reader looking
	// for a field the loader no longer wants.
	if err := checkAgainstSchema(t, schema, `{"project":{"root":"."},"build":{"dir":"b"}}`); err != nil {
		t.Errorf("a project without a name was rejected: %v", err)
	}
}

// Every section the loader knows has to appear in the schema. Generating it
// from the same types makes that true by construction; this fails loudly if
// somebody replaces the generator with a hand-written document again.
func TestEverySectionOfTheLoaderIsInTheSchema(t *testing.T) {
	var published map[string]any
	if err := json.NewDecoder(bytes.NewReader([]byte(Schema()))).Decode(&published); err != nil {
		t.Fatal(err)
	}
	properties, ok := published["properties"].(map[string]any)
	if !ok {
		t.Fatal("the schema has no properties")
	}
	for _, section := range []string{
		"schemaVersion", "project", "build", "mode", "artifacts",
		"policy", "output", "anchors", "discovery", "components", "manifests",
	} {
		if _, present := properties[section]; !present {
			t.Errorf("the schema does not mention %q, which the loader accepts", section)
		}
	}
}
