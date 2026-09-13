package generate

import (
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

// Section 16, evidence source 2: the build graph names the inputs of the rule
// that produced a generated file.
//
// The direction matters for the whole attribution export. A generated source
// is in the artifact and is therefore distributed, while the generator that
// wrote it -- and everything that generator was built from -- is reachable
// only through `generator-input` edges and is therefore build-time-only
// (section 24.5). Without these edges a code generator is not in the evidence
// graph at all, so its licence is neither reported nor excluded: it is unseen,
// which is the one outcome an attribution document may not have.
//
// A generator's existence is never evidence that it ran. The edges are read
// from the build graph, which records what the build declared, and no path is
// inspected for a directory name.

// maxGeneratorChainDepth bounds how far the inputs of a generated file are
// followed through the build graph (section 30). One hop reaches the
// generator, a second the source it was compiled from; eight leaves room for a
// generator that was itself generated, and refuses to walk a build graph that
// describes a cycle.
const maxGeneratorChainDepth = 8

// generatorEvidence is the build graph as this derivation needs it: edges
// keyed by the identity of the file they produce, because a node carries an
// identity and not the spelling the build system happened to record.
type generatorEvidence struct {
	// inputs maps the identity of an output to the inputs its edge declares,
	// each still spelled as the build recorded it so that it can be resolved
	// against the roots of section 7.
	inputs map[string][]string
	// objectSources maps the identity of an object to the source the mapping
	// evidence of section 13.2 claimed for it.
	objectSources map[string]string
}

// addGeneratorEvidence connects every generated file already in the graph to
// the inputs the build graph declares for it, and follows an input that is
// itself a build product down to the files it was made from.
//
// It adds only nodes and edges below files the graph already holds, so a
// generator nothing delivered stays out of the document: the reachability
// filter runs afterwards, as it does for packaging evidence.
func addGeneratorEvidence(graph *evidence.Graph, b *builder, compile *compileEvidence, logger *Logger) {
	if compile == nil || len(compile.buildEdges) == 0 {
		return
	}
	build := &generatorEvidence{
		inputs:        make(map[string][]string, len(compile.buildEdges)),
		objectSources: make(map[string]string, len(compile.objectSources)),
	}
	// identityOf rather than identify: keying the build graph must not
	// register every output of the build as a file of the product.
	//
	// The keys are taken in sorted order and the first spelling of an identity
	// wins, because two recorded spellings can resolve to one identity -- the
	// same output named "generated/table.c" and "./generated/table.c" -- and
	// letting map order decide which of them supplies the inputs would make
	// the document depend on it (section 25).
	for _, output := range sortedKeys(compile.buildEdges) {
		identity := b.identityOf(output).Canonical()
		if _, taken := build.inputs[identity]; !taken {
			build.inputs[identity] = compile.buildEdges[output]
		}
	}
	for _, object := range sortedKeys(compile.objectSources) {
		identity := b.identityOf(object).Canonical()
		if _, taken := build.objectSources[identity]; !taken {
			build.objectSources[identity] = compile.objectSources[object]
		}
	}

	expanded := map[string]bool{}
	added := 0
	// The node list is taken once. The walk adds nodes, and the files whose
	// inputs are being asked for are the ones the graph already holds.
	for _, node := range graph.Nodes() {
		if !generatedFileNode(node) {
			continue
		}
		added += addGeneratorInputs(graph, b, build, node.ID, string(node.ID), 0, expanded)
	}
	if added > 0 {
		logger.Info("The build graph named %d generator input(s) of the generated files in the graph", added)
	}
}

// generatedFileNode reports whether a node is a file the build produced and
// that a rule could have generated. The build root is a registered anchor of
// section 7.2 and not a directory name to be recognized.
//
// Objects and archives are not asked about: what produced them is compile and
// archive evidence, which section 13.2 already answers, and asking the build
// graph the same question again would put a second kind of edge on a chain
// that has one.
func generatedFileNode(node domain.Node) bool {
	if node.File == nil || node.File.Anchor != "build" || node.File.RelPath == "" {
		return false
	}
	switch node.Kind {
	case domain.NodeSource, domain.NodeHeader, domain.NodeAsset:
		return true
	default:
		return false
	}
}

// addGeneratorInputs records the inputs of one build-graph output and follows
// an input that is itself produced by the build. Following it is what carries
// a generator's own sources into the graph: the generator is an input of the
// file it wrote, and the source it was compiled from is an input of it.
//
// An object never becomes a node on this chain. It is a transient build
// artifact (section 13.1) that no document carries, and the object-to-source
// mapping of section 13.2 has already answered what it was compiled from, so
// the chain is recorded to that source directly. Where no mapping claimed the
// object, the build graph is asked for its inputs instead and they are
// attached to the same generator.
//
// Each output is expanded once. A file that is an input of two generated files
// still gets both edges -- that is what makes it reachable from both -- but its
// own subtree is walked a single time.
func addGeneratorInputs(
	graph *evidence.Graph,
	b *builder,
	build *generatorEvidence,
	fromID domain.NodeID,
	output string,
	depth int,
	expanded map[string]bool,
) int {
	if depth >= maxGeneratorChainDepth {
		b.logger.Debug("The generator chain of '%s' is deeper than %d edges and is not followed further",
			output, maxGeneratorChainDepth)
		return 0
	}
	inputs, known := build.inputs[output]
	if !known {
		return 0
	}
	added := 0
	for _, input := range inputs {
		identity := b.identityOf(input).Canonical()
		_, produced := build.inputs[identity]
		if isObjectPath(input) {
			if source, mapped := build.objectSources[identity]; mapped {
				added += attachGeneratorInput(graph, b, build, fromID, source, depth, expanded)
				continue
			}
			if produced {
				// An object is not a node on this chain, so what reaches the
				// document here is the recursion: it attaches the object's own
				// inputs to *this* parent. Memoizing it by the object alone
				// would give the second of two generated files that share a
				// tool object no edge at all -- a spurious
				// MISSING_GENERATOR_INPUT_EVIDENCE, and its generator's licence
				// attributed to nothing. The guard is therefore per parent,
				// which is what the doc comment above promises: both edges, one
				// walk each. Cycles stay bounded by maxGeneratorChainDepth.
				key := string(fromID) + "\x00" + identity
				if !expanded[key] {
					expanded[key] = true
					added += addGeneratorInputs(graph, b, build, fromID, identity, depth+1, expanded)
				}
				continue
			}
			// No mapping claimed it and no edge produced it: an object of its
			// own, which is a file nothing else accounts for, so it is
			// recorded rather than dropped.
		}
		added += attachGeneratorInput(graph, b, build, fromID, input, depth, expanded)
	}
	return added
}

// attachGeneratorInput records one input of a generation step and, when that
// input is itself something the build produced, follows it.
func attachGeneratorInput(
	graph *evidence.Graph,
	b *builder,
	build *generatorEvidence,
	fromID domain.NodeID,
	input string,
	depth int,
	expanded map[string]bool,
) int {
	canonical, scope := b.identify(input)
	if canonical == string(fromID) {
		return 0
	}
	_, produced := build.inputs[canonical]
	// A node the graph already holds keeps what it is. AddNode replaces, and
	// this walk classifies by one question alone -- did the build produce this
	// file -- which is true of a generated source that was then compiled into
	// the artifact. Replacing it would call that source a generator, and
	// section 13.1 keeps a build-anchored generator whose inputs resolved out
	// of the document: the file would leave the SBOM with its licence, while
	// still being in the product. The same replacement discarded an archive
	// member's attributes, which two readers of the graph depend on.
	//
	// The edge below is added either way, because being an input of a rule is
	// true whatever else the file is. This mirrors the source-mapping loop of
	// section 13.2, which skips a node the graph already holds for the same
	// reason.
	if _, known := graph.Node(domain.NodeID(canonical)); !known {
		graph.AddNode(domain.Node{
			ID:         domain.NodeID(canonical),
			Kind:       generatorNodeKind(canonical, produced),
			File:       &domain.FileID{Anchor: anchorOf(canonical), RelPath: relOf(canonical)},
			Attributes: map[string]string{"scope": string(scope)},
		})
	}
	graph.AddEdge(domain.Edge{
		From: fromID, To: domain.NodeID(canonical),
		Type: "generator-input", Strength: "generated", Confidence: domain.ConfidenceHigh,
		Source: "build.ninja", Adapter: "ninja",
	})
	added := 1
	if produced && !expanded[canonical] {
		expanded[canonical] = true
		added += addGeneratorInputs(graph, b, build, domain.NodeID(canonical), canonical, depth+1, expanded)
	}
	return added
}

// generatorNodeKind names what an input of a generation step is. A file the
// build itself produced and then fed to another rule is the generator -- the
// tool that ran -- and section 13.1 keeps a build product whose inputs are
// represented out of the document. Anything else is a file the generator read,
// and that is an input of the product like any other.
func generatorNodeKind(canonical string, produced bool) domain.NodeKind {
	if produced {
		if strings.HasSuffix(canonical, ".a") || strings.HasSuffix(canonical, ".lib") {
			return domain.NodeArchive
		}
		return domain.NodeGenerator
	}
	switch {
	case isObjectPath(canonical):
		return domain.NodeObject
	case isHeaderPath(canonical):
		return domain.NodeHeader
	case isSourcePath(canonical):
		return domain.NodeSource
	default:
		return domain.NodeGeneratorInput
	}
}

// missingGeneratorInputFindings reports a generated file the document carries
// and that no evidence source could trace to an input (section 16). The
// absence has to be said out loud: a generated source whose inputs are unknown
// means the code generator behind it -- and its licence -- is not described by
// this document, and silence there is indistinguishable from a build that
// generated nothing.
func missingGeneratorInputFindings(graph *evidence.Graph, used []domain.Node) []domain.Finding {
	findings := make([]domain.Finding, 0)
	for _, node := range used {
		if !generatedFileNode(node) {
			continue
		}
		if represented, _ := representInSBOM(graph, node); !represented {
			continue
		}
		var traced bool
		for _, edge := range graph.EdgesFrom(node.ID) {
			if edge.Type == "generator-input" {
				traced = true
				break
			}
		}
		if traced {
			continue
		}
		findings = append(findings, domain.Finding{
			ID: "MISSING_GENERATOR_INPUT_EVIDENCE", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "file", Ref: string(node.ID)},
			Message: "the build produced this file and no evidence names what produced it, " +
				"so whatever generated it is not described by this document",
			Remediation: "Build with Ninja, whose build graph names the inputs of the generating rule, " +
				"or declare the generated file and its inputs in a packaging manifest.",
		})
	}
	return findings
}
