// Package headers classifies used headers into the seven classes of
// specification section 14.4 and applies the inclusion defaults that follow
// from them.
package headers

import (
	"path/filepath"
	"sort"
	"strings"
)

// Class is the single class a used header belongs to (section 14.4).
type Class string

const (
	ClassGenerated       Class = "generated-header"
	ClassProject         Class = "project-header"
	ClassThirdParty      Class = "third-party-header"
	ClassSDK             Class = "sdk-header"
	ClassCompilerRuntime Class = "compiler-runtime-header"
	ClassSystem          Class = "system-header"
	ClassUnknown         Class = "unknown-header"
)

// conventionalSystemRoots is the last resort of section 14.4, used only when
// the toolchain reported no implicit include directories. Classification must
// prefer what the toolchain says over a hardcoded list.
var conventionalSystemRoots = []string{"/usr/include", "/usr/local/include"}

// ComponentRoot marks a directory that belongs to a mapped component. A root
// whose mapping came from something other than the project itself -- a
// submodule, a package manifest, curated configuration naming an external
// dependency -- makes the headers below it third-party rather than project
// headers.
type ComponentRoot struct {
	// Canonical is the component root as an identity prefix, for example
	// "project:dep/mbedtls".
	Canonical string
	// SDK marks a root supplied by a vendor SDK.
	SDK bool
	// External marks a root that did not originate in the project.
	External bool
}

// Input is one header to classify. Physical may be empty; classification then
// works from the identity alone.
type Input struct {
	// Canonical is the "<anchor>:<relative path>" identity of section 7.7.
	Canonical string
	// Physical is where the file was read from, used to test the implicit
	// include directories the toolchain reported.
	Physical string
	// Generated says the build system named the file as generated.
	Generated bool
}

// Classifier holds what section 14.4 needs beyond the identity itself.
type Classifier struct {
	implicitIncludeDirs []string
	compilerRoots       []string
	componentRoots      []ComponentRoot
}

// New builds a classifier. implicitIncludeDirs come from the File API's
// toolchains-v1 object; compilerRoots are the toolchain installation roots.
func New(implicitIncludeDirs, compilerRoots []string, componentRoots []ComponentRoot) *Classifier {
	c := &Classifier{
		implicitIncludeDirs: normalizeDirs(implicitIncludeDirs),
		compilerRoots:       normalizeDirs(compilerRoots),
		componentRoots:      append([]ComponentRoot{}, componentRoots...),
	}
	// Longest root first, so a nested component wins over its parent.
	sort.Slice(c.componentRoots, func(i, j int) bool {
		return len(c.componentRoots[i].Canonical) > len(c.componentRoots[j].Canonical)
	})
	return c
}

// Classify assigns exactly one class. The order of the tests is the order of
// the table in section 14.4: the more specific origin wins.
func (c *Classifier) Classify(in Input) Class {
	anchor, _ := split(in.Canonical)

	// Generated output is generated wherever it lives; the build anchor is the
	// usual case but the File API may name a generated file elsewhere.
	if in.Generated || anchor == "build" {
		return ClassGenerated
	}

	switch {
	case strings.HasPrefix(anchor, "sdk:"):
		return ClassSDK
	case strings.HasPrefix(anchor, "pkg:"), strings.HasPrefix(anchor, "extern:"):
		return ClassThirdParty
	case strings.HasPrefix(anchor, "sysroot:"):
		return ClassSystem
	case strings.HasPrefix(anchor, "toolchain:"):
		// A toolchain anchor covers the whole installation. Only what lies in
		// the compiler's own include directories is a compiler runtime header;
		// the rest of the installation is a distribution's system headers.
		if c.inImplicitIncludeDir(in.Physical) || c.inCompilerRoot(in.Physical) {
			return ClassCompilerRuntime
		}
		return ClassSystem
	}

	if c.inImplicitIncludeDir(in.Physical) {
		return ClassSystem
	}

	if root, found := c.componentRootFor(in.Canonical); found {
		switch {
		case root.SDK:
			return ClassSDK
		case root.External:
			return ClassThirdParty
		}
	}

	if anchor == "project" {
		return ClassProject
	}

	if anchor == "abs" && underConventionalSystemRoot(in.Physical, in.Canonical) {
		return ClassSystem
	}

	return ClassUnknown
}

// IncludedByDefault is the default policy table of section 14.4. An
// unclassifiable header is included and flagged, never dropped: nothing
// disappears silently.
func IncludedByDefault(class Class) bool {
	switch class {
	case ClassSystem, ClassCompilerRuntime:
		return false
	default:
		return true
	}
}

func (c *Classifier) componentRootFor(canonical string) (ComponentRoot, bool) {
	for _, root := range c.componentRoots {
		if root.Canonical == "" {
			continue
		}
		if canonical == root.Canonical || strings.HasPrefix(canonical, root.Canonical+"/") {
			return root, true
		}
	}
	return ComponentRoot{}, false
}

func (c *Classifier) inImplicitIncludeDir(physical string) bool {
	return underAny(physical, c.implicitIncludeDirs)
}

func (c *Classifier) inCompilerRoot(physical string) bool {
	return underAny(physical, c.compilerRoots)
}

func underConventionalSystemRoot(physical, canonical string) bool {
	if underAny(physical, conventionalSystemRoots) {
		return true
	}
	// An "abs" identity keeps the absolute path in its relative part, so it
	// can be tested even when the file was never resolved on disk.
	_, rel := split(canonical)
	if rel == "" {
		return false
	}
	return underAny("/"+strings.TrimPrefix(rel, "/"), conventionalSystemRoots)
}

// underAny reports whether path lies inside one of the directories, comparing
// whole segments so that "/usr/includex" does not match "/usr/include".
func underAny(path string, dirs []string) bool {
	if path == "" {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if clean == dir || strings.HasPrefix(clean, strings.TrimSuffix(dir, "/")+"/") {
			return true
		}
	}
	return false
}

func normalizeDirs(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Clean(dir)))
	}
	sort.Strings(out)
	return out
}

// split separates a canonical identity into its anchor key and relative path.
// Anchor keys may themselves contain a colon ("pkg:conan/mbedtls").
func split(canonical string) (string, string) {
	parts := strings.SplitN(canonical, ":", 2)
	if len(parts) != 2 {
		return canonical, ""
	}
	switch parts[0] {
	case "project", "build", "abs":
		return parts[0], parts[1]
	}
	rest := strings.SplitN(parts[1], ":", 2)
	if len(rest) != 2 {
		return canonical, ""
	}
	return parts[0] + ":" + rest[0], rest[1]
}
