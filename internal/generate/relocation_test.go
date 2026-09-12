package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/adapters/pkgmanager"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/pathmodel"
)

// Section 7.9: the source tree may be read somewhere other than where it was
// built. These tests hold the two halves of that apart -- what is read moves,
// what the document says does not.

// relocatedBuilder is a builder whose evidence names /src and whose source tree
// is at the returned directory.
func relocatedBuilder(t *testing.T, flavor pathmodel.Flavor, logicalSource string) (*builder, string) {
	t.Helper()
	tree := t.TempDir()
	b := newBuilder(evidence.New(), assembledAnchors(t), "/bd", "/bd", NewLogger(0, nil))
	b.setSourceRoots(logicalSource, tree, flavor)
	return b, tree
}

// The point of the whole mechanism: relocating a tree relocates the read and
// nothing else. The identity a relocated run computes is the identity the
// un-relocated run computes, character for character.
func TestSourceRelocationLeavesIdentityAlone(t *testing.T) {
	relocated, tree := relocatedBuilder(t, pathmodel.PosixFlavor{}, "/src")
	local := newBuilder(evidence.New(), assembledAnchors(t), "/bd", "/bd", NewLogger(0, nil))

	canonical, scope := relocated.identify("/src/dep/mit-lib/src/mit_a.c")
	wantCanonical, wantScope := local.identify("/src/dep/mit-lib/src/mit_a.c")
	if canonical != wantCanonical {
		t.Errorf("canonical = %q, want %q", canonical, wantCanonical)
	}
	if scope != wantScope {
		t.Errorf("scope = %q, want %q", scope, wantScope)
	}
	if got, want := relocated.physical[canonical], filepath.Join(tree, "dep/mit-lib/src/mit_a.c"); got != want {
		t.Errorf("physical path = %q, want %q", got, want)
	}
	if got := local.physical[wantCanonical]; got != "/src/dep/mit-lib/src/mit_a.c" {
		t.Errorf("the un-relocated run reads from %q, want the path the evidence recorded", got)
	}
}

// The inverse direction, for an adapter that reports where it read something:
// a path in the relocated tree is expressed in the logical root before it is
// identified, so the relocation cannot reach the document that way either.
func TestLogicalForInvertsSourceRelocation(t *testing.T) {
	b, tree := relocatedBuilder(t, pathmodel.PosixFlavor{}, "/src")
	if got, want := b.logicalFor(filepath.Join(tree, "dep/mit-lib/LICENSE")), "/src/dep/mit-lib/LICENSE"; got != want {
		t.Errorf("logicalFor = %q, want %q", got, want)
	}
	if got := b.logicalFor("/elsewhere/LICENSE"); got != "/elsewhere/LICENSE" {
		t.Errorf("a path outside the relocated tree was rewritten to %q", got)
	}
}

