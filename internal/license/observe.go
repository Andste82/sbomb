package license

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// A licence file sometimes holds more than one licence. "Dual licensed under
// MIT or Apache-2.0" is two complete texts one after the other, with a
// sentence in between saying how they relate. Compared as a whole the file is
// neither of them, so every technique in section 22.3 returns nothing at all,
// which is the worst possible answer: both licences are plainly there.
//
// What can be established is which licence texts are present. What cannot is
// how they relate -- whether both apply or the recipient may choose is stated
// in the prose between them, and reading that would be the keyword heuristic
// section 22.3 forbids.
//
// So the two claims are kept apart, exactly as section 22.4 requires and as
// CycloneDX 1.6 models it: the licences found go to component.evidence.licenses
// as an observation, and component.licenses stays NOASSERTION until somebody
// concludes. A curated `components[].license` fills it in, and because the
// observation sits beside it, the assertion can be checked rather than
// believed.

// Observation is one complete licence text found inside a file.
type Observation struct {
	ID string
	// Start and End bound the text in the loose normal form, so that
	// overlapping candidates can be resolved and the order reported.
	Start, End int
}

// Observe returns the complete licence texts inside a file, in the order they
// appear. It is not a weaker match than the whole-file techniques: each one
// found is a full licence text, matched end to end. What is weaker is the
// claim -- that the text is present, not that it is the file's licence.
//
// A file that is exactly one licence yields exactly that licence here too, so
// callers use this only after the whole-file techniques have already failed.
func Observe(text string) []Observation {
	entries, err := loadTemplates()
	if err != nil {
		return nil
	}
	normalized := looseNormalize(text)
	if normalized == "" {
		return nil
	}

	var found []Observation
	for _, candidate := range entries {
		// A template with no invariant text of its own is skipped. It could
		// match almost anywhere, and "this span is some licence" is not an
		// observation worth making.
		if candidate.anchor == "" || !strings.Contains(normalized, candidate.anchor) {
			continue
		}
		pattern, err := candidate.unanchored()
		if err != nil {
			continue
		}
		// Searched in a window around the anchor rather than over the whole
		// file. An unanchored search tries every start position, and the
		// expression is the size of a licence; over a corpus that turned a
		// second of work into half a minute. The licence cannot be far from
		// its own invariant text, so the window is what it could span.
		start, end := candidate.window(normalized)
		location := pattern.FindStringIndex(normalized[start:end])
		if location == nil {
			continue
		}
		from, to := start+location[0], start+location[1]
		// The span has to contain the template's own invariant text. Without
		// that a template whose content is mostly variable could match a short
		// stretch of unrelated prose.
		if !strings.Contains(normalized[from:to], candidate.anchor) {
			continue
		}
		found = append(found, Observation{ID: candidate.id, Start: from, End: to})
	}
	return resolveOverlaps(found, entries)
}

// resolveOverlaps keeps the longest match where two overlap: a licence whose
// text quotes another -- the GPL appendix contains a notice, and several BSD
// variants are one another's prefix -- would otherwise be reported twice.
func resolveOverlaps(found []Observation, entries []*entry) []Observation {
	deprecated := map[string]bool{}
	for _, e := range entries {
		deprecated[e.id] = e.deprecated
	}
	sort.Slice(found, func(i, j int) bool {
		left, right := found[i], found[j]
		if left.End-left.Start != right.End-right.Start {
			return left.End-left.Start > right.End-right.Start
		}
		// Among equals a current identifier beats a superseded one, then
		// lexical order, so the answer does not depend on map iteration.
		if deprecated[left.ID] != deprecated[right.ID] {
			return !deprecated[left.ID]
		}
		return left.ID < right.ID
	})

	var kept []Observation
	for _, candidate := range found {
		overlaps := false
		for _, existing := range kept {
			if candidate.Start < existing.End && existing.Start < candidate.End {
				overlaps = true
				break
			}
		}
		if !overlaps {
			kept = append(kept, candidate)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Start < kept[j].Start })
	return kept
}

// ObserveFindings is Observe rendered as license findings, for a component's
// license evidence. The source is the file the texts were found in.
func ObserveFindings(text, source string) []domain.LicenseFinding {
	observations := Observe(text)
	if len(observations) == 0 {
		return nil
	}
	out := make([]domain.LicenseFinding, 0, len(observations))
	for _, observation := range observations {
		out = append(out, domain.LicenseFinding{
			Expression: observation.ID,
			SPDXID:     observation.ID,
			Name:       observation.ID,
			// The evidence class of section 22.4: the text is in a file of the
			// component. It is not the component's concluded licence.
			Evidence:   "component-level",
			Confidence: domain.ConfidenceHigh,
			Source:     source,
			Technique:  TechniqueTemplate,
		})
	}
	return out
}

