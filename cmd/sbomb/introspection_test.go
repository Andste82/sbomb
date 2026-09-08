package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/testutil"
)

// resolveIntrospection is the only place a group name becomes a permission, so
// it is where a group that no longer exists has to be refused. "cmake" was
// accepted here and enabled two commands nothing could call.
func TestResolveIntrospectionKnowsOnlyTheGroupsThatCanRun(t *testing.T) {
	if _, err := resolveIntrospection(config.Config{}, false, []string{"cmake"}); err == nil {
		t.Error("a group with no command behind it was accepted")
	} else if strings.Contains(err.Error(), "cmake, ") {
		t.Errorf("the error offers the removed group as a choice: %v", err)
	}
	if _, err := resolveIntrospection(config.Config{}, false, []string{"nonsense"}); err == nil {
		t.Error("an unknown group was accepted")
	}

	features, err := resolveIntrospection(config.Config{}, false, []string{"git", "ninja", "osPackages", "compiler"})
	if err != nil {
		t.Fatal(err)
	}
	if features != (exec.Features{Ninja: true, Git: true, OSPackages: true, Compiler: true}) {
		t.Errorf("features = %+v; the four remaining groups have to stay reachable", features)
	}

	// The default of section 9.2 is off, whatever the build directory holds.
	if resolved, err := resolveIntrospection(config.Config{}, false, nil); err != nil || resolved.Enabled() {
		t.Errorf("resolved = %+v, err = %v; nothing is enabled unless asked", resolved, err)
	}
}

func TestAnUnknownIntrospectionGroupEndsTheRun(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	code, _, stderr := execute([]string{"generate",
		"--build-dir", buildDir, "--allow-introspection=cmake",
		"--output", filepath.Join(t.TempDir(), "out.cdx.json"), "--evidence-dump=off"})
	if code == 0 {
		t.Errorf("exit code = %d; the run continued with a group that does not exist", code)
	}
	if !strings.Contains(stderr, "unknown introspection group") {
		t.Errorf("stderr = %q", stderr)
	}
}

// What a run announces it may start has to be what it may start. The line used
// to print the whole allowlist as soon as any group was on, naming commands
// that run could not have started and, at that time, commands no run could
// have started at all.
func TestTheIntrospectionLineNamesOnlyTheEnabledGroup(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	cfg := filepath.Join("..", "..", "testdata", "config", "portable.json")
	code, log, stderr := execute([]string{"-v", "generate",
		"--build-dir", buildDir, "--config", cfg, "--policy", "lenient",
		"--allow-introspection=git",
		"--output", filepath.Join(t.TempDir(), "out.cdx.json"), "--evidence-dump=off"})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	line := ""
	for _, candidate := range strings.Split(log, "\n") {
		if strings.Contains(candidate, "Introspection enabled:") {
			line = candidate
		}
	}
	if line == "" {
		t.Fatalf("the run enabled a group and said nothing about it:\n%s", log)
	}
	if !strings.Contains(line, "git -C") {
		t.Errorf("the enabled group is not named: %q", line)
	}
	for _, absent := range []string{"ninja", "dpkg", "rpm", "<compiler>", "cmake"} {
		if strings.Contains(line, absent) {
			t.Errorf("the line names %q, which this run may not start: %q", absent, line)
		}
	}
}
