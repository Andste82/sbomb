package ninja

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestParseDepsRejectsBadHeaderAndVersion(t *testing.T) {
	if _, err := ParseDeps([]byte("bad")); !errors.Is(err, ErrDepsMalformed) {
		t.Fatalf("bad header error = %v", err)
	}
	data := append([]byte(depsSignature), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(data[len(depsSignature):], 3)
	if _, err := ParseDeps(data); !errors.Is(err, ErrDepsUnsupported) {
		t.Fatalf("bad version error = %v", err)
	}
}

func TestParseDepsRejectsTruncatedRecord(t *testing.T) {
	data := append([]byte(depsSignature), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(data[len(depsSignature):], depsVersion)
	data = append(data, 8, 0, 0, 128)
	if _, err := ParseDeps(data); !errors.Is(err, ErrDepsMalformed) {
		t.Fatalf("truncated record error = %v", err)
	}
}
