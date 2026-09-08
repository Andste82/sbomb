package generate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/adapters/compiledb"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/pathmodel"
)

func allIntrospection() exec.Features {
	return exec.Features{Ninja: true, Git: true, OSPackages: true, Compiler: true}
}

func findingsWithID(findings []domain.Finding, id string) []domain.Finding {
	out := make([]domain.Finding, 0, 1)
	for _, finding := range findings {
		if finding.ID == id {
			out = append(out, finding)
		}
	}
	return out
}

// The whole point of the fallbacks: they are second. A build directory that
// carries its own evidence must not start a process, however much
// introspection the caller allowed.
func TestPresentEvidenceStartsNoProcess(t *testing.T) {
	cfg, buildDir := portableFixture(t)
	result, err := RunWithOptions(cfg, buildDir, true, Options{Introspection: allIntrospection()})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Introspection) != 0 {
		t.Errorf("commands executed with every file source present: %v", result.Introspection)
	}
}

// Section 9.2: an adapter that degrades names the evidence it could not
// obtain. A deps log that cannot be read used to be passed over in silence.
func TestNinjaDepsUnavailableNamesTheMissingLog(t *testing.T) {
	cfg, fixture := portableFixture(t)
	buildDir := copyFixtureBuild(t, fixture)
	if err := os.Remove(filepath.Join(buildDir, ".ninja_deps")); err != nil {
		t.Fatal(err)
	}

	result, err := RunWithOptions(cfg, buildDir, true, Options{})
	if err != nil {
		t.Fatal(err)
	}
	reported := findingsWithID(result.Findings, "NINJA_DEPS_UNAVAILABLE")
	if len(reported) != 1 {
		t.Fatalf("got %d NINJA_DEPS_UNAVAILABLE finding(s), want exactly one", len(reported))
	}
	if reported[0].Severity != domain.SeverityInfo {
		t.Errorf("severity = %s, want info", reported[0].Severity)
	}
	// There is no permitted command that reads the log without rewriting it,
	// so the remediation must not offer one (deviation D29).
	if strings.Contains(reported[0].Remediation, "--allow-introspection") {
		t.Errorf("remediation offers introspection: %q", reported[0].Remediation)
	}
	if len(result.Introspection) != 0 {
		t.Errorf("a process was started with introspection off: %v", result.Introspection)
	}
}

// The same log, the same missing evidence, with every group enabled: still no
// process. `ninja -t deps` rewrites a log it cannot read, and a tool pointed at
// somebody's build directory does not repair it.
func TestAMissingDepsLogStartsNoProcessEvenWithEveryGroupOn(t *testing.T) {
	cfg, fixture := portableFixture(t)
	buildDir := copyFixtureBuild(t, fixture)
	if err := os.Remove(filepath.Join(buildDir, ".ninja_deps")); err != nil {
		t.Fatal(err)
	}

	result, err := RunWithOptions(cfg, buildDir, true, Options{Introspection: allIntrospection()})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range result.Introspection {
		if strings.Contains(command, "-t deps") {
			t.Errorf("a deps log was rewritten in a directory sbomb only reads: %q", command)
		}
	}
	if len(findingsWithID(result.Findings, "NINJA_DEPS_UNAVAILABLE")) != 1 {
		t.Error("the evidence is missing either way and has to be named either way")
	}
}

