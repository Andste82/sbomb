package spdx23

import (
	"bytes"
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// The official SPDX 2.3 JSON Schema, embedded so that validation needs no
// network at runtime (sections 30.8 and 32.5). The file is
// https://raw.githubusercontent.com/spdx/spdx-spec/v2.3/schemas/spdx-schema.json
// byte for byte -- 45312 bytes, SHA-256
// 239208b7ac287b3cf5d9a9af23f9d69863971102a5e1587a27a398b43490b89b, which
// TestTheEmbeddedSchemaIsTheUpstreamFile pins -- the schema released with the
// version documents declare. It is draft-07, which the vendored validator
// implements.
//
// It is deliberately not the v2.3.1 errata schema. That one respells enum
// values (OPERATING-SYSTEM for OPERATING_SYSTEM) and would refuse what the
// SPDX reference implementation writes for "SPDX-2.3"; the stricter rules it
// adds -- documentNamespace required, the SPDXID, date and checksum patterns
// -- are checked by Validate instead (section 28.11.11, deviation D57).
//
//go:embed schema/spdx-schema-2.3.json
var schemaJSON []byte

// schemaURL is the identifier the schema declares, and is registered under.
const schemaURL = "http://spdx.org/rdf/terms/2.3"

// SchemaJSON returns the embedded schema, so that a user can see exactly what
// their document is checked against.
func SchemaJSON() []byte { return append([]byte(nil), schemaJSON...) }

var (
	compileOnce sync.Once
	compiledSch *jsonschema.Schema
	compileErr  error
)

var errorPrinter = message.NewPrinter(language.English)

func compiled() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
		if err != nil {
			compileErr = fmt.Errorf("parsing the embedded SPDX 2.3 schema: %w", err)
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(schemaURL, document); err != nil {
			compileErr = fmt.Errorf("registering the embedded SPDX 2.3 schema: %w", err)
			return
		}
		compiledSch, compileErr = compiler.Compile(schemaURL)
	})
	return compiledSch, compileErr
}

// validateAgainstSchema holds a decoded document to the embedded schema, and
// reports its deepest failures, at most maxReportedIssues of them.
func validateAgainstSchema(instance any) error {
	schema, err := compiled()
	if err != nil {
		return err
	}
	err = schema.Validate(instance)
	if err == nil {
		return nil
	}
	found := map[string]bool{}
	gatherSchemaIssues(err, found)
	lines := make([]string, 0, len(found))
	for issue := range found {
		lines = append(lines, "  "+issue)
	}
	return fmt.Errorf("SPDX 2.3 schema validation failed:\n%s", capped(lines))
}

func gatherSchemaIssues(err error, into map[string]bool) {
	validationError, ok := err.(*jsonschema.ValidationError)
	if !ok {
		into[err.Error()] = true
		return
	}
	if len(validationError.Causes) == 0 {
		location := "/" + strings.Join(validationError.InstanceLocation, "/")
		into[location+": "+validationError.ErrorKind.LocalizedString(errorPrinter)] = true
		return
	}
	for _, cause := range validationError.Causes {
		gatherSchemaIssues(cause, into)
	}
}

// maxReportedIssues is how many problems a failed check lists, as the 3.0.1
// and CycloneDX validators do: enough to fix a document by, few enough to
// read. capped is the 3.0.1 validator's, kept per version on purpose: it is
// report formatting, not part of the mapping both versions share, and no
// package below the two renderers is about reporting.
const maxReportedIssues = 20

func capped(lines []string) string {
	sort.Strings(lines)
	if len(lines) > maxReportedIssues {
		remaining := len(lines) - maxReportedIssues
		lines = append(lines[:maxReportedIssues:maxReportedIssues], fmt.Sprintf("  ... and %d more", remaining))
	}
	return strings.Join(lines, "\n")
}
