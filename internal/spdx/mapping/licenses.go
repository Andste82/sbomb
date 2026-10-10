package mapping

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/licenselist"
)

// sbombLicenseRefPrefix is the prefix of every licence reference sbomb mints
// itself, for a licence it observed by name and that the SPDX list does not
// carry. Both SPDX versions accept LicenseRef-[A-Za-z0-9.-]+, so one encoding
// serves both.
const sbombLicenseRefPrefix = "LicenseRef-sbomb-"

// sbombAdditionRefPrefix is the prefix of every exception reference sbomb
// mints, for an exception it observed after WITH that the SPDX exception list
// does not carry. AdditionRef exists only in SPDX 3.x; a version without it
// never sees one (Options.CustomAdditions).
const sbombAdditionRefPrefix = "AdditionRef-sbomb-"

// licenseTarget is a routed licence finding: the target a relationship points
// at, and the definitions the references sbomb minted for it need.
type licenseTarget struct {
	license License
	custom  []CustomLicense
	// whole says the target is one minted reference that stands for the whole
	// finding -- a name, a lone unlisted identifier, or a text that is no
	// expression -- so that a licence text retained for the file the finding
	// was read from is the text of that reference. A reference minted for one
	// operand of a compound expression is not: the file's text is the text of
	// the whole expression, not of that operand.
	whole bool
}

// routeLicense decides what an SPDX document says for one licence finding
// (section 28.11.5), in the order the specification states:
//
//   - NOASSERTION or NONE in any of the three fields is that kind, never an
//     expression -- 3.0.1 has individuals for them and 2.3 keywords, and an
//     expression "NOASSERTION" would be a licence by that name;
//   - an expression is carried with its structure. Operators written in lower
//     case are upper-cased, an identifier on the list is matched without case
//     and written as the list spells it, every LicenseRef stays as it is, a
//     "+" set apart by white space is joined to its identifier, and each
//     identifier the list does not carry becomes a LicenseRef sbomb mints for
//     that operand, so that "MIT AND Acme-Proprietary-1.0" stays an AND of two
//     licences. An exception the list does not carry becomes an AdditionRef
//     sbomb mints for it, where the version being written has that form
//     (customAdditions); an AdditionRef the build stated stays as it is there.
//     Only a text that is no expression even then -- or, in a version without
//     AdditionRef, one with an exception off the list or an AdditionRef --
//     becomes one reference for the whole text, which every version can write;
//   - without an expression, a listed SPDXID, matched without case, is the
//     licence;
//   - otherwise the name, or an SPDXID the list does not carry, becomes a
//     LicenseRef sbomb mints;
//   - an empty finding says nothing and produces no statement.
//
// The SPDXID is never preferred over an expression that is set. The pipeline
// fills SPDXID with the first identifier of the expression, so falling back
// to it would publish "MIT" for "MIT AND Acme-Proprietary-1.0" -- a different
// licence than the one observed, and one obligation fewer.
//
// customAdditions says the version being written can name a custom exception
// (Options.CustomAdditions). It is the one routing decision that differs
// between the versions, and it is made here, once, so that no renderer has to
// repeat it.
func routeLicense(finding domain.LicenseFinding, customAdditions bool) (licenseTarget, bool) {
	expression := strings.TrimSpace(finding.Expression)
	spdxID := strings.TrimSpace(finding.SPDXID)
	name := strings.TrimSpace(finding.Name)
	for _, value := range []string{expression, spdxID, name} {
		switch value {
		case "NOASSERTION":
			return licenseTarget{license: License{Kind: LicenseNoAssertion}}, true
		case "NONE":
			return licenseTarget{license: License{Kind: LicenseNone}}, true
		}
	}
	if expression != "" {
		return routeExpression(expression, customAdditions), true
	}
	if canonical, ok := licenselist.Canonical(spdxID); ok {
		// Matched without case, written as the list spells it, as an
		// identifier inside an expression is (normaliseExpression).
		return licenseTarget{license: License{Kind: LicenseExpression, Expression: canonical, ListedIDs: true}}, true
	}
	text := name
	if text == "" {
		text = spdxID
	}
	if text == "" {
		return licenseTarget{}, false
	}
	return mintWhole(text), true
}

