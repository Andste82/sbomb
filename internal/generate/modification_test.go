package generate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	sbombexec "github.com/example/sbomb/internal/exec"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
)

// Section 19.4, tri-state. The corpus cannot state these cases: the harvest
// skips .git (open question Q9), so every component of every fixture is
// `unknown` there -- which is the honest answer and not the interesting one.
// So the repository is built here, in a temporary directory, from real git.

// gitRepository makes root a repository with one commit and one tag, and
// returns whether git was available at all.
func gitRepository(t *testing.T, root string) bool {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "fixture", "--no-gpg-sign")
	git(t, root, "tag", "-f", "v1.2.0")
	return true
}

// git runs one command in root with an identity of its own and with the user's
// own configuration out of the way, so that the test does not depend on
// whoever runs it.
func git(t *testing.T, root string, args ...string) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=sbomb", "GIT_AUTHOR_EMAIL=t@sbomb.invalid",
		"GIT_COMMITTER_NAME=sbomb", "GIT_COMMITTER_EMAIL=t@sbomb.invalid",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
		"GIT_CONFIG_GLOBAL="+empty, "GIT_CONFIG_SYSTEM="+empty)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// modificationOf derives the status for a component rooted at root.
func modificationOf(t *testing.T, root string, features sbombexec.Features) (*domain.Component, []domain.Finding) {
	t.Helper()
	source := filepath.Join(root, "src", "lib.c")
	file := domain.UsedFile{ID: fileID("project", "src/lib.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source}, map[string]string{"project": root}, nil)
	resolver.setIntrospection(&sbombexec.Runner{Features: features, Anchors: []string{root}}, context.Background())
	component := &domain.Component{ID: "component:lib", Name: "lib"}
	findings := resolver.resolveModification(component, componentRootResult{
		ID: domain.FileID{Anchor: "project"}, Physical: root, Source: "curated",
	})
	return component, findings
}

// A tree with a source file in it, ready to become a repository.
func modificationTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "src", "lib.c"), "int lib(void){return 0;}\n")
	return root
}

// The four cases the milestone lists, each with the finding that goes with it.
func TestModificationStatusIsTriState(t *testing.T) {
	t.Run("a clean checkout standing on its tag is false", func(t *testing.T) {
		root := modificationTree(t)
		if !gitRepository(t, root) {
			t.Skip("git is not available")
		}
		component, findings := modificationOf(t, root, sbombexec.Features{Git: true})
		if component.Modification.Status != domain.ModificationUnmodified {
			t.Errorf("status = %q, want %q (%s)", component.Modification.Status,
				domain.ModificationUnmodified, component.Modification.Signal)
		}
		if component.Modification.Commit == "" {
			t.Error("no commit was recorded, so the pedigree would name no revision")
		}
		if hasFindingID(findings, "FOSS_MODIFICATION_UNKNOWN") {
			t.Errorf("findings = %v; the answer was established", findingIDs(findings))
		}
	})

	t.Run("an uncommitted change is true", func(t *testing.T) {
		root := modificationTree(t)
		if !gitRepository(t, root) {
			t.Skip("git is not available")
		}
		write(t, filepath.Join(root, "src", "lib.c"), "int lib(void){return 1;}\n")
		component, findings := modificationOf(t, root, sbombexec.Features{Git: true})
		if component.Modification.Status != domain.ModificationModified {
			t.Errorf("status = %q, want %q (%s)", component.Modification.Status,
				domain.ModificationModified, component.Modification.Signal)
		}
		if hasFindingID(findings, "FOSS_MODIFICATION_UNKNOWN") {
			t.Errorf("findings = %v; the answer was established", findingIDs(findings))
		}
	})

	t.Run("no repository at all is unknown", func(t *testing.T) {
		root := modificationTree(t)
		component, findings := modificationOf(t, root, sbombexec.Features{Git: true})
		if component.Modification.Status != domain.ModificationUnknown {
			t.Errorf("status = %q, want %q", component.Modification.Status, domain.ModificationUnknown)
		}
		if !hasFindingID(findings, "FOSS_MODIFICATION_UNKNOWN") {
			t.Errorf("findings = %v, want FOSS_MODIFICATION_UNKNOWN", findingIDs(findings))
		}
	})

	t.Run("introspection off with a repository present is unknown", func(t *testing.T) {
		root := modificationTree(t)
		if !gitRepository(t, root) {
			t.Skip("git is not available")
		}
		component, findings := modificationOf(t, root, sbombexec.Features{})
		if component.Modification.Status != domain.ModificationUnknown {
			t.Errorf("status = %q, want %q", component.Modification.Status, domain.ModificationUnknown)
		}
		// And the signal says which of the two unknowns it is, because a
		// missing permission and a missing repository are different gaps.
		if component.Modification.Signal == "" {
			t.Error("the signal is empty, so the report cannot say why the answer is unknown")
		}
		if !hasFindingID(findings, "FOSS_MODIFICATION_UNKNOWN") {
			t.Errorf("findings = %v, want FOSS_MODIFICATION_UNKNOWN", findingIDs(findings))
		}
	})
}

