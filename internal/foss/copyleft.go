package foss

import (
	"sort"
	"strings"
)

// The obligation names, spelled once. They are names and not prose: the
// property `sbomb:component:sourceObligation` carries them, and a consumer
// that groups components by obligation needs a token rather than a sentence.
const (
	// ObligationCorrespondingSource is the complete corresponding source of
	// the whole work -- GPL-2.0 section 3, GPL-3.0 section 6.
	ObligationCorrespondingSource = "corresponding-source"
	// ObligationLibrarySource is the source of the library alone, which is
	// what the LGPL asks for.
	ObligationLibrarySource = "library-source"
	// ObligationCoveredFileSource is the source of the files the licence
	// covers -- MPL-2.0 section 3.2, EPL-2.0 section 3.1, CDDL.
	ObligationCoveredFileSource = "covered-file-source"
	// ObligationInstallationInformation is GPL-3.0 section 6 for a consumer
	// device: the source alone does not discharge it.
	ObligationInstallationInformation = "installation-information"
	// ObligationNetworkSource is the AGPL section 13 case, and the CPAL and
	// RPL equivalents: offering the work over a network triggers the offer.
	ObligationNetworkSource = "network-source"
	// ObligationRelinking is the one conditional entry in this file: static
	// linkage against an LGPL library additionally requires the material to
	// relink the application against a modified library.
	ObligationRelinking = "relinking"
)

// classified is the committed list: an SPDX identifier and the obligation
// names it triggers. An entry with an empty value is classified and triggers
// nothing; an identifier that is not a key at all is **unclassified** and
// reported as such (FOSS_LICENSE_UNCLASSIFIED) rather than assumed permissive.
//
// It is deliberately small and hand-written (decision Q19). Importing the
// ScanCode LicenseDB would cover every exotic licence and would put two
// thousand classifications nobody at the manufacturer has read into a document
// that carries the manufacturer's name. These entries can be read and
// defended, and a test asserts that every one of them is an identifier the
// SPDX table of section 22.3 already knows.
//
// It is not a rules engine. The single condition anywhere in it is static
// linkage against an LGPL library, and that condition is written out in
// Assess rather than expressed here as data.
var classified = map[string][]string{
	// --- The strong copyleft family: the source of the whole work. ---
	"GPL-1.0-only":     {ObligationCorrespondingSource},
	"GPL-1.0-or-later": {ObligationCorrespondingSource},
	"GPL-2.0-only":     {ObligationCorrespondingSource},
	"GPL-2.0-or-later": {ObligationCorrespondingSource},
	"GPL-3.0-only":     {ObligationCorrespondingSource, ObligationInstallationInformation},
	"GPL-3.0-or-later": {ObligationCorrespondingSource, ObligationInstallationInformation},
	"AGPL-3.0-only":    {ObligationCorrespondingSource, ObligationInstallationInformation, ObligationNetworkSource},
	"AGPL-3.0-or-later": {ObligationCorrespondingSource, ObligationInstallationInformation,
		ObligationNetworkSource},
	"CECILL-2.1": {ObligationCorrespondingSource},
	"EUPL-1.1":   {ObligationCorrespondingSource},
	"EUPL-1.2":   {ObligationCorrespondingSource},
	"Sleepycat":  {ObligationCorrespondingSource},

	// --- The LGPL family: the library's own source, plus relinking material
	// when the linkage was static. ---
	"LGPL-2.0-only":     {ObligationLibrarySource},
	"LGPL-2.0-or-later": {ObligationLibrarySource},
	"LGPL-2.1-only":     {ObligationLibrarySource},
	"LGPL-2.1-or-later": {ObligationLibrarySource},
	"LGPL-3.0-only":     {ObligationLibrarySource},
	"LGPL-3.0-or-later": {ObligationLibrarySource},

	// --- File-scoped copyleft: the covered files, not the whole work. ---
	"MPL-1.0":                       {ObligationCoveredFileSource},
	"MPL-1.1":                       {ObligationCoveredFileSource},
	"MPL-2.0":                       {ObligationCoveredFileSource},
	"MPL-2.0-no-copyleft-exception": {ObligationCoveredFileSource},
	"EPL-1.0":                       {ObligationCoveredFileSource},
	"EPL-2.0":                       {ObligationCoveredFileSource},
	"CPL-1.0":                       {ObligationCoveredFileSource},
	"CDDL-1.0":                      {ObligationCoveredFileSource},
	"CDDL-1.1":                      {ObligationCoveredFileSource},
	"CECILL-C":                      {ObligationCoveredFileSource},
	"OSL-3.0":                       {ObligationCoveredFileSource},
	"QPL-1.0":                       {ObligationCoveredFileSource},
	"APSL-2.0":                      {ObligationCoveredFileSource},
	"MS-RL":                         {ObligationCoveredFileSource},
	"CPAL-1.0":                      {ObligationCoveredFileSource, ObligationNetworkSource},
	"RPL-1.5":                       {ObligationCoveredFileSource, ObligationNetworkSource},

	// --- The deprecated short forms of the same licences. Detection reads
	// identifiers out of files people wrote, and "GPL-2.0" is still what a
	// great many headers say. Leaving them out would classify a GPL component
	// as unclassified on a spelling. ---
	"GPL-1.0":  {ObligationCorrespondingSource},
	"GPL-2.0":  {ObligationCorrespondingSource},
	"GPL-3.0":  {ObligationCorrespondingSource, ObligationInstallationInformation},
	"AGPL-3.0": {ObligationCorrespondingSource, ObligationInstallationInformation, ObligationNetworkSource},
	"LGPL-2.0": {ObligationLibrarySource},
	"LGPL-2.1": {ObligationLibrarySource},
	"LGPL-3.0": {ObligationLibrarySource},

	// --- Classified, and triggering nothing. Present so that the common
	// permissive licences do not each produce FOSS_LICENSE_UNCLASSIFIED:
	// "on no list" has to mean something. Their attribution obligations are
	// discharged by THIRD-PARTY-NOTICES.txt; none of them asks for source. ---
	"0BSD":                 nil,
	"Apache-2.0":           nil,
	"BSD-1-Clause":         nil,
	"BSD-2-Clause":         nil,
	"BSD-3-Clause":         nil,
	"BSD-3-Clause-Clear":   nil,
	"BSL-1.0":              nil,
	"CC0-1.0":              nil,
	"IJG":                  nil,
	"ISC":                  nil,
	"Libpng":               nil,
	"MIT":                  nil,
	"MIT-0":                nil,
	"MS-PL":                nil,
	"NCSA":                 nil,
	"PostgreSQL":           nil,
	"Python-2.0":           nil,
	"Unlicense":            nil,
	"Zlib":                 nil,
	"zlib-acknowledgement": nil,
	// CC-BY-4.0 requires indicating whether changes were made, and OFL-1.1
	// reserves font names. Neither is a source obligation, and neither is
	// something sbomb can check -- it answers modification per component
	// rather than per embedded blob, and it cannot read a font's name at all.
	// Both limits are stated in the documentation rather than left to be
	// discovered (decision Q18).
	"CC-BY-4.0": nil,
	"OFL-1.1":   nil,
}

