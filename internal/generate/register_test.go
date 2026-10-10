package generate

// The run resolves its writer through the registry and never names a format,
// so nothing in this package's production code registers one. This test
// binary does, explicitly: before, the CycloneDX writer reached the registry
// here only because an input adapter imports its package to read bundled
// documents, and a test that depends on that accident breaks the day the
// adapter stops needing it.
import _ "github.com/example/sbomb/internal/cyclonedx"
