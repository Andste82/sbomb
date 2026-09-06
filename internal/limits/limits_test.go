package limits

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Section 30 point 2: a parser must be bounded. A file with no newline in it
// is the shape that makes an unbounded scanner allocate without limit.
func TestScannerRefusesALineOverTheLimit(t *testing.T) {
	scanner := Scanner(strings.NewReader(strings.Repeat("x", MaxLine+1)))
	if scanner.Scan() {
		t.Fatal("a line over the limit was accepted")
	}
	if scanner.Err() == nil {
		t.Error("the breach was not reported")
	}
}

func TestScannerAcceptsALineAtTheLimit(t *testing.T) {
	scanner := Scanner(strings.NewReader(strings.Repeat("x", MaxLine-1) + "\n"))
	if !scanner.Scan() {
		t.Fatalf("a line within the limit was refused: %v", scanner.Err())
	}
}

func TestTheZeroConfigIsTheSpecifiedDefault(t *testing.T) {
	var config Config
	if config.InputCeiling() != MaxInput {
		t.Errorf("ceiling = %d, want the default %d", config.InputCeiling(), MaxInput)
	}
}

// The ceiling is checked before the read, so an oversized file is refused
// rather than allocated for and then rejected.
func TestReadFileRefusesAnOversizedInputBeforeReadingIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.map")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 4096)), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{MaxInput: 1024}
	if _, err := config.ReadFile(path); !errors.Is(err, ErrInputLimitExceeded) {
		t.Errorf("err = %v, want ErrInputLimitExceeded", err)
	}
	if _, err := (Config{}).ReadFile(path); err != nil {
		t.Errorf("the default ceiling refused a 4 KiB file: %v", err)
	}
}

// Section 30.5. Without the option a symbolic link is followed, which is the
// ordinary behaviour; with it, the link is refused rather than resolved.
func TestStrictSymlinksRefusesALink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	lenient := Config{}
	if _, err := lenient.Stat(link); err != nil {
		t.Errorf("without the option the link should be followed: %v", err)
	}
	if _, err := lenient.ReadFile(link); err != nil {
		t.Errorf("without the option the link should be readable: %v", err)
	}

	strict := Config{StrictSymlinks: true}
	if _, err := strict.Stat(link); !errors.Is(err, ErrSymlink) {
		t.Errorf("Stat err = %v, want ErrSymlink", err)
	}
	if _, err := strict.ReadFile(link); !errors.Is(err, ErrSymlink) {
		t.Errorf("ReadFile err = %v, want ErrSymlink", err)
	}
	// Open asks the kernel, which closes the window an Lstat alone leaves
	// between the check and the open.
	if _, err := strict.Open(link); !errors.Is(err, ErrSymlink) {
		t.Errorf("Open err = %v, want ErrSymlink", err)
	}
	// A real file is still readable with the option on.
	if file, err := strict.Open(target); err != nil {
		t.Errorf("a regular file was refused: %v", err)
	} else {
		file.Close()
	}
}

func TestTokenLimit(t *testing.T) {
	if TooManyTokens(MaxTokensPerLine) {
		t.Error("the limit itself was refused")
	}
	if !TooManyTokens(MaxTokensPerLine + 1) {
		t.Error("a line over the token limit was accepted")
	}
}
