// Package gobin reads the module evidence a Go link records inside the binary
// it produced.
//
// For a Go program this is the strongest evidence there is. It is not a
// description of what the build was asked to do -- a manifest, a lock file, a
// compile database -- it is what the linker wrote into the deliverable, so it
// cannot disagree with the deliverable. Section 4.1 asks for a chain from a
// file to the artifact; here the chain has length zero.
//
// Nothing in this package runs a subprocess, walks a source tree or touches
// the network. The same answer comes back on a machine with no Go toolchain
// installed, from a binary cross-compiled for a platform this one cannot run.
package gobin

import (
	"debug/buildinfo"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/example/sbomb/internal/limits"
)

// ErrNoBuildInfo reports a file that is not a Go binary, or one whose module
// information was stripped. It is a refusal, not a crash: the caller turns it
// into a finding.
var ErrNoBuildInfo = errors.New("the file carries no Go build information")

// Module is one module the linker recorded.
type Module struct {
	Path    string
	Version string
	// Sum is the go.sum entry for the module, "h1:" followed by base64. It
	// hashes the module's whole file tree, not any single file, so it is not
	// a CycloneDX component hash and must never be emitted as one.
	Sum string
	// ReplacedBy is set when a replace directive redirected this module. What
	// was compiled is the replacement; both are kept, because an SBOM that
	// names only one of them is ambiguous about which it means.
	ReplacedBy *Module
}

// Effective is the module whose code is in the binary: the replacement when
// there is one, otherwise the module itself.
func (m Module) Effective() Module {
	if m.ReplacedBy != nil {
		return *m.ReplacedBy
	}
	return m
}

// Binary is everything the linker recorded about one executable.
type Binary struct {
	// GoVersion is the toolchain that linked it, e.g. "go1.22.2".
	GoVersion string
	// MainPackage is the import path of the main package.
	MainPackage string
	// Main is the module the main package belongs to. Its version is
	// "(devel)" for a build from a working tree rather than a released tag.
	Main Module
	// Deps are the modules whose code was linked in, in the order the linker
	// recorded them.
	Deps []Module
	// Settings are the build settings: -buildmode, GOOS, GOARCH, CGO_ENABLED,
	// vcs.revision and the rest.
	Settings map[string]string
}

// Setting returns one build setting and whether it was recorded.
func (b *Binary) Setting(key string) (string, bool) {
	value, ok := b.Settings[key]
	return value, ok
}

// GOOS and GOARCH are the target platform, empty when the linker recorded
// neither -- which happens for a binary built by a toolchain older than the
// one that started recording them.
func (b *Binary) GOOS() string   { return b.Settings["GOOS"] }
func (b *Binary) GOARCH() string { return b.Settings["GOARCH"] }

// Devel reports a main module with no released version, which is what a build
// from a working tree gets. The caller has to supply the version itself.
func (b *Binary) Devel() bool {
	return b.Main.Version == "" || b.Main.Version == "(devel)"
}

// Inspect reads the build information out of a binary on disk. The limit
// policy of section 30 applies: an oversized file is refused before it is
// mapped, and a symbolic link is refused when strict symlinks are on.
func Inspect(path string, bounds limits.Config) (*Binary, error) {
	info, err := bounds.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > bounds.InputCeiling() {
		return nil, fmt.Errorf("%s: %w (%d bytes, ceiling %d)", path, limits.ErrInputLimitExceeded, info.Size(), bounds.InputCeiling())
	}
	file, err := bounds.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// buildinfo.Read seeks around the file rather than reading it whole, which
	// is why the size check above is a refusal to look at an implausible input
	// and not a memory bound.
	raw, err := buildinfo.Read(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %v", path, ErrNoBuildInfo, err)
	}
	return FromBuildInfo(raw), nil
}

// FromBuildInfo converts the standard library's representation into this one.
// It exists so that a test can build the evidence without writing an ELF file,
// and so that the conversion has one home rather than being inlined.
func FromBuildInfo(raw *debug.BuildInfo) *Binary {
	if raw == nil {
		return nil
	}
	out := &Binary{
		GoVersion:   raw.GoVersion,
		MainPackage: raw.Path,
		Main:        convertModule(&raw.Main),
		Settings:    make(map[string]string, len(raw.Settings)),
	}
	for _, dep := range raw.Deps {
		if dep == nil {
			continue
		}
		out.Deps = append(out.Deps, convertModule(dep))
	}
	for _, setting := range raw.Settings {
		out.Settings[setting.Key] = setting.Value
	}
	return out
}

func convertModule(raw *debug.Module) Module {
	if raw == nil {
		return Module{}
	}
	out := Module{
		Path:    strings.TrimSpace(raw.Path),
		Version: strings.TrimSpace(raw.Version),
		Sum:     strings.TrimSpace(raw.Sum),
	}
	// A replace chain is at most one link deep in the data the linker writes,
	// but following it defensively costs nothing and a cycle would otherwise
	// hang the caller.
	if raw.Replace != nil && raw.Replace != raw {
		replacement := convertModule(raw.Replace)
		out.ReplacedBy = &replacement
	}
	return out
}
