// Package buildinfo carries the single authoritative build identity of the
// tool. Every place that reports a tool version -- the CLI, the CycloneDX
// metadata, the review report -- reads it from here, so a released binary can
// never disagree with the SBOM it produces about which tool wrote it.
package buildinfo

// Version is the released tool version. Release builds override it with
// -ldflags "-X github.com/example/sbomb/internal/buildinfo.Version=<version>",
// which derives it from the git tag, so a build from a working tree reports
// the version of the last release plus its distance from it.
var Version = "0.8.0"

// Name is the tool name recorded as the SBOM creator.
const Name = "sbomb"

// Vendor is the supplier recorded alongside Name.
const Vendor = "sbomb"

// License is the SPDX expression the tool itself is distributed under. A tool
// that reports the licence of everything it inspects should be able to state
// its own.
const License = "MIT"
