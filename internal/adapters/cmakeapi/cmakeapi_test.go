package cmakeapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseReplyDir(t *testing.T) {
	buildDir := t.TempDir()
	replyDir := filepath.Join(buildDir, ".cmake", "api", "v1", "reply")
	if err := os.MkdirAll(replyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replyDir, "codemodel-v2.json"), []byte(`{"configurations":[{"name":"Debug","targets":[{"name":"hello","type":"EXECUTABLE","artifacts":[{"path":"/tmp/hello"}],"sources":[{"path":"src/main.c","isGenerated":false}],"compileGroups":[{"sourceFiles":["src/main.c"]}]}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replyDir, "cache-v2.json"), []byte(`{"entries":[{"name":"CMAKE_BUILD_TYPE","value":"Debug"},{"name":"CMAKE_GENERATOR","value":"Ninja"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replyDir, "toolchains-v1.json"), []byte(`{"toolchains":[{"compiler":{"id":"GNU","path":"/usr/bin/gcc","version":"13.2.0"},"implicitIncludeDirs":["/usr/include","/usr/local/include"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	model, err := ParseReplyDir(replyDir)
	if err != nil {
		t.Fatalf("ParseReplyDir() error = %v", err)
	}
	if len(model.Configurations) != 1 {
		t.Fatalf("len(model.Configurations) = %d, want 1", len(model.Configurations))
	}
	if len(model.ArtifactCandidates()) != 1 {
		t.Fatalf("ArtifactCandidates() = %d, want 1", len(model.ArtifactCandidates()))
	}
	if model.Cache["CMAKE_BUILD_TYPE"] != "Debug" {
		t.Fatalf("cache build type = %q, want %q", model.Cache["CMAKE_BUILD_TYPE"], "Debug")
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
