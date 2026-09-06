// Command propertydoc keeps the property catalogue honest.
//
// It is the counterpart of tools/findingsdoc, for the other catalogue. Three
// sets have to agree: the property names appendix B of the specification
// defines, the ones the code writes into a document, and the ones
// docs/properties.md documents. A property that reaches somebody's SBOM with
// no catalogue entry cannot be looked up; a catalogue entry nothing writes
// describes a document sbomb does not produce.
//
// Both were true when this was written: eleven properties were emitted and
// uncatalogued, and thirty-nine were catalogued and never emitted.
//
//	go run ./tools/propertydoc            regenerate docs/properties.md
//	go run ./tools/propertydoc --check    fail if it is out of date
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	beginMarker = "<!-- BEGIN GENERATED CATALOGUE -->"
	endMarker   = "<!-- END GENERATED CATALOGUE -->"
)

// entry is one property of appendix B.
type entry struct {
	Name  string
	Group string
	// Note is the parenthetical the catalogue carries for some properties:
	// the value set, or that the property repeats.
	Note string
}

// propertyName matches a full property name, never the bare "sbomb:" prefix a
// validator tests against.
var propertyName = regexp.MustCompile(`^sbomb:[A-Za-z][A-Za-z0-9]*(?::[A-Za-z][A-Za-z0-9]*)+$`)

func main() {
	specPath := flag.String("spec", filepath.Join("docs", "dev", "spec.md"), "path to the specification")
	docPath := flag.String("doc", filepath.Join("docs", "properties.md"), "path to the property document")
	root := flag.String("root", ".", "repository root to scan for emitted properties")
	check := flag.Bool("check", false, "fail if the document is out of date")
	flag.Parse()

	entries, err := readAppendixB(*specPath)
	if err != nil {
		fail(err)
	}
	if len(entries) == 0 {
		fail(fmt.Errorf("no property catalogue found in %s", *specPath))
	}
	emitted, err := emittedProperties(*root)
	if err != nil {
		fail(err)
	}

	// A property the code writes that the catalogue does not define reaches a
	// consumer's document with nowhere to look it up.
	defined := map[string]bool{}
	for _, e := range entries {
		defined[e.Name] = true
	}
	var undefined []string
	for name := range emitted {
		if !defined[name] {
			undefined = append(undefined, name)
		}
	}
	sort.Strings(undefined)
	if len(undefined) > 0 {
		fail(fmt.Errorf("emitted but not in appendix B: %s", strings.Join(undefined, ", ")))
	}

	table := render(entries, emitted)
	current, err := os.ReadFile(*docPath)
	if err != nil {
		fail(err)
	}
	updated, err := replaceSection(string(current), table)
	if err != nil {
		fail(err)
	}
	if *check {
		if updated != string(current) {
			fail(fmt.Errorf("%s is out of date; run: go run ./tools/propertydoc", *docPath))
		}
		fmt.Printf("propertydoc: %s is current (%d properties, %d emitted)\n",
			*docPath, len(entries), len(emitted))
		return
	}
	if err := os.WriteFile(*docPath, []byte(updated), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("propertydoc: wrote %s (%d properties, %d emitted)\n", *docPath, len(entries), len(emitted))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "propertydoc:", err)
	os.Exit(1)
}

var (
	groupHeading = regexp.MustCompile(`^\*\*(.+?)\*\*$`)
	nameInLine   = regexp.MustCompile(`sbomb:[A-Za-z][A-Za-z0-9]*(?::[A-Za-z][A-Za-z0-9]*)+`)
	noteInLine   = regexp.MustCompile(`\((.+?)\)`)
)

// readAppendixB parses the catalogue out of the specification. The catalogue is
// a set of fenced blocks under bold group headings, one or two names per line,
// with an occasional parenthetical note.
func readAppendixB(path string) ([]entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	start := strings.Index(text, "## Appendix B")
	if start < 0 {
		return nil, fmt.Errorf("no appendix B in %s", path)
	}
	section := text[start:]
	if next := strings.Index(section[len("## Appendix B"):], "\n## "); next >= 0 {
		section = section[:next+len("## Appendix B")]
	}

	var entries []entry
	seen := map[string]bool{}
	group := ""
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if heading := groupHeading.FindStringSubmatch(trimmed); heading != nil {
			group = strings.TrimSuffix(heading[1], ":")
			continue
		}
		names := nameInLine.FindAllString(trimmed, -1)
		if len(names) == 0 {
			continue
		}
		note := ""
		// A note belongs to the line's single property; a line naming two
		// carries none, which is how the catalogue is written.
		if len(names) == 1 {
			if match := noteInLine.FindStringSubmatch(trimmed); match != nil {
				note = match[1]
			}
		}
		for _, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			entries = append(entries, entry{Name: name, Group: group, Note: note})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// emittedProperties collects the property names the code writes. A name is a
// string literal; there is no helper to key on as there is for findings.
func emittedProperties(root string) (map[string]bool, error) {
	names := map[string]bool{}
	fileSet := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git", "testdata", "node_modules", ".devcontainer", "propertydoc":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", path, parseErr)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil || !propertyName.MatchString(value) {
				return true
			}
			names[value] = true
			return true
		})
		return nil
	})
	return names, err
}

func render(entries []entry, emitted map[string]bool) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, `
This build writes %d of the %d properties below. The rest are specified and
reserved: they describe evidence this version does not yet record, and no
document sbomb writes will contain them. They are listed and marked so that the
table is the whole catalogue rather than a snapshot of one version.

| Property | Where | Values | Status |
|---|---|---|---|
`, countEmitted(entries, emitted), len(entries))
	for _, e := range entries {
		status := "reserved"
		if emitted[e.Name] {
			status = "emitted"
		}
		note := e.Note
		if note == "" {
			note = "—"
		}
		group := e.Group
		if group == "" {
			group = "—"
		}
		fmt.Fprintf(&builder, "| `%s` | %s | %s | %s |\n", e.Name, group, note, status)
	}
	return builder.String()
}

func countEmitted(entries []entry, emitted map[string]bool) int {
	count := 0
	for _, e := range entries {
		if emitted[e.Name] {
			count++
		}
	}
	return count
}

func replaceSection(document, table string) (string, error) {
	start := strings.Index(document, beginMarker)
	end := strings.Index(document, endMarker)
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("markers %q and %q not found", beginMarker, endMarker)
	}
	return document[:start+len(beginMarker)] + table + document[end:], nil
}
