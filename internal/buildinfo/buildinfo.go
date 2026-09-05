// Package buildinfo carries the single authoritative build identity of the
// tool. Every place that reports a tool version -- the CLI, the CycloneDX
// metadata, the review report -- reads it from here, so a released binary can
// never disagree with the SBOM it produces about which tool wrote it.
package buildinfo

// Version is the released tool version. Release builds override it with
// -ldflags "-X github.com/example/sbomb/internal/buildinfo.Version=<version>".
var Version = "0.0.0-dev"

// Name is the tool name recorded as the SBOM creator.
const Name = "sbomb"

// Vendor is the supplier recorded alongside Name.
const Vendor = "sbomb"
