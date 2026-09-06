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

var spdxExprPattern = regexp.MustCompile(`(?i)SPDX-License-Identifier\s*:\s*([A-Za-z0-9\.\-+()\s/]+)`)

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
