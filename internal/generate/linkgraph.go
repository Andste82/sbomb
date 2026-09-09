package generate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/adapters/linkers/depfile"
	"github.com/example/sbomb/internal/adapters/linkers/mapparser"
	"github.com/example/sbomb/internal/adapters/ninja"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/headers"
)

// linkInput is one file the linker consumed, together with what kind of input
// it was and which evidence source reported it.
type linkInput struct {
	Path    string // as recorded in the evidence
	Kind    domain.NodeKind
	Archive string // set for an extracted archive member
	Member  string
	Source  string // evidence source identifier, e.g. "ld:app.map"
	Adapter string
}

// builder accumulates the evidence graph for one run.
type builder struct {
	graph   *evidence.Graph
	anchors *anchors.Result
	logger  *Logger
	// logicalBuild is the build root as it appears in the evidence and is what
	// identity is computed against (section 7.6). physicalBuild is where the
	// evidence is being read from now; the two differ whenever a build tree is
	// analysed somewhere other than where it was produced.
	logicalBuild  string
	physicalBuild string
	findings      []domain.Finding

	// physical maps a canonical identity back to a readable path, because
	// hashing and license detection need the bytes, not the identity.
	physical map[string]string
	// headerClass assigns each used header one of the seven classes of
	// section 14.4, driven by the include directories the toolchain reports.
	headerClass *headers.Classifier
	// archiveInputs maps an archive identity to the objects it was built from,
	// which is how an extracted member is traced back to a build-tree object.
	archiveInputs map[string][]string
	// discardedObjects counts the input sections the link evidence reported as
	// discarded, per object path as recorded (section 4.5).
	discardedObjects map[string]int
	// retainedObjects are the objects the link evidence reports as contributing
	// at least one section to the image. A LOAD line is not enough: GNU ld
	// writes one for every input it opens, including the ones it discards
	// entirely. Without this half, "fully discarded" cannot be decided, and
	// section 4.5 forbids acting on partial information.
	retainedObjects map[string]bool
	// reconstructedLinks holds link command lines recovered from the build
	// system, keyed by the artifact they produce. This is priority 5 of
	// section 11.2 and the only link evidence a Makefiles build without map or
	// dependency file offers.
	reconstructedLinks map[string][]string

	// runner and ctx are the run's own introspection gateway, used only where
	// a file source has already failed to answer (section 9.2).
	runner *exec.Runner
	ctx    context.Context
	// askedArchives remembers which archives were already put to `ninja -t
	// inputs`, so that a second unresolved member of the same archive does not
	// start a second process.
	askedArchives map[string]bool
}

func newBuilder(graph *evidence.Graph, anchorResult *anchors.Result, logicalBuild, physicalBuild string, logger *Logger) *builder {
	return &builder{
		graph:              graph,
		anchors:            anchorResult,
		logger:             logger,
		logicalBuild:       strings.TrimSuffix(logicalBuild, "/"),
		physicalBuild:      physicalBuild,
		findings:           []domain.Finding{},
		physical:           map[string]string{},
		archiveInputs:      map[string][]string{},
		discardedObjects:   map[string]int{},
		retainedObjects:    map[string]bool{},
		reconstructedLinks: map[string][]string{},
		askedArchives:      map[string]bool{},
	}
}

// setIntrospection hands over the runner an archive fallback may ask ninja
// with. It is the run's own runner, carrying its anchors and its log, so that
// every command sbomb starts is bounded and recorded in one place (section 9.2).
func (b *builder) setIntrospection(runner *exec.Runner, ctx context.Context) {
	b.runner = runner
	b.ctx = ctx
}

// recordReconstructedLink remembers the inputs of a link command recovered from
// the build system, keyed by the artifact basename it produces.
func (b *builder) recordReconstructedLink(artifact string, inputs []string) {
	name := filepath.Base(artifact)
	b.reconstructedLinks[name] = append(b.reconstructedLinks[name], inputs...)
}

// identify resolves a path recorded in build evidence to its portable
// identity, and remembers where the bytes can be read.
func (b *builder) identify(path string) (string, anchors.Scope) {
	id, scope := b.anchors.ScopeOfPath(b.logicalBuild, b.logicalFor(path))
	canonical := id.Canonical()
	if _, known := b.physical[canonical]; !known {
		b.physical[canonical] = b.physicalFor(b.logicalFor(path))
		b.logger.Trace("Identity: %-64s <- %s", canonical, path)
		// One finding per file, not one per mention: a path named by both the
		// dependency file and the map would otherwise be reported twice.
		if id.Anchor == "abs" {
			b.findings = append(b.findings, anchors.UnanchoredFinding(id))
		}
	}
	return canonical, scope
}

