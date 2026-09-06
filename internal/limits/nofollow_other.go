//go:build !unix

package limits

import (
	"fmt"
	"os"
)

// openNoFollow refuses a symbolic link on platforms with no O_NOFOLLOW, which
// is Windows and Plan 9.
//
// The check has to precede the open here, so a path replaced in between would
// still be followed. That window is real and cannot be closed without the
// kernel's help; it is narrower than no check at all, and section 30.5 asks
// for the refusal rather than for a particular mechanism. The alternative --
// refusing to run under --strict-symlinks on Windows at all -- would make the
// option mean something different on each platform, which is worse.
func openNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s: %w", path, ErrSymlink)
	}
	return os.Open(path)
}
