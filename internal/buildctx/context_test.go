package buildctx
package buildctx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeDetectsGeneratorAndConfig(t *testing.T) {
	buildDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(buildDir, "CMakeCache.txt"), []byte("CMAKE_GENERATOR:INTERNAL=Ninja\nCMAKE_BUILD_TYPE:STRING=Debug\nCMAKE_SOURCE_DIR:PATH=/tmp/project\nCMAKE_C_COMPILER:FILEPATH=/usr/bin/gcc\nCMAKE_CXX_COMPILER:FILEPATH=/usr/bin/g++\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, err := Probe(buildDir)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if ctx.Generator != "Ninja" {
		t.Fatalf("Generator = %q, want %q", ctx.Generator, "Ninja")
	}
	if got, err := ctx.SelectConfig(""); err != nil || got != "Debug" {
		t.Fatalf("SelectConfig() = (%q, %v), want (%q, nil)", got, err, "Debug")
	}
	if ctx.Compiler == "" {
		t.Fatal("Compiler should be detected from CMakeCache")
	}
}
