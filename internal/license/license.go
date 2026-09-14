package license

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/example/sbomb/internal/domain"
)

const (
	ReasonNoEvidence              = "no-evidence"
	ReasonLicenseTextUnrecognized = "license-text-unrecognized"
	ReasonConflictingEvidence     = "conflicting-evidence"
	ReasonComponentUnresolved     = "component-unresolved"
	ReasonScannerInconclusive     = "scanner-inconclusive"
	// ReasonLicenseCompositionUnresolved reports a file that contains one or
	// more complete licence texts without being one of them: two licences one
	// after the other, or a licence with material around it. Which licences
	// are present is recorded as evidence; how they compose is stated in the
	// prose between them and is not machine-decidable (deviation D19).
	ReasonLicenseCompositionUnresolved = "license-composition-unresolved"
)

// The detection techniques of section 22.3, recorded so that a reviewer can
// tell how a licence was determined rather than only what it was.
const (
	TechniqueIdentifier = "spdx-identifier"
	TechniqueDigest     = "spdx-digest"
	TechniqueTemplate   = "spdx-template"
)

// spdxExprPattern reads one identifier line. The character class deliberately
// admits only spaces and tabs, not \s: an expression is a single line, and a
// class containing the newline ran the match on into whatever followed --
// "SPDX-License-Identifier: MIT" above a copyright statement yielded
// "MIT\nCopyright (c) 2009" as the expression.
var spdxExprPattern = regexp.MustCompile(`(?i)SPDX-License-Identifier[ \t]*:[ \t]*([A-Za-z0-9.\-+()/ \t]+)`)

// Text is one file's bytes together with the normal forms the techniques of
// section 22.3 compare against. Both forms are derived from those bytes alone
// and no technique changes them, so a caller that asks two questions of one
// file -- what licence is this, and which licence texts are in it -- pays for
// each form once instead of once per question.
//
// That pair of questions is how a file is actually examined: the resolution
// order asks for the observation only when identification found nothing, so
// the loose form was computed for the templates and then computed again, byte
// for byte the same, for the observation. On a profile of the licence path
// that second pass was part of the 38% looseNormalize cost, and the strict
// form the digest table is keyed by another 22%.
//
// The forms are computed on first use rather than up front, because most files
// need neither: a source file that carries SPDX-License-Identifier is answered
// by technique 1 before either normalization is reached. A Text holds no state
// beyond what its bytes determine -- two of them over the same bytes answer
// identically -- and like any other value it belongs to one goroutine at a
// time.
type Text struct {
	raw string

	looseComputed bool
	looseForm     string

	strictComputed bool
	strictForm     string
}

// Prepare wraps a text so that the techniques applied to it share its normal
// forms. It is the entry point for a caller that asks more than one question
// of the same bytes; ResolveFromText and ObserveFindings are the
// single-question spellings and go through it too.
func Prepare(text string) *Text { return &Text{raw: text} }

// loose is the normal form a template is matched against (template.go): every
// whitespace run collapsed to one space, lowercase, nothing removed.
func (t *Text) loose() string {
	if !t.looseComputed {
		t.looseForm = looseNormalize(t.raw)
		t.looseComputed = true
	}
	return t.looseForm
}

// strict is the normal form the embedded digest table is keyed by: the loose
// one with copyright statements and punctuation-only lines dropped as well.
func (t *Text) strict() string {
	if !t.strictComputed {
		t.strictForm = normalizeLicenseText(t.raw)
		t.strictComputed = true
	}
	return t.strictForm
}

// ResolveFromText applies the techniques of section 22.3 to a text that is
// examined once. A caller that also observes the same text prepares it first,
// so that the two share the normal forms.
func ResolveFromText(text, source string) domain.LicenseFinding {
	return Prepare(text).Resolve(source)
}

