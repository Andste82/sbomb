package spdx23

import (
	"fmt"
	"strings"

	"github.com/example/sbomb/internal/spdx/mapping"
)

// SPDX 2.3 has no name/value field on an element. sbomb's properties
// (appendix B) travel as annotations instead (section 28.11.11, deviation
// D57): annotationType OTHER, the tool as annotator, the creation time as the
// date, and a comment of one "name=value" line per property, in the model's
// order. The comment is free text by the specification and machine-readable
// by this convention: a value's backslash, line feed and carriage return are
// written "\\", "\n" and "\r", so that one line is always one property.
//
// Three kinds of annotation are written, each recognisable by its first line:
//
//   - a property set: every line is "sbomb:...=value";
//   - a qualified licence statement: "<field>: <expression>" (the field is
//     licenseConcluded, licenseDeclared or licenseInfoInFiles), then the
//     properties that qualify that one statement -- 2.3 has nowhere to attach
//     them but the element the statement is about;
//   - an upstream claim that a CVE does not apply: "not affected: <id>:
//     <statement>", because 2.3 has no VEX.

// annotationType is the annotation type of everything sbomb annotates.
const annotationType = "OTHER"

// licenceStatementFields are the fields a qualified licence statement
// annotation can name.
var licenceStatementFields = []string{"licenseConcluded", "licenseDeclared", "licenseInfoInFiles"}

// notAffectedPrefix starts the annotation of a CVE exclusion.
const notAffectedPrefix = "not affected: "

// escapeValue writes a value so that it stays on one line.
func escapeValue(value string) string {
	if !strings.ContainsAny(value, "\\\n\r") {
		return value
	}
	var builder strings.Builder
	for _, r := range value {
		switch r {
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// unescapeValue reverses escapeValue, refusing an escape it cannot have
// written.
func unescapeValue(value string) (string, error) {
	if !strings.Contains(value, `\`) {
		return value, nil
	}
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' {
			builder.WriteByte(value[index])
			continue
		}
		if index+1 >= len(value) {
			return "", fmt.Errorf("a value ends in an unpaired backslash")
		}
		index++
		switch value[index] {
		case '\\':
			builder.WriteByte('\\')
		case 'n':
			builder.WriteByte('\n')
		case 'r':
			builder.WriteByte('\r')
		default:
			return "", fmt.Errorf("a value holds the unknown escape \\%c", value[index])
		}
	}
	return builder.String(), nil
}

// propertyLines is a property set as annotation lines.
func propertyLines(properties []mapping.Property) []string {
	lines := make([]string, 0, len(properties))
	for _, property := range properties {
		lines = append(lines, property.Name+"="+escapeValue(property.Value))
	}
	return lines
}

// ParseProperties reads the "name=value" lines of an annotation comment sbomb
// wrote back into properties.
func ParseProperties(lines []string) ([]mapping.Property, error) {
	properties := make([]mapping.Property, 0, len(lines))
	for _, line := range lines {
		name, value, found := strings.Cut(line, "=")
		if !found || !strings.HasPrefix(name, "sbomb:") || strings.ContainsAny(name, " \t") {
			return nil, fmt.Errorf("the annotation line %q is not an sbomb:name=value property", line)
		}
		decoded, err := unescapeValue(value)
		if err != nil {
			return nil, fmt.Errorf("the annotation line %q: %w", line, err)
		}
		properties = append(properties, mapping.Property{Name: name, Value: decoded})
	}
	return properties, nil
}

// annotationKind says which of the three kinds an annotation comment sbomb
// wrote is, and checks that it is well-formed.
func annotationKind(comment string) (string, error) {
	lines := strings.Split(comment, "\n")
	if strings.HasPrefix(lines[0], notAffectedPrefix) {
		rest := strings.TrimPrefix(lines[0], notAffectedPrefix)
		id, statement, found := strings.Cut(rest, ": ")
		if len(lines) != 1 || !found || id == "" || statement == "" {
			return "", fmt.Errorf("the CVE exclusion annotation %q is not \"not affected: <id>: <statement>\"", comment)
		}
		if _, err := unescapeValue(statement); err != nil {
			return "", err
		}
		return "not-affected", nil
	}
	for _, field := range licenceStatementFields {
		if expression, found := strings.CutPrefix(lines[0], field+": "); found {
			if expression == "" {
				return "", fmt.Errorf("the licence statement annotation %q names no licence", comment)
			}
			if len(lines) < 2 {
				return "", fmt.Errorf("the licence statement annotation %q carries no qualifying property", comment)
			}
			if _, err := ParseProperties(lines[1:]); err != nil {
				return "", err
			}
			return "statement", nil
		}
	}
	if _, err := ParseProperties(lines); err != nil {
		return "", err
	}
	return "properties", nil
}
