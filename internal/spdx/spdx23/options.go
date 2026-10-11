package spdx23

import "github.com/example/sbomb/internal/digest"

// What the shared mapping and the run have to know about SPDX 2.3. The
// spdx-json writer's version table takes these from here, and so do this
// package's tests, so the documents the tests render are the ones the writer
// writes.
const (
	// CustomAdditions is false: 2.3 has no AdditionRef, so an exception the
	// SPDX list does not carry cannot be named on its own
	// (mapping.Options.CustomAdditions).
	CustomAdditions = false
	// SelfContainedLicenseRefs is true: a LicenseRef resolves only through
	// the document's own hasExtractedLicensingInfos or a checksummed external
	// document, which sbomb cannot state
	// (mapping.Options.SelfContainedLicenseRefs).
	SelfContainedLicenseRefs = true
)

// FileDigests are the digests every file must carry beyond SHA-256: clause
// 8.4 requires a SHA-1 (mapping.Options.Digests). Each call returns a new
// slice.
func FileDigests() []string { return []string{digest.SHA1} }
