package generate

import (
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

// Section 24.5. These tests state the derivation itself; what only the corpus
// can state -- that a real build's generator input comes out build-time-only
// and that a real archive's ratio is right -- is in cmd/sbomb.

const testArtifact = domain.NodeID("artifact:build:app")

// attributeFixture builds a graph from a list of edges and derives the
// attributes over it. Node kinds are taken from kindForPath, which is what the
// link graph itself uses, so a ".a" is an archive and an ".o" is an object
// without the test having to say so.
func attributeFixture(t *testing.T, edges []domain.Edge) *graphAttributes {
	t.Helper()
	graph := evidence.New()
	for _, edge := range edges {
		for _, id := range []domain.NodeID{edge.From, edge.To} {
			if id == testArtifact {
				graph.AddNode(domain.Node{ID: id, Kind: domain.NodeArtifact})
				continue
			}
			graph.AddNode(domain.Node{ID: id, Kind: kindForPath(string(id))})
		}
		edge.Strength, edge.Confidence = "linked", domain.ConfidenceHigh
		edge.Source, edge.Adapter = "test", "test"
		graph.AddEdge(edge)
	}
	return deriveGraphAttributes(graph, []domain.NodeID{testArtifact}, nil, nil, NewLogger(0, nil))
}

// The regression test for the *direction* of 6a. Section 8.3 defines fourteen
// evidence types and six are emitted today; if the derivation were an
// allowlist of distributing types, the seventh would make a component vanish
// from an attribution document in silence. So an evidence type this code has
// never seen must leave the node distributed.
func TestAnUnknownEvidenceTypeLeavesTheNodeDistributed(t *testing.T) {
	attributes := attributeFixture(t, []domain.Edge{
		{From: testArtifact, To: "build:app.o", Type: "link"},
		{From: "build:app.o", To: "project:mystery.c", Type: "a-type-nobody-has-implemented-yet"},
	})

	if role := attributes.roleOf("project:mystery.c"); role != domain.RoleDistributed {
		t.Errorf("role = %q, want %q: omission has to fail towards inclusion", role, domain.RoleDistributed)
	}
	// And it establishes no linkage form of its own: an unrecognized chain is
	// silence, not a form.
	if form := attributes.linkageOf("project:mystery.c"); form != domain.LinkageStaticObject {
		t.Errorf("form = %q, want the form the chain was already carrying", form)
	}
}

// The three edge types that do make a node build-time-only, each on its own.
func TestOnlyTheThreeBuildTimeEdgeTypesRemoveANodeFromTheArtifact(t *testing.T) {
	for _, evidenceType := range []domain.EvidenceType{"generator-input", "generator-output", "toolchain"} {
		t.Run(string(evidenceType), func(t *testing.T) {
			attributes := attributeFixture(t, []domain.Edge{
				{From: testArtifact, To: "build:app.o", Type: "link"},
				{From: "build:app.o", To: "project:schema.yaml", Type: evidenceType},
			})
			if role := attributes.roleOf("project:schema.yaml"); role != domain.RoleBuildTimeOnly {
				t.Errorf("role = %q, want %q", role, domain.RoleBuildTimeOnly)
			}
			if form := attributes.linkageOf("project:schema.yaml"); form != domain.LinkageBuildTool {
				t.Errorf("form = %q, want %q", form, domain.LinkageBuildTool)
			}
			// Everything below a build-time edge is build-time too, by that
			// chain: nothing a generator read is in the artifact because the
			// generator read it.
			if role := attributes.roleOf("project:schema.yaml"); role == domain.RoleDistributed {
				t.Error("the chain leaked past the build-time edge")
			}
		})
	}
}

// The case the milestone names: one file, two chains. It is a generator input
// and it is also compiled, so it is in the artifact and the answer is
// distributed. "Exclusively" is the word the definition turns on.
func TestAFileThatIsAlsoCompiledIsDistributed(t *testing.T) {
	attributes := attributeFixture(t, []domain.Edge{
		{From: testArtifact, To: "build:gen.o", Type: "link"},
		{From: "build:gen.o", To: "project:table.c", Type: "generator-input"},
		{From: testArtifact, To: "build:app.o", Type: "link"},
		{From: "build:app.o", To: "project:table.c", Type: "source-mapping"},
	})

	if role := attributes.roleOf("project:table.c"); role != domain.RoleDistributed {
		t.Errorf("role = %q, want %q", role, domain.RoleDistributed)
	}
	// And the published form is the direct one, not the build-time one.
	if form := attributes.linkageOf("project:table.c"); form != domain.LinkageStaticObject {
		t.Errorf("form = %q, want %q", form, domain.LinkageStaticObject)
	}
}

// The five per-file forms a chain can establish, one graph each.
func TestLinkageFormFollowsTheChain(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		edges []domain.Edge
		file  string
		want  string
	}{
		{
			name: "an object the linker took out of an archive",
			edges: []domain.Edge{
				{From: testArtifact, To: "build:libfoo.a", Type: "link"},
				{From: "build:libfoo.a", To: "build:foo.o", Type: "archive-member"},
				{From: "build:foo.o", To: "project:foo.c", Type: "source-mapping"},
			},
			file: "project:foo.c", want: domain.LinkageStaticArchiveMember,
		},
		{
			name: "an object linked directly",
			edges: []domain.Edge{
				{From: testArtifact, To: "build:foo.o", Type: "link"},
				{From: "build:foo.o", To: "project:foo.c", Type: "source-mapping"},
			},
			file: "project:foo.c", want: domain.LinkageStaticObject,
		},
		{
			name: "a shared library the artifact references",
			edges: []domain.Edge{
				{From: testArtifact, To: "sysroot:usr/lib/libssl.so.3", Type: "link"},
			},
			file: "sysroot:usr/lib/libssl.so.3", want: domain.LinkageDynamic,
		},
		{
			name: "a header the compiler read",
			edges: []domain.Edge{
				{From: testArtifact, To: "build:foo.o", Type: "link"},
				{From: "build:foo.o", To: "project:foo.h", Type: "header-dependency"},
			},
			file: "project:foo.h", want: domain.LinkageHeaderOnly,
		},
		{
			name: "a file a manifest says was packaged into the image",
			edges: []domain.Edge{
				{From: testArtifact, To: "project:assets/logo.png", Type: "packaging"},
			},
			file: "project:assets/logo.png", want: domain.LinkageEmbeddedAsset,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			attributes := attributeFixture(t, testCase.edges)
			if form := attributes.linkageOf(testCase.file); form != testCase.want {
				t.Errorf("form = %q, want %q", form, testCase.want)
			}
			if role := attributes.roleOf(testCase.file); role != domain.RoleDistributed {
				t.Errorf("role = %q, want %q", role, domain.RoleDistributed)
			}
		})
	}
}

