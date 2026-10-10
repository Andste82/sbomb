package pathmodel

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	if got := Resolve("/tmp/project/src", "/tmp/project", "/tmp/project"); got != "project:src" {
		t.Fatalf("Resolve() = %q, want %q", got, "project:src")
	}
	if got := Resolve("/tmp/project/../project/src", "/tmp/project", "/tmp/project"); got != "project:src" {
		t.Fatalf("Resolve() = %q, want %q", got, "project:src")
	}
	if got := Slug("/tmp/project/src/very-long-name/alpha-beta-gamma", 16); got == "" || len(got) > 32 {
		t.Fatalf("Slug() = %q, length too long", got)
	}
}

func TestWindowsFlavor(t *testing.T) {
	flavor := WindowsFlavor{}
	if got := ResolveWithFlavor(`C:\work\project\src\main.c`, `c:/work/project`, `C:/work/build`, flavor); got != `project:src/main.c` {
		t.Fatalf("ResolveWithFlavor() = %q, want %q", got, `project:src/main.c`)
	}
	if got := ResolveWithFlavor(`\\server\share\project\src\main.c`, `\\SERVER\SHARE\PROJECT`, ``, flavor); got != `project:src/main.c` {
		t.Fatalf("UNC ResolveWithFlavor() = %q, want %q", got, `project:src/main.c`)
	}
}

