package spdx3

import "strings"

// hashAlgorithms maps the algorithm names discovery keys digests by (the
// CycloneDX spelling, "SHA-256") onto the HashAlgorithm vocabulary of 3.0.1.
var hashAlgorithms = map[string]string{
	"MD2":         "md2",
	"MD4":         "md4",
	"MD5":         "md5",
	"MD6":         "md6",
	"SHA-1":       "sha1",
	"SHA1":        "sha1",
	"SHA-224":     "sha224",
	"SHA224":      "sha224",
	"SHA-256":     "sha256",
	"SHA256":      "sha256",
	"SHA-384":     "sha384",
	"SHA384":      "sha384",
	"SHA-512":     "sha512",
	"SHA512":      "sha512",
	"SHA3-224":    "sha3_224",
	"SHA3-256":    "sha3_256",
	"SHA3-384":    "sha3_384",
	"SHA3-512":    "sha3_512",
	"BLAKE2B-256": "blake2b256",
	"BLAKE2B-384": "blake2b384",
	"BLAKE2B-512": "blake2b512",
	"BLAKE3":      "blake3",
	"ADLER32":     "adler32",
}

// hashAlgorithm is the 3.0.1 algorithm of a discovery key. A key the
// vocabulary has no word for is "other", and the key itself travels in the
// hash's comment, so the digest is still stated and still checkable by
// somebody who knows the algorithm.
func hashAlgorithm(key string) (algorithm string, known bool) {
	if algorithm, known := hashAlgorithms[strings.ToUpper(key)]; known {
		return algorithm, true
	}
	return "other", false
}

// hashLengths are the hex lengths of the algorithms whose digest has a fixed
// length. md6, blake3, adler32, the post-quantum algorithms and "other" are
// not here: their length varies or is not a digest length at all.
var hashLengths = map[string]int{
	"md2":        32,
	"md4":        32,
	"md5":        32,
	"sha1":       40,
	"sha224":     56,
	"sha256":     64,
	"sha384":     96,
	"sha512":     128,
	"sha3_224":   56,
	"sha3_256":   64,
	"sha3_384":   96,
	"sha3_512":   128,
	"blake2b256": 64,
	"blake2b384": 96,
	"blake2b512": 128,
}
