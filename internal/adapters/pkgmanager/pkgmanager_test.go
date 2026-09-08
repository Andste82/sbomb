package pkgmanager

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	if found.Name != "tinylog" || found.Version.Value != "1.2.0" {
		t.Errorf("name/version = %q/%q", found.Name, found.Version.Value)
	}
	if found.Version.Source != "fetchcontent" || found.Version.Confidence != "high" {
		t.Errorf("version source = %q, confidence = %q", found.Version.Source, found.Version.Confidence)
	}
	if found.VCSURL != "https://example.invalid/org/tinylog" {
		t.Errorf("vcs url = %q, want the .git suffix removed", found.VCSURL)
	}
	if found.Supplier.Value != "" {
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
	if !filepath.IsAbs(packages[0].Root()) && packages[0].Root() == "" {
		t.Error("the package has no root to map files against")
	}
}

func TestAPopulatedDependencyWithNoTagIsReported(t *testing.T) {
	build := t.TempDir()
	if err := os.MkdirAll(filepath.Join(build, "_deps", "mystery-src"), 0o755); err != nil {
		t.Fatal(err)
	}
	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || packages[0].Version.Value != "" {
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
	if len(packages) != 1 || packages[0].Version.Value != "1.2.0" {
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
	if found.Name != "tinycbor" || found.Version.Value != "0.6.1" {
		t.Errorf("name/version = %q/%q", found.Name, found.Version.Value)
	}
	if found.Root() != packageRoot {
		t.Errorf("root = %q, want the package folder the data file names", found.Root())
	}
	if found.PURL.Value != "pkg:conan/tinycbor@0.6.1" {
		t.Errorf("purl = %q", found.PURL.Value)
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
	if found.PURL.Value != "pkg:vcpkg/tinyfmt@2.1.0?triplet=x64-linux" {
		t.Errorf("purl = %q, want the one vcpkg wrote", found.PURL.Value)
	}
	if found.Version.Value != "2.1.0" || found.License.Value != "MIT" {
		t.Errorf("version/license = %q/%q", found.Version.Value, found.License.Value)
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
	if packages[0].License.Value != "" {
		t.Errorf("license = %q, want none", packages[0].License.Value)
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
	if byName["mbedtls"].Root() != filepath.Join(source, "dep", "mbedtls") {
		t.Errorf("root = %q", byName["mbedtls"].Root())
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

func TestRecursiveSubmoduleDiscovery(t *testing.T) {
	source := t.TempDir()
	topGitmodules := `[submodule "dep/esp-idf"]
	path = dep/esp-idf
	url = https://github.com/espressif/esp-idf.git
`
	if err := os.WriteFile(filepath.Join(source, ".gitmodules"), []byte(topGitmodules), 0o600); err != nil {
		t.Fatal(err)
	}

	nestedDir := filepath.Join(source, "dep", "esp-idf")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedGitmodules := `[submodule "components/mbedtls/mbedtls"]
	path = components/mbedtls/mbedtls
	url = https://github.com/espressif/mbedtls.git
`
	if err := os.WriteFile(filepath.Join(nestedDir, ".gitmodules"), []byte(nestedGitmodules), 0o600); err != nil {
		t.Fatal(err)
	}

	packages, _ := Discover(Options{SourceDir: source, Context: context.Background()})
	byName := map[string]Package{}
	for _, entry := range packages {
		byName[entry.Name] = entry
	}
	if len(packages) != 2 {
		t.Fatalf("expected 2 packages, got %d: %#v", len(packages), packages)
	}
	if _, ok := byName["esp-idf"]; !ok {
		t.Errorf("expected esp-idf package")
	}
	if got, ok := byName["mbedtls"]; !ok {
		t.Errorf("expected mbedtls package")
	} else if got.Root() != filepath.Join(nestedDir, "components", "mbedtls", "mbedtls") {
		t.Errorf("mbedtls Root = %q, want %q", got.Root(), filepath.Join(nestedDir, "components", "mbedtls", "mbedtls"))
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

// A FetchContent dependency is two directories, not one: the checkout and the
// tree CMake filled for it. A header written by configure_file exists only in
// the second, and while the checkout was the only root such a header belonged
// to no package at all.
func TestFetchContentClaimsTheBuildTreeBesideTheCheckout(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.com/tinylog.git", "v1.4.0")
	buildTree := filepath.Join(build, "_deps", "tinylog-build")
	if err := os.MkdirAll(buildTree, 0o755); err != nil {
		t.Fatal(err)
	}

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Root() != filepath.Join(build, "_deps", "tinylog-src") {
		t.Errorf("identity root = %q, want the checkout", found.Root())
	}
	if len(found.Roots) != 2 || found.Roots[1] != buildTree {
		t.Errorf("roots = %q, want the checkout and the build tree", found.Roots)
	}
}

// A directory nobody built is not evidence, so a package without a build tree
// claims none.
func TestFetchContentWithoutABuildTreeHasOneRoot(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.com/tinylog.git", "v1.4.0")

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if len(packages[0].Roots) != 1 {
		t.Errorf("roots = %q, want only the checkout", packages[0].Roots)
	}
}

// writeVcpkgFileList writes the list vcpkg keeps of everything a package
// installed. Its entries are relative to the install directory, so they begin
// with the triplet, and a directory is listed with a trailing slash.
func writeVcpkgFileList(t *testing.T, buildDir, triplet, name, version string, entries []string) string {
	t.Helper()
	dir := filepath.Join(buildDir, "vcpkg_installed", "vcpkg", "info")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+"_"+version+"_"+triplet+".list")
	var contents strings.Builder
	for _, entry := range entries {
		contents.WriteString(entry)
		contents.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(contents.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Inside a triplet tree every package shares include/ and lib/, so the layout
// cannot say which header belongs to which port. The list vcpkg wrote when it
// installed the package can, and it is read back as absolute paths.
func TestVcpkgReadsTheFilesItInstalled(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")
	writeVcpkgFileList(t, build, "x64-linux", "tinyfmt", "2.1.0", []string{
		"x64-linux/",
		"x64-linux/include/",
		"x64-linux/include/tinyfmt.h",
		"x64-linux/lib/libtinyfmt.a",
	})

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	installed := filepath.Join(build, "vcpkg_installed", "x64-linux")
	want := []string{
		filepath.Join(installed, "include", "tinyfmt.h"),
		filepath.Join(installed, "lib", "libtinyfmt.a"),
	}
	if len(packages[0].Files) != len(want) {
		t.Fatalf("files = %q, want %q", packages[0].Files, want)
	}
	for i, path := range want {
		if packages[0].Files[i] != path {
			t.Errorf("file %d = %q, want %q", i, packages[0].Files[i], path)
		}
	}
	for _, finding := range findings {
		if finding.ID == "INPUT_LIMIT_EXCEEDED" {
			t.Errorf("a readable list reported %s", finding.ID)
		}
	}
}

// A list entry that is absolute or walks upwards names a file outside the
// install tree, which no install list may do (section 30.3).
func TestVcpkgRefusesAListEntryThatLeavesTheInstallTree(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")
	writeVcpkgFileList(t, build, "x64-linux", "tinyfmt", "2.1.0", []string{
		"x64-linux/../../../etc/passwd",
		"/etc/shadow",
		"x64-linux/include/tinyfmt.h",
	})

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	want := filepath.Join(build, "vcpkg_installed", "x64-linux", "include", "tinyfmt.h")
	if len(packages[0].Files) != 1 || packages[0].Files[0] != want {
		t.Errorf("files = %q, want only %q", packages[0].Files, want)
	}
}

// A list that breaches a bound of section 30 is refused whole and reported.
// The package survives it: the SPDX document proved it, and only the precise
// attribution of its files is lost.
func TestVcpkgReportsAFileListOverTheLimit(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")
	entries := make([]string, 0, maxFileListEntries+1)
	for i := 0; i <= maxFileListEntries; i++ {
		entries = append(entries, "x64-linux/include/header"+strconv.Itoa(i)+".h")
	}
	path := writeVcpkgFileList(t, build, "x64-linux", "tinyfmt", "2.1.0", entries)

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if len(packages[0].Files) != 0 {
		t.Errorf("files = %d, want none: a partial list attributes some files and hides the rest", len(packages[0].Files))
	}
	if packages[0].Root() != filepath.Join(build, "vcpkg_installed", "x64-linux", "share", "tinyfmt") {
		t.Errorf("root = %q, want the share directory the package keeps", packages[0].Root())
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "INPUT_LIMIT_EXCEEDED" && finding.Subject.Ref == path {
			reported = true
		}
	}
	if !reported {
		t.Error("a list over the limit was skipped silently")
	}
}

// The list is an improvement, not an expectation. A tree without one behaves
// exactly as it did before and says nothing about it.
func TestVcpkgWithoutAFileListReportsNothing(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 || len(packages[0].Files) != 0 {
		t.Fatalf("packages = %#v", packages)
	}
	for _, finding := range findings {
		if finding.ID == "INPUT_LIMIT_EXCEEDED" {
			t.Errorf("a tree with no file list reported %s", finding.ID)
		}
	}
}

// Two lists for one port mean two versions of it are installed beside each
// other. Which of them the build used is not written down anywhere, so neither
// list is read: picking one would be a guess about which files are whose.
func TestTwoFileListsForOnePortLeaveBothUnread(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")
	writeVcpkgFileList(t, build, "x64-linux", "tinyfmt", "2.1.0", []string{"x64-linux/include/tinyfmt.h"})
	writeVcpkgFileList(t, build, "x64-linux", "tinyfmt", "2.0.0", []string{"x64-linux/include/tinyfmt.h"})

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if len(packages[0].Files) != 0 {
		t.Errorf("files = %q, want none: two lists are two answers", packages[0].Files)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %#v, want none: the package is known either way", findings)
	}
}

// A triplet tree is only described by the list belonging to it. A list written
// for another triplet names files that are not in this tree at all, so it is
// not the record of what is installed here.
func TestAFileListOfAnotherTripletIsNotRead(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")
	writeVcpkgFileList(t, build, "arm64-osx", "tinyfmt", "2.1.0", []string{"arm64-osx/include/tinyfmt.h"})

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if len(packages[0].Files) != 0 {
		t.Errorf("files = %q, want none: the list describes a different triplet", packages[0].Files)
	}
}

// Section 30 bounds a parser by what it reads, not only by what it produces.
// A list past the byte limit is refused before it is opened, and the package
// keeps the root and the metadata its SPDX document proved.
func TestVcpkgRefusesAFileListOverTheByteLimit(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")
	// Enough entries to pass the byte limit while staying under the entry
	// limit, so that it is the size of the file that is being refused and not
	// the number of names in it, which has a test of its own.
	entries := make([]string, 0, maxFileListEntries)
	padding := strings.Repeat("d", 80)
	for i := 0; len(entries)*(len(padding)+26) <= maxFileListBytes; i++ {
		entries = append(entries, "x64-linux/include/"+padding+"/header"+strconv.Itoa(i)+".h")
	}
	if len(entries) >= maxFileListEntries {
		t.Fatalf("the list needs %d entries to pass the byte limit, which is past the entry limit", len(entries))
	}
	path := writeVcpkgFileList(t, build, "x64-linux", "tinyfmt", "2.1.0", entries)

	packages, findings := Discover(Options{BuildDir: build, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	if got := len(packages[0].Files); got != 0 {
		t.Errorf("files = %d, want none: a list past the limit is not read at all", got)
	}
	if packages[0].Version.Value != "2.1.0" {
		t.Errorf("version = %q, want the one the SPDX document states", packages[0].Version.Value)
	}
	var reported bool
	for _, finding := range findings {
		if finding.ID == "INPUT_LIMIT_EXCEEDED" && finding.Subject.Ref == path {
			reported = true
		}
	}
	if !reported {
		t.Error("a list over the byte limit was skipped silently")
	}
}

// Git introspection may be allowed and still answer nothing -- here the
// checkout lies outside every registered anchor, so the runner refuses the
// command before a process exists. The package has to survive that with the
// evidence the files already gave it, roots included.
func TestAGitQueryThatIsRefusedLeavesThePackageIntact(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.4.0")
	if err := os.MkdirAll(filepath.Join(build, "_deps", "tinylog-build"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Git is on, but no anchor is registered, so every path argument is
	// outside them and no command runs.
	runner := &exec.Runner{Features: exec.Features{Git: true}}

	packages, _ := Discover(Options{BuildDir: build, Runner: runner, Context: context.Background()})
	if len(packages) != 1 {
		t.Fatalf("packages = %#v", packages)
	}
	found := packages[0]
	if found.Version.Value != "1.4.0" || found.Version.Source != "fetchcontent" {
		t.Errorf("version = %q from %q, want the one the populate script states", found.Version.Value, found.Version.Source)
	}
	if len(found.Roots) != 2 {
		t.Errorf("roots = %q, want the checkout and the build tree", found.Roots)
	}
	if found.Commit != "" {
		t.Errorf("commit = %q, want none: no command answered", found.Commit)
	}
	if records := runner.Records(); len(records) != 0 {
		t.Errorf("commands ran = %v, want none: a path outside every anchor is refused before a process exists", records)
	}
}

// Every root of a package is claimed, not only the identity one. Otherwise a
// later adapter would offer the build tree of a FetchContent dependency as a
// package in its own right, and the same files would belong to two components.
func TestASecondRootIsNotOfferedAsAPackageOfItsOwn(t *testing.T) {
	build := t.TempDir()
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.4.0")
	buildTree := filepath.Join(build, "_deps", "tinylog-build")
	if err := os.MkdirAll(buildTree, 0o755); err != nil {
		t.Fatal(err)
	}
	// An in-source build, so that .gitmodules is read from the same directory
	// the build tree lies in, and declares the very tree FetchContent filled.
	gitmodules := "[submodule \"tinylog-build\"]\n\tpath = _deps/tinylog-build\n" +
		"\turl = https://example.invalid/org/other.git\n"
	if err := os.WriteFile(filepath.Join(build, ".gitmodules"), []byte(gitmodules), 0o600); err != nil {
		t.Fatal(err)
	}

	packages, _ := Discover(Options{BuildDir: build, SourceDir: build, Context: context.Background()})
	for _, found := range packages {
		if found.Root() == buildTree {
			t.Errorf("%s claims %q, which the FetchContent package already covers", found.Manager, found.Root())
		}
	}
	if len(packages) != 1 || packages[0].Name != "tinylog" {
		t.Fatalf("packages = %#v, want the FetchContent dependency alone", packages)
	}
}
