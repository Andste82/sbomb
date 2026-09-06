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

// writeConan lays out what the CMakeDeps generator writes, which is what the
// adapter reads: a version in the config-version file and the package root in
// the per-configuration data file.
func writeConan(t *testing.T, buildDir, name, version, packageRoot string) {
	t.Helper()
	version_file := "set(PACKAGE_VERSION \"" + version + "\")\n" +
		"if(PACKAGE_VERSION VERSION_LESS PACKAGE_FIND_VERSION)\n  set(PACKAGE_VERSION_COMPATIBLE FALSE)\nendif()\n"
	if err := os.WriteFile(filepath.Join(buildDir, name+"-config-version.cmake"), []byte(version_file), 0o600); err != nil {
		t.Fatal(err)
	}
	data := "set(" + name + "_COMPONENT_NAMES \"\")\n" +
		"set(" + name + "_PACKAGE_FOLDER_RELEASE \"" + packageRoot + "\")\n" +
		"set(" + name + "_INCLUDE_DIRS_RELEASE \"${" + name + "_PACKAGE_FOLDER_RELEASE}/include\")\n"
	if err := os.WriteFile(filepath.Join(buildDir, name+"-release-x86_64-data.cmake"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConanReadsVersionRootAndLicence(t *testing.T) {
	build := t.TempDir()
	packageRoot := filepath.Join(t.TempDir(), "p")
	if err := os.MkdirAll(filepath.Join(packageRoot, "licenses"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "licenses", "LICENSE"), []byte("MIT"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeConan(t, build, "tinycbor", "0.6.1", packageRoot)

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v (findings %#v)", packages, findings)
	}
	found := packages[0]
	if found.Name != "tinycbor" || found.Version != "0.6.1" {
		t.Errorf("name/version = %q/%q", found.Name, found.Version)
	}
	if found.Root != packageRoot {
		t.Errorf("root = %q, want the package folder the data file names", found.Root)
	}
	if found.PURL != "pkg:conan/tinycbor@0.6.1" {
		t.Errorf("purl = %q", found.PURL)
	}
	if found.LicenseFile == "" {
		t.Error("the licence Conan copied into the package was not found")
	}
}

// Without a package folder the entry would name a component that owns nothing,
// so it is reported rather than emitted.
func TestConanWithoutAPackageFolderIsReported(t *testing.T) {
	build := t.TempDir()
	if err := os.WriteFile(filepath.Join(build, "mystery-config-version.cmake"),
		[]byte("set(PACKAGE_VERSION \"1.0\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 0 {
		t.Errorf("packages = %#v, want none", packages)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "MISSING_PACKAGE_EVIDENCE" {
			reported = true
		}
	}
	if !reported {
		t.Error("a dependency with no package folder was dropped silently")
	}
}

func writeVcpkg(t *testing.T, buildDir, triplet, name, version, license, purl string) {
	t.Helper()
	dir := filepath.Join(buildDir, "vcpkg_installed", triplet, "share", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	document := `{"packages":[{"name":"` + name + `","versionInfo":"` + version +
		`","licenseConcluded":"` + license + `","externalRefs":[{"referenceCategory":"PACKAGE-MANAGER",` +
		`"referenceType":"purl","referenceLocator":"` + purl + `"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "vcpkg.spdx.json"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "copyright"), []byte("MIT"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// vcpkg states the purl itself, so nothing has to be reconstructed.
func TestVcpkgTakesThePurlItStates(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT",
		"pkg:vcpkg/tinyfmt@2.1.0?triplet=x64-linux")

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.PURL != "pkg:vcpkg/tinyfmt@2.1.0?triplet=x64-linux" {
		t.Errorf("purl = %q, want the one vcpkg wrote", found.PURL)
	}
	if found.Version != "2.1.0" || found.License != "MIT" {
		t.Errorf("version/license = %q/%q", found.Version, found.License)
	}
	if found.LicenseFile == "" {
		t.Error("the copyright file vcpkg writes was not found")
	}
}

// NOASSERTION is vcpkg saying it does not know. Recording it as a licence
// would turn an absence of evidence into an assertion.
func TestVcpkgNoAssertionIsNotALicence(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "mystery", "1.0", "NOASSERTION", "pkg:vcpkg/mystery@1.0")

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if packages[0].License != "" {
		t.Errorf("license = %q, want none", packages[0].License)
	}
}

// Section 19.2 strategy 3: a submodule is a boundary the project declared, so
// .gitmodules is read rather than guessed at from a directory layout.
func TestSubmoduleBoundariesComeFromGitmodules(t *testing.T) {
	source := t.TempDir()
	content := `[submodule "dep/mbedtls"]
	path = dep/mbedtls
	url = git@github.com:Mbed-TLS/mbedtls.git
[submodule "dep/tinycbor"]
	path = dep/tinycbor
	url = https://user:secret@example.invalid/tinycbor.git
`
	if err := os.WriteFile(filepath.Join(source, ".gitmodules"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	packages, findings := Discover(Options{SourceDir: source, Context: context.Background()})
	if len(packages) != 2 {
		t.Fatalf("packages = %#v", packages)
	}
	byName := map[string]Package{}
	for _, entry := range packages {
		byName[entry.Name] = entry
	}
	if got := byName["mbedtls"].VCSURL; got != "https://github.com/Mbed-TLS/mbedtls" {
		t.Errorf("scp-like url normalized to %q", got)
	}
	if got := byName["tinycbor"].VCSURL; indexOf(got, "secret") >= 0 {
		t.Errorf("a credential survived normalization: %q", got)
	}
	if byName["mbedtls"].Root != filepath.Join(source, "dep", "mbedtls") {
		t.Errorf("root = %q", byName["mbedtls"].Root)
	}
	// .gitmodules records neither a tag nor a commit, and section 20.1 forbids
	// guessing one, so this has to be reported rather than filled in.
	var unknown int
	for _, finding := range findings {
		if finding.ID == "UNKNOWN_VERSION" {
			unknown++
		}
	}
	if unknown != 2 {
		t.Errorf("UNKNOWN_VERSION reported %d time(s), want one per submodule", unknown)
	}
}

// Section 19.4: git metadata may describe a component but must never expand
// the used-file set. Adapters only annotate; the reachability filter decides.
func TestNoGitmodulesMeansNoPackages(t *testing.T) {
	packages, findings := Discover(Options{SourceDir: t.TempDir(), Context: context.Background()})
	if len(packages) != 0 || len(findings) != 0 {
		t.Errorf("packages = %#v, findings = %#v", packages, findings)
	}
}
