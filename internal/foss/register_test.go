package foss

// The writer registry has to have the CycloneDX writer in it for
// foss-review.json, and this package resolves it by format identifier and
// never imports it. So the test binary registers it explicitly, here, rather
// than relying on some other import to have done it.
import _ "github.com/example/sbomb/internal/cyclonedx"
