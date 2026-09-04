package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/example/sbomb/internal/adapters/compiledb"
	makeadapter "github.com/example/sbomb/internal/adapters/make"
	"github.com/example/sbomb/internal/adapters/manifest"
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
	return RunWithOptions(cfg, buildDir, reproducible, Options{PathFlavor: pathmodel.DefaultFlavor()})
}

type Options struct {
	PathFlavor pathmodel.Flavor
}

func RunWithOptions(cfg config.Config, buildDir string, reproducible bool, options Options) (Result, error) {
	if buildDir == "" {
		return Result{}, fmt.Errorf("build dir is required")
	}
	if options.PathFlavor == nil {
		options.PathFlavor = pathmodel.DefaultFlavor()
	}
	projectRoot := cfg.Project.Root
	if projectRoot == "" {
		projectRoot = "."
	}
	graph := evidence.New()
	artifactName := "build"
	if len(cfg.Artifacts) > 0 && cfg.Artifacts[0].Path != "" {
		artifactName = pathmodel.Base(cfg.Artifacts[0].Path, options.PathFlavor)
	}
	artifactID := domain.NodeID("artifact:" + pathmodel.Slug(artifactName, 64))
	graph.AddNode(domain.Node{ID: artifactID, Kind: domain.NodeArtifact})
	findings := make([]domain.Finding, 0)
	components := make([]cyclonedx.Component, 0)
	for _, manifestPath := range cfg.Manifests {
		path := manifestPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectRoot, path)
		}
		if _, err := manifest.ParseFile(path); err != nil {
			id := "MISSING_PACKAGE_EVIDENCE"
			if errors.Is(err, manifest.ErrInputLimitExceeded) {
				id = "INPUT_LIMIT_EXCEEDED"
			}
			findings = append(findings, domain.Finding{ID: id, Severity: domain.SeverityWarning, Subject: domain.Subject{Kind: "configuration", Ref: manifestPath}, Message: err.Error()})
		}
	}

	compilePath := filepath.Join(buildDir, "compile_commands.json")
	commands, err := compiledb.ParseFile(compilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return Result{}, fmt.Errorf("parse compile database: %w", err)
		}
		findings = append(findings, domain.Finding{ID: "MISSING_COMPILE_EVIDENCE", Severity: domain.SeverityWarning, Subject: domain.Subject{Kind: "build", Ref: buildDir}, Message: "compile_commands.json was not found"})
	}
	seen := map[string]bool{}
	componentSeen := map[string]bool{}
	addComponent := func(path string) {
		canonical := pathmodel.ResolveWithFlavor(path, projectRoot, buildDir, options.PathFlavor)
		if componentSeen[canonical] {
			return
		}
		componentSeen[canonical] = true
		components = append(components, fileComponent(canonical, path, options.PathFlavor))
	}
	for _, command := range commands {
		rel := pathmodel.ResolveWithFlavor(command.File, projectRoot, buildDir, options.PathFlavor)
		if seen[rel] {
			continue
		}
		seen[rel] = true
		sourceID := domain.NodeID(rel)
		objectRef := command.Output
		if objectRef == "" {
			objectRef = rel + ".o"
		}
		objectID := domain.NodeID("object:" + pathmodel.ResolveWithFlavor(objectRef, projectRoot, buildDir, options.PathFlavor))
		graph.AddNode(domain.Node{ID: objectID, Kind: domain.NodeObject})
		graph.AddNode(domain.Node{ID: sourceID, Kind: domain.NodeSource})
		graph.AddEdge(domain.Edge{From: artifactID, To: objectID, Type: "compile-output", Strength: "linked", Confidence: domain.ConfidenceMedium, Source: "compile_commands", Adapter: "compiledb"})
		graph.AddEdge(domain.Edge{From: objectID, To: sourceID, Type: "compile", Strength: "derived", Confidence: domain.ConfidenceHigh, Source: "compile_commands", Adapter: "compiledb"})
		addComponent(command.File)
	}
	if len(commands) == 0 {
		if makeBuild, makeErr := makeadapter.Parse(buildDir); makeErr == nil {
			for _, target := range makeBuild.Targets {
				for _, input := range target.LinkInputs {
					canonical := pathmodel.ResolveWithFlavor(input, projectRoot, buildDir, options.PathFlavor)
					objectID := domain.NodeID("object:" + canonical)
					kind := domain.NodeObject
					if strings.HasSuffix(input, ".a") || strings.HasSuffix(input, ".lib") {
						kind = domain.NodeArchive
					}
					graph.AddNode(domain.Node{ID: objectID, Kind: kind})
					graph.AddEdge(domain.Edge{From: artifactID, To: objectID, Type: "link", Strength: "linked", Confidence: domain.ConfidenceHigh, Source: filepath.Join(target.Directory, "link.txt"), Adapter: "make"})
					if source, ok := target.ObjectSources[input]; ok {
						sourceCanonical := pathmodel.ResolveWithFlavor(source, projectRoot, buildDir, options.PathFlavor)
						sourceID := domain.NodeID(sourceCanonical)
						graph.AddNode(domain.Node{ID: sourceID, Kind: domain.NodeSource})
						graph.AddEdge(domain.Edge{From: objectID, To: sourceID, Type: "source-mapping", Strength: "derived", Confidence: domain.ConfidenceHigh, Source: filepath.Join(target.Directory, "build.make"), Adapter: "make"})
						addComponent(source)
					}
					for _, dependency := range target.ObjectDeps[input] {
						if dependency == target.ObjectSources[input] {
							continue
						}
						dependencyCanonical := pathmodel.ResolveWithFlavor(dependency, projectRoot, buildDir, options.PathFlavor)
						dependencyID := domain.NodeID(dependencyCanonical)
						graph.AddNode(domain.Node{ID: dependencyID, Kind: domain.NodeHeader})
						graph.AddEdge(domain.Edge{From: objectID, To: dependencyID, Type: "include", Strength: "derived", Confidence: domain.ConfidenceMedium, Source: target.CompilerDepend, Adapter: "make"})
						addComponent(dependency)
					}
					addComponent(input)
				}
			}
		}
	}
	if (len(commands) == 0 && !hasMakeLinkEvidence(buildDir)) || !hasLinkEvidence(buildDir) {
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
		bom.SerialNumber = cyclonedx.ReproducibleSerialNumber(bom)
	} else {
		bom.SerialNumber = "urn:uuid:" + uuid.NewString()
		bom.Metadata.Timestamp = buildTimestamp()
	}
	return Result{Graph: graph, Findings: findings, BOM: bom}, nil
}

