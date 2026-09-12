package generate

import (
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

// Section 16, evidence source 2. These tests state what the build graph is
// read for; what only the corpus can state -- that p14-foss's GPL-2.0 code
// generator comes out build-time-only with its own licence -- is in cmd/sbomb.

// generatorFixture builds the graph a run would hold before section 16 is
// read: one artifact, one object, and the generated source the object was
// compiled from. The build edges and the object mappings are handed over as
// the adapters record them.
func generatorFixture(t *testing.T, edges map[string][]string, objectSources map[string]string) *evidence.Graph {
	t.Helper()
	graph := evidence.New()
	b := newBuilder(graph, assembledAnchors(t), "/bd", "/bd", NewLogger(0, nil))
	graph.AddNode(domain.Node{ID: testArtifact, Kind: domain.NodeArtifact})
	graph.AddNode(domain.Node{ID: "build:app.o", Kind: domain.NodeObject})
	graph.AddNode(domain.Node{
		ID:   "build:generated/table.c",
		Kind: domain.NodeSource,
		File: &domain.FileID{Anchor: "build", RelPath: "generated/table.c"},
	})
	graph.AddEdge(domain.Edge{
		From: testArtifact, To: "build:app.o", Type: "link",
		Strength: "linked", Confidence: domain.ConfidenceHigh, Source: "test", Adapter: "test",
	})
	graph.AddEdge(domain.Edge{
		From: "build:app.o", To: "build:generated/table.c", Type: "source-mapping",
		Strength: "derived", Confidence: domain.ConfidenceHigh, Source: "test", Adapter: "test",
	})

	compile := newCompileEvidence()
	compile.buildEdges = edges
	for object, source := range objectSources {
		compile.addSource(object, source, "ninja-buildgraph")
	}
	addGeneratorEvidence(graph, b, compile, NewLogger(0, nil))
	return graph
}

// The whole chain the fixture project exercises: the generated source was
// written by a generator the build compiled, so the generator's own source is
// in the graph -- and everything below the generator-input edge is
// build-time-only, which is what makes a GPL-2.0 code generator describable
// without being distributed.
func TestTheGeneratorsOwnSourceEntersTheGraphThroughTheBuildEdge(t *testing.T) {
	graph := generatorFixture(t,
		map[string][]string{
			"generated/table.c": {"table_gen"},
			"table_gen":         {"CMakeFiles/table_gen.dir/gen.c.o"},
		},
		map[string]string{"CMakeFiles/table_gen.dir/gen.c.o": "/src/dep/gpl-gen/table_gen.c"},
	)

	if _, known := graph.Node("build:table_gen"); !known {
		t.Fatal("the generator is not in the graph")
	}
	if node, _ := graph.Node("build:table_gen"); node.Kind != domain.NodeGenerator {
		t.Errorf("the generator is a %q node, want %q", node.Kind, domain.NodeGenerator)
	}
	// The object is not on the chain: it is a transient build artifact, and
	// section 13.2 already answered what it was compiled from.
	if _, known := graph.Node("build:CMakeFiles/table_gen.dir/gen.c.o"); known {
		t.Error("the generator's object became a node of the chain")
	}
	if _, known := graph.Node("project:dep/gpl-gen/table_gen.c"); !known {
		t.Fatal("the generator's source is not in the graph")
	}

	attributes := deriveGraphAttributes(graph, []domain.NodeID{testArtifact}, nil, nil, NewLogger(0, nil))
	if role := attributes.roleOf("build:generated/table.c"); role != domain.RoleDistributed {
		t.Errorf("the generated source is %q, want %q: it is in the artifact", role, domain.RoleDistributed)
	}
	for _, canonical := range []string{"build:table_gen", "project:dep/gpl-gen/table_gen.c"} {
		if role := attributes.roleOf(canonical); role != domain.RoleBuildTimeOnly {
			t.Errorf("%s is %q, want %q", canonical, role, domain.RoleBuildTimeOnly)
		}
	}
}

// Every input the generating rule declares is an input, whether or not the
// build produced it: the data a generator read is the case section 16 states
// with `schema.yaml`.
func TestAGeneratingRulesDeclaredInputsAreAllRecorded(t *testing.T) {
	graph := generatorFixture(t,
		map[string][]string{"generated/table.c": {"/src/schema.yaml", "/src/generate.cmake"}},
		nil,
	)
	for _, canonical := range []string{"project:schema.yaml", "project:generate.cmake"} {
		node, known := graph.Node(domain.NodeID(canonical))
		if !known {
			t.Fatalf("%s was not recorded as a generator input", canonical)
		}
		if node.Kind != domain.NodeGeneratorInput {
			t.Errorf("%s is a %q node, want %q", canonical, node.Kind, domain.NodeGeneratorInput)
		}
	}
	var edges int
	for _, edge := range graph.EdgesFrom("build:generated/table.c") {
		if edge.Type == "generator-input" {
			edges++
		}
	}
	if edges != 2 {
		t.Errorf("generator-input edges = %d, want 2", edges)
	}
}

// A build graph that describes a cycle, or a chain of generators generating
// generators, is walked to the bound of section 30 and no further.
func TestAGeneratorChainIsFollowedOnlyToItsBound(t *testing.T) {
	edges := map[string][]string{"generated/table.c": {"step0"}}
	for i := 0; i < maxGeneratorChainDepth+4; i++ {
		edges[stepName(i)] = []string{stepName(i + 1)}
	}
	graph := generatorFixture(t, edges, nil)

	reached := 0
	for _, node := range graph.Nodes() {
		if node.File != nil && node.File.Anchor == "build" && node.Kind == domain.NodeGenerator {
			reached++
		}
	}
	if reached >= maxGeneratorChainDepth+4 {
		t.Errorf("the walk followed %d steps, and the bound is %d", reached, maxGeneratorChainDepth)
	}
	if reached == 0 {
		t.Error("the walk followed nothing at all")
	}
}

func stepName(index int) string {
	return "step" + string(rune('0'+index))
}

// A generated file the document carries and that no evidence traced is
// reported. The absence is the finding: whatever wrote the file is not
// described by the document.
func TestAGeneratedFileWithNoTracedInputIsReported(t *testing.T) {
	graph := generatorFixture(t, map[string][]string{"something/else.c": {"/src/x.c"}}, nil)
	used := usedFiles(graph, []domain.NodeID{testArtifact}, nil)

	findings := missingGeneratorInputFindings(graph, used)
	if len(findings) != 1 {
		t.Fatalf("findings = %#v, want one MISSING_GENERATOR_INPUT_EVIDENCE", findings)
	}
	if findings[0].ID != "MISSING_GENERATOR_INPUT_EVIDENCE" ||
		findings[0].Subject.Ref != "build:generated/table.c" {
		t.Errorf("finding = %+v", findings[0])
	}

	// And it is not reported once the chain exists.
	traced := generatorFixture(t, map[string][]string{"generated/table.c": {"/src/schema.yaml"}}, nil)
	if findings := missingGeneratorInputFindings(traced,
		usedFiles(traced, []domain.NodeID{testArtifact}, nil)); len(findings) != 0 {
		t.Errorf("findings = %#v, want none once the input is known", findings)
	}
}

// A source of the project that no rule produced is not a generated file, so
// the build graph is never asked about it. Asking would put a second kind of
// edge on a chain section 13.2 already answers.
func TestOnlyAFileTheBuildProducedIsAskedAboutAtAll(t *testing.T) {
	for _, node := range []domain.Node{
		{ID: "project:main.c", Kind: domain.NodeSource, File: &domain.FileID{Anchor: "project", RelPath: "main.c"}},
		{ID: "build:app.o", Kind: domain.NodeObject, File: &domain.FileID{Anchor: "build", RelPath: "app.o"}},
		{ID: "build:libx.a", Kind: domain.NodeArchive, File: &domain.FileID{Anchor: "build", RelPath: "libx.a"}},
	} {
		if generatedFileNode(node) {
			t.Errorf("%s is asked about, and it is a %q", node.ID, node.Kind)
		}
	}
}
