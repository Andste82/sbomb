package spdx3

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// deepCopy copies a decoded JSON value, so that a mutation leaves the seed as
// it was.
func deepCopy(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		copied := make(map[string]any, len(typed))
		for key, item := range typed {
			copied[key] = deepCopy(item)
		}
		return copied
	case []any:
		copied := make([]any, len(typed))
		for index, item := range typed {
			copied[index] = deepCopy(item)
		}
		return copied
	}
	return value
}

// replacements are values of every JSON kind and of every string form the
// schema tells apart: an IRI, a blank node, a named individual, an enum
// value of the wrong case, and a word that is none of them.
var replacements = []any{
	json.Number("42"), true, nil, "word", "https://example.com/x", "_:b",
	"NoneElement", "NoAssertionElement", "SHA256", "sha256", "dependsOn",
	"source", "file", "complete", []any{}, []any{"https://example.com/x"},
	[]any{"word"}, []any{json.Number("1")}, map[string]any{},
	map[string]any{"type": "Hash", "algorithm": "sha256", "hashValue": "00"},
}

// variants are the seed and every mutation of it: each key removed, each key
// given each replacement, an unknown key added, and the nested values sbomb
// writes -- property entries, hashes, relationship ends -- bent one way at a
// time.
func variants(seed map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{"seed": seed}
	keys := make([]string, 0, len(seed))
	for key := range seed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		removed := deepCopy(seed).(map[string]any)
		delete(removed, key)
		out["without "+key] = removed
		for index, value := range replacements {
			replaced := deepCopy(seed).(map[string]any)
			replaced[key] = deepCopy(value)
			out[fmt.Sprintf("%s = replacement %d", key, index)] = replaced
		}
	}
	unknown := deepCopy(seed).(map[string]any)
	unknown["bogus"] = "x"
	out["unknown key"] = unknown

	nested := func(name string, edit func(map[string]any) bool) {
		copied := deepCopy(seed).(map[string]any)
		if edit(copied) {
			out[name] = copied
		}
	}
	entry := func(node map[string]any) map[string]any {
		extensions, _ := node["extension"].([]any)
		if len(extensions) == 0 {
			return nil
		}
		extension, _ := extensions[0].(map[string]any)
		entries, _ := extension["extension_cdxProperty"].([]any)
		if len(entries) == 0 {
			return nil
		}
		first, _ := entries[0].(map[string]any)
		return first
	}
	for name, edit := range map[string]func(map[string]any){
		"entry extra key":      func(e map[string]any) { e["@id"] = "_:p" },
		"entry unknown key":    func(e map[string]any) { e["bogus"] = "x" },
		"entry name a number":  func(e map[string]any) { e["extension_cdxPropName"] = json.Number("1") },
		"entry without name":   func(e map[string]any) { delete(e, "extension_cdxPropName") },
		"entry value a number": func(e map[string]any) { e["extension_cdxPropValue"] = json.Number("1") },
		"entry without value":  func(e map[string]any) { delete(e, "extension_cdxPropValue") },
		"entry another type":   func(e map[string]any) { e["type"] = "extension_CdxPropertiesExtension" },
		"entry without type":   func(e map[string]any) { delete(e, "type") },
	} {
		edit := edit
		nested(name, func(node map[string]any) bool {
			if found := entry(node); found != nil {
				edit(found)
				return true
			}
			return false
		})
	}
	for name, edit := range map[string]func(map[string]any){
		"extension no entries":    func(x map[string]any) { x["extension_cdxProperty"] = []any{} },
		"extension entries a map": func(x map[string]any) { x["extension_cdxProperty"] = map[string]any{} },
		"extension extra key":     func(x map[string]any) { x["@id"] = "_:e" },
		"extension another type":  func(x map[string]any) { x["type"] = "extension_Extension" },
		"extension a word":        func(x map[string]any) { x["type"] = "word" },
	} {
		edit := edit
		nested(name, func(node map[string]any) bool {
			extensions, _ := node["extension"].([]any)
			if len(extensions) == 0 {
				return false
			}
			extension, isObject := extensions[0].(map[string]any)
			if isObject {
				edit(extension)
			}
			return isObject
		})
	}
	for name, edit := range map[string]func(map[string]any){
		"hash algorithm upper case": func(h map[string]any) { h["algorithm"] = "SHA256" },
		"hash with comment":         func(h map[string]any) { h["comment"] = "c" },
		"hash unknown key":          func(h map[string]any) { h["bogus"] = "x" },
		"hash without value":        func(h map[string]any) { delete(h, "hashValue") },
		"hash value a number":       func(h map[string]any) { h["hashValue"] = json.Number("1") },
		"hash another type":         func(h map[string]any) { h["type"] = "PackageVerificationCode" },
	} {
		edit := edit
		nested(name, func(node map[string]any) bool {
			hashes, _ := node["verifiedUsing"].([]any)
			if len(hashes) == 0 {
				return false
			}
			hash, isObject := hashes[0].(map[string]any)
			if isObject {
				edit(hash)
			}
			return isObject
		})
	}
	for name, ends := range map[string][]any{
		"to a word beside an IRI": {"https://example.com/x", "word"},
		"to NoAssertionElement":   {"NoAssertionElement"},
		"to a license individual": {"expandedlicensing_NoneLicense"},
		"to a number":             {json.Number("1")},
	} {
		ends := ends
		nested(name, func(node map[string]any) bool {
			if _, has := node["to"]; !has {
				return false
			}
			node["to"] = deepCopy(ends)
			return true
		})
	}
	for name, items := range map[string][]any{
		"element a word":         {"word"},
		"element a number":       {json.Number("1")},
		"element an object":      {map[string]any{"type": "Bogus"}},
		"element an individual":  {"NoneElement"},
		"element a blank node":   {"_:b"},
		"rootElement a word":     {"word"},
		"rootElement individual": {"SpdxOrganization"},
	} {
		key := strings.Fields(name)[0]
		items := items
		nested(name, func(node map[string]any) bool {
			existing, isArray := node[key].([]any)
			if !isArray {
				return false
			}
			node[key] = append(deepCopy(existing).([]any), deepCopy(items).([]any)...)
			return true
		})
	}
	return out
}

