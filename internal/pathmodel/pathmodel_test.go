package pathmodel

import (
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
