package spdx23

import (
	"sort"

	"github.com/example/sbomb/internal/spdx/mapping"
)

// edgeSpellings is how 2.3 says each structural edge of the model. Two of
// them are stated from the other end: 2.3 names a build tool and a provided
// dependency by what they are of ("A BUILD_TOOL_OF B", "A
// PROVIDED_DEPENDENCY_OF B"), where the model states what the deliverable
// uses.
var edgeSpellings = map[mapping.EdgeKind]struct {
	relationshipType string
	inverse          bool
}{
	mapping.EdgeContains:           {"CONTAINS", false},
	mapping.EdgeEmbeds:             {"CONTAINS", false},
	mapping.EdgeStaticLink:         {"STATIC_LINK", false},
	mapping.EdgeDynamicLink:        {"DYNAMIC_LINK", false},
	mapping.EdgeProvidedDependency: {"PROVIDED_DEPENDENCY_OF", true},
	mapping.EdgeTool:               {"BUILD_TOOL_OF", true},
	mapping.EdgeDependsOn:          {"DEPENDS_ON", false},
}

// evidenceComment is what an OTHER relationship from a package to a file it
// read a copyright statement from says.
const evidenceComment = "evidence: a copyright statement of this package was read from this file"

func (r *renderer) add(from, relationshipType, to, comment string) {
	r.rels[relationship{SPDXElementID: from, RelationshipType: relationshipType, RelatedSPDXElement: to, Comment: comment}] = true
}

// packageRelationships are what a package says beyond the structural edges:
// its patches, its evidence and that it depends on nothing.
//
// A retained licence, notice or copyright file is documentation of the
// package (DOCUMENTATION_OF). A file a copyright statement was read from is
// evidence of a kind 2.3 has no word for, so it is OTHER, with a comment that
// says what it is. A patch is applied to the package (PATCH_APPLIED: "A is a
// patch file that has been applied to B"). No DESCENDANT_OF is written for a
// modified package: the upstream it descends from is no element of the
// document, and inventing one would describe a package nobody observed.
func (r *renderer) packageRelationships(p mapping.Package) {
	id := elementID(p.LocalID)
	for _, local := range p.EvidenceFiles {
		if r.files[local].Origin&mapping.OriginLicenseFile != 0 {
			r.add(elementID(local), "DOCUMENTATION_OF", id, "")
		} else {
			r.add(id, "OTHER", elementID(local), evidenceComment)
		}
	}
	for _, patch := range p.Patches {
		r.add(elementID(patch.LocalID), "PATCH_APPLIED", id, "")
	}
	if p.KnownLeaf {
		r.add(id, "DEPENDS_ON", none, "")
	}
}

// relationships are every relationship once, in a total order.
func (r *renderer) relationships() []relationship {
	out := make([]relationship, 0, len(r.rels))
	for rel := range r.rels {
		out = append(out, rel)
	}
	sort.Slice(out, func(i, j int) bool { return relationshipLess(out[i], out[j]) })
	return out
}

func relationshipLess(a, b relationship) bool {
	if a.SPDXElementID != b.SPDXElementID {
		return a.SPDXElementID < b.SPDXElementID
	}
	if a.RelationshipType != b.RelationshipType {
		return a.RelationshipType < b.RelationshipType
	}
	if a.RelatedSPDXElement != b.RelatedSPDXElement {
		return a.RelatedSPDXElement < b.RelatedSPDXElement
	}
	return a.Comment < b.Comment
}
