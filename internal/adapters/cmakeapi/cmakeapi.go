package cmakeapi

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// Model captures the minimal File API data needed for milestone 03.
type Model struct {
	Cache         map[string]string
	Configurations []Configuration
}

type Configuration struct {
	Name   string
	Targets []Target
}

type Target struct {
	Name            string
	Type            string
	Artifacts       []Artifact
	Sources         []Source
	CompileGroups   []CompileGroup
	SourceGroups    []SourceGroup
	Install         []InstallRule
}

type Artifact struct { Path string }
type Source struct { Path string; IsGenerated bool }
type CompileGroup struct { SourceFiles []string }
type SourceGroup struct { Name string; Files []string }
type InstallRule struct { Dest string }

// DiscoverReplyDir locates a CMake File API reply directory under a build root.
func DiscoverReplyDir(buildDir string) (string, error) {
	if buildDir == "" {
		return "", errors.New("build dir is required")
	}
	candidate := filepath.Join(buildDir, ".cmake", "api", "v1", "reply")
	info, err := os.Stat(candidate)
	if err == nil && info.IsDir() {
		return candidate, nil
	}
	return "", os.ErrNotExist
}

// ParseReplyDir reads the JSON reply files for codemodel-v2, cache-v2, etc.
func ParseReplyDir(replyDir string) (*Model, error) {
	if replyDir == "" {
		return nil, errors.New("reply dir is required")
	}
	model := &Model{Cache: map[string]string{}}

	for _, name := range []string{"codemodel-v2.json", "cache-v2.json", "toolchains-v1.json"} {
		path := filepath.Join(replyDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		switch name {
		case "codemodel-v2.json":
			if err := model.parseCodeModel(raw); err != nil {
				return nil, err
			}
		case "cache-v2.json":
			if err := model.parseCache(raw); err != nil {
				return nil, err
			}
		case "toolchains-v1.json":
			if err := model.parseToolchains(raw); err != nil {
				return nil, err
			}
		}
	}
	return model, nil
}

func (m *Model) parseCodeModel(raw map[string]any) error {
	configs, ok := raw["configurations"].([]any)
	if !ok {
		return nil
	}
	for _, entry := range configs {
		cfgMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		cfg := Configuration{Name: asString(cfgMap["name"])}
		targets, _ := cfgMap["targets"].([]any)
		for _, targetEntry := range targets {
			targetMap, ok := targetEntry.(map[string]any)
			if !ok {
				continue
			}
			target := Target{
				Name: asString(targetMap["name"]),
				Type: asString(targetMap["type"]),
			}
			for _, art := range toSlice(targetMap["artifacts"]) {
				if am, ok := art.(map[string]any); ok {
					target.Artifacts = append(target.Artifacts, Artifact{Path: asString(am["path"])})
				}
			}
			for _, src := range toSlice(targetMap["sources"]) {
				if sm, ok := src.(map[string]any); ok {
					target.Sources = append(target.Sources, Source{Path: asString(sm["path"]), IsGenerated: asBool(sm["isGenerated"])})
				}
			}
			for _, cg := range toSlice(targetMap["compileGroups"]) {
				if cm, ok := cg.(map[string]any); ok {
					target.CompileGroups = append(target.CompileGroups, CompileGroup{SourceFiles: stringsToSlice(cm["sourceFiles"])})
				}
			}
			for _, sg := range toSlice(targetMap["sourceGroups"]) {
				if sm, ok := sg.(map[string]any); ok {
					target.SourceGroups = append(target.SourceGroups, SourceGroup{Name: asString(sm["name"]), Files: stringsToSlice(sm["files"])})
				}
			}
			for _, inst := range toSlice(targetMap["install"]) {
				if im, ok := inst.(map[string]any); ok {
					target.Install = append(target.Install, InstallRule{Dest: asString(im["dest"])})
				}
			}
			cfg.Targets = append(cfg.Targets, target)
		}
		m.Configurations = append(m.Configurations, cfg)
	}
	return nil
}

func (m *Model) parseCache(raw map[string]any) error {
	entries, _ := raw["entries"].([]any)
	for _, entry := range entries {
		em, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		m.Cache[asString(em["name"])] = asString(em["value"]) 
	}
	return nil
}

func (m *Model) parseToolchains(raw map[string]any) error {
	tools, _ := raw["toolchains"].([]any)
	for _, tool := range tools {
		if tm, ok := tool.(map[string]any); ok {
			compiler, _ := tm["compiler"].(map[string]any)
			if compiler != nil {
				m.Cache["CMAKE_C_COMPILER_ID"] = asString(compiler["id"])
				m.Cache["CMAKE_C_COMPILER_VERSION"] = asString(compiler["version"])
				m.Cache["CMAKE_C_COMPILER_PATH"] = asString(compiler["path"])
			}
		}
	}
	return nil
}

func (m *Model) ArtifactCandidates() []string {
	out := make([]string, 0)
	for _, cfg := range m.Configurations {
		for _, target := range cfg.Targets {
			if target.Type != "EXECUTABLE" {
				continue
			}
			for _, art := range target.Artifacts {
				if art.Path != "" {
					out = append(out, art.Path)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	if sl, ok := v.([]any); ok {
		return sl
	}
	return nil
}

func stringsToSlice(v any) []string {
	items := toSlice(v)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
