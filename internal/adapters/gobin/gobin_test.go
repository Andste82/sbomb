package gobin

import (
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/example/sbomb/internal/limits"
)

// The test binary is itself a Go executable produced by the linker, so it is
// the one piece of real evidence every machine running these tests is
// guaranteed to have. Reading it proves Inspect against a linker's output
// rather than against a hand-written struct.
//
// What it can assert is deliberately narrow. A test binary is not a release
// binary: the go command records no main module path for it, and a package
// whose imports all live inside this module has no dependency modules to
// record. The shape of the data is covered by FromBuildInfo below; this covers
// the part only a real executable can, which is that the container parses.
func TestInspectReadsTheTestBinary(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	binary, err := Inspect(executable, limits.Config{})
	if err != nil {
		t.Fatalf("inspect %s: %v", executable, err)
	}
	if binary.GoVersion == "" {
		t.Error("the linker recorded no Go version")
	}
	for _, dep := range binary.Deps {
		if dep.Path == "" {
			t.Error("a dependency was recorded with no module path")
		}
	}
}

func TestInspectRefusesAFileThatIsNotAGoBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-binary")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Inspect(path, limits.Config{})
	if !errors.Is(err, ErrNoBuildInfo) {
		t.Fatalf("error = %v, want ErrNoBuildInfo", err)
	}
}

// An implausible input is refused before it is opened, not after it has been
// read, which is the whole point of the ceiling.
func TestInspectRefusesAnInputOverTheCeiling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(path, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Inspect(path, limits.Config{MaxInput: 1024})
	if !errors.Is(err, limits.ErrInputLimitExceeded) {
		t.Fatalf("error = %v, want ErrInputLimitExceeded", err)
	}
}

func TestFromBuildInfoFollowsAReplaceDirective(t *testing.T) {
	replacement := &debug.Module{Path: "example.com/fork", Version: "v1.2.3", Sum: "h1:fork"}
	raw := &debug.BuildInfo{
		GoVersion: "go1.22.2",
		Path:      "example.com/app/cmd/app",
		Main:      debug.Module{Path: "example.com/app", Version: "(devel)"},
		Deps: []*debug.Module{
			{Path: "example.com/original", Version: "v0.1.0", Replace: replacement},
			{Path: "example.com/plain", Version: "v2.0.0", Sum: "h1:plain"},
		},
		Settings: []debug.BuildSetting{{Key: "GOOS", Value: "linux"}, {Key: "vcs.modified", Value: "true"}},
	}

	binary := FromBuildInfo(raw)
	if !binary.Devel() {
		t.Error("a main module at (devel) should report Devel")
	}
	if binary.GOOS() != "linux" {
		t.Errorf("GOOS = %q, want linux", binary.GOOS())
	}

	replaced := binary.Deps[0]
	if replaced.ReplacedBy == nil {
		t.Fatal("the replace directive was dropped")
	}
	// What was compiled is the replacement, and what was asked for is still
	// recorded: an SBOM naming only one of them is ambiguous.
	effective := replaced.Effective()
	if effective.Path != "example.com/fork" || effective.Version != "v1.2.3" {
		t.Errorf("effective module = %s@%s, want example.com/fork@v1.2.3", effective.Path, effective.Version)
	}
	if replaced.Path != "example.com/original" {
		t.Errorf("original module path = %q, want example.com/original", replaced.Path)
	}
	if plain := binary.Deps[1].Effective(); plain.Path != "example.com/plain" {
		t.Errorf("unreplaced module = %q, want example.com/plain", plain.Path)
	}
}

// A module that replaces itself is not something the go command writes, but a
// crafted binary could carry it and the conversion must not recurse forever.
func TestFromBuildInfoSurvivesASelfReplacement(t *testing.T) {
	module := &debug.Module{Path: "example.com/loop", Version: "v1.0.0"}
	module.Replace = module
	binary := FromBuildInfo(&debug.BuildInfo{
		Main: debug.Module{Path: "example.com/app"},
		Deps: []*debug.Module{module},
	})
	if got := binary.Deps[0].Effective().Path; got != "example.com/loop" {
		t.Errorf("effective module = %q, want example.com/loop", got)
	}
}