// seedNodes are the nodes of the documents the tests render, every class
// sbomb writes, as parseDocument reads them: one node per class and set of
// keys, since nodes of the same make bend the same ways.
func seedNodes(t *testing.T) []map[string]any {
	t.Helper()
	var nodes []map[string]any
	seen := map[string]bool{}
	for _, data := range [][]byte{renderAllFields(t), small(t)} {
		parsed, err := parseDocument(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range parsed.nodes {
			keys := make([]string, 0, len(node))
			for key := range node {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			signature := nodeType(node) + " " + strings.Join(keys, ",")
			if !seen[signature] {
				seen[signature] = true
				nodes = append(nodes, node)
			}
		}
	}
	return nodes
}

// The shapes may only ever vouch for a node the schema accepts, and leaving
// out the items elide leaves out may not change what the schema says. Both
// are checked for every node the renderer writes and for every way of
// bending it the variants know, against the class definition the node path
// would otherwise use.
func TestTheShapesAgreeWithTheSchema(t *testing.T) {
	set, err := compiled()
	if err != nil {
		t.Fatal(err)
	}
	checked, vouched := 0, 0
	for _, seed := range seedNodes(t) {
		for name, node := range variants(seed) {
			schema := set.anyNode
			if class, ok := node["type"].(string); ok && set.byClass[class] != nil {
				schema = set.byClass[class]
			}
			accepted := schema.Validate(node) == nil
			if set.shapes.conforms(node) {
				vouched++
				if !accepted {
					t.Errorf("%s, %s: the shapes vouch for a node the schema refuses", describe(seed), name)
				}
			}
			if elided := schema.Validate(set.shapes.elide(node)) == nil; elided != accepted {
				t.Errorf("%s, %s: the schema says %v of the node and %v of it elided", describe(seed), name, accepted, elided)
			}
			checked++
		}
	}
	if vouched == 0 {
		t.Fatal("the shapes vouched for nothing; the test does not exercise them")
	}
	t.Logf("%d nodes checked, %d vouched for", checked, vouched)
}

// The shapes are only worth having if they cover what sbomb writes: every
// file and relationship of a rendering without licence texts or external
// references.
func TestTheShapesCoverWhatSbombWrites(t *testing.T) {
	set, err := compiled()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDocument(small(t))
	if err != nil {
		t.Fatal(err)
	}
	counted := map[string]int{}
	for _, node := range parsed.nodes {
		class := nodeType(node)
		if class != "software_File" && class != "Relationship" {
			continue
		}
		counted[class]++
		if !set.shapes.conforms(node) {
			t.Errorf("%s has none of the shapes", describe(node))
		}
	}
	if counted["software_File"] == 0 || counted["Relationship"] == 0 {
		t.Fatalf("the document has %v; the test needs files and relationships", counted)
	}
}

// parseDocument reads the document element by element and shares its strings;
// what it reads has to be what decoding the whole document reads.
func TestParseDocumentReadsWhatDecodingReads(t *testing.T) {
	documents := map[string]string{
		"small":          string(small(t)),
		"all fields":     string(renderAllFields(t)),
		"numbers":        `{"@context": "c", "@graph": [{"n": 1.50, "m": -2e3, "deep": [{"x": [1, [2]]}]}], "v": 3}`,
		"repeated key":   `{"@graph": [{"a": 1}], "@context": "c", "@graph": [{"b": 2}]}`,
		"single node":    `{"@context": "c", "type": "software_File", "spdxId": "https://x/f", "o": {"k": [true, null]}}`,
		"graph a map":    `{"@context": "c", "@graph": {"a": 1}}`,
		"graph a string": `{"@context": "c", "@graph": "x"}`,
		"empty":          `{}`,
		"long strings":   `{"@graph": [{"` + strings.Repeat("k", 60) + `": "` + strings.Repeat("v", 60) + `"}]}`,
	}
	for name, document := range documents {
		var want map[string]any
		decoder := json.NewDecoder(strings.NewReader(document))
		decoder.UseNumber()
		if err := decoder.Decode(&want); err != nil {
			t.Fatal(name, err)
		}
		parsed, err := parseDocument([]byte(document))
		if err != nil {
			if name == "graph a map" || name == "graph a string" {
				continue // refused after reading, as before
			}
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(parsed.root, want) {
			t.Errorf("%s: parseDocument read\n%#v\nwant\n%#v", name, parsed.root, want)
		}
		if parsed.trailing {
			t.Errorf("%s: trailing content reported where there is none", name)
		}
	}
	for name, document := range map[string]string{
		"trailing value": `{"@graph": []} {}`,
		"trailing word":  `{"@graph": []} x`,
	} {
		parsed, err := parseDocument([]byte(document))
		if err == nil && !parsed.trailing {
			t.Errorf("%s: what follows the document went unnoticed", name)
		}
		_, schemaErr := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(document)))
		if schemaErr == nil {
			t.Errorf("%s: the schema's own reading accepts it", name)
		}
	}
	for name, document := range map[string]string{
		"an array":   `[{"@graph": []}]`,
		"a string":   `"x"`,
		"cut off":    `{"@graph": [{"a": 1}`,
		"bad member": `{"@graph": [{"a": }]}`,
	} {
		if _, err := parseDocument([]byte(document)); err == nil {
			t.Errorf("%s: parseDocument accepted it", name)
		}
	}
}

// ValidateOwnOutput is Validate and then CheckOwnOutput over one decoding;
// it has to come to the same verdict, in the same words.
func TestValidateOwnOutputIsValidateThenCheck(t *testing.T) {
	base := small(t)
	cases := map[string][]byte{
		"valid":      base,
		"all fields": renderAllFields(t),
		"schema": mutate(t, base, func(_ map[string]any, graph []any) []any {
			findNode(graph, "component:zlib")["software_primaryPurpose"] = "everything"
			return graph
		}),
		"conformance": mutate(t, base, func(_ map[string]any, graph []any) []any {
			return append(graph, deepCopy(findNode(graph, "component:zlib")))
		}),
		"own output": mutate(t, base, func(_ map[string]any, graph []any) []any {
			findNode(graph, "component:zlib")["name"] = "  "
			return graph
		}),
		"trailing": append(append([]byte(nil), base...), []byte(" {}")...),
	}
	for name, data := range cases {
		want := Validate(data)
		if want == nil {
			want = CheckOwnOutput(data)
		}
		got := ValidateOwnOutput(data)
		if (got == nil) != (want == nil) || (got != nil && got.Error() != want.Error()) {
			t.Errorf("%s: ValidateOwnOutput = %v, want %v", name, got, want)
		}
		if name != "valid" && name != "all fields" && got == nil {
			t.Errorf("%s: the broken document passed", name)
		}
	}
}
