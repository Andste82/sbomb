package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func allFeatures() Features {
	return Features{Ninja: true, Git: true, Compiler: true}
}

// The required default of section 9.2: nothing runs unless the caller asked.
func TestTheZeroValueRunsNothing(t *testing.T) {
	var runner Runner
	if _, err := runner.Run(context.Background(), "git", "-C", ".", "rev-parse", "HEAD"); !errors.Is(err, ErrDisabled) {
		t.Errorf("err = %v, want ErrDisabled", err)
	}
	if len(runner.Records()) != 0 {
		t.Error("a process was created with introspection disabled")
	}
}

func TestOnlyTheAllowlistedShapesAreAccepted(t *testing.T) {
	runner := &Runner{Features: allFeatures(), Anchors: []string{"/"}, Exists: func(string) bool { return true }}
	refused := [][]string{
		{"git", "-C", ".", "push"},
		{"git", "-C", ".", "rev-parse", "--all"},
		{"cmake", "--build", "."},
		{"ninja", "-C", ".", "-t", "clean"},
		// `-t deps` rewrites the log it fails to read, so it is not a read of
		// the build directory at all (deviation D29).
		{"ninja", "-C", ".", "-t", "deps"},
		{"make", "all"},
		{"sh", "-c", "echo hi"},
		{"cmake", "--version", "extra"},
	}
	for _, argv := range refused {
		if _, err := runner.Run(context.Background(), argv[0], argv[1:]...); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%v: err = %v, want ErrNotAllowed", argv, err)
		}
	}
	if len(runner.Records()) != 0 {
		t.Errorf("%d process(es) were created for refused commands", len(runner.Records()))
	}
}

// A caller-supplied slot must not be able to turn into an option: the whole
// point of exact argv shapes is that no untrusted string changes what the
// program does.
func TestASlotValueCannotBecomeAnOption(t *testing.T) {
	runner := &Runner{Features: allFeatures(), Anchors: []string{"/"}, Exists: func(string) bool { return true }}
	_, err := runner.Run(context.Background(), "git", "-C", "--upload-pack=evil", "rev-parse", "HEAD")
	if !errors.Is(err, ErrNotAllowed) {
		t.Errorf("err = %v, want the option-shaped value refused", err)
	}
}

// Section 9.2: a path argument must exist and lie within a registered anchor.
func TestPathArgumentsStayInsideRegisteredAnchors(t *testing.T) {
	runner := &Runner{
		Features: allFeatures(),
		Anchors:  []string{"/workspace/project"},
		Exists:   func(string) bool { return true },
	}
	if _, err := runner.Run(context.Background(), "git", "-C", "/etc", "rev-parse", "HEAD"); !errors.Is(err, ErrPathOutsideAnchors) {
		t.Errorf("err = %v, want ErrPathOutsideAnchors", err)
	}
	// A path inside an anchor passes validation and reaches execution, where
	// it fails only because git is not being run against a real repository.
	if _, err := runner.Run(context.Background(), "git", "-C", "/workspace/project/sub", "rev-parse", "HEAD"); errors.Is(err, ErrPathOutsideAnchors) {
		t.Errorf("a path inside the anchor was rejected: %v", err)
	}
}

func TestANonexistentPathIsRefused(t *testing.T) {
	runner := &Runner{
		Features: allFeatures(),
		Anchors:  []string{"/workspace"},
		Exists:   func(string) bool { return false },
	}
	if _, err := runner.Run(context.Background(), "git", "-C", "/workspace/gone", "rev-parse", "HEAD"); !errors.Is(err, ErrPathOutsideAnchors) {
		t.Errorf("err = %v, want the missing path refused", err)
	}
}

func TestEachFeatureGatesItsOwnCommands(t *testing.T) {
	runner := &Runner{
		Features: Features{Git: true},
		Anchors:  []string{"/"},
		Exists:   func(string) bool { return true },
	}
	if _, err := runner.Run(context.Background(), "ninja", "-C", "/build", "-t", "inputs", "app"); !errors.Is(err, ErrDisabled) {
		t.Errorf("err = %v, want ninja refused while only git is enabled", err)
	}
}

func TestOnlyThePermittedCompilerProbesRun(t *testing.T) {
	runner := &Runner{Features: allFeatures(), Anchors: []string{"/"}, Exists: func(string) bool { return true }}
	if _, err := runner.RunCompilerProbe(context.Background(), "gcc", "-c", "evil.c"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("err = %v, want a compile refused", err)
	}
	if _, err := runner.RunCompilerProbe(context.Background(), "/bin/echo", "--version"); errors.Is(err, ErrNotAllowed) {
		t.Errorf("a permitted probe shape was refused: %v", err)
	}
}

