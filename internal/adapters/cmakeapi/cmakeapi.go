// Package cmakeapi reads the CMake File API reply directory, the richest
// structured description of a build that CMake offers.
//
// The reply is not a fixed set of filenames. CMake writes one
// index-<timestamp>.json naming content-addressed object files such as
// codemodel-v2-07769b617595cce5ba84.json, and the codemodel in turn refers to
// one file per target. Everything here is driven by that index.
package cmakeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxReplyFileBytes bounds a single reply file, per specification section 30.
const maxReplyFileBytes = 64 << 20

// Model is the normalized File API data.
type Model struct {
	// SourceRoot and BuildRoot come from the codemodel's paths object and are
	// the authoritative roots for the project and build anchors (section 7.4).
	SourceRoot string
	BuildRoot  string

	Cache          map[string]string
	Configurations []Configuration
	Toolchains     []Toolchain
}

// Toolchain describes one language's compiler as CMake detected it. The
// implicit include directories are what section 14.4 requires for classifying
// system headers, in preference to hardcoded path lists.
type Toolchain struct {
	Language            string
	CompilerID          string
	CompilerVersion     string
	CompilerPath        string
	ImplicitIncludeDirs []string
	ImplicitLinkDirs    []string
}

type Configuration struct {
	Name    string
	Targets []Target
}

type Target struct {
	Name string
	Type string
	// Artifacts are relative to BuildRoot; Sources are relative to SourceRoot.
	Artifacts     []Artifact
	Sources       []Source
	CompileGroups []CompileGroup
	SourceGroups  []SourceGroup
	Install       []InstallRule
	LinkFragments []string
	Dependencies  []string
}

type Artifact struct{ Path string }

type Source struct {
	Path              string
	IsGenerated       bool
	CompileGroupIndex int
}

type CompileGroup struct {
	Language     string
	IncludeDirs  []string
	SourceFiles  []string
	SourceIndexs []int
}

type SourceGroup struct {
	Name  string
	Files []string
}

// InstallRule records one install destination, which section 5.3 uses to
// discover final deliverables.
type InstallRule struct{ Dest string }

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

// indexDocument is the subset of index-*.json this adapter needs.
type indexDocument struct {
	Objects []struct {
		Kind     string `json:"kind"`
		JSONFile string `json:"jsonFile"`
	} `json:"objects"`
}

// ParseReplyDir reads a reply directory by way of its index file.
func ParseReplyDir(replyDir string) (*Model, error) {
	if replyDir == "" {
		return nil, errors.New("reply dir is required")
	}
	indexPath, err := findIndexFile(replyDir)
	if err != nil {
		return nil, err
	}

	var index indexDocument
	if err := readJSON(indexPath, &index); err != nil {
		return nil, fmt.Errorf("reading File API index: %w", err)
	}

	model := &Model{Cache: map[string]string{}}
	for _, object := range index.Objects {
		if !safeReplyName(object.JSONFile) {
			continue
		}
		path := filepath.Join(replyDir, object.JSONFile)
		var raw map[string]any
		if err := readJSON(path, &raw); err != nil {
			// A single unreadable object must not lose the rest of the reply.
			continue
		}
		switch object.Kind {
		case "codemodel":
			if err := model.parseCodeModel(raw, replyDir); err != nil {
				return nil, err
			}
		case "cache":
			model.parseCache(raw)
		case "toolchains":
			model.parseToolchains(raw)
		}
	}
	return model, nil
}

// findIndexFile returns the newest index file. CMake writes exactly one and
// removes older ones, but a crashed configure can leave more than one behind.
func findIndexFile(replyDir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(replyDir, "index-*.json"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no File API index in %s: %w", replyDir, os.ErrNotExist)
	}
	// The name embeds the configure timestamp, so lexical order is temporal.
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// safeReplyName rejects anything but a plain filename. The index is untrusted
// input (section 30), and a name such as "../../../etc/passwd" would otherwise
// make the adapter read outside the reply directory.
func safeReplyName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return filepath.Base(name) == name
}

