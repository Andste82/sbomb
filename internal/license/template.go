package license

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The SPDX license list publishes, beside each license text, a
// standardLicenseTemplate: the same text with the spans that may vary marked
// up, and a regular expression saying what each may vary into.
//
//	<<var;name="copyright";original="Copyright (c) <year> <owner>";match=".{0,5000}">>
//	<<var;name="bullet";original="1.";match=".{0,20}">> Redistributions of
//	<<beginOptional>>MIT License<<endOptional>>
//
// That makes the matching guidelines machine readable, and it is what closes
// the gap the hash table cannot: a real licence file fills in the copyright
// holder and renumbers the clause list, so it is unmistakable to a person and
// unmatchable to a digest. Measured over 142 distinct real licence files, the
// hash recognized 32 and this recognizes 56 more.
//
// This is not the similarity matching section 22.7 forbids. There is no score
// and no threshold: outside the marked spans the comparison is exact under the
// same normalization, and which spans may vary is declared by the same
// authority that publishes the text the hash table is built from. It is a
// fourth detection technique nonetheless, which section 22.3 lists
// exhaustively, so it is recorded as deviation D18.
//
//go:embed spdxtemplates.gz
var templateBlob []byte

// looseNormalize is the normal form both sides of a template match share:
// lowercase, every whitespace run collapsed to one space.
//
// It is deliberately weaker than normalizeLicenseText, which also drops
// copyright and punctuation-only lines. A template covers the copyright
// statement with a variable of its own, so removing those lines first would
// take away text the template expects to see, and dropping a line is a
// line-oriented operation that a template's variables cut across.
func looseNormalize(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

// entry is one license's compiled matcher. Pattern is built and compiled on
// demand: translating all 739 templates takes 3.5 seconds, which is far too
// much to spend on a run that never needs one.
type entry struct {
	id string
	// anchor is the longest invariant literal in the template, already in the
	// loose normal form. A candidate text that does not contain it cannot
	// match, and checking that is a substring search rather than a regular
	// expression.
	anchor string
	// deprecated marks an identifier SPDX has superseded. Emitting one in a
	// compliance document is a defect, so a current identifier that matches
	// the same text wins.
	deprecated bool
	// source is the template, kept until the pattern is needed.
	source string

	once     sync.Once
	pattern  *regexp.Regexp
	compiled error

	// The same expression without the whole-text anchors, for locating the
	// licence inside a larger file (observe.go).
	looseOnce    sync.Once
	loosePattern *regexp.Regexp
	looseErr     error

	spanOnce  sync.Once
	spanValue int
}

var (
	templatesOnce sync.Once
	templates     []*entry
	templatesErr  error
)

// loadTemplates decompresses the embedded templates the first time one is
// needed. A run whose licences all match by digest never pays for it.
func loadTemplates() ([]*entry, error) {
	templatesOnce.Do(func() {
		reader, err := gzip.NewReader(bytes.NewReader(templateBlob))
		if err != nil {
			templatesErr = fmt.Errorf("license templates: %w", err)
			return
		}
		defer reader.Close()

		// The records are NUL separated. A template is arbitrary text full of
		// newlines and backslashes -- the match expressions are regular
		// expressions -- so a line-oriented format would need escaping, and an
		// escaping bug here would corrupt a licence text silently.
		decompressed, err := io.ReadAll(reader)
		if err != nil {
			templatesErr = fmt.Errorf("license templates: %w", err)
			return
		}
		fields := bytes.Split(decompressed, []byte{0})
		for index := 0; index+2 < len(fields); index += 3 {
			id := string(fields[index])
			source := string(fields[index+2])
			if id == "" || source == "" {
				continue
			}
			templates = append(templates, &entry{
				id:         id,
				deprecated: string(fields[index+1]) == "1",
				anchor:     templateAnchor(source),
				source:     source,
			})
		}
		// Sorted, so that the order candidates are tried in -- and therefore
		// which of several matches is reported first -- does not depend on the
		// order the generator happened to write them.
		sort.Slice(templates, func(i, j int) bool { return templates[i].id < templates[j].id })
	})
	return templates, templatesErr
}

func (e *entry) regexp() (*regexp.Regexp, error) {
	e.once.Do(func() {
		e.pattern, e.compiled = compileTemplate(e.source)
	})
	return e.pattern, e.compiled
}

// matchTemplates returns the license identifiers whose template matches the
// text. More than one is possible -- several BSD variants differ only in a
// clause one of them makes optional -- and the caller must treat that as an
// ambiguity rather than pick one.
func matchTemplates(text string) ([]string, error) {
	entries, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	normalized := looseNormalize(text)
	if normalized == "" {
		return nil, nil
	}
	var matched, deprecatedMatches []string
	for _, candidate := range entries {
		if candidate.anchor != "" && !strings.Contains(normalized, candidate.anchor) {
			continue
		}
		pattern, err := candidate.regexp()
		if err != nil {
			// A template this build cannot translate is skipped, not fatal:
			// the SPDX list changes and one unusable entry must not stop the
			// other 738 from matching.
			continue
		}
		if !pattern.MatchString(normalized) {
			continue
		}
		if candidate.deprecated {
			deprecatedMatches = append(deprecatedMatches, candidate.id)
			continue
		}
		matched = append(matched, candidate.id)
	}
	// A deprecated identifier counts only when nothing current matched. GPL-2.0
	// is the superseded spelling of GPL-2.0-only and matches every text it
	// does; reporting both as an ambiguity would be reporting SPDX's own
	// renaming as a disagreement. The digest table resolves collisions the
	// same way.
	if len(matched) == 0 {
		return deprecatedMatches, nil
	}
	return matched, nil
}

var (
	// varPattern matches one substitution point. The fields are always in this
	// order in the published list.
	varPattern = regexp.MustCompile(`(?s)<<var;name="(.*?)";original="(.*?)";match="(.*?)">>`)
	// wideRepeat finds a bounded repeat beyond what RE2 accepts. RE2 caps the
	// count at 1000 and SPDX writes ".{0,5000}" throughout; without this, 334
	// of the 739 templates fail to compile.
	wideRepeat = regexp.MustCompile(`\{(\d+),(\d+)\}`)
)

const (
	beginOptional = "<<beginOptional"
	endOptional   = "<<endOptional>>"
	// minAnchor is how long an invariant literal has to be before it is
	// trusted as a prefilter. Short ones appear in every licence and filter
	// nothing.
	minAnchor = 24
)

// compileTemplate translates one template into a single anchored expression
// over the loose normal form.
func compileTemplate(template string) (*regexp.Regexp, error) {
	var builder strings.Builder
	// Case-insensitive because the text is lowercased and the match
	// expressions are not; dot-all is irrelevant once newlines are gone, but
	// costs nothing and guards a template that expects one.
	builder.WriteString(`(?is)\A`)
	if err := writeSegments(&builder, template); err != nil {
		return nil, err
	}
	builder.WriteString(`\z`)
	return regexp.Compile(builder.String())
}

// writeSegments walks a template, emitting literals, variables and optional
// blocks. Optional blocks nest -- 46 of the 739 templates do it, three deep at
// the most -- so the recursion is unbounded in depth and the closing marker is
// found by counting rather than by taking the first one.
func writeSegments(builder *strings.Builder, template string) error {
	rest := template
	for {
		optionalAt := strings.Index(rest, beginOptional)
		variableAt := varPattern.FindStringIndex(rest)

		switch {
		case optionalAt < 0 && variableAt == nil:
			builder.WriteString(literalPattern(rest))
			return nil

		case variableAt != nil && (optionalAt < 0 || variableAt[0] < optionalAt):
			builder.WriteString(literalPattern(rest[:variableAt[0]]))
			fields := varPattern.FindStringSubmatch(rest[variableAt[0]:variableAt[1]])
			builder.WriteString("(?:" + clampRepeats(fields[3]) + ")")
			rest = rest[variableAt[1]:]

		default:
			builder.WriteString(literalPattern(rest[:optionalAt]))
			closing := closingOptional(rest, optionalAt)
			if closing < 0 {
				return fmt.Errorf("unterminated optional block")
			}
			builder.WriteString("(?:")
			if err := writeSegments(builder, optionalBody(rest, optionalAt, closing)); err != nil {
				return err
			}
			builder.WriteString(")?")
			rest = rest[closing+len(endOptional):]
		}
	}
}

// closingOptional returns the index of the marker that closes the block
// opening at openAt, or -1. Taking the first <<endOptional>> instead closes an
// outer block on an inner one's marker, which silently truncates it: the
// appendix of GPL-2.0 and of Apache-2.0 both contain nested blocks, and both
// licences failed to match a real file until this counted.
func closingOptional(template string, openAt int) int {
	depth := 0
	index := openAt
	for index < len(template) {
		nextOpen := strings.Index(template[index:], beginOptional)
		nextClose := strings.Index(template[index:], endOptional)
		if nextClose < 0 {
			return -1
		}
		if nextOpen >= 0 && nextOpen < nextClose {
			depth++
			index += nextOpen + len(beginOptional)
			continue
		}
		depth--
		if depth == 0 {
			return index + nextClose
		}
		index += nextClose + len(endOptional)
	}
	return -1
}

// optionalBody is what sits between an optional block's markers.
func optionalBody(template string, openAt, closing int) string {
	body := template[openAt:closing]
	if marker := strings.Index(body, ">>"); marker >= 0 {
		return body[marker+2:]
	}
	return body
}

// repeatLimit is RE2's maximum repeat count.
const repeatLimit = 1000

// clampRepeats brings a bounded repeat within what RE2 accepts.
//
// The bound must be kept rather than dropped. Replacing ".{0,5000}" with an
// unbounded repeat lets one variable swallow arbitrary text, and a two-clause
// licence template then matches a file containing three clauses and a second
// licence after it -- which is exactly what it did before this clamped instead.
func clampRepeats(pattern string) string {
	return wideRepeat.ReplaceAllStringFunc(pattern, func(match string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}")
		lowText, highText, found := strings.Cut(inner, ",")
		if !found {
			return match
		}
		low, lowErr := strconv.Atoi(lowText)
		high, highErr := strconv.Atoi(highText)
		if lowErr != nil || highErr != nil {
			return match
		}
		if high <= repeatLimit {
			return match
		}
		if low > repeatLimit {
			low = repeatLimit
		}
		return fmt.Sprintf("{%d,%d}", low, repeatLimit)
	})
}

