package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// TestTheActionDownloadsFromItsOwnRepository asserts where the action looks
// for the release when the workflow does not say. An input default is
// evaluated in the caller's context, so `${{ github.repository }}` there names
// the calling repository, and every project using the action from outside
// looked for sbomb in its own releases and got a 404. The default is therefore
// empty and the download step resolves it: the input, then the repository the
// action was loaded from, then the owner and repository in the remote checkout
// path, because github.action_repository can be empty in a composite action
// used remotely as well. Only a use by local path -- sbomb's own smoke test --
// falls through to github.repository.
func TestTheActionDownloadsFromItsOwnRepository(t *testing.T) {
	root := testutil.RepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "actions", "sbomb", "action.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout gives the file CRLF line ends.
	action := strings.ReplaceAll(string(data), "\r\n", "\n")

	_, input, ok := strings.Cut(action, "\n  repository:\n")
	if !ok {
		t.Fatal("the action has no repository input")
	}
	// The input's own lines end where the next input begins.
	input, _, _ = strings.Cut(input, "\n  version:\n")
	if strings.Contains(input, "${{ github.repository }}") {
		t.Errorf("the repository input defaults to the calling repository:\n%s", input)
	}
	if !strings.Contains(input, "default: ''") {
		t.Errorf("the repository input does not default to empty:\n%s", input)
	}

	var download string
	for _, step := range strings.Split(action, "\n    - name: ")[1:] {
		if strings.HasPrefix(step, "Download sbomb\n") {
			download = step
		}
	}
	if download == "" {
		t.Fatal("the action has no download step")
	}
	for _, want := range []string{
		"SBOMB_REPOSITORY: ${{ inputs.repository }}",
		"SBOMB_ACTION_REPOSITORY: ${{ github.action_repository }}",
		"SBOMB_ACTION_PATH: ${{ github.action_path }}",
	} {
		if !strings.Contains(download, want) {
			t.Errorf("the download step does not contain %q", want)
		}
	}

	// The examples are what a consumer copies. One that still names the
	// repository works, but teaches that it has to be set.
	ci, err := os.ReadFile(filepath.Join(root, "docs", "ci.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ci), "repository: Andste82/sbomb") {
		t.Error("docs/ci.md still sets repository in an example, as if the default pointed elsewhere")
	}
	testTheActionResolvesItsRepository(t, download)
}

// testTheActionResolvesItsRepository runs the download step's script up to the
// line that reports the repository, so that the resolution is executed rather
// than read. The smoke test cannot cover it: it uses the action by local path,
// and a remote use with an empty github.action_repository is the case that
// sent consumers to their own releases.
func testTheActionResolvesItsRepository(t *testing.T, download string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	_, script, ok := strings.Cut(download, "      run: |\n")
	if !ok {
		t.Fatal("the download step has no script")
	}
	script, _, ok = strings.Cut(script, "        echo \"Downloading sbomb ")
	if !ok {
		t.Fatal("the download step does not report the repository it resolved")
	}
	script += `printf '%s' "$SBOMB_REPOSITORY"` + "\n"

	for _, tc := range []struct {
		name, input, actionRepository, actionPath string
		want                                      string // empty: the step must fail
	}{
		{"input wins", "fork/sbomb", "Andste82/sbomb", "/home/runner/work/_actions/Andste82/sbomb/v1/.github/actions/sbomb", "fork/sbomb"},
		{"action repository", "", "Andste82/sbomb", "/home/runner/work/_actions/Andste82/sbomb/v1/.github/actions/sbomb", "Andste82/sbomb"},
		{"remote checkout path", "", "", "/home/runner/work/_actions/Andste82/sbomb/v1/.github/actions/sbomb", "Andste82/sbomb"},
		{"remote checkout path on Windows", "", "", `D:\a\_actions\Andste82\sbomb\v1\.github\actions\sbomb`, "Andste82/sbomb"},
		{"local path", "", "", "/home/runner/work/sbomb/sbomb/./.github/actions/sbomb", "caller/project"},
		{"unreadable remote checkout path", "", "", "/home/runner/work/_actions/Andste82", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bash, "-c", script)
			cmd.Env = append(os.Environ(),
				"SBOMB_REPOSITORY="+tc.input,
				"SBOMB_ACTION_REPOSITORY="+tc.actionRepository,
				"SBOMB_ACTION_PATH="+tc.actionPath,
				"GITHUB_REPOSITORY=caller/project",
			)
			out, err := cmd.Output()
			if tc.want == "" {
				if err == nil {
					t.Fatalf("the step resolved %q instead of asking for the repository input", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("the step failed: %v", err)
			}
			if got := string(out); got != tc.want {
				t.Errorf("the step resolved %q, want %q", got, tc.want)
			}
		})
	}
}
