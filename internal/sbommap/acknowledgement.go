package sbommap

import (
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/license"
)

// CuratedLicense is one of the two facts both writers state a retained
// licence file's acknowledgement from; the other is the detection technique,
// which the file carries. What each writer makes of them is its own
// vocabulary: CycloneDX calls only a file's own SPDX-License-Identifier
// "declared" (the CycloneDX writer's acknowledgementFor), SPDX 3.0.1 calls
// every detection hasDeclaredLicense (the SPDX mapping's fileAcknowledgement).
// Only the inputs are shared, so they live here.

// CuratedLicense reports whether a component's licence is a curated
// conclusion, which makes every licence file beside it a conclusion too.
//
// A curated value is a conclusion whatever else happened. Testing the source
// for "curated" alone misses the case that matters most -- section 22.5's
// conflict, where a reviewer overrode what the file said, and ResolveConflict
// carries the *file's* name as the source -- so the override would be
// published as the component's own declaration. The reason code is what
// survives both paths.
func CuratedLicense(component domain.Component) bool {
	return len(component.Licenses) > 0 &&
		(component.Licenses[0].Source == "curated" ||
			component.Licenses[0].Reason == license.ReasonConflictingEvidence)
}
