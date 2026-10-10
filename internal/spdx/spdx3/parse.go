package spdx3

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// parsedDocument is a 3.0.1 document read for checking: every node as the
// generic JSON value it is, so that a check can look at any key of any class,
// including the ones sbomb never writes.
type parsedDocument struct {
	root    map[string]any
	context any
	// nodes are the top-level nodes: the @graph, or the root itself for a
	// document of one node.
	nodes []map[string]any
	// byID is every top-level node by its spdxId or @id.
	byID map[string]map[string]any
}

func parseDocument(data []byte) (*parsedDocument, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("the document is not a JSON object: %w", err)
	}
	parsed := &parsedDocument{root: root, context: root["@context"], byID: map[string]map[string]any{}}
	if graph, isGraph := root["@graph"]; isGraph {
		items, isArray := graph.([]any)
		if !isArray {
			return nil, fmt.Errorf("@graph is not an array")
		}
		for index, item := range items {
			object, isObject := item.(map[string]any)
			if !isObject {
				return nil, fmt.Errorf("@graph[%d] is not an object", index)
			}
			parsed.nodes = append(parsed.nodes, object)
		}
	} else {
		single := map[string]any{}
		for key, value := range root {
			if key != "@context" {
				single[key] = value
			}
		}
		parsed.nodes = []map[string]any{single}
	}
	for _, node := range parsed.nodes {
		if id := nodeID(node); id != "" {
			if _, taken := parsed.byID[id]; !taken {
				parsed.byID[id] = node
			}
		}
	}
	return parsed, nil
}

// nodeID is the identifier of a node: its spdxId, or its @id.
func nodeID(node map[string]any) string {
	if id, ok := node["spdxId"].(string); ok && id != "" {
		return id
	}
	if id, ok := node["@id"].(string); ok {
		return id
	}
	return ""
}

func nodeType(node map[string]any) string {
	class, _ := node["type"].(string)
	return class
}

func stringValue(node map[string]any, key string) string {
	value, _ := node[key].(string)
	return value
}

// stringList reads a key that holds IRIs: one string, or an array of them.
// Anything else -- an inlined object above all -- is reported as not a string,
// so a check can tell "refers to" from "contains".
func stringList(node map[string]any, key string) (values []string, allStrings bool) {
	switch value := node[key].(type) {
	case nil:
		return nil, true
	case string:
		return []string{value}, true
	case []any:
		allStrings = true
		for _, item := range value {
			text, isString := item.(string)
			if !isString {
				allStrings = false
				continue
			}
			values = append(values, text)
		}
		return values, allStrings
	default:
		return nil, false
	}
}

// walkObjects calls visit for every JSON object inside a value, at any depth,
// the value itself included.
func walkObjects(value any, visit func(map[string]any)) {
	switch typed := value.(type) {
	case map[string]any:
		visit(typed)
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			walkObjects(typed[key], visit)
		}
	case []any:
		for _, item := range typed {
			walkObjects(item, visit)
		}
	}
}

// issues collects what a check finds, so that a document is reported with
// everything wrong with it and not only the first thing.
type issues struct{ lines map[string]bool }

func (i *issues) addf(format string, args ...any) {
	if i.lines == nil {
		i.lines = map[string]bool{}
	}
	i.lines["  "+fmt.Sprintf(format, args...)] = true
}

func (i *issues) err(heading string) error {
	if len(i.lines) == 0 {
		return nil
	}
	lines := make([]string, 0, len(i.lines))
	for line := range i.lines {
		lines = append(lines, line)
	}
	return fmt.Errorf("%s:\n%s", heading, capped(lines))
}

// describe names a node in a message: its identifier, or its type.
func describe(node map[string]any) string {
	if id := nodeID(node); id != "" {
		return id
	}
	if class := nodeType(node); class != "" {
		return "a " + class + " node"
	}
	return "a node without a type"
}

// profileOfName is the profile a serialized class or property name belongs
// to: the prefix before the first underscore, and Core for a name without one.
func profileOfName(name string) string {
	prefix, _, found := strings.Cut(name, "_")
	if !found {
		return "core"
	}
	switch prefix {
	case "software":
		return "software"
	case "simplelicensing":
		return "simpleLicensing"
	case "expandedlicensing":
		return "expandedLicensing"
	case "security":
		return "security"
	case "build":
		return "build"
	case "dataset":
		return "dataset"
	case "ai":
		return "ai"
	case "extension":
		return "extension"
	default:
		return "core"
	}
}

// sortedKeys is the keys of a map in byte order, so that what a check reports
// does not depend on map iteration.
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
