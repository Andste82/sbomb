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
	// probeBits has a bit set for every probe byProbe holds, and probeMask
	// selects the bit a probe hash belongs to. Together they are the cheap
	// half of the lookup: see maybeInMap.
	probeBits []uint64
	probeMask uint64
	// longestSpan is the largest a text can be and still match some template
	// whole. matchTemplates compares the whole text against an expression
	// anchored at both ends, so a longer text matches none of them and the
	// pass can be skipped outright -- which is the case a source file falls
	// into.
	longestSpan int
}

var templateIndex = sync.OnceValue(func() *anchorIndex {
	index := &anchorIndex{byProbe: make(map[uint64][]int32, 1024)}
	if entries, err := loadTemplates(); err == nil {
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
	}
	// Built even when the templates could not be loaded, so that the pass can
	// test a bit without first asking whether there is a filter to test.
	index.buildFilter()
	return index
})

// bitsPerProbe is how much room the filter gets for each distinct probe. The
// filter is a single hash per probe, so its false answers are as common as its
// bits are crowded: sixteen bits apiece leaves it a few per cent full, which
// sends a few per cent of the positions that would have missed on to the map.
// It also keeps the whole array inside the first-level cache, which is the
// reason the test is cheaper than the lookup it stands in front of.
const bitsPerProbe = 16

// buildFilter fills probeBits from the probes already in byProbe. It has to run
// after the map is complete, because the two must agree: every probe in the map
// has its bit set here, or the pass would drop a template the map would have
// named.
func (index *anchorIndex) buildFilter() {
	bits := uint64(64)
	for bits < uint64(len(index.byProbe))*bitsPerProbe {
		bits *= 2
	}
	index.probeMask = bits - 1
	index.probeBits = make([]uint64, bits/64)
	for probe := range index.byProbe {
		slot := index.slot(probe)
		index.probeBits[slot>>6] |= 1 << (slot & 63)
	}
}

// slot is the bit a probe hash occupies in the filter.
//
// The two halves of the rolling hash are folded together first. The hash is a
// polynomial, so a byte only reaches the bits above the ones it was added into:
// the byte that just entered the window is in the bottom eight bits and almost
// nowhere else, while the high half carries the whole window well mixed. Taking
// the low bits alone would put every text that differs only in its last byte in
// its own slot but leave the rest of the window barely represented; taking the
// high bits alone would ignore the last byte altogether. The fold keeps both.
func (index *anchorIndex) slot(h uint64) uint64 {
	return (h ^ (h >> 32)) & index.probeMask
}

// maybeInMap says whether a probe hash can be in byProbe. A clear bit is a fact
// -- the filter is built from exactly the probes the map holds, so no probe in
// the map has a clear bit -- and that is the direction the pass needs, because
// it is the direction that lets a position be dropped without a lookup. A set
// bit is only a maybe: probes share bits. Every maybe goes on to the map, so
// the candidates that come out are the ones the map alone would have named.
func (index *anchorIndex) maybeInMap(h uint64) bool {
	slot := index.slot(h)
	return index.probeBits[slot>>6]&(1<<(slot&63)) != 0
}

// candidates names the templates whose probe occurs in the text, as a set of
// indices into the template table. A nil result means the text is too short to
// hold any probe at all.
//
// The pass is exact in the direction that matters: a template whose anchor is
// in the text always has its probe in the text, so it is always named. The
// reverse does not hold -- two byte strings can hash alike, and a probe is only
// the opening of an anchor -- which is why the caller still confirms.
//
// Nearly every position in a text opens no anchor at all, and once the map had
// replaced the 739 searches, asking it that question a byte at a time was the
// most expensive thing left in the run. So the filter is asked first, and only
// the positions it cannot rule out are looked up. It rules a position out on
// the strength of a clear bit, which is the direction it is exact in, and it
// never rules one in: the map still decides, and names the same templates it
// named before.
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
	if index.maybeInMap(h) {
		for _, position := range index.byProbe[h] {
			found[position] = true
		}
	}
	for i := probeK; i < len(text); i++ {
		h = (h-uint64(text[i-probeK])*pow)*gramBase + uint64(text[i])
		if index.maybeInMap(h) {
			for _, position := range index.byProbe[h] {
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

// deprecatedByID says, for each identifier, whether SPDX has superseded it. It
// is derived from the template table, which never changes after it is loaded,
// so it is built once rather than per call.
var deprecatedByID = sync.OnceValue(func() map[string]bool {
	entries, err := loadTemplates()
	if err != nil {
		return map[string]bool{}
	}
	byID := make(map[string]bool, len(entries))
	for _, e := range entries {
		byID[e.id] = e.deprecated
	}
	return byID
})
