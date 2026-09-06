package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/adapters/binfmt"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// dwarfEvidence is what the debug information of the deliverables says: which
// translation units are actually present in the artifact, which headers reached
// them, and whether the link went through LTO.
type dwarfEvidence struct {
	// headersBySource maps a canonical source identity to the headers the
	// line-table file table of its compilation unit named.
	headersBySource map[string][]string
	// coveredSources are the sources whose compilation unit carried a line
	// program. A unit without one is not evidence of "no headers".
	coveredSources map[string]bool
	// LTO says at least one deliverable shows link-time optimization.
	LTO      bool
	Findings []domain.Finding
}

func newDWARFEvidence() *dwarfEvidence {
	return &dwarfEvidence{
		headersBySource: map[string][]string{},
		coveredSources:  map[string]bool{},
		Findings:        []domain.Finding{},
	}
}

// covers reports whether debug information described this translation unit.
func (d *dwarfEvidence) covers(sourceCanonical string) bool {
	return d != nil && d.coveredSources[sourceCanonical]
}

// available reports whether any deliverable carried usable debug information.
// Without it, the DWARF-preferred mode has nothing to prefer.
func (d *dwarfEvidence) available() bool {
	return d != nil && len(d.coveredSources) > 0
}

// inspectArtifacts reads the debug information of every deliverable
// (section 11.4). Paths in DWARF are the ones the compiler saw, so they are put
// through the same identity resolution as every other recorded path.
func inspectArtifacts(b *builder, deliverables []Deliverable, logger *Logger) *dwarfEvidence {
	result := newDWARFEvidence()
	for _, deliverable := range deliverables {
		path := b.physicalFor(b.logicalFor(deliverable.EvidencePath))
		inspected, err := binfmt.Inspect(path, binfmt.Options{})
		if err != nil {
			logger.Debug("Artifact '%s' could not be inspected: %v", path, err)
			continue
		}
		result.Findings = append(result.Findings, inspected.Findings...)
		if inspected.LTO {
			result.LTO = true
		}
		for _, unit := range inspected.CompilationUnits {
			sourceCanonical, _ := b.identify(unit.Source)
			// "DWARF is available for the CU" (section 4.4) has to mean the
			// unit carries usable header evidence, not merely that it has a
			// line program. A file table naming only the primary source -- what
			// clang emits for a unit whose headers contribute no code -- is the
			// absence of header evidence, and narrowing the depfile set against
			// it would delete headers the unit demonstrably included.
			if !unit.LineTable || len(unit.Headers) == 0 {
				continue
			}
			result.coveredSources[sourceCanonical] = true
			for _, header := range unit.Headers {
				headerCanonical, _ := b.identify(header.Path)
				result.headersBySource[sourceCanonical] = append(result.headersBySource[sourceCanonical], headerCanonical)
			}
		}
		logger.Info("Debug information in '%s': %d translation unit(s), LTO %v",
			deliverable.Path, len(inspected.CompilationUnits), inspected.LTO)
	}
	for source := range result.headersBySource {
		result.headersBySource[source] = dedupe(result.headersBySource[source])
	}
	return result
}

// headerAttachment is one translation unit's header evidence, keyed by the
// object the compilation produced. The object is the attachment point because
// reachability runs artifact -> object -> header.
type headerAttachment struct {
	object string
	source string
	// depfileHeaders are the headers the preprocessor reported reading.
	depfileHeaders []string
	// dwarfHeaders are the headers the emitted compilation unit names.
	dwarfHeaders []string
	// dwarfCovered says debug information named at least one header for this
	// unit. A unit whose file table names no header carries no header
	// evidence, and narrowing against it would only delete what the dependency
	// file proved.
	dwarfCovered bool
	// pchHeaders are the headers this unit reached through a precompiled
	// header (section 14.5).
	pchHeaders []string
}

// resolvedHeader is one header edge after the mode of section 4.4 was applied.
type resolvedHeader struct {
	object     string
	header     string
	source     string
	confidence domain.Confidence
	viaPCH     bool
}

// narrowedHeader records a header that DWARF narrowing removed. Section 4.4
// requires the narrowing to be auditable rather than silent, so what was
// dropped is kept, counted and reported.
type narrowedHeader struct {
	object string
	header string
}

type headerResolution struct {
	edges    []resolvedHeader
	narrowed []narrowedHeader
	findings []domain.Finding
	// pchOnly are headers whose only evidence path is through the PCH.
	pchOnly map[string]bool
}

