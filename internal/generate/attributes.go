package generate

import (
	"fmt"
	"strings"

	"github.com/example/sbomb/internal/adapters/archive"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

// This file derives the two attributes of section 24.5 that come out of the
// evidence graph and nothing else: what is distributed, and how it got into
// the artifact. No path is inspected for a directory name, no layout is
// assumed, and no list of "interesting" components is consulted.

// buildTimeEdge reports whether an evidence type carries a file into the build
// rather than into the artifact.
//
// The list is deliberately the *exception* and not the rule. Section 8.3
// defines fourteen evidence types and the code emits six; an allowlist of
// "distributing" types would make build-time-only the default for anything
// unlisted, so adding an evidence type later would silently drop a component
// out of an attribution document. Omission has to fail towards inclusion, so
// the question asked here is the narrow one: is this edge one of the three
// that provably does not put bytes in the deliverable.
func buildTimeEdge(evidenceType domain.EvidenceType) bool {
	switch evidenceType {
	case "generator-input", "generator-output", "toolchain":
		return true
	default:
		return false
	}
}

// linkageRank orders the forms from the most direct presence in the artifact
// to the least, so that a file reached by more than one chain publishes the
// strongest of them. A file that is both a generator input and a compiled
// source is compiled, which is the case section 24.5 names.
var linkageRank = []string{
	domain.LinkageStaticObject,
	domain.LinkageStaticArchiveMember,
	domain.LinkageDynamic,
	domain.LinkageEmbeddedAsset,
	domain.LinkageHeaderOnly,
	domain.LinkageBuildTool,
}

// objectContributing are the forms that put object code or bytes into the
// artifact. A component carrying none of them contributed no object code,
// which is what section 24.5 calls header-only.
var objectContributing = map[string]bool{
	domain.LinkageStaticObject:        true,
	domain.LinkageStaticArchiveMember: true,
	domain.LinkageDynamic:             true,
	domain.LinkageEmbeddedAsset:       true,
}

// archiveUsage is how much of one static archive the linker took.
type archiveUsage struct {
	// used is the number of members the link evidence proves were extracted.
	used int
	// total is the number of members the archive holds, read from the archive
	// itself. It is zero when the archive could not be read, and the ratio is
	// then not published at all rather than published against a guess.
	total int
}

// graphAttributes is the answer for one run: a role and a linkage form per
// file, and the archive arithmetic behind static-archive-member.
type graphAttributes struct {
	role    map[string]string
	linkage map[string]string
	// archivesOf names, per file identity, the static archives its object was
	// extracted from. Nearly always one; a source compiled into two archives
	// that are both linked is two.
	archivesOf map[string]map[string]bool
	usage      map[string]archiveUsage
}

// roleOf answers for one file identity. A file the derivation never saw is
// distributed: the same direction the edge-type question is asked in.
func (a *graphAttributes) roleOf(canonical string) string {
	if a == nil {
		return domain.RoleDistributed
	}
	if role, known := a.role[canonical]; known {
		return role
	}
	return domain.RoleDistributed
}

// linkageOf answers for one file identity, or "" when no chain established a
// form. Silence is deliberate: an unrecognized evidence type must not be
// dressed up as a linkage form.
func (a *graphAttributes) linkageOf(canonical string) string {
	if a == nil {
		return ""
	}
	return a.linkage[canonical]
}

// walkState is what a chain carries downward: the form the chain has
// established so far and, for a chain that went through an archive, which
// archive it was.
type walkState struct {
	form    string
	archive string
}

// deriveGraphAttributes walks the graph once from every artifact and records,
// per node, the strongest linkage form any chain establishes and whether any
// chain reaches it without crossing a build-time edge.
//
// The traversal is memoized on (node, state) rather than on node alone,
// because the state is what a chain carries: the same object reached through
// an archive and reached directly is two different answers, and collapsing
// them would make the result depend on which chain the walk happened to see
// first.
func deriveGraphAttributes(
	graph *evidence.Graph,
	artifactIDs []domain.NodeID,
	excluded map[string]bool,
	physical map[string]string,
	logger *Logger,
) *graphAttributes {
	attributes := &graphAttributes{
		role:       map[string]string{},
		linkage:    map[string]string{},
		archivesOf: map[string]map[string]bool{},
		usage:      map[string]archiveUsage{},
	}
	forms := map[string]map[string]bool{}
	distributed := map[string]bool{}
	reached := map[string]bool{}

	type visitKey struct {
		node  domain.NodeID
		state walkState
	}
	seen := map[visitKey]bool{}

	var walk func(node domain.NodeID, state walkState)
	walk = func(node domain.NodeID, state walkState) {
		key := visitKey{node: node, state: state}
		if seen[key] {
			return
		}
		seen[key] = true

		canonical := string(node)
		reached[canonical] = true
		if state.form != "" {
			if forms[canonical] == nil {
				forms[canonical] = map[string]bool{}
			}
			forms[canonical][state.form] = true
		}
		if state.form != domain.LinkageBuildTool {
			distributed[canonical] = true
		}
		if state.archive != "" {
			if attributes.archivesOf[canonical] == nil {
				attributes.archivesOf[canonical] = map[string]bool{}
			}
			attributes.archivesOf[canonical][state.archive] = true
		}

		for _, edge := range graph.EdgesFrom(node) {
			if excluded[string(edge.To)] {
				continue
			}
			walk(edge.To, nextState(graph, state, edge))
		}
	}

	for _, artifactID := range artifactIDs {
		// The artifact carries no form of its own: it is what the forms are
		// relative to.
		walk(artifactID, walkState{})
	}

	for canonical, set := range forms {
		attributes.linkage[canonical] = strongestLinkage(set)
	}
	for canonical := range reached {
		if distributed[canonical] {
			attributes.role[canonical] = domain.RoleDistributed
			continue
		}
		attributes.role[canonical] = domain.RoleBuildTimeOnly
	}

	attributes.countArchiveMembers(graph, excluded, physical, logger)
	return attributes
}

// nextState applies the edge rules of section 24.5 to one step of the walk.
func nextState(graph *evidence.Graph, state walkState, edge domain.Edge) walkState {
	// A chain that has crossed a build-time edge stays build-time for the rest
	// of its length: nothing below a generator input is in the artifact by way
	// of that chain. Another chain may still reach the same node directly, and
	// that is exactly what makes the node distributed.
	if buildTimeEdge(edge.Type) || state.form == domain.LinkageBuildTool {
		return walkState{form: domain.LinkageBuildTool}
	}
	switch edge.Type {
	case "link":
		if sharedLibraryIdentity(string(edge.To)) {
			return walkState{form: domain.LinkageDynamic}
		}
		if node, known := graph.Node(edge.To); known && node.Kind == domain.NodeArchive {
			return walkState{form: domain.LinkageStaticArchiveMember, archive: string(edge.To)}
		}
		return walkState{form: domain.LinkageStaticObject}
	case "archive-member":
		return walkState{form: domain.LinkageStaticArchiveMember, archive: string(edge.From)}
	case "packaging":
		return walkState{form: domain.LinkageEmbeddedAsset}
	case "header-dependency":
		return walkState{form: domain.LinkageHeaderOnly}
	case "source-mapping":
		// A source inherits the form of the object it was compiled into: how
		// the object reached the artifact is how the source contributed.
		return state
	default:
		// An evidence type this derivation has never seen carries the chain on
		// unchanged. It establishes no form -- silence is the honest answer --
		// and it is not a build-time edge, so the node stays distributed.
		return state
	}
}

// sharedLibraryIdentity reports whether an identity names a shared library.
// The suffixes are the linker's own, and they are the same ones kindForPath
// uses to decide what a link input is -- kindForPath cannot be reused for the
// answer, because it maps an archive and a shared library onto the same node
// kind, which is exactly the distinction needed here.
//
// The test is on the whole identity rather than on the part below the anchor:
// a suffix is a suffix either way, and splitting first would make the answer
// depend on whether the anchor key happens to carry a sub-name.
func sharedLibraryIdentity(canonical string) bool {
	lowered := strings.ToLower(canonical)
	switch {
	case strings.HasSuffix(lowered, ".so"), strings.HasSuffix(lowered, ".dll"),
		strings.HasSuffix(lowered, ".dylib"), strings.Contains(lowered, ".so."):
		return true
	default:
		return false
	}
}

// strongestLinkage picks the published form for a node several chains reached.
func strongestLinkage(set map[string]bool) string {
	for _, form := range linkageRank {
		if set[form] {
			return form
		}
	}
	return ""
}

// countArchiveMembers reads each static archive the link evidence named and
// records how many of its members were extracted. The archive itself is the
// only honest source for the denominator: the build system's declared inputs
// say what was meant to be archived, and the archive says what is in it.
func (a *graphAttributes) countArchiveMembers(
	graph *evidence.Graph,
	excluded map[string]bool,
	physical map[string]string,
	logger *Logger,
) {
	extracted := map[string]map[string]bool{}
	for _, edge := range graph.Edges() {
		if edge.Type != "archive-member" || excluded[string(edge.To)] {
			// A member every contributed section was discarded from is not in
			// the document (section 4.5), so counting it among the used ones
			// would publish a ratio the document contradicts.
			continue
		}
		from := string(edge.From)
		if extracted[from] == nil {
			extracted[from] = map[string]bool{}
		}
		extracted[from][string(edge.To)] = true
	}
	for _, archiveID := range sortedKeys(extracted) {
		usage := archiveUsage{used: len(extracted[archiveID])}
		if archivePath := physical[archiveID]; archivePath != "" {
			if members, err := archive.ParseFile(archivePath); err == nil {
				usage.total = len(members)
			} else if logger != nil {
				logger.Debug("Archive '%s' index could not be read: %v", archiveID, err)
			}
		}
		a.usage[archiveID] = usage
	}
}

// applyFileAttributes writes the two file-level properties of section 24.5.
func applyFileAttributes(files []domain.UsedFile, attributes *graphAttributes) {
	for index := range files {
		canonical := files[index].ID.Canonical()
		if files[index].Properties == nil {
			files[index].Properties = map[string][]string{}
		}
		files[index].Properties["sbomb:file:distributionRole"] = []string{attributes.roleOf(canonical)}
		if form := attributes.linkageOf(canonical); form != "" {
			files[index].Properties["sbomb:file:linkageForm"] = []string{form}
		}
	}
}

// applyComponentAttributes aggregates the file answers onto a component
// (section 24.5) and records them as resolved facts. Rendering is the writer's
// job: component.scope and the properties are decided there.
func applyComponentAttributes(component *domain.Component, files []domain.UsedFile, attributes *graphAttributes) {
	forms := map[string]bool{}
	role := domain.RoleBuildTimeOnly
	generatedOnly, objectFiles := true, 0
	archives := map[string]bool{}
	for _, file := range files {
		canonical := file.ID.Canonical()
		if attributes.roleOf(canonical) == domain.RoleDistributed {
			// One distributed file makes the component distributed: it is
			// inside what is shipped, whatever else it also did.
			role = domain.RoleDistributed
		}
		form := attributes.linkageOf(canonical)
		if form != "" {
			forms[form] = true
		}
		if objectContributing[form] {
			objectFiles++
			if !generatedIdentity(file.ID) {
				generatedOnly = false
			}
		}
		for archiveID := range attributes.archivesOf[canonical] {
			archives[archiveID] = true
		}
	}
	component.DistributionRole = role

	// header-only is a statement about the component and not about one file:
	// a library whose header was read and whose archive member was extracted
	// is not header-only, however many headers it has. So the form survives
	// the aggregation only where nothing contributed object code.
	if objectFiles > 0 {
		delete(forms, domain.LinkageHeaderOnly)
	}
	component.HeaderOnly = len(forms) == 1 && forms[domain.LinkageHeaderOnly]

	// generated-source is the other form section 24.5 words exclusively:
	// "contributed only through generated sources". It is added beside the
	// form the linker saw rather than replacing it, because both are true --
	// the object was linked, and the source it came from was produced by the
	// build.
	if objectFiles > 0 && generatedOnly {
		forms[domain.LinkageGeneratedSource] = true
	}
	if role == domain.RoleBuildTimeOnly {
		// Section 24.5: build-tool is the linkage form of a build-time-only
		// component, so the two attributes cannot disagree.
		forms[domain.LinkageBuildTool] = true
	}
	component.LinkageForms = sortedSet(forms)

	if forms[domain.LinkageStaticArchiveMember] {
		used, total := 0, 0
		readable := len(archives) > 0
		for _, archiveID := range sortedSet(archives) {
			usage := attributes.usage[archiveID]
			if usage.total == 0 {
				readable = false
				continue
			}
			used += usage.used
			total += usage.total
		}
		if readable && total > 0 {
			component.ArchiveMembersUsed = fmt.Sprintf("%d/%d", used, total)
		}
	}

	// The document carries the role beside the specified field rather than
	// instead of it (section 28.1): component.scope is runtime reachability
	// and the role is presence in the artifact, and the two are not the same
	// axis. The writer renders the field; the property keeps the
	// evidence-based meaning.
	component.Properties = addProperty(component.Properties, "sbomb:component:distributionRole", component.DistributionRole)
	for _, form := range component.LinkageForms {
		component.Properties = addProperty(component.Properties, "sbomb:component:linkageForm", form)
	}
	component.Properties = addProperty(component.Properties, "sbomb:component:archiveMembersUsed", component.ArchiveMembersUsed)
	if component.HeaderOnly {
		component.Properties = addProperty(component.Properties, "sbomb:component:headerOnly", "true")
	}
}

// generatedIdentity reports whether a file is one the build produced. The
// build root is not a directory name to be recognized: it is a registered
// anchor of section 7.2, and nothing in a build tree was written by hand.
func generatedIdentity(id domain.FileID) bool {
	return id.Anchor == "build"
}
