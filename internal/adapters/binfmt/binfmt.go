// Package binfmt inspects ELF and PE artifacts without executing external tools.
package binfmt

import (
	"bytes"
	"debug/dwarf"
	"debug/elf"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

type Options struct{ IncludeRuntimeLibraries bool }

type Format string

const (
	FormatELF Format = "elf"
	FormatPE  Format = "pe"
)

type FileReference struct {
	Path   string
	Direct bool
}

type CompilationUnit struct {
	Name      string
	CompDir   string
	Source    string
	Headers   []FileReference
	DebugInfo bool
}

type Result struct {
	Path             string
	Format           Format
	BuildID          string
	PDBPath          string
	PDBSignature     string
	RuntimeLibraries []string
	CompilationUnits []CompilationUnit
	LTO              bool
	Findings         []domain.Finding
}

// Inspect opens an ELF or PE artifact and extracts artifact-intrinsic evidence.
// Malformed and debug-info-free artifacts are represented by findings rather
// than returned as errors, so callers can continue discovery.
func Inspect(path string, opts Options) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Path: path}, err
	}
	result := Result{Path: path}
	switch {
	case bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}):
		result.Format = FormatELF
		inspectELF(&result, data, opts)
	case len(data) >= 2 && data[0] == 'M' && data[1] == 'Z':
		result.Format = FormatPE
		inspectPE(&result, data)
	default:
		result.Findings = append(result.Findings, finding("MALFORMED_BINARY", "artifact is not a supported ELF or PE file"))
	}
	return result, nil
}

func inspectELF(result *Result, data []byte, opts Options) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		result.Findings = append(result.Findings, finding("MALFORMED_BINARY", err.Error()))
		return
	}
	defer f.Close()
	result.BuildID = elfBuildID(f)
	for _, section := range f.Sections {
		if strings.HasPrefix(section.Name, ".gnu.lto_") {
			result.LTO = true
			break
		}
	}
	if opts.IncludeRuntimeLibraries {
		if names, err := f.DynString(elf.DT_NEEDED); err == nil {
			result.RuntimeLibraries = append(result.RuntimeLibraries, names...)
		}
		sort.Strings(result.RuntimeLibraries)
	}
	dwarfData, err := f.DWARF()
	if err != nil {
		result.Findings = append(result.Findings, finding("DEBUG_INFO_UNAVAILABLE", err.Error()))
		return
	}
	result.CompilationUnits = compilationUnits(dwarfData)
	if len(result.CompilationUnits) == 0 {
		result.Findings = append(result.Findings, finding("DEBUG_INFO_UNAVAILABLE", "artifact contains no DWARF compilation units"))
	}
}

func inspectPE(result *Result, data []byte) {
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		result.Findings = append(result.Findings, finding("MALFORMED_BINARY", err.Error()))
		return
	}
	defer f.Close()
	result.PDBPath, result.PDBSignature = peCodeView(f)
	if result.PDBPath == "" {
		result.PDBPath = pdbPath(data)
	}
	if dwarfData, dwarfErr := f.DWARF(); dwarfErr == nil {
		result.CompilationUnits = compilationUnits(dwarfData)
	}
	if len(result.CompilationUnits) == 0 {
		message := "artifact has no supported DWARF debug information"
		if result.PDBPath != "" {
			message = "artifact references a PDB; PDB parsing is unavailable"
		}
		result.Findings = append(result.Findings, finding("DEBUG_INFO_UNAVAILABLE", message))
	}
}

func compilationUnits(data *dwarf.Data) []CompilationUnit {
	reader := data.Reader()
	var units []CompilationUnit
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			break
		}
		if entry.Tag != dwarf.TagCompileUnit {
			if entry.Children {
				reader.SkipChildren()
			}
			continue
		}
		name, _ := entry.Val(dwarf.AttrName).(string)
		compDir, _ := entry.Val(dwarf.AttrCompDir).(string)
		unit := CompilationUnit{Name: name, CompDir: compDir, Source: resolvePath(compDir, name), DebugInfo: true}
		lineReader, err := data.LineReader(entry)
		if err == nil {
			seen := map[string]bool{}
			for {
				var line dwarf.LineEntry
				if err := lineReader.Next(&line); err != nil {
					break
				}
				if line.File == nil {
					continue
				}
				path := resolvePath(compDir, line.File.Name)
				if path == "" || path == unit.Source || seen[path] {
					continue
				}
				seen[path] = true
				unit.Headers = append(unit.Headers, FileReference{Path: path})
			}
		}
		sort.Slice(unit.Headers, func(i, j int) bool { return unit.Headers[i].Path < unit.Headers[j].Path })
		units = append(units, unit)
	}
	sort.Slice(units, func(i, j int) bool {
		if units[i].Source != units[j].Source {
			return units[i].Source < units[j].Source
		}
		return units[i].Name < units[j].Name
	})
	return units
}

