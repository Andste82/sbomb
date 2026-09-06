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
	Name    string
	CompDir string
	Source  string
	Headers []FileReference
	// DebugInfo says the compilation unit was found in the debug information.
	DebugInfo bool
	// LineTable says the unit carried a line program. Without one the unit
	// contributes no header evidence, which section 4.4 must distinguish from
	// a unit that genuinely included nothing.
	LineTable bool
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
	return InspectBytes(data, path, opts), nil
}

// InspectBytes is Inspect for content that is not a file of its own: a member
// of a static archive, which exists only inside it. path names the content for
// diagnostics and is not opened.
func InspectBytes(data []byte, path string, opts Options) Result {
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
	return result
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
		unit.Headers, unit.LineTable = lineTableFiles(data, entry, compDir, unit.Source)
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

// lineTableFiles reads the line-table file table of one compilation unit
// (section 11.4 point 2). The file table is the right source, not the line
// entries: a header that contributes only declarations produces no line entry
// at all, so walking entries reports it as absent from a unit that plainly
// included it.
func lineTableFiles(data *dwarf.Data, entry *dwarf.Entry, compDir, source string) ([]FileReference, bool) {
	lineReader, err := data.LineReader(entry)
	if err != nil || lineReader == nil {
		return nil, false
	}
	seen := map[string]bool{}
	headers := make([]FileReference, 0)
	for index, file := range lineReader.Files() {
		// Entry 0 is not a header: DWARF 5 defines it as the primary source
		// file and DWARF 4 leaves it unused. Clang emits nothing else, and its
		// entry 0 does not even resolve to the path the unit was compiled
		// from, so taking it would invent a file that never existed.
		if index == 0 || file == nil {
			continue
		}
		if strings.ContainsAny(file.Name, "<>") {
			// Synthetic entries such as "<built-in>" name no file.
			continue
		}
		path := resolvePath(compDir, file.Name)
		if path == "" || path == source || seen[path] {
			continue
		}
		seen[path] = true
		// DWARF records no inclusion depth, so directness cannot be asserted
		// here; section 14.4 asks for it only where the source distinguishes it.
		headers = append(headers, FileReference{Path: path})
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].Path < headers[j].Path })
	return headers, true
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