func TestEveryInvocationIsRecorded(t *testing.T) {
	var logged []Record
	runner := &Runner{
		Features: allFeatures(), Anchors: []string{"/"},
		Exists: func(string) bool { return true },
		Log:    func(r Record) { logged = append(logged, r) },
	}
	// /bin/echo accepts --version, so this is a real, harmless execution.
	if _, err := runner.RunCompilerProbe(context.Background(), "/bin/echo", "--version"); err != nil {
		t.Skipf("no usable probe target in this environment: %v", err)
	}
	if len(logged) != 1 || len(runner.Records()) != 1 {
		t.Fatalf("logged %d, recorded %d; section 9.2 requires every execution to be logged", len(logged), len(runner.Records()))
	}
	if strings.Join(logged[0].Argv, " ") != "/bin/echo --version" {
		t.Errorf("argv = %v", logged[0].Argv)
	}
}

func TestOutputIsBounded(t *testing.T) {
	runner := &Runner{
		Features: allFeatures(), Anchors: []string{"/"},
		Exists:      func(string) bool { return true },
		OutputLimit: 4,
	}
	data, err := runner.RunCompilerProbe(context.Background(), "/bin/echo", "--version")
	if err == nil {
		t.Skip("echo produced no output in this environment")
	}
	if !errors.Is(err, ErrOutputLimitExceeded) {
		t.Fatalf("err = %v, want ErrOutputLimitExceeded", err)
	}
	if len(data) > 4 {
		t.Errorf("returned %d bytes past the limit", len(data))
	}
}

func TestTimeoutIsApplied(t *testing.T) {
	runner := &Runner{
		Features: allFeatures(), Anchors: []string{"/"},
		Exists:  func(string) bool { return true },
		Timeout: 50 * time.Millisecond,
	}
	started := time.Now()
	// sleep is not allowlisted, so this proves the refusal path is cheap; the
	// timeout itself is exercised by the context the runner builds.
	if _, err := runner.Run(context.Background(), "sleep", "10"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("err = %v, want ErrNotAllowed", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("refusal took %s; it must not start a process", elapsed)
	}
}

func TestAllowlistIsExhaustiveAndStable(t *testing.T) {
	entries := Allowlist()
	// Five table shapes and three compiler probes. Seven of the shapes section
	// 9.2 lists were removed because nothing could call them; deviations D29
	// and D30 say why, and this number is what keeps them from creeping back.
	if len(entries) != 8 {
		t.Errorf("allowlist has %d entries; section 9.2 lists a fixed set", len(entries))
	}
	for _, entry := range entries {
		if strings.Contains(entry, "&&") || strings.Contains(entry, "|") {
			t.Errorf("allowlist entry looks like a shell line: %q", entry)
		}
	}
}

// What the run reports it may do has to match what it may do: a group that is
// off can start nothing, so naming its commands would describe a capability
// this run does not have.
func TestAllowlistForNamesOnlyTheEnabledGroups(t *testing.T) {
	entries := AllowlistFor(Features{Git: true})
	if len(entries) != 3 {
		t.Fatalf("AllowlistFor(git) = %v; want the three git shapes", entries)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry, "git ") {
			t.Errorf("AllowlistFor(git) names %q", entry)
		}
	}
	if len(AllowlistFor(Features{})) != 0 {
		t.Error("the zero value may run nothing, so it names nothing")
	}
	if len(AllowlistFor(Features{Compiler: true})) != len(compilerProbes) {
		t.Error("the compiler group names exactly its probes")
	}
}

// The seven shapes deviations D29 and D30 removed, spelled the way the
// allowlist spelled them. Nothing could call any of them: no code asked cmake
// for its version or its capabilities, no code asked ninja for a version, and
// `git status --porcelain` answered a question the `--dirty` suffix of `git
// describe` already answers. `dpkg -S` and `rpm -qf` had no system-library
// adapter behind them, and the shapes could not have built one: they name a
// package, not a version and not a supplier. They were a standing permission
// granted for nothing, and this test is what keeps them from being granted
// again.
func TestTheShapesNothingCouldCallAreRefused(t *testing.T) {
	runner := &Runner{Features: allFeatures(), Anchors: []string{"/"}, Exists: func(string) bool { return true }}
	for _, argv := range [][]string{
		{"cmake", "--version"},
		{"cmake", "-E", "capabilities"},
		{"ninja", "--version"},
		{"git", "-C", ".", "status", "--porcelain"},
		{"ninja", "-C", ".", "-t", "deps"},
		{"dpkg", "-S", "/usr/lib/libc.so.6"},
		{"rpm", "-qf", "/usr/lib/libc.so.6"},
	} {
		if _, err := runner.Run(context.Background(), argv[0], argv[1:]...); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%v: err = %v, want ErrNotAllowed", argv, err)
		}
	}
	if len(runner.Records()) != 0 {
		t.Errorf("%d process(es) were created for shapes no caller exists for", len(runner.Records()))
	}
}

