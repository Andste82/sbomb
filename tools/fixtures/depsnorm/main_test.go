package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/adapters/ninja"
)

// depsLog builds a deps log in ninja's version 4 format: the signature, the
// version, then path records that define the node table and dependency records
// that reference it by index.
type depsLog struct {
	buffer bytes.Buffer
	nodes  int
}

func newDepsLog() *depsLog {
	log := &depsLog{}
	log.buffer.WriteString(signature)
	_ = binary.Write(&log.buffer, binary.LittleEndian, uint32(4))
	return log
}

// path appends a path record and returns the node id it was given.
func (l *depsLog) path(value string) int32 {
	payload := []byte(value)
	for len(payload)%4 != 0 {
		payload = append(payload, 0)
	}
	// The trailing checksum is the ones complement of the node index, which is
	// how ninja detects a log written by a different node table.
	checksum := ^uint32(l.nodes)
	record := make([]byte, 0, len(payload)+4)
	record = append(record, payload...)
	record = binary.LittleEndian.AppendUint32(record, checksum)

	_ = binary.Write(&l.buffer, binary.LittleEndian, uint32(len(record)))
	l.buffer.Write(record)
	id := int32(l.nodes)
	l.nodes++
	return id
}

func (l *depsLog) deps(output int32, mtime uint64, inputs ...int32) {
	record := make([]byte, 0, 12+4*len(inputs))
	record = binary.LittleEndian.AppendUint32(record, uint32(output))
	record = binary.LittleEndian.AppendUint32(record, uint32(mtime))
	record = binary.LittleEndian.AppendUint32(record, uint32(mtime>>32))
	for _, input := range inputs {
		record = binary.LittleEndian.AppendUint32(record, uint32(input))
	}
	_ = binary.Write(&l.buffer, binary.LittleEndian, uint32(len(record))|0x80000000)
	l.buffer.Write(record)
}

func sampleLog(t *testing.T, mtime uint64) []byte {
	t.Helper()
	log := newDepsLog()
	object := log.path("CMakeFiles/app.dir/main.c.o")
	source := log.path("../src/main.c")
	header := log.path("../include/app.h")
	log.deps(object, mtime, source, header)

	data := log.buffer.Bytes()
	if _, err := ninja.ParseDeps(data); err != nil {
		t.Fatalf("the test's own log does not parse: %v", err)
	}
	return data
}

func TestNormalizeZeroesTheTimestampAndChangesNothingElse(t *testing.T) {
	original := sampleLog(t, 0x0123456789abcdef)

	normalized, changed, err := normalize(original)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("a log with a timestamp should report a change")
	}
	if len(normalized) != len(original) {
		t.Fatalf("length changed from %d to %d", len(original), len(normalized))
	}

	// Exactly the eight timestamp bytes may differ. Everything else -- record
	// sizes, node ids, path padding, checksums -- is copied through.
	differing := 0
	for index := range original {
		if original[index] != normalized[index] {
			differing++
		}
	}
	if differing != 8 {
		t.Errorf("%d bytes changed, want the 8 of the timestamp", differing)
	}

	records, err := ninja.ParseDeps(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].Mtime != 0 {
		t.Errorf("mtime = %d, want 0", records[0].Mtime)
	}
	if records[0].Output != "CMakeFiles/app.dir/main.c.o" {
		t.Errorf("output = %q", records[0].Output)
	}
	if len(records[0].Dependencies) != 2 {
		t.Errorf("dependencies = %v, want two", records[0].Dependencies)
	}
}

// Normalizing an already normalized log must be a no-op, or regenerating a
// corpus would keep rewriting the same files.
func TestNormalizeIsIdempotent(t *testing.T) {
	once, _, err := normalize(sampleLog(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	twice, changed, err := normalize(once)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("a log with no timestamps should report no change")
	}
	if !bytes.Equal(once, twice) {
		t.Error("the second pass changed the bytes")
	}
}

func TestNormalizeFileRejectsSomethingThatIsNotADepsLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ninja_deps")
	if err := os.WriteFile(path, []byte("this is not a deps log"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalizeFile(path); err == nil {
		t.Fatal("normalizing a non-deps file should fail rather than rewrite it")
	}
}

func TestNormalizeFileRewritesInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ninja_deps")
	if err := os.WriteFile(path, sampleLog(t, 0xdeadbeef), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalizeFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := ninja.ParseDeps(data)
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Mtime != 0 {
		t.Errorf("mtime = %d, want 0", records[0].Mtime)
	}
}
