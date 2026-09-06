package cyclonedx

import (
	"bytes"
	"embed"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// errorPrinter renders validation messages. The library requires a printer;
// passing nil panics.
var errorPrinter = message.NewPrinter(language.English)

// The official CycloneDX schemas, embedded so that validation needs no
// network at runtime (specification sections 30.8 and 32.5). They are
// draft-07, not the draft 2020-12 that section 32.5 assumes; see
// docs/dev/deviations.md D6.
//
//go:embed schema/*.json
var schemaFS embed.FS

const (
	schemaBase       = "http://cyclonedx.org/schema/"
	schemaBOM16      = schemaBase + "bom-1.6.schema.json"
	schemaSPDX       = schemaBase + "spdx.schema.json"
	schemaJSF        = schemaBase + "jsf-0.82.schema.json"
	maxReportedIssue = 20
)

var (
	compileOnce sync.Once
	compiled    *jsonschema.Schema
	compileErr  error
)

// schemaFor returns the compiled CycloneDX 1.6 schema. Compilation happens
// once; the schemas are static.
func schemaFor() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		for url, name := range map[string]string{
			schemaBOM16: "schema/bom-1.6.schema.json",
			schemaSPDX:  "schema/spdx.schema.json",
			schemaJSF:   "schema/jsf-0.82.schema.json",
		} {
			file, err := schemaFS.Open(name)
			if err != nil {
				compileErr = fmt.Errorf("opening embedded %s: %w", name, err)
				return
			}
			document, err := jsonschema.UnmarshalJSON(file)
			file.Close()
			if err != nil {
				compileErr = fmt.Errorf("parsing embedded %s: %w", name, err)
				return
			}
			if err := compiler.AddResource(url, document); err != nil {
				compileErr = fmt.Errorf("registering %s: %w", url, err)
				return
			}
		}
		compiled, compileErr = compiler.Compile(schemaBOM16)
	})
	return compiled, compileErr
}

// ValidateAgainstSchema checks a document against the official CycloneDX 1.6
// JSON Schema. This is the first of the two layers section 32.5 requires; the
// second is ValidateDocument, which checks what a schema cannot express.
func ValidateAgainstSchema(data []byte) error {
	schema, err := schemaFor()
	if err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("the document is not valid JSON: %w", err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("CycloneDX schema validation failed:\n%s", summarizeSchemaError(err))
	}
	return nil
}

// summarizeSchemaError renders a validation failure as a short, deterministic
// list of locations. The raw error is a deeply nested tree that is unreadable
// in a CI log.
func summarizeSchemaError(err error) string {
	validationError, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return err.Error()
	}
	issues := map[string]bool{}
	collectSchemaIssues(validationError, issues)
	lines := make([]string, 0, len(issues))
	for issue := range issues {
		lines = append(lines, "  "+issue)
	}
	sort.Strings(lines)
	if len(lines) > maxReportedIssue {
		remaining := len(lines) - maxReportedIssue
		lines = lines[:maxReportedIssue]
		lines = append(lines, fmt.Sprintf("  ... and %d more", remaining))
	}
	return strings.Join(lines, "\n")
}

func collectSchemaIssues(err *jsonschema.ValidationError, into map[string]bool) {
	if len(err.Causes) == 0 {
		location := "/" + strings.Join(err.InstanceLocation, "/")
		into[location+": "+err.ErrorKind.LocalizedString(errorPrinter)] = true
		return
	}
	for _, cause := range err.Causes {
		collectSchemaIssues(cause, into)
	}
}

// EmbeddedSchema returns the embedded CycloneDX 1.6 JSON Schema, so that a
// user can see exactly what their document is checked against.
func EmbeddedSchema() (string, error) {
	data, err := schemaFS.ReadFile("schema/bom-1.6.schema.json")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
