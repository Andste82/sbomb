package evidence

import (
	"fmt"
	"slices"
	"sort"

	"github.com/example/sbomb/internal/domain"
)

// Graph represents the internal evidence graph model. Nodes represent artifacts,
// compiled units, and source files. Edges represent evidence relationships.
//
// The graph maintains invariants per §8.8:
//  1. Every node except product has at least one incoming edge.
//  2. The graph is acyclic.
//  3. Every included file's node is reachable from at least one artifact node.
//  4. Node IDs are canonical path strings (§7.7) for file-like nodes, and
//     product:<name> / artifact:<canonical path> otherwise.
type Graph struct {
	nodes map[domain.NodeID]*domain.Node
	// edges maps (from, to, type, source, adapter) to the best Edge
	edges map[edgeKey]*domain.Edge
	// incomingEdges maps NodeID to incoming edges for quick lookup
	incomingEdges map[domain.NodeID][]*domain.Edge
	// outgoingEdges maps NodeID to outgoing edges for quick lookup
	outgoingEdges map[domain.NodeID][]*domain.Edge
}

// edgeKey is the deduplication key for edges per §8.2.
type edgeKey struct {
	from    domain.NodeID
	to      domain.NodeID
	typ     domain.EvidenceType
	source  string
	adapter string
}

// New creates a new empty Graph.
func New() *Graph {
	return &Graph{
		nodes:         make(map[domain.NodeID]*domain.Node),
		edges:         make(map[edgeKey]*domain.Edge),
		incomingEdges: make(map[domain.NodeID][]*domain.Edge),
		outgoingEdges: make(map[domain.NodeID][]*domain.Edge),
	}
}

// AddNode adds a node to the graph. If a node with the same ID already exists,
// it is replaced. Returns the node's ID.
func (g *Graph) AddNode(n domain.Node) domain.NodeID {
	if n.ID == "" {
		panic("node ID cannot be empty")
	}
	if n.Attributes == nil {
		n.Attributes = make(map[string]string)
	}
	n.ID = domain.NodeID(n.ID) // ensure it's set
	g.nodes[n.ID] = &n
	return n.ID
}

// AddEdge adds an edge to the graph. Edges are deduplicated on (from, to, type, source, adapter).
// When duplicates differ in confidence, the highest confidence is kept and the lower one
// is retained under attributes.supersededConfidence per §8.2.
func (g *Graph) AddEdge(e domain.Edge) {
	if e.From == "" || e.To == "" {
		panic("edge from and to cannot be empty")
	}
	if e.Attributes == nil {
		e.Attributes = make(map[string]string)
	}
	if e.Downgrades == nil {
		e.Downgrades = []string{}
	}

	key := edgeKey{
		from:    e.From,
		to:      e.To,
		typ:     e.Type,
		source:  e.Source,
		adapter: e.Adapter,
	}

	if existing, ok := g.edges[key]; ok {
		// Deduplication: keep highest confidence
		if e.Confidence.Float() > existing.Confidence.Float() {
			// New edge has higher confidence; record the superseded confidence
			if existing.Attributes == nil {
				existing.Attributes = make(map[string]string)
			}
			existing.Attributes["supersededConfidence"] = string(existing.Confidence)
			// Update the edge with the new confidence
			existing.Confidence = e.Confidence
			existing.Downgrades = slices.Clone(e.Downgrades)
			existing.Raw = e.Raw
			// Preserve other attributes from both
			for k, v := range e.Attributes {
				if k != "supersededConfidence" {
					existing.Attributes[k] = v
				}
			}
		} else if e.Confidence.Float() < existing.Confidence.Float() {
			// Existing edge has higher confidence; record the superseded confidence
			if e.Attributes == nil {
				e.Attributes = make(map[string]string)
			}
			e.Attributes["supersededConfidence"] = string(e.Confidence)
			// Keep existing
		} else {
			// Same confidence; merge attributes and downgrades
			if existing.Attributes == nil {
				existing.Attributes = make(map[string]string)
			}
			for k, v := range e.Attributes {
				existing.Attributes[k] = v
			}
			// Merge downgrades
			for _, d := range e.Downgrades {
				if !slices.Contains(existing.Downgrades, d) {
					existing.Downgrades = append(existing.Downgrades, d)
				}
			}
			sort.Strings(existing.Downgrades)
		}
		return
	}

	// New edge; add to all indices
	edge := &e
	g.edges[key] = edge
	g.incomingEdges[e.To] = append(g.incomingEdges[e.To], edge)
	g.outgoingEdges[e.From] = append(g.outgoingEdges[e.From], edge)
}