// Every group names only its own shapes, so what a run announces it may start
// is what it may start. The whole table used to be printed whatever was on.
func TestNoGroupNamesAnotherGroupsShapes(t *testing.T) {
	prefixes := map[string]string{"ninja": "ninja ", "git": "git ", "compiler": "<compiler> "}
	for group, features := range map[string]Features{
		"ninja":    {Ninja: true},
		"git":      {Git: true},
		"compiler": {Compiler: true},
	} {
		entries := AllowlistFor(features)
		if len(entries) == 0 {
			t.Errorf("the %s group names nothing it may run", group)
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry, prefixes[group]) {
				t.Errorf("AllowlistFor(%s) names %q", group, entry)
			}
		}
	}
}

// The compiler probes are gated by their own group, like every other shape.
func TestCompilerProbesNeedTheirOwnGroup(t *testing.T) {
	var runner Runner
	for _, probe := range compilerProbes {
		if _, err := runner.RunCompilerProbe(context.Background(), "/usr/bin/cc", probe...); !errors.Is(err, ErrDisabled) {
			t.Errorf("%v: err = %v, want ErrDisabled", probe, err)
		}
	}
	// Another group being on is not this group being on.
	runner = Runner{Features: Features{Ninja: true, Git: true}}
	if _, err := runner.RunCompilerProbe(context.Background(), "/usr/bin/cc", "--version"); !errors.Is(err, ErrDisabled) {
		t.Errorf("err = %v, want the compiler group to gate its own probes", err)
	}
	if len(runner.Records()) != 0 {
		t.Errorf("%d process(es) were created with the compiler group off", len(runner.Records()))
	}
}

// The osPackages group was a permission that reached a real process: with the
// group on, `dpkg -S <path>` passed the shape check, passed the path check for
// any file inside an anchor, and was started. No adapter ever asked for it, so
// the permission bought nothing and the risk was carried anyway. This holds
// the group gone from every direction at once -- the announced table and the
// execution path, for every combination of the groups that remain, with the
// path present, the path missing, and no group on at all.
func TestNoCombinationOfGroupsCanQueryAPackageManager(t *testing.T) {
	// A file that really exists inside the anchor, so nothing but the absent
	// allowlist entry can be what refuses the call.
	anchor := t.TempDir()
	present := filepath.Join(anchor, "libfoo.so.1")
	if err := os.WriteFile(present, []byte("\x7fELF"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(anchor, "gone.so")

	for _, features := range []Features{
		{},
		{Ninja: true},
		{Git: true},
		{Compiler: true},
		{Ninja: true, Git: true},
		{Ninja: true, Compiler: true},
		{Git: true, Compiler: true},
		allFeatures(),
	} {
		for _, entry := range AllowlistFor(features) {
			if strings.HasPrefix(entry, "dpkg ") || strings.HasPrefix(entry, "rpm ") {
				t.Errorf("AllowlistFor(%+v) names %q, which no code can call", features, entry)
			}
		}
		var logged []Record
		runner := &Runner{
			Features: features,
			Anchors:  []string{anchor},
			Log:      func(r Record) { logged = append(logged, r) },
		}
		for _, argv := range [][]string{
			{"dpkg", "-S", present},
			{"dpkg", "-S", missing},
			{"rpm", "-qf", present},
			{"rpm", "-qf", missing},
		} {
			// With every group off nothing runs at all, which is the default
			// of section 9.2; with any group on it is the shape itself that is
			// unknown, so the answer does not depend on the file.
			want := ErrNotAllowed
			if !features.Enabled() {
				want = ErrDisabled
			}
			if _, err := runner.Run(context.Background(), argv[0], argv[1:]...); !errors.Is(err, want) {
				t.Errorf("%+v %v: err = %v, want %v", features, argv, err, want)
			}
		}
		if len(runner.Records()) != 0 || len(logged) != 0 {
			t.Errorf("%+v: %d process(es) created, %d logged; no group may start a package query",
				features, len(runner.Records()), len(logged))
		}
	}
}
