package pkgmanager

import "github.com/example/sbomb/internal/domain"

// This file holds the provenance model of section 21.1: every metadata value a
// manager states carries where it came from and how strong that origin is, so
// that two files describing the same component can be resolved without a coin
// toss and so that the document can say which origin actually won.

// Rank is the standing of an origin, not a measure of how sure the value is.
// The two must not merge: a manifest can state a version exactly and still be
// out of date, so an exact declaration of high confidence loses to the weaker
// but current answer of the checkout itself. Confidence travels with the value
// (section 20.3); Rank alone decides which value survives.
//
// The order ascends so that the zero value asserts nothing: a claim nobody
// filled in cannot beat a claim somebody made.
type Rank int

const (
	// RankNone is the zero value: no origin was stated, so nothing is claimed.
	RankNone Rank = iota
	// RankForeignManifest is any other manifest file in the same directory --
	// a manifest of a manager that does not own this package. It describes the
	// right place but was written by the wrong hand.
	RankForeignManifest
	// RankDeclaredManifest is the manifest the owning manager declares, which
	// says what was asked for rather than what was installed.
	RankDeclaredManifest
	// RankInstallState is what the owning manager recorded when it installed:
	// a file list, a lock file, a resolved dependency, a generated config. It
	// names the version that is actually on disk, not the range that was
	// requested.
	RankInstallState
	// RankBundledSBOM is an SBOM the upstream shipped inside the package, at
	// the package root. Nobody knows the package better than its author.
	RankBundledSBOM
	// RankObservedCheckout is the working copy itself, asked directly through
	// introspection. It outranks every declared origin because a declaration
	// says what was wanted and the checkout says what is there, including the
	// edits somebody has since made to it.
	RankObservedCheckout
)

// Field names the metadata value a claim is about. It exists so that a
// superseded contribution can say what was disputed; the winning values stay
// named struct fields, because every consumer reads exactly these four and
// should do so type-safely rather than through a map lookup.
type Field string

const (
	FieldVersion  Field = "version"
	FieldLicense  Field = "license"
	FieldSupplier Field = "supplier"
	FieldPURL     Field = "purl"
)

// Claim is one origin's statement about one metadata value.
type Claim struct {
	// Value is the value itself. An empty value is no claim at all.
	Value string
	// Source is the origin as the document names it -- "conan", "vcpkg",
	// "fetchcontent", "git-describe". It reaches the reader through
	// component.VersionSource, where section 20.3 maps it onto a closed
	// CycloneDX technique vocabulary, so these strings are output and not an
	// internal label.
	Source string
	// Rank decides which claim wins. See the type for why it is not a
	// confidence.
	Rank Rank
	// Confidence is what section 20.3 assigns to a version. The other three
	// fields have no confidence table, so it stays empty for them.
	Confidence domain.Confidence
}

// Contribution is a claim together with the field it is about. It is what an
// enricher hands back, because a reader that describes a root has to say which
// value it is describing, and it is also what Package.Superseded keeps: there
// the claims are the ones that lost, and a loser nobody can name a field for
// cannot be reported.
type Contribution struct {
	Field Field
	Claim Claim
}

// Take records one origin's claim about one field and keeps the strongest.
// It is the only place the four metadata values are written, so that no
// adapter can assign past the ranking by accident.
//
// Rules, in order: an empty value or an unranked claim is not a claim and is
// dropped without trace; a stronger claim wins and pushes the previous one
// aside; an equally or less strongly ranked claim is itself pushed aside, so
// that the first origin found keeps the field and the output stays
// deterministic. That last rule makes the order of the Take calls inside an
// adapter part of the behaviour and no longer a matter of taste: reordering
// two equally ranked calls changes which value is published.
//
// Take mutates the package, so it may only be called while the package is
// being discovered. Once Discover has returned, packages are copied by value
// through the resolver, and a Take on a copy would append to a slice the
// original never sees. Enrichment counts as part of discovery for this reason
// and runs inside Discover, on the package the adapter just produced.
func (p *Package) Take(field Field, claim Claim) {
	if claim.Value == "" || claim.Rank == RankNone {
		return
	}
	current := p.claimFor(field)
	if current == nil {
		return
	}
	if current.Value == "" {
		*current = claim
		return
	}
	// Nothing is thrown away: the loser is kept so that section 21.1 can report
	// that two origins disagreed and which one the document went with.
	if claim.Rank > current.Rank {
		p.Superseded = append(p.Superseded, Contribution{Field: field, Claim: *current})
		*current = claim
		return
	}
	p.Superseded = append(p.Superseded, Contribution{Field: field, Claim: claim})
}

// claimFor points at the field a Field names, or nil for a name that is none
// of them.
func (p *Package) claimFor(field Field) *Claim {
	switch field {
	case FieldVersion:
		return &p.Version
	case FieldLicense:
		return &p.License
	case FieldSupplier:
		return &p.Supplier
	case FieldPURL:
		return &p.PURL
	}
	return nil
}