// literalPattern turns invariant text into a pattern that tolerates the
// whitespace a template and a real file disagree about. SPDX puts a space
// around every substitution point whether the sentence has one or not, so the
// words are joined with \s* rather than compared as one string.
func literalPattern(literal string) string {
	words := strings.Fields(strings.ToLower(literal))
	if len(words) == 0 {
		return `\s*`
	}
	quoted := make([]string, len(words))
	for index, word := range words {
		quoted[index] = regexp.QuoteMeta(word)
	}
	return `\s*` + strings.Join(quoted, `\s*`) + `\s*`
}

// templateAnchor is the longest run of invariant text in a template, in the
// loose normal form. It is the prefilter: a candidate that does not contain it
// cannot match, and finding that out costs a substring search instead of a
// regular expression over the whole licence.
func templateAnchor(template string) string {
	longest := ""
	for _, run := range invariantRuns(template) {
		normalized := looseNormalize(run)
		if len(normalized) > len(longest) {
			longest = normalized
		}
	}
	if len(longest) < minAnchor {
		return ""
	}
	return longest
}

// invariantRuns is the text between the markup: everything a matching file
// must contain literally. Optional blocks are excluded, because a file that
// omits one still matches.
func invariantRuns(template string) []string {
	var runs []string
	rest := template
	for {
		optionalAt := strings.Index(rest, beginOptional)
		variableAt := varPattern.FindStringIndex(rest)
		switch {
		case optionalAt < 0 && variableAt == nil:
			runs = append(runs, rest)
			return runs
		case variableAt != nil && (optionalAt < 0 || variableAt[0] < optionalAt):
			runs = append(runs, rest[:variableAt[0]])
			rest = rest[variableAt[1]:]
		default:
			runs = append(runs, rest[:optionalAt])
			closing := closingOptional(rest, optionalAt)
			if closing < 0 {
				return runs
			}
			rest = rest[closing+len(endOptional):]
		}
	}
}

// TemplateIDs is the identifiers the embedded template table covers, for the
// generator's check mode and for tests.
func TemplateIDs() ([]string, error) {
	entries, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.id)
	}
	return ids, nil
}

// MatchTemplate exposes the matcher for tools and tests. It returns every
// identifier whose template matches.
func MatchTemplate(text string) ([]string, error) { return matchTemplates(text) }
