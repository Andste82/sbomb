package mapping

import (
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbommap"
	"github.com/example/sbomb/internal/sbomwriter"
)

// edgeKindsOfForm is how each linkage form of section 24.5 reaches a
// deliverable. The list is closed, as the forms are: a form missing here is
// one sbomb does not emit, and it adds no edge rather than a guessed one.
//
// generated-source is deliberately absent. Section 24.5 adds it beside the
// linker form ("both are true: the object was linked, and the source it came
// from was generated"), so the linker form already gives the edge; the
// generated-ness stays a property of the component and of its files. Reading
// it as "the component generates the deliverable" would state something
// false about every library that ships a configure-time header.
var edgeKindsOfForm = map[string]EdgeKind{
	domain.LinkageStaticArchiveMember: EdgeStaticLink,
	domain.LinkageStaticObject:        EdgeStaticLink,
	domain.LinkageDynamic:             EdgeDynamicLink,
	domain.LinkageBuildTool:           EdgeTool,
	domain.LinkageHeaderOnly:          EdgeDependsOn,
	domain.LinkageEmbeddedAsset:       EdgeEmbeds,
}

// edgeBuilder derives the structural edges of section 28.11.4 from the
// document's relations.
type edgeBuilder struct {
	document *sbomwriter.Document
	table    *sbommap.Table
	// components and files by document identity.
	components map[string]domain.Component
	files      map[string]domain.UsedFile
	artifacts  map[string]bool
	productID  string
	assembly   bool
	// filesOf is the files each component's relations say it contains.
	filesOf map[string][]string
	edges   map[Edge]bool
	// targets counts, per local identity, the relation targets that resolved:
	// zero is a known leaf.
	targets map[string]int
}

func newEdgeBuilder(document *sbomwriter.Document, table *sbommap.Table) *edgeBuilder {
	b := &edgeBuilder{
		document:   document,
		table:      table,
		components: map[string]domain.Component{},
		files:      map[string]domain.UsedFile{},
		artifacts:  map[string]bool{},
		productID:  document.Product.ID,
		assembly:   len(document.Artifacts) > 0,
		filesOf:    map[string][]string{},
		edges:      map[Edge]bool{},
		targets:    map[string]int{},
	}
	for _, artifact := range document.Artifacts {
		b.artifacts[artifact.ID] = true
	}
	for _, component := range document.Components {
		b.components[component.ID] = component
	}
	for _, file := range document.Files {
		b.files[file.ID.Canonical()] = file
	}
	for _, relation := range document.Relations {
		if _, isComponent := b.components[relation.From]; !isComponent {
			continue
		}
		for _, target := range relation.To {
			if _, isFile := b.files[target]; isFile {
				b.filesOf[relation.From] = append(b.filesOf[relation.From], target)
			}
		}
	}
	return b
}

// build walks every relation once. Each target is typed by what its two ends
// are, which is all a Relation says:
//
//   - a component to its files, the product to its artifacts, and a grouping
//     component to its members (section 24.2) contain them;
//   - a deliverable -- the product, or an artifact in assembly mode -- to a
//     component is typed by the linkage forms that component reached that
//     deliverable through;
//   - anything else depends on its target, which claims reachability and
//     nothing more.
//
// The members of a grouping are also linked from the deliverables they reach,
// by the same rule, when they reached them through a linkage form: a system
// library the product links dynamically is a dynamic link of the product,
// whatever component groups it. A member with no form stays only under its
// grouping, which keeps toolchain files off the product's own dependency path
// (section 24.2) unless the evidence put them there.
func (b *edgeBuilder) build() []Edge {
	var members []string
	for _, relation := range b.document.Relations {
		from, known := b.table.Resolve(relation.From)
		if !known {
			continue
		}
		for _, target := range relation.To {
			to, resolved := b.table.Resolve(target)
			if !resolved {
				continue
			}
			b.targets[from]++
			_, fromComponent := b.components[relation.From]
			_, toComponent := b.components[target]
			_, toFile := b.files[target]
			switch {
			case fromComponent && toFile:
				b.add(from, EdgeContains, to)
			case fromComponent && toComponent:
				b.add(from, EdgeContains, to)
				members = append(members, target)
			case relation.From == b.productID && b.artifacts[target]:
				b.add(from, EdgeContains, to)
			case b.isDeliverable(relation.From) && toComponent:
				kinds := b.kindsFor(b.components[target], relation.From)
				if len(kinds) == 0 {
					kinds = []EdgeKind{EdgeDependsOn}
				}
				for _, kind := range kinds {
					b.add(from, kind, to)
				}
			default:
				b.add(from, EdgeDependsOn, to)
			}
		}
	}
	sort.Strings(members)
	for _, id := range members {
		member := b.components[id]
		to := b.table.Component(id)
		for _, deliverable := range b.deliverablesOf(member) {
			from := b.table.Component(deliverable)
			for _, kind := range b.kindsFor(member, deliverable) {
				b.add(from, kind, to)
			}
		}
	}

	edges := make([]Edge, 0, len(b.edges))
	for edge := range b.edges {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].Kind != edges[j].Kind {
			return edges[i].Kind < edges[j].Kind
		}
		return edges[i].To < edges[j].To
	})
	return edges
}

