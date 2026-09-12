package version

import "testing"

// The grammar of a `git describe --tags --always --dirty` answer, which two
// sections read: section 20.3 takes the version from it and section 19.4 the
// modification status. A tag name may contain the two characters that
// introduce the distance suffix -- `v1.0-gamma`, `release-gcc13` -- so the
// suffix is matched whole and anchored, and the abbreviated commit `--always`
// falls back to is told from a tag by comparing it with HEAD.
func TestDescribeCheckoutSeparatesTagDistanceAndDirty(t *testing.T) {
	const head = "1d0f2c3b4a5968778695a4b3c2d1e0f918273645"
	for _, testCase := range []struct {
		described string
		want      Checkout
	}{
		{"v1.2.0", Checkout{Tag: "v1.2.0", Tagged: true}},
		{"v1.2.0-dirty", Checkout{Tag: "v1.2.0", Dirty: true, Tagged: true}},
		{"v1.2.0-4-gdeadbee", Checkout{Tag: "v1.2.0", Distance: 4, Tagged: true}},
		{"v1.2.0-4-gdeadbee-dirty", Checkout{Tag: "v1.2.0", Distance: 4, Dirty: true, Tagged: true}},
		// Tag names that carry the marker of a distance suffix.
		{"v1.0-gamma", Checkout{Tag: "v1.0-gamma", Tagged: true}},
		{"release-gcc13", Checkout{Tag: "release-gcc13", Tagged: true}},
		{"v2.0-gtest-support", Checkout{Tag: "v2.0-gtest-support", Tagged: true}},
		{"v1.0-gamma-12-g1d0f2c3", Checkout{Tag: "v1.0-gamma", Distance: 12, Tagged: true}},
		// No tag: `--always` answers with the abbreviated commit, which is a
		// prefix of HEAD. It is not a version and not a tag.
		{head[:8], Checkout{}},
		{head[:8] + "-dirty", Checkout{Dirty: true}},
		{"", Checkout{}},
	} {
		t.Run(testCase.described, func(t *testing.T) {
			if got := DescribeCheckout(testCase.described, head); got != testCase.want {
				t.Errorf("DescribeCheckout(%q) = %+v, want %+v", testCase.described, got, testCase.want)
			}
		})
	}
}

// Without HEAD there is nothing to compare an abbreviated commit against, so a
// hex answer reads as a tag. Every caller reads HEAD first for that reason;
// this pins what the function does when one does not.
func TestDescribeCheckoutWithoutHeadIsAmbiguous(t *testing.T) {
	// No HEAD and no distance suffix: the answer could be either, and saying
	// so is what lets a caller rate it lower instead of publishing a commit as
	// an exact tag.
	if got := DescribeCheckout("1d0f2c3", ""); !got.Tagged || got.Tag != "1d0f2c3" || !got.Ambiguous {
		t.Errorf("DescribeCheckout(%q, \"\") = %+v, want a tag marked ambiguous", "1d0f2c3", got)
	}
	// A distance suffix is proof of a tag whatever HEAD says, so nothing is
	// ambiguous there.
	if got := DescribeCheckout("v1.2.0-4-gdeadbee", ""); got.Ambiguous || got.Tag != "v1.2.0" || got.Distance != 4 {
		t.Errorf("DescribeCheckout with a distance and no HEAD = %+v, want an unambiguous tag", got)
	}
}