// identityOf resolves a path to its identity without recording it. Registering
// a path that no evidence chain reached would put it in the physical map and,
// when it anchors nowhere, emit UNANCHORED_FILE for a file that is not in the
// SBOM at all.
func (b *builder) identityOf(path string) domain.FileID {
	id, _ := b.anchors.ScopeOfPath(b.logicalBuild, b.logicalFor(path))
	return id
}

// scopeOfCanonical reports the origin scope of an identity that has already
// been resolved. ScopeOfPath must not be used for this: it resolves a *path*,
// and handing it a canonical identity silently re-anchors the string against
// the build root, which classifies a toolchain header as build output.
func (b *builder) scopeOfCanonical(canonical string) anchors.Scope {
	id := domain.FileID{Anchor: anchorOf(canonical), RelPath: relOf(canonical)}
	if scope := b.anchors.Scope(id); scope != anchors.ScopeUnknown {
		return scope
	}
	// An unanchored file may still be recognizable as a system path from where
	// its bytes actually live (section 14.4).
	if physical := b.physical[canonical]; physical != "" {
		if _, scope := b.anchors.ScopeOfPath(b.logicalBuild, physical); scope != anchors.ScopeUnknown {
			return scope
		}
	}
	return anchors.ScopeUnknown
}

// logicalFor is the inverse of physicalFor: an adapter that resolved a path
// against the directory being read reports a physical absolute path, which has
// to be expressed in the logical build root before it can be identified.
func (b *builder) logicalFor(path string) string {
	if !filepath.IsAbs(path) || b.logicalBuild == "" {
		return path
	}
	absoluteBuild, err := filepath.Abs(b.physicalBuild)
	if err != nil {
		return path
	}
	if rel, found := strings.CutPrefix(path, absoluteBuild+"/"); found {
		return b.logicalBuild + "/" + rel
	}
	return path
}

// physicalFor maps a path from the evidence to where its bytes live now.
func (b *builder) physicalFor(path string) string {
	if b.logicalBuild != "" {
		if rel, found := strings.CutPrefix(path, b.logicalBuild+"/"); found {
			return filepath.Join(b.physicalBuild, rel)
		}
		if path == b.logicalBuild {
			return b.physicalBuild
		}
	}
	if !filepath.IsAbs(path) {
		return filepath.Join(b.physicalBuild, path)
	}
	return path
}