func (b *edgeBuilder) add(from string, kind EdgeKind, to string) {
	b.edges[Edge{From: from, Kind: kind, To: to}] = true
}

// knownLeaf reports whether the document says a local identity depends on
// nothing: no relation from it named a target that exists. That is how the
// CycloneDX dependency cascade arrives at an empty dependsOn, and SPDX states
// the same fact rather than leaving it unsaid -- an absent relationship there
// means no assertion.
func (b *edgeBuilder) knownLeaf(local string) bool { return b.targets[local] == 0 }

func (b *edgeBuilder) isDeliverable(id string) bool {
	return id == b.productID || b.artifacts[id]
}

// deliverablesOf is the deliverables a grouped member reached: in
// single-artifact mode the product, in assembly mode the artifacts its files
// name -- or the product, when no file names any.
func (b *edgeBuilder) deliverablesOf(member domain.Component) []string {
	if !b.assembly {
		return []string{b.productID}
	}
	named := map[string]bool{}
	for _, canonical := range b.filesOf[member.ID] {
		for _, ref := range b.files[canonical].Properties["sbomb:evidence:artifacts"] {
			if id := strings.TrimPrefix(ref, "artifact:"); b.artifacts[id] {
				named[id] = true
			}
		}
	}
	if len(named) == 0 {
		return []string{b.productID}
	}
	out := make([]string, 0, len(named))
	for id := range named {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// kindsFor is the edge kinds of the pair (deliverable, component).
//
// In single-artifact mode every form of the component reached the one
// deliverable. In assembly mode a form counts for an artifact only when a file
// of the component that names that artifact (sbomb:evidence:artifacts) was
// reached through it -- the component may be a static archive member of one
// artifact and a header-only dependency of another. build-tool is the
// exception: a tool reaches a deliverable through the build, not through a
// file the deliverable contains, so its form holds for every artifact that
// named the component at all. The product of an assembly stands for "no
// artifact claimed this", and takes the component's forms as they are.
func (b *edgeBuilder) kindsFor(component domain.Component, deliverable string) []EdgeKind {
	forms := component.LinkageForms
	if b.assembly && deliverable != b.productID {
		reached := map[string]bool{}
		for _, canonical := range b.filesOf[component.ID] {
			file := b.files[canonical]
			if !namesArtifact(file, deliverable) {
				continue
			}
			for _, form := range file.Properties["sbomb:file:linkageForm"] {
				reached[form] = true
			}
		}
		var kept []string
		for _, form := range component.LinkageForms {
			if reached[form] || form == domain.LinkageBuildTool {
				kept = append(kept, form)
			}
		}
		forms = kept
	}
	seen := map[EdgeKind]bool{}
	var kinds []EdgeKind
	add := func(kind EdgeKind) {
		if !seen[kind] {
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	for _, form := range forms {
		kind, known := edgeKindsOfForm[form]
		if !known {
			continue
		}
		add(kind)
		if kind == EdgeDynamicLink && component.EnvironmentProvided {
			add(EdgeProvidedDependency)
		}
	}
	return kinds
}

func namesArtifact(file domain.UsedFile, artifact string) bool {
	for _, ref := range file.Properties["sbomb:evidence:artifacts"] {
		if strings.TrimPrefix(ref, "artifact:") == artifact {
			return true
		}
	}
	return false
}
