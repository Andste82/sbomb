package spdx23

import (
	"slices"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/spdx/mapping"
)

// licenseString is one licence target as 2.3 writes it: the expression, or
// one of the two keywords.
func licenseString(license mapping.License) string {
	switch license.Kind {
	case mapping.LicenseNoAssertion:
		return noAssertion
	case mapping.LicenseNone:
		return none
	default:
		return license.Expression
	}
}

// licenseField is a list of licence statements as the one expression a 2.3
// licence field holds (deviation D57). SPDX 3.0.1 states each licence as a
// relationship of its own; 2.3 has one value per field, so the distinct
// expressions are joined with AND -- every one of them was found to apply --
// each compound expression in parentheses so that the join does not change
// how it binds. NOASSERTION and NONE stand only where nothing else was
// stated, NOASSERTION over NONE: "nothing could be established" is the weaker
// claim. Nothing stated is the empty string, and the caller decides whether
// the field is then left out or unasserted.
func licenseField(uses []mapping.LicenseUse) string {
	var expressions []string
	noAssertionSeen, noneSeen := false, false
	for _, use := range uses {
		switch use.License.Kind {
		case mapping.LicenseNoAssertion:
			noAssertionSeen = true
		case mapping.LicenseNone:
			noneSeen = true
		default:
			if !slices.Contains(expressions, use.License.Expression) {
				expressions = append(expressions, use.License.Expression)
			}
		}
	}
	switch {
	case len(expressions) == 0 && noAssertionSeen:
		return noAssertion
	case len(expressions) == 0 && noneSeen:
		return none
	case len(expressions) == 1:
		return expressions[0]
	}
	sort.Strings(expressions)
	for index, expression := range expressions {
		if strings.ContainsAny(expression, " ") {
			expressions[index] = "(" + expression + ")"
		}
	}
	return strings.Join(expressions, " AND ")
}

// licenseList is the licences found in a file (licenseInfoInFiles), one entry
// per distinct statement, sorted. 2.3 allows an expression in each entry, so
// none is split into its identifiers and nothing about a choice or an
// exception is lost. Clause 8.6.1 suggests an entry per option of a choice;
// keeping the choice whole departs from that SHOULD on purpose (deviation
// D57).
func licenseList(uses []mapping.LicenseUse) []string {
	var out []string
	for _, use := range uses {
		value := licenseString(use.License)
		if !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

// Comments on a licence sbomb defines (hasExtractedLicensingInfos) whose
// extractedText is not a licence text. A licence text is never invented: the
// text is the name that was observed, and the comment says so.
const (
	noTextComment      = "sbomb observed only this licence name; no licence text was retained (licenseTextInSBOM is off, or no licence file was read)"
	nonUTF8NameComment = "sbomb observed this licence name; the licence text was retained but is not UTF-8, and SPDX 2.3 cannot carry it unaltered; it is in the FOSS outputs, read from the file "
	statedRefComment   = "the build stated this licence reference; sbomb has no text for it beyond its identifier"
	additionComment    = "sbomb observed only this licence exception name, after WITH; the SPDX exception list does not carry it and no text of it was retained"
)

// extractedLicenseOf defines one LicenseRef: its identifier, the name that was
// observed and the retained text, or the name with a comment when no text was
// retained (deviation D57).
func extractedLicenseOf(custom mapping.CustomLicense) extractedLicense {
	out := extractedLicense{LicenseID: custom.Ref, Name: custom.Name, ExtractedText: custom.Text}
	if out.ExtractedText == "" {
		out.ExtractedText = custom.Ref
	}
	switch {
	case custom.TextRetained:
	case custom.TextFile != "":
		out.Comment = nonUTF8NameComment + custom.TextFile
	case custom.Addition:
		out.Comment = additionComment
	case !mapping.IsSbombLicenseRef(custom.Ref):
		out.Comment = statedRefComment
	default:
		out.Comment = noTextComment
	}
	return out
}
