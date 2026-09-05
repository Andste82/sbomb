package evidence

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"

	"github.com/example/sbomb/internal/domain"
)

// SourceClass represents the class of an evidence source per §8.6.
type SourceClass string

const (
	SourceClassStructuredAuthoritative SourceClass = "structured-authoritative"
	SourceClassStructuredSecondary     SourceClass = "structured-secondary"
	SourceClassTextualFallback         SourceClass = "textual-fallback"
)

// DeriveConfidence returns the base confidence for an edge given its strength and source class,
// per the table in §8.6. This is the starting point before downgrades in §8.7.
func DeriveConfidence(strength domain.Strength, sourceClass SourceClass) domain.Confidence {
	// Table from §8.6: Strength × SourceClass → base confidence
	table := map[domain.Strength]map[SourceClass]domain.Confidence{
		"direct": {
			SourceClassStructuredAuthoritative: domain.ConfidenceHigh,
			SourceClassStructuredSecondary:     domain.ConfidenceHigh,
			SourceClassTextualFallback:         domain.ConfidenceMedium,
		},
		"linked": {
			SourceClassStructuredAuthoritative: domain.ConfidenceHigh,
			SourceClassStructuredSecondary:     domain.ConfidenceMedium,
			SourceClassTextualFallback:         domain.ConfidenceLow,
		},
		"derived": {
			SourceClassStructuredAuthoritative: domain.ConfidenceHigh,
			SourceClassStructuredSecondary:     domain.ConfidenceMedium,
			SourceClassTextualFallback:         domain.ConfidenceLow,
		},
		"packaged": {
			SourceClassStructuredAuthoritative: domain.ConfidenceHigh,
			SourceClassStructuredSecondary:     domain.ConfidenceMedium,
			SourceClassTextualFallback:         domain.ConfidenceLow,
		},
		"generated": {
			SourceClassStructuredAuthoritative: domain.ConfidenceMedium,
			SourceClassStructuredSecondary:     domain.ConfidenceMedium,
			SourceClassTextualFallback:         domain.ConfidenceLow,
		},
		"weak": {
			SourceClassStructuredAuthoritative: domain.ConfidenceLow,
			SourceClassStructuredSecondary:     domain.ConfidenceLow,
			SourceClassTextualFallback:         domain.ConfidenceLow,
		},
	}

	if c, ok := table[strength]; ok {
		if conf, ok := c[sourceClass]; ok {
			return conf
		}
	}
	return domain.ConfidenceUnknown
}

// ApplyDowngrades applies downgrades to a confidence level per §8.7.
// Each downgrade applies Downgrade() once (cumulative), flooring at unknown.
// The downgrades are returned sorted.
func ApplyDowngrades(conf domain.Confidence, reasons ...string) (domain.Confidence, []string) {
	result := conf
	for range reasons {
		result = result.Downgrade()
	}
	sort.Strings(reasons)
	return result, reasons
}

// DumpFormat is the JSON structure for the evidence dump per Appendix C.
type DumpFormat struct {
	SchemaVersion int64            `json:"schemaVersion"`
	ToolVersion   string           `json:"toolVersion"`
	GeneratedAt   string           `json:"generatedAt,omitempty"`
	Anchors       []AnchorDump     `json:"anchors"`
	Nodes         []NodeDump       `json:"nodes"`
	Edges         []EdgeDump       `json:"edges"`
	Findings      []domain.Finding `json:"findings,omitempty"`
}

type AnchorDump struct {
	Key  domain.AnchorKey `json:"key"`
	Hint string           `json:"hint,omitempty"` // basename hint only, never absolute path
}

type NodeDump struct {
	ID         domain.NodeID     `json:"id"`
	Kind       domain.NodeKind   `json:"kind"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type EdgeDump struct {
	From       domain.NodeID       `json:"from"`
	To         domain.NodeID       `json:"to"`
	Type       domain.EvidenceType `json:"type"`
	Strength   domain.Strength     `json:"strength"`
	Confidence domain.Confidence   `json:"confidence"`
	Source     string              `json:"source"`
	Adapter    string              `json:"adapter"`
	Attributes map[string]string   `json:"attributes,omitempty"`
	Downgrades []string            `json:"downgrades,omitempty"`
}

// Dump writes the evidence graph to a JSON dump file in Appendix C format.
func (g *Graph) Dump(w io.Writer) error {
	// Collect anchors from nodes (file-like nodes only)
	anchorKeys := make(map[domain.AnchorKey]bool)
	for _, node := range g.nodes {
		if node.File != nil {
			anchorKeys[node.File.Anchor] = true
		}
	}

	// Sort anchor keys
	var anchors []domain.AnchorKey
	for key := range anchorKeys {
		anchors = append(anchors, key)
	}
	sort.Slice(anchors, func(i, j int) bool {
		return anchors[i] < anchors[j]
	})

	// Build anchor dumps
	anchorDumps := make([]AnchorDump, len(anchors))
	for i, key := range anchors {
		anchorDumps[i] = AnchorDump{
			Key: key,
			// Hint would be populated with basename only if known from config/discovery
			// For now, leave empty
		}
	}

	// Build node dumps (sorted)
	nodes := g.Nodes()
	nodeDumps := make([]NodeDump, len(nodes))
	for i, node := range nodes {
		nodeDumps[i] = NodeDump{
			ID:         node.ID,
			Kind:       node.Kind,
			Attributes: node.Attributes,
		}
	}

	// Build edge dumps (sorted)
	edges := g.Edges()
	edgeDumps := make([]EdgeDump, len(edges))
	for i, edge := range edges {
		downgrades := edge.Downgrades
		if downgrades == nil {
			downgrades = []string{}
		}
		edgeDumps[i] = EdgeDump{
			From:       edge.From,
			To:         edge.To,
			Type:       edge.Type,
			Strength:   edge.Strength,
			Confidence: edge.Confidence,
			Source:     edge.Source,
			Adapter:    edge.Adapter,
			Attributes: edge.Attributes,
			Downgrades: downgrades,
		}
	}

	dump := DumpFormat{
		SchemaVersion: 1,
		ToolVersion:   "0.0.0", // will be replaced by CLI layer
		Anchors:       anchorDumps,
		Nodes:         nodeDumps,
		Edges:         edgeDumps,
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(dump); err != nil {
		return fmt.Errorf("dump error: %w", err)
	}

	return nil
}

// LoadDump loads an evidence graph from a JSON dump file.
func LoadDump(r io.Reader) (*Graph, error) {
	var dump DumpFormat
	dec := json.NewDecoder(r)
	if err := dec.Decode(&dump); err != nil {
		return nil, fmt.Errorf("load error: %w", err)
	}

	if dump.SchemaVersion != 1 {
		return nil, fmt.Errorf("load error: unsupported schemaVersion %d", dump.SchemaVersion)
	}

	g := New()

	// Load nodes
	for _, nodeDump := range dump.Nodes {
		node := domain.Node{
			ID:         nodeDump.ID,
			Kind:       nodeDump.Kind,
			Attributes: nodeDump.Attributes,
		}
		if node.Attributes == nil {
			node.Attributes = make(map[string]string)
		}
		g.AddNode(node)
	}

	// Load edges
	for _, edgeDump := range dump.Edges {
		edge := domain.Edge{
			From:       edgeDump.From,
			To:         edgeDump.To,
			Type:       edgeDump.Type,
			Strength:   edgeDump.Strength,
			Confidence: edgeDump.Confidence,
			Source:     edgeDump.Source,
			Adapter:    edgeDump.Adapter,
			Attributes: edgeDump.Attributes,
			Downgrades: slices.Clone(edgeDump.Downgrades),
		}
		if edge.Attributes == nil {
			edge.Attributes = make(map[string]string)
		}
		g.AddEdge(edge)
	}

	return g, nil
}
