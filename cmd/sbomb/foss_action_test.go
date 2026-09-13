package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/foss"
	"github.com/example/sbomb/internal/testutil"
)

// The composite action's attribution step, from two sides.
//
// A composite action cannot be run from a Go test -- there is no runner, and
// the step it would download does not exist until a release carries it. So the
// property is split in two, and neither half is a mock of the other: the
// behaviour is asserted by running the command the step runs, and the wiring
// -- that the step exists, that it is optional, and that nothing it does can
// fail the workflow -- is asserted against the action file itself. The CI job
// `foss-outputs` runs the first half again outside the test binary.

// actionFOSSArgs is the argument list the action's attribution step builds:
//
//	args=(foss --build-dir "$SBOMB_BUILD_DIR" --out "$SBOMB_FOSS_OUT" --policy "$SBOMB_POLICY")
//
// --source-dir is added here and not there because the corpus build directory
// is copied per test and the harvested source tree lives elsewhere; in a
// workflow the build directory sits inside the tree that was built.
func actionFOSSArgs(buildDir, sourceDir, outDir, profile string) []string {
	return []string{"foss", "--build-dir", buildDir, "--out", outDir, "--policy", profile,
		"--source-dir", sourceDir, "--reproducible"}
}

// TestTheActionsAttributionStepProducesFourFilesOnAnIncompleteFixture is the
// property a release pipeline depends on: the step leaves four files for the
// upload and exits 0, although the components it describes are incomplete.
//
// The strict profile is the interesting one. Its gates would fail this fixture
// in `generate` -- unknown versions, missing suppliers, a component whose
// licence resolved to nothing -- and `foss` evaluates none of them, because no
// exit code of the attribution outputs depends on licence content (§32.6).
func TestTheActionsAttributionStepProducesFourFilesOnAnIncompleteFixture(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "sbomb-foss")
	code, _, stderr := execute(actionFOSSArgs(
		testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
		testutil.CorpusSourceTree(t), outDir, "strict"))
	if code != 0 || stderr != "" {
		t.Fatalf("the attribution step = code %d, stderr %q; it must never gate a build", code, stderr)
	}
	names := foss.Files()
	if len(names) != 4 {
		t.Fatalf("the upload covers %d files, not four: %v", len(names), names)
	}
	for _, name := range names {
		info, err := os.Stat(filepath.Join(outDir, name))
		if err != nil {
			t.Errorf("%s was not written, so the upload would have nothing: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
	// "Incomplete" is not an assumption about the fixture: the marker says so.
	notices, err := os.ReadFile(filepath.Join(outDir, foss.NoticesFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(notices), "attribution incomplete") {
		t.Error("no component of this run is incomplete, so the test asserts nothing")
	}
}

// TestTheActionCannotGateTheBuildOnAttribution asserts the wiring the runner
// would honour. Attribution is informational, so both steps carry
// continue-on-error: without it a crash in the notices renderer would turn a
// build that produced a valid SBOM red.
func TestTheActionCannotGateTheBuildOnAttribution(t *testing.T) {
	path := filepath.Join(testutil.RepoRoot(t), ".github", "actions", "sbomb", "action.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	action := string(data)
	for _, want := range []string{
		// The input, and the directory it writes into.
		"\n  foss:\n",
		"\n  foss-out:\n",
		// Opt-in: a workflow that does not ask for attribution runs neither step.
		"if: inputs.foss == 'true'",
		// The upload, which is what makes the four files reachable afterwards.
		"uses: actions/upload-artifact@v4",
		"path: ${{ inputs.foss-out }}/",
	} {
		if !strings.Contains(action, want) {
			t.Errorf("the action does not contain %q", want)
		}
	}
	steps := strings.Split(action, "\n    - name: ")
	attribution := 0
	for _, step := range steps[1:] {
		if !strings.Contains(step, "inputs.foss == 'true'") {
			continue
		}
		attribution++
		if !strings.Contains(step, "continue-on-error: true") {
			t.Errorf("an attribution step can fail the workflow:\n%s", step)
		}
	}
	if attribution != 2 {
		t.Errorf("found %d attribution steps, want the run and the upload", attribution)
	}
}

// The tests above run the command the action runs and read what action.yaml
// declares. Neither executes the shell in it that assembles the arguments, and
// that line is the action's only content of its own.
//
// The smoke-test workflow is where it is executed: it calls the action for
// real, on two operating systems, against a published release. It covered the
// SBOM step alone, so this asserts that it asks for the attribution step too
// and looks at what that step wrote. Without the assertion, dropping
// `foss: "true"` from the workflow would remove the coverage and nothing would
// say so.
//
// It runs against a release rather than against the working tree, which is
// what the workflow is for and is also its limit: a break in the action is
// found when a release is made, not in the pull request that caused it
// (open question Q18).
func TestTheSmokeTestExercisesTheAttributionStep(t *testing.T) {
	path := filepath.Join(testutil.RepoRoot(t), ".github", "workflows", "smoke-test.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	if !strings.Contains(workflow, "uses: ./.github/actions/sbomb") {
		t.Fatal("the smoke test does not call the action, so it exercises none of its shell")
	}
	for _, want := range []string{
		`foss: "true"`,
		"foss-out: smoke-foss",
		foss.NoticesFile,
		foss.ReviewTextFile,
		foss.ReviewJSONFile,
		foss.ObligationsFile,
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("the smoke test does not mention %q, so the attribution step is unchecked there", want)
		}
	}
}
