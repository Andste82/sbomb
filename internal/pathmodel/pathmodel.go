package pathmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// Resolve returns the canonical identity for a path in the form anchor:relpath.
func Resolve(path string, projectRoot string, buildRoot string) string {
	if path == "" {
		return "abs:"
	}
	clean := normalize(path)
	project := normalize(projectRoot)
	build := normalize(buildRoot)

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
		if hasPathPrefix(clean, anchor.root) {
			rel := strings.TrimPrefix(clean, anchor.root)
			rel = strings.TrimPrefix(rel, "/")
			if rel == "" {
				return anchor.key + ":."
			}
			return anchor.key + ":" + rel
		}
	}
	return "abs:" + strings.TrimPrefix(clean, "/")
}

func normalize(p string) string {
	if p == "" {
		return "/"
	}
	p = filepath.Clean(filepath.FromSlash(strings.ReplaceAll(p, "\\", "/")))
	if p == "." {
		return "/"
	}
	if strings.HasPrefix(p, "/") {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(filepath.Clean("/" + p))
}

func hasPathPrefix(path, prefix string) bool {
	if prefix == "/" {
		return true
	}
	path = strings.TrimSuffix(path, "/")
	prefix = strings.TrimSuffix(prefix, "/")
	if path == prefix {
		return true
	}
	if strings.HasPrefix(path, prefix+"/") {
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
