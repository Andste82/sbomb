// Package licenselist answers one question for every writer that needs it:
// is an identifier on the SPDX licence list, and is it a licence or an
// exception.
//
// The question is the same whichever format asks it, and the answer must be
// the same too. An SPDX document routes an identifier the list does not carry
// through a LicenseRef, and a CycloneDX document moves it out of the
// enumerated license.id field; a writer that thought "MIT-0" was listed while
// the other did not would describe one build two ways. CycloneDX keeps reading
// the enum of the schema that validates its documents, and a test here pins
// that this list and that enum are the same set, so the two cannot drift
// apart without a test failing.
//
// The tables are generated from one tagged release of the list and replaced
// as a whole when it moves (list_gen.go says how).
//
// Two questions are asked of it, and they differ in case. Known and
// KnownException are exact: they answer whether a spelling is the list's own,
// which is what an enumerated field such as CycloneDX's license.id needs and
// what a document that promises the list's spelling is checked against.
// Canonical and CanonicalException match without case, because SPDX 3.0.1
// annex B (and SPDX 2.3 annex D) says licence and exception identifiers
// "should be matched in a case-insensitive manner": "mit" in a header is the
// listed MIT licence, not an unknown one, and a writer that took it for a
// custom licence would tell a reader the component is under something nobody
// can look up. They return the list's spelling, so a writer can state the
// licence the way every reader resolves it.
package licenselist

import (
	"sort"
	"strings"
	"sync"
)

// Known reports whether id is a licence identifier on the list. Exceptions
// are not licences: "LLVM-exception" alone licenses nothing.
func Known(id string) bool { return contains(licenses, id) }

// KnownException reports whether id is a licence exception on the list, the
// operand that may follow WITH.
func KnownException(id string) bool { return contains(exceptions, id) }

// Licenses returns the licence identifiers of the list, sorted.
func Licenses() []string { return append([]string(nil), licenses...) }

// Exceptions returns the exception identifiers of the list, sorted.
func Exceptions() []string { return append([]string(nil), exceptions...) }

// Canonical reports whether id, compared without case, is a licence
// identifier on the list, and returns the list's spelling of it.
func Canonical(id string) (string, bool) { return lookup(id, &licenseFolded) }

// CanonicalException reports whether id, compared without case, is a licence
// exception on the list, and returns the list's spelling of it.
func CanonicalException(id string) (string, bool) { return lookup(id, &exceptionFolded) }

var (
	foldOnce        sync.Once
	licenseFolded   map[string]string
	exceptionFolded map[string]string
)

func lookup(id string, table *map[string]string) (string, bool) {
	foldOnce.Do(func() {
		licenseFolded = fold(licenses)
		exceptionFolded = fold(exceptions)
	})
	canonical, ok := (*table)[strings.ToLower(id)]
	return canonical, ok
}

// fold maps the lower-case form of every identifier to the identifier. No two
// entries of one table differ only in case (a test pins it), so the map loses
// nothing.
func fold(sorted []string) map[string]string {
	folded := make(map[string]string, len(sorted))
	for _, id := range sorted {
		folded[strings.ToLower(id)] = id
	}
	return folded
}

func contains(sorted []string, id string) bool {
	index := sort.SearchStrings(sorted, id)
	return index < len(sorted) && sorted[index] == id
}