// routeExpression carries an expression with its structure: as it is when
// every identifier is on the list or a LicenseRef (and every exception on the
// list or, where the version has them, an AdditionRef), and otherwise with
// each unlisted operand replaced by a reference minted for it.
func routeExpression(expression string, customAdditions bool) licenseTarget {
	normalised := normaliseExpression(expression)
	if listed, ok := listedExpression(normalised, customAdditions); ok {
		return licenseTarget{license: License{Kind: LicenseExpression, Expression: normalised, ListedIDs: listed}}
	}
	rewritten, custom, ok := mintUnlisted(normalised, customAdditions)
	if !ok {
		return mintWhole(expression)
	}
	if len(custom) == 1 && custom[0].Ref == rewritten {
		// A lone unlisted identifier: the reference is the whole finding.
		return licenseTarget{license: License{Kind: LicenseExpression, Expression: rewritten}, custom: custom, whole: true}
	}
	listed, _ := listedExpression(rewritten, customAdditions)
	return licenseTarget{license: License{Kind: LicenseExpression, Expression: rewritten, ListedIDs: listed}, custom: custom}
}

// mintWhole is one reference for a text that is stated as a whole.
func mintWhole(text string) licenseTarget {
	ref := sbombRef(sbombLicenseRefPrefix, text)
	return licenseTarget{
		license: License{Kind: LicenseExpression, Expression: ref},
		custom:  []CustomLicense{{Ref: ref, Name: text, Text: text}},
		whole:   true,
	}
}

// normaliseExpression writes an observed expression in the one spelling
// sbomb states. It changes three things the annexes say are not a difference
// in meaning, and nothing else:
//
//   - an operator in lower case is upper-cased. Both annexes allow an
//     operator in all lower case ("MIT or Apache-2.0"), and such lines are
//     common in real headers; sbomb writes one spelling so that one licence
//     is one expression element and not two. It is lenient beyond the grammar
//     on purpose: a mixed-case "Or" is what a header meant as well, and
//     taking it for an identifier would turn one licence choice into three
//     licences;
//   - a listed identifier in another case is written as the list spells it.
//     Annex B says identifiers "should be matched in a case-insensitive
//     manner", so "mit" is the listed MIT licence, and writing it "MIT" states
//     the same licence in the spelling every reader resolves -- where keeping
//     "mit" would leave a reader that compares exactly with an identifier it
//     cannot find (deviation D55). An identifier off the list keeps its case:
//     it becomes a reference minted for what was observed;
//   - white space before a "+" is dropped. Annex B: "There MUST NOT be white
//     space between a license-id and any following +", so "GPL-2.0 +" is not
//     an expression, and what it meant is "GPL-2.0+".
//
// An expression none of that applies to is returned as it is, byte for byte.
// A text that does not tokenize is returned as it is too; the routing then
// mints one reference for it.
func normaliseExpression(expression string) string {
	tokens, spaced, err := tokenize(expression)
	if err != nil {
		return expression
	}
	changed := false
	afterWith := false
	for index, token := range tokens {
		upper := strings.ToUpper(token)
		switch {
		case upper == "AND" || upper == "OR" || upper == "WITH":
			if token != upper {
				tokens[index] = upper
				changed = true
			}
			afterWith = upper == "WITH"
			continue
		case token == "+" && spaced[index]:
			changed = true
		case !isOperand(token) || strings.Contains(token, ":"):
		case afterWith:
			if canonical, ok := licenselist.CanonicalException(token); ok && canonical != token {
				tokens[index] = canonical
				changed = true
			}
		default:
			if canonical, ok := licenselist.Canonical(token); ok && canonical != token {
				tokens[index] = canonical
				changed = true
			}
		}
		afterWith = false
	}
	if !changed {
		return expression
	}
	return joinTokens(tokens)
}

