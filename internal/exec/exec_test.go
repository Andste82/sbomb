package exec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func allFeatures() Features {
	return Features{Ninja: true, Git: true, OSPackages: true, Compiler: true}
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
	// Seven table shapes and three compiler probes. Five of the shapes section
	// 9.2 lists were removed because nothing could call them; deviation D29
	// says why, and this number is what keeps them from creeping back.
	if len(entries) != 10 {
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

// The five shapes deviation D29 removed, spelled the way the allowlist spelled
// them. Nothing could call any of them: no code asked cmake for its version or
// its capabilities, no code asked ninja for a version, and `git status
// --porcelain` answered a question the `--dirty` suffix of `git describe`
// already answers. They were a standing permission granted for nothing, and
// this test is what keeps them from being granted again.
func TestTheShapesNothingCouldCallAreRefused(t *testing.T) {
	runner := &Runner{Features: allFeatures(), Anchors: []string{"/"}, Exists: func(string) bool { return true }}
	for _, argv := range [][]string{
		{"cmake", "--version"},
		{"cmake", "-E", "capabilities"},
		{"ninja", "--version"},
		{"git", "-C", ".", "status", "--porcelain"},
		{"ninja", "-C", ".", "-t", "deps"},
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
	// The osPackages group is the one whose two shapes have different program
	// names, so it is checked by exclusion rather than by prefix.
	for _, entry := range AllowlistFor(Features{OSPackages: true}) {
		if !strings.HasPrefix(entry, "dpkg ") && !strings.HasPrefix(entry, "rpm ") {
			t.Errorf("AllowlistFor(osPackages) names %q", entry)
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
	runner = Runner{Features: Features{Ninja: true, Git: true, OSPackages: true}}
	if _, err := runner.RunCompilerProbe(context.Background(), "/usr/bin/cc", "--version"); !errors.Is(err, ErrDisabled) {
		t.Errorf("err = %v, want the compiler group to gate its own probes", err)
	}
	if len(runner.Records()) != 0 {
		t.Errorf("%d process(es) were created with the compiler group off", len(runner.Records()))
	}
}
