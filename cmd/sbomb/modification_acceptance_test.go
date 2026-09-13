package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	commit := map[string]string{}
	for _, component := range document.Components {
		for _, property := range component.Properties {
			switch property.Name {
			case "sbomb:component:modified":
				status[component.BomRef] = property.Value
			case "sbomb:component:vcsCommit":
				commit[component.BomRef] = property.Value
			}
		}
		pedigree[component.BomRef] = component.Pedigree != nil
	}
	// Compiled from a tree that is not the tree its tag names. A dirty tree
	// needs nobody's corroboration: the edit is in front of git, not inferred
	// from a name.
	if status["component:lgpl-lib"] != "true" {
		t.Errorf("component:lgpl-lib is %q, want %q", status["component:lgpl-lib"], "true")
	}
	if !pedigree["component:lgpl-lib"] {
		t.Error("component:lgpl-lib carries no pedigree, and a settled status is what fills it")
	}

	// The other two are clean, stand on or past a tag, and no package manager
	// owns them -- `dep/*` is found by the marker file of section 19.2 and
	// nothing else. Section 19.4: a tag is a name the repository gives itself,
	// so there is nothing here to hold the checkout against, and the answer is
	// unknown rather than the `false` and `true` this asserted before.
	for _, ref := range []string{"component:mit-lib", "component:apache-lib"} {
		if status[ref] != "unknown" {
			t.Errorf("%s is %q, want %q", ref, status[ref], "unknown")
		}
		if pedigree[ref] {
			t.Errorf("%s carries a pedigree; an unknown status must fill none, or an absent one would read as unmodified", ref)
		}
		// The fact the answer could not be drawn from is published anyway: an
		// unknown status fills no pedigree, so without this the commit would
		// reach the document nowhere, and it is what lets a consumer holding
		// the upstream finish the comparison sbomb could not.
		if commit[ref] == "" {
			t.Errorf("%s publishes no commit, and it is the one fact that was read", ref)
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

// The other half of section 19.4, which the FOSS fixture cannot show: a
// component whose revision somebody declared.
//
// `p10-fetchcontent` pins `tinylog` to `GIT_TAG v1.2.0`, and the populate
// script CMake generated out of that declaration is in the committed corpus.
// That is the second statement the status needs -- one that does not come from
// the checkout -- so this is where `false` and `true` can be established at
// document level at all.
//
// The repository is built here for the same reason it is in the test above:
// git will not put a path with a `.git` component into an index, and a
// harvested index churns (open question Q9). What is committed is the tree.
func TestModificationStatusAgainstADeclaredRevision(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required to build the fixture repositories")
	}

	run := func(t *testing.T, prepare func(t *testing.T, checkout string)) (map[string]string, map[string]string, []byte) {
		t.Helper()
		buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p10-fetchcontent")
		checkout := filepath.Join(buildDir, "_deps", "tinylog-src")
		fixtureGit(t, checkout, "init", "-q", "-b", "main")
		fixtureGit(t, checkout, "add", "-A")
		fixtureGit(t, checkout, "commit", "-qm", "tinylog v1.2.0", "--no-gpg-sign")
		fixtureGit(t, checkout, "tag", "-f", "v1.2.0")
		if prepare != nil {
			prepare(t, checkout)
		}

		output := filepath.Join(t.TempDir(), "out.cdx.json")
		code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
			"--allow-introspection=git", "--output", output, "--reproducible"})
		if code != 0 || stderr != "" {
			t.Fatalf("generate = code %d, stderr %q", code, stderr)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Components []struct {
				BomRef     string `json:"bom-ref"`
				Properties []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"properties"`
				Pedigree *struct {
					Notes string `json:"notes"`
				} `json:"pedigree"`
			} `json:"components"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		properties := map[string]string{}
		notes := map[string]string{}
		for _, component := range document.Components {
			for _, property := range component.Properties {
				properties[component.BomRef+" "+property.Name] = property.Value
			}
			if component.Pedigree != nil {
				notes[component.BomRef] = component.Pedigree.Notes
			}
		}
		return properties, notes, data
	}

	t.Run("standing on the declared revision", func(t *testing.T) {
		properties, notes, document := run(t, nil)
		if got := properties["component:tinylog sbomb:component:modified"]; got != "false" {
			t.Errorf("modified = %q, want %q (%s)", got, "false", notes["component:tinylog"])
		}
		// Published whatever the answer: it is the fact the answer was drawn
		// from, and a consumer can check it against an upstream of its own.
		if got := properties["component:tinylog sbomb:component:declaredRevision"]; got != "v1.2.0" {
			t.Errorf("declaredRevision = %q, want v1.2.0", got)
		}
		assertGolden(t, "gcc-ninja-p10-fetchcontent-declared.cdx.json", document)
	})

	t.Run("a commit past the declared revision", func(t *testing.T) {
		properties, notes, _ := run(t, func(t *testing.T, checkout string) {
			if err := os.WriteFile(filepath.Join(checkout, "DOWNSTREAM-FIX.md"),
				[]byte("A fix committed on top of v1.2.0.\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			fixtureGit(t, checkout, "add", "-A")
			fixtureGit(t, checkout, "commit", "-qm", "downstream fix", "--no-gpg-sign")
		})
		if got := properties["component:tinylog sbomb:component:modified"]; got != "true" {
			t.Errorf("modified = %q, want %q (%s)", got, "true", notes["component:tinylog"])
		}
		if !strings.Contains(notes["component:tinylog"], "1 commit(s) past the declared revision v1.2.0") {
			t.Errorf("notes = %q, want the distance to the declared tag", notes["component:tinylog"])
		}
	})

	t.Run("a tag the maintainer added is not the declared one", func(t *testing.T) {
		properties, notes, _ := run(t, func(t *testing.T, checkout string) {
			if err := os.WriteFile(filepath.Join(checkout, "DOWNSTREAM-FIX.md"),
				[]byte("A fix committed on top of v1.2.0.\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			fixtureGit(t, checkout, "add", "-A")
			fixtureGit(t, checkout, "commit", "-qm", "downstream fix", "--no-gpg-sign")
			// Nearer than v1.2.0, so `git describe` answers with this one. It
			// is what used to make this checkout "unmodified" at distance zero.
			fixtureGit(t, checkout, "tag", "-f", "acme-1")
		})
		if got := properties["component:tinylog sbomb:component:modified"]; got != "true" {
			t.Errorf("modified = %q, want %q (%s): a tag somebody else's release never had", got, "true", notes["component:tinylog"])
		}
		if !strings.Contains(notes["component:tinylog"], "v1.2.0") {
			t.Errorf("notes = %q, want the declared revision named, not the tag git happened to find", notes["component:tinylog"])
		}
	})
}
