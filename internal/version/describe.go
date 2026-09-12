package version

import (
	"regexp"
	"strconv"
	"strings"
)

// distanceSuffix matches the suffix `git describe` appends when HEAD is some
// number of commits past the nearest tag: a dash, the count, a dash, and the
// abbreviated commit behind a "g".
//
// The whole suffix is matched, anchored at the end, rather than the "g" that
// introduces the hash. A tag name may contain the two characters "-g" --
// `v1.0-gamma`, `release-gcc13` -- and reading such a tag as a distance suffix
// would call a component that stands exactly on it modified.
var distanceSuffix = regexp.MustCompile(`^(.+)-([0-9]+)-g[0-9a-f]{4,}$`)

// Checkout is what a `git describe --tags --always --dirty` answer says about
// a checkout, separated into the three things it states at once.
type Checkout struct {
	// Tag is the tag the answer names, without the distance suffix and without
	// the dirty marker. Empty when the answer names no tag at all.
	Tag string
	// Distance is how many commits HEAD stands past Tag. Zero means HEAD is
	// the tagged commit.
	Distance int
	// Dirty reports uncommitted changes in the tree.
	Dirty bool
	// Tagged is false when `--always` fell back to the abbreviated commit,
	// which happens in a repository with no reachable tag.
	Tagged bool
	// Ambiguous is true when the answer could be a tag or the abbreviated
	// commit and nothing was there to tell them apart: no distance suffix, and
	// no HEAD to compare against. Tag is set in that case and may be the
	// commit, so a caller states it at lower confidence rather than as an
	// exact tag. Section 20.3 already rates an unreadable commit that way.
	Ambiguous bool
}

// DescribeCheckout reads that answer. The commit is `git rev-parse HEAD` and is
// what distinguishes a tag from the abbreviated hash `--always` falls back to:
// a tag named after a hex string is conceivable, and comparing against HEAD
// settles it without guessing at the shape of tag names.
//
// It lives here, in the package that owns sections 20.2 and 20.3, because
// four readers derived the same three facts from the same string and each
// carried its own copy of the parsing: this package, and the FetchContent,
// submodule and west adapters. internal/generate reads it too. That is one
// import edge from the adapters to this package, taken deliberately: three
// copies of a grammar are three places for it to drift.
//
// It is one function for all readers of the answer. Section 20.3 derives a
// version from it -- the tag, at lower confidence when HEAD stands past it --
// and section 19.4 derives the modification status from the same three parts,
// so a difference between the two would be a difference about one string.
func DescribeCheckout(described, commit string) Checkout {
	value := strings.TrimSpace(described)
	if value == "" {
		return Checkout{}
	}
	out := Checkout{Dirty: strings.HasSuffix(value, "-dirty")}
	value = strings.TrimSuffix(value, "-dirty")
	if value == "" {
		// A bare dirty marker names nothing at all.
		return out
	}
	if commit != "" && strings.HasPrefix(commit, value) {
		return out
	}
	out.Tagged = true
	if parts := distanceSuffix.FindStringSubmatch(value); parts != nil {
		// A distance suffix is proof of a tag: `--always` writes no such
		// suffix around a bare commit, so no comparison is needed here.
		out.Tag = parts[1]
		out.Distance, _ = strconv.Atoi(parts[2])
		return out
	}
	out.Tag = value
	out.Ambiguous = commit == ""
	return out
}
