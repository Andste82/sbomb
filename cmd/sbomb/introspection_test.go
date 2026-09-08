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
// accepted here and enabled two commands nothing could call; "osPackages" was
// accepted and enabled two more.
func TestResolveIntrospectionKnowsOnlyTheGroupsThatCanRun(t *testing.T) {
	for _, group := range []string{"cmake", "osPackages", "os-packages"} {
		_, err := resolveIntrospection(config.Config{}, false, []string{group})
		if err == nil {
			t.Errorf("%q: a group with no command behind it was accepted", group)
			continue
		}
		if strings.Contains(err.Error(), group+",") || strings.Contains(err.Error(), " "+group+")") {
			t.Errorf("the error offers the removed group as a choice: %v", err)
		}
	}
	if _, err := resolveIntrospection(config.Config{}, false, []string{"nonsense"}); err == nil {
		t.Error("an unknown group was accepted")
	}

	features, err := resolveIntrospection(config.Config{}, false, []string{"git", "ninja", "compiler"})
	if err != nil {
		t.Fatal(err)
	}
	if features != (exec.Features{Ninja: true, Git: true, Compiler: true}) {
		t.Errorf("features = %+v; the three remaining groups have to stay reachable", features)
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

// --allow-introspection without a value is the widest permission the CLI can
// grant, so it is where a group with no caller stayed visible longest: the
// bare flag announced `dpkg -S <arg>` and `rpm -qf <arg>`, two commands the
// run could never have started against a system library, because a runner's
// anchors are the project and the build tree. The widest permission now names
// only what a caller exists for.
func TestTheWidestPermissionNamesOnlyCommandsWithACaller(t *testing.T) {
	features, err := resolveIntrospection(config.Config{}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if features != (exec.Features{Ninja: true, Git: true, Compiler: true}) {
		t.Errorf("features = %+v; the bare flag grants the three groups that have a caller", features)
	}
	announced := strings.Join(exec.AllowlistFor(features), "; ")
	for _, absent := range []string{"dpkg", "rpm", "cmake"} {
		if strings.Contains(announced, absent) {
			t.Errorf("the widest permission announces %q, which nothing can call: %s", absent, announced)
		}
	}
	// A configuration file cannot widen it either: the removed group is not a
	// field any more, so the only way in would be a key the loader refuses.
	fromConfig, err := resolveIntrospection(config.Config{}, false, nil)
	if err != nil || fromConfig.Enabled() {
		t.Errorf("resolved = %+v, err = %v; an empty configuration enables nothing", fromConfig, err)
	}
}
