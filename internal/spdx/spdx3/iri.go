// Package spdx3 is everything about sbomb's SPDX documents that is SPDX 3.0.1
// spelling: the JSON-LD graph, the element and relationship classes, the
// vocabulary of purposes, hash algorithms and relationship types, the element
// identifiers, the embedded schema and the checks a 3.0.1 document is held to.
// What the document states comes from the version-neutral model of package
// mapping; this package decides only how 3.0.1 says it (section 28.11).
package spdx3

import (
	"strings"
)

// iriFor is the IRI of a local identity in the document namespace U:
// urn:uuid:U#<escaped local>. Every IRI is absolute. The official 3.0.1 schema
// holds @context to the plain context URL, so a prefix declared there -- the
// shorter spelling -- would make the document invalid (deviation D48).
func iriFor(namespace, local string) string {
	return "urn:uuid:" + namespace + "#" + escapeFragment(local)
}

// escapeFragment makes a local identity a legal RFC 3987 fragment, and keeps
// the mapping injective. The characters a fragment may carry as they are --
// letters, digits and - . _ ~ ! $ & ' ( ) * + , ; = : @ / ? -- stay; every
// other byte of the UTF-8 form becomes %XX, "%" and "#" included, so that two
// different local identities can never escape to the same fragment. Canonical
// paths are almost always left alone: anchors, slashes, colons and dots are all
// fragment characters.
func escapeFragment(local string) string {
	const hex = "0123456789ABCDEF"
	var builder strings.Builder
	builder.Grow(len(local))
	for i := 0; i < len(local); i++ {
		c := local[i]
		if fragmentSafe(c) {
			builder.WriteByte(c)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(hex[c>>4])
		builder.WriteByte(hex[c&0x0f])
	}
	return builder.String()
}

func fragmentSafe(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("-._~!$&'()*+,;=:@/?", c) >= 0
}

// unescapeFragment reverses escapeFragment. A malformed escape is kept as it
// is, so that reading a document somebody else wrote never fails here.
func unescapeFragment(fragment string) string {
	if !strings.Contains(fragment, "%") {
		return fragment
	}
	var builder strings.Builder
	for i := 0; i < len(fragment); i++ {
		if fragment[i] == '%' && i+2 < len(fragment) {
			if value, ok := hexByte(fragment[i+1], fragment[i+2]); ok {
				builder.WriteByte(value)
				i += 2
				continue
			}
		}
		builder.WriteByte(fragment[i])
	}
	return builder.String()
}

func hexByte(high, low byte) (byte, bool) {
	h, okHigh := hexDigit(high)
	l, okLow := hexDigit(low)
	return h<<4 | l, okHigh && okLow
}

func hexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// LocalIdentity is the local identity behind an element IRI of sbomb's scheme,
// urn:uuid:<uuid>#<local>, and the reference itself when it is not one. It is
// what the command line uses to read back an IRI somebody typed, so that it
// and the document reader decode a fragment by the same rule: a malformed
// escape is kept where it stands and every well-formed one is decoded.
func LocalIdentity(reference string) string {
	if !strings.HasPrefix(reference, "urn:uuid:") || !strings.Contains(reference, "#") {
		return reference
	}
	return localOf(reference)
}

// localOf is the local identity behind an IRI of sbomb's scheme: the
// unescaped fragment. Anything without a fragment is returned as it is.
func localOf(iri string) string {
	if _, fragment, found := strings.Cut(iri, "#"); found {
		return unescapeFragment(fragment)
	}
	return iri
}
