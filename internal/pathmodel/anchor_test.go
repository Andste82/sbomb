package pathmodel

import (
	"strings"
	"testing"
)

func mustRegister(t *testing.T, r *Registry, key, root string) {
	t.Helper()
	if _, err := r.Register(key, root, "test"); err != nil {
		t.Fatalf("Register(%q, %q): %v", key, root, err)
	}
}

func TestLongestMatchingAnchorWins(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "project", "/src")
	mustRegister(t, r, "sdk:espidf", "/src/vendor/esp-idf")
	mustRegister(t, r, "pkg:conan/mbedtls", "/src/vendor/esp-idf/components/mbedtls")

	cases := map[string]string{
		"/src/main.c": "project:main.c",
		"/src/vendor/esp-idf/components/driver/gpio.c": "sdk:espidf:components/driver/gpio.c",
		"/src/vendor/esp-idf/components/mbedtls/aes.c": "pkg:conan/mbedtls:aes.c",
	}
	for input, want := range cases {
		if got := r.Resolve(input).Canonical(); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPrefixMatchingRespectsSegmentBoundaries(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "project", "/usr/lib")

	// /usr/libx is a sibling of /usr/lib, not a child of it.
	if got := r.Resolve("/usr/libx/file.c").Canonical(); got != "abs:usr/libx/file.c" {
		t.Errorf("Resolve(/usr/libx/file.c) = %q, want the abs anchor", got)
	}
	if got := r.Resolve("/usr/lib/file.c").Canonical(); got != "project:file.c" {
		t.Errorf("Resolve(/usr/lib/file.c) = %q, want project:file.c", got)
	}
}

func TestAnchorRootItselfResolvesToDot(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "build", "/build")
	if got := r.Resolve("/build").Canonical(); got != "build:." {
		t.Errorf("Resolve(/build) = %q, want build:.", got)
	}
}

func TestLaterRegistrationDoesNotOverrideSameDirectory(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "project", "/src")
	// Section 7.4: a later source must not take over a directory an earlier
	// one already claimed.
	added, err := r.Register("extern:vendor", "/src", "later")
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Fatal("a second anchor claimed an already registered directory")
	}
	if got := r.Resolve("/src/main.c").Canonical(); got != "project:main.c" {
		t.Errorf("Resolve() = %q, want the first registration to win", got)
	}
}

func TestSameKeyTwiceIsAnError(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "extern:shared", "/a")
	if _, err := r.Register("extern:shared", "/b", "test"); err == nil {
		t.Fatal("registering one key at two directories should fail")
	}
}

func TestUnanchoredFilesUseTheAbsAnchor(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "project", "/src")
	got := r.Resolve("/opt/vendor/blob.a")
	if got.Canonical() != "abs:opt/vendor/blob.a" {
		t.Errorf("Resolve() = %q, want abs:opt/vendor/blob.a", got.Canonical())
	}
}

func TestRedactionReplacesUnanchoredPaths(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "project", "/src")
	r.SetRedactUnanchored(true)

	got := r.Resolve("/home/alice/.conan2/p/ab12/include/aes.h").Canonical()
	if strings.Contains(got, "alice") {
		t.Fatalf("redacted identity still leaks the original path: %q", got)
	}
	if !strings.HasPrefix(got, "abs:redacted/") {
		t.Fatalf("Resolve() = %q, want an abs:redacted/ identity", got)
	}
	if len(strings.TrimPrefix(got, "abs:redacted/")) != 16 {
		t.Fatalf("redacted digest should be 16 hex characters, got %q", got)
	}
	// Anchored paths must be unaffected by redaction.
	if anchored := r.Resolve("/src/main.c").Canonical(); anchored != "project:main.c" {
		t.Errorf("redaction changed an anchored path: %q", anchored)
	}
}

func TestRelativePathsResolveAgainstTheGivenBase(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "build", "/build")
	// A depfile names its object relative to the build directory.
	if got := r.ResolveIn("/build", "CMakeFiles/app.dir/main.c.o").Canonical(); got != "build:CMakeFiles/app.dir/main.c.o" {
		t.Errorf("ResolveIn() = %q, want build:CMakeFiles/app.dir/main.c.o", got)
	}
	// An absolute path ignores the base.
	if got := r.ResolveIn("/build", "/build/app").Canonical(); got != "build:app" {
		t.Errorf("ResolveIn() = %q, want build:app", got)
	}
}

func TestRelativePathsAreNormalizedBeforeAnchoring(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "project", "/src")
	mustRegister(t, r, "build", "/build")
	// "../src/main.c" seen from the build directory belongs to the project.
	if got := r.ResolveIn("/build", "../src/main.c").Canonical(); got != "project:main.c" {
		t.Errorf("ResolveIn() = %q, want project:main.c", got)
	}
}

func TestRelativePathsNeverContainParentSegments(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "project", "/src")
	for _, input := range []string{"/src/a/../b/main.c", "/src/./nested/../main.c"} {
		got := r.Resolve(input)
		if strings.Contains(got.RelPath, "..") {
			t.Errorf("Resolve(%q) produced a relative path with a parent segment: %q", input, got.RelPath)
		}
	}
}

func TestWindowsFlavorMatchesCaseInsensitively(t *testing.T) {
	r := NewRegistry(WindowsFlavor{})
	mustRegister(t, r, "project", `C:\Work\Project`)
	if got := r.Resolve(`c:/work/PROJECT/src/main.c`).Canonical(); got != "project:src/main.c" {
		t.Errorf("Resolve() = %q, want project:src/main.c", got)
	}
}

func TestPosixFlavorMatchesCaseSensitively(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	mustRegister(t, r, "project", "/Work/Project")
	if got := r.Resolve("/work/project/main.c").Canonical(); got != "abs:work/project/main.c" {
		t.Errorf("Resolve() = %q, want the abs anchor under POSIX case rules", got)
	}
}

func TestWindowsUnanchoredPathDropsTheDriveColon(t *testing.T) {
	r := NewRegistry(WindowsFlavor{})
	mustRegister(t, r, "project", `C:\Work\Project`)
	got := r.Resolve(`D:\vendor\blob.lib`).Canonical()
	if got != "abs:D/vendor/blob.lib" {
		t.Errorf("Resolve() = %q, want abs:D/vendor/blob.lib", got)
	}
	if strings.Contains(got, `\`) {
		t.Errorf("canonical path must not contain backslashes: %q", got)
	}
}

func TestAnchorKeyValidation(t *testing.T) {
	valid := []string{"project", "build", "sdk:espidf", "pkg:conan/mbedtls", "toolchain:gcc-13", "sysroot:arm", "extern:shared"}
	for _, key := range valid {
		if err := ValidateAnchorKey(key); err != nil {
			t.Errorf("ValidateAnchorKey(%q) = %v, want nil", key, err)
		}
	}
	invalid := []string{"", "project:root", "sdk", "pkg:", "abs", "abs:x", "vendor:foo"}
	for _, key := range invalid {
		if err := ValidateAnchorKey(key); err == nil {
			t.Errorf("ValidateAnchorKey(%q) = nil, want an error", key)
		}
	}
}

func TestEmptyRootIsIgnored(t *testing.T) {
	r := NewRegistry(PosixFlavor{})
	added, err := r.Register("sysroot:none", "", "test")
	if err != nil || added {
		t.Fatalf("Register with an empty root = (%v, %v), want (false, nil)", added, err)
	}
	if len(r.Anchors()) != 0 {
		t.Fatalf("an empty root should register no anchor, got %v", r.Anchors())
	}
}