// Resolve says which licence this text declares or is, in the order of section
// 22.3: the identifier the text states about itself, the digest of its
// normalized text, then the SPDX templates.
func (t *Text) Resolve(source string) domain.LicenseFinding {
	text := t.raw
	if strings.TrimSpace(text) == "" {
		return domain.LicenseFinding{
			Name:       "NOASSERTION",
			Evidence:   "unknown",
			Confidence: domain.ConfidenceUnknown,
			Source:     source,
			Reason:     ReasonNoEvidence,
		}
	}
	if expression, ok := extractSPDXExpression(text); ok {
		return domain.LicenseFinding{
			Expression: expression,
			SPDXID:     firstSPDXID(expression),
			Name:       expression,
			Evidence:   "file-level",
			Confidence: domain.ConfidenceHigh,
			Source:     source,
			Technique:  TechniqueIdentifier,
		}
	}
	if id, ok := lookupNormalizedHash(t.strict()); ok {
		return domain.LicenseFinding{
			Expression: id,
			SPDXID:     id,
			Name:       id,
			Evidence:   "component-level",
			Confidence: domain.ConfidenceHigh,
			Source:     source,
			Technique:  TechniqueDigest,
		}
	}

	// Technique 4 (deviation D18): the SPDX template, which declares which
	// spans of the text may vary and what they may vary into. It runs only
	// after the digest has missed, because that is the case it exists for: a
	// licence whose copyright holder has been filled in or whose clause list
	// has been renumbered is unmatchable by digest and unmistakable here.
	if matches, err := matchTemplates(t.loose()); err == nil {
		switch {
		case len(matches) == 1:
			return domain.LicenseFinding{
				Expression: matches[0],
				SPDXID:     matches[0],
				Name:       matches[0],
				Evidence:   "component-level",
				Confidence: domain.ConfidenceHigh,
				Source:     source,
				Technique:  TechniqueTemplate,
			}
		case len(matches) > 1:
			// Several licenses can be templates of one another -- a variant
			// that makes a clause optional matches every text the stricter one
			// does. Picking one would be the guessing section 22.7 forbids, so
			// the ambiguity is reported with the candidates named.
			return domain.LicenseFinding{
				Name:       "NOASSERTION",
				Evidence:   "unknown",
				Confidence: domain.ConfidenceUnknown,
				Source:     source,
				Reason:     ReasonConflictingEvidence,
				Conflicts:  matches,
				Technique:  TechniqueTemplate,
			}
		}
	}

	return domain.LicenseFinding{
		Name:       "NOASSERTION",
		Evidence:   "unknown",
		Confidence: domain.ConfidenceUnknown,
		Source:     source,
		Reason:     ReasonLicenseTextUnrecognized,
	}
}

func ResolveFile(path string) (domain.LicenseFinding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.LicenseFinding{}, err
	}
	return ResolveFromText(string(data), filepath.Clean(path)), nil
}

func ResolveConflict(curated, conflicting, source string) (domain.LicenseFinding, bool) {
	curated = strings.TrimSpace(curated)
	conflicting = strings.TrimSpace(conflicting)
	if curated == "" {
		return domain.LicenseFinding{Expression: conflicting, Name: conflicting, Evidence: "unknown", Confidence: domain.ConfidenceUnknown, Source: source}, false
	}
	if conflicting == "" || sameLicense(curated, conflicting) {
		return domain.LicenseFinding{Expression: curated, Name: curated, Evidence: "component-level", Confidence: domain.ConfidenceHigh, Source: source}, false
	}
	return domain.LicenseFinding{
		Expression: curated,
		SPDXID:     firstSPDXID(curated),
		Name:       curated,
		Evidence:   "component-level",
		Confidence: domain.ConfidenceHigh,
		Source:     source,
		Reason:     ReasonConflictingEvidence,
		Conflicts:  []string{conflicting},
	}, true
}

