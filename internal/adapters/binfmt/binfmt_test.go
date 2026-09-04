package binfmt

import (
	"os"
	"path/filepath"
	"testing"
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
	if result.Format != FormatELF {
		t.Fatalf("format = %q, want ELF", result.Format)
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
