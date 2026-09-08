package version

import (
	"context"
	"os"
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
		"another group": {Runner: &exec.Runner{Features: exec.Features{CMake: true}, Anchors: []string{root}}, Context: context.Background()},
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
			name:      "a modified tree is reported as such and rated lower",
			described: "v1.2.3-dirty", head: head, wantOK: true, wantNoAsks: true,
			want: Result{Version: "1.2.3", Source: "git-describe", Confidence: domain.ConfidenceMedium, Dirty: true},
		},
		{
			name:      "commits past the tag mean the tag does not name this commit",
			described: "v1.2.3-4-gdeadbee", head: head, wantOK: true, wantNoAsks: true,
			want: Result{Version: "1.2.3-4-gdeadbee", Source: "git-describe", Confidence: domain.ConfidenceMedium},
		},
		{
			name:      "distance and modification together stay at the lower rating",
			described: "v1.2.3-4-gdeadbee-dirty", head: head, wantOK: true, wantNoAsks: true,
			want: Result{Version: "1.2.3-4-gdeadbee", Source: "git-describe", Confidence: domain.ConfidenceMedium, Dirty: true},
		},
		{
			name:      "the --always fallback is a commit, not a tag",
			described: head[:7], head: head, wantOK: true, wantAsked: true,
			want: Result{Version: head[:7], Source: "git-describe", Confidence: domain.ConfidenceMedium},
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
