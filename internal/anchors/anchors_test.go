package anchors

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/pathmodel"
)

func corpusModel(t *testing.T, toolchain, project string) *cmakeapi.Model {
	t.Helper()
	buildDir := filepath.Join("..", "..", "testdata", "fixtures", toolchain, project, "build")
	replyDir, err := cmakeapi.DiscoverReplyDir(buildDir)
	if err != nil {
		t.Fatalf("no File API reply for %s/%s: %v", toolchain, project, err)
	}
	model, err := cmakeapi.ParseReplyDir(replyDir)
	if err != nil {
		t.Fatalf("parsing %s/%s: %v", toolchain, project, err)
	}
	return model
}

// TestRealLinkEvidenceIsFullyClassified is the acceptance criterion of roadmap
// phase 1: every path in a real linker dependency file must resolve to a named
// anchor, and the toolchain and system noise -- roughly four fifths of the
// file -- must be separable from the project's own code.
func TestRealLinkEvidenceIsFullyClassified(t *testing.T) {
	model := corpusModel(t, "gcc-ninja", "p02-static")
	result, err := Assemble(Options{
		Flavor: pathmodel.PosixFlavor{},
		Model:  model,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Paths taken verbatim from testdata/fixtures/gcc-ninja/p02-static/build/app.d.
	linkInputs := []string{
		"/usr/lib/gcc/x86_64-linux-gnu/13/../../../x86_64-linux-gnu/Scrt1.o",
		"/usr/lib/gcc/x86_64-linux-gnu/13/crtbeginS.o",
		"CMakeFiles/app.dir/main.c.o",
		"libcrypto.a",
		"/usr/lib/gcc/x86_64-linux-gnu/13/libgcc.a",
		"/lib/x86_64-linux-gnu/libc.so.6",
		"/lib64/ld-linux-x86-64.so.2",
	}

	byScope := map[Scope][]string{}
	for _, input := range linkInputs {
		id, scope := result.ScopeOfPath("/__fixture_build__", input)
		byScope[scope] = append(byScope[scope], id.Canonical())
	}

	if got := len(byScope[ScopeBuild]); got != 2 {
		t.Errorf("build-scope inputs = %v, want the object and the archive", byScope[ScopeBuild])
	}
	if len(byScope[ScopeToolchain]) == 0 {
		t.Errorf("no toolchain-scope inputs; libgcc and the crt objects should be there")
	}
	if len(byScope[ScopeUnknown]) != 0 {
		t.Errorf("unclassified link inputs remain: %v", byScope[ScopeUnknown])
	}

	// The two files the SBOM should actually keep.
	kept := 0
	for scope, ids := range byScope {
		if IncludedByDefault(scope) {
			kept += len(ids)
		}
	}
	if kept != 2 {
		t.Errorf("default policy keeps %d of %d link inputs, want 2", kept, len(linkInputs))
	}
}

func TestProjectAndBuildRootsComeFromTheFileAPI(t *testing.T) {
	result, err := Assemble(Options{
		Flavor: pathmodel.PosixFlavor{},
		Model:  corpusModel(t, "gcc-ninja", "p01-hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Registry.Resolve("/__fixture_src__/main.c").Canonical(); got != "project:main.c" {
		t.Errorf("Resolve() = %q, want project:main.c", got)
	}
	if got := result.Registry.Resolve("/__fixture_build__/hello").Canonical(); got != "build:hello" {
		t.Errorf("Resolve() = %q, want build:hello", got)
	}
}

func TestExplicitRootsOverrideTheFileAPI(t *testing.T) {
	result, err := Assemble(Options{
		Flavor:      pathmodel.PosixFlavor{},
		ProjectRoot: "/elsewhere/src",
		BuildRoot:   "/elsewhere/build",
		Model:       corpusModel(t, "gcc-ninja", "p01-hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Registry.Resolve("/elsewhere/src/main.c").Canonical(); got != "project:main.c" {
		t.Errorf("Resolve() = %q, want the configured root to win", got)
	}
	if got := result.Registry.Resolve("/__fixture_src__/main.c").Canonical(); got != "abs:__fixture_src__/main.c" {
		t.Errorf("Resolve() = %q, want the File API root to be unused", got)
	}
}

func TestToolchainAnchorIsRegisteredFromTheReply(t *testing.T) {
	result, err := Assemble(Options{
		Flavor: pathmodel.PosixFlavor{},
		Model:  corpusModel(t, "gcc-ninja", "p01-hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var toolchainKey string
	for _, anchor := range result.Registry.Anchors() {
		if strings.HasPrefix(anchor.Key, "toolchain:") {
			toolchainKey = anchor.Key
		}
	}
	if toolchainKey == "" {
		t.Fatalf("no toolchain anchor registered; anchors are %v", result.Registry.Anchors())
	}
	if !strings.HasPrefix(toolchainKey, "toolchain:gnu-") {
		t.Errorf("toolchain anchor = %q, want a toolchain:gnu-<version> key", toolchainKey)
	}
	if len(result.ImplicitIncludeDirs) == 0 {
		t.Error("implicit include directories were not carried over from toolchains-v1")
	}
}

func TestConfigAnchorsAreRegisteredAndNamed(t *testing.T) {
	result, err := Assemble(Options{
		Flavor:      pathmodel.PosixFlavor{},
		ProjectRoot: "/src",
		BuildRoot:   "/build",
		ConfigAnchors: []config.Anchor{
			{Key: "shared", Path: "/opt/shared"},
			{Key: "sdk:espidf", Path: "/opt/esp-idf"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"/opt/shared/lib.c":           "extern:shared:lib.c",
		"/opt/esp-idf/components/x.c": "sdk:espidf:components/x.c",
	}
	for input, want := range cases {
		if got := result.Registry.Resolve(input).Canonical(); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", input, got, want)
		}
	}
	if got := result.Scope(result.Registry.Resolve("/opt/shared/lib.c")); got != ScopeThirdParty {
		t.Errorf("Scope(extern) = %q, want third-party", got)
	}
	if got := result.Scope(result.Registry.Resolve("/opt/esp-idf/components/x.c")); got != ScopeSDK {
		t.Errorf("Scope(sdk) = %q, want sdk", got)
	}
}

func TestNormalizeConfigKey(t *testing.T) {
	cases := map[string]string{
		"shared":            "extern:shared",
		"extern:shared":     "extern:shared",
		"sdk:espidf":        "sdk:espidf",
		"pkg:conan/mbedtls": "pkg:conan/mbedtls",
		"project":           "project",
		"build":             "build",
	}
	for input, want := range cases {
		if got := NormalizeConfigKey(input); got != want {
			t.Errorf("NormalizeConfigKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSysrootFromFlags(t *testing.T) {
	cases := []struct {
		flags []string
		want  string
	}{
		{[]string{"-O2", "--sysroot=/opt/arm/sysroot", "-c"}, "/opt/arm/sysroot"},
		{[]string{"--sysroot", "/opt/arm/sysroot"}, "/opt/arm/sysroot"},
		{[]string{"-isysroot/opt/sdk"}, "/opt/sdk"},
		{[]string{"-O2", "-c"}, ""},
		{[]string{"--sysroot"}, ""},
	}
	for _, c := range cases {
		if got := SysrootFromFlags(c.flags); got != c.want {
			t.Errorf("SysrootFromFlags(%v) = %q, want %q", c.flags, got, c.want)
		}
	}
}

func TestSysrootAnchorIsRegisteredFromCompileFlags(t *testing.T) {
	result, err := Assemble(Options{
		Flavor:       pathmodel.PosixFlavor{},
		ProjectRoot:  "/src",
		BuildRoot:    "/build",
		CompileFlags: []string{"--sysroot=/opt/arm-none-eabi/sysroot"},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := result.Registry.Resolve("/opt/arm-none-eabi/sysroot/include/stdio.h")
	if got := id.Canonical(); got != "sysroot:sysroot:include/stdio.h" {
		t.Errorf("Resolve() = %q, want a sysroot anchor", got)
	}
	if got := result.Scope(id); got != ScopeSystem {
		t.Errorf("Scope() = %q, want system", got)
	}
	if IncludedByDefault(ScopeSystem) {
		t.Error("system scope must be excluded by default (section 24.1)")
	}
}

func TestMissingToolchainInfoIsReported(t *testing.T) {
	result, err := Assemble(Options{
		Flavor:      pathmodel.PosixFlavor{},
		ProjectRoot: "/src",
		BuildRoot:   "/build",
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, finding := range result.Findings {
		if finding.ID == "TOOLCHAIN_LAYOUT_UNKNOWN" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected TOOLCHAIN_LAYOUT_UNKNOWN, got %v", result.Findings)
	}
}

func TestConventionalSystemRootsAreTheFallback(t *testing.T) {
	result, err := Assemble(Options{
		Flavor:      pathmodel.PosixFlavor{},
		ProjectRoot: "/src",
		BuildRoot:   "/build",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Without toolchains-v1 the classification falls back to path heuristics.
	if _, scope := result.ScopeOfPath("", "/usr/include/stdio.h"); scope != ScopeSystem {
		t.Errorf("ScopeOfPath(/usr/include/stdio.h) = %q, want system", scope)
	}
	if _, scope := result.ScopeOfPath("", "/opt/vendor/blob.a"); scope != ScopeUnknown {
		t.Errorf("ScopeOfPath(/opt/vendor/blob.a) = %q, want unknown", scope)
	}
}

func TestUnanchoredFilesProduceAFinding(t *testing.T) {
	result, err := Assemble(Options{
		Flavor:      pathmodel.PosixFlavor{},
		ProjectRoot: "/src",
		BuildRoot:   "/build",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := result.Registry.Resolve("/opt/vendor/blob.a")
	finding := UnanchoredFinding(id)
	if finding.ID != "UNANCHORED_FILE" || finding.Severity != "warning" {
		t.Errorf("finding = %+v, want a warning-level UNANCHORED_FILE", finding)
	}
	if finding.Subject.Ref != "abs:opt/vendor/blob.a" {
		t.Errorf("finding subject = %q, want the canonical abs path", finding.Subject.Ref)
	}
}

func TestEveryFixtureAssemblesAnchors(t *testing.T) {
	for _, toolchain := range []string{"gcc-ninja", "gcc-make", "clang-ninja", "arm-none-eabi", "mingw-w64"} {
		model := corpusModel(t, toolchain, "p02-static")
		result, err := Assemble(Options{Flavor: pathmodel.PosixFlavor{}, Model: model})
		if err != nil {
			t.Fatalf("%s: %v", toolchain, err)
		}
		var hasToolchain bool
		for _, anchor := range result.Registry.Anchors() {
			if strings.HasPrefix(anchor.Key, "toolchain:") {
				hasToolchain = true
			}
		}
		if !hasToolchain {
			t.Errorf("%s: no toolchain anchor was registered", toolchain)
		}
		if got := result.Registry.Resolve("/__fixture_src__/crypto.c").Canonical(); got != "project:crypto.c" {
			t.Errorf("%s: Resolve() = %q, want project:crypto.c", toolchain, got)
		}
	}
}