func TestWindowsFlavorOnLinux(t *testing.T) {
	flavor := WindowsFlavor{}
	paths := map[string]string{
		`C:\fixture\project\src\main.c`:              `project:src/main.c`,
		`c:/FIXTURE/PROJECT/src/../include/config.h`: `project:include/config.h`,
		`C:\fixture\build\generated\version.h`:       `build:generated/version.h`,
	}
	if got := ResolveWithFlavor(`\\server\share\project\src\main.c`, `\\SERVER\SHARE\PROJECT`, ``, flavor); got != `project:src/main.c` {
		t.Errorf("UNC ResolveWithFlavor() = %q, want %q", got, `project:src/main.c`)
	}
	for input, want := range paths {
		if got := ResolveWithFlavor(input, `C:\fixture\project`, `C:\fixture\build`, flavor); got != want {
			t.Errorf("ResolveWithFlavor(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWindowsFlavorUnescapesNinjaDriveColon(t *testing.T) {
	flavor := WindowsFlavor{}
	if got := ResolveWithFlavor(`C$:\fixture\project\src\main.c`, `C:/fixture/project`, `C:/fixture/build`, flavor); got != "project:src/main.c" {
		t.Fatalf("ResolveWithFlavor(C$:) = %q, want project:src/main.c", got)
	}
}

// TestIsAbsolutePortable: the answer is the same on every host. A POSIX
// absolute path from Linux evidence is absolute when a Windows host reads it,
// as a Windows drive path is when a Linux host does.
func TestIsAbsolutePortable(t *testing.T) {
	for _, path := range []string{"/tmp/build", `C:\build\app.obj`, "C:/build/app.obj", `\\server\share\app.obj`} {
		if !IsAbsolute(path) {
			t.Errorf("IsAbsolute(%q) = false, want true", path)
		}
	}
	if IsAbsolute("build/app.obj") {
		t.Error("IsAbsolute(relative path) = true, want false")
	}
}

func TestNormalizeSeparators(t *testing.T) {
	if got := NormalizeSeparators(`C:\build\main.obj`); got != "C:/build/main.obj" {
		t.Fatalf("NormalizeSeparators() = %q, want C:/build/main.obj", got)
	}
}

func TestSlugFollowsTheSpecifiedNormalization(t *testing.T) {
	cases := map[string]string{
		"mbedTLS":          "mbedtls",
		"my project/name":  "my-project-name",
		"  spaced  out  ":  "spaced-out",
		"Already-Fine_1.2": "already-fine_1.2",
		"///":              "unnamed",
		"a---b":            "a-b",
	}
	for input, want := range cases {
		if got := Slug(input, 64); got != want {
			t.Errorf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSlugTruncatesWithADigest(t *testing.T) {
	long := strings.Repeat("component-name-", 20)
	got := Slug(long, 32)
	if len(got) > 32 {
		t.Errorf("Slug() = %q, longer than the limit", got)
	}
	other := Slug(long+"x", 32)
	if got == other {
		t.Error("two different long names produced the same slug")
	}
}

// TestAnEvidencePathMeansTheSameFileOnEveryHost: build evidence names paths of
// the machine that built the project. A path in a foreign spelling -- POSIX on
// Windows, Windows on Linux -- is cleaned in slash form by the same rule on
// every host; one that is the host's own keeps the host's spelling, so that it
// matches the paths sbomb joins onto a directory it reads. Either way the file
// is the same, which is what is compared: filepath alone turned /usr/bin/cc
// into \usr\bin\cc on Windows, and the same evidence gave other identities
// there than on Linux.
func TestAnEvidencePathMeansTheSameFileOnEveryHost(t *testing.T) {
	same := func(name, got, want string) {
		t.Helper()
		if NormalizeSeparators(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	same("CleanEvidence(/usr/lib/../bin/cc)", CleanEvidence("/usr/lib/../bin/cc"), "/usr/bin/cc")
	same(`CleanEvidence(C:\src\inc\..\a.h)`, CleanEvidence(`C:\src\inc\..\a.h`), "C:/src/a.h")
	same("CleanEvidence(C:/)", CleanEvidence("C:/"), "C:/")
	same(`CleanEvidence(\\server\share\x)`, CleanEvidence(`\\server\share\x`), "//server/share/x")
	same("JoinEvidence(/b, ../src/a.c)", JoinEvidence("/b", "../src/a.c"), "/src/a.c")
	same("JoinEvidence(/b, /abs/a.c)", JoinEvidence("/b", "/abs/a.c"), "/abs/a.c")
	same(`JoinEvidence(C:\b, ..\src\a.c)`, JoinEvidence(`C:\b`, `..\src\a.c`), "C:/src/a.c")
	same(`JoinEvidence(\\server\share, a.c)`, JoinEvidence(`\\server\share`, "a.c"), "//server/share/a.c")
	same("DirEvidence(/usr/bin/cc)", DirEvidence("/usr/bin/cc"), "/usr/bin")
	// Found in the review of #65: a UNC directory lost its second slash, a
	// backslash in a name joined to a host directory stayed one, and two
	// leading forward slashes were taken for UNC rather than read as POSIX.
	same(`DirEvidence(\\server\share\bin\cc)`, DirEvidence(`\\server\share\bin\cc`), "//server/share/bin")
	same(`JoinEvidence(/b, sub\a.c)`, JoinEvidence("/b", `sub\a.c`), "/b/sub/a.c")
	same("CleanEvidence(//usr/include/a.h)", CleanEvidence("//usr/include/a.h"), "/usr/include/a.h")
	same("DirEvidence(C:/bin)", DirEvidence("C:/bin"), "C:/")
	same("BaseEvidence(/opt/gcc/bin/)", BaseEvidence("/opt/gcc/bin/"), "bin")
	same("ResolveEvidence(host, /usr/include/a.h)", ResolveEvidence(t.TempDir(), "/usr/include/a.h"), "/usr/include/a.h")
	host := t.TempDir()
	same("ResolveEvidence(host, sub/a.c)", ResolveEvidence(host, "sub/a.c"), NormalizeSeparators(filepath.Join(host, "sub", "a.c")))
}

// TestBothFlavorsReadADotPathAlike: a relative path that cleans to nothing is
// the directory it is relative to, and ".." above it survives, under either
// flavor. Since a relative Windows path stays relative, the Windows flavor
// answered "" for "." -- an identity of no file -- where the POSIX flavor
// answers "/", so a run given --source-dir . under the Windows flavor lost its
// source root.
func TestBothFlavorsReadADotPathAlike(t *testing.T) {
	for _, p := range []string{".", "./", "a/..", `a\..`, "a/../../b", "..", "../x"} {
		posix, windows := PosixFlavor{}.Normalize(p), WindowsFlavor{}.Normalize(p)
		if posix != windows || windows == "" {
			t.Errorf("Normalize(%q): posix %q, windows %q", p, posix, windows)
		}
	}
}
