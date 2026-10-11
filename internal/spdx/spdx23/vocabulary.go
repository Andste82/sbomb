package spdx23

import (
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// packagePurpose maps the stated component type of section 19.1 onto
// primaryPackagePurpose. SPDX 2.3 has fewer words than 3.0.1: platform, data,
// device-driver, machine-learning-model and cryptographic-asset have none, and
// are OTHER rather than the nearest-looking word. The exact type always
// travels in the sbomb:component:type annotation line. "OPERATING_SYSTEM" is
// the spelling of the v2.3 schema; the specification text writes
// "OPERATING-SYSTEM", which that schema refuses (deviation D57).
func packagePurpose(pkg mapping.Package) string {
	switch pkg.StatedType {
	case "application":
		return "APPLICATION"
	case "library":
		return "LIBRARY"
	case "framework":
		return "FRAMEWORK"
	case "firmware":
		return "FIRMWARE"
	case "container":
		return "CONTAINER"
	case "device":
		return "DEVICE"
	case "file":
		return "FILE"
	case "operating-system":
		return "OPERATING_SYSTEM"
	default:
		return "OTHER"
	}
}

// fileTypes maps a file onto the fileTypes of SPDX 2.3: its class, and
// DOCUMENTATION and TEXT for a retained licence, notice or copyright file. A
// file whose class is unknown and which is no licence file states none rather
// than a guessed one; the exact class is in the sbomb:file:class annotation
// line.
func fileTypes(file mapping.File) []string {
	set := map[string]bool{}
	switch domain.FileClass(file.Class) {
	case domain.FileClassSource, domain.FileClassHeader,
		domain.FileClassGeneratedSource, domain.FileClassGeneratedHeader:
		set["SOURCE"] = true
	case domain.FileClassObject, domain.FileClassSharedLibrary:
		set["BINARY"] = true
	case domain.FileClassArchive:
		set["ARCHIVE"] = true
	case domain.FileClassAsset:
		set["OTHER"] = true
	}
	if file.Origin&mapping.OriginLicenseFile != 0 {
		set["DOCUMENTATION"] = true
		set["TEXT"] = true
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// checksumAlgorithms maps the algorithm names discovery keys digests by onto
// the checksum algorithms of SPDX 2.3. A key that is not here -- SHA3-224,
// which 2.3 does not name, or one no format names -- is no checksum; the
// digest is stated in the element's comment instead, so that it is not lost.
var checksumAlgorithms = map[string]string{
	"SHA-1":       "SHA1",
	"SHA1":        "SHA1",
	"SHA-224":     "SHA224",
	"SHA224":      "SHA224",
	"SHA-256":     "SHA256",
	"SHA256":      "SHA256",
	"SHA-384":     "SHA384",
	"SHA384":      "SHA384",
	"SHA-512":     "SHA512",
	"SHA512":      "SHA512",
	"SHA3-256":    "SHA3-256",
	"SHA3-384":    "SHA3-384",
	"SHA3-512":    "SHA3-512",
	"BLAKE2B-256": "BLAKE2b-256",
	"BLAKE2B-384": "BLAKE2b-384",
	"BLAKE2B-512": "BLAKE2b-512",
	"BLAKE3":      "BLAKE3",
	"MD2":         "MD2",
	"MD4":         "MD4",
	"MD5":         "MD5",
	"MD6":         "MD6",
	"ADLER32":     "ADLER32",
}

// checksumAlgorithm is the 2.3 algorithm of a discovery key.
func checksumAlgorithm(key string) (string, bool) {
	algorithm, known := checksumAlgorithms[strings.ToUpper(key)]
	return algorithm, known
}

// checksumLengths are the hex lengths of the algorithms whose digest has a
// fixed length. MD6 and BLAKE3 are not here: their length varies.
var checksumLengths = map[string]int{
	"SHA1":        40,
	"SHA224":      56,
	"SHA256":      64,
	"SHA384":      96,
	"SHA512":      128,
	"SHA3-256":    64,
	"SHA3-384":    96,
	"SHA3-512":    128,
	"BLAKE2b-256": 64,
	"BLAKE2b-384": 96,
	"BLAKE2b-512": 128,
	"MD2":         32,
	"MD4":         32,
	"MD5":         32,
	"ADLER32":     8,
}
