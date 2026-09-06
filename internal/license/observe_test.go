package license

import (
	"strings"
	"testing"
)

// dualLicensed is the shape the whole mechanism exists for: a sentence saying
// there is a choice, then two complete licence texts.
func dualLicensed() string {
	return "This software is dual licensed. You may use it under either licence below.\n\n" +
		"=== The MIT License ===\n\n" + verbatimMIT +
		"\n\n=== The BSD 3-Clause License ===\n\n" + uuidLicence
}

func TestObserveFindsBothLicencesInADualLicensedFile(t *testing.T) {
	observations := Observe(dualLicensed())
	if len(observations) != 2 {
		var got []string
		for _, o := range observations {
			got = append(got, o.ID)
		}
		t.Fatalf("found %v, want MIT and BSD-3-Clause", got)
	}
	// In the order they appear, so a reader can line the answer up with the
	// file.
	if observations[0].ID != "MIT" || observations[1].ID != "BSD-3-Clause" {
		t.Errorf("found %s then %s", observations[0].ID, observations[1].ID)
	}
	if observations[0].End > observations[1].Start {
		t.Error("the two spans overlap")
	}
}

// The whole-file techniques must still say nothing: the file is not MIT and it
// is not BSD-3-Clause. Concluding either would be inventing a licence.
func TestADualLicensedFileConcludesNothing(t *testing.T) {
	finding := ResolveFromText(dualLicensed(), "LICENSE")
	if finding.Expression != "" {
		t.Fatalf("concluded %q from a file holding two licences", finding.Expression)
	}
}

// A file that is exactly one licence is found here too, which is why callers
// use this only after the whole-file techniques have failed.
func TestObserveFindsASingleLicence(t *testing.T) {
	observations := Observe(uuidLicence)
	if len(observations) != 1 || observations[0].ID != "BSD-3-Clause" {
		t.Fatalf("got %+v, want one BSD-3-Clause", observations)
	}
}

// A licence text surrounded by other material is still present, and saying so
// is more useful than saying nothing. The Apache LICENSE that ships with many
// projects is exactly this: the licence, then an appendix whose wording
// predates the one SPDX publishes.
func TestObserveFindsALicenceWithMaterialAroundIt(t *testing.T) {
	text := "Portions of this product are covered as follows.\n\n" + verbatimMIT +
		"\n\nFor support, write to nobody@example.invalid.\n"
	observations := Observe(text)
	if len(observations) != 1 || observations[0].ID != "MIT" {
		t.Fatalf("got %+v, want one MIT", observations)
	}
}

func TestObserveFindsNothingInProse(t *testing.T) {
	if observations := Observe("This project is maintained by volunteers. Be kind."); len(observations) != 0 {
		t.Fatalf("got %+v, want none", observations)
	}
}

func TestObserveFindingsCarryTheirSourceAndTechnique(t *testing.T) {
	findings := ObserveFindings(dualLicensed(), "vendor/thing/LICENSE")
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
	for _, finding := range findings {
		if finding.Source != "vendor/thing/LICENSE" {
			t.Errorf("source = %q", finding.Source)
		}
		if finding.Technique != TechniqueTemplate {
			t.Errorf("technique = %q", finding.Technique)
		}
		if finding.Evidence != "component-level" {
			t.Errorf("evidence class = %q", finding.Evidence)
		}
	}
}

// Overlaps have to be resolved, or a licence whose text quotes another is
// reported twice.
func TestOverlappingObservationsKeepTheLongest(t *testing.T) {
	entries := []*entry{{id: "Long"}, {id: "Short"}, {id: "Later"}}
	kept := resolveOverlaps([]Observation{
		{ID: "Short", Start: 10, End: 40},
		{ID: "Long", Start: 0, End: 100},
		{ID: "Later", Start: 100, End: 150},
	}, entries)
	if len(kept) != 2 || kept[0].ID != "Long" || kept[1].ID != "Later" {
		t.Fatalf("kept %+v", kept)
	}
}

func BenchmarkObserveDualLicensedFile(b *testing.B) {
	text := dualLicensed()
	if len(Observe(text)) != 2 {
		b.Fatal("the fixture does not hold two licences")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Observe(text)
	}
}

func TestObserveIsDeterministic(t *testing.T) {
	first := Observe(dualLicensed())
	for i := 0; i < 3; i++ {
		again := Observe(dualLicensed())
		if len(again) != len(first) {
			t.Fatalf("run %d found %d licences, first run found %d", i, len(again), len(first))
		}
		for index := range first {
			if again[index] != first[index] {
				t.Fatalf("run %d differs at %d: %+v vs %+v", i, index, again[index], first[index])
			}
		}
	}
	_ = strings.TrimSpace("")
}
