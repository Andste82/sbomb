package foss

import (
	"fmt"
	"strings"
)

// renderObligations produces source-obligations.txt: which components owe
// source material, and why.
//
// It names the obligation, its trigger and its consequence, and states that
// the material is out of scope. It never produces any of it (requirement R6).
// Complete Corresponding Source is the source of the whole work plus the
// scripts used to control compilation and installation; an evidence-derived
// subset would look like a source offer while being materially incomplete,
// which converts the tool's precision into a compliance defect. That is why
// the structural guarantee of section 32.6 is a test and not a sentence here.
func renderObligations(m model, s style) string {
	var b strings.Builder
	s.title(&b, "SOURCE OBLIGATIONS")
	b.WriteString("\n")
	s.prose(&b, "", "For every distributed component whose licence carries a source obligation: "+
		"what is owed, what triggered it, and the fact that sbomb neither produces nor can "+
		"produce the material. A build-time-only component is not in this list, because a "+
		"component the product does not contain triggered nothing.")
	b.WriteString("\n")
	s.prose(&b, "", "This is a list of obligations, not a compliance verdict. Whether an "+
		"obligation was discharged is not something sbomb can observe.")

	s.section(&b, "COMPONENTS OWING SOURCE MATERIAL")
	var owing int
	for _, candidate := range m.distributed {
		if !candidate.assessment.Owed() {
			continue
		}
		if owing > 0 {
			b.WriteString("\n")
		}
		owing++
		s.bullet(&b, strings.Join([]string{heading(candidate), candidate.license,
			listOrDash(candidate.linkage)}, " - "))
		if len(candidate.deliverables) > 0 {
			// Which deliverable owes it. LGPL-2.1 section 6 asks for what is
			// needed to relink *the application*, so a component two
			// deliverables share owes two different things and the reader has
			// to know which is which.
			s.prose(&b, "  ", "Reached from: "+strings.Join(candidate.deliverables, ", "))
		}
		for _, sentence := range candidate.assessment.Prose() {
			s.prose(&b, "  ", sentence)
		}
		s.prose(&b, "  ", "sbomb does not and cannot produce this material.")
	}
	if owing == 0 {
		s.prose(&b, "", "No distributed component's licence carries a source obligation.")
	}

	s.section(&b, "UNCLASSIFIED LICENCES")
	s.prose(&b, "", "An identifier on neither obligation list is reported here and never "+
		"assumed permissive, so that a gap in the list is visible rather than silent. The "+
		"list is small and curated on purpose: two thousand imported classifications nobody "+
		"has read do not belong in a document that carries the manufacturer's name.")
	b.WriteString("\n")
	var unclassified int
	for _, candidate := range m.distributed {
		if len(candidate.assessment.Unclassified) == 0 {
			continue
		}
		unclassified++
		s.bullet(&b, fmt.Sprintf("%s - %s", heading(candidate),
			strings.Join(candidate.assessment.Unclassified, ", ")))
	}
	if unclassified == 0 {
		s.prose(&b, "", "None: every licence identifier of every distributed component is on one of the lists.")
	}
	return b.String()
}
