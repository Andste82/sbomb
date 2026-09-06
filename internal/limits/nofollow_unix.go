//go:build unix

package limits

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// openNoFollow asks the kernel to refuse a symbolic link in the final
// component. Doing it in the open call rather than in a preceding Lstat closes
// the window in which the path could be replaced between the check and the
// open.
func openNoFollow(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s: %w", path, ErrSymlink)
		}
		return nil, err
	}
	return file, nil
}
