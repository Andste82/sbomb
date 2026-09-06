package generate

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/adapters/manifest"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/limits"
)

// Section 18. A firmware image contains inputs the compiler and linker never
// see: filesystem contents, certificates, partition tables, configuration
// blobs. For those, a manifest that explicitly names a file as an input is
// sufficient evidence -- and adjacency never is. An asset sitting next to a
// generated output is not evidence that it produced it.

// maxInstallManifestBytes bounds the install manifest (section 30).
const maxInstallManifestBytes = 8 << 20

// roleKinds maps the roles of appendix E to what they are in the graph.
var roleKinds = map[string]struct {
	node  domain.NodeKind
	class domain.FileClass
}{
	"asset":           {domain.NodeAsset, domain.FileClassAsset},
	"generated-asset": {domain.NodeAsset, domain.FileClassAsset},
	"config":          {domain.NodeAsset, domain.FileClassAsset},
	"data":            {domain.NodeAsset, domain.FileClassAsset},
	"image":           {domain.NodeImage, domain.FileClassUnknown},
	"artifact":        {domain.NodeArtifact, domain.FileClassUnknown},
}

// manifestSource is one manifest and where it was read from.
type manifestSource struct {
	path     string
	manifest manifest.Manifest
}

// addPackagingEvidence records what the package and image manifests say. The
// edges point from an output to its inputs, so the reachability filter decides
// what reaches the product: a manifest describing an image nothing delivers
// contributes nothing, which is the same rule every other adapter follows.
func addPackagingEvidence(
	graph *evidence.Graph,
	b *builder,
	cfg config.Config,
	buildDir string,
	deliverables []Deliverable,
	logger *Logger,
) []domain.Finding {
	findings := make([]domain.Finding, 0)
	sources, sourceFindings := readManifests(cfg, buildDir, logger)
	findings = append(findings, sourceFindings...)

	// A manifest output that names a deliverable has to reuse that node, or
	// the chain would run beside the artifact instead of through it.
	artifactIDs := map[string]domain.NodeID{}
	for _, deliverable := range deliverables {
		canonical, _ := b.identify(deliverable.EvidencePath)
		artifactIDs[canonical] = domain.NodeID("artifact:" + canonical)
	}

	for _, source := range sources {
		for _, output := range source.manifest.Outputs {
			outputCanonical, _ := b.identify(resolveManifestPath(cfg, buildDir, output.Path))
			outputID, isArtifact := artifactIDs[outputCanonical]
			if !isArtifact {
				outputID = domain.NodeID(outputCanonical)
				graph.AddNode(domain.Node{
					ID:         outputID,
					Kind:       nodeKindForOutput(output.Kind),
					File:       &domain.FileID{Anchor: anchorOf(outputCanonical), RelPath: relOf(outputCanonical)},
					Attributes: map[string]string{"scope": string(b.scopeOfCanonical(outputCanonical))},
				})
			}
			for _, input := range output.Inputs {
				findings = append(findings,
					addManifestInput(graph, b, cfg, buildDir, source.path, outputID, input)...)
			}
			logger.Info("Manifest '%s': %s has %d declared input(s)",
				filepath.Base(source.path), outputCanonical, len(output.Inputs))
		}
	}
	return findings
}

// addManifestInput records one declared input and, when the manifest says
// where it came from, the generator inputs behind it (section 16).
func addManifestInput(
	graph *evidence.Graph,
	b *builder,
	cfg config.Config,
	buildDir, manifestPath string,
	outputID domain.NodeID,
	input manifest.Input,
) []domain.Finding {
	canonical, scope := b.identify(resolveManifestPath(cfg, buildDir, input.Path))
	kinds, known := roleKinds[input.Role]
	if !known {
		kinds = roleKinds["data"]
	}
	graph.AddNode(domain.Node{
		ID:   domain.NodeID(canonical),
		Kind: kinds.node,
		File: &domain.FileID{Anchor: anchorOf(canonical), RelPath: relOf(canonical)},
		Attributes: map[string]string{
			"scope": string(scope),
			"role":  input.Role,
		},
	})
	graph.AddEdge(domain.Edge{
		From: outputID, To: domain.NodeID(canonical),
		Type: "packaging", Strength: "packaged", Confidence: domain.ConfidenceHigh,
		Source: "manifest:" + filepath.Base(manifestPath), Adapter: "manifest",
	})

	findings := make([]domain.Finding, 0)
	if len(input.GeneratedFrom) == 0 {
		if input.Role == "generated-asset" {
			// Section 16: a generated file with no named input is a hole in
			// the chain, not something to fill in by looking next to it.
			findings = append(findings, domain.Finding{
				ID: "MISSING_GENERATOR_INPUT_EVIDENCE", Severity: domain.SeverityWarning,
				Subject:     domain.Subject{Kind: "file", Ref: canonical},
				Message:     "the manifest calls this a generated asset but names nothing it was generated from",
				Remediation: "Add generatedFrom to the manifest input, or record the custom command's dependencies.",
			})
		}
		return findings
	}
	for _, from := range input.GeneratedFrom {
		fromCanonical, fromScope := b.identify(resolveManifestPath(cfg, buildDir, from))
		graph.AddNode(domain.Node{
			ID:         domain.NodeID(fromCanonical),
			Kind:       domain.NodeAsset,
			File:       &domain.FileID{Anchor: anchorOf(fromCanonical), RelPath: relOf(fromCanonical)},
			Attributes: map[string]string{"scope": string(fromScope), "role": "asset"},
		})
		graph.AddEdge(domain.Edge{
			From: domain.NodeID(canonical), To: domain.NodeID(fromCanonical),
			Type: "generator-input", Strength: "generated", Confidence: domain.ConfidenceHigh,
			Source: "manifest:" + filepath.Base(manifestPath), Adapter: "manifest",
		})
	}
	return findings
}

