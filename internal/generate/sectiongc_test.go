package generate

import (
	"testing"

	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/policy"
)

// gcFixture builds a two-object graph in which only one object has every
// contributed section discarded.
func gcFixture(t *testing.T) (*evidence.Graph, *builder) {
	t.Helper()
	graph := evidence.New()
	result, err := anchors.Assemble(anchors.Options{
		Flavor: pathmodel.DefaultFlavor(), ProjectRoot: "/src", BuildRoot: "/bd",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := newBuilder(graph, result, "/bd", "/bd", NewLogger(0, nil))
	for _, object := range []string{"build:kept.o", "build:gone.o"} {
		graph.AddNode(domain.Node{ID: domain.NodeID(object), Kind: domain.NodeObject})
		graph.AddEdge(domain.Edge{
			From: "artifact:build:app", To: domain.NodeID(object),
			Type: "link", Strength: "linked", Confidence: domain.ConfidenceHigh,
			Source: "gnu-ld:app.map", Adapter: "linker-map",
		})
	}
	b.retainedObjects["build:kept.o"] = true
	b.discardedObjects["build:kept.o"] = 1
	b.discardedObjects["build:gone.o"] = 3
	return graph, b
}

func TestSectionGCIgnoredByDefault(t *testing.T) {
	graph, b := gcFixture(t)
	if excluded, _ := applySectionGC(graph, b, policy.Config{SectionGarbageCollection: "ignore"}, NewLogger(0, nil)); len(excluded) != 0 {
		t.Errorf("the default removed %d object(s); section 4.5 says it tracks nothing", len(excluded))
	}
}

func TestSectionGCAnnotateKeepsTheObjectAndLowersConfidence(t *testing.T) {
	graph, b := gcFixture(t)
	excluded, _ := applySectionGC(graph, b, policy.Config{SectionGarbageCollection: "annotate"}, NewLogger(0, nil))
	if len(excluded) != 0 {
		t.Fatal("annotate removed a file; it may only annotate")
	}
	for _, edge := range graph.Edges() {
		switch edge.To {
		case "build:gone.o":
			if edge.Attributes["sbomb:evidence:link:fullyDiscarded"] != "true" {
				t.Error("the fully discarded object was not annotated")
			}
			if edge.Confidence != domain.ConfidenceMedium {
				t.Errorf("confidence = %q, want one level below high", edge.Confidence)
			}
			if len(edge.Downgrades) != 1 || edge.Downgrades[0] != "section-gc" {
				t.Errorf("downgrades = %v, want the reason recorded (section 8.7)", edge.Downgrades)
			}
		case "build:kept.o":
			if edge.Confidence != domain.ConfidenceHigh {
				t.Error("an object with retained sections was downgraded")
			}
		}
	}
}

func TestSectionGCExcludeRemovesOnlyFullyDiscardedObjects(t *testing.T) {
	graph, b := gcFixture(t)
	excluded, _ := applySectionGC(graph, b, policy.Config{SectionGarbageCollection: "exclude"}, NewLogger(0, nil))
	if !excluded["build:gone.o"] {
		t.Error("the fully discarded object was kept")
	}
	if excluded["build:kept.o"] {
		t.Error("an object with retained sections was removed")
	}
}

func TestPartialInformationNeverExcludes(t *testing.T) {
	// Section 4.5: an object counts as fully discarded only when the evidence
	// enumerates both what was kept and what was dropped.
	graph, b := gcFixture(t)
	b.retainedObjects = map[string]bool{}
	if excluded, _ := applySectionGC(graph, b, policy.Config{SectionGarbageCollection: "exclude"}, NewLogger(0, nil)); len(excluded) != 0 {
		t.Errorf("removed %d object(s) from evidence that lists no retained section", len(excluded))
	}
}