// A Makefiles build has no deps log that could be missing. Reporting one would
// name a gap that does not exist in this build.
func TestNinjaDepsUnavailableIsNotReportedForAMakefilesBuild(t *testing.T) {
	cfg := config.Config{Project: config.Project{Root: "/__fixture_src__"}}
	buildDir := filepath.Join("..", "..", "testdata", "fixtures", "gcc-make", "p02-static", "build")
	result, err := RunWithOptions(cfg, buildDir, true, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if reported := findingsWithID(result.Findings, "NINJA_DEPS_UNAVAILABLE"); len(reported) != 0 {
		t.Errorf("a Makefiles build reported %d missing Ninja deps log(s)", len(reported))
	}
}

// build.ninja names the objects an archive was built from. Without the file
// there is no build graph for ninja to read either, so the finding must not
// offer a group that could not answer. The case where it can is covered end to
// end in test/e2e, against a real ninja.
func TestArchiveMembersUnresolvedOffersNothingItCannotDeliver(t *testing.T) {
	cfg, fixture := portableFixture(t)
	buildDir := copyFixtureBuild(t, fixture)
	if err := os.Remove(filepath.Join(buildDir, "build.ninja")); err != nil {
		t.Fatal(err)
	}

	result, err := RunWithOptions(cfg, buildDir, true, Options{})
	if err != nil {
		t.Fatal(err)
	}
	reported := findingsWithID(result.Findings, "ARCHIVE_MEMBERS_UNRESOLVED")
	if len(reported) == 0 {
		t.Fatal("no archive member was reported as unresolved")
	}
	for _, finding := range reported {
		if strings.Contains(finding.Remediation, "--allow-introspection") {
			t.Errorf("remediation offers a group with no build graph to read: %q", finding.Remediation)
		}
	}
	if len(result.Introspection) != 0 {
		t.Errorf("a process was started with introspection off: %v", result.Introspection)
	}
}

// The compile database is the evidence; ninja is asked only in its absence,
// and the finding says which way was open.
func TestMissingCompileEvidenceNamesTheNinjaGroup(t *testing.T) {
	cfg, fixture := portableFixture(t)
	buildDir := copyFixtureBuild(t, fixture)
	if err := os.Remove(filepath.Join(buildDir, "compile_commands.json")); err != nil {
		t.Fatal(err)
	}

	result, err := RunWithOptions(cfg, buildDir, true, Options{})
	if err != nil {
		t.Fatal(err)
	}
	reported := findingsWithID(result.Findings, "MISSING_COMPILE_EVIDENCE")
	if len(reported) != 1 {
		t.Fatalf("got %d MISSING_COMPILE_EVIDENCE finding(s), want exactly one", len(reported))
	}
	if !strings.Contains(reported[0].Remediation, "--allow-introspection=ninja") {
		t.Errorf("remediation = %q", reported[0].Remediation)
	}
	if len(result.Introspection) != 0 {
		t.Errorf("a process was started with introspection off: %v", result.Introspection)
	}
}

// Section 13.2 counts `ninja -t commands` as strategy 2, the build graph
// itself. Its mappings must not carry the compile database's label, because
// that label is printed -- in the evidence dump and in the review report --
// for a build directory that has no such file at all.
func TestNinjaCommandMappingsAreNotLabelledCompileDatabase(t *testing.T) {
	dir := t.TempDir()
	commands := []compiledb.Command{{
		Directory: dir,
		File:      filepath.Join(dir, "a.c"),
		Output:    filepath.Join(dir, "a.o"),
	}}

	fromDatabase := collectCompileEvidence(dir, commands, "compile-commands-json", nil)
	if got := fromDatabase.strategy[filepath.Join(dir, "a.o")]; got != "compile-commands-json" {
		t.Errorf("compile database mapping labelled %q", got)
	}
	fromNinja := collectCompileEvidence(dir, commands, "ninja-buildgraph", nil)
	if got := fromNinja.strategy[filepath.Join(dir, "a.o")]; got != "ninja-buildgraph" {
		t.Errorf("`ninja -t commands` mapping labelled %q, want ninja-buildgraph", got)
	}
}

// ninjaBuildDir is a build directory that has the graph ninja reads and
// nothing else, so that the gate in front of every fallback can be exercised
// without a ninja to run.
func ninjaBuildDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.ninja"), []byte("rule cc\n  command = cc -c $in -o $out\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The three ways the compile fallback must ask nothing: outside a Ninja build,
// with the group off, and when the command is refused before a process exists.
// The last one is what an absent or unusable ninja looks like from here, and
// it has to leave the run without commands rather than without an answer.
func TestTheCompileFallbackAsksOnlyWhereItCanAnswer(t *testing.T) {
	deliverables := []Deliverable{{Path: "app", EvidencePath: "app"}}
	cases := []struct {
		name     string
		buildDir func(*testing.T) string
		runner   func(string) *exec.Runner
	}{{
		name:     "a build directory with no ninja graph in it",
		buildDir: func(t *testing.T) string { return t.TempDir() },
		runner: func(dir string) *exec.Runner {
			return &exec.Runner{Features: exec.Features{Ninja: true}, Anchors: []string{dir}}
		},
	}, {
		name:     "the ninja group off",
		buildDir: ninjaBuildDir,
		runner:   func(dir string) *exec.Runner { return &exec.Runner{Anchors: []string{dir}} },
	}, {
		name:     "another group on, but not this one",
		buildDir: ninjaBuildDir,
		runner: func(dir string) *exec.Runner {
			return &exec.Runner{Features: exec.Features{Git: true, Compiler: true}, Anchors: []string{dir}}
		},
	}, {
		name:     "the build directory outside every anchor",
		buildDir: ninjaBuildDir,
		runner: func(string) *exec.Runner {
			return &exec.Runner{Features: exec.Features{Ninja: true}, Anchors: []string{"/__no_such_anchor__"}}
		},
	}}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := testCase.buildDir(t)
			runner := testCase.runner(dir)
			commands := ninjaCompileCommands(context.Background(), dir, deliverables, runner, NewLogger(0, nil))
			if len(commands) != 0 {
				t.Errorf("commands = %+v; nothing could have answered", commands)
			}
			if len(runner.Records()) != 0 {
				t.Errorf("a process was started: %v", runner.Records())
			}
		})
	}
}