// collectLinkEvidence reads the link evidence for one deliverable in the
// preference order of section 11.2, corrected by deviation D1: the dependency
// file is authoritative for which files the link consumed, but only the map
// names the archive members that were actually extracted.
func (b *builder) collectLinkEvidence(deliverable Deliverable, buildDir, mapPath, depfilePath string) []linkInput {
	inputs := []linkInput{}

	if path := b.locateEvidence(depfilePath, deliverable.Path, ".d"); path != "" {
		if records, err := readDepfile(path); err == nil {
			for _, record := range records {
				inputs = append(inputs, linkInput{
					Path:    record.Path,
					Kind:    kindForPath(record.Path),
					Source:  "ld:" + filepath.Base(path),
					Adapter: "linker-depfile",
				})
			}
			b.logger.Info("Link dependency file '%s': %d input(s)", filepath.Base(path), len(records))
		} else {
			b.logger.Info("Link dependency file '%s' could not be read: %v", path, err)
			b.findings = append(b.findings, domain.Finding{
				ID: "MALFORMED_LINK_EVIDENCE", Severity: domain.SeverityWarning,
				Subject: domain.Subject{Kind: "evidence", Ref: path}, Message: err.Error(),
			})
		}
	}

	if path := b.locateEvidence(mapPath, deliverable.Path, ".map"); path != "" {
		result, err := readMap(path)
		if err != nil {
			b.logger.Info("Linker map '%s' could not be read: %v", path, err)
			b.findings = append(b.findings, domain.Finding{
				ID: "MALFORMED_LINK_EVIDENCE", Severity: domain.SeverityWarning,
				Subject: domain.Subject{Kind: "evidence", Ref: path}, Message: err.Error(),
			})
		}
		var members int
		for _, record := range result.Records {
			switch record.Kind {
			case mapparser.ArchiveMember:
				members++
				inputs = append(inputs, linkInput{
					Path:    record.Path,
					Kind:    domain.NodeArchiveMember,
					Archive: record.Archive,
					Member:  record.Member,
					Source:  result.Format + ":" + filepath.Base(path),
					Adapter: "linker-map",
				})
			case mapparser.LinkedObject, mapparser.StaticArchive, mapparser.SharedLibrary:
				inputs = append(inputs, linkInput{
					Path:    record.Path,
					Kind:    kindForPath(record.Path),
					Source:  result.Format + ":" + filepath.Base(path),
					Adapter: "linker-map",
				})
			case mapparser.DiscardedSection:
				canonical, _ := b.identify(record.Path)
				b.discardedObjects[canonical]++
			case mapparser.RetainedSection:
				canonical, _ := b.identify(record.Path)
				b.retainedObjects[canonical] = true
			}
		}
		b.logger.Info("Linker map '%s' (%s): %d record(s), %d extracted archive member(s)",
			filepath.Base(path), result.Format, len(result.Records), members)
	}

	// link.exe writes /VERBOSE:REF to the build stream. CMake/Ninja callers
	// that preserve that stream as build.log provide discarded-member evidence
	// here; a missing log is a normal degradation, not a failed build.
	if data, err := os.ReadFile(filepath.Join(buildDir, "build.log")); err == nil {
		for _, record := range mapparser.ParseMSVCVerbose(string(data)) {
			canonical, _ := b.identify(record.Path)
			b.discardedObjects[canonical]++
		}
	}

	if len(inputs) == 0 {
		// Priority 5 of section 11.2: the link command line as the build
		// system recorded it. Weaker than a map, because it names what was
		// offered to the linker rather than what the linker used.
		for _, input := range b.reconstructedLinks[filepath.Base(deliverable.Path)] {
			inputs = append(inputs, linkInput{
				Path:    input,
				Kind:    kindForPath(input),
				Source:  "buildsystem:link-command",
				Adapter: "make",
			})
		}
		if len(inputs) > 0 {
			b.logger.Info("Reconstructed link command for '%s': %d input(s)", filepath.Base(deliverable.Path), len(inputs))
		}
	}

	if len(inputs) == 0 {
		b.findings = append(b.findings, domain.Finding{
			ID:       "MISSING_LINK_EVIDENCE",
			Severity: domain.SeverityError,
			Subject:  domain.Subject{Kind: "artifact", Ref: deliverable.Path},
			Message:  "no link evidence source could be read for this artifact",
			Remediation: "Build with -Wl,-Map=<artifact>.map and -Wl,--dependency-file=<artifact>.d, " +
				"which cmake/Sbomb.cmake adds for you.",
		})
	}
	return inputs
}

