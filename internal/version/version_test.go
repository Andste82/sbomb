package version

import (
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
)

func TestResolveVersionHeaderMacro(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "include", "demo", "version.h")
	if err := os.MkdirAll(filepath.Dir(header), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(header, []byte("#define DEMO_VERSION_STRING \"7.8.9\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rules := []string{"header:include/demo/version.h:DEMO_VERSION_STRING"}
	got, ok := Resolve(rules, dir, Options{})
	if !ok || got.Version != "7.8.9" || got.Source != "header" || got.Confidence != domain.ConfidenceMedium {
		t.Fatalf("header version failed: got %+v ok=%v", got, ok)
	}
}

func TestResolveVersionHonorsRestrictionList(t *testing.T) {
	rules := []string{"header:include/demo/version.h:DEMO_VERSION_STRING"}
	if got, ok := Resolve(rules, t.TempDir(), Options{}); ok || got.Version != "" {
		t.Fatalf("expected no version when the header is missing and the rule list allows nothing else, got %+v ok=%v", got, ok)
	}
}

// TestGitRulesProduceNothingWithoutIntrospection is the rule this package
// exists to keep. The root here carries a .git directory, which is exactly the
// condition the old implementation took for an answer: it then handed back the
// directory path as the version, while the commit rule handed back a constant
// regardless of the root. Both counted as success and so hid UNKNOWN_VERSION.
// Neither may produce anything when git was never asked.
func TestGitRulesProduceNothingWithoutIntrospection(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	options := map[string]Options{
		"no runner":     {},
		"zero runner":   {Runner: &exec.Runner{}, Context: context.Background()},
		"another group": {Runner: &exec.Runner{Features: exec.Features{Ninja: true}, Anchors: []string{root}}, Context: context.Background()},
	}
	for _, rule := range []string{"git", "commit"} {
		for name, option := range options {
			got, ok := Resolve([]string{rule}, root, option)
			if ok || got.Version != "" {
				t.Errorf("rule %q with %s: got %+v ok=%v, want no version at all", rule, name, got, ok)
			}
			if got.Version == root {
				t.Errorf("rule %q with %s: the component root was published as its version", rule, name)
			}
		}
	}
}

// TestGitRulesStartOneAllowlistedCommandAndSurviveItsFailure covers the wiring
// and the failure at once, without needing git to be installed: the runner
// records an invocation only after the argv shape, the enabled group and the
// anchor check have all passed, and a command that answers nothing useful --
// a directory that is no repository, or no git at all -- yields no version.
func TestGitRulesStartOneAllowlistedCommandAndSurviveItsFailure(t *testing.T) {
	for rule, argv := range map[string][]string{
		"git":    {"git", "-C", "", "describe", "--tags", "--always", "--dirty"},
		"commit": {"git", "-C", "", "rev-parse", "HEAD"},
	} {
		root := t.TempDir()
		runner := &exec.Runner{Features: exec.Features{Git: true}, Anchors: []string{root}}
		got, ok := Resolve([]string{rule}, root, Options{Runner: runner, Context: context.Background()})
		if ok || got.Version != "" {
			t.Errorf("rule %q in a directory that is no repository: got %+v ok=%v, want no version", rule, got, ok)
		}
		records := runner.Records()
		if len(records) != 1 {
			t.Fatalf("rule %q attempted %d commands, want exactly the one allowlisted question", rule, len(records))
		}
		argv[2] = root
		if strings.Join(records[0].Argv, " ") != strings.Join(argv, " ") {
			t.Errorf("rule %q ran %v, want %v", rule, records[0].Argv, argv)
		}
	}
}

// TestGitRulesAreRefusedOutsideTheAnchors keeps section 9.2 visible from here:
// a component root nobody registered is not asked about, and no process is
// created to find out.
func TestGitRulesAreRefusedOutsideTheAnchors(t *testing.T) {
	root := t.TempDir()
	runner := &exec.Runner{Features: exec.Features{Git: true}, Anchors: []string{filepath.Join(root, "elsewhere")}}
	for _, rule := range []string{"git", "commit"} {
		got, ok := Resolve([]string{rule}, root, Options{Runner: runner, Context: context.Background()})
		if ok || got.Version != "" {
			t.Errorf("rule %q outside every anchor: got %+v ok=%v, want no version", rule, got, ok)
		}
	}
	if records := runner.Records(); len(records) != 0 {
		t.Errorf("%d process(es) were created for a root outside every anchor", len(records))
	}
}

// TestGitVersionRatesWhatGitAnswered is the whole of section 20.3 for the
// describe rule, driven by the answers git can give rather than by git.
func TestGitVersionRatesWhatGitAnswered(t *testing.T) {
	const head = "037797e856cb1ec53c1545741d08fa758b6f0edb"

	cases := []struct {
		name       string
		described  string
		head       string
		headFails  bool
		want       Result
		wantOK     bool
		wantAsked  bool
		wantNoAsks bool
	}{
		{
			name:      "an exact tag on a clean tree is the only high rating",
			described: "v1.2.3", head: head, wantOK: true, wantAsked: true,
			want: Result{Version: "1.2.3", Source: "git-describe", Confidence: domain.ConfidenceHigh},
		},
		{
			name:      "a tag without the v prefix is read the same way",
			described: "1.2.3", head: head, wantOK: true, wantAsked: true,
			want: Result{Version: "1.2.3", Source: "git-describe", Confidence: domain.ConfidenceHigh},
		},
		{
			// The commit is read here too: `abc1234-dirty` is a bare hash with
			// a dirty tree, and only HEAD tells that from a tag.
			name:      "a modified tree is reported as such and rated lower",
			described: "v1.2.3-dirty", head: head, wantOK: true, wantAsked: true,
			want: Result{Version: "1.2.3", Source: "git-describe", Confidence: domain.ConfidenceMedium, Dirty: true},
		},
		{
			// Section 19.4: the version is the release this checkout derives
			// from. The distance lowers the rating and stays out of the value,
			// because `1.2.3-4-gdeadbee` sorts below `1.2.3` under semantic
			// versioning and would make an advisory fixed in 1.2.3 match a
			// checkout standing after it.
			name:      "commits past the tag mean the tag does not name this commit",
			described: "v1.2.3-4-gdeadbee", head: head, wantOK: true, wantNoAsks: true,
			want: Result{Version: "1.2.3", Source: "git-describe", Confidence: domain.ConfidenceMedium},
		},
		{
			name:      "distance and modification together stay at the lower rating",
			described: "v1.2.3-4-gdeadbee-dirty", head: head, wantOK: true, wantNoAsks: true,
			want: Result{Version: "1.2.3", Source: "git-describe", Confidence: domain.ConfidenceMedium, Dirty: true},
		},
		{
			// Section 20.2 makes a commit a version only at point 5, which an
			// explicit versionFrom rule has to ask for. The git rule of point 4
			// claims nothing from a bare commit: it identifies content and does
			// not order against a range.
			name:      "the --always fallback is a commit, not a version",
			described: head[:7], head: head, wantAsked: true,
		},
		{
			name:      "a bare commit with a dirty tree is no version either",
			described: head[:7] + "-dirty", head: head, wantAsked: true,
		},
		{
			name:      "an unreadable commit cannot promote a tag",
			described: "v1.2.3", headFails: true, wantOK: true, wantAsked: true,
			want: Result{Version: "1.2.3", Source: "git-describe", Confidence: domain.ConfidenceMedium},
		},
		{name: "an empty answer is no answer", described: ""},
		{name: "a bare dirty marker names nothing", described: "-dirty"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var asked int
			got, ok := gitVersion(test.described, func() (string, bool) {
				asked++
				return test.head, !test.headFails
			})
			if ok != test.wantOK {
				t.Fatalf("ok = %v, want %v", ok, test.wantOK)
			}
			if !test.wantOK {
				if got.Version != "" {
					t.Fatalf("version = %q, want nothing when git said nothing usable", got.Version)
				}
				return
			}
			if got != test.want {
				t.Errorf("got %+v, want %+v", got, test.want)
			}
			if strings.HasSuffix(got.Version, "-dirty") {
				t.Errorf("version %q still carries the -dirty suffix; the fact belongs in Result.Dirty", got.Version)
			}
			if test.wantNoAsks && asked != 0 {
				t.Errorf("the commit was read %d time(s) although it could not change the rating", asked)
			}
			if test.wantAsked && asked == 0 {
				t.Error("the rating was decided without reading the commit that decides it")
			}
		})
	}
}

// TestCommitVersionIsTheShortenedCommit pins section 20.2 point 5, and with it
// the end of the constant the commit rule used to return for every root.
func TestCommitVersionIsTheShortenedCommit(t *testing.T) {
	const head = "037797e856cb1ec53c1545741d08fa758b6f0edb"

	got, ok := commitVersion(head)
	if !ok {
		t.Fatalf("a full SHA produced no version")
	}
	want := Result{Version: "0.0.0-git.037797e856cb", Source: "git-commit", Confidence: domain.ConfidenceMedium}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !strings.HasPrefix(head, strings.TrimPrefix(got.Version, "0.0.0-git.")) {
		t.Errorf("version %q is not an abbreviation of the commit it claims", got.Version)
	}

	// Anything else git might answer is not evidence of a commit, and this
	// rule used to answer with a constant no matter what git said.
	for _, answer := range []string{
		"",
		"037797e",
		"037797e856cb1ec53c1545741d08fa758b6f0edbb",
		"037797E856CB1EC53C1545741D08FA758B6F0EDB",
		"fatal: not a git repository",
	} {
		if got, ok := commitVersion(answer); ok || got.Version != "" {
			t.Errorf("answer %q produced %+v; only a full SHA is a commit", answer, got)
		}
	}
}

func TestPURLPercentEncodesSpecialCharacters(t *testing.T) {
	got := PURL("pkg:generic", "demo+core@latest", "1.2.3")
	if got != "pkg:generic/demo%2Bcore%40latest@1.2.3" {
		t.Fatalf("unexpected purl: %q", got)
	}
}

// A repository with no reachable tag: `git describe --tags --always --dirty`
// falls back to the abbreviated commit. The git rule of section 20.2 point 4
// must produce nothing from it, so that resolution continues to point 5 --
// which an explicit `commit` rule has to ask for -- and otherwise to point 7,
// where UNKNOWN_VERSION says there is no version.
//
// This is the same property TestGitRulesProduceNothingWithoutIntrospection
// keeps for a different cause: an answer that is not a version must not count
// as success, because a success here hides UNKNOWN_VERSION.
func TestABareCommitDoesNotSatisfyTheGitRule(t *testing.T) {
	root := t.TempDir()
	if !realGitRepository(t, root) {
		t.Skip("git is not available")
	}
	runner := &exec.Runner{Features: exec.Features{Git: true}, Anchors: []string{root}}
	options := Options{Runner: runner, Context: context.Background()}

	if got, ok := Resolve([]string{"git"}, root, options); ok || got.Version != "" {
		t.Errorf("the git rule answered %+v ok=%v for a repository with no tag, want nothing", got, ok)
	}
	// The commit rule, which somebody has to ask for by name, still answers.
	if got, ok := Resolve([]string{"commit"}, root, options); !ok || got.Version == "" {
		t.Errorf("the commit rule answered %+v ok=%v, want the commit version of point 5", got, ok)
	}
}

// realGitRepository makes root a repository with one commit and no tag at all,
// with an identity of its own so the test does not depend on whoever runs it.
func realGitRepository(t *testing.T, root string) bool {
	t.Helper()
	if _, err := osexec.LookPath("git"); err != nil {
		return false
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"commit", "-qm", "one", "--no-gpg-sign"},
	} {
		command := osexec.Command("git", append([]string{"-C", root}, args...)...)
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=sbomb", "GIT_AUTHOR_EMAIL=t@sbomb.invalid",
			"GIT_COMMITTER_NAME=sbomb", "GIT_COMMITTER_EMAIL=t@sbomb.invalid",
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
			"GIT_CONFIG_GLOBAL="+empty, "GIT_CONFIG_SYSTEM="+empty)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return true
}