// A deliverable ninja does not know by that name is not asked about: a path
// outside the build directory is a target of some other build, or of none.
func TestTheCompileFallbackAsksOnlyAboutTargetsOfThisBuild(t *testing.T) {
	dir := ninjaBuildDir(t)
	runner := &exec.Runner{Features: exec.Features{Ninja: true}, Anchors: []string{dir}}
	commands := ninjaCompileCommands(context.Background(),
		dir, []Deliverable{{Path: "/opt/elsewhere/app", EvidencePath: "/opt/elsewhere/app"}}, runner, NewLogger(0, nil))
	if len(commands) != 0 || len(runner.Records()) != 0 {
		t.Errorf("commands = %+v, records = %v", commands, runner.Records())
	}
}

// A remediation is a promise. It may name the ninja group only where ninja has
// a graph to read and the group is what stands in the way.
func TestMissingCompileEvidenceOffersOnlyWhatCanAnswer(t *testing.T) {
	off := &exec.Runner{}
	on := &exec.Runner{Features: exec.Features{Ninja: true}}
	cases := []struct {
		name       string
		runner     *exec.Runner
		recovered  int
		ninjaBuild bool
		wantGroup  bool
	}{
		{name: "a ninja build with the group off", runner: off, ninjaBuild: true, wantGroup: true},
		{name: "a build with no ninja graph", runner: off, ninjaBuild: false},
		{name: "the group already on", runner: on, ninjaBuild: true},
		{name: "the command already answered", runner: off, recovered: 3, ninjaBuild: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			finding := missingCompileEvidenceFinding(config.Config{}, "/build", testCase.runner, testCase.recovered, testCase.ninjaBuild)
			if finding.ID != "MISSING_COMPILE_EVIDENCE" {
				t.Fatalf("id = %q", finding.ID)
			}
			offersGroup := strings.Contains(finding.Remediation, "--allow-introspection=ninja")
			if offersGroup != testCase.wantGroup {
				t.Errorf("remediation = %q, want it to name the group: %v", finding.Remediation, testCase.wantGroup)
			}
			if testCase.recovered > 0 && finding.Detail["introspection"] == nil {
				t.Error("the command supplied the compile lines and the finding does not say so")
			}
		})
	}
}

// A deps log that cannot be read is the case section 9.2 names in so many
// words. It used to be passed over without a log line, let alone a finding:
// the run simply had no header evidence and never said why.
func TestAnUnreadableDepsLogIsNamedRatherThanSwallowed(t *testing.T) {
	dir := ninjaBuildDir(t)
	if err := os.WriteFile(filepath.Join(dir, ".ninja_deps"), []byte("not a deps log"), 0o600); err != nil {
		t.Fatal(err)
	}
	compile := collectCompileEvidence(dir, nil, "", NewLogger(0, nil))
	reported := findingsWithID(compile.findings, "NINJA_DEPS_UNAVAILABLE")
	if len(reported) != 1 {
		t.Fatalf("got %d NINJA_DEPS_UNAVAILABLE finding(s), want exactly one: %+v", len(reported), compile.findings)
	}
	if reported[0].Severity != domain.SeverityInfo {
		t.Errorf("severity = %s, want info", reported[0].Severity)
	}
	if !strings.Contains(reported[0].Message, "headers") {
		t.Errorf("the message does not name the evidence that is missing: %q", reported[0].Message)
	}
}