func extractSPDXExpression(text string) (string, bool) {
	// The expression cannot be there if the tag is not, and the tag is a fixed
	// string: a byte scan settles it for the overwhelming majority of files,
	// which are source files that declare nothing. Running the regular
	// expression over every one of them instead was 36% of a measured run --
	// the same reason ExtractCopyright looks for its keyword first.
	if !containsFold(text, "spdx-license-identifier") {
		return "", false
	}
	matches := spdxExprPattern.FindStringSubmatch(text)
	if len(matches) != 2 {
		return "", false
	}
	value := strings.TrimSpace(matches[1])
	if value == "" {
		return "", false
	}
	return normalizeSPDXExpression(value), true
}

func normalizeSPDXExpression(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, ";")
	value = strings.TrimSuffix(value, ".")
	return strings.TrimSpace(value)
}

func firstSPDXID(expression string) string {
	parts := strings.FieldsFunc(expression, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '(' || r == ')' || r == '|' || r == '&' || r == '+' || r == ','
	})
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func sameLicense(a, b string) bool {
	return strings.EqualFold(normalizeSPDXExpression(a), normalizeSPDXExpression(b))
}

// NormalizeText applies the normalization of specification section 22.3
// technique 3: lowercase, whitespace collapsed, copyright lines and
// punctuation-only lines removed. tools/spdxgen must use exactly this
// function, or the embedded hashes would never match a real license file.
func NormalizeText(text string) string { return normalizeLicenseText(text) }

func normalizeLicenseText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isCopyrightNotice(trimmed) {
			continue
		}
		if isPunctuationOnly(trimmed) {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	joined := strings.Join(filtered, " ")
	joined = strings.ToLower(joined)
	joined = strings.ReplaceAll(joined, "\t", " ")
	return collapseSpaces(joined)
}

// collapseSpaces reduces every run of spaces to one. It reads the string once
// and copies it at most once: replacing "  " with " " until none is left was a
// fresh allocation and a fresh scan of the whole text per pass, and a licence
// laid out in a column of double-spaced sentences pays that many times over.
// The single pass is the same function -- a run of n spaces becomes one space
// either way.
func collapseSpaces(s string) string {
	doubled := strings.Index(s, "  ")
	if doubled < 0 {
		// Nothing to collapse, which is the common case once the lines have
		// been trimmed and joined, so nothing is copied either.
		return strings.TrimSpace(s)
	}
	out := make([]byte, 0, len(s))
	// Everything up to and including the first space of that run is kept as
	// it stands; from there each space is dropped when the byte before it in
	// the original was a space too.
	out = append(out, s[:doubled+1]...)
	for i := doubled + 1; i < len(s); i++ {
		if s[i] == ' ' && s[i-1] == ' ' {
			continue
		}
		out = append(out, s[i])
	}
	return strings.TrimSpace(string(out))
}

// copyrightNotice matches a copyright *statement*, which the SPDX matching
// guidelines ignore. It deliberately does not match prose that merely begins
// with the word: the Apache-2.0 text wraps "copyright notice that is included
// in or attached to the work" onto its own line, and dropping it mutilates the
// licence. A statement is recognized by what follows the keyword -- a year, a
// (c) or an unfilled SPDX placeholder -- so the result no longer depends on
// where the source happened to wrap its lines.
var copyrightNotice = regexp.MustCompile(`(?i)^(copyright|\(c\)|©)\b[^a-z0-9]*((\(c\)|©|\d{4}|\[[^\]]*\]|<[^>]*>)|$)`)

func isCopyrightNotice(line string) bool {
	return copyrightNotice.MatchString(line)
}

