// Package digest is the one table of the file digests a run can compute. Both
// the hashing of used files and the mapping of retained licence files compute
// from it, so a digest a writer asks for is either known to both or to
// neither. It names algorithms the way discovery keys digests ("SHA-256"),
// and knows nothing about which format asks for which.
package digest

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
)

// SHA256 is the digest every run computes of every file it reads.
const SHA256 = "SHA-256"

// SHA1 is computed only when the writer of a run's document asks for it.
const SHA1 = "SHA-1"

// onRequest are the digests computed only when a writer asks for them.
var onRequest = map[string]func([]byte) string{
	SHA1: func(data []byte) string {
		sum := sha1.Sum(data)
		return hex.EncodeToString(sum[:])
	},
}

// Supported reports whether the named digest can be computed: SHA-256 always,
// the others on request.
func Supported(name string) bool {
	if name == SHA256 {
		return true
	}
	_, known := onRequest[name]
	return known
}

// OnRequest reports whether the named digest is one a run computes only
// because a writer asked for it. Such a digest says which format was chosen,
// so an output that must not move with the serialization leaves it out.
func OnRequest(name string) bool {
	_, known := onRequest[name]
	return known
}

// Compute returns the named digest of data, hex-encoded, and false for a name
// Supported does not know.
func Compute(name string, data []byte) (string, bool) {
	if name == SHA256 {
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), true
	}
	compute, known := onRequest[name]
	if !known {
		return "", false
	}
	return compute(data), true
}