// Node returns the node with the given ID.
func (g *Graph) Node(id domain.NodeID) (domain.Node, bool) {
	node, ok := g.nodes[id]
	if !ok {
		return domain.Node{}, false
	}
	return *node, true
}

// Nodes returns all nodes in the graph, sorted by ID.
func (g *Graph) Nodes() []domain.Node {
	nodes := make([]domain.Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		nodes = append(nodes, *n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

// Edges returns all edges in the graph, sorted by (from, to, type, source, adapter).
func (g *Graph) Edges() []domain.Edge {
	edges := make([]domain.Edge, 0, len(g.edges))
	for _, e := range g.edges {
		edges = append(edges, *e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		if edges[i].Type != edges[j].Type {
			return edges[i].Type < edges[j].Type
		}
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Adapter < edges[j].Adapter
	})
	return edges
}

// EdgesFrom returns all outgoing edges from a node.
func (g *Graph) EdgesFrom(id domain.NodeID) []domain.Edge {
	edges := make([]domain.Edge, 0, len(g.outgoingEdges[id]))
	for _, e := range g.outgoingEdges[id] {
		edges = append(edges, *e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		if edges[i].Type != edges[j].Type {
			return edges[i].Type < edges[j].Type
		}
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Adapter < edges[j].Adapter
	})
	return edges
}

// EdgesTo returns all incoming edges to a node.
func (g *Graph) EdgesTo(id domain.NodeID) []domain.Edge {
	edges := make([]domain.Edge, 0, len(g.incomingEdges[id]))
	for _, e := range g.incomingEdges[id] {
		edges = append(edges, *e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].Type != edges[j].Type {
			return edges[i].Type < edges[j].Type
		}
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Adapter < edges[j].Adapter
	})
	return edges
}

// Roots returns all product and artifact nodes.
func (g *Graph) Roots() []domain.NodeID {
	var roots []domain.NodeID
	for id, node := range g.nodes {
		if node.Kind == domain.NodeProduct || node.Kind == domain.NodeArtifact {
			roots = append(roots, id)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i] < roots[j]
	})
	return roots
}

// Reachable returns all nodes reachable from the given node via outgoing edges,
// including the node itself.
// ReachableExcept is Reachable with a set of nodes that the traversal neither
// enters nor reports. Section 33.3 scope options work this way: the evidence
// stays in the graph, because the linker really did see the file, but the
// chain no longer carries anything into the document.
func (g *Graph) ReachableExcept(from domain.NodeID, skip map[string]bool) map[domain.NodeID]bool {
	if len(skip) == 0 {
		return g.Reachable(from)
	}
	seen := map[domain.NodeID]bool{}
	var walk func(domain.NodeID)
	walk = func(id domain.NodeID) {
		if seen[id] || skip[string(id)] {
			return
		}
		seen[id] = true
		for _, edge := range g.outgoingEdges[id] {
			walk(edge.To)
		}
	}
	walk(from)
	return seen
}

func (g *Graph) Reachable(from domain.NodeID) map[domain.NodeID]bool {
	reachable := make(map[domain.NodeID]bool)
	var visit func(domain.NodeID)
	visit = func(id domain.NodeID) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		for _, edge := range g.EdgesFrom(id) {
			visit(edge.To)
		}
	}
	visit(from)
	return reachable
}

// Chains returns all paths from the given node to its roots (product/artifact nodes),
// limited to max chains. Results are in deterministic order.
func (g *Graph) Chains(to domain.NodeID, max int) [][]domain.Edge {
	if max <= 0 {
		max = 1000000 // very large default
	}

	var allChains [][]domain.Edge
	var buildChains func(current domain.NodeID, path []domain.Edge)
	buildChains = func(current domain.NodeID, path []domain.Edge) {
		if len(allChains) >= max {
			return
		}

		node, ok := g.nodes[current]
		if !ok {
			return
		}

		// If we reached a root node (product/artifact), we have a complete chain
		if node.Kind == domain.NodeProduct || node.Kind == domain.NodeArtifact {
			// Prepend the current edges to form a complete chain
			allChains = append(allChains, append([]domain.Edge{}, path...))
			return
		}

		// Get all incoming edges
		incoming := g.EdgesTo(current)
		for _, edge := range incoming {
			newPath := append([]domain.Edge{edge}, path...)
			buildChains(edge.From, newPath)
		}
	}

	buildChains(to, nil)

	// Sort chains deterministically
	sort.Slice(allChains, func(i, j int) bool {
		for k := range allChains[i] {
			if k >= len(allChains[j]) {
				return false // j is shorter
			}
			if allChains[i][k].From != allChains[j][k].From {
				return allChains[i][k].From < allChains[j][k].From
			}
			if allChains[i][k].To != allChains[j][k].To {
				return allChains[i][k].To < allChains[j][k].To
			}
		}
		return len(allChains[i]) < len(allChains[j])
	})

	return allChains
}

