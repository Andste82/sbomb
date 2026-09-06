// Command findingsdoc keeps the user-facing findings catalogue honest.
//
// Three sets have to agree: the identifiers appendix A of the specification
// defines, the ones the code actually emits, and the ones docs/findings.md
// documents. The catalogue in the user documentation is generated from the
// specification and annotated with what this build emits, so a finding cannot
// be documented without existing, and cannot be emitted without being
// documented.
//
//	go run ./tools/findingsdoc            regenerate docs/findings.md
//	go run ./tools/findingsdoc --check    fail if it is out of date
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

// entry is one row of appendix A.
type entry struct {
	ID       string
	Severity string
	Gate     string
	Meaning  string
}

func main() {
	specPath := flag.String("spec", "docs/dev/spec.md", "path to the specification")
	docPath := flag.String("doc", "docs/findings.md", "path to the findings document")
	root := flag.String("root", ".", "repository root to scan for emitted identifiers")
	check := flag.Bool("check", false, "fail if the document is out of date")
	flag.Parse()

	entries, err := readAppendixA(*specPath)
	if err != nil {
		fail(err)
	}
	if len(entries) == 0 {
		fail(fmt.Errorf("no findings catalogue found in %s", *specPath))
	}
	emitted, err := emittedIDs(*root)
	if err != nil {
		fail(err)
	}

	// An identifier the code emits that the specification does not define is a
	// real problem: it cannot be gated, waived or explained.
	defined := map[string]bool{}
	for _, e := range entries {
		defined[e.ID] = true
	}
	var undefined []string
	for id := range emitted {
		if !defined[id] {
			undefined = append(undefined, id)
		}
	}
	sort.Strings(undefined)
	if len(undefined) > 0 {
		fail(fmt.Errorf("emitted but not in appendix A: %s", strings.Join(undefined, ", ")))
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
			fail(fmt.Errorf("%s is out of date; run: go run ./tools/findingsdoc", *docPath))
		}
		fmt.Printf("findingsdoc: %s is current (%d identifiers, %d emitted)\n",
			*docPath, len(entries), len(emitted))
		return
	}
	if err := os.WriteFile(*docPath, []byte(updated), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("findingsdoc: wrote %s (%d identifiers, %d emitted)\n", *docPath, len(entries), len(emitted))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "findingsdoc:", err)
	os.Exit(1)
}

var rowPattern = regexp.MustCompile("^\\| `([A-Z0-9_]+)` \\| ([^|]*)\\| ([^|]*)\\| ([^|]*)\\|")

// readAppendixA parses the catalogue table out of the specification.
func readAppendixA(path string) ([]entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	start := strings.Index(text, "## Appendix A")
	if start < 0 {
		return nil, fmt.Errorf("no appendix A in %s", path)
	}
	rest := text[start:]
	if next := strings.Index(rest[len("## Appendix A"):], "\n## "); next >= 0 {
		rest = rest[:len("## Appendix A")+next]
	}
	entries := make([]entry, 0)
	for _, line := range strings.Split(rest, "\n") {
		match := rowPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		entries = append(entries, entry{
			ID:       match[1],
			Severity: strings.TrimSpace(match[2]),
			Gate:     strings.TrimSpace(match[3]),
			Meaning:  strings.TrimSpace(match[4]),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// emittedIDs finds the identifiers the code can actually produce: a string
// assigned to a field named ID in a composite literal, or the first argument of
// a helper whose name ends in "Finding". Matching on the shape rather than on
// the spelling keeps a table that merely *mentions* an identifier -- the gate
// mapping, for instance -- from counting as an emitter.
func emittedIDs(root string) (map[string]bool, error) {
	ids := map[string]bool{}
	fileSet := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git", "testdata", "node_modules", ".devcontainer":
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
			switch typed := node.(type) {
			case *ast.KeyValueExpr:
				key, ok := typed.Key.(*ast.Ident)
				if !ok || key.Name != "ID" {
					return true
				}
				if id, ok := stringLiteral(typed.Value); ok {
					ids[id] = true
				}
			case *ast.CallExpr:
				if !isFindingHelper(typed.Fun) || len(typed.Args) == 0 {
					return true
				}
				if id, ok := stringLiteral(typed.Args[0]); ok {
					ids[id] = true
				}
			}
			return true
		})
		return nil
	})
	return ids, err
}

func isFindingHelper(fun ast.Expr) bool {
	switch typed := fun.(type) {
	case *ast.Ident:
		return strings.HasSuffix(typed.Name, "Finding") || strings.HasSuffix(typed.Name, "finding")
	case *ast.SelectorExpr:
		return strings.HasSuffix(typed.Sel.Name, "Finding")
	}
	return false
}

func stringLiteral(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil || !isIdentifier(value) {
		return "", false
	}
	return value, true
}

var identifierPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{3,}$`)

func isIdentifier(value string) bool { return identifierPattern.MatchString(value) }

func render(entries []entry, emitted map[string]bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This build emits %d of the %d identifiers below. The rest are specified and\n",
		len(emitted), len(entries))
	b.WriteString("reserved: they describe evidence this version does not yet read, and a run will\n")
	b.WriteString("never report them. They are listed and marked so that the table is the whole\n")
	b.WriteString("catalogue rather than a snapshot of one version.\n\n")
	b.WriteString("| ID | Severity | Gate | Meaning | Status |\n|---|---|---|---|---|\n")
	for _, e := range entries {
		state := "emitted"
		if !emitted[e.ID] {
			state = "reserved"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", e.ID, e.Severity, e.Gate, e.Meaning, state)
	}
	return b.String()
}

func replaceSection(document, table string) (string, error) {
	start := strings.Index(document, beginMarker)
	end := strings.Index(document, endMarker)
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("markers %s / %s not found", beginMarker, endMarker)
	}
	return document[:start+len(beginMarker)] + "\n\n" + table + "\n" + document[end:], nil
}