// The same finding for a log that is not there at all, and none where there is
// no Ninja build to have written one.
func TestTheDepsLogIsOnlyMissedWhereNinjaWouldHaveWrittenOne(t *testing.T) {
	if reported := findingsWithID(collectCompileEvidence(ninjaBuildDir(t), nil, "", NewLogger(0, nil)).findings, "NINJA_DEPS_UNAVAILABLE"); len(reported) != 1 {
		t.Errorf("got %d finding(s) for a Ninja build without a deps log", len(reported))
	}
	if reported := findingsWithID(collectCompileEvidence(t.TempDir(), nil, "", NewLogger(0, nil)).findings, "NINJA_DEPS_UNAVAILABLE"); len(reported) != 0 {
		t.Errorf("a build that is not a Ninja build was told its deps log is missing: %+v", reported)
	}
	// And a readable log is not reported at all.
	_, fixture := portableFixture(t)
	if reported := findingsWithID(collectCompileEvidence(fixture, nil, "", NewLogger(0, nil)).findings, "NINJA_DEPS_UNAVAILABLE"); len(reported) != 0 {
		t.Errorf("a log that was read was reported as unavailable: %+v", reported)
	}
}

// The archive fallback stands behind the same gate: no graph, no group, no
// process. build.ninja is the file source, and it is asked first everywhere.
func TestTheArchiveFallbackAsksOnlyWhereItCanAnswer(t *testing.T) {
	const canonical = "build:libx.a"
	cases := []struct {
		name     string
		buildDir func(*testing.T) string
		features exec.Features
	}{
		{name: "no ninja graph to read", buildDir: func(t *testing.T) string { return t.TempDir() }, features: exec.Features{Ninja: true}},
		{name: "the ninja group off", buildDir: ninjaBuildDir},
		{name: "another group on, but not this one", buildDir: ninjaBuildDir, features: exec.Features{Git: true}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := testCase.buildDir(t)
			runner := &exec.Runner{Features: testCase.features, Anchors: []string{dir}}
			b := newBuilder(evidence.New(), assembledAnchors(t), dir, dir, NewLogger(0, nil))
			b.setIntrospection(runner, context.Background())
			b.physical[canonical] = filepath.Join(dir, "libx.a")
			if objects := b.ninjaArchiveInputs(canonical); len(objects) != 0 {
				t.Errorf("objects = %v; nothing could have named them", objects)
			}
			if len(runner.Records()) != 0 {
				t.Errorf("a process was started: %v", runner.Records())
			}
		})
	}
}

// An archive whose inputs build.ninja did state is answered from the file, and
// no fallback is reached -- the file source is first, always.
func TestAnArchiveNamedByTheBuildGraphIsNotPutToNinja(t *testing.T) {
	dir := ninjaBuildDir(t)
	runner := &exec.Runner{Features: exec.Features{Ninja: true}, Anchors: []string{dir}}
	b := newBuilder(evidence.New(), assembledAnchors(t), dir, dir, NewLogger(0, nil))
	b.setIntrospection(runner, context.Background())
	b.archiveInputs["build:libx.a"] = []string{"a.o", "b.o"}

	object, ok := b.memberObject("build:libx.a", "a.o")
	if !ok || object != "a.o" {
		t.Errorf("memberObject = %q, %v", object, ok)
	}
	if len(runner.Records()) != 0 {
		t.Errorf("the build graph answered and a process ran anyway: %v", runner.Records())
	}
}

func assembledAnchors(t *testing.T) *anchors.Result {
	t.Helper()
	result, err := anchors.Assemble(anchors.Options{
		Flavor: pathmodel.DefaultFlavor(), ProjectRoot: "/src", BuildRoot: "/bd",
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
