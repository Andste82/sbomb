package inventory

import (
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

func TestResolveObjectSourceStrategyOrdering(t *testing.T) {
	g := evidence.New()
	r := New(g)

	objPath := "build/app.o"
	source1 := "src/main.cpp"
	source2 := "src/other.cpp"

	// Add same object with different sources in different strategies
	// CMake (highest priority) should win
	r.AddCMakeMapping(objPath, source1)
	r.AddNinjaMapping(objPath, source2) // Conflicting source

	srcID, strategy, conflict, err := r.ResolveObjectSource(domain.NodeID(objPath))
	if err != nil {
		t.Fatalf("ResolveObjectSource failed: %v", err)
	}

	if strategy != "cmake-file-api" {
		t.Fatalf("expected strategy cmake-file-api, got %s", strategy)
	}

	if string(srcID) != source1 {
		t.Fatalf("expected source %s, got %s", source1, srcID)
	}

	if conflict == nil {
		t.Fatalf("expected conflict to be detected")
	}
}

func TestResolveObjectSourceNoConflict(t *testing.T) {
	g := evidence.New()
	r := New(g)

	objPath := "build/app.o"
	source := "src/main.cpp"

	// Add same source in multiple strategies - should agree
	r.AddCMakeMapping(objPath, source)
	r.AddNinjaMapping(objPath, source)

	srcID, _, conflict, err := r.ResolveObjectSource(domain.NodeID(objPath))
	if err != nil {
		t.Fatalf("ResolveObjectSource failed: %v", err)
	}

	if conflict != nil {
		t.Fatalf("expected no conflict, got %+v", conflict)
	}

	if string(srcID) != source {
		t.Fatalf("expected source %s, got %s", source, srcID)
	}
}

// Three strategies naming three sources are three sides. Reporting only the
// first disagreement would hide the third answer from the reviewer, and the
// whole point of the report is that every answer can be seen.
func TestEveryDisagreeingStrategyIsASideOfTheConflict(t *testing.T) {
	r := New(evidence.New())

	objPath := "build/app.o"
	r.AddCMakeMapping(objPath, "src/main.cpp")
	r.AddNinjaMapping(objPath, "src/other.cpp")
	r.AddDWARFMapping(objPath, "src/third.cpp")

	_, _, conflict, err := r.ResolveObjectSource(domain.NodeID(objPath))
	if err != nil {
		t.Fatalf("ResolveObjectSource failed: %v", err)
	}
	if conflict == nil {
		t.Fatal("expected a conflict")
	}
	if len(conflict.Sides) != 3 {
		t.Fatalf("sides = %+v, want the winner and both dissenters", conflict.Sides)
	}
	// The winner is named, and named first: the sides are in the strategy order
	// of section 13.2, which is also the reason it won.
	if conflict.Winner != "cmake-file-api" || conflict.Sides[0].Source != "cmake-file-api" {
		t.Errorf("winner = %q, sides = %+v, want the File API first", conflict.Winner, conflict.Sides)
	}
	if conflict.Subject.Ref != objPath {
		t.Errorf("subject = %+v, want the object", conflict.Subject)
	}
}

// A strategy that agrees is not a side. Only a different source is.
func TestOnlyADifferentSourceMakesASide(t *testing.T) {
	r := New(evidence.New())

	objPath := "build/app.o"
	r.AddCMakeMapping(objPath, "src/main.cpp")
	r.AddNinjaMapping(objPath, "src/main.cpp")
	r.AddDWARFMapping(objPath, "src/other.cpp")

	_, _, conflict, err := r.ResolveObjectSource(domain.NodeID(objPath))
	if err != nil {
		t.Fatalf("ResolveObjectSource failed: %v", err)
	}
	if conflict == nil {
		t.Fatal("expected a conflict")
	}
	if len(conflict.Sides) != 2 {
		t.Fatalf("sides = %+v, want the winner and the one that differed", conflict.Sides)
	}
	if conflict.Sides[1].Source != "dwarf" {
		t.Errorf("dissenting side = %+v, want dwarf", conflict.Sides[1])
	}
}

// A disagreement says something about the evidence, not about the winner's
// quality. Section 8.7 lists the reasons a confidence may be downgraded and a
// second, weaker answer is not one of them.
func TestAConflictDoesNotWeakenTheEdge(t *testing.T) {
	g := evidence.New()
	r := New(g)
	objID := domain.NodeID("build:app.o")
	g.AddNode(domain.Node{ID: objID, Kind: domain.NodeObject})
	r.AddCMakeMapping(string(objID), "project:src/main.cpp")
	r.AddNinjaMapping(string(objID), "project:src/other.cpp")

	if _, findings, err := r.ResolveAndAddEdges(); err != nil || len(findings) != 1 {
		t.Fatalf("ResolveAndAddEdges() = (%+v, %v), want one finding", findings, err)
	}
	for _, edge := range g.Edges() {
		if edge.Type != "source-mapping" {
			continue
		}
		if edge.Confidence != domain.ConfidenceHigh {
			t.Errorf("confidence = %s, want the winning strategy's own high", edge.Confidence)
		}
		if len(edge.Downgrades) != 0 {
			t.Errorf("downgrades = %v, want none: a conflict is not a downgrade reason", edge.Downgrades)
		}
	}
}

func TestResolveObjectSourceNotFound(t *testing.T) {
	g := evidence.New()
	r := New(g)

	objPath := "build/app.o"
	srcID, strategy, _, err := r.ResolveObjectSource(domain.NodeID(objPath))

	if err == nil {
		t.Fatalf("expected error for unresolved object, got nil")
	}

	if srcID != "" {
		t.Fatalf("expected empty source ID, got %s", srcID)
	}

	if strategy != "no-strategy" {
		t.Fatalf("expected 'no-strategy', got %s", strategy)
	}
}

func TestStrategyPriority(t *testing.T) {
	// Test that strategies are applied in the correct priority order
	g := evidence.New()
	r := New(g)

	objPath := "build/app.o"
	cmakeSource := "src/cmake.cpp"
	ninjaSource := "src/ninja.cpp"
	depfileSource := "src/depfile.cpp"

	// Add in reverse priority order
	r.AddDepfileMapping(objPath, depfileSource)
	r.AddNinjaMapping(objPath, ninjaSource)
	r.AddCMakeMapping(objPath, cmakeSource)

	srcID, strategy, _, err := r.ResolveObjectSource(domain.NodeID(objPath))
	if err != nil {
		t.Fatalf("ResolveObjectSource failed: %v", err)
	}

	if strategy != "cmake-file-api" {
		t.Fatalf("expected cmake-file-api strategy, got %s", strategy)
	}

	if string(srcID) != cmakeSource {
		t.Fatalf("expected cmake source to win, got %s", srcID)
	}
}

func TestMSBuildMappingUsesItsOwnStrategy(t *testing.T) {
	g := evidence.New()
	r := New(g)

	r.AddMSBuildMapping("build:main.obj", "project:main.c")

	source, strategy, _, err := r.ResolveObjectSource("build:main.obj")
	if err != nil {
		t.Fatalf("ResolveObjectSource failed: %v", err)
	}
	if source != "project:main.c" {
		t.Fatalf("source = %s, want project:main.c", source)
	}
	if strategy != "msbuild-tlog" {
		t.Fatalf("strategy = %s, want msbuild-tlog", strategy)
	}
	if confidence := r.confidenceForStrategy(strategy); confidence != domain.ConfidenceHigh {
		t.Fatalf("confidence = %s, want high", confidence)
	}
}

func TestDuplicateBasenames(t *testing.T) {
	// Test that duplicate basenames are properly distinguished by path
	g := evidence.New()
	r := New(g)

	obj1 := "build/targetA/CMakeFiles/foo.dir/main.cpp.o"
	obj2 := "build/targetB/CMakeFiles/foo.dir/main.cpp.o"
	src1 := "targetA/main.cpp"
	src2 := "targetB/main.cpp"

	r.AddCMakeMapping(obj1, src1)
	r.AddCMakeMapping(obj2, src2)

	srcID1, _, _, err := r.ResolveObjectSource(domain.NodeID(obj1))
	if err != nil {
		t.Fatalf("ResolveObjectSource failed for obj1: %v", err)
	}

	srcID2, _, _, err := r.ResolveObjectSource(domain.NodeID(obj2))
	if err != nil {
		t.Fatalf("ResolveObjectSource failed for obj2: %v", err)
	}

	if string(srcID1) != src1 {
		t.Fatalf("expected %s for obj1, got %s", src1, srcID1)
	}

	if string(srcID2) != src2 {
		t.Fatalf("expected %s for obj2, got %s", src2, srcID2)
	}

	if srcID1 == srcID2 {
		t.Fatalf("objects with duplicate basenames should resolve to different sources")
	}
}
func TestResolveAndAddEdges(t *testing.T) {
	g := evidence.New()
	r := New(g)

	// Create an object node
	g.AddNode(domain.Node{
		ID:   "build:app.o",
		Kind: domain.NodeObject,
	})

	// Add a mapping
	r.AddCMakeMapping("build:app.o", "project:src/main.cpp")

	// Resolve and add edges
	resolved, findings, err := r.ResolveAndAddEdges()
	if err != nil {
		t.Fatalf("ResolveAndAddEdges failed: %v", err)
	}

	if resolved != 1 {
		t.Fatalf("expected 1 resolved object, got %d", resolved)
	}
	if len(findings) != 0 {
		t.Fatalf("one strategy alone is no disagreement, got %+v", findings)
	}

	// Verify edge was added to graph
	edges := g.Edges()
	found := false
	for _, edge := range edges {
		if edge.From == "build:app.o" && edge.To == "project:src/main.cpp" {
			found = true
			if edge.Type != "source-mapping" {
				t.Fatalf("expected source-mapping edge type, got %s", edge.Type)
			}
			if edge.Confidence != domain.ConfidenceHigh {
				t.Fatalf("expected high confidence for CMake mapping, got %s", edge.Confidence)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected source-mapping edge not found in graph")
	}
}

func TestConfidenceForStrategy(t *testing.T) {
	g := evidence.New()
	r := New(g)

	tests := []struct {
		strategy string
		expected domain.Confidence
	}{
		{"cmake-file-api", domain.ConfidenceHigh},
		{"ninja-buildgraph", domain.ConfidenceHigh},
		{"compile-commands-json", domain.ConfidenceMedium},
		{"depfile-adjacency", domain.ConfidenceMedium},
		{"dwarf", domain.ConfidenceHigh},
		{"build-log-fallback", domain.ConfidenceLow},
		{"unknown-strategy", domain.ConfidenceUnknown},
	}

	for _, tt := range tests {
		conf := r.confidenceForStrategy(tt.strategy)
		if conf != tt.expected {
			t.Errorf("confidenceForStrategy(%q): expected %s, got %s", tt.strategy, tt.expected, conf)
		}
	}
}