// obligationProse is one sentence per obligation name. The sentences state the
// obligation and its source; they state no legal conclusion, and none of them
// says whether it was discharged.
var obligationProse = map[string]string{
	ObligationCorrespondingSource: "The complete corresponding source of the work is owed, together with the " +
		"scripts used to control its compilation and installation; a written offer must stay valid for at least three years.",
	ObligationLibrarySource: "A source offer for the library, including any modification to it, is required.",
	ObligationCoveredFileSource: "The source of the files this licence covers is owed, on the same medium as the " +
		"binary or through a mechanism the distribution states.",
	ObligationInstallationInformation: "For a consumer device the source alone does not discharge the obligation: " +
		"Installation Information must accompany it (GPL-3.0 section 6).",
	ObligationNetworkSource: "Offering the work to users over a network triggers the offer to those users as well " +
		"(AGPL-3.0 section 13 and the equivalents in CPAL and RPL).",
	ObligationRelinking: "Static linkage additionally implicates the relinking provision of the LGPL " +
		"(section 6 of LGPL-2.0 and LGPL-2.1, section 4 of LGPL-3.0): the material needed to relink the " +
		"application against a modified library is part of what is owed.",
}

// Assessment is what the committed list says about one component's licence. It
// is an assessment of the *licence*, never of the component's compliance:
// nothing here reports whether an obligation was met, because sbomb cannot
// observe that.
type Assessment struct {
	// Expression is the licence expression it was derived from.
	Expression string
	// Identifiers are the licence identifiers the expression names, in the
	// order they appear, without duplicates. Exception identifiers after WITH
	// are not among them.
	Identifiers []string
	// Obligations are the obligation names the licence triggers, sorted and
	// deduplicated. Empty means the list classified every identifier and none
	// of them asks for source material.
	Obligations []string
	// Unclassified are the identifiers on neither list. They are reported
	// (FOSS_LICENSE_UNCLASSIFIED) and never assumed permissive, so that a gap
	// in the list is visible rather than silent.
	Unclassified []string
}

