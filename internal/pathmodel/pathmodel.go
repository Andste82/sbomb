package pathmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strings"
)

// Flavor controls the platform-specific rules used for logical path identity.
type Flavor interface {
	Normalize(string) string
	CaseSensitive() bool
}

type PosixFlavor struct{}

func (PosixFlavor) Normalize(path string) string { return normalizePosix(path) }
func (PosixFlavor) CaseSensitive() bool          { return true }

type WindowsFlavor struct{}

func (WindowsFlavor) Normalize(path string) string { return normalizeWindows(path) }
func (WindowsFlavor) CaseSensitive() bool          { return false }

// DefaultFlavor returns the logical path flavor for the current host.
func DefaultFlavor() Flavor {
	if runtime.GOOS == "windows" {
		return WindowsFlavor{}
	}
	return PosixFlavor{}
}

// Resolve returns the canonical identity for a path in the form anchor:relpath.
func Resolve(path string, projectRoot string, buildRoot string) string {
	return ResolveWithFlavor(path, projectRoot, buildRoot, DefaultFlavor())
}

// ResolveWithFlavor returns a canonical identity using the supplied path rules.
func ResolveWithFlavor(path string, projectRoot string, buildRoot string, flavor Flavor) string {
	if path == "" {
		return "abs:"
	}
	clean := flavor.Normalize(path)
	project := flavor.Normalize(projectRoot)
	build := flavor.Normalize(buildRoot)
	if !flavor.CaseSensitive() {
		clean = strings.ToLower(clean)
		project = strings.ToLower(project)
		build = strings.ToLower(build)
	}

	for _, anchor := range []struct {
		key  string
		root string
	}{
		{key: "project", root: project},
		{key: "build", root: build},
	} {
		if anchor.root == "" {
			continue
		}
		if hasPathPrefix(clean, anchor.root, flavor) {
			rel := strings.TrimPrefix(clean, anchor.root)
			rel = strings.TrimPrefix(rel, flavor.Normalize("/"))
			if rel == "" {
				return anchor.key + ":."
			}
			return anchor.key + ":" + rel
		}
	}
	return "abs:" + strings.TrimLeft(clean, "/\\")
}

// Base returns the final logical path component using the supplied flavor.
func Base(path string, flavor Flavor) string {
	if flavor == nil {
		flavor = DefaultFlavor()
	}
	clean := flavor.Normalize(path)
	clean = strings.TrimRight(clean, "/\\")
	if index := strings.LastIndexAny(clean, "/\\"); index >= 0 {
		return clean[index+1:]
	}
	return clean
}

func normalizePosix(p string) string {
	if p == "" {
		return "/"
	}
	return cleanPath(strings.ReplaceAll(p, "\\", "/"), "/")
}

func normalizeWindows(p string) string {
	if p == "" {
		return "/"
	}
	p = strings.ReplaceAll(p, "/", "\\")
	root := "/"
	if strings.HasPrefix(p, "\\\\") {
		root = "//"
	} else if len(p) >= 2 && p[1] == ':' {
		root = strings.ToUpper(p[:1]) + ":/"
		p = p[2:]
	}
	clean := cleanPath(p, "\\")
	return strings.ReplaceAll(root+strings.TrimPrefix(clean, "\\"), "\\", "/")
}

func cleanPath(p, separator string) string {
	absolute := strings.HasPrefix(p, separator)
	parts := strings.Split(p, separator)
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(cleaned) > 0 && cleaned[len(cleaned)-1] != ".." {
				cleaned = cleaned[:len(cleaned)-1]
			} else if !absolute {
				cleaned = append(cleaned, part)
			}
		default:
			cleaned = append(cleaned, part)
		}
	}
	result := strings.Join(cleaned, separator)
	if absolute || result == "" {
		return separator + result
	}
	return result
}

func hasPathPrefix(path, prefix string, flavor Flavor) bool {
	separator := "/"
	if prefix == separator || strings.HasSuffix(prefix, ":\\") {
		return true
	}
	path = strings.TrimSuffix(path, separator)
	prefix = strings.TrimSuffix(prefix, separator)
	if path == prefix {
		return true
	}
	if strings.HasPrefix(path, prefix+separator) {
		return true
	}
	return false
}

// Slug creates a stable short identifier with a hash suffix when truncated.
func Slug(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	h := sha256.Sum256([]byte(s))
	trim := maxLen - 1 - 10
	if trim < 1 {
		trim = 1
	}
	prefix := s[:trim]
	return prefix + "-" + hex.EncodeToString(h[:])[:10]
}
