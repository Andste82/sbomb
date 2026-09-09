package pathmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
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

// IsAbsolute reports whether a path is absolute under the host OS or in a
// Windows path spelling carried by cross-platform build evidence.
func IsAbsolute(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}
	return len(path) >= 3 && isASCIIAlpha(path[0]) && path[1] == ':' && (path[2] == '/' || path[2] == '\\') || strings.HasPrefix(path, `\\\\`)
}

// NormalizeSeparators converts Windows and POSIX separators to slash form for
// host-side path joining and comparison.
func NormalizeSeparators(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

func isASCIIAlpha(value byte) bool {
	return (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}

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
	if len(p) >= 3 && p[1] == '$' && p[2] == ':' {
		p = p[:1] + p[2:]
	}
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

// Slug normalizes a string into a stable identifier, per the definition in
// specification section 28.4: lowercase, every character outside
// [a-z0-9._-] replaced by a hyphen, runs of hyphens collapsed, leading and
// trailing hyphens trimmed, and truncation to maxLen with a digest suffix so
// that two long names cannot collide.
func Slug(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(s))
	previousHyphen := false
	for _, r := range strings.ToLower(s) {
		keep := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !keep {
			r = '-'
		}
		if r == '-' {
			if previousHyphen {
				continue
			}
			previousHyphen = true
		} else {
			previousHyphen = false
		}
		builder.WriteRune(r)
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		slug = "unnamed"
	}
	if len(slug) <= maxLen {
		return slug
	}
	digest := sha256.Sum256([]byte(s))
	suffix := "-" + hex.EncodeToString(digest[:])[:8]
	keep := maxLen - len(suffix)
	if keep < 1 {
		keep = 1
	}
	return strings.Trim(slug[:keep], "-") + suffix
}
