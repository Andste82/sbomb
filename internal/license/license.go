package license

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

const (
	ReasonNoEvidence              = "no-evidence"
	ReasonLicenseTextUnrecognized = "license-text-unrecognized"
	ReasonConflictingEvidence     = "conflicting-evidence"
	ReasonComponentUnresolved     = "component-unresolved"
	ReasonScannerInconclusive     = "scanner-inconclusive"
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

func ResolveFromText(text, source string) domain.LicenseFinding {
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
	if id, ok := lookupNormalizedHash(normalizeLicenseText(text)); ok {
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
	if matches, err := matchTemplates(text); err == nil {
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

func collapseSpaces(s string) string {
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
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
