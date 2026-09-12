package generate

import (
	"io"
	"os"
	"sort"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/foss"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/limits"
	"github.com/example/sbomb/internal/sbomwriter"
)

// FOSSView is what the licence view of section 32.6 adds over the view the
// document was built from.
//
// The two views differ in exactly one place. DWARF narrowing is right for a
// bill of materials and wrong for a licence question: whether a header emitted
// code is not the question, whether its interface was used is. So the FOSS
// view is the union set -- used plus narrowed -- and the difference is
// reported rather than implied.
//
// It is not a second discovery. The narrowed headers were kept with their
// names by the run that produced them, and what happens here is a bounded read
// over that known list: no graph is rebuilt, no artifact is re-parsed, nothing
// is re-hashed.
type FOSSView struct {
	// Components is the delta per component, ordered by component name.
	Components []FOSSViewDelta
	// NarrowedTotal is how many headers DWARF narrowing removed in this run,
	// across every component, including the ones no FOSS output mentions.
	// It is the "licence view vs. SBOM view" number of the completeness
	// block.
	NarrowedTotal int
}

// FOSSViewDelta is one component's share of the difference between the two
// views.
type FOSSViewDelta struct {
	// Component is the component name, which is what the review record
	// prints and what the notices document is ordered by.
	Component string
	// Narrowed is how many headers of this component the DWARF set excluded.
	Narrowed int
	// Copyrights are the statements the narrowed headers state that the
	// component does not already carry, deduplicated and ordered by text. It
	// is empty unless the narrowed headers were read, which happens only for
	// a component a FOSS output reports (section 32.6).
	Copyrights []domain.CopyrightStatement
}

// DeltaFor is the delta of one component by name, and the zero value for a
// component that has none. The renderer asks per component, and a lookup that
// answers "nothing" for an absent component keeps every call site free of a
// second branch.
func (v *FOSSView) DeltaFor(name string) FOSSViewDelta {
	if v == nil {
		return FOSSViewDelta{}
	}
	for _, delta := range v.Components {
		if delta.Component == name {
			return delta
		}
	}
	return FOSSViewDelta{}
}

// fossView builds the licence view from the narrowing the run recorded.
//
// reads decides whose narrowed headers are opened. Only the components a FOSS
// output reports are in it: section 31 forbids reading a file that is not
// needed for the output, and the narrowed header of a component that appears
// in no document is not needed. The delta is still counted for every
// component, so the number the review record states is the run's whole
// narrowing and not the part that happened to be interesting.
func fossView(narrowing []NarrowingCount, document *sbomwriter.Document,
	resolve func(string) (string, bool), bounds limits.Config, logger *Logger) *FOSSView {
	view := &FOSSView{}
	reads := fossReportedComponents(document)
	existing := copyrightsByComponent(document)
	for _, entry := range narrowing {
		view.NarrowedTotal += entry.Count
		delta := FOSSViewDelta{Component: entry.Component, Narrowed: entry.Count}
		if reads[entry.Component] {
			delta.Copyrights = narrowedHeaderCopyright(entry.Headers, existing[entry.Component], resolve, bounds, logger)
		}
		view.Components = append(view.Components, delta)
	}
	sort.Slice(view.Components, func(i, j int) bool { return view.Components[i].Component < view.Components[j].Component })
	return view
}

// fossReportedComponents names the components a FOSS output writes an entry
// for: the distributed ones whose type is not "application" (section 32.6).
//
// The criterion is the renderer's own predicate, called here rather than
// restated: foss.Reported decides who is in THIRD-PARTY-NOTICES.txt, so the
// read set and the output set cannot drift apart. Spelling the same rule twice
// is how they would -- and a component the renderer reports but this function
// does not would silently lose the copyright statements of its narrowed
// headers.
func fossReportedComponents(document *sbomwriter.Document) map[string]bool {
	reported := map[string]bool{}
	if document == nil {
		return reported
	}
	for _, component := range document.Components {
		if !foss.Reported(component) {
			continue
		}
		reported[component.Name] = true
	}
	return reported
}

// copyrightsByComponent is what each component already states, so that the
// delta is what the union view *adds* rather than what it repeats.
func copyrightsByComponent(document *sbomwriter.Document) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if document == nil {
		return out
	}
	for _, component := range document.Components {
		known := map[string]bool{}
		for _, statement := range component.Copyrights {
			known[statement.Text] = true
		}
		out[component.Name] = known
	}
	return out
}

// narrowedHeaderCopyright reads the narrowed headers of one component and
// returns the statements section 22.10 recognises in them, minus the ones the
// component already carries.
//
// The window is the file's first bytes, as everywhere else in section 22.10,
// and the read is bounded by the same parser limits the rest of the run uses.
// A header that cannot be read is skipped without a finding: it was already
// excluded from the document, and a warning about a file nobody asked to see
// would be noise.
func narrowedHeaderCopyright(headers []string, known map[string]bool,
	resolve func(string) (string, bool), bounds limits.Config, logger *Logger) []domain.CopyrightStatement {
	var collected []domain.CopyrightStatement
	for _, header := range headers {
		path, readable := resolve(header)
		if !readable || path == "" {
			continue
		}
		data, err := readCopyrightWindow(path, bounds)
		if err != nil {
			logger.Debug("Narrowed header '%s' could not be read for copyright statements: %v", header, err)
			continue
		}
		id := domain.FileID{Anchor: anchorOf(header), RelPath: relOf(header)}
		for _, text := range license.ExtractCopyright(data) {
			if known[text] {
				continue
			}
			collected = append(collected, domain.CopyrightStatement{Text: text, File: id})
		}
	}
	return license.DedupeCopyright(collected)
}

// readCopyrightWindow reads at most the section 22.10 window of a file. The
// whole file is never loaded: a narrowed header is not in the document, so
// nothing else about its bytes is ever wanted.
func readCopyrightWindow(path string, bounds limits.Config) ([]byte, error) {
	if _, err := bounds.Stat(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	// A limited reader rather than one Read call: a short read is legal and
	// would silently cut a statement in half.
	return io.ReadAll(io.LimitReader(file, license.CopyrightWindow))
}
