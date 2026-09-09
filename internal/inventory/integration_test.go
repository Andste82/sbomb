package inventory

import (
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

// TestResolverWithGraphIntegration tests end-to-end resolution with evidence graph
func TestResolverWithGraphIntegration(t *testing.T) {
	// Build a realistic scenario:
	// artifact -> objects -> sources
	g := evidence.New()
	r := New(g)

	// Create artifact node
	artifactID := domain.NodeID("artifact:build/app")
	g.AddNode(domain.Node{ID: artifactID, Kind: domain.NodeArtifact})

	// Create object nodes
	obj1ID := domain.NodeID("build:app.o")
	obj2ID := domain.NodeID("build:util.o")
	g.AddNode(domain.Node{ID: obj1ID, Kind: domain.NodeObject})
	g.AddNode(domain.Node{ID: obj2ID, Kind: domain.NodeObject})

	// Create source nodes
	src1ID := domain.NodeID("project:src/main.cpp")
	src2ID := domain.NodeID("project:src/util.cpp")
	g.AddNode(domain.Node{ID: src1ID, Kind: domain.NodeSource})
	g.AddNode(domain.Node{ID: src2ID, Kind: domain.NodeSource})

	// Add link evidence edges (artifact -> objects)
	g.AddEdge(domain.Edge{
		From:       artifactID,
		To:         obj1ID,
		Type:       "link",
		Strength:   "linked",
		Confidence: domain.ConfidenceHigh,
		Source:     "ld:map",
		Adapter:    "linker",
	})
	g.AddEdge(domain.Edge{
		From:       artifactID,
		To:         obj2ID,
		Type:       "link",
		Strength:   "linked",
		Confidence: domain.ConfidenceHigh,
		Source:     "ld:map",
		Adapter:    "linker",
	})

	// Add object->source mappings to resolver
	r.AddCMakeMapping("build:app.o", "project:src/main.cpp")
	r.AddCMakeMapping("build:util.o", "project:src/util.cpp")

	// Resolve and add edges
	resolved, _, err := r.ResolveAndAddEdges()
	if err != nil {
		t.Fatalf("ResolveAndAddEdges failed: %v", err)
	}

	if resolved != 2 {
		t.Fatalf("expected 2 resolved objects, got %d", resolved)
	}

	// Verify the graph now has source-mapping edges
	edges := g.Edges()
	sourceMappingCount := 0
	for _, edge := range edges {
		if edge.Type == "source-mapping" {
			sourceMappingCount++
			if edge.Strength != "derived" {
				t.Fatalf("expected derived strength for source-mapping, got %s", edge.Strength)
			}
		}
	}

	if sourceMappingCount != 2 {
		t.Fatalf("expected 2 source-mapping edges, got %d", sourceMappingCount)
	}
}

// TestResolverWithConflict tests conflict detection during graph resolution
func TestResolverWithConflict(t *testing.T) {
	g := evidence.New()
	r := New(g)

	// Create object node
	objID := domain.NodeID("build:app.o")
	g.AddNode(domain.Node{ID: objID, Kind: domain.NodeObject})

	// Add conflicting mappings from different strategies
	// CMake says main.cpp, Ninja says other.cpp
	r.AddCMakeMapping("build:app.o", "project:src/main.cpp")
	r.AddNinjaMapping("build:app.o", "project:src/other.cpp")

	// Resolve - CMake should win (higher priority)
	resolved, findings, _ := r.ResolveAndAddEdges()
	if resolved != 1 {
		t.Fatalf("expected 1 resolved, got %d", resolved)
	}

	// The disagreement is reported, not only recorded on the edge: a reviewer
	// reads findings, not the evidence dump.
	if len(findings) != 1 || findings[0].ID != "OBJECT_SOURCE_MAPPING_CONFLICT" {
		t.Fatalf("findings = %+v, want one OBJECT_SOURCE_MAPPING_CONFLICT", findings)
	}
	for _, want := range []string{"cmake-file-api", "ninja-buildgraph", "project:src/main.cpp", "project:src/other.cpp"} {
		if !strings.Contains(findings[0].Message, want) {
			t.Errorf("message %q does not name %q", findings[0].Message, want)
		}
	}

	// Find the edge and check for conflict attribute
	edges := g.Edges()
	for _, edge := range edges {
		if edge.Type == "source-mapping" && edge.From == objID {
			// CMake should win, so target should be main.cpp
			if edge.To != "project:src/main.cpp" {
				t.Fatalf("expected CMake resolution to win (main.cpp), got %s", edge.To)
			}
			// Should have conflict recorded
			if conflict, ok := edge.Attributes["conflictingStrategy"]; !ok {
				t.Fatalf("expected conflict attribute, none found")
			} else if conflict == "" {
				t.Fatalf("expected non-empty conflict attribute")
			}
			return
		}
	}
	t.Fatalf("expected source-mapping edge not found")
}

// TestResolverStrategyFallback tests fallback when first strategy fails
func TestResolverStrategyFallback(t *testing.T) {
	g := evidence.New()
	r := New(g)

	// Create object node
	objID := domain.NodeID("build:app.o")
	g.AddNode(domain.Node{ID: objID, Kind: domain.NodeObject})

	// Only add Ninja mapping (CMake not available)
	r.AddNinjaMapping("build:app.o", "project:src/main.cpp")

	resolved, _, _ := r.ResolveAndAddEdges()
	if resolved != 1 {
		t.Fatalf("expected 1 resolved with fallback strategy")
	}

	// Find edge and verify it was resolved by Ninja strategy
	edges := g.Edges()
	for _, edge := range edges {
		if edge.Type == "source-mapping" && edge.From == objID {
			if edge.Confidence != domain.ConfidenceHigh {
				t.Fatalf("Ninja strategy should have high confidence, got %s", edge.Confidence)
			}
			return
		}
	}
	t.Fatalf("expected source-mapping edge")
}

// TestResolverUnresolvedObject tests handling of objects with no mapping
func TestResolverUnresolvedObject(t *testing.T) {
	g := evidence.New()
	r := New(g)

	// Create two objects: one with mapping, one without
	obj1ID := domain.NodeID("build:app.o")
	obj2ID := domain.NodeID("build:unknown.o")
	g.AddNode(domain.Node{ID: obj1ID, Kind: domain.NodeObject})
	g.AddNode(domain.Node{ID: obj2ID, Kind: domain.NodeObject})

	// Only map the first object
	r.AddCMakeMapping("build:app.o", "project:src/main.cpp")

	resolved, findings, _ := r.ResolveAndAddEdges()
	// Should only resolve 1 out of 2
	if resolved != 1 {
		t.Fatalf("expected 1 resolved, got %d", resolved)
	}
	// An object no strategy could map is missing evidence, not contradicted
	// evidence. Reporting a disagreement there would invent both sides.
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: no strategy said anything about build:unknown.o", findings)
	}

	// Verify that only one source-mapping edge exists
	edges := g.Edges()
	mappingCount := 0
	for _, edge := range edges {
		if edge.Type == "source-mapping" {
			mappingCount++
		}
	}
	if mappingCount != 1 {
		t.Fatalf("expected 1 source-mapping edge, got %d", mappingCount)
	}
}
