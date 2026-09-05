package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"

	"github.com/example/sbomb/internal/testutil"
)

func TestExecuteVersion(t *testing.T) {
	code, _, _ := execute([]string{"version"})
	if code != 0 {
		t.Fatalf("version exit code = %d, want 0", code)
	}
}

func TestExecuteUnknownFlag(t *testing.T) {
	code, _, _ := execute([]string{"--bogus"})
	if code != 1 {
		t.Fatalf("--bogus exit code = %d, want 1", code)
	}
}

func TestExecuteExplainLoadsEvidenceDump(t *testing.T) {
	buildDir := t.TempDir()
	graph := evidence.New()
	graph.AddNode(domain.Node{ID: "artifact:build/demo.elf", Kind: domain.NodeArtifact})
	graph.AddNode(domain.Node{ID: "build:demo.o", Kind: domain.NodeObject})
	graph.AddNode(domain.Node{ID: "project:src/main.c", Kind: domain.NodeSource})
	graph.AddEdge(domain.Edge{From: "artifact:build/demo.elf", To: "build:demo.o", Type: "link", Strength: "linked", Confidence: domain.ConfidenceHigh, Source: "linker", Adapter: "linker"})
	graph.AddEdge(domain.Edge{From: "build:demo.o", To: "project:src/main.c", Type: "compile", Strength: "derived", Confidence: domain.ConfidenceHigh, Source: "compiledb", Adapter: "compiledb"})
	dump, err := os.Create(filepath.Join(buildDir, "evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Dump(dump); err != nil {
		t.Fatal(err)
	}
	if err := dump.Close(); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := execute([]string{"explain", "--build-dir", buildDir, "--file", "project:src/main.c", "--format", "json"})
	if code != 0 || errOut != "" {
		t.Fatalf("explain failed: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, `"subject": "project:src/main.c"`) {
		t.Fatalf("explain output missing subject: %q", out)
	}
}

func TestExecuteVerboseLevels(t *testing.T) {
	fixtureDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")

	// Level 1: [INFO]
	outDir1 := t.TempDir()
	code1, out1, errOut1 := execute([]string{"generate", "--build-dir", fixtureDir, "--policy", "lenient", "--output", filepath.Join(outDir1, "out.json"), "-v", "--reproducible"})
	if code1 != 0 || errOut1 != "" {
		t.Fatalf("generate -v failed: code=%d err=%q", code1, errOut1)
	}
	if !strings.Contains(out1, "[INFO]") {
		t.Fatalf("expected [INFO] logs in level 1 output: %s", out1)
	}
	if strings.Contains(out1, "[DEBUG]") || strings.Contains(out1, "[TRACE]") {
		t.Fatalf("level 1 output should not contain [DEBUG] or [TRACE]: %s", out1)
	}

	// Level 2: [INFO] and [DEBUG]
	outDir2 := t.TempDir()
	code2, out2, errOut2 := execute([]string{"generate", "--build-dir", fixtureDir, "--policy", "lenient", "--output", filepath.Join(outDir2, "out.json"), "-vv", "--reproducible"})
	if code2 != 0 || errOut2 != "" {
		t.Fatalf("generate -vv failed: code=%d err=%q", code2, errOut2)
	}
	if !strings.Contains(out2, "[INFO]") || !strings.Contains(out2, "[DEBUG]") {
		t.Fatalf("expected [INFO] and [DEBUG] logs in level 2 output: %s", out2)
	}
	if strings.Contains(out2, "[TRACE]") {
		t.Fatalf("level 2 output should not contain [TRACE]: %s", out2)
	}

	// Level 3: [INFO], [DEBUG] and [TRACE]
	outDir3 := t.TempDir()
	code3, out3, errOut3 := execute([]string{"generate", "--build-dir", fixtureDir, "--policy", "lenient", "--output", filepath.Join(outDir3, "out.json"), "--verbose=3", "--reproducible"})
	if code3 != 0 || errOut3 != "" {
		t.Fatalf("generate --verbose=3 failed: code=%d err=%q", code3, errOut3)
	}
	if !strings.Contains(out3, "[INFO]") || !strings.Contains(out3, "[DEBUG]") || !strings.Contains(out3, "[TRACE]") {
		t.Fatalf("expected [INFO], [DEBUG], and [TRACE] logs in level 3 output: %s", out3)
	}
}