// The third signal of section 19.4: metadata that records a patch. No git is
// involved, and none is needed -- a patched component is modified whatever its
// checkout says.
func TestRecordedPatchesMakeAComponentModified(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		file     string
		content  string
		wantType string
	}{
		{
			name: "a Conan recipe's conandata.yml", file: "conandata.yml",
			content: "patches:\n  \"1.2.0\":\n    - patch_file: \"patches/0001-fix-build.patch\"\n" +
				"      patch_description: \"fix the build on musl\"\n      patch_type: \"portability\"\n",
			wantType: domain.PatchUnofficial,
		},
		{
			name: "a Conan recipe that names the patch kind CycloneDX knows", file: "conandata.yml",
			content:  "patches:\n  - patch_file: \"0002.patch\"\n    patch_type: \"backport\"\n",
			wantType: domain.PatchBackport,
		},
		{
			name: "a vcpkg portfile", file: "portfile.cmake",
			content:  "vcpkg_from_github(\n    REPO foo/bar\n    PATCHES\n        fix-cmake.patch\n)\n",
			wantType: domain.PatchUnofficial,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := modificationTree(t)
			write(t, filepath.Join(root, testCase.file), testCase.content)
			component, findings := modificationOf(t, root, sbombexec.Features{})

			if component.Modification.Status != domain.ModificationModified {
				t.Fatalf("status = %q, want %q", component.Modification.Status, domain.ModificationModified)
			}
			if len(component.Modification.Patches) != 1 {
				t.Fatalf("patches = %#v, want exactly one", component.Modification.Patches)
			}
			if got := component.Modification.Patches[0].Type; got != testCase.wantType {
				t.Errorf("patch type = %q, want %q", got, testCase.wantType)
			}
			if hasFindingID(findings, "FOSS_MODIFICATION_UNKNOWN") {
				t.Errorf("findings = %v; a patch record is an answer", findingIDs(findings))
			}
		})
	}
}

// A comment in a portfile is not a patch. The reader lexes comments out rather
// than matching the whole file, so a name somebody wrote down is not a patch
// somebody applied.
func TestACommentedPatchNameIsNotAPatch(t *testing.T) {
	root := modificationTree(t)
	write(t, filepath.Join(root, "portfile.cmake"),
		"# PATCHES do-not-apply.patch\nvcpkg_from_github(REPO foo/bar)\n")
	component, _ := modificationOf(t, root, sbombexec.Features{})
	if component.Modification.Status != domain.ModificationUnknown {
		t.Errorf("status = %q, want %q: the patch name is in a comment",
			component.Modification.Status, domain.ModificationUnknown)
	}
}

