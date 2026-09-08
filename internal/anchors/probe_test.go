package anchors

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/pathmodel"
)

// The lines are what gcc 13.3.0 printed for -print-search-dirs. The parser is
// held against the real output, including the "=" prefix and the ".." segments
// that would otherwise become a second spelling of one directory.
const realSearchDirs = "install: /usr/lib/gcc/x86_64-linux-gnu/13/\n" +
	"programs: =/usr/libexec/gcc/x86_64-linux-gnu/13/:/usr/lib/gcc/x86_64-linux-gnu/\n" +
	"libraries: =/usr/lib/gcc/x86_64-linux-gnu/13/:/usr/lib/gcc/x86_64-linux-gnu/13/../../../../lib/:/usr/lib/\n"

func TestParseSearchDirsReadsTheInstallationAndTheLibraries(t *testing.T) {
	probe := parseSearchDirs(realSearchDirs)
	if probe.Root != "/usr/lib/gcc/x86_64-linux-gnu/13" {
		t.Errorf("root = %q", probe.Root)
	}
	if len(probe.LinkDirs) != 3 {
		t.Fatalf("link dirs = %v, want the three the compiler named", probe.LinkDirs)
	}
	if probe.LinkDirs[1] != "/usr/lib" {
		t.Errorf("the \"..\" segments were not removed: %q", probe.LinkDirs[1])
	}
}

func TestSearchPathEntriesStripsThePrefixAndNormalises(t *testing.T) {
	entries := searchPathEntries("=/usr/lib/gcc/x86_64-linux-gnu/13/:/usr/lib/gcc/x86_64-linux-gnu/13/../../../../lib/")
	want := []string{"/usr/lib/gcc/x86_64-linux-gnu/13", "/usr/lib"}
	if len(entries) != len(want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
	for index := range want {
		if entries[index] != want[index] {
			t.Errorf("entry %d = %q, want %q", index, entries[index], want[index])
		}
	}
}

// Only a bare version number is taken. The rest of the banner is distribution
// prose, and putting it into an anchor key would make the identity of a file
// depend on the machine that read it.
func TestVersionFromBannerTakesOnlyTheNumber(t *testing.T) {
	cases := map[string]string{
		"gcc (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0\nCopyright (C) 2023\n": "13.3.0",
		"clang version 17.0.6":            "17.0.6",
		"some compiler without a version": "",
		"":                                "",
	}
	for banner, want := range cases {
		if got := versionFromBanner(banner); got != want {
			t.Errorf("versionFromBanner(%q) = %q, want %q", banner, got, want)
		}
	}
}

func TestAnchorKeyNamesTheToolchainFromItsOwnAnswers(t *testing.T) {
	probe := toolchainProbe{Root: "/usr/lib/gcc/x86_64-linux-gnu/13", Triple: "x86_64-linux-gnu", Version: "13.3.0"}
	if got := probe.anchorKey(); got != "toolchain:x86_64-linux-gnu-13.3.0" {
		t.Errorf("anchorKey = %q", got)
	}
	// A compiler that answered neither question still gets a key, from the
	// directory it lives in rather than from nothing.
	bare := toolchainProbe{Root: "/opt/gcc-arm-none-eabi-12"}
	if got := bare.anchorKey(); got != "toolchain:gcc-arm-none-eabi-12" {
		t.Errorf("anchorKey without answers = %q", got)
	}
}

// The probe is a fallback. A reply that named a toolchain has already
// answered, so no process may be started.
func TestToolchainsFromTheReplyLeaveTheCompilerUnasked(t *testing.T) {
	runner := &exec.Runner{Features: exec.Features{Compiler: true}}
	result, err := Assemble(Options{
		Flavor:      pathmodel.PosixFlavor{},
		ProjectRoot: "/src",
		BuildRoot:   "/build",
		Model: &cmakeapi.Model{Toolchains: []cmakeapi.Toolchain{{
			Language: "C", CompilerID: "GNU", CompilerVersion: "13.3.0",
			CompilerPath:        "/usr/bin/gcc",
			ImplicitIncludeDirs: []string{"/usr/include"},
		}}},
		Runner:    runner,
		Ctx:       context.Background(),
		Compilers: []string{"/usr/bin/gcc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.Records()) != 0 {
		t.Errorf("the reply answered, but %d command(s) ran anyway", len(runner.Records()))
	}
	if len(result.ImplicitIncludeDirs) != 1 {
		t.Errorf("implicit include dirs = %v", result.ImplicitIncludeDirs)
	}
}

// The whole path, against a program that answers the three questions the way
// a compiler does: the anchor and the link directories reach the result, and
// the include directories do not, because -print-search-dirs never names any.
func TestTheCompilerProbeSuppliesLinkDirsButNoIncludeDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in compiler is a shell script")
	}
	compiler := filepath.Join(t.TempDir(), "fakecc")
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  -print-search-dirs) printf '%s' \"" + realSearchDirs + "\" ;;\n" +
		"  -dumpmachine) echo x86_64-linux-gnu ;;\n" +
		"  --version) echo 'fakecc (Test) 13.3.0' ;;\n" +
		"esac\n"
	if err := os.WriteFile(compiler, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	runner := &exec.Runner{Features: exec.Features{Compiler: true}}
	result, err := Assemble(Options{
		Flavor:      pathmodel.PosixFlavor{},
		ProjectRoot: "/src",
		BuildRoot:   "/build",
		Runner:      runner,
		Ctx:         context.Background(),
		Compilers:   []string{compiler},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.Records()) != 3 {
		t.Fatalf("%d command(s) ran; the three permitted probes are one each", len(runner.Records()))
	}
	var registered bool
	for _, anchor := range result.Registry.Anchors() {
		if anchor.Key == "toolchain:x86_64-linux-gnu-13.3.0" {
			registered = true
		}
	}
	if !registered {
		t.Errorf("no toolchain anchor was registered: %+v", result.Registry.Anchors())
	}
	if len(result.ImplicitLinkDirs) == 0 {
		t.Error("the compiler named its library directories and none arrived")
	}
	if len(result.ImplicitIncludeDirs) != 0 {
		t.Errorf("include dirs = %v; -print-search-dirs names none", result.ImplicitIncludeDirs)
	}
	if len(findingsWithID(result.Findings, "TOOLCHAIN_LAYOUT_UNKNOWN")) != 1 {
		t.Error("the include directories are still unknown and the finding has to say so")
	}
}

func findingsWithID(findings []domain.Finding, id string) []domain.Finding {
	out := make([]domain.Finding, 0, 1)
	for _, finding := range findings {
		if finding.ID == id {
			out = append(out, finding)
		}
	}
	return out
}

// With the compiler group off nothing may run, however little the reply said.
func TestWithoutTheCompilerGroupNothingIsProbed(t *testing.T) {
	runner := &exec.Runner{}
	result, err := Assemble(Options{
		Flavor:      pathmodel.PosixFlavor{},
		ProjectRoot: "/src",
		BuildRoot:   "/build",
		Runner:      runner,
		Ctx:         context.Background(),
		Compilers:   []string{"/usr/bin/gcc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.Records()) != 0 {
		t.Errorf("%d command(s) ran with introspection off", len(runner.Records()))
	}
	if len(result.Findings) == 0 {
		t.Error("nothing was learned about the toolchain and nothing was reported")
	}
}
