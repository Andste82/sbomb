package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// The modification status of section 19.4 is read from the component root's
// git repository, and the committed corpus carries none: the harvest skips
// `.git`, so every fixture component reports `unknown` there (open question
// Q9). That leaves two things untested, and both are what this test is for.
//
// The first is the interaction between the status and the source-tree
// relocation of section 7.9. The check is handed the *physical* component
// root, and relocation is what produces it, so a relocated tree that carries a
// repository has to be answered from the relocated copy. Nothing asserted it.
//
// The second is a document. Every golden shows `unknown`, so no golden showed
// what a `modified` component looks like -- the property, the pedigree, the
// signal -- and a reader of the corpus could not tell the shape of the answer
// from the answer itself.
//
// The repository is built here rather than committed, because git will not put
// a path with a `.git` component into its index and a harvested `.git/index`
// carries per-file ctime and ino that churn on every regeneration. Both were
// measured while F1 was written and are recorded in Q9. What is committed is
// the tree: `tools/fixtures/projects/p14-foss` is what the tag names, and
// `testdata/fixtures/p14-foss-src` is what was compiled. The difference
// between them is the modification, and reconstructing the history from the
// two of them takes nothing that is not already in the repository.
func TestModificationStatusOverRelocatedRepositories(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required to build the fixture repositories")
	}
	source := materializeFossRepositories(t)
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	output := filepath.Join(t.TempDir(), "modified.cdx.json")

	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
		"--source-dir", source, "--allow-introspection=git", "--policy", "lenient",
		"--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}

	// The three states, in one document, from one run over the corpus's own
	// build evidence.
	var document struct {
		Components []struct {
			BomRef     string `json:"bom-ref"`
			Properties []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"properties"`
			Pedigree *struct {
				Notes   string `json:"notes"`
				Commits []struct {
					UID string `json:"uid"`
				} `json:"commits"`
			} `json:"pedigree"`
		} `json:"components"`
	}
	if err := json.Unmarshal(actual, &document); err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	pedigree := map[string]bool{}
	for _, component := range document.Components {
		for _, property := range component.Properties {
			if property.Name == "sbomb:component:modified" {
				status[component.BomRef] = property.Value
			}
		}
		pedigree[component.BomRef] = component.Pedigree != nil
	}
	for ref, want := range map[string]string{
		// Compiled from a tree that is not the tree its tag names.
		"component:lgpl-lib": "true",
		// Clean, and one commit past its tag: the commonest shape of a
		// dependency somebody fixed, and `unknown` before D47.
		"component:apache-lib": "true",
		// Clean and standing on its tag.
		"component:mit-lib": "false",
	} {
		if status[ref] != want {
			t.Errorf("%s is %q, want %q", ref, status[ref], want)
		}
		if !pedigree[ref] {
			t.Errorf("%s carries no pedigree, and a settled status is what fills it", ref)
		}
	}

	assertGolden(t, "gcc-ninja-p14-foss-modified.cdx.json", actual)
}

// materializeFossRepositories rebuilds what `tools/fixtures/regen.sh` had on
// disk when the corpus was produced: every `dep/*/` a repository with the same
// fixed identity, date and tag, so that every commit hash in the golden is the
// same on every machine.
//
// It starts from the harvested tree, which holds the bytes that were compiled,
// and commits the *pristine* content of the one file regen.sh left dirty. What
// the tag names is therefore `tools/fixtures/projects`, and what the working
// tree holds is what the object code came from -- which is the difference the
// status reports.
func materializeFossRepositories(t *testing.T) string {
	t.Helper()
	root := testutil.CorpusSourceTreeCopy(t)
	repoRoot := testutil.RepoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, "dep"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dep := filepath.Join(root, "dep", entry.Name())

		// The one dependency regen.sh left dirty: its tag names the pristine
		// file, so the pristine file is what gets committed.
		dirty := filepath.Join(dep, "src", "lgpl_extra.c")
		harvested, restored := []byte(nil), false
		if entry.Name() == "lgpl-lib" {
			pristine, err := os.ReadFile(filepath.Join(repoRoot,
				"tools", "fixtures", "projects", "p14-foss", "dep", "lgpl-lib", "src", "lgpl_extra.c"))
			if err != nil {
				t.Fatal(err)
			}
			if harvested, err = os.ReadFile(dirty); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dirty, pristine, 0o600); err != nil {
				t.Fatal(err)
			}
			restored = true
		}

		fixtureGit(t, dep, "init", "-q", "-b", "main")
		fixtureGit(t, dep, "add", "-A")
		fixtureGit(t, dep, "commit", "-qm", "fixture dependency", "--no-gpg-sign")
		fixtureGit(t, dep, "tag", "-f", "v1.2.0")

		if restored {
			if err := os.WriteFile(dirty, harvested, 0o600); err != nil {
				t.Fatal(err)
			}
		}

		// One dependency gets a commit on top of its tag, which regen.sh does
		// not produce. The file it adds is not a used file, so no byte the
		// build evidence recorded changes: what changes is only what git says
		// about the checkout.
		if entry.Name() == "apache-lib" {
			if err := os.WriteFile(filepath.Join(dep, "UPSTREAM-FIX.md"),
				[]byte("A downstream fix committed on top of v1.2.0.\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			fixtureGit(t, dep, "add", "-A")
			fixtureGit(t, dep, "commit", "-qm", "downstream fix", "--no-gpg-sign")
		}
	}
	return root
}

// fixtureGit runs one git command with the identity and the date
// tools/fixtures/regen.sh uses, and with the user's own configuration out of
// the way, so that the commit hashes in the golden do not depend on who runs
// the test.
func fixtureGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=sbomb", "GIT_AUTHOR_EMAIL=fixtures@sbomb.invalid",
		"GIT_COMMITTER_NAME=sbomb", "GIT_COMMITTER_EMAIL=fixtures@sbomb.invalid",
		"GIT_AUTHOR_DATE=2026-09-05T00:00:00Z", "GIT_COMMITTER_DATE=2026-09-05T00:00:00Z",
		"GIT_CONFIG_GLOBAL="+empty, "GIT_CONFIG_SYSTEM="+empty)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, output)
	}
}
