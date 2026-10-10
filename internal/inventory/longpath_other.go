//go:build !windows

package inventory

import "path/filepath"

// longPath is the identity where a file system has no short names.
func longPath(path string) string { return path }

// samePath compares two paths by the host's rules, which here tell case apart.
func samePath(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }
