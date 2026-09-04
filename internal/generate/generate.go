package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example/sbomb/internal/adapters/compiledb"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/google/uuid"
)

type Result struct {
	Graph    *evidence.Graph
	Findings []domain.Finding
	BOM      cyclonedx.BOM
}

// Run assembles the currently available build evidence into one deterministic
// graph and CycloneDX document. Adapters contribute only evidence they can
// prove; missing evidence is returned as a finding for policy evaluation.
func Run(cfg config.Config, buildDir string, reproducible bool) (Result, error) {
	if buildDir == "" {
		return Result{}, fmt.Errorf("build dir is required")
	}
	projectRoot := cfg.Project.Root
	if projectRoot == "" {
		projectRoot = "."
	}
	graph := evidence.New()
	artifactID := domain.NodeID("artifact:" + pathmodel.Slug(filepath.Base(buildDir), 64))
	graph.AddNode(domain.Node{ID: artifactID, Kind: domain.NodeArtifact})
	findings := make([]domain.Finding, 0)
	components := make([]cyclonedx.Component, 0)

	compilePath := filepath.Join(buildDir, "compile_commands.json")
	commands, err := compiledb.ParseFile(compilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return Result{}, fmt.Errorf("parse compile database: %w", err)
		}
		findings = append(findings, domain.Finding{ID: "MISSING_COMPILE_EVIDENCE", Severity: domain.SeverityWarning, Subject: domain.Subject{Kind: "build", Ref: buildDir}, Message: "compile_commands.json was not found"})
	}
	seen := map[string]bool{}
	for _, command := range commands {
		rel := pathmodel.Resolve(command.File, projectRoot, buildDir)
		if seen[rel] {
			continue
		}
		seen[rel] = true
		sourceID := domain.NodeID(rel)
		objectRef := command.Output
		if objectRef == "" {
			objectRef = rel + ".o"
		}
		objectID := domain.NodeID("object:" + pathmodel.Resolve(objectRef, projectRoot, buildDir))
		graph.AddNode(domain.Node{ID: objectID, Kind: domain.NodeObject})
		graph.AddNode(domain.Node{ID: sourceID, Kind: domain.NodeSource})
		graph.AddEdge(domain.Edge{From: artifactID, To: objectID, Type: "compile-output", Strength: "linked", Confidence: domain.ConfidenceMedium, Source: "compile_commands", Adapter: "compiledb"})
		graph.AddEdge(domain.Edge{From: objectID, To: sourceID, Type: "compile", Strength: "derived", Confidence: domain.ConfidenceHigh, Source: "compile_commands", Adapter: "compiledb"})
		components = append(components, fileComponent(rel, command.File))
	}
	if len(commands) == 0 {
		findings = append(findings, domain.Finding{ID: "MISSING_LINK_EVIDENCE", Severity: domain.SeverityError, Subject: domain.Subject{Kind: "artifact", Ref: string(artifactID)}, Message: "no compile or link evidence was discovered"})
	}
	sort.Slice(components, func(i, j int) bool { return components[i].BomRef < components[j].BomRef })
	deps := make([]cyclonedx.Dependency, 0, len(components)+1)
	for _, component := range components {
		deps = append(deps, cyclonedx.Dependency{Ref: component.BomRef})
	}
	bom := cyclonedx.BOM{BomFormat: "CycloneDX", SpecVersion: "1.6", Version: 1, Components: components, Dependencies: deps, Metadata: &cyclonedx.Metadata{Tools: []cyclonedx.Tool{{Vendor: "sbomb", Name: "sbomb", Version: "0.0.0-milestone14"}}}}
	if reproducible {
		// The writer omits timestamps in reproducible mode; the serial is derived
		// from the canonical document by the existing CycloneDX implementation.
		bom.SerialNumber = "urn:uuid:" + deterministicSerial(bom)
	} else {
		bom.SerialNumber = "urn:uuid:" + uuid.NewString()
		bom.Metadata.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	return Result{Graph: graph, Findings: findings, BOM: bom}, nil
}

func fileComponent(canonical, path string) cyclonedx.Component {
	component := cyclonedx.Component{Type: "file", Name: filepath.Base(path), BomRef: "file:" + canonical, Properties: []cyclonedx.Property{{Name: "sbomb:path:canonical", Value: canonical}}}
	data, err := os.ReadFile(path)
	if err == nil {
		hash := sha256.Sum256(data)
		component.Hashes = []cyclonedx.Hash{{Alg: "SHA-256", Value: hex.EncodeToString(hash[:])}}
	}
	return component
}

func deterministicSerial(bom cyclonedx.BOM) string {
	b, _ := cyclonedx.MarshalBOM(bom)
	hash := sha256.Sum256([]byte(b))
	return strings.ToLower(hex.EncodeToString(hash[:]))[:32]
}
