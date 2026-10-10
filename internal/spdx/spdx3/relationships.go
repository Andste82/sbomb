package spdx3

import (
	"sort"
	"strconv"
	"strings"

	"github.com/example/sbomb/internal/spdx/mapping"
)

const (
	classRelationship    = "Relationship"
	classLifecycleScoped = "LifecycleScopedRelationship"
	classVexNotAffected  = "security_VexNotAffectedVulnAssessmentRelationship"
)

// edgeSpellings is how 3.0.1 says each structural edge of the model (section
// 28.11.4). The lifecycle scope is stated where sbomb knows it: a tool is used
// at build time, and a dynamic link -- or a dependency the environment
// provides -- is resolved at run time. A static link and a plain dependency
// carry no scope, because sbomb can assert no period beyond what the type
// already says.
var edgeSpellings = map[mapping.EdgeKind]struct{ relationshipType, class, scope string }{
	mapping.EdgeContains:           {"contains", classRelationship, ""},
	mapping.EdgeEmbeds:             {"contains", classRelationship, ""},
	mapping.EdgeStaticLink:         {"hasStaticLink", classRelationship, ""},
	mapping.EdgeDynamicLink:        {"hasDynamicLink", classLifecycleScoped, "runtime"},
	mapping.EdgeProvidedDependency: {"hasProvidedDependency", classLifecycleScoped, "runtime"},
	mapping.EdgeTool:               {"usesTool", classLifecycleScoped, "build"},
	mapping.EdgeDependsOn:          {"dependsOn", classRelationship, ""},
}

// relationship is one relationship before it is numbered.
type relationship struct {
	class            string
	from             string
	relationshipType string
	to               []string
	completeness     string
	scope            string
	properties       []mapping.Property
	impactStatement  string
	suppliedBy       string
}

// identity is everything about a relationship except its targets: two
// relationships with the same identity say the same thing about the same
// element, and are one relationship with the union of their targets.
func (r relationship) identity() string {
	var key strings.Builder
	for _, part := range []string{r.from, r.relationshipType, r.class, r.scope, r.completeness} {
		key.WriteString(part)
		key.WriteByte(0)
	}
	for _, property := range r.properties {
		key.WriteString(property.Name)
		key.WriteByte(1)
		key.WriteString(property.Value)
		key.WriteByte(2)
	}
	key.WriteByte(0)
	key.WriteString(r.impactStatement)
	key.WriteByte(0)
	key.WriteString(r.suppliedBy)
	return key.String()
}

// sortKey orders relationships totally: the source, the type, the class, the
// scope, the completeness, the targets, then the qualifiers. Every IRI shares
// the document prefix, so ordering IRIs orders local identities.
func (r relationship) sortKey() []string {
	return []string{r.from, r.relationshipType, r.class, r.scope, r.completeness, strings.Join(r.to, "\n"), r.identity()}
}

// relationshipSet collects relationships, merging those of one identity.
type relationshipSet struct {
	byIdentity map[string]*relationship
}

func newRelationshipSet() *relationshipSet {
	return &relationshipSet{byIdentity: map[string]*relationship{}}
}

func (s *relationshipSet) add(r relationship) {
	identity := r.identity()
	if existing, known := s.byIdentity[identity]; known {
		existing.to = append(existing.to, r.to...)
		return
	}
	r.to = append([]string(nil), r.to...)
	s.byIdentity[identity] = &r
}

// numbered returns the relationships in their final order, with their targets
// sorted and deduplicated, and gives each its number: relationship:1 to
// relationship:n, without gaps, in that order.
func (s *relationshipSet) numbered() []relationship {
	type keyed struct {
		relationship relationship
		key          []string
	}
	// The keys are computed once: a document of fifty thousand files has a
	// relationship per file, and building them inside the comparison would
	// build each one log n times.
	entries := make([]keyed, 0, len(s.byIdentity))
	for _, r := range s.byIdentity {
		sort.Strings(r.to)
		r.to = dedupeSorted(r.to)
		entries = append(entries, keyed{relationship: *r, key: r.sortKey()})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].key, entries[j].key
		for k := range a {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return false
	})
	out := make([]relationship, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.relationship)
	}
	return out
}

func relationshipLocal(number int) string { return "relationship:" + strconv.Itoa(number) }

func dedupeSorted(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