// header-only is a statement about the component, not about one file. A
// library whose header was read and whose archive member was extracted is not
// header-only, and a library that contributed nothing but a header is.
func TestHeaderOnlyIsAnAggregateConclusion(t *testing.T) {
	attributes := attributeFixture(t, []domain.Edge{
		{From: testArtifact, To: "build:libmit.a", Type: "link"},
		{From: "build:libmit.a", To: "build:mit_a.o", Type: "archive-member"},
		{From: "build:mit_a.o", To: "project:dep/mit/src/mit_a.c", Type: "source-mapping"},
		{From: "build:mit_a.o", To: "project:dep/mit/include/mit.h", Type: "header-dependency"},
		{From: "build:mit_a.o", To: "project:dep/hdr/include/hdr.h", Type: "header-dependency"},
	})

	mixed := domain.Component{ID: "component:mit", Name: "mit"}
	applyComponentAttributes(&mixed, []domain.UsedFile{
		{ID: fileID("project", "dep/mit/src/mit_a.c")},
		{ID: fileID("project", "dep/mit/include/mit.h")},
	}, attributes)
	if mixed.HeaderOnly {
		t.Error("a component with an extracted archive member is header-only")
	}
	if got := mixed.LinkageForms; len(got) != 1 || got[0] != domain.LinkageStaticArchiveMember {
		t.Errorf("forms = %v, want only %q", got, domain.LinkageStaticArchiveMember)
	}

	headers := domain.Component{ID: "component:hdr", Name: "hdr"}
	applyComponentAttributes(&headers, []domain.UsedFile{
		{ID: fileID("project", "dep/hdr/include/hdr.h")},
	}, attributes)
	if !headers.HeaderOnly {
		t.Error("a component that contributed nothing but a header is not header-only")
	}
	if got := headers.LinkageForms; len(got) != 1 || got[0] != domain.LinkageHeaderOnly {
		t.Errorf("forms = %v, want only %q", got, domain.LinkageHeaderOnly)
	}
	if headers.ArchiveMembersUsed != "" {
		t.Errorf("archiveMembersUsed = %q for a component with no archive", headers.ArchiveMembersUsed)
	}
}

// generated-source is the other exclusively worded form: it is added where
// every object-contributing file of the component is one the build produced,
// and beside the form the linker saw rather than instead of it.
func TestGeneratedSourceIsAddedOnlyWhenNothingHandWrittenContributed(t *testing.T) {
	attributes := attributeFixture(t, []domain.Edge{
		{From: testArtifact, To: "build:gen.o", Type: "link"},
		{From: "build:gen.o", To: "build:generated/table.c", Type: "source-mapping"},
		{From: testArtifact, To: "build:main.o", Type: "link"},
		{From: "build:main.o", To: "project:src/main.c", Type: "source-mapping"},
	})

	generated := domain.Component{ID: "component:tables", Name: "tables"}
	applyComponentAttributes(&generated, []domain.UsedFile{
		{ID: fileID("build", "generated/table.c")},
	}, attributes)
	if got := generated.LinkageForms; len(got) != 2 ||
		got[0] != domain.LinkageGeneratedSource || got[1] != domain.LinkageStaticObject {
		t.Errorf("forms = %v, want both generated-source and static-object", got)
	}

	mixed := domain.Component{ID: "component:project", Name: "project"}
	applyComponentAttributes(&mixed, []domain.UsedFile{
		{ID: fileID("build", "generated/table.c")},
		{ID: fileID("project", "src/main.c")},
	}, attributes)
	for _, form := range mixed.LinkageForms {
		if form == domain.LinkageGeneratedSource {
			t.Error("a component holding a hand-written source contributed only through generated sources")
		}
	}
}

