package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidFile(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "sbomb.json")
	if err := os.WriteFile(cfgPath, []byte(`{"project":{"name":"demo","root":"."},"build":{"dir":"build"},"mode":"single"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Project.Name != "demo" {
		t.Fatalf("Project.Name = %q", cfg.Project.Name)
	}
	if cfg.Build.Dir != "build" {
		t.Fatalf("Build.Dir = %q", cfg.Build.Dir)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "sbomb.json")
	if err := os.WriteFile(cfgPath, []byte(`{"notARealKey":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath); err == nil {
		t.Fatal("Load() accepted unknown key")
	}
}