func isPunctuationOnly(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func lookupNormalizedHash(normalized string) (string, bool) {
	h := sha256.Sum256([]byte(normalized))
	key := hex.EncodeToString(h[:])
	id, ok := knownLicenseHashes[key]
	return id, ok
}

// KnownIdentifiers is every SPDX license identifier this build carries a
// record of: the digests of section 22.3 technique 2 and the templates of
// technique 4. It exists so that a list of identifiers written by hand
// somewhere else can be checked against the table rather than against a
// reviewer's memory -- a typo in an identifier is otherwise a silent
// classification failure.
func KnownIdentifiers() map[string]bool {
	ids := make(map[string]bool, len(knownLicenseHashes))
	for _, id := range knownLicenseHashes {
		ids[id] = true
	}
	// A template failure is not fatal here: the digest table alone is a real
	// answer, and every caller asks "is this identifier known", never "how
	// many are there".
	if templates, err := TemplateIDs(); err == nil {
		for _, id := range templates {
			ids[id] = true
		}
	}
	return ids
}

// spdxIdentifiers is every identifier the embedded digest table names, which
// is the SPDX licence list as of the version spdxhashes.go records. It is the
// value side of that map rather than a second table: one generated file, one
// source of truth.
// Both embedded tables are read, because neither is the whole list on its own:
// the digest table is keyed by normalized text and 739 licences share 698 of
// them, so the identifiers that differ only in a clause the normalization
// removes -- `GPL-2.0-only` and `GPL-2.0-or-later` -- are not both in it. The
// templates carry one entry per identifier and fill that gap. Decompressing
// them is what technique 4 already pays for, and this pays it once.
var spdxIdentifiers = sync.OnceValue(func() []string {
	seen := map[string]bool{}
	ids := make([]string, 0, len(knownLicenseHashes))
	add := func(id string) {
		if upper := strings.ToUpper(id); upper != "" && !seen[upper] {
			seen[upper] = true
			ids = append(ids, upper)
		}
	}
	for _, id := range knownLicenseHashes {
		add(id)
	}
	if fromTemplates, err := TemplateIDs(); err == nil {
		for _, id := range fromTemplates {
			add(id)
		}
	}
	return ids
})

// NamesALicence reports whether a string is an SPDX identifier or the start of
// one, compared without case.
//
// It answers one question, for the `LICENSE-<id>` form of section 22.3: is
// what follows the dash a licence, or something else that happens to begin
// with the same eight characters. `MIT` is an identifier; `APACHE` is how a
// project spells `LICENSE-APACHE` beside `LICENSE-MIT`, and it opens
// `Apache-2.0`; `HEADER`, as in the `license-header.txt` a licence-header
// checker ships, opens nothing at all.
//
// A prefix is enough on purpose. Requiring the whole identifier would reject
// `LICENSE-APACHE`, which is the commonest spelling of the case the form
// exists for, and the question here is only whether a file name names a
// licence -- what licence it stated is read from its bytes.
func NamesALicence(candidate string) bool {
	candidate = strings.ToUpper(strings.TrimSpace(candidate))
	if candidate == "" {
		return false
	}
	for _, id := range spdxIdentifiers() {
		if strings.HasPrefix(id, candidate) {
			return true
		}
	}
	return false
}

// DeclaredIdentifier is technique 1 of section 22.3 alone: the SPDX expression
// a file states about itself, or the empty string.
//
// It exists beside ResolveFromText so that a caller which only wants what a
// file *declared* does not pay for the three techniques that follow -- the
// digest, the templates and the normalized comparison are about recognizing a
// licence *text*, and a source file is not one.
func DeclaredIdentifier(data []byte) string {
	if expression, ok := extractSPDXExpression(string(data)); ok {
		return expression
	}
	return ""
}

// IdentifiersIn is every SPDX identifier an expression names, so that a caller
// can ask whether an expression accounts for one. The expression grammar's
// operators and parentheses are separators here and nothing else: this answers
// "is this licence named in there", not "what does the expression mean".
func IdentifiersIn(expression string) []string {
	fields := strings.FieldsFunc(expression, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '(' || r == ')'
	})
	out := make([]string, 0, len(fields))
	var afterWith bool
	for _, field := range fields {
		operator := strings.ToUpper(field)
		if afterWith {
			// The operand of WITH is an exception rather than a licence: it
			// qualifies the licence it follows instead of being one more
			// thing the component is under.
			afterWith = false
			continue
		}
		switch operator {
		case "AND", "OR", "":
			continue
		case "WITH":
			afterWith = true
			continue
		}
		out = append(out, field)
	}
	return out
}