// locateEvidence returns a configured path when it exists, otherwise the
// conventional sidecar next to the artifact, otherwise the empty string.
func (b *builder) locateEvidence(configured, artifactPath, extension string) string {
	for _, candidate := range []string{configured, artifactPath + extension, strings.TrimSuffix(artifactPath, filepath.Ext(artifactPath)) + extension} {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func readDepfile(path string) ([]depfile.Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return depfile.ParseString(string(data))
}

func readMap(path string) (mapparser.Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return mapparser.Result{}, err
	}
	format := mapparser.Sniff(string(data))
	if format == "" {
		return mapparser.Result{}, mapparser.ErrUnknownFormat
	}
	result := mapparser.Parse(strings.NewReader(string(data)), format)
	return result, result.Err
}

// addLinkEdges turns link inputs into graph edges rooted at the artifact.
// Extracted archive members are attached to the build-tree object they came
// from, so that an unextracted member has no path to the artifact at all --
// which is exactly what section 12 requires.
func (b *builder) addLinkEdges(artifactID domain.NodeID, inputs []linkInput) {
	for _, input := range inputs {
		if input.Kind == domain.NodeArchiveMember {
			b.addArchiveMember(artifactID, input)
			continue
		}
		canonical, scope := b.identify(input.Path)
		b.graph.AddNode(domain.Node{
			ID:         domain.NodeID(canonical),
			Kind:       input.Kind,
			File:       &domain.FileID{Anchor: anchorOf(canonical), RelPath: relOf(canonical)},
			Attributes: map[string]string{"scope": string(scope)},
		})
		b.graph.AddEdge(domain.Edge{
			From: artifactID, To: domain.NodeID(canonical),
			Type: "link", Strength: "linked", Confidence: domain.ConfidenceHigh,
			Source: input.Source, Adapter: input.Adapter,
		})
	}
}

// addArchiveMember links an extracted member to the object it was archived
// from. The mapping is by basename within one archive, which is unambiguous
// because ar stores members under their basename.
func (b *builder) addArchiveMember(artifactID domain.NodeID, input linkInput) {
	archiveCanonical, archiveScope := b.identify(input.Archive)
	b.graph.AddNode(domain.Node{
		ID:         domain.NodeID(archiveCanonical),
		Kind:       domain.NodeArchive,
		File:       &domain.FileID{Anchor: anchorOf(archiveCanonical), RelPath: relOf(archiveCanonical)},
		Attributes: map[string]string{"scope": string(archiveScope)},
	})
	b.graph.AddEdge(domain.Edge{
		From: artifactID, To: domain.NodeID(archiveCanonical),
		Type: "link", Strength: "linked", Confidence: domain.ConfidenceHigh,
		Source: input.Source, Adapter: input.Adapter,
	})

	objectPath, resolved := b.memberObject(archiveCanonical, input.Member)
	if !resolved {
		// The member is real evidence even when its build-tree object is
		// unknown; it must not silently disappear (section 39.2).
		memberID := domain.NodeID(archiveCanonical + "(" + input.Member + ")")
		b.graph.AddNode(domain.Node{
			ID: memberID, Kind: domain.NodeObject,
			Attributes: map[string]string{"archive": archiveCanonical, "member": input.Member},
		})
		b.graph.AddEdge(domain.Edge{
			From: domain.NodeID(archiveCanonical), To: memberID,
			Type: "archive-member", Strength: "linked", Confidence: domain.ConfidenceMedium,
			Source: input.Source, Adapter: input.Adapter,
		})
		finding := domain.Finding{
			ID: "ARCHIVE_MEMBERS_UNRESOLVED", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "file", Ref: string(memberID)},
			Message: fmt.Sprintf("the extracted member %q could not be traced to an object in the build tree", input.Member),
		}
		// The archive's inputs have one other source, and naming it is only
		// useful while it is still available: without a build graph for ninja
		// to read, the command has nothing to answer from (section 9.2).
		if b.runner != nil && !b.runner.Features.Ninja && b.hasNinjaGraph() {
			finding.Remediation = "Pass --allow-introspection=ninja so that `ninja -t inputs` may be asked which objects the archive was built from."
		}
		b.findings = append(b.findings, finding)
		return
	}

	canonical, scope := b.identify(objectPath)
	b.graph.AddNode(domain.Node{
		ID:   domain.NodeID(canonical),
		Kind: domain.NodeObject,
		File: &domain.FileID{Anchor: anchorOf(canonical), RelPath: relOf(canonical)},
		Attributes: map[string]string{
			"scope":   string(scope),
			"archive": archiveCanonical,
			"member":  input.Member,
		},
	})
	b.graph.AddEdge(domain.Edge{
		From: domain.NodeID(archiveCanonical), To: domain.NodeID(canonical),
		Type: "archive-member", Strength: "linked", Confidence: domain.ConfidenceHigh,
		Source: input.Source, Adapter: input.Adapter,
		Attributes: map[string]string{"member": input.Member},
	})
}

