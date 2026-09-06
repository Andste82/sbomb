package license

import (
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// The case the whole technique exists for: a BSD-3-Clause file with the
// copyright holder filled in and the clauses bulleted rather than numbered.
// The digest table cannot see it, and a person cannot mistake it.
func TestAFilledInTemplateIsRecognized(t *testing.T) {
	finding := ResolveFromText(uuidLicence, "vendor/github.com/google/uuid/LICENSE")
	if finding.Expression != "BSD-3-Clause" {
		t.Fatalf("expression = %q, want BSD-3-Clause", finding.Expression)
	}
	if finding.Technique != TechniqueTemplate {
		t.Errorf("technique = %q, want %q", finding.Technique, TechniqueTemplate)
	}
	if finding.Evidence != "component-level" {
		t.Errorf("evidence class = %q, want component-level", finding.Evidence)
	}
	if finding.Confidence != domain.ConfidenceHigh {
		t.Errorf("confidence = %q", finding.Confidence)
	}
}

// The digest stays the fast path. A verbatim text must not reach the template
// matcher at all, which is what the technique it reports proves.
func TestAVerbatimTextStillMatchesByDigest(t *testing.T) {
	finding := ResolveFromText(verbatimMIT, "LICENSE")
	if finding.Expression != "MIT" {
		t.Fatalf("expression = %q, want MIT", finding.Expression)
	}
	if finding.Technique != TechniqueDigest {
		t.Errorf("technique = %q, want %q", finding.Technique, TechniqueDigest)
	}
}

func TestAnSPDXIdentifierBeatsBoth(t *testing.T) {
	finding := ResolveFromText("SPDX-License-Identifier: Apache-2.0\n"+uuidLicence, "header.h")
	if finding.Expression != "Apache-2.0" {
		t.Fatalf("expression = %q, want the declared identifier", finding.Expression)
	}
	if finding.Technique != TechniqueIdentifier {
		t.Errorf("technique = %q, want %q", finding.Technique, TechniqueIdentifier)
	}
}

// Text that is not a licence must stay NOASSERTION. A matcher that recognizes
// everything is worth nothing.
func TestProseIsNotALicense(t *testing.T) {
	finding := ResolveFromText("This project is maintained by volunteers. Please be kind.", "README.md")
	if finding.Expression != "" {
		t.Fatalf("expression = %q, want none", finding.Expression)
	}
	if finding.Reason != ReasonLicenseTextUnrecognized {
		t.Errorf("reason = %q, want %q", finding.Reason, ReasonLicenseTextUnrecognized)
	}
}

// Every embedded template has to translate to an expression RE2 accepts. Two
// hundred of them did not before bounded repeats were clamped, and a silent
// skip would have looked exactly like a licence that does not match.
func TestEveryEmbeddedTemplateCompiles(t *testing.T) {
	entries, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 700 {
		t.Fatalf("only %d templates are embedded; the SPDX list has around 739", len(entries))
	}
	var failed []string
	for _, entry := range entries {
		if _, err := entry.regexp(); err != nil {
			failed = append(failed, entry.id+": "+err.Error())
		}
	}
	if len(failed) > 0 {
		t.Fatalf("%d template(s) did not compile, first: %s", len(failed), failed[0])
	}
}

// The prefilter must never reject a text the expression would have accepted.
// It is sound exactly when the anchor is invariant text of that same template.
func TestEveryAnchorIsInvariantTextOfItsOwnTemplate(t *testing.T) {
	entries, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	anchored := 0
	for _, entry := range entries {
		if entry.anchor == "" {
			continue
		}
		anchored++
		if !strings.Contains(looseNormalize(entry.source), entry.anchor) {
			t.Errorf("%s: anchor %q is not in its own template", entry.id, entry.anchor)
		}
	}
	if anchored < len(entries)/2 {
		t.Errorf("only %d of %d templates have an anchor; the prefilter would barely filter", anchored, len(entries))
	}
}

func TestTheListCoversTheCommonLicenses(t *testing.T) {
	ids, err := TemplateIDs()
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, id := range ids {
		present[id] = true
	}
	for _, want := range []string{"MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "GPL-3.0-only", "ISC"} {
		if !present[want] {
			t.Errorf("%s has no embedded template", want)
		}
	}
}

// A bound must be clamped, not dropped. An unbounded variable swallows
// arbitrary text, and a two-clause template then matches a file that holds
// three clauses and a second licence after them -- which is what happened
// before this clamped instead.
func TestBoundedRepeatsAreClampedNotDropped(t *testing.T) {
	for _, testCase := range []struct{ in, want string }{
		{`.{0,5000}`, `.{0,1000}`},
		{`.{0,20}`, `.{0,20}`},
		{`.{2000,9000}`, `.{1000,1000}`},
		{`a{1,3}b{0,7000}`, `a{1,3}b{0,1000}`},
		{`no repeats here`, `no repeats here`},
	} {
		if got := clampRepeats(testCase.in); got != testCase.want {
			t.Errorf("clampRepeats(%q) = %q, want %q", testCase.in, got, testCase.want)
		}
	}
}

func TestCompileTemplateHandlesVariablesAndOptionalBlocks(t *testing.T) {
	template := `<<beginOptional>>Short License<<endOptional>> ` +
		`<<var;name="copyright";original="Copyright (c) <year>";match=".{0,5000}">>` +
		"\nPermission is granted to " +
		`<<var;name="who";original="anyone";match="anyone|any person">>.`

	pattern, err := compileTemplate(template)
	if err != nil {
		t.Fatal(err)
	}
	for _, accepted := range []string{
		"short license copyright (c) 2026 someone permission is granted to anyone.",
		"copyright (c) 2026 someone else permission is granted to any person.",
		"Short License\n\nCopyright (c) 1999 A\n\nPermission is granted to anyone.",
	} {
		if !pattern.MatchString(looseNormalize(accepted)) {
			t.Errorf("did not match: %q", accepted)
		}
	}
	for _, rejected := range []string{
		"permission is granted to nobody.",
		"permission is granted to anyone. and then some other licence text",
	} {
		if pattern.MatchString(looseNormalize(rejected)) {
			t.Errorf("matched but should not: %q", rejected)
		}
	}
}

// The whitespace a template and a real file disagree about must not matter:
// SPDX puts a space around every substitution point whether the sentence has
// one or not.
func TestLiteralPatternToleratesWhitespaceDisagreement(t *testing.T) {
	template := `the ` + `<<var;name="thing";original="software";match="software|work">>` + ` (the " Software ")`
	pattern, err := compileTemplate(template)
	if err != nil {
		t.Fatal(err)
	}
	if !pattern.MatchString(looseNormalize(`the software (the "Software")`)) {
		t.Error("a file without the template's padding spaces did not match")
	}
}

// Optional blocks nest: 46 of the 739 templates do it, three deep at the most.
// Closing an outer block on an inner block's marker truncates it silently, and
// the licence then matches nothing -- which is what GPL-2.0 and LGPL-3.0 did,
// because the appendix that explains how to apply them is a nest of optional
// pieces.
func TestOptionalBlocksNest(t *testing.T) {
	template := `Terms.<<beginOptional>> Appendix.<<beginOptional>> Sub<<endOptional>> End.<<endOptional>>`
	pattern, err := compileTemplate(template)
	if err != nil {
		t.Fatal(err)
	}
	for _, accepted := range []string{
		"terms.",
		"terms. appendix. end.",
		"terms. appendix. sub end.",
	} {
		if !pattern.MatchString(looseNormalize(accepted)) {
			t.Errorf("did not match: %q", accepted)
		}
	}
	// Taking the first <<endOptional>> would end the outer block after "Sub",
	// leaving " End." as mandatory literal text.
	if pattern.MatchString(looseNormalize("terms. appendix. sub")) {
		t.Error(`matched a text that omits the outer block's tail`)
	}
}

func TestClosingOptionalCountsDepth(t *testing.T) {
	template := `<<beginOptional>>a<<beginOptional>>b<<endOptional>>c<<endOptional>>tail`
	closing := closingOptional(template, 0)
	if closing < 0 {
		t.Fatal("no closing marker found")
	}
	if rest := template[closing+len(endOptional):]; rest != "tail" {
		t.Errorf("the outer block ended too early; the rest is %q, want %q", rest, "tail")
	}
	if body := optionalBody(template, 0, closing); body != `a<<beginOptional>>b<<endOptional>>c` {
		t.Errorf("body = %q", body)
	}
}

// A deprecated identifier matches every text its current spelling does. GPL-2.0
// is the superseded name of GPL-2.0-only; reporting both would report SPDX's
// own renaming as a disagreement.
func TestADeprecatedIdentifierYieldsToACurrentOne(t *testing.T) {
	entries, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var current, superseded bool
	for _, e := range entries {
		switch e.id {
		case "GPL-2.0-only":
			current = !e.deprecated
		case "GPL-2.0":
			superseded = e.deprecated
		}
	}
	if !current || !superseded {
		t.Fatalf("the list no longer marks GPL-2.0 deprecated and GPL-2.0-only current (current=%v superseded=%v)", current, superseded)
	}
}
