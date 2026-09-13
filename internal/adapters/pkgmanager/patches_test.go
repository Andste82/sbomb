package pkgmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A root with no metadata at all is silence, not a gap: most component roots
// carry no patch record, and reporting one for each of them would be noise.
func TestARootWithNoMetadataRecordsNothing(t *testing.T) {
	patches, findings := Patches(t.TempDir())
	if len(patches) != 0 || len(findings) != 0 {
		t.Errorf("patches = %#v, findings = %#v, want neither", patches, findings)
	}
	if got, _ := Patches(""); got != nil {
		t.Errorf("patches = %#v for no root at all", got)
	}
}

// Both shapes of a Conan recipe's patches block, and the enum mapping: only a
// patch kind CycloneDX has a value for is carried over, everything else is
// "unofficial", which is what a patch a package manager applied is.
func TestConandataPatchesAreReadInBothShapes(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		want    []domain.Patch
	}{
		{
			name: "a mapping from version to a sequence",
			content: "patches:\n  \"1.2.0\":\n    - patch_file: \"patches/a.patch\"\n" +
				"      patch_type: \"portability\"\n  \"1.3.0\":\n    - patch_file: \"patches/b.patch\"\n",
			want: []domain.Patch{
				{File: "patches/a.patch", Type: domain.PatchUnofficial, Source: "conandata.yml"},
				{File: "patches/b.patch", Type: domain.PatchUnofficial, Source: "conandata.yml"},
			},
		},
		{
			name:    "a bare sequence",
			content: "patches:\n  - patch_file: \"only.patch\"\n    patch_type: \"cherry-pick\"\n",
			want: []domain.Patch{
				{File: "only.patch", Type: domain.PatchCherryPick, Source: "conandata.yml"},
			},
		},
		{
			name:    "a recipe that declares no patches",
			content: "sources:\n  \"1.2.0\":\n    url: \"https://example.invalid/x.tar.gz\"\n",
			want:    nil,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "conandata.yml"), testCase.content)
			patches, _ := Patches(root)
			if len(patches) != len(testCase.want) {
				t.Fatalf("patches = %#v, want %#v", patches, testCase.want)
			}
			for index, want := range testCase.want {
				got := patches[index]
				if got.File != want.File || got.Type != want.Type || got.Source != want.Source {
					t.Errorf("patch %d = %+v, want %+v", index, got, want)
				}
			}
		})
	}
}

// A portfile is CMake, and reading it as a program would mean running somebody
// else's code. The rule is the narrow one -- a token naming a patch file is a
// patch the port applies -- and a comment is lexed out first.
func TestPortfilePatchesAreTheNamesItApplies(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "portfile.cmake"),
		"# fix-nothing.patch is not applied any more\n"+
			"vcpkg_from_github(\n    REPO foo/bar\n    PATCHES\n"+
			"        \"fix-cmake.patch\"\n        support-musl.diff\n)\n")

	patches, _ := Patches(root)
	names := make([]string, 0, len(patches))
	for _, patch := range patches {
		if patch.Type != domain.PatchUnofficial {
			t.Errorf("patch type = %q, want unofficial: a portfile states no kind", patch.Type)
		}
		names = append(names, patch.File)
	}
	if strings.Join(names, ",") != "fix-cmake.patch,support-musl.diff" {
		t.Errorf("patches = %v, want the two the port applies and not the commented one", names)
	}
}

// A conandata.yml this reader cannot parse contributes nothing and says so
// (section 21): the alternative is a partial answer, and a partial patch
// record would publish "unmodified" for a component that was patched.
func TestAnUnreadableConandataIsReported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "conandata.yml"), "patches: &anchor\n  - patch_file: a.patch\n")
	patches, findings := Patches(root)
	if len(patches) != 0 {
		t.Errorf("patches = %#v from a file that was refused", patches)
	}
	if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
		t.Errorf("findings = %#v, want one EVIDENCE_UNREADABLE", findings)
	}
}

// A record over the bound of section 30 is not a record that says "no
// patches". It is one that was not read, and the two must not come out the
// same: the second would publish `unknown` for a component whose metadata
// states that it was patched.
func TestAPatchRecordOverTheBoundIsReported(t *testing.T) {
	for _, name := range []string{"conandata.yml", "portfile.cmake"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			padding := strings.Repeat("# padding\n", (maxPatchRecordBytes/10)+1)
			writeFile(t, filepath.Join(root, name), padding+"patches:\n  - patch_file: a.patch\n")
			patches, findings := Patches(root)
			if len(patches) != 0 {
				t.Errorf("patches = %#v from a file that was never read", patches)
			}
			if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
				t.Fatalf("findings = %#v, want one INPUT_LIMIT_EXCEEDED", findings)
			}
			// This reader is handed a directory and knows no component, so it
			// names none: the caller, which has one, fills the subject in. The
			// base name it used to put here is a physical path of section 7.9
			// -- for the project's own component it is the checkout directory
			// -- and it resolved to no component in the document.
			if findings[0].Subject.Kind != "component" || findings[0].Subject.Ref != "" {
				t.Errorf("subject = %+v, want the kind alone for the caller to name", findings[0].Subject)
			}
		})
	}
}

// The list is bounded and the answer is not: a component with more recorded
// patches than the bound is still modified, and the finding names the detail
// the document lost.
func TestMorePatchesThanTheBoundAreReportedAndTheAnswerStands(t *testing.T) {
	root := t.TempDir()
	var recipe strings.Builder
	recipe.WriteString("patches:\n")
	for index := 0; index < maxPatchesPerComponent+5; index++ {
		fmt.Fprintf(&recipe, "  - patch_file: \"p%03d.patch\"\n", index)
	}
	writeFile(t, filepath.Join(root, "conandata.yml"), recipe.String())

	patches, findings := Patches(root)
	if len(patches) != maxPatchesPerComponent {
		t.Errorf("patches = %d, want the bound of %d", len(patches), maxPatchesPerComponent)
	}
	if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
		t.Fatalf("findings = %#v, want one INPUT_LIMIT_EXCEEDED", findings)
	}
	// The first entries are kept, in the order section 29 requires, so the
	// same recipe read twice publishes the same list.
	if patches[0].File != "p000.patch" || patches[maxPatchesPerComponent-1].File !=
		fmt.Sprintf("p%03d.patch", maxPatchesPerComponent-1) {
		t.Errorf("the bounded list is %q .. %q", patches[0].File, patches[len(patches)-1].File)
	}
}

// The git root is the component root itself and never an enclosing one:
// asking git from inside a vendored directory would answer for the project.
func TestHasGitRootLooksAtTheRootItself(t *testing.T) {
	outer := t.TempDir()
	writeFile(t, filepath.Join(outer, ".git", "HEAD"), "ref: refs/heads/main\n")
	inner := filepath.Join(outer, "dep", "vendored")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasGitRoot(outer) {
		t.Error("the repository root was not recognized")
	}
	if HasGitRoot(inner) {
		t.Error("a directory inside a repository was taken for a repository root")
	}
	// A submodule's .git is a file, not a directory, and is just as much a
	// root.
	submodule := filepath.Join(outer, "dep", "submodule")
	writeFile(t, filepath.Join(submodule, ".git"), "gitdir: ../../.git/modules/submodule\n")
	if !HasGitRoot(submodule) {
		t.Error("a submodule working tree was not recognized")
	}
	if HasGitRoot("") {
		t.Error("the empty path was taken for a repository root")
	}
}
