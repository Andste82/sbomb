package binfmt

import (
	"github.com/example/sbomb/internal/pathmodel"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

func TestInspectCurrentTestBinary(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result, err := Inspect(path, Options{IncludeRuntimeLibraries: true})
	if err != nil {
		t.Fatal(err)
	}
	// The test binary is whatever the host builds: PE on Windows, ELF on
	// Linux. A host whose executables are neither has nothing to inspect here.
	want := FormatELF
	switch runtime.GOOS {
	case "windows":
		want = FormatPE
	case "linux", "freebsd", "netbsd", "openbsd":
	default:
		t.Skipf("%s executables are neither ELF nor PE", runtime.GOOS)
	}
	if result.Format != want {
		t.Fatalf("format = %q, want %q", result.Format, want)
	}
	if len(result.CompilationUnits) == 0 {
		t.Skip("test binary was built without DWARF")
	}
	for _, unit := range result.CompilationUnits {
		if unit.Source == "" {
			t.Error("compilation unit has no source")
		}
	}
}

func TestInspectDynamicELF(t *testing.T) {
	path := "/bin/ls"
	if _, err := os.Stat(path); err != nil {
		t.Skip("host has no /bin/ls")
	}
	result, err := Inspect(path, Options{IncludeRuntimeLibraries: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != FormatELF {
		t.Fatalf("format = %q, want ELF", result.Format)
	}
	if len(result.RuntimeLibraries) == 0 {
		t.Skip("/bin/ls is static on this host")
	}
}

func TestInspectMalformedELFReturnsFinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.elf")
	if err := os.WriteFile(path, []byte{0x7f, 'E', 'L', 'F', 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Inspect(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].ID != "MALFORMED_BINARY" {
		t.Fatalf("findings = %+v", result.Findings)
	}
}

func TestInspectUnsupportedInputReturnsFinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.bin")
	if err := os.WriteFile(path, []byte("not a binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Inspect(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].ID != "MALFORMED_BINARY" {
		t.Fatalf("findings = %+v", result.Findings)
	}
}

// The DWARF line-table file table is the header source of section 11.4. A
// header that contributes only declarations produces no line entry, so reading
// line entries reports it as absent from a unit that plainly included it.
func TestLineTableFileTableYieldsDeclarationOnlyHeaders(t *testing.T) {
	artifact := filepath.Join(testutil.RepoRoot(t), "testdata", "fixtures", "gcc-ninja", "p02-static", "build", "app")
	result, err := Inspect(artifact, Options{})
	if err != nil {
		t.Fatalf("Inspect() = %v", err)
	}
	var main *CompilationUnit
	for index := range result.CompilationUnits {
		if strings.HasSuffix(result.CompilationUnits[index].Source, "main.c") {
			main = &result.CompilationUnits[index]
		}
	}
	if main == nil {
		t.Fatalf("no compilation unit for main.c in %d unit(s)", len(result.CompilationUnits))
	}
	if !main.LineTable {
		t.Fatal("the compilation unit carried no line program")
	}
	var found bool
	for _, header := range main.Headers {
		if strings.HasSuffix(header.Path, "crypto.h") {
			found = true
		}
		if header.Path == main.Source {
			t.Errorf("the unit's own source %q was reported as a header", header.Path)
		}
	}
	if !found {
		t.Errorf("crypto.h is not among the headers of main.c: %v", main.Headers)
	}
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestADebugInfoPathIsReadTheSameOnEveryHost: the names in the debug
// information are paths of the machine the binary was built for. They used to
// be joined and cleaned with the host's path rules, so on a Windows host a
// POSIX header came out with backslashes and no longer met the identity the
// other evidence gave it -- which adapter attached the header then depended on
// the platform sbomb ran on, and a reproducible SPDX document, which names the
// contributing adapters, differed between Linux and Windows. Every case here
// must give the same answer on every host.
func TestADebugInfoPathIsReadTheSameOnEveryHost(t *testing.T) {
	for _, tc := range []struct {
		compDir, name, want string
	}{
		{"/__fixture_build__", "/__fixture_src__/crypto.h", "/__fixture_src__/crypto.h"},
		{"/__fixture_build__", "../__fixture_src__/./inc/../crypto.h", "/__fixture_src__/crypto.h"},
		{"/__fixture_build__", "main.c", "/__fixture_build__/main.c"},
		{"", "src/main.c", "src/main.c"},
		{`C:\__fixture_build__`, `..\src\main.c`, "C:/src/main.c"},
		{"C:/__fixture_build__", `C:\src\inc\..\crypto.h`, "C:/src/crypto.h"},
		{`\\server\share\build`, "main.c", "//server/share/build/main.c"},
		{"/b", "", ""},
	} {
		// A path that is absolute in the host's own spelling keeps that
		// spelling -- C:\src\main.c on Windows -- so the file is compared,
		// not its separators.
		if got := pathmodel.NormalizeSeparators(resolvePath(tc.compDir, tc.name)); got != tc.want {
			t.Errorf("resolvePath(%q, %q) = %q, want %q", tc.compDir, tc.name, got, tc.want)
		}
	}
}