func readJSON(path string, into any) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > maxReplyFileBytes {
		return fmt.Errorf("File API object %s exceeds the input limit", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

func (m *Model) parseCodeModel(raw map[string]any, replyDir string) error {
	if paths, ok := raw["paths"].(map[string]any); ok {
		m.SourceRoot = asString(paths["source"])
		m.BuildRoot = asString(paths["build"])
	}
	for _, entry := range toSlice(raw["configurations"]) {
		cfgMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		cfg := Configuration{Name: asString(cfgMap["name"])}
		for _, targetEntry := range toSlice(cfgMap["targets"]) {
			targetMap, ok := targetEntry.(map[string]any)
			if !ok {
				continue
			}
			// Target details live in their own file; the codemodel only names it.
			jsonFile := asString(targetMap["jsonFile"])
			if !safeReplyName(jsonFile) {
				continue
			}
			var targetRaw map[string]any
			if err := readJSON(filepath.Join(replyDir, jsonFile), &targetRaw); err != nil {
				continue
			}
			cfg.Targets = append(cfg.Targets, parseTarget(targetRaw))
		}
		m.Configurations = append(m.Configurations, cfg)
	}
	return nil
}

func parseTarget(raw map[string]any) Target {
	target := Target{
		Name: asString(raw["name"]),
		Type: asString(raw["type"]),
	}
	for _, art := range toSlice(raw["artifacts"]) {
		if am, ok := art.(map[string]any); ok {
			if path := asString(am["path"]); path != "" {
				target.Artifacts = append(target.Artifacts, Artifact{Path: path})
			}
		}
	}
	for _, src := range toSlice(raw["sources"]) {
		sm, ok := src.(map[string]any)
		if !ok {
			continue
		}
		target.Sources = append(target.Sources, Source{
			Path:              asString(sm["path"]),
			IsGenerated:       asBool(sm["isGenerated"]),
			CompileGroupIndex: asInt(sm["compileGroupIndex"], -1),
		})
	}
	for _, cg := range toSlice(raw["compileGroups"]) {
		cm, ok := cg.(map[string]any)
		if !ok {
			continue
		}
		group := CompileGroup{
			Language:    asString(cm["language"]),
			SourceFiles: stringsToSlice(cm["sourceFiles"]),
		}
		for _, include := range toSlice(cm["includes"]) {
			if im, ok := include.(map[string]any); ok {
				if path := asString(im["path"]); path != "" {
					group.IncludeDirs = append(group.IncludeDirs, path)
				}
			}
		}
		for _, index := range toSlice(cm["sourceIndexes"]) {
			group.SourceIndexs = append(group.SourceIndexs, asInt(index, -1))
		}
		target.CompileGroups = append(target.CompileGroups, group)
	}
	for _, sg := range toSlice(raw["sourceGroups"]) {
		if sm, ok := sg.(map[string]any); ok {
			target.SourceGroups = append(target.SourceGroups, SourceGroup{
				Name:  asString(sm["name"]),
				Files: stringsToSlice(sm["sourceIndexes"]),
			})
		}
	}
	// install is an object with a destinations array, not an array.
	if install, ok := raw["install"].(map[string]any); ok {
		for _, dest := range toSlice(install["destinations"]) {
			if dm, ok := dest.(map[string]any); ok {
				target.Install = append(target.Install, InstallRule{Dest: asString(dm["path"])})
			}
		}
	}
	if link, ok := raw["link"].(map[string]any); ok {
		for _, fragment := range toSlice(link["commandFragments"]) {
			if fm, ok := fragment.(map[string]any); ok {
				if value := asString(fm["fragment"]); value != "" {
					target.LinkFragments = append(target.LinkFragments, value)
				}
			}
		}
	}
	for _, dependency := range toSlice(raw["dependencies"]) {
		if dm, ok := dependency.(map[string]any); ok {
			if id := asString(dm["id"]); id != "" {
				target.Dependencies = append(target.Dependencies, id)
			}
		}
	}
	return target
}

func (m *Model) parseCache(raw map[string]any) {
	for _, entry := range toSlice(raw["entries"]) {
		if em, ok := entry.(map[string]any); ok {
			if name := asString(em["name"]); name != "" {
				m.Cache[name] = asString(em["value"])
			}
		}
	}
}

func (m *Model) parseToolchains(raw map[string]any) {
	for _, tool := range toSlice(raw["toolchains"]) {
		tm, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		toolchain := Toolchain{Language: asString(tm["language"])}
		if compiler, ok := tm["compiler"].(map[string]any); ok {
			toolchain.CompilerID = asString(compiler["id"])
			toolchain.CompilerVersion = asString(compiler["version"])
			toolchain.CompilerPath = asString(compiler["path"])
			if implicit, ok := compiler["implicit"].(map[string]any); ok {
				toolchain.ImplicitIncludeDirs = stringsToSlice(implicit["includeDirectories"])
				toolchain.ImplicitLinkDirs = stringsToSlice(implicit["linkDirectories"])
			}
		}
		m.Toolchains = append(m.Toolchains, toolchain)
	}
}

// Sysroot reports the configured sysroot, which anchors cross-compilation
// system files (section 7.2).
func (m *Model) Sysroot() string {
	for _, key := range []string{"CMAKE_SYSROOT", "CMAKE_SYSROOT_COMPILE", "CMAKE_OSX_SYSROOT"} {
		if value := m.Cache[key]; value != "" {
			return value
		}
	}
	return ""
}

// ToolchainRoot derives the compiler installation root from a compiler path,
// by dropping a trailing bin directory: /usr/bin/cc yields /usr.
func ToolchainRoot(compilerPath string) string {
	if compilerPath == "" {
		return ""
	}
	dir := filepath.Dir(filepath.Clean(compilerPath))
	if strings.EqualFold(filepath.Base(dir), "bin") {
		return filepath.Dir(dir)
	}
	return dir
}

// ToolchainID builds the anchor name for a toolchain, e.g. "gnu-13.3.0".
func ToolchainID(t Toolchain) string {
	id := strings.ToLower(t.CompilerID)
	if id == "" {
		id = "unknown"
	}
	if t.CompilerVersion == "" {
		return id
	}
	return id + "-" + t.CompilerVersion
}

// Configuration returns the named configuration, or the single one present
// when name is empty. Section 10.3 requires exactly one configuration per SBOM.
func (m *Model) Configuration(name string) (Configuration, error) {
	if len(m.Configurations) == 0 {
		return Configuration{}, fmt.Errorf("the File API reply contains no configuration")
	}
	if name == "" {
		if len(m.Configurations) == 1 {
			return m.Configurations[0], nil
		}
		names := make([]string, 0, len(m.Configurations))
		for _, cfg := range m.Configurations {
			names = append(names, cfg.Name)
		}
		sort.Strings(names)
		return Configuration{}, fmt.Errorf("AMBIGUOUS_BUILD_CONFIG: select one of %s", strings.Join(names, ", "))
	}
	for _, cfg := range m.Configurations {
		if strings.EqualFold(cfg.Name, name) {
			return cfg, nil
		}
	}
	return Configuration{}, fmt.Errorf("no configuration named %q in the File API reply", name)
}

// ArtifactCandidates lists executable artifacts, preferring targets that have
// an install rule, per the discovery order of section 5.3.
func (m *Model) ArtifactCandidates() []string {
	installed := make([]string, 0)
	other := make([]string, 0)
	for _, cfg := range m.Configurations {
		for _, target := range cfg.Targets {
			if target.Type != "EXECUTABLE" {
				continue
			}
			for _, art := range target.Artifacts {
				if art.Path == "" {
					continue
				}
				if len(target.Install) > 0 {
					installed = append(installed, art.Path)
				} else {
					other = append(other, art.Path)
				}
			}
		}
	}
	sort.Strings(installed)
	if len(installed) > 0 {
		return installed
	}
	sort.Strings(other)
	return other
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

func asInt(v any, fallback int) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return fallback
}

func toSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
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
