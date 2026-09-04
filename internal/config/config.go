package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var schemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "project": {"type": "object", "properties": {"name":{"type":"string"},"root":{"type":"string"}}},
    "build": {"type": "object", "properties": {"dir":{"type":"string"}}},
    "mode": {"type": "string", "enum": ["single", "assembly"]},
    "artifacts": {"type": "array"},
    "policy": {"type": "object"}
  },
  "additionalProperties": false
}`

type Config struct {
	SchemaVersion int         `json:"schemaVersion,omitempty"`
	Project       Project     `json:"project"`
	Build         Build       `json:"build"`
	Mode          string      `json:"mode"`
	Artifacts     []Artifact  `json:"artifacts,omitempty"`
	Policy        Policy      `json:"policy,omitempty"`
	Output        Output      `json:"output,omitempty"`
	Anchors       []Anchor    `json:"anchors,omitempty"`
	Discovery     Discovery   `json:"discovery,omitempty"`
	Components    []Component `json:"components,omitempty"`
	Generators    []Generator `json:"generators,omitempty"`
	Manifests     []string    `json:"manifests,omitempty"`
}

type Project struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Root     string `json:"root,omitempty"`
	Version  string `json:"version,omitempty"`
	Supplier string `json:"supplier,omitempty"`
	License  string `json:"license,omitempty"`
}

type Build struct {
	Dir           string `json:"dir"`
	Config        string `json:"config,omitempty"`
	Introspection struct {
		CMake      bool `json:"cmake,omitempty"`
		Ninja      bool `json:"ninja,omitempty"`
		Git        bool `json:"git,omitempty"`
		OSPackages bool `json:"osPackages,omitempty"`
	} `json:"introspection,omitempty"`
}

type Artifact struct {
	Path        string `json:"path"`
	Role        string `json:"role,omitempty"`
	Map         string `json:"map,omitempty"`
	LinkDepfile string `json:"linkDepfile,omitempty"`
}

type Policy struct {
	Profile              string `json:"profile,omitempty"`
	HeaderEvidence       string `json:"headerEvidence,omitempty"`
	FailOnUnanchoredFile bool   `json:"failOnUnanchoredFile,omitempty"`
}

type Output struct {
	Format         string   `json:"format,omitempty"`
	SpecVersion    string   `json:"specVersion,omitempty"`
	Reproducible   bool     `json:"reproducible,omitempty"`
	HashAlgorithms []string `json:"hashAlgorithms,omitempty"`
}

type Anchor struct {
	Key  string `json:"key"`
	Path string `json:"path"`
}

type Discovery struct {
	ExcludeTargetPatterns []string `json:"excludeTargetPatterns,omitempty"`
}

type Component struct {
	Path    string `json:"path,omitempty"`
	Match   string `json:"match,omitempty"`
	Name    string `json:"name,omitempty"`
	Type    string `json:"type,omitempty"`
	CDXType string `json:"cdxType,omitempty"`
	License string `json:"license,omitempty"`
}

type Generator struct {
	Output string   `json:"output"`
	Inputs []string `json:"inputs,omitempty"`
	Tool   string   `json:"tool,omitempty"`
}

func Schema() string { return schemaJSON }

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, err
	}
	if err := validateUnknownKeys(raw); err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	if cfg.Project.Name == "" {
		return Config{}, fmt.Errorf("missing required field: project.name")
	}
	if cfg.Build.Dir == "" {
		return Config{}, fmt.Errorf("missing required field: build.dir")
	}
	if cfg.Mode == "" {
		cfg.Mode = "single"
	}
	if cfg.Project.Root == "" {
		cfg.Project.Root = "."
	}
	if cfg.Build.Dir == "" {
		cfg.Build.Dir = filepath.Dir(path)
	}
	if cfg.Policy.Profile == "" {
		cfg.Policy.Profile = "default"
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if cfg.Mode != "" && cfg.Mode != "single" && cfg.Mode != "assembly" {
		return fmt.Errorf("invalid mode %q", cfg.Mode)
	}
	for _, a := range cfg.Artifacts {
		if a.Path == "" {
			return fmt.Errorf("artifact path is required")
		}
		if a.Role != "" && a.Role != "application" && a.Role != "bootloader" && a.Role != "library" && a.Role != "filesystem" && a.Role != "image" && a.Role != "package" && a.Role != "data" && a.Role != "other" {
			return fmt.Errorf("invalid artifact role %q", a.Role)
		}
	}
	for _, a := range cfg.Anchors {
		if a.Key == "" || a.Path == "" {
			return fmt.Errorf("anchor key and path are required")
		}
	}
	for _, g := range cfg.Generators {
		if g.Output == "" {
			return fmt.Errorf("generator output is required")
		}
	}
	return nil
}

func validateUnknownKeys(raw map[string]json.RawMessage) error {
	allowed := map[string]bool{
		"schemaVersion": true,
		"project":       true,
		"build":         true,
		"mode":          true,
		"artifacts":     true,
		"policy":        true,
		"anchors":       true,
		"discovery":     true,
		"components":    true,
		"generators":    true,
		"manifests":     true,
		"output":        true,
	}
	var extra []string
	for k := range raw {
		if !allowed[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		return fmt.Errorf("unknown key: %s", strings.Join(extra, ", "))
	}
	return nil
}
