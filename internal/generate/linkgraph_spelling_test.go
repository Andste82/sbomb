package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
)

// TestWhereAFileIsReadDoesNotDependOnWhichSpellingCameFirst: one file named
// several ways -- a Ninja graph's escaped absolute path, a path relative to
// the build directory, a compilation database's own spelling -- resolves to
// one identity, but not every spelling to a location that can be read. The
// location kept must not depend on the order the spellings arrive in: it did,
// and a Windows build read on Linux was hashed on one run and reported missing
// on the next, which changed the reproducible document identity with it.
func TestWhereAFileIsReadDoesNotDependOnWhichSpellingCameFirst(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.c"), []byte("int a;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const canonical = "build:sub/a.c"
	want := filepath.Join(dir, "sub", "a.c")
	// The first spelling cannot be read on this host, the second can, the
	// third names a file that is not there.
	spellings := []string{`sub\a.c`, "sub/a.c", "gone/a.c"}
	for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		b := newBuilder(evidence.New(), assembledAnchors(t), dir, dir, NewLogger(0, nil))
		for position, index := range order {
			if position == 0 {
				b.physical[canonical], _ = b.physicalFor(b.logicalFor(spellings[index]))
				continue
			}
			b.reconsiderPhysical(canonical, b.physical[canonical], spellings[index])
		}
		if got := b.physical[canonical]; got != want {
			t.Errorf("order %v: the file is read at %q, want %q", order, got, want)
		}
	}

	// A refused read recorded for the first spelling is withdrawn when a later
	// one can be read: the file is hashed after all.
	b := newBuilder(evidence.New(), assembledAnchors(t), dir, dir, NewLogger(0, nil))
	b.physical[canonical] = ""
	b.refusedReads[canonical] = true
	b.findings = append(b.findings, domain.Finding{ID: "MISSING_FILE_HASH", Subject: domain.Subject{Kind: "file", Ref: canonical}, Message: refusedReadMessage})
	b.reconsiderPhysical(canonical, "", "sub/a.c")
	if b.physical[canonical] != want || len(b.findings) != 0 {
		t.Errorf("physical %q, findings %v", b.physical[canonical], b.findings)
	}
}

// TestEveryCompileSpellingIsSettledBeforeAnythingReads: a reader asks for a
// file by the spelling its own evidence gives, and the spelling that can be
// read may come from evidence about another object -- a header list, a forced
// include. Without settling every compile-side spelling first, the reader
// opened what it was handed, and whether that was the readable spelling
// depended on the order the stages ranged over their maps.
func TestEveryCompileSpellingIsSettledBeforeAnythingReads(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.c"), []byte("int a;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := newBuilder(evidence.New(), assembledAnchors(t), dir, dir, NewLogger(0, nil))
	compile := newCompileEvidence()
	// The object's own evidence names its source in a spelling this host may
	// not be able to read; another object's header list names it readably.
	compile.objectSources["one.o"] = `sub\a.c`
	compile.objectHeaders["two.o"] = []string{"sub/a.c"}

	settleLocations(b, compile)

	canonical, _ := b.identify(`sub\a.c`)
	if got, want := b.physical[canonical], filepath.Join(dir, "sub", "a.c"); got != want {
		t.Errorf("the reader of one.o's source opens %q, want %q", got, want)
	}
}