// memberObject finds the build-tree object an archive member was created from,
// using the archive's declared inputs.
func (b *builder) memberObject(archiveCanonical, member string) (string, bool) {
	candidates := b.archiveInputs[archiveCanonical]
	if len(candidates) == 0 {
		// build.ninja did not name this archive, either because it could not
		// be read or because it does not mention it. Only then is ninja asked.
		candidates = b.ninjaArchiveInputs(archiveCanonical)
	}
	matches := make([]string, 0, 1)
	for _, candidate := range candidates {
		if filepath.Base(candidate) == member {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

// ninjaArchiveInputs is the section 9.2 fallback for an archive whose inputs
// build.ninja did not state: `ninja -t inputs <archive>` names them.
//
// Its answer is transitive -- an archive that pulls in another one reports the
// second one's objects too -- which is safe here only because memberObject
// insists on exactly one candidate per member name. An ambiguous answer
// therefore degrades to ARCHIVE_MEMBERS_UNRESOLVED rather than to a wrong one.
func (b *builder) ninjaArchiveInputs(archiveCanonical string) []string {
	if b.runner == nil || b.askedArchives[archiveCanonical] || !b.hasNinjaGraph() {
		return nil
	}
	b.askedArchives[archiveCanonical] = true
	target := b.ninjaTarget(archiveCanonical)
	if target == "" {
		return nil
	}
	ctx := b.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	out, err := b.runner.Run(ctx, "ninja", "-C", b.physicalBuild, "-t", "inputs", target)
	if err != nil {
		b.logger.Debug("`ninja -t inputs %s` did not answer: %v", target, err)
		return nil
	}
	objects := make([]string, 0)
	for _, line := range strings.Split(string(out), "\n") {
		input := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if strings.HasSuffix(input, ".o") || strings.HasSuffix(input, ".obj") {
			objects = append(objects, input)
		}
	}
	if len(objects) > 0 {
		b.logger.Info("`ninja -t inputs %s` named %d object(s) build.ninja did not", target, len(objects))
	}
	b.archiveInputs[archiveCanonical] = objects
	return objects
}

// hasNinjaGraph reports whether ninja has a build graph to be asked about
// here. Without one the command would fail, and a remediation offering it
// would promise an answer nothing can give.
func (b *builder) hasNinjaGraph() bool {
	return b.physicalBuild != "" && hasNinjaBuildGraph(b.physicalBuild)
}

// ninjaTarget names an archive the way ninja does: relative to the build
// directory it was invoked in. A path that lies outside it is not a target of
// this build, and asking about it would be asking about something else.
func (b *builder) ninjaTarget(archiveCanonical string) string {
	path := b.physical[archiveCanonical]
	if path == "" || b.physicalBuild == "" {
		return ""
	}
	relative, err := filepath.Rel(b.physicalBuild, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return ""
	}
	return filepath.ToSlash(relative)
}

// recordArchiveInputs remembers which objects an archive was built from, so
// that extracted members can be traced back to them.
func (b *builder) recordArchiveInputs(archivePath string, objects []string) {
	canonical, _ := b.identify(archivePath)
	b.archiveInputs[canonical] = append(b.archiveInputs[canonical], objects...)
}

// loadNinjaArchiveInputs reads build.ninja for the inputs of every archive it
// produces. Without this an extracted member cannot be traced to a source.
func (b *builder) loadNinjaArchiveInputs(buildDir string) {
	file, err := os.Open(filepath.Join(buildDir, "build.ninja"))
	if err != nil {
		return
	}
	defer file.Close()
	parsed, err := ninja.ParseFile(file)
	if err != nil {
		b.logger.Debug("build.ninja could not be parsed: %v", err)
		return
	}
	for _, rule := range parsed.Rules {
		for _, output := range rule.Outputs {
			if !strings.HasSuffix(output, ".a") && !strings.HasSuffix(output, ".lib") {
				continue
			}
			objects := make([]string, 0, len(rule.Inputs))
			for _, input := range rule.Inputs {
				if strings.HasSuffix(input, ".o") || strings.HasSuffix(input, ".obj") {
					objects = append(objects, input)
				}
			}
			b.recordArchiveInputs(output, objects)
		}
	}
}

// Findings returns the findings accumulated while building the graph.
func (b *builder) Findings() []domain.Finding {
	sort.SliceStable(b.findings, func(i, j int) bool { return b.findings[i].ID < b.findings[j].ID })
	return b.findings
}

func kindForPath(path string) domain.NodeKind {
	switch {
	case strings.HasSuffix(path, ".a"), strings.HasSuffix(path, ".lib"):
		return domain.NodeArchive
	case strings.HasSuffix(path, ".so"), strings.HasSuffix(path, ".dll"), strings.Contains(path, ".so."):
		return domain.NodeArchive
	default:
		return domain.NodeObject
	}
}

// anchorOf and relOf split a canonical identity back into its two parts.
// The anchor key itself may contain colons, so the split is on the last one
// that separates a registered key from the relative path.
func anchorOf(canonical string) domain.AnchorKey {
	anchor, _ := splitCanonical(canonical)
	return domain.AnchorKey(anchor)
}

func relOf(canonical string) string {
	_, rel := splitCanonical(canonical)
	return rel
}

func splitCanonical(canonical string) (string, string) {
	// Keys are "project", "build", "abs" or "<kind>:<name>"; the relative path
	// follows the final colon that closes the key.
	parts := strings.SplitN(canonical, ":", 2)
	if len(parts) != 2 {
		return canonical, ""
	}
	switch parts[0] {
	case "project", "build", "abs":
		return parts[0], parts[1]
	}
	rest := strings.SplitN(parts[1], ":", 2)
	if len(rest) != 2 {
		return canonical, ""
	}
	return parts[0] + ":" + rest[0], rest[1]
}

// classify assigns a header one of the seven classes of section 14.4. Files
// that are not headers have no class.
func (b *builder) classify(canonical string, generated bool) headers.Class {
	if b.headerClass == nil {
		return headers.ClassUnknown
	}
	return b.headerClass.Classify(headers.Input{
		Canonical: canonical,
		Physical:  b.physical[canonical],
		Generated: generated,
	})
}