// Section 7.9 rule 3 and section 30.4: build evidence is untrusted input, so a
// recorded path that climbs out of the source root is refused rather than
// resolved against the directory the caller named. It is refused outright:
// there is no flag that permits it.
//
// The logical source root exists on this machine as well, which is the case
// that decides whether the check is real: relocating to a restored copy while
// the original checkout is still present. The kernel resolves the `..` happily,
// so a refusal that returned the path would be no refusal at all.
func TestRelocatedReadLeavingTheSourceRootIsRefused(t *testing.T) {
	base := t.TempDir()
	logical := filepath.Join(base, "checkout")
	tree := filepath.Join(base, "restored")
	for _, dir := range []string{logical, tree, filepath.Join(base, "outside")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "outside", "secret.c"), []byte("int secret;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := newBuilder(evidence.New(), assembledAnchors(t), "/bd", "/bd", NewLogger(0, nil))
	b.setSourceRoots(logical, tree, pathmodel.PosixFlavor{})

	escaping := logical + "/../outside/secret.c"
	// Without the containment check this path is readable, and the assertions
	// below would pass while the bytes were being read. Establish that first, so
	// the test cannot quietly stop testing anything.
	if _, err := os.ReadFile(escaping); err != nil {
		t.Fatalf("the escaping path is not readable here, so the test proves nothing: %v", err)
	}
	if got, readable := b.physicalFor(escaping); readable || got != "" {
		t.Errorf("physicalFor = (%q, %v); want no path and a refusal", got, readable)
	}
	// And nothing the builder hands its readers can reach the file either: the
	// physical map is where every read starts.
	canonical, _ := b.identify(escaping)
	if got := b.physical[canonical]; got != "" {
		t.Errorf("the builder recorded %q for a refused read", got)
	}
	// Refused reads are not silent (section 7.9 rule 3, section 7.6).
	refusals := 0
	for _, finding := range b.findings {
		if finding.ID == "MISSING_FILE_HASH" && finding.Subject.Ref == canonical {
			refusals++
		}
	}
	if refusals != 1 {
		t.Errorf("MISSING_FILE_HASH for the refused read appears %d time(s), want 1", refusals)
	}
	// A path that stays inside is still relocated, so the refusal is about the
	// escape and not about the mechanism failing.
	if got, readable := b.physicalFor(logical + "/inside.c"); !readable || got != filepath.Join(tree, "inside.c") {
		t.Errorf("physicalFor = (%q, %v), want %q", got, readable, filepath.Join(tree, "inside.c"))
	}
}

// Section 7.9 rule 1, at the one place where it is easy to lose: a package
// anchor. The adapters are pointed at the relocated tree because they open
// files, so the roots they report are physical -- and an anchor root is what
// file identities are resolved against, so a physical one would anchor a
// relocated run differently from the run on the build machine.
func TestPackageAnchorsAreRegisteredInTheLogicalSourceRoot(t *testing.T) {
	tree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tree, "extern", "tinylog"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitmodules := "[submodule \"tinylog\"]\n\tpath = extern/tinylog\n\turl = https://example.invalid/tinylog.git\n"
	if err := os.WriteFile(filepath.Join(tree, ".gitmodules"), []byte(gitmodules), 0o644); err != nil {
		t.Fatal(err)
	}

	// identityOfSubmoduleFile is one whole run of the chain the defect lives in:
	// discover packages where the bytes are, register the anchors, then resolve
	// a path the evidence recorded. With logicalSource == physicalSource it is
	// the run on the build machine; with the two apart it is the relocated run.
	identityOfSubmoduleFile := func(t *testing.T, logicalSource string) string {
		t.Helper()
		flavor := pathmodel.PosixFlavor{}
		packages, _ := pkgmanager.Discover(pkgmanager.Options{BuildDir: "/bd", SourceDir: tree})
		packageAnchors := make([]anchors.PackageAnchor, 0, len(packages))
		for _, entry := range packages {
			if entry.AnchorKey == "" || entry.Root() == "" {
				continue
			}
			packageAnchors = append(packageAnchors, anchors.PackageAnchor{
				Key:  entry.AnchorKey,
				Root: logicalPackageRoot(entry.Root(), logicalSource, tree, flavor),
			})
		}
		if len(packageAnchors) != 1 {
			t.Fatalf("the .gitmodules yielded %d package anchor(s), want 1", len(packageAnchors))
		}
		result, err := anchors.Assemble(anchors.Options{
			Flavor: flavor, ProjectRoot: logicalSource, BuildRoot: "/bd", Packages: packageAnchors,
		})
		if err != nil {
			t.Fatal(err)
		}
		b := newBuilder(evidence.New(), result, "/bd", "/bd", NewLogger(0, nil))
		b.setSourceRoots(logicalSource, tree, flavor)
		canonical, _ := b.identify(logicalSource + "/extern/tinylog/src/log.c")
		return canonical
	}

	relocated := identityOfSubmoduleFile(t, "/src")
	local := identityOfSubmoduleFile(t, tree)
	if relocated != local {
		t.Errorf("the relocated run identifies the file as %q and the local run as %q", relocated, local)
	}
	if relocated != "extern:tinylog:src/log.c" {
		t.Errorf("canonical = %q, want the submodule anchor to have matched", relocated)
	}
}

// A root the relocation does not concern is handed on untouched: rule 7 moves
// the source root and nothing else, and a package cache path comes out of a
// file the build wrote.
func TestLogicalPackageRootLeavesEveryOtherRootAlone(t *testing.T) {
	tree := t.TempDir()
	cases := []struct {
		name                    string
		root, logical, physical string
		want                    string
	}{
		{name: "no relocation", root: "/src/extern/foo", logical: "/src", physical: "/src", want: "/src/extern/foo"},
		{name: "no logical root", root: filepath.Join(tree, "extern/foo"), logical: "", physical: tree, want: filepath.Join(tree, "extern/foo")},
		{name: "a package cache", root: "/home/ci/.conan2/p/abcd/p", logical: "/src", physical: tree, want: "/home/ci/.conan2/p/abcd/p"},
		{name: "the build tree", root: "/bd/_deps/tinylog-src", logical: "/src", physical: tree, want: "/bd/_deps/tinylog-src"},
		{name: "the relocated tree", root: filepath.Join(tree, "extern/foo"), logical: "/src", physical: tree, want: "/src/extern/foo"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := logicalPackageRoot(testCase.root, testCase.logical, testCase.physical, pathmodel.PosixFlavor{})
			if got != testCase.want {
				t.Errorf("logicalPackageRoot = %q, want %q", got, testCase.want)
			}
		})
	}
}

