package config

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// The configuration schema is derived from the Go types rather than written
// beside them.
//
// It was written beside them once, and it drifted: `sbomb schema` published a
// document naming five of the eleven sections the loader accepts, with
// additionalProperties false, so every documented example configuration failed
// against the schema the tool itself hands out. A schema that describes
// something other than the loader is worse than none, because it is machine
// readable and therefore believed.
//
// Deriving it removes the possibility. The loader refuses unknown fields at
// every level (deviation D21) and so does this, from the same struct tags, so
// the two cannot disagree about what a valid file is.

// enums are the closed value sets `validate` enforces. They cannot be read off
// a Go type -- a string field admits any string -- so they are listed here, by
// the path of the field they belong to.
var enums = map[string][]string{
	"mode":           {"single", "assembly"},
	"artifacts.role": {"application", "bootloader", "library", "filesystem", "image", "package", "data", "other"},
	// The output enums are the writer registry's answer, written here because
	// the loader must not depend on a serializer. TestConfigEnumsMatchTheWriter
	// in cmd/sbomb imports both and fails if they part company.
	"output.format":                    {"cyclonedx-json"},
	"output.specVersion":               {"1.6", "1.7"},
	"policy.headerEvidence":            {"dwarf-preferred", "union", "depfiles"},
	"policy.includeToolchainRuntime":   {"separate-component", "report-only", "exclude"},
	"policy.systemLibraries":           {"exclude", "separate-component", "report-only"},
	"policy.pchHeaders":                {"include", "exclude", "annotate-only"},
	"policy.sectionGarbageCollection":  {"ignore", "annotate", "exclude"},
	"policy.profile":                   {"default", "lenient", "cra", "strict"},
	"policy.profileOverlay":            {"host-linux"},
	"components.versionFrom":           nil, // free-form rules; see documentation
	"discovery.excludeTargetPatterns":  nil,
	"build.introspection.cmake":        nil,
	"project.type":                     nil,
	"components.type":                  nil,
	"policy.severityOverrides.default": {"error", "warning", "info"},
}

// required are the fields `validate` and `Load` insist on, by path.
var required = map[string][]string{
	"":        {"project"},
	"project": {"name"},
	"build":   {"dir"},
}

// Schema returns the JSON Schema of the configuration file.
func Schema() string {
	document := objectSchema(reflect.TypeOf(Config{}), "")
	document["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	document["title"] = "sbomb configuration"
	document["description"] = "Derived from the Go types the loader uses, so the two cannot disagree."
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		// Unreachable: every value put into the map is JSON-encodable.
		return "{}"
	}
	return string(encoded)
}

// objectSchema describes a struct. path is the dotted field path, used to look
// up the enum and required tables.
func objectSchema(typ reflect.Type, path string) map[string]any {
	properties := map[string]any{}
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		name := jsonName(field)
		if name == "" {
			continue
		}
		properties[name] = fieldSchema(field.Type, join(path, name))
	}
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
		// The loader decodes with DisallowUnknownFields, so anything else is
		// an error there too. A schema that permitted more would be the more
		// permissive of two checks and therefore useless.
		"additionalProperties": false,
	}
	if names := required[path]; len(names) > 0 {
		sort.Strings(names)
		schema["required"] = names
	}
	return schema
}

func fieldSchema(typ reflect.Type, path string) map[string]any {
	// StringList accepts a bare string or an array of them, which is a shape
	// no Go type expresses; its UnmarshalJSON is the authority.
	if typ == reflect.TypeOf(StringList{}) {
		return map[string]any{
			"description": "A string, or an array of strings.",
			"oneOf": []any{
				map[string]any{"type": "string"},
				map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		}
	}

	switch typ.Kind() {
	case reflect.Pointer:
		// A pointer is how the policy distinguishes "absent" from "explicitly
		// false"; the value it points at is what a document carries.
		return fieldSchema(typ.Elem(), path)
	case reflect.String:
		schema := map[string]any{"type": "string"}
		if values, listed := enums[path]; listed && len(values) > 0 {
			schema["enum"] = values
		}
		return schema
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": fieldSchema(typ.Elem(), path)}
	case reflect.Map:
		value := fieldSchema(typ.Elem(), path+".default")
		return map[string]any{"type": "object", "additionalProperties": value}
	case reflect.Struct:
		return objectSchema(typ, path)
	default:
		// No field has another kind; describing one loosely would be worse
		// than saying nothing about it.
		return map[string]any{}
	}
}

// jsonName is the field's name in a document, or "" when it has none.
func jsonName(field reflect.StructField) string {
	if field.PkgPath != "" {
		return ""
	}
	tag := field.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return field.Name
	}
	return name
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}
