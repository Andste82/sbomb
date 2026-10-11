package spdx23

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
)

// stringsOf collects every non-empty string a value holds: struct fields,
// slice and map elements, and map keys.
func stringsOf(value reflect.Value, into map[string]bool) {
	switch value.Kind() {
	case reflect.String:
		if value.String() != "" {
			into[value.String()] = true
		}
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			stringsOf(value.Elem(), into)
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).IsExported() {
				stringsOf(value.Field(index), into)
			}
		}
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		for index := 0; index < value.Len(); index++ {
			stringsOf(value.Index(index), into)
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			stringsOf(iterator.Key(), into)
			stringsOf(iterator.Value(), into)
		}
	}
}

// renderedText is every string value of a rendered document, one per line.
func renderedText(t *testing.T, data []byte) string {
	t.Helper()
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	var lines []string
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case string:
			lines = append(lines, v)
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(root)
	// An element identifier encodes a local identity, which is a value of the
	// document it came from.
	for _, line := range append([]string(nil), lines...) {
		if local, ok := LocalIdentity(line); ok {
			lines = append(lines, local)
		}
	}
	return strings.Join(lines, "\n")
}

// notRendered are the values of the all-fields document that no SPDX 2.3
// document of sbomb's carries, each with the reason. Every reason names
// either a value no writer renders, or the point of deviation D57 (section
// 28.11.11) that says why 2.3 does not.
var notRendered = map[string]string{
	// Never rendered by any writer (sbomwritertest marks them as such).
	"info":                            "findings are the review report's, not the SBOM's",
	"SENTINEL_FINDING_NEVER_RENDERED": "findings are the review report's, not the SBOM's",
	"sentinel finding message never rendered": "findings are the review report's, not the SBOM's",
	"sentinel-bomref-never-read":              "BomRef is no writer's input",
	"sentinel-component-id-never-rendered":    "UsedFile.ComponentID is no writer's input",
	"sentinel-header-class-never-read":        "UsedFile.HeaderClass is read through its property",
	"sentinel remediation never rendered":     "the remediation belongs to the finding",
	"sentinel-files-never-read":               "Component.Files is no writer's input",
	"x":                                       "Component.Files is no writer's input",
	"finding":                                 "an internal marker of discovery",
	"MISSING_FILE_HASH":                       "an internal marker of discovery",
	"sentinel finding name, unused beside a valid expression": "a name is not preferred over an expression",
	"component:nowhere":       "a relation target that is nothing is skipped by every writer",
	"product":                 "a document identity, not a value; elements carry local identities",
	"component:kitchen-again": "a document identity; section 28.4 escalates a repeated name to name@version as its local identity",
	"project":                 "the anchor of a path, rendered within the canonical path",
	"build":                   "the anchor of a path, rendered within the canonical path",
	"sysroot":                 "the anchor of a path, rendered within the canonical path",
	"_deps/kitchen-src":       "a relative path, rendered within the canonical path",
	// Spelled the way every SPDX document spells them (section 28.11.5).
	"MIT AND Sentinel-Unlisted-1.0":      "an unlisted operand is replaced by a reference minted for it, whose name is the operand",
	"MIT or Sentinel-Header-Licence-2.0": "operators are written in upper case and the unlisted operand is minted",
	"SHA-256":                            "SPDX 2.3 spells the algorithm SHA256",
	// D57: a digest without a 2.3 algorithm is in the element comment as
	// key:value, so neither half stands alone as a value.
	"SENTINEL-UNKNOWN-KEY": "D57: a digest without a 2.3 algorithm travels in the comment as key:value",
	"abcdef0123":           "D57: a digest without a 2.3 algorithm travels in the comment as key:value",
}

// TestEveryFieldReachesTheDocumentOrIsAccountedFor: every value of the
// all-fields document appears in its 2.3 rendering, or the allow-list says
// why it does not.
func TestEveryFieldReachesTheDocumentOrIsAccountedFor(t *testing.T) {
	data := renderAllFields(t)
	text := renderedText(t, data)
	values := map[string]bool{}
	stringsOf(reflect.ValueOf(*sbomwritertest.AllFields()), values)
	var missing []string
	for value := range values {
		if _, allowed := notRendered[value]; allowed {
			continue
		}
		if !strings.Contains(text, value) && !strings.Contains(text, escapeValue(value)) {
			missing = append(missing, value)
		}
	}
	sort.Strings(missing)
	for _, value := range missing {
		t.Errorf("%q reaches no SPDX 2.3 document and is not accounted for", value)
	}
	// An allow-list entry for a value that is rendered after all is stale.
	for value, reason := range notRendered {
		if !values[value] {
			t.Errorf("%q is in the allow-list but not in the all-fields document", value)
		}
		if strings.Contains(text, "\n"+value+"\n") {
			t.Errorf("%q is rendered as a value of its own, but the allow-list says: %s", value, reason)
		}
	}
	if !strings.Contains(text, "SENTINEL-UNKNOWN-KEY:abcdef0123") {
		t.Error("the digest without a 2.3 algorithm is not in a comment")
	}
}
