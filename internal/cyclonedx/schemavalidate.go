package cyclonedx

import (
	"bytes"
	"embed"
	"fmt"
	"path/filepath"
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
// docs/dev/deviations.md D6 -- 1.7 is draft-07 too, so that deviation is
// unaffected by the second version.
//
// Both document versions are here rather than only the default. Half a
// megabyte of JSON is what it costs to let `validate` check a document
// somebody else wrote, whichever of the two it is, and a validator that can
// only check what this build happens to emit is not a validator.
//
// They are stored uncompressed, unlike the SPDX templates beside them. The
// templates are a generated blob nobody reads, so gzip costs nothing there.
// These are published documents checked in verbatim, and the reason to keep
// them readable is that a reviewer can diff one against the schema upstream
// publishes and see that it is the same file. Compressing them would trade
// that for a third of a megabyte.
//
//go:embed schema/*.json
var schemaFS embed.FS

const (
	schemaBase       = "http://cyclonedx.org/schema/"
	schemaSPDX       = schemaBase + "spdx.schema.json"
	schemaJSF        = schemaBase + "jsf-0.82.schema.json"
	schemaCrypto     = schemaBase + "cryptography-defs.schema.json"
	maxReportedIssue = 20
)

// schemaFiles names the embedded document schema of every version this build
// writes. Both are embedded rather than only the default, so that `validate`
// can check either -- including a document sbomb did not write.
var schemaFiles = map[string]string{
	Version16: "schema/bom-1.6.schema.json",
	Version17: "schema/bom-1.7.schema.json",
}

// supportFiles are the schemas the document schemas reference. 1.6 needs SPDX
// and JSF; 1.7 adds the cryptographic-algorithm definitions. Registering all
// of them for both compilations costs nothing and keeps the table honest.
var supportFiles = map[string]string{
	schemaSPDX:   "schema/spdx.schema.json",
	schemaJSF:    "schema/jsf-0.82.schema.json",
	schemaCrypto: "schema/cryptography-defs.schema.json",
}

var (
	compileMutex sync.Mutex
	compiled     = map[string]*jsonschema.Schema{}
)

// schemaFor returns the compiled CycloneDX schema of one version. Compilation
// happens once per version; the schemas are static, and compiling the one that
// is never used would be pure start-up cost.
func schemaFor(specVersion string) (*jsonschema.Schema, error) {
	name, known := schemaFiles[specVersion]
	if !known {
		return nil, fmt.Errorf("no embedded schema for CycloneDX %s; this build has %s", specVersion, strings.Join(supportedVersions, ", "))
	}

	compileMutex.Lock()
	defer compileMutex.Unlock()
	if schema, done := compiled[specVersion]; done {
		return schema, nil
	}

	compiler := jsonschema.NewCompiler()
	resources := map[string]string{schemaBase + filepath.Base(name): name}
	for url, file := range supportFiles {
		resources[url] = file
	}
	for url, file := range resources {
		document, err := readEmbeddedSchema(file)
		if err != nil {
			return nil, err
		}
		if err := compiler.AddResource(url, document); err != nil {
			return nil, fmt.Errorf("registering %s: %w", url, err)
		}
	}
	schema, err := compiler.Compile(schemaBase + filepath.Base(name))
	if err != nil {
		return nil, err
	}
	compiled[specVersion] = schema
	return schema, nil
}

// declaredSpecVersion reads the version a parsed document claims, without
// judging it. Anything that is not a document with a string specVersion is the
// empty string.
func declaredSpecVersion(instance any) string {
	object, isObject := instance.(map[string]any)
	if !isObject {
		return ""
	}
	version, isString := object["specVersion"].(string)
	if !isString {
		return ""
	}
	return version
}

func readEmbeddedSchema(name string) (any, error) {
	file, err := schemaFS.Open(name)
	if err != nil {
		return nil, fmt.Errorf("opening embedded %s: %w", name, err)
	}
	defer file.Close()
	document, err := jsonschema.UnmarshalJSON(file)
	if err != nil {
		return nil, fmt.Errorf("parsing embedded %s: %w", name, err)
	}
	return document, nil
}

// ValidateAgainstSchema checks a document against the official CycloneDX JSON
// Schema of the version the document itself declares. This is the first of the
// two layers section 32.5 requires; the second is ValidateDocument, which
// checks what a schema cannot express.
//
// The version comes from the document rather than from the caller on purpose:
// a file is checked against what it claims to be, so a 1.7 document sbomb did
// not write is checked as 1.7 and not silently held to 1.6.
func ValidateAgainstSchema(data []byte) error {
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("the document is not valid JSON: %w", err)
	}
	// The version is read off the value already parsed rather than by parsing
	// the document a second time: generate validates every document it writes,
	// and a fifty-thousand-unit BOM is not a document to walk twice for one
	// string.
	specVersion := declaredSpecVersion(instance)
	if specVersion == "" {
		// A document that does not say which version it is gets checked
		// against the default, so that the schema reports what is wrong with
		// it rather than this layer reporting only that it could not choose.
		specVersion = Writer{}.DefaultVersion()
	}
	schema, err := schemaFor(specVersion)
	if err != nil {
		return err
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

// EmbeddedSchema returns the embedded CycloneDX JSON Schema of one version, so
// that a user can see exactly what their document is checked against. An empty
// version is the writer's default.
func EmbeddedSchema(specVersion string) (string, error) {
	if specVersion == "" {
		specVersion = Writer{}.DefaultVersion()
	}
	name, known := schemaFiles[specVersion]
	if !known {
		return "", fmt.Errorf("no embedded schema for CycloneDX %s; this build has %s", specVersion, strings.Join(supportedVersions, ", "))
	}
	data, err := schemaFS.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