// A build-time-only component always carries build-tool, so the role and the
// linkage form cannot disagree.
func TestABuildTimeOnlyComponentCarriesBuildTool(t *testing.T) {
	attributes := attributeFixture(t, []domain.Edge{
		{From: testArtifact, To: "build:app.o", Type: "link"},
		{From: "build:app.o", To: "project:dep/gen/gen.c", Type: "generator-input"},
	})
	component := domain.Component{ID: "component:gen", Name: "gen"}
	applyComponentAttributes(&component, []domain.UsedFile{
		{ID: fileID("project", "dep/gen/gen.c")},
	}, attributes)

	if component.DistributionRole != domain.RoleBuildTimeOnly {
		t.Errorf("role = %q, want %q", component.DistributionRole, domain.RoleBuildTimeOnly)
	}
	if got := component.LinkageForms; len(got) != 1 || got[0] != domain.LinkageBuildTool {
		t.Errorf("forms = %v, want only %q", got, domain.LinkageBuildTool)
	}
	// One distributed file is enough to make the whole component distributed:
	// a component with a file inside the product is inside the product.
	shared := domain.Component{ID: "component:gen", Name: "gen"}
	applyComponentAttributes(&shared, []domain.UsedFile{
		{ID: fileID("project", "dep/gen/gen.c")},
		{ID: fileID("build", "app.o")},
	}, attributes)
	if shared.DistributionRole != domain.RoleDistributed {
		t.Errorf("role = %q, want %q", shared.DistributionRole, domain.RoleDistributed)
	}
}

// Shuffling the order the adapters contributed their evidence in changes none
// of the three attributes. Edge insertion order is the one thing a caller can
// vary without changing what the build did, and section 29 requires the answer
// not to move with it.
func TestAttributesDoNotDependOnEdgeOrder(t *testing.T) {
	edges := []domain.Edge{
		{From: testArtifact, To: "build:libmit.a", Type: "link"},
		{From: "build:libmit.a", To: "build:mit_a.o", Type: "archive-member"},
		{From: "build:mit_a.o", To: "project:dep/mit/src/mit_a.c", Type: "source-mapping"},
		{From: "build:mit_a.o", To: "project:dep/mit/include/mit.h", Type: "header-dependency"},
		{From: testArtifact, To: "build:main.o", Type: "link"},
		{From: "build:main.o", To: "project:src/main.c", Type: "source-mapping"},
		{From: "build:main.o", To: "project:dep/gen/gen.c", Type: "generator-input"},
		{From: testArtifact, To: "sysroot:usr/lib/libssl.so.3", Type: "link"},
	}
	files := []domain.UsedFile{
		{ID: fileID("project", "dep/mit/src/mit_a.c")},
		{ID: fileID("project", "dep/mit/include/mit.h")},
		{ID: fileID("project", "src/main.c")},
		{ID: fileID("project", "dep/gen/gen.c")},
		{ID: fileID("sysroot", "usr/lib/libssl.so.3")},
	}

	describe := func(order []domain.Edge) string {
		attributes := attributeFixture(t, order)
		out := ""
		for _, file := range files {
			canonical := file.ID.Canonical()
			out += canonical + " " + attributes.roleOf(canonical) + " " + attributes.linkageOf(canonical) + "\n"
			component := domain.Component{ID: "component:c", Name: "c"}
			applyComponentAttributes(&component, []domain.UsedFile{file}, attributes)
			out += "  " + component.DistributionRole + " " +
				component.ArchiveMembersUsed + " " + joinForms(component.LinkageForms) + "\n"
		}
		return out
	}

	want := describe(edges)
	// Every rotation of the list, which is the cheapest way to shuffle
	// deterministically: the test must not itself depend on a random seed.
	for offset := 1; offset < len(edges); offset++ {
		rotated := append(append([]domain.Edge{}, edges[offset:]...), edges[:offset]...)
		if got := describe(rotated); got != want {
			t.Fatalf("rotation by %d changed the attributes:\n%s\nwant:\n%s", offset, got, want)
		}
	}
}

func joinForms(forms []string) string {
	out := ""
	for _, form := range forms {
		out += form + ","
	}
	return out
}