// mintUnlisted replaces every licence identifier that is neither on the list
// nor a LicenseRef by a reference minted for it, a "+" after it included --
// a LicenseRef takes no "+", and "Acme-1.0+" is a different observation than
// "Acme-1.0". With customAdditions it replaces an exception the list does not
// carry by an AdditionRef minted for it: SPDX 3.0.1 defines
// SimpleLicensingText as "a license or addition that is not listed", and its
// customIdToUri maps "a LicenseRef or AdditionRef", so "GPL-2.0-or-later WITH
// Acme-linking-exception" keeps its listed licence machine-readable.
//
// It fails for a text that does not parse; for a licence identifier from the
// list where an exception belongs ("MIT WITH Apache-2.0"), which is a
// misreading no minted exception would state truthfully; and, without
// customAdditions, for any exception that is not on the list, an AdditionRef
// included -- that version has no custom form for an exception, and leaving
// one in would make the expression one no reader of it can resolve.
func mintUnlisted(expression string, customAdditions bool) (string, []CustomLicense, bool) {
	if _, err := ParseExpression(expression); err != nil {
		return "", nil, false
	}
	tokens, _, err := tokenize(expression)
	if err != nil {
		return "", nil, false
	}
	var out []string
	var custom []CustomLicense
	afterWith := false
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		switch {
		case token == "WITH":
			afterWith = true
		case !isOperand(token):
		case afterWith:
			afterWith = false
			switch {
			case licenselist.KnownException(token):
			case !customAdditions || licenselist.Known(token):
				return "", nil, false
			case !IsAdditionRef(token):
				token = sbombRef(sbombAdditionRefPrefix, tokens[index])
				custom = append(custom, CustomLicense{Ref: token, Name: tokens[index], Text: tokens[index], Addition: true})
			}
		case !licenselist.Known(token) && !IsLicenseRef(token):
			text := token
			if index+1 < len(tokens) && tokens[index+1] == "+" {
				text += "+"
				index++
			}
			token = sbombRef(sbombLicenseRefPrefix, text)
			custom = append(custom, CustomLicense{Ref: token, Name: text, Text: text})
		}
		out = append(out, token)
	}
	return joinTokens(out), custom, true
}

// joinTokens writes tokens back as an expression: one space between tokens,
// none inside parentheses and none before a "+".
func joinTokens(tokens []string) string {
	var builder strings.Builder
	for index, token := range tokens {
		if index > 0 && tokens[index-1] != "(" && token != ")" && token != "+" {
			builder.WriteByte(' ')
		}
		builder.WriteString(token)
	}
	return builder.String()
}

// listedExpression reports whether an expression can be carried as it is:
// well formed, every licence identifier on the list or a LicenseRef, every
// exception on the list or -- where the version has them -- an AdditionRef.
// It also says whether it names anything from the list.
func listedExpression(expression string, customAdditions bool) (listed bool, ok bool) {
	parsed, err := ParseExpression(expression)
	if err != nil {
		return false, false
	}
	for _, id := range parsed.Licenses {
		switch {
		case licenselist.Known(id):
			listed = true
		case IsLicenseRef(id):
		default:
			return false, false
		}
	}
	for _, id := range parsed.Additions {
		switch {
		case licenselist.KnownException(id):
			listed = true
		case IsAdditionRef(id) && customAdditions:
		default:
			return false, false
		}
	}
	return listed, true
}

// sbombRef mints the reference, under a prefix, for an observed licence or
// exception text. Every character outside [A-Za-z0-9.-] becomes "-", and
// whenever that changed anything a digest of the original follows, so that
// "GPL v2" and "GPL-v2" -- which a reader would call different observations --
// do not become one reference.
func sbombRef(prefix, text string) string {
	var builder strings.Builder
	changed := false
	for _, r := range text {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('-')
		changed = true
	}
	ref := prefix + builder.String()
	if changed {
		sum := sha256.Sum256([]byte(text))
		ref += "-" + hex.EncodeToString(sum[:])[:8]
	}
	return ref
}

// IsSbombLicenseRef reports whether a licence identifier is one sbomb minted,
// and therefore one the document must define.
func IsSbombLicenseRef(id string) bool { return strings.HasPrefix(id, sbombLicenseRefPrefix) }

// IsSbombAdditionRef reports whether an exception identifier is one sbomb
// minted, and therefore one the document must define.
func IsSbombAdditionRef(id string) bool { return strings.HasPrefix(id, sbombAdditionRefPrefix) }
