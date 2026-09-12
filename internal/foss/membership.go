package foss

import "github.com/example/sbomb/internal/domain"

// Reported says whether the FOSS outputs write an entry for this component:
// its distribution role is `distributed` and its CycloneDX type is not
// `application` (section 32.6).
//
// It is exported because two callers have to agree about it. The renderer
// decides who is in THIRD-PARTY-NOTICES.txt with it, and `generate` decides
// with it whose narrowed headers the union licence view opens -- section 31
// forbids reading a file that is not needed for an output. Two predicates
// spelling the same rule would let the read set and the output set drift
// apart, which is why there is one function rather than two conditions.
//
// Both halves are tested, and the second half is tested positively rather
// than by exclusion. A component whose role the graph derivation never
// established carries the empty string, which is not a third value of section
// 24.5 but the absence of a derivation: section 24.2's synthetic
// build-environment grouping has no files of its own, so no chain from an
// artifact ever reaches it and nothing decides its role. Section 24.5's rule
// that omission fails towards inclusion is about an *evidence type* the
// derivation does not recognise, and it leaves the node distributed there;
// applying it here would put sbomb's own grouping node into the customer's
// notices document as a third-party component under an unknown licence. So
// the predicate asks for `distributed` and the review record names what it
// did not answer for.
func Reported(component domain.Component) bool {
	return component.Type != "application" && component.DistributionRole == domain.RoleDistributed
}
