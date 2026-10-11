package generate

import (
	"runtime"
	"testing"

	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/pathmodel"
)

// hostReadAnchors is a build at /bd with a toolchain installed at /opt/gcc and
// a package cache at /cache/zlib, as the build machine had them.
func hostReadAnchors(t *testing.T) *anchors.Result {
	t.Helper()
	result, err := anchors.Assemble(anchors.Options{
		Flavor: pathmodel.PosixFlavor{}, ProjectRoot: "/src", BuildRoot: "/bd",
		Packages: []anchors.PackageAnchor{
			{Key: "toolchain:gcc", Root: "/opt/gcc"},
			{Key: "pkg:zlib", Root: "/cache/zlib"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func hostReadFindings(b *builder) []domain.Finding {
	var found []domain.Finding
	for _, finding := range b.findings {
		if finding.ID == "HOST_FILES_NOT_READ" || finding.ID == "HOST_FILES_READ" || finding.ID == "MISSING_FILE_HASH" {
			found = append(found, finding)
		}
	}
	return found
}

// TestARelocatedRunDoesNotReadTheToolchainOfThisHost is section 7.9 rule 8:
// evidence from another machine names its toolchain by absolute path, and the
// file at that path here is this host's. It is not read -- no location, so no
// hash and no debug information -- and the run says so once, not once per file.
// Where the file lies is still known, because classification needs it.
func TestARelocatedRunDoesNotReadTheToolchainOfThisHost(t *testing.T) {
	b := newBuilder(evidence.New(), hostReadAnchors(t), "/bd", t.TempDir(), NewLogger(0, nil))

	for _, path := range []string{"/opt/gcc/lib/crtbeginS.o", "/opt/gcc/lib/libgcc.a", "/elsewhere/vendor.o"} {
		canonical, _ := b.identify(path)
		if got := b.physical[canonical]; got != "" {
			t.Errorf("%s is read at %q on a host that did not build", canonical, got)
		}
		if got := b.location(canonical); got != path {
			t.Errorf("%s lies at %q, want %q", canonical, got, path)
		}
	}
	// Rule 7: a package cache keeps the path the build recorded.
	canonical, _ := b.identify("/cache/zlib/lib/libz.a")
	if got := b.physical[canonical]; got != "/cache/zlib/lib/libz.a" {
		t.Errorf("the package cache file is read at %q", got)
	}

	findings := hostReadFindings(b)
	if len(findings) != 1 || findings[0].ID != "HOST_FILES_NOT_READ" || findings[0].Severity != domain.SeverityWarning {
		t.Errorf("findings = %+v, want one HOST_FILES_NOT_READ warning", findings)
	}
}

// TestALocalRunReadsTheToolchainAndSaysSo: on the machine that built, the files
// the evidence names are the ones that were linked, and they are read as
// before. That the document now rests on them is stated once, as information.
func TestALocalRunReadsTheToolchainAndSaysSo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the build root /bd is not an absolute path on Windows")
	}
	b := newBuilder(evidence.New(), hostReadAnchors(t), "/bd", "/bd", NewLogger(0, nil))
	canonical, _ := b.identify("/opt/gcc/lib/crtbeginS.o")
	if got := b.physical[canonical]; got != "/opt/gcc/lib/crtbeginS.o" {
		t.Errorf("the toolchain file is read at %q on the machine that built", got)
	}
	b.identify("/opt/gcc/lib/libgcc.a")
	findings := hostReadFindings(b)
	if len(findings) != 1 || findings[0].ID != "HOST_FILES_READ" || findings[0].Severity != domain.SeverityInfo {
		t.Errorf("findings = %+v, want one HOST_FILES_READ information", findings)
	}
}

// TestALocalRunIsNotTakenForARelocatedOneOverASpelling: the build root the
// configuration names may carry a trailing separator, a `..` or be relative;
// none of that moves the build, and a local run must still read its toolchain.
func TestALocalRunIsNotTakenForARelocatedOneOverASpelling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the build root /bd is not an absolute path on Windows")
	}
	for logical, want := range map[string]bool{
		"/bd": false, "/bd/": false, "/x/../bd": false, "//bd": false, "build": false,
		"/elsewhere": true,
	} {
		b := newBuilder(evidence.New(), hostReadAnchors(t), logical, "/bd", NewLogger(0, nil))
		if got := b.relocatesBuild(); got != want {
			t.Errorf("logical build root %q read at /bd: relocated = %v, want %v", logical, got, want)
		}
	}
}

// TestAToolchainInATreeIsReadFromTheTree: a toolchain checked into the source
// tree is read from it when only the build tree moved. It is no file of the
// host, whatever its anchor.
func TestAToolchainInATreeIsReadFromTheTree(t *testing.T) {
	result, err := anchors.Assemble(anchors.Options{
		Flavor: pathmodel.PosixFlavor{}, ProjectRoot: "/src", BuildRoot: "/bd",
		Packages: []anchors.PackageAnchor{{Key: "toolchain:vendored", Root: "/src/toolchains/gcc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := newBuilder(evidence.New(), result, "/bd", t.TempDir(), NewLogger(0, nil))
	b.setSourceRoots("/src", "/src", pathmodel.PosixFlavor{})
	canonical, scope := b.identify("/src/toolchains/gcc/lib/crt1.o")
	if scope != anchors.ScopeToolchain {
		t.Fatalf("scope = %s, want toolchain; the test checks nothing", scope)
	}
	if got := b.physical[canonical]; got != "/src/toolchains/gcc/lib/crt1.o" {
		t.Errorf("the vendored toolchain file is read at %q", got)
	}
	if findings := hostReadFindings(b); len(findings) != 0 {
		t.Errorf("findings = %+v for a file in the source tree", findings)
	}
}
