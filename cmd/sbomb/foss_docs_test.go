package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// docs/foss.md is the one document a reader who has not read docs/dev/ can use
// to find out what the attribution outputs are and what sbomb refuses to
// decide. Two of its properties are load-bearing enough to pin: the sentence
// that draws the line between the tool and the assessment, and the table of
// what is deliberately not built. Prose can be rewritten freely around them.
func TestTheAttributionDocumentStatesTheBoundaryAndWhatIsNotBuilt(t *testing.T) {
	path := filepath.Join(testutil.RepoRoot(t), "docs", "foss.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document := string(data)

	// The same sentence stands in THIRD-PARTY-NOTICES.txt and foss-review.txt,
	// where the goldens pin it. It is the whole separation: the tool supplies
	// the facts, somebody else draws the conclusion.
	const boundary = "sbomb produces data; the manufacturer performs the assessment."
	if !strings.Contains(document, boundary) {
		t.Errorf("docs/foss.md does not contain %q", boundary)
	}

	// A refusal nobody can find is a refusal nobody can plan around, which is
	// why the table is governance rather than decoration.
	if !strings.Contains(document, "| Not built | Why |") {
		t.Error("docs/foss.md has no table of what is not built")
	}
	for _, row := range []string{
		"Licence compatibility verdicts",
		"Source bundles, corresponding source",
		"Obligation fulfilment tracking",
		"Per-licence exemptions for binary distribution",
		"Indicating whether an asset was changed",
		"Checking reserved font names",
		"Similarity or percentage licence matching",
		"A licence database or network lookup",
		"VEX / CVE mapping",
	} {
		if !strings.Contains(document, row) {
			t.Errorf("the table of what is not built no longer names %q", row)
		}
	}
}
