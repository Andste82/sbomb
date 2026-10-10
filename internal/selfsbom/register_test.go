package selfsbom

// The self-SBOM is assembled as a format-neutral document and rendered by
// whichever writer the command line selects, so this package registers none.
// The test binary registers CycloneDX explicitly, here, for the tests that
// render what was assembled.
import _ "github.com/example/sbomb/internal/cyclonedx"
