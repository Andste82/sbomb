//go:build windows

package inventory

import (
	"path/filepath"
	"strings"
	"syscall"
)

// longPath expands the 8.3 short names in a path, as filepath.EvalSymlinks
// does on Windows, without following a link. It is what a resolved path is
// compared against: EvalSymlinks also turns C:\Users\RUNNER~1 into
// C:\Users\runneradmin, and comparing its answer with the path as given took
// every file below a short name for a link -- and refused to hash it.
func longPath(path string) string {
	from, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buffer := make([]uint16, 260)
	for {
		n, err := syscall.GetLongPathName(from, &buffer[0], uint32(len(buffer)))
		if err != nil || n == 0 {
			return path
		}
		if n < uint32(len(buffer)) {
			return syscall.UTF16ToString(buffer[:n])
		}
		buffer = make([]uint16, n)
	}
}

// samePath compares two paths as the Windows file system does: without regard
// to case, which EvalSymlinks takes from the disk.
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
