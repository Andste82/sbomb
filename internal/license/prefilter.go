package license

import (
	"strings"
	"sync"
)

// The template matcher asks the same question of 739 templates in a row: does
// this text contain that template's anchor. Each ask is a substring search over
// the whole text, so the cost is 739 times the text -- and on a real run that
// was 55% of all CPU, spent almost entirely proving that a source file is not
// any of the 739 licences.
//
// The loop is inverted here instead. Every anchor contributes one fixed-length
// probe to an index built once for the process; one rolling pass over the text
// then names the few templates whose probe occurs in it, and only those are
// searched for in full. The cost stops depending on how many templates there
// are: it is one pass over the text plus a confirmation per candidate.
//
// Nothing is decided by the index. A template it names is still confirmed with
// the same substring search as before, and a template it does not name cannot
// contain its own probe, so it could not have matched. The set of templates
// that reach the regular expression is unchanged, and so is every answer.

// probeK is how many bytes of an anchor go into the index. It is minAnchor, so
// every anchor is long enough to have one.
const probeK = minAnchor

const gramBase = 1099511628211

// probeHash is the rolling hash of the first probeK bytes of s.
func probeHash(s string) uint64 {
	var h uint64
	for i := 0; i < probeK; i++ {
		h = h*gramBase + uint64(s[i])
	}
	return h
}

// anchorIndex maps a probe to the templates whose anchor opens with it. Several
// licences share an opening -- the BSD family agrees for far more than 24
// characters -- so the value is a list.
type anchorIndex struct {
	byProbe map[uint64][]int32
	// longestSpan is the largest a text can be and still match some template
	// whole. matchTemplates compares the whole text against an expression
	// anchored at both ends, so a longer text matches none of them and the
	// pass can be skipped outright -- which is the case a source file falls
	// into.
	longestSpan int
}

var templateIndex = sync.OnceValue(func() *anchorIndex {
	index := &anchorIndex{byProbe: make(map[uint64][]int32, 1024)}
	entries, err := loadTemplates()
	if err != nil {
		return index
	}
	for position, entry := range entries {
		if span := entry.span(); span > index.longestSpan {
			index.longestSpan = span
		}
		if len(entry.anchor) < probeK {
			continue
		}
		probe := probeHash(entry.anchor)
		index.byProbe[probe] = append(index.byProbe[probe], int32(position))
	}
	return index
})

// candidates names the templates whose probe occurs in the text, as a set of
// indices into the template table. A nil result means the text is too short to
// hold any probe at all.
//
// The pass is exact in the direction that matters: a template whose anchor is
// in the text always has its probe in the text, so it is always named. The
// reverse does not hold -- two byte strings can hash alike, and a probe is only
// the opening of an anchor -- which is why the caller still confirms.
func (index *anchorIndex) candidates(text string) map[int32]bool {
	if len(text) < probeK {
		return nil
	}
	found := make(map[int32]bool, 8)
	var pow uint64 = 1
	for i := 0; i < probeK-1; i++ {
		pow *= gramBase
	}
	h := probeHash(text)
	if positions, hit := index.byProbe[h]; hit {
		for _, position := range positions {
			found[position] = true
		}
	}
	for i := probeK; i < len(text); i++ {
		h = (h-uint64(text[i-probeK])*pow)*gramBase + uint64(text[i])
		if positions, hit := index.byProbe[h]; hit {
			for _, position := range positions {
				found[position] = true
			}
		}
	}
	return found
}

// anchoredIn confirms a candidate the index named.
func anchoredIn(text, anchor string) bool {
	return anchor == "" || strings.Contains(text, anchor)
}