func fileComponent(canonical, path string, flavor pathmodel.Flavor) cyclonedx.Component {
	component := cyclonedx.Component{Type: "file", Name: pathmodel.Base(path, flavor), BomRef: "file:" + canonical, Properties: []cyclonedx.Property{{Name: "sbomb:path:canonical", Value: canonical}}}
	data, err := os.ReadFile(path)
	if err == nil {
		hash := sha256.Sum256(data)
		component.Hashes = []cyclonedx.Hash{{Alg: "SHA-256", Value: hex.EncodeToString(hash[:])}}
	}
	return component
}

func buildTimestamp() string {
	if value := os.Getenv("SOURCE_DATE_EPOCH"); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
		}
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func hasLinkEvidence(buildDir string) bool {
	for _, name := range []string{"link-trace.txt", "link.d", "link.map"} {
		if info, err := os.Stat(filepath.Join(buildDir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	if hasMakeLinkEvidence(buildDir) {
		return true
	}
	for _, pattern := range []string{"*.map", "*.d"} {
		matches, err := filepath.Glob(filepath.Join(buildDir, pattern))
		if err == nil && len(matches) > 0 {
			return true
		}
	}
	return false
}

func hasMakeLinkEvidence(buildDir string) bool {
	matches, err := filepath.Glob(filepath.Join(buildDir, "CMakeFiles", "*.dir", "link.txt"))
	return err == nil && len(matches) > 0
}