// resolveHeaderEvidence applies policy.headerEvidence (section 4.4) to the two
// header sources. Under dwarf-preferred the DWARF set wins for a covered
// translation unit and the depfile-only remainder is dropped but counted; under
// union both sets are kept; under depfiles the DWARF set is ignored.
func resolveHeaderEvidence(attachments []headerAttachment, mode string) headerResolution {
	if mode == "" {
		mode = "dwarf-preferred"
	}
	resolution := headerResolution{
		edges:    []resolvedHeader{},
		narrowed: []narrowedHeader{},
		findings: []domain.Finding{},
		pchOnly:  map[string]bool{},
	}
	sort.Slice(attachments, func(i, j int) bool { return attachments[i].object < attachments[j].object })

	// Section 14.5. CMake force-includes the aggregation header into every
	// translation unit of the target, so "reached only via the PCH" cannot be
	// decided by asking which units name the header -- they all do. What
	// distinguishes them is whether any emitted compilation unit shows the
	// header contributing: a force-included header that nothing uses appears in
	// every dependency file and in no debug information.
	viaPCH := map[string]bool{}
	seenInDwarf := map[string]bool{}
	for _, attachment := range attachments {
		for _, header := range attachment.pchHeaders {
			viaPCH[header] = true
		}
		for _, header := range attachment.dwarfHeaders {
			seenInDwarf[header] = true
		}
	}
	for header := range viaPCH {
		if !seenInDwarf[header] {
			resolution.pchOnly[header] = true
		}
	}

	for _, attachment := range attachments {
		inDwarf := toSet(attachment.dwarfHeaders)
		inDepfile := toSet(attachment.depfileHeaders)

		useDwarf := mode != "depfiles" && attachment.dwarfCovered
		narrow := mode == "dwarf-preferred" && useDwarf

		if mode == "dwarf-preferred" && !attachment.dwarfCovered && len(attachment.depfileHeaders) > 0 {
			resolution.findings = append(resolution.findings, domain.Finding{
				ID: "HEADER_EVIDENCE_FALLBACK", Severity: domain.SeverityInfo,
				Subject: domain.Subject{Kind: "file", Ref: attachment.object},
				Message: "no debug information covers this translation unit; its headers come from dependency files",
			})
		}

		effective := map[string]bool{}
		if useDwarf {
			for header := range inDwarf {
				effective[header] = true
			}
		}
		if !narrow {
			for header := range inDepfile {
				effective[header] = true
			}
		}
		// The precompiled header is evidence in its own right: section 14.5
		// makes every unit of the target depend on the whole PCH header set,
		// and DWARF narrowing must not drop what a different source proved.
		for _, header := range attachment.pchHeaders {
			effective[header] = true
		}

		for _, header := range sortedSet(effective) {
			edge := resolvedHeader{object: attachment.object, header: header, viaPCH: viaPCH[header]}
			switch {
			case inDwarf[header]:
				// Section 11.4: what the emitted unit names is high confidence,
				// whether or not a depfile agrees.
				edge.confidence = domain.ConfidenceHigh
				edge.source = "debug-info"
				if inDepfile[header] {
					edge.source = "debug-info+depfile"
				}
			case inDepfile[header]:
				edge.confidence = domain.ConfidenceMedium
				edge.source = "depfile"
			default:
				edge.confidence = domain.ConfidenceMedium
				edge.source = "pch"
			}
			if edge.viaPCH {
				// Section 14.5: a header the build forced in through the
				// precompiled header is weaker evidence than a direct
				// inclusion, whatever the source that named it.
				edge.confidence = domain.ConfidenceMedium
			}
			resolution.edges = append(resolution.edges, edge)
		}

		if narrow {
			for _, header := range sortedSet(inDepfile) {
				if !effective[header] {
					resolution.narrowed = append(resolution.narrowed, narrowedHeader{object: attachment.object, header: header})
				}
			}
		}
	}

	sort.Slice(resolution.narrowed, func(i, j int) bool {
		if resolution.narrowed[i].object != resolution.narrowed[j].object {
			return resolution.narrowed[i].object < resolution.narrowed[j].object
		}
		return resolution.narrowed[i].header < resolution.narrowed[j].header
	})
	return resolution
}

// isPCHArtifact reports whether a path is one of the files CMake generates for
// target_precompile_headers: the aggregation header, the source that compiles
// it, and the compiled header itself. Section 14.5 calls them transient build
// artifacts, so they are evidence but never components.
func isPCHArtifact(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasPrefix(base, "cmake_pch.")
}

// pchIncludes reads the headers a generated PCH aggregation header names. This
// is the same deterministic, execution-free parse that section 17.1 permits for
// unity sources, applied to the file section 14.5 describes; without it the PCH
// header set has no evidence at all. Unlike a unity source the file also
// carries pragmas, so anything that is not an include is skipped rather than
// treated as a refusal.
func pchIncludes(path string) []string {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxUnityFileBytes {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	includes := make([]string, 0)
	scanner := limits.Scanner(file)
	for scanner.Scan() {
		if included, ok := includedPath(strings.TrimSpace(scanner.Text())); ok {
			includes = append(includes, included)
		}
	}
	if scanner.Err() != nil {
		return nil
	}
	return includes
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// pchExcludedFinding reports how many headers the pchHeaders policy removed
// (section 14.5). One counted finding beats one finding per header.
func pchExcludedFinding(count int, subject string) domain.Finding {
	return domain.Finding{
		ID: "PCH_HEADERS_EXCLUDED", Severity: domain.SeverityInfo,
		Subject: domain.Subject{Kind: "build", Ref: subject},
		Message: fmt.Sprintf("%d header(s) reached only through the precompiled header and were excluded by policy", count),
	}
}
