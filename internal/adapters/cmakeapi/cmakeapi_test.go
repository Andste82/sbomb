package cmakeapi

import (
	"os"
	"path/filepath"
	"testing"
)

// corpusReply returns the reply directory of a committed fixture. The fixtures
// are real CMake output, so these tests fail if the adapter only understands
// an idealized reply layout.
func corpusReply(t *testing.T, toolchain, project string) string {
	t.Helper()
	buildDir := filepath.Join("..", "..", "..", "testdata", "fixtures", toolchain, project, "build")
	replyDir, err := DiscoverReplyDir(buildDir)
	if err != nil {
		t.Fatalf("no File API reply in %s: %v", buildDir, err)
	}
	return replyDir
}

func TestParseRealReplyDirectory(t *testing.T) {
	model, err := ParseReplyDir(corpusReply(t, "gcc-ninja", "p02-static"))
	if err != nil {
		t.Fatalf("ParseReplyDir() error = %v", err)
	}

	if model.SourceRoot != "/__fixture_src__" {
		t.Errorf("SourceRoot = %q, want /__fixture_src__", model.SourceRoot)
	}
	if model.BuildRoot != "/__fixture_build__" {
		t.Errorf("BuildRoot = %q, want /__fixture_build__", model.BuildRoot)
	}
	if model.Cache["CMAKE_BUILD_TYPE"] != "Debug" {
		t.Errorf("CMAKE_BUILD_TYPE = %q, want Debug", model.Cache["CMAKE_BUILD_TYPE"])
	}

	config, err := model.Configuration("")
	if err != nil {
		t.Fatalf("Configuration() error = %v", err)
	}
	if config.Name != "Debug" {
		t.Errorf("configuration = %q, want Debug", config.Name)
	}
	if len(config.Targets) != 2 {
		t.Fatalf("got %d targets, want 2 (app and crypto)", len(config.Targets))
	}

	byName := map[string]Target{}
	for _, target := range config.Targets {
		byName[target.Name] = target
	}

	app, ok := byName["app"]
	if !ok {
		t.Fatal("target app is missing")
	}
	if app.Type != "EXECUTABLE" {
		t.Errorf("app type = %q, want EXECUTABLE", app.Type)
	}
	// Target detail lives in its own reply file; an adapter that only reads
	// the codemodel sees an empty target here.
	if len(app.Artifacts) != 1 || app.Artifacts[0].Path != "app" {
		t.Errorf("app artifacts = %v, want [app]", app.Artifacts)
	}
	if len(app.Sources) != 1 || app.Sources[0].Path != "main.c" {
		t.Errorf("app sources = %v, want [main.c]", app.Sources)
	}
	if len(app.Install) == 0 {
		t.Error("app should carry an install rule; the fixture installs it")
	}
	if len(app.Dependencies) == 0 {
		t.Error("app should depend on crypto")
	}

	crypto, ok := byName["crypto"]
	if !ok {
		t.Fatal("target crypto is missing")
	}
	if crypto.Type != "STATIC_LIBRARY" {
		t.Errorf("crypto type = %q, want STATIC_LIBRARY", crypto.Type)
	}
	if len(crypto.Sources) != 2 {
		t.Errorf("crypto sources = %v, want crypto.c and unused.c", crypto.Sources)
	}
}

func TestLinkFragmentsAreCaptured(t *testing.T) {
	model, err := ParseReplyDir(corpusReply(t, "gcc-ninja", "p02-static"))
	if err != nil {
		t.Fatal(err)
	}
	config, _ := model.Configuration("")
	var fragments []string
	for _, target := range config.Targets {
		if target.Name == "app" {
			fragments = target.LinkFragments
		}
	}
	// The map and depfile flags are how link evidence gets produced at all.
	var sawMap bool
	for _, fragment := range fragments {
		if len(fragment) > 8 && fragment[:8] == "-Wl,-Map" {
			sawMap = true
		}
	}
	if !sawMap {
		t.Errorf("link fragments do not mention the map flag: %v", fragments)
	}
}

func TestToolchainsCarryImplicitIncludeDirectories(t *testing.T) {
	model, err := ParseReplyDir(corpusReply(t, "gcc-ninja", "p01-hello"))
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Toolchains) == 0 {
		t.Fatal("no toolchain in the reply")
	}
	toolchain := model.Toolchains[0]
	if toolchain.Language != "C" {
		t.Errorf("language = %q, want C", toolchain.Language)
	}
	if toolchain.CompilerID != "GNU" {
		t.Errorf("compiler id = %q, want GNU", toolchain.CompilerID)
	}
	if toolchain.CompilerPath == "" {
		t.Error("compiler path is empty")
	}
	// Section 14.4 requires classification to use these rather than a
	// hardcoded list of system include paths.
	if len(toolchain.ImplicitIncludeDirs) == 0 {
		t.Error("implicit include directories are empty")
	}
	if len(toolchain.ImplicitLinkDirs) == 0 {
		t.Error("implicit link directories are empty")
	}
}

