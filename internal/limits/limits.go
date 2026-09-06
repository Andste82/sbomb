// Package limits holds the parser bounds of specification section 30.
//
// Build metadata is untrusted input: it comes from somebody else's build tree
// and may be truncated, enormous or hostile. Section 30 answers that with
// explicit limits, and they live here rather than in each parser so that
// eighteen files cannot disagree about what "the maximum line length" is --
// which is what they did before this package existed.
package limits

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// The bounds of section 30 point 2. They are the defaults; MaxInput is
// overridable per run through --max-input-size.
const (
	// MaxLine is the longest line any parser will read.
	MaxLine = 1 << 20
	// MaxTokensPerLine bounds a single line's field count, so a line that is
	// short in bytes but pathological in structure cannot cost unbounded work.
	MaxTokensPerLine = 100_000
	// MaxInput is the default ceiling for one input file.
	MaxInput int64 = 2 << 30
	// MaxDepth bounds every recursive expansion: response files, includes,
	// nested manifests.
	MaxDepth = 64

	// InitialBuffer is what a line scanner starts with. Growing to MaxLine is
	// allowed; starting there would cost a megabyte per open file.
	InitialBuffer = 64 << 10
)

var (
	// ErrInputLimitExceeded reports an input larger than the ceiling. A parser
	// returning it MUST return what it read before the breach, per section
	// 11.5: nothing disappears silently.
	ErrInputLimitExceeded = errors.New("input limit exceeded")
	// ErrSymlink reports a path that is a symbolic link while --strict-symlinks
	// is set (section 30.5).
	ErrSymlink = errors.New("path is a symbolic link and strict symlinks are enabled")
)

// Config is the per-run limit policy. The zero value is the default policy, so
// a caller that has no opinion still gets the bounds of section 30.
type Config struct {
	// MaxInput overrides the default input ceiling. Zero means the default.
	MaxInput int64
	// StrictSymlinks refuses to open a path whose final component is a
	// symbolic link (section 30.5).
	StrictSymlinks bool
}

// InputCeiling is the effective ceiling for one input.
func (c Config) InputCeiling() int64 {
	if c.MaxInput > 0 {
		return c.MaxInput
	}
	return MaxInput
}

// Scanner returns a line scanner bounded to MaxLine. Every parser in this
// repository reads lines this way, so none of them can be made to allocate
// without bound by a file with no newline in it.
func Scanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, InitialBuffer), MaxLine)
	return scanner
}

// ReadFile reads a whole file under the run's limits. It refuses a file over
// the ceiling before allocating for it, rather than after.
func (c Config) ReadFile(path string) ([]byte, error) {
	info, err := c.Stat(path)
	if err != nil {
		return nil, err
	}
	if ceiling := c.InputCeiling(); info.Size() > ceiling {
		return nil, fmt.Errorf("%s is %d bytes, over the %d byte limit: %w",
			path, info.Size(), ceiling, ErrInputLimitExceeded)
	}
	return os.ReadFile(path)
}

// Stat is Lstat when strict symlinks are enabled, so that a link is seen as
// one rather than followed.
func (c Config) Stat(path string) (os.FileInfo, error) {
	if !c.StrictSymlinks {
		return os.Stat(path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s: %w", path, ErrSymlink)
	}
	return info, nil
}

// Open opens a file under the run's limits. With strict symlinks it asks the
// kernel to refuse a symbolic link in the final component (section 30.5),
// which closes the window between checking and opening that an Lstat alone
// leaves open.
func (c Config) Open(path string) (*os.File, error) {
	if !c.StrictSymlinks {
		return os.Open(path)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s: %w", path, ErrSymlink)
		}
		return nil, err
	}
	return file, nil
}

// TooManyTokens reports a line whose field count is beyond what section 30
// permits.
func TooManyTokens(count int) bool { return count > MaxTokensPerLine }