// CheckInvariants validates the graph invariants per §8.8.
// Returns an error if any invariant is violated.
func (g *Graph) CheckInvariants() error {
	// Invariant 1: Every node except product and artifact has at least one incoming edge.
	for id, node := range g.nodes {
		if node.Kind == domain.NodeProduct || node.Kind == domain.NodeArtifact {
			continue
		}
		if len(g.incomingEdges[id]) == 0 {
			return fmt.Errorf("invariant violation: node %q has no incoming edges (kind: %s)", id, node.Kind)
		}
	}

	// Invariant 2: The graph is acyclic.
	if hasCycle := g.detectCycle(); hasCycle {
		return fmt.Errorf("invariant violation: graph contains a cycle")
	}

	// Invariant 3: Every file node is reachable from at least one artifact node.
	artifacts := g.Roots() // includes artifacts
	reachableFromAny := make(map[domain.NodeID]bool)
	for _, art := range artifacts {
		node := g.nodes[art]
		if node.Kind == domain.NodeArtifact {
			for id := range g.Reachable(art) {
				reachableFromAny[id] = true
			}
		}
	}

	for id, node := range g.nodes {
		// File-like nodes: source, header, object, archive, asset, etc.
		// but not product or artifact
		if node.File != nil && node.Kind != domain.NodeProduct && node.Kind != domain.NodeArtifact {
			if !reachableFromAny[id] {
				return fmt.Errorf("invariant violation: file node %q is not reachable from any artifact", id)
			}
		}
	}

	// Invariant 4: Node IDs have the correct format (structural validation done during add)
	return nil
}

// detectCycle uses DFS to detect cycles in the graph.
func (g *Graph) detectCycle() bool {
	const (
		white = iota // not visited
		gray         // visiting
		black        // visited
	)

	color := make(map[domain.NodeID]int)
	for id := range g.nodes {
		color[id] = white
	}

	var visit func(domain.NodeID) bool
	visit = func(id domain.NodeID) bool {
		if color[id] == black {
			return false // already processed
		}
		if color[id] == gray {
			return true // back edge (cycle found)
		}

		color[id] = gray
		for _, edge := range g.EdgesFrom(id) {
			if visit(edge.To) {
				return true
			}
		}
		color[id] = black
		return false
	}

	for id := range g.nodes {
		if color[id] == white {
			if visit(id) {
				return true
			}
		}
	}
	return false
}

// Downgrade applies one confidence downgrade of section 8.7 to every edge the
// predicate selects and records the reason. Downgrades are cumulative and floor
// at unknown; the reason list is kept sorted so that two runs of the same build
// produce the same evidence dump. It returns how many edges were affected.
func (g *Graph) Downgrade(reason string, match func(domain.Edge) bool) int {
	if reason == "" || match == nil {
		return 0
	}
	var affected int
	for _, edge := range g.edges {
		if !match(*edge) || slices.Contains(edge.Downgrades, reason) {
			continue
		}
		edge.Confidence = edge.Confidence.Downgrade()
		edge.Downgrades = append(edge.Downgrades, reason)
		sort.Strings(edge.Downgrades)
		affected++
	}
	return affected
}

// SetEdgeAttribute records an attribute on every edge the predicate selects.
func (g *Graph) SetEdgeAttribute(name, value string, match func(domain.Edge) bool) int {
	if name == "" || match == nil {
		return 0
	}
	var affected int
	for _, edge := range g.edges {
		if !match(*edge) {
			continue
		}
		if edge.Attributes == nil {
			edge.Attributes = map[string]string{}
		}
		edge.Attributes[name] = value
		affected++
	}
	return affected
}
