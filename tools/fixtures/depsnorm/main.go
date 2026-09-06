// Command depsnorm rewrites the modification times inside a ninja deps log to
// a constant.
//
// It exists so that regenerating an unchanged fixture corpus is a no-op.
// `.ninja_deps` is a binary log, and every dependency record in it carries the
// output's modification time, which is wall clock. Two regenerations of the
// same sources therefore produced different bytes for every Ninja fixture --
// around a hundred files of pure churn, which made reviewing a real corpus
// change harder than it should be.
//
// Nothing sbomb reads is affected. The parser in internal/adapters/ninja does
// decode the field, and no caller uses it: staleness is decided from the
// filesystem, not from what a build log remembers about it.
//
// The rewrite is surgical. Only the eight bytes of each record's timestamp
// change; record sizes, node identifiers, path padding and checksums are
// copied through untouched, so the result is the same log with one field
// pinned rather than a re-serialization that might normalize something else by
// accident.
//
//	go run ./tools/fixtures/depsnorm <file>...
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/example/sbomb/internal/adapters/ninja"
)

const (
	signature = "# ninjadeps\n"
	// headerSize is the signature plus the four-byte version.
	headerSize = len(signature) + 4
	// mtimeOffset is where the timestamp sits inside a dependency record,
	// after the four-byte output node identifier.
	mtimeOffset = 4
	mtimeSize   = 8
)

var errMalformed = errors.New("malformed ninja deps log")

func main() {
	paths := os.Args[1:]
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "usage: depsnorm <file>...")
		os.Exit(1)
	}
	for _, path := range paths {
		if err := normalizeFile(path); err != nil {
			fmt.Fprintf(os.Stderr, "depsnorm: %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

func normalizeFile(path string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// What the log means has to survive the rewrite, so the records are read
	// before and after and compared. Pinning a timestamp is not worth a
	// corrupted fixture.
	before, err := ninja.ParseDeps(original)
	if err != nil {
		return fmt.Errorf("parse before: %w", err)
	}

	normalized, changed, err := normalize(original)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	after, err := ninja.ParseDeps(normalized)
	if err != nil {
		return fmt.Errorf("parse after: %w", err)
	}
	if err := sameRecords(before, after); err != nil {
		return err
	}
	return writeAtomic(path, normalized)
}

// normalize zeroes the timestamp of every dependency record, walking the log
// with the same record framing the parser uses.
func normalize(data []byte) ([]byte, bool, error) {
	if len(data) < headerSize || string(data[:len(signature)]) != signature {
		return nil, false, errMalformed
	}
	out := append([]byte(nil), data...)
	changed := false
	offset := headerSize
	for offset < len(out) {
		if len(out)-offset < 4 {
			return nil, false, fmt.Errorf("record size truncated: %w", errMalformed)
		}
		header := binary.LittleEndian.Uint32(out[offset : offset+4])
		offset += 4
		size := int(header & 0x7fffffff)
		if size > len(out)-offset {
			return nil, false, fmt.Errorf("record truncated: %w", errMalformed)
		}
		isDeps := header&0x80000000 != 0
		if isDeps {
			if size < mtimeOffset+mtimeSize {
				return nil, false, fmt.Errorf("dependency record too short: %w", errMalformed)
			}
			field := out[offset+mtimeOffset : offset+mtimeOffset+mtimeSize]
			if !bytes.Equal(field, make([]byte, mtimeSize)) {
				for index := range field {
					field[index] = 0
				}
				changed = true
			}
		}
		offset += size
	}
	return out, changed, nil
}

func sameRecords(before, after []ninja.DepsRecord) error {
	if len(before) != len(after) {
		return fmt.Errorf("record count changed from %d to %d", len(before), len(after))
	}
	for index := range before {
		if before[index].Output != after[index].Output {
			return fmt.Errorf("record %d output changed", index)
		}
		if len(before[index].Dependencies) != len(after[index].Dependencies) {
			return fmt.Errorf("record %d dependency count changed", index)
		}
		for position := range before[index].Dependencies {
			if before[index].Dependencies[position] != after[index].Dependencies[position] {
				return fmt.Errorf("record %d dependency %d changed", index, position)
			}
		}
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".depsnorm-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		os.Remove(name)
		return err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}