func TestToolchainRootDropsBinDirectory(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/cc":                         "/usr",
		"/opt/gcc-arm-none-eabi-13.2/bin/gcc": "/opt/gcc-arm-none-eabi-13.2",
		"/usr/lib/llvm-18/clang":              "/usr/lib/llvm-18",
		"":                                    "",
	}
	for input, want := range cases {
		if got := ToolchainRoot(input); got != want {
			t.Errorf("ToolchainRoot(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestToolchainIDCombinesCompilerAndVersion(t *testing.T) {
	got := ToolchainID(Toolchain{CompilerID: "GNU", CompilerVersion: "13.3.0"})
	if got != "gnu-13.3.0" {
		t.Errorf("ToolchainID() = %q, want gnu-13.3.0", got)
	}
	if got := ToolchainID(Toolchain{}); got != "unknown" {
		t.Errorf("ToolchainID() on an empty toolchain = %q, want unknown", got)
	}
}

func TestArtifactCandidatesPreferInstalledTargets(t *testing.T) {
	model, err := ParseReplyDir(corpusReply(t, "gcc-ninja", "p02-static"))
	if err != nil {
		t.Fatal(err)
	}
	candidates := model.ArtifactCandidates()
	if len(candidates) != 1 || candidates[0] != "app" {
		t.Errorf("ArtifactCandidates() = %v, want [app]", candidates)
	}
}

func TestEveryFixtureReplyParses(t *testing.T) {
	corpus := filepath.Join("..", "..", "..", "testdata", "fixtures")
	toolchains, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	var parsed int
	for _, toolchain := range toolchains {
		if !toolchain.IsDir() {
			continue
		}
		projects, err := os.ReadDir(filepath.Join(corpus, toolchain.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, project := range projects {
			if !project.IsDir() {
				continue
			}
			buildDir := filepath.Join(corpus, toolchain.Name(), project.Name(), "build")
			replyDir, err := DiscoverReplyDir(buildDir)
			if err != nil {
				continue
			}
			model, err := ParseReplyDir(replyDir)
			if err != nil {
				t.Errorf("%s/%s: %v", toolchain.Name(), project.Name(), err)
				continue
			}
			if model.SourceRoot == "" || model.BuildRoot == "" {
				t.Errorf("%s/%s: source or build root missing", toolchain.Name(), project.Name())
			}
			if len(model.Configurations) == 0 {
				t.Errorf("%s/%s: no configuration", toolchain.Name(), project.Name())
			}
			parsed++
		}
	}
	// Five toolchains times twelve projects, less the four p11-conan builds
	// the other toolchains cannot produce, plus the five p13-prebuilt ones.
	if parsed != 63 {
		t.Errorf("parsed %d fixture replies, want 63", parsed)
	}
}

func TestMissingIndexIsReported(t *testing.T) {
	empty := t.TempDir()
	if _, err := ParseReplyDir(empty); err == nil {
		t.Fatal("a reply directory without an index should be an error")
	}
}

func TestDiscoverReplyDir(t *testing.T) {
	buildDir := t.TempDir()
	replyDir := filepath.Join(buildDir, ".cmake", "api", "v1", "reply")
	if err := os.MkdirAll(replyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverReplyDir(buildDir)
	if err != nil {
		t.Fatalf("DiscoverReplyDir() error = %v", err)
	}
	if got != replyDir {
		t.Fatalf("DiscoverReplyDir() = %q, want %q", got, replyDir)
	}
}

func TestReplyFilenamesCannotEscapeTheReplyDirectory(t *testing.T) {
	rejected := []string{"", ".", "..", "../secret.json", "sub/dir.json", `..\secret.json`, "/etc/passwd"}
	for _, name := range rejected {
		if safeReplyName(name) {
			t.Errorf("safeReplyName(%q) = true, want false", name)
		}
	}
	for _, name := range []string{"codemodel-v2-abc.json", "index-2026.json"} {
		if !safeReplyName(name) {
			t.Errorf("safeReplyName(%q) = false, want true", name)
		}
	}
}
