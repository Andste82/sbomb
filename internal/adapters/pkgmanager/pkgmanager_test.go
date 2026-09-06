package pkgmanager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/exec"
)

// writePopulate lays out the files CMake generates for a FetchContent
// dependency, so the adapter is tested against the shape it really meets.
func writePopulate(t *testing.T, buildDir, name, repository, tag string) {
	t.Helper()
	tmp := filepath.Join(buildDir, "_deps", name+"-subbuild", name+"-populate-prefix", "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(buildDir, "_deps", name+"-src"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `
execute_process(
  COMMAND "/usr/bin/git" clone --no-checkout --config "advice.detachedHead=false" "` + repository + `" "` + name + `-src"
  WORKING_DIRECTORY "/build/_deps"
)
execute_process(
  COMMAND "/usr/bin/git" checkout "` + tag + `" --
  WORKING_DIRECTORY "/build/_deps/` + name + `-src"
)
`
	path := filepath.Join(tmp, name+"-populate-gitclone.cmake")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFetchContentReadsNameVersionAndRepository(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.2.0")

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Name != "tinylog" || found.Version != "1.2.0" {
		t.Errorf("name/version = %q/%q", found.Name, found.Version)
	}
	if found.VersionSource != "fetchcontent" || found.VersionConfidence != "high" {
		t.Errorf("version source = %q, confidence = %q", found.VersionSource, found.VersionConfidence)
	}
	if found.VCSURL != "https://example.invalid/org/tinylog" {
		t.Errorf("vcs url = %q, want the .git suffix removed", found.VCSURL)
	}
	if found.Supplier != "" {
		t.Error("a supplier was invented; section 20.5 forbids deriving it from a repository host")
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v", findings)
	}
}

// The corpus commits build evidence, not sources. A build tree analysed
// somewhere other than where it was produced still has to yield its packages.
func TestAPackageIsFoundWithoutItsCheckedOutSources(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.2.0")
	if err := os.RemoveAll(filepath.Join(build, "_deps", "tinylog-src")); err != nil {
		t.Fatal(err)
	}
	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || packages[0].Name != "tinylog" {
		t.Fatalf("packages = %#v", packages)
	}
	if !filepath.IsAbs(packages[0].Root) && packages[0].Root == "" {
		t.Error("the package has no root to map files against")
	}
}

func TestAPopulatedDependencyWithNoTagIsReported(t *testing.T) {
	build := t.TempDir()
	if err := os.MkdirAll(filepath.Join(build, "_deps", "mystery-src"), 0o755); err != nil {
		t.Fatal(err)
	}
	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || packages[0].Version != "" {
		t.Fatalf("packages = %#v", packages)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "UNKNOWN_VERSION" {
			reported = true
		}
	}
	if !reported {
		t.Error("a dependency with no version was accepted silently")
	}
}

// Introspection is off by default, so the adapter must be useful without it.
func TestTheAdapterWorksWithIntrospectionDisabled(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.2.0")
	packages, _ := Discover(Options{
		BuildDir: build,
		Runner:   &exec.Runner{},
		Context:  context.Background(),
	})
	if len(packages) != 1 || packages[0].Version != "1.2.0" {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestVCSURLsAreNormalizedAndStrippedOfCredentials(t *testing.T) {
	cases := map[string]string{
		"git@github.com:org/repo.git":           "https://github.com/org/repo",
		"https://user:secret@host/org/repo.git": "https://host/org/repo",
		"https://host/org/repo":                 "https://host/org/repo",
		"ssh://git@host:2222/org/repo.git":      "ssh://host:2222/org/repo",
	}
	for input, want := range cases {
		if got := NormalizeVCSURL(input); got != want {
			t.Errorf("NormalizeVCSURL(%q) = %q, want %q", input, got, want)
		}
	}
	if got := NormalizeVCSURL("https://user:secret@host/org/repo.git"); contains(got, "secret") {
		t.Fatal("a credential reached the output")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && filepath.Base(haystack) != needle &&
		(len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestGenericPURLCarriesTheCheckout(t *testing.T) {
	// Section 20.4: a version alone does not identify a git checkout, so the
	// repository and commit travel with it.
	got := GenericPURL("tinylog", "1.2.0", "https://host/org/repo", "abc123")
	want := "pkg:generic/tinylog@1.2.0?vcs_url=git%2Bhttps%3A%2F%2Fhost%2Forg%2Frepo%40abc123"
	if got != want {
		t.Errorf("GenericPURL() = %q, want %q", got, want)
	}
	if GenericPURL("", "1.0", "", "") != "" {
		t.Error("a purl was built without a name")
	}
}