// window bounds where this licence's text could sit: it has to contain the
// anchor, and it cannot be longer than its own template plus what the
// variables may add. Everything outside is not worth searching.
func (e *entry) window(text string) (int, int) {
	position := strings.Index(text, e.anchor)
	if position < 0 {
		return 0, len(text)
	}
	span := e.span()
	start := position - span
	if start < 0 {
		start = 0
	}
	end := position + len(e.anchor) + span
	if end > len(text) {
		end = len(text)
	}
	return start, end
}

// span is an upper bound on how long this licence's text can be: its invariant
// words plus the most each variable may expand to. It has to be an upper
// bound, and it should be a tight one -- the cost of matching is the pattern's
// size times the text's, so a window twice as wide costs twice as much.
func (e *entry) span() int {
	e.spanOnce.Do(func() {
		total := 0
		for _, run := range invariantRuns(e.source) {
			total += len(looseNormalize(run)) + 1
		}
		for _, fields := range varPattern.FindAllStringSubmatch(e.source, -1) {
			total += maxExpansion(fields[3])
		}
		// Optional blocks are counted through invariantRuns, which excludes
		// them, so add their text back: a file that keeps every optional part
		// is the longest one that can match.
		total += optionalLength(e.source)
		e.spanValue = total
	})
	return e.spanValue
}

// maxExpansion bounds what one substitution may contribute.
func maxExpansion(match string) int {
	if location := wideRepeat.FindStringSubmatch(match); location != nil {
		if high, err := strconv.Atoi(location[2]); err == nil {
			if high > repeatLimit {
				high = repeatLimit
			}
			return high
		}
	}
	if strings.ContainsAny(match, "+*") {
		// Unbounded, so bounded the same way clampRepeats bounds a repeat.
		return repeatLimit
	}
	// An alternation of literals cannot produce more than its own source.
	return len(match)
}

// optionalLength is how much text the optional blocks could add.
func optionalLength(template string) int {
	total := 0
	rest := template
	for {
		openAt := strings.Index(rest, beginOptional)
		if openAt < 0 {
			return total
		}
		closing := closingOptional(rest, openAt)
		if closing < 0 {
			return total
		}
		body := optionalBody(rest, openAt, closing)
		total += len(looseNormalize(body)) + 1
		for _, fields := range varPattern.FindAllStringSubmatch(body, -1) {
			total += maxExpansion(fields[3])
		}
		rest = rest[closing+len(endOptional):]
	}
}

// unanchored is the expression used to locate the licence inside a larger
// file: the template from its first fixed words to its last, with the variable
// material at the edges dropped.
//
// Dropping it is not a relaxation, it is what makes locating possible. A
// template that opens with the copyright variable -- BSD-3-Clause does -- would
// otherwise start matching up to a thousand characters before the licence
// does, because an unanchored search takes the earliest start that can work.
// In a file holding two licences that swallowed the end of the first one, and
// the two matches then overlapped so only one survived.
func (e *entry) unanchored() (*regexp.Regexp, error) {
	e.looseOnce.Do(func() {
		core := invariantCore(e.source)
		if core == "" {
			e.looseErr = fmt.Errorf("template has no fixed text to locate it by")
			return
		}
		anchored, err := compileTemplate(core)
		if err != nil {
			e.looseErr = err
			return
		}
		source := strings.TrimPrefix(anchored.String(), `(?is)\A`)
		source = strings.TrimSuffix(source, `\z`)
		e.loosePattern, e.looseErr = regexp.Compile(`(?is)` + source)
	})
	return e.loosePattern, e.looseErr
}

// invariantCore is the template between its first and last fixed words. The
// segments removed are whole variables and whole optional blocks, so what
// remains is still balanced.
func invariantCore(template string) string {
	start, end := -1, -1
	rest := template
	offset := 0
	for {
		optionalAt := strings.Index(rest, beginOptional)
		variableAt := varPattern.FindStringIndex(rest)

		var literal string
		var literalStart, next int
		switch {
		case optionalAt < 0 && variableAt == nil:
			literal, literalStart, next = rest, offset, offset+len(rest)
		case variableAt != nil && (optionalAt < 0 || variableAt[0] < optionalAt):
			literal, literalStart, next = rest[:variableAt[0]], offset, offset+variableAt[1]
		default:
			closing := closingOptional(rest, optionalAt)
			if closing < 0 {
				literal, literalStart, next = rest, offset, offset+len(rest)
				break
			}
			literal, literalStart, next = rest[:optionalAt], offset, offset+closing+len(endOptional)
		}

		if strings.TrimSpace(literal) != "" {
			if start < 0 {
				start = literalStart
			}
			end = literalStart + len(literal)
		}
		if next >= offset+len(rest) {
			break
		}
		rest = template[next:]
		offset = next
	}
	if start < 0 || end <= start {
		return ""
	}
	return template[start:end]
}