// readManifests collects every manifest this run can see: the ones the
// configuration names, the native manifest a build may generate into its own
// tree, and the install manifest CMake writes.
func readManifests(cfg config.Config, buildDir string, logger *Logger) ([]manifestSource, []domain.Finding) {
	findings := make([]domain.Finding, 0)
	sources := make([]manifestSource, 0)
	seen := map[string]bool{}

	candidates := make([]string, 0, len(cfg.Manifests)+1)
	for _, path := range cfg.Manifests {
		if !filepath.IsAbs(path) {
			path = filepath.Join(cfg.Project.Root, path)
		}
		candidates = append(candidates, path)
	}
	// A build that generates its own packaging manifest puts it in the build
	// tree, where it is evidence like any other generated file.
	candidates = append(candidates, filepath.Join(buildDir, "sbomb-manifest.json"))

	for _, path := range candidates {
		if seen[path] {
			continue
		}
		seen[path] = true
		parsed, err := manifest.ParseFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			id := "MISSING_PACKAGE_EVIDENCE"
			if errors.Is(err, manifest.ErrInputLimitExceeded) {
				id = "INPUT_LIMIT_EXCEEDED"
			}
			findings = append(findings, domain.Finding{
				ID: id, Severity: domain.SeverityWarning,
				Subject: domain.Subject{Kind: "configuration", Ref: filepath.Base(path)},
				Message: err.Error(),
			})
			continue
		}
		sources = append(sources, manifestSource{path: path, manifest: parsed})
	}

	if installed, ok := readInstallManifest(filepath.Join(buildDir, "install_manifest.txt")); ok {
		logger.Info("Install manifest: %d installed file(s)", len(installed.Outputs[0].Inputs))
		sources = append(sources, manifestSource{
			path:     filepath.Join(buildDir, "install_manifest.txt"),
			manifest: installed,
		})
	}
	return sources, findings
}

// readInstallManifest turns the file list CMake writes on install into the
// native shape. It names what a package would contain, which section 18 lists
// as a manifest kind of its own.
func readInstallManifest(path string) (manifest.Manifest, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxInstallManifestBytes {
		return manifest.Manifest{}, false
	}
	file, err := os.Open(path)
	if err != nil {
		return manifest.Manifest{}, false
	}
	defer file.Close()

	inputs := make([]manifest.Input, 0)
	scanner := limits.Scanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		inputs = append(inputs, manifest.Input{Path: line, Role: "artifact"})
	}
	if scanner.Err() != nil || len(inputs) == 0 {
		return manifest.Manifest{}, false
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Path < inputs[j].Path })
	// The install set is attributed to the install destination, which CMake
	// does not name here; the output carries the manifest's own path so the
	// node is identifiable and the chain reaches it only if something else
	// delivers it.
	return manifest.Manifest{
		SchemaVersion: 1,
		Outputs: []manifest.Output{{
			Path:   filepath.Join(filepath.Dir(path), "install_manifest.txt"),
			Kind:   "package",
			Inputs: inputs,
		}},
	}, true
}

// resolveManifestPath applies the rule of appendix E: paths are relative to
// the project root unless absolute. A path inside the build tree is given as
// such by the build that wrote it.
func resolveManifestPath(cfg config.Config, buildDir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	root := cfg.Project.Root
	if root == "" || root == "." {
		// Without a configured project root the only root this run knows is
		// the build tree; the path is left relative so that identity is
		// computed against the logical build root rather than against the
		// directory the evidence happens to be read from (section 7.6).
		return path
	}
	return filepath.Join(root, path)
}

func nodeKindForOutput(kind string) domain.NodeKind {
	switch kind {
	case "image":
		return domain.NodeImage
	case "package":
		return domain.NodePackage
	default:
		return domain.NodeArtifact
	}
}

// packagingClassOf reports the file class a manifest role implies, so that an
// asset is recorded as one rather than as an unclassified file.
func packagingClassOf(node domain.Node) (domain.FileClass, bool) {
	if node.Attributes == nil {
		return "", false
	}
	kinds, known := roleKinds[node.Attributes["role"]]
	if !known || kinds.class == domain.FileClassUnknown {
		return "", false
	}
	return kinds.class, true
}
