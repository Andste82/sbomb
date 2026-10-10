package spdx3

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// testNamespace is the namespace every rendering in these tests uses, so that
// IRIs can be written out in assertions.
const testNamespace = "00000000-0000-5000-8000-000000000000"

func iriOf(local string) string { return iriFor(testNamespace, local) }

// render maps and renders a document the way the writer does, with a fixed
// namespace, and holds the result to both tiers.
func render(t *testing.T, document *sbomwriter.Document, options sbomwriter.Options) []byte {
	t.Helper()
	model, err := mapping.Build(document, mapping.Options{SpecVersion: specVersion, LicenseText: options.LicenseText, Reproducible: options.Reproducible, CustomAdditions: true})
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := Render(&buffer, model, testNamespace); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	if err := Validate(data); err != nil {
		t.Fatalf("the rendering is not conformant: %v", err)
	}
	if err := CheckOwnOutput(data); err != nil {
		t.Fatalf("the rendering breaks an invariant: %v", err)
	}
	return data
}

func renderAllFields(t *testing.T) []byte {
	t.Helper()
	return render(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions())
}

// graphOf decodes the @graph of a rendering.
func graphOf(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var root struct {
		Graph []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	return root.Graph
}

// nodeAt is the node of a local identity.
func nodeAt(t *testing.T, data []byte, local string) map[string]any {
	t.Helper()
	for _, node := range graphOf(t, data) {
		if node["spdxId"] == iriOf(local) {
			return node
		}
	}
	t.Fatalf("no node %s", local)
	return nil
}

// relationshipsFrom lists the relationships of one source, as
// "type -> local,local".
func relationshipsFrom(t *testing.T, data []byte, local string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, node := range graphOf(t, data) {
		if node["from"] == iriOf(local) {
			out = append(out, node)
		}
	}
	return out
}

// targetsOf is the local identities a relationship of one type from one
// source points at, across every such relationship.
func targetsOf(t *testing.T, data []byte, from, relationshipType string) []string {
	t.Helper()
	var out []string
	for _, rel := range relationshipsFrom(t, data, from) {
		if rel["relationshipType"] != relationshipType {
			continue
		}
		for _, to := range rel["to"].([]any) {
			out = append(out, localOf(to.(string)))
		}
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// propertiesOf is the extension properties of a node, as name=value.
func propertiesOf(node map[string]any) []string {
	var out []string
	extensions, _ := node["extension"].([]any)
	for _, extension := range extensions {
		entries := extension.(map[string]any)["extension_cdxProperty"].([]any)
		for _, entry := range entries {
			object := entry.(map[string]any)
			out = append(out, object["extension_cdxPropName"].(string)+"="+object["extension_cdxPropValue"].(string))
		}
	}
	return out
}

// mutate decodes a rendering, lets edit change it, and encodes it again.
func mutate(t *testing.T, data []byte, edit func(root map[string]any, graph []any) []any) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	graph, _ := root["@graph"].([]any)
	graph = edit(root, graph)
	if graph != nil {
		root["@graph"] = graph
	}
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// findNode is the decoded node of a local identity inside a mutable graph.
func findNode(graph []any, local string) map[string]any {
	for _, item := range graph {
		node := item.(map[string]any)
		if node["spdxId"] == iriOf(local) {
			return node
		}
	}
	return nil
}

// assertRefused checks that err names the problem.
func assertRefused(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("the document was accepted; want a refusal naming %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("refusal = %v\nwant it to name %q", err, want)
	}
}

// smallDocument is a single-artifact product with one static library of one
// source file, the smallest document with every kind of statement in it.
func smallDocument() *sbomwriter.Document {
	source := domain.UsedFile{
		ID:    domain.FileID{Anchor: "project", RelPath: "lib/zlib.c"},
		Class: domain.FileClassSource,
		Hashes: map[string]string{
			"SHA-256": "1111111111111111111111111111111111111111111111111111111111111111",
		},
		Properties: map[string][]string{"sbomb:file:linkageForm": {domain.LinkageStaticArchiveMember}},
	}
	return &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "app", Type: "application", Supplier: "Acme"},
		Components: []domain.Component{{
			ID: "component:zlib", Name: "zlib", Version: "1.3.1", Type: "library", Scope: "third-party",
			LinkageForms: []string{domain.LinkageStaticArchiveMember},
			Licenses:     []domain.LicenseFinding{{Expression: "Zlib", Evidence: "file-level"}},
		}},
		Files: []domain.UsedFile{source},
		Relations: []sbomwriter.Relation{
			{From: "component:zlib", To: []string{source.ID.Canonical()}},
			{From: "product", To: []string{"component:zlib"}},
		},
		Run: sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "Acme", ToolVersion: "1.0.0", Timestamp: "2023-11-14T22:13:20Z"},
	}
}

func smallOptions() sbomwriter.Options { return sbomwriter.Options{Reproducible: true} }
