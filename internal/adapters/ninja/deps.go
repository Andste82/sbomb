package ninja

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	depsSignature = "# ninjadeps\n"
	depsVersion   = uint32(4)
	maxDepsInput  = 64 << 20
	maxDepsRecord = (1 << 19) - 1
)

var (
	ErrDepsMalformed   = errors.New("malformed ninja deps log")
	ErrDepsUnsupported = errors.New("unsupported ninja deps version")
	ErrDepsInputLimit  = errors.New("ninja deps input limit exceeded")
)

type DepsRecord struct {
	Output       string
	Dependencies []string
	Mtime        uint64
}

func ParseDepsFile(path string) ([]DepsRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDeps(data)
}

func ParseDeps(data []byte) ([]DepsRecord, error) {
	if len(data) > maxDepsInput {
		return nil, fmt.Errorf("deps log exceeds %d bytes: %w", maxDepsInput, ErrDepsInputLimit)
	}
	if len(data) < len(depsSignature)+4 || string(data[:len(depsSignature)]) != depsSignature {
		return nil, ErrDepsMalformed
	}
	version := binary.LittleEndian.Uint32(data[len(depsSignature):])
	if version != depsVersion {
		return nil, fmt.Errorf("deps log version %d: %w", version, ErrDepsUnsupported)
	}
	data = data[len(depsSignature)+4:]
	nodes := make([]string, 0, 1024)
	var records []DepsRecord
	for len(data) > 0 {
		if len(data) < 4 {
			return nil, fmt.Errorf("record size truncated: %w", ErrDepsMalformed)
		}
		header := binary.LittleEndian.Uint32(data[:4])
		data = data[4:]
		isDeps := header&0x80000000 != 0
		size := int(header & 0x7fffffff)
		if size > maxDepsRecord {
			return nil, fmt.Errorf("record exceeds %d bytes: %w", maxDepsRecord, ErrDepsInputLimit)
		}
		if size > len(data) {
			return nil, fmt.Errorf("record truncated: %w", ErrDepsMalformed)
		}
		record := data[:size]
		data = data[size:]
		if isDeps {
			if size < 12 || size%4 != 0 {
				return nil, fmt.Errorf("invalid dependency record: %w", ErrDepsMalformed)
			}
			words := size / 4
			outputID := int32(binary.LittleEndian.Uint32(record[0:4]))
			if outputID < 0 || int(outputID) >= len(nodes) {
				return nil, fmt.Errorf("invalid output node id: %w", ErrDepsMalformed)
			}
			mtime := uint64(binary.LittleEndian.Uint32(record[4:8])) | uint64(binary.LittleEndian.Uint32(record[8:12]))<<32
			dependencies := make([]string, 0, words-3)
			for index := 3; index < words; index++ {
				id := int32(binary.LittleEndian.Uint32(record[index*4 : index*4+4]))
				if id < 0 || int(id) >= len(nodes) {
					return nil, fmt.Errorf("invalid dependency node id: %w", ErrDepsMalformed)
				}
				dependencies = append(dependencies, nodes[id])
			}
			records = append(records, DepsRecord{Output: nodes[outputID], Dependencies: dependencies, Mtime: mtime})
			continue
		}
		if size < 8 {
			return nil, fmt.Errorf("invalid path record: %w", ErrDepsMalformed)
		}
		pathEnd := size - 4
		for pathEnd > 0 && record[pathEnd-1] == 0 && size-pathEnd < 3 {
			pathEnd--
		}
		if pathEnd == 0 {
			return nil, fmt.Errorf("empty path record: %w", ErrDepsMalformed)
		}
		checksum := binary.LittleEndian.Uint32(record[size-4:])
		expectedID := ^checksum
		if int(expectedID) != len(nodes) {
			return nil, fmt.Errorf("path node id mismatch: %w", ErrDepsMalformed)
		}
		nodes = append(nodes, string(record[:pathEnd]))
	}
	return records, nil
}

func ReadDeps(r io.Reader) ([]DepsRecord, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDepsInput+1))
	if err != nil {
		return nil, err
	}
	return ParseDeps(data)
}