// Section 7.9 rule 4: a Windows build records both separators, sometimes in one
// path, and the comparison against the source root is case-insensitive there.
func TestSourceRelocationAcceptsTheWindowsPathFlavour(t *testing.T) {
	b, tree := relocatedBuilder(t, pathmodel.WindowsFlavor{}, `C:/Source/p14`)
	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "backslashes", path: `C:\Source\p14\dep\mit-lib\LICENSE`, want: filepath.Join(tree, "dep", "mit-lib", "LICENSE")},
		{name: "mixed separators", path: `C:\Source\p14/dep\mit-lib/mit_a.c`, want: filepath.Join(tree, "dep", "mit-lib", "mit_a.c")},
		{name: "another case", path: `c:/source/P14/src/main.c`, want: filepath.Join(tree, "src", "main.c")},
		{name: "the root itself", path: `C:/Source/p14`, want: tree},
		{name: "a sibling that merely shares a prefix", path: `C:/Source/p14x/src/main.c`, want: `C:/Source/p14x/src/main.c`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, readable := b.physicalFor(testCase.path)
			if !readable || got != testCase.want {
				t.Errorf("physicalFor(%q) = (%q, %v), want %q", testCase.path, got, readable, testCase.want)
			}
		})
	}
}

// Under the POSIX flavour the same comparison is case-sensitive, because two
// directories that differ in case are two directories.
func TestSourceRelocationIsCaseSensitiveUnderPosix(t *testing.T) {
	b, _ := relocatedBuilder(t, pathmodel.PosixFlavor{}, "/Source/p14")
	if got, readable := b.physicalFor("/source/p14/src/main.c"); !readable || got != "/source/p14/src/main.c" {
		t.Errorf("physicalFor = (%q, %v); a path of another case was relocated", got, readable)
	}
}

// Nothing is relocated when the two roots are the same, and nothing is
// relocated when there is no logical source root at all -- which is the case
// without a File API reply (decision Q16).
func TestRelocationIsInactiveWithoutTwoRoots(t *testing.T) {
	same := newBuilder(evidence.New(), assembledAnchors(t), "/bd", "/bd", NewLogger(0, nil))
	same.setSourceRoots("/src", "/src", pathmodel.PosixFlavor{})
	if same.relocatesSource() {
		t.Error("a source root that did not move is treated as relocated")
	}
	none := newBuilder(evidence.New(), assembledAnchors(t), "/bd", "/bd", NewLogger(0, nil))
	none.setSourceRoots("", t.TempDir(), pathmodel.PosixFlavor{})
	if none.relocatesSource() {
		t.Error("relocation is active without a logical source root to relocate from")
	}
	if got, readable := none.physicalFor("/src/main.c"); !readable || got != "/src/main.c" {
		t.Errorf("physicalFor = (%q, %v), want the recorded path", got, readable)
	}
}
