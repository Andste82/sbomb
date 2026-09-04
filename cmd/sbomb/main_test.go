package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
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