// A clean checkout that does not stand on a tag is unknown and not false.
// Counting the commits between HEAD and the tag needs `git rev-list`, which
// section 9.2 does not permit, and a clean tree some distance past a tag is
// not an unmodified component.
func TestACleanCheckoutOffItsTagIsUnknown(t *testing.T) {
	root := modificationTree(t)
	if !gitRepository(t, root) {
		t.Skip("git is not available")
	}
	// A second commit, so HEAD is one past the tag and the tree is clean.
	write(t, filepath.Join(root, "src", "lib.c"), "int lib(void){return 2;}\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "second", "--no-gpg-sign")

	component, _ := modificationOf(t, root, sbombexec.Features{Git: true})
	if component.Modification.Status != domain.ModificationUnknown {
		t.Errorf("status = %q, want %q (%s)", component.Modification.Status,
			domain.ModificationUnknown, component.Modification.Signal)
	}
}

// The nearest enclosing repository is not the component's repository. A
// library copied into a project's tree must not inherit the project's dirty
// state, or an edit to the manufacturer's own code would be published as a
// modification of a third-party component.
func TestAnEnclosingRepositoryDoesNotDecideAVendoredComponent(t *testing.T) {
	outer := modificationTree(t)
	write(t, filepath.Join(outer, "dep", "vendored", "src", "lib.c"), "int v(void){return 0;}\n")
	if !gitRepository(t, outer) {
		t.Skip("git is not available")
	}
	// Dirty the outer repository, which is what a project under development
	// looks like.
	write(t, filepath.Join(outer, "src", "lib.c"), "int lib(void){return 3;}\n")

	inner := filepath.Join(outer, "dep", "vendored")
	file := domain.UsedFile{ID: fileID("project", "dep/vendored/src/lib.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): filepath.Join(inner, "src", "lib.c")},
		map[string]string{"project": outer}, nil)
	resolver.setIntrospection(&sbombexec.Runner{
		Features: sbombexec.Features{Git: true}, Anchors: []string{outer}}, context.Background())
	component := &domain.Component{ID: "component:vendored", Name: "vendored"}
	resolver.resolveModification(component, componentRootResult{
		ID: domain.FileID{Anchor: "project", RelPath: "dep/vendored"}, Physical: inner, Source: "marker:LICENSE",
	})

	if component.Modification.Status != domain.ModificationUnknown {
		t.Errorf("status = %q, want %q: the dirty tree is the project's, not this component's",
			component.Modification.Status, domain.ModificationUnknown)
	}
}

// A tag whose own name contains "-g" is a tag. `git describe` marks a distance
// from a tag with the whole suffix `-<count>-g<hash>`, and reading the two
// characters alone would report `unknown` for a component standing exactly on
// `v1.0-gamma`.
func TestATagWhoseNameContainsTheDistanceMarkerIsStillATag(t *testing.T) {
	commit := "1d0f2c3b4a5968778695a4b3c2d1e0f918273645"
	exact := []string{"v1.2.0", "v1.0-gamma", "release-gcc13", "v2.0-gtest-support"}
	for _, described := range exact {
		if !exactTagDescribe(described, commit) {
			t.Errorf("%q reads as a distance from a tag, and it is a tag name", described)
		}
	}
	distances := []string{"v1.2.0-4-gdeadbee", "v1.0-gamma-12-g1d0f2c3"}
	for _, described := range distances {
		if exactTagDescribe(described, commit) {
			t.Errorf("%q reads as a tag, and it is a distance from one", described)
		}
	}
	// `--always` falls back to the abbreviated commit for a repository with no
	// tag, and an abbreviated hash is a prefix of the full one.
	if exactTagDescribe(commit[:8], commit) {
		t.Error("an abbreviated commit reads as a tag")
	}
}

// A tag with a distance suffix, from real git, so that the shape the regexp
// matches is the shape git writes and not the one this test imagines.
func TestTheDistanceSuffixIsTheOneGitWrites(t *testing.T) {
	root := modificationTree(t)
	if !gitRepository(t, root) {
		t.Skip("git is not available")
	}
	git(t, root, "tag", "-f", "v1.0-gamma")
	write(t, filepath.Join(root, "src", "lib.c"), "int lib(void){return 4;}\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "past the tag", "--no-gpg-sign")

	component, _ := modificationOf(t, root, sbombexec.Features{Git: true})
	if component.Modification.Status != domain.ModificationUnknown {
		t.Errorf("status = %q, want %q one commit past a tag named v1.0-gamma (%s)",
			component.Modification.Status, domain.ModificationUnknown, component.Modification.Signal)
	}

	// And standing on that same tag is "false": the tag's name is not a
	// distance.
	git(t, root, "tag", "-f", "v1.0-gamma")
	onTag, _ := modificationOf(t, root, sbombexec.Features{Git: true})
	if onTag.Modification.Status != domain.ModificationUnmodified {
		t.Errorf("status = %q, want %q standing on v1.0-gamma (%s)",
			onTag.Modification.Status, domain.ModificationUnmodified, onTag.Modification.Signal)
	}
}
