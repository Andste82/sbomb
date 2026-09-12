package generate

import (
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

// TestEnvironmentProvidedNeedsMoreThanSystemScope pins the distinction the
// CycloneDX field actually makes. "Provided by the environment" is a claim
// about the shipped artifact, not about where a file happened to live at build
// time: a system archive linked statically ends up inside the artifact, and
// calling that externally provided would be false.
//
// The second condition is now the `dynamic` linkage form of section 24.5, so
// the case is built out of a graph rather than out of a file class. That is
// not a weakening of the test: it used to assert on
// domain.FileClassSharedLibrary, which fileClassOf never produces, so the
// condition it was pinning could not be reached from a real run at all.
func TestEnvironmentProvidedNeedsMoreThanSystemScope(t *testing.T) {
	shared := domain.UsedFile{ID: fileID("system", "libc.so.6")}
	archive := domain.UsedFile{ID: fileID("system", "libm.a"), Class: domain.FileClassArchive}

	graph := evidence.New()
	for _, input := range []struct {
		id   string
		kind domain.NodeKind
	}{
		{"system:libc.so.6", domain.NodeArchive},
		{"system:libm.a", domain.NodeArchive},
	} {
		graph.AddNode(domain.Node{ID: domain.NodeID(input.id), Kind: input.kind})
		graph.AddEdge(domain.Edge{
			From: "artifact:build:app", To: domain.NodeID(input.id),
			Type: "link", Strength: "linked", Confidence: domain.ConfidenceHigh,
			Source: "gnu-ld:app.map", Adapter: "linker-map",
		})
	}
	attributes := deriveGraphAttributes(graph, []domain.NodeID{"artifact:build:app"}, nil, nil, NewLogger(0, nil))

	for _, testCase := range []struct {
		name  string
		scope string
		files []domain.UsedFile
		want  bool
	}{
		{"a dynamically linked system library", "system", []domain.UsedFile{shared}, true},
		{"a statically linked system archive", "system", []domain.UsedFile{archive}, false},
		{"both, because one shared library is enough", "system", []domain.UsedFile{archive, shared}, true},
		{"a project component", "project", []domain.UsedFile{shared}, false},
		{"a third-party component", "third-party", []domain.UsedFile{shared}, false},
		{"a system component with no files at all", "system", nil, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			component := domain.Component{ID: "c", Name: "c", Scope: testCase.scope}
			applyComponentAttributes(&component, testCase.files, attributes)
			if got := environmentProvided(component); got != testCase.want {
				t.Errorf("environmentProvided = %v, want %v", got, testCase.want)
			}
		})
	}
}
