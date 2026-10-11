package spdx23

import (
	"strings"
)

// documentID is the SPDX identifier of the document itself.
const documentID = "SPDXRef-DOCUMENT"

// idPrefix starts every element identifier.
const idPrefix = "SPDXRef-"

const hexDigits = "0123456789ABCDEF"

// keptInID reports whether a byte of a local identity stands for itself in an
// SPDX identifier. "-" does not: it is the escape character.
func keptInID(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.'
}

// elementID is the SPDX identifier of a local identity (section 28.11.11):
// "SPDXRef-" and the local identity, every byte in [A-Za-z0-9.] kept and every
// other byte -- "-" included -- written as "-" and two upper-case hex digits.
// A "-" is therefore always followed by exactly two hex digits, so the
// encoding is a prefix code: two local identities never share an identifier,
// and every identifier decodes back to the one local identity it came from
// (LocalIdentity). It cannot be "SPDXRef-DOCUMENT", because every local
// identity contains a ":", which is escaped.
func elementID(local string) string {
	var builder strings.Builder
	builder.Grow(len(idPrefix) + len(local) + 8)
	builder.WriteString(idPrefix)
	for index := 0; index < len(local); index++ {
		c := local[index]
		if keptInID(c) {
			builder.WriteByte(c)
			continue
		}
		builder.WriteByte('-')
		builder.WriteByte(hexDigits[c>>4])
		builder.WriteByte(hexDigits[c&0x0f])
	}
	return builder.String()
}

// LocalIdentity decodes an element identifier sbomb wrote back into the local
// identity it encodes. It refuses anything elementID cannot have written --
// another prefix, a byte that should have been escaped, an escape that is not
// two upper-case hex digits -- so that a decoded identity always encodes back
// to the identifier it came from.
func LocalIdentity(id string) (string, bool) {
	rest, found := strings.CutPrefix(id, idPrefix)
	if !found || rest == "" || id == documentID {
		return "", false
	}
	out := make([]byte, 0, len(rest))
	for index := 0; index < len(rest); index++ {
		c := rest[index]
		if keptInID(c) {
			out = append(out, c)
			continue
		}
		if c != '-' || index+2 >= len(rest) {
			return "", false
		}
		high := strings.IndexByte(hexDigits, rest[index+1])
		low := strings.IndexByte(hexDigits, rest[index+2])
		if high < 0 || low < 0 {
			return "", false
		}
		out = append(out, byte(high<<4|low))
		index += 2
	}
	return string(out), true
}
