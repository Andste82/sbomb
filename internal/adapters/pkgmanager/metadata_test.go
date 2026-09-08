package pkgmanager

import (
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// A single origin is the ordinary case: what it said is what the document
// publishes, origin and confidence included.
func TestOneClaimIsTakenWhole(t *testing.T) {
	var found Package
	found.Take(FieldVersion, Claim{
		Value: "1.2.0", Source: "conan", Rank: RankInstallState, Confidence: domain.ConfidenceHigh,
	})

	if found.Version.Value != "1.2.0" || found.Version.Source != "conan" {
		t.Errorf("version = %q from %q", found.Version.Value, found.Version.Source)
	}
	if found.Version.Confidence != domain.ConfidenceHigh {
		t.Errorf("confidence = %q, want the one the origin stated", found.Version.Confidence)
	}
	if len(found.Superseded) != 0 {
		t.Errorf("superseded = %#v, want none: nobody was outranked", found.Superseded)
	}
}

// Two origins for one field must produce the same winner however the adapter
// happened to reach them, or the document would depend on the order in which
// files were read.
func TestTheStrongerOriginWinsInEitherOrder(t *testing.T) {
	declared := Claim{Value: "1.2.0", Source: "fetchcontent", Rank: RankInstallState}
	observed := Claim{Value: "1.3.0", Source: "git-describe", Rank: RankObservedCheckout}

	for _, order := range [][]Claim{{declared, observed}, {observed, declared}} {
		var found Package
		for _, claim := range order {
			found.Take(FieldVersion, claim)
		}
		if found.Version.Value != "1.3.0" || found.Version.Source != "git-describe" {
			t.Errorf("version = %q from %q, want the checkout to win",
				found.Version.Value, found.Version.Source)
		}
		if len(found.Superseded) != 1 || found.Superseded[0].Claim.Value != "1.2.0" {
			t.Errorf("superseded = %#v, want the declared tag", found.Superseded)
		}
		if found.Superseded[0].Field != FieldVersion {
			t.Errorf("superseded field = %q, want version", found.Superseded[0].Field)
		}
	}
}

// Equal standing is not a coin toss: the origin that was read first keeps the
// field, so two runs over the same tree publish the same value.
func TestEqualRanksKeepTheFirstOrigin(t *testing.T) {
	build := func() Package {
		var found Package
		found.Take(FieldLicense, Claim{Value: "MIT", Source: "vcpkg", Rank: RankInstallState})
		found.Take(FieldLicense, Claim{Value: "Apache-2.0", Source: "conan", Rank: RankInstallState})
		return found
	}
	first, second := build(), build()

	if first.License.Value != "MIT" || second.License.Value != first.License.Value {
		t.Errorf("license = %q then %q, want MIT both times", first.License.Value, second.License.Value)
	}
	if len(first.Superseded) != 1 || first.Superseded[0].Claim.Value != "Apache-2.0" {
		t.Errorf("superseded = %#v, want the second claim", first.Superseded)
	}
}

// Rank and confidence answer different questions, and merging them would make
// an exact but stale declaration beat the tree it describes. A manifest states
// its version exactly; a describe with distance is less certain and still
// closer to the truth about what is on disk.
func TestRankDecidesEvenAgainstAHigherConfidence(t *testing.T) {
	var found Package
	found.Take(FieldVersion, Claim{
		Value: "1.2.0", Source: "fetchcontent", Rank: RankForeignManifest, Confidence: domain.ConfidenceHigh,
	})
	found.Take(FieldVersion, Claim{
		Value: "1.3.0-4-gabcdef", Source: "git-describe", Rank: RankInstallState, Confidence: domain.ConfidenceMedium,
	})

	if found.Version.Value != "1.3.0-4-gabcdef" {
		t.Errorf("version = %q, want the higher-ranked origin", found.Version.Value)
	}
	if found.Version.Confidence != domain.ConfidenceMedium {
		t.Errorf("confidence = %q, want the winner's own confidence", found.Version.Confidence)
	}
}

// A file that was read but said nothing is not an origin. Recording it would
// publish evidence that a version came from somewhere while no version exists.
func TestAClaimWithoutAValueOrARankIsNotTaken(t *testing.T) {
	var found Package
	found.Take(FieldVersion, Claim{Value: "", Source: "vcpkg", Rank: RankInstallState})
	found.Take(FieldSupplier, Claim{Value: "Example Org", Source: "vcpkg", Rank: RankNone})

	if found.Version.Source != "" || found.Version.Rank != RankNone {
		t.Errorf("version = %#v, want nothing taken from an empty value", found.Version)
	}
	if found.Supplier.Value != "" {
		t.Errorf("supplier = %#v, want nothing taken from an unranked claim", found.Supplier)
	}
	if len(found.Superseded) != 0 {
		t.Errorf("superseded = %#v, want none: neither claim was a claim", found.Superseded)
	}
}

// Take writes the four metadata values and nothing else. A name that is none
// of them changes no field and records no loser, so a field this package does
// not carry cannot quietly become one.
func TestAFieldThatIsNoneOfTheFourIsNotTaken(t *testing.T) {
	var found Package
	found.Take(Field("homepage"), Claim{Value: "https://example.invalid", Source: "vcpkg", Rank: RankInstallState})

	if found.Version.Value != "" || found.License.Value != "" ||
		found.Supplier.Value != "" || found.PURL.Value != "" {
		t.Errorf("package = %#v, want every field untouched", found)
	}
	if len(found.Superseded) != 0 {
		t.Errorf("superseded = %#v, want none: nothing was displaced", found.Superseded)
	}
}

// The four fields are kept apart, and every one of them can be claimed.
func TestEachFieldIsTakenSeparately(t *testing.T) {
	var found Package
	found.Take(FieldVersion, Claim{Value: "2.1.0", Source: "vcpkg", Rank: RankInstallState})
	found.Take(FieldLicense, Claim{Value: "MIT", Source: "vcpkg", Rank: RankInstallState})
	found.Take(FieldSupplier, Claim{Value: "Example Org", Source: "vcpkg", Rank: RankInstallState})
	found.Take(FieldPURL, Claim{Value: "pkg:vcpkg/tinyfmt@2.1.0", Source: "vcpkg", Rank: RankInstallState})

	if found.Version.Value != "2.1.0" || found.License.Value != "MIT" ||
		found.Supplier.Value != "Example Org" || found.PURL.Value != "pkg:vcpkg/tinyfmt@2.1.0" {
		t.Errorf("package = %#v, want all four fields filled", found)
	}
	if len(found.Superseded) != 0 {
		t.Errorf("superseded = %#v, want none: four fields are not one dispute", found.Superseded)
	}
}