func elfBuildID(f *elf.File) string {
	section := f.Section(".note.gnu.build-id")
	if section == nil {
		return ""
	}
	data, err := section.Data()
	if err != nil || len(data) < 16 {
		return ""
	}
	order := f.ByteOrder
	namesz, descsz := order.Uint32(data[0:4]), order.Uint32(data[4:8])
	nameEnd := uint64(12) + uint64((namesz+3)&^3)
	descEnd := nameEnd + uint64(descsz)
	if descEnd > uint64(len(data)) || namesz == 0 {
		return ""
	}
	return fmt.Sprintf("%x", data[nameEnd:descEnd])
}

func pdbPath(data []byte) string {
	for _, marker := range []string{".pdb", ".PDB"} {
		index := bytes.Index(data, []byte(marker))
		if index < 0 {
			continue
		}
		start := index
		for start > 0 && data[start-1] >= 0x20 && data[start-1] < 0x7f {
			start--
		}
		return string(data[start : index+len(marker)])
	}
	return ""
}

func peCodeView(f *pe.File) (string, string) {
	var directory pe.DataDirectory
	switch header := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		directory = header.DataDirectory[6]
	case *pe.OptionalHeader64:
		directory = header.DataDirectory[6]
	default:
		return "", ""
	}
	if directory.VirtualAddress == 0 {
		return "", ""
	}
	section, offset, ok := peSectionData(f, directory.VirtualAddress)
	if !ok {
		return "", ""
	}
	if offset+int(directory.Size) > len(section) {
		return "", ""
	}
	for pos := offset; pos+28 <= offset+int(directory.Size); pos += 28 {
		if binary.LittleEndian.Uint32(section[pos+12:pos+16]) != 2 {
			continue
		}
		size := binary.LittleEndian.Uint32(section[pos+16 : pos+20])
		rva := binary.LittleEndian.Uint32(section[pos+20 : pos+24])
		debugSection, debugOffset, ok := peSectionData(f, rva)
		if !ok || size < 24 || debugOffset+int(size) > len(debugSection) {
			continue
		}
		record := debugSection[debugOffset : debugOffset+int(size)]
		if !bytes.Equal(record[:4], []byte("RSDS")) {
			continue
		}
		age := binary.LittleEndian.Uint32(record[20:24])
		pathEnd := bytes.IndexByte(record[24:], 0)
		if pathEnd < 0 {
			pathEnd = len(record) - 24
		}
		guid := fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x", binary.LittleEndian.Uint32(record[4:8]), binary.LittleEndian.Uint16(record[8:10]), binary.LittleEndian.Uint16(record[10:12]), record[12], record[13], record[14], record[15], record[16], record[17], record[18], record[19])
		return string(record[24 : 24+pathEnd]), fmt.Sprintf("%s-%d", guid, age)
	}
	return "", ""
}

func peSectionData(f *pe.File, rva uint32) ([]byte, int, bool) {
	for _, section := range f.Sections {
		start := section.VirtualAddress
		end := start + section.Size
		if rva < start || rva >= end {
			continue
		}
		data, err := section.Data()
		if err != nil {
			return nil, 0, false
		}
		offset := int(rva - start)
		if offset >= len(data) {
			return nil, 0, false
		}
		return data, offset, true
	}
	return nil, 0, false
}

func resolvePath(compDir, name string) string {
	if name == "" {
		return ""
	}
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	if compDir == "" {
		return filepath.Clean(name)
	}
	return filepath.Clean(filepath.Join(compDir, name))
}

func finding(id, message string) domain.Finding {
	return domain.Finding{ID: id, Severity: domain.SeverityInfo, Message: message, Subject: domain.Subject{Kind: "artifact"}}
}