// Owed reports whether the licence triggers any source obligation at all,
// which is the criterion source-obligations.txt lists a component by.
func (a Assessment) Owed() bool { return len(a.Obligations) > 0 }

// Prose is the sentences for the obligations, in the fixed order of
// obligationOrder so that two runs read the same.
func (a Assessment) Prose() []string {
	out := make([]string, 0, len(a.Obligations))
	for _, name := range obligationOrder {
		for _, owed := range a.Obligations {
			if owed == name {
				out = append(out, obligationProse[name])
			}
		}
	}
	return out
}

// obligationOrder is reading order rather than alphabetical: what is owed
// first, then what a condition adds to it.
var obligationOrder = []string{
	ObligationCorrespondingSource,
	ObligationLibrarySource,
	ObligationCoveredFileSource,
	ObligationRelinking,
	ObligationInstallationInformation,
	ObligationNetworkSource,
}

// staticLinkage is the one condition in this file: an LGPL library the linker
// pulled object code out of, rather than one the artifact loads at run time.
func staticLinkage(linkageForms []string) bool {
	for _, form := range linkageForms {
		if form == "static-archive-member" || form == "static-object" {
			return true
		}
	}
	return false
}

// Assess classifies a licence expression against the committed list.
//
// An expression rather than an identifier, because that is what a component
// carries: "MIT OR Apache-2.0" is one licence field naming two licences. Every
// identifier in it is classified, and the obligations are the union -- a
// disjunction is a choice the manufacturer makes and not one the tool makes,
// so an "OR" that contains a copyleft identifier still reports it.
func Assess(expression string, linkageForms []string) Assessment {
	assessment := Assessment{Expression: strings.TrimSpace(expression)}
	obligations := map[string]bool{}
	seen := map[string]bool{}
	for _, identifier := range identifiersIn(assessment.Expression) {
		if seen[identifier] {
			continue
		}
		seen[identifier] = true
		assessment.Identifiers = append(assessment.Identifiers, identifier)
		owed, known := classified[identifier]
		if !known {
			assessment.Unclassified = append(assessment.Unclassified, identifier)
			continue
		}
		for _, name := range owed {
			obligations[name] = true
		}
		if isLGPL(identifier) && staticLinkage(linkageForms) {
			obligations[ObligationRelinking] = true
		}
	}
	assessment.Obligations = sortedKeys(obligations)
	return assessment
}

// isLGPL recognises the family the one condition applies to. It matches on the
// identifier's prefix because the LGPL identifiers are exactly the ones that
// begin with it -- AGPL does not, and neither does anything else in the table.
func isLGPL(identifier string) bool { return strings.HasPrefix(identifier, "LGPL-") }

// identifiersIn splits a licence expression into the identifiers it names.
//
// It is not an SPDX expression parser and does not need to be: the question is
// which licences are mentioned, so the operators and the grouping can be
// dropped. What must not be dropped is the distinction after WITH -- the token
// there is an exception identifier, not a licence, and reporting
// "Classpath-exception-2.0" as an unclassified licence would be a false alarm
// on a very common expression.
func identifiersIn(expression string) []string {
	fields := strings.FieldsFunc(expression, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '(' || r == ')'
	})
	out := make([]string, 0, len(fields))
	skipNext := false
	for _, field := range fields {
		switch strings.ToUpper(field) {
		case "AND", "OR":
			continue
		case "WITH":
			skipNext = true
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		// "GPL-2.0+" is the older way of saying GPL-2.0-or-later. The plus is
		// not part of the identifier, and the obligations are the same either
		// way, so it is dropped rather than looked up.
		identifier := strings.TrimSuffix(field, "+")
		if identifier == "" || identifier == "NOASSERTION" || identifier == "NONE" {
			continue
		}
		out = append(out, identifier)
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Identifiers is every identifier the committed list classifies, sorted. It is
// what the test that checks the list against the embedded SPDX table reads.
func Identifiers() []string {
	out := make([]string, 0, len(classified))
	for id := range classified {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
