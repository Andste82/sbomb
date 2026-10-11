package main

import (
	"os"
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
// action was loaded from, then github.repository, which is what a use by
// local path -- sbomb's own smoke test -- falls through to, because
// github.action_repository is empty there.
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
		`SBOMB_REPOSITORY="${SBOMB_REPOSITORY:-${SBOMB_ACTION_REPOSITORY:-$GITHUB_REPOSITORY}}"`,
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
}
