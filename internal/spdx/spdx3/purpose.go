package spdx3

import (
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// packagePurpose maps the component type of section 19.1 onto
// software_SoftwarePurpose. Where the two vocabularies name the same thing in
// different words the 3.0.1 word is used; a type with no counterpart is
// "other" rather than the nearest-looking word. It reads the stated type, in
// which an unset type is already settled (sbommap.EffectiveType), so the
// purpose, the BSI triple and the CycloneDX document of the run agree; it
// decides nothing about an unset type itself.
func packagePurpose(pkg mapping.Package) string {
	switch pkg.StatedType {
	case "application", "library", "framework", "firmware", "file", "device",
		"container", "platform", "data":
		return pkg.StatedType
	case "operating-system":
		return "operatingSystem"
	case "device-driver":
		return "deviceDriver"
	case "machine-learning-model":
		return "model"
	default:
		// cryptographic-asset, and any type a later CycloneDX revision adds.
		// The exact type is in sbomb:component:type, which the mapping
		// writes for every package with a type, and nowhere guessed at.
		return "other"
	}
}

// packageAdditionalPurposes says what a package also is. A header-only
// component is consumed as source (section 15): it contributes no object code,
// so "library" alone would overstate what reached the deliverable.
func packageAdditionalPurposes(pkg mapping.Package) []string {
	if pkg.HeaderOnly && packagePurpose(pkg) != "source" {
		return []string{"source"}
	}
	return nil
}

// filePurpose maps the file class of discovery onto software_SoftwarePurpose.
// An object file has no word of its own -- it is neither an executable nor a
// library until something links it -- and an unknown class states no purpose
// rather than a guessed one; the exact class is always in sbomb:file:class.
// A retained licence, notice or copyright file the build did not use is
// documentation. A file that is only evidence because a copyright line was
// read from it states no purpose: what it is was never established.
func filePurpose(file mapping.File) string {
	switch {
	case file.Origin&mapping.OriginPatch != 0:
		return "patch"
	case file.Origin&mapping.OriginUsed != 0:
		return classPurpose(domain.FileClass(file.Class))
	case file.Origin&mapping.OriginLicenseFile != 0:
		return "documentation"
	default:
		return ""
	}
}

func classPurpose(class domain.FileClass) string {
	switch class {
	case domain.FileClassSource, domain.FileClassHeader,
		domain.FileClassGeneratedSource, domain.FileClassGeneratedHeader:
		return "source"
	case domain.FileClassObject:
		return "other"
	case domain.FileClassArchive:
		return "archive"
	case domain.FileClassSharedLibrary:
		return "library"
	case domain.FileClassAsset:
		return "data"
	default:
		return ""
	}
}

// fileAdditionalPurposes keeps a used file's purpose primary when the file is
// also a retained licence file, and says the second thing it is in the
// additional purposes: a LICENSE compiled into the binary as an asset is data
// first and documentation too. A used source file a copyright line was read
// from is evidence for its component (hasEvidence says so) but no
// documentation, and its purpose is left alone.
func fileAdditionalPurposes(file mapping.File) []string {
	if file.Origin&mapping.OriginUsed != 0 && file.Origin&mapping.OriginLicenseFile != 0 && filePurpose(file) != "documentation" {
		return []string{"documentation"}
	}
	return nil
}
