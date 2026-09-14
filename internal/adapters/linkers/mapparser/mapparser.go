// Package mapparser reads linker map files.
//
// Map formats are not standardized and are not line-oriented in any uniform
// way: they are divided into named sections whose contents mean different
// things. A parser that scans every line for anything that looks like a path
// reports linker-script wildcards such as *(.gnu.attributes) and lld section
// placements such as foo.o:(.text) as archive members. Everything here is
// therefore driven by which section the line belongs to.
//
// Map evidence matters more than specification section 11.2 suggests: GNU ld
// does not list extracted archive members in its dependency file, only the
// archive, so member-level attribution comes from here (docs/dev/deviations.md D1).
package mapparser

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

const (
	MaxLineLength = 1 << 20
	MaxInputSize  = 2 << 30
	MaxTokensLine = 100000
)

// Format identifiers returned by Sniff.
const (
	FormatGNULD   = "gnu-ld"
	FormatGNUGold = "gnu-gold"
	FormatLLD     = "lld"
	FormatMSVC    = "msvc"
	FormatIAR     = "iar"
)

type Kind string

const (
	LinkedObject     Kind = "object"
	StaticArchive    Kind = "archive"
	ArchiveMember    Kind = "archive-member"
	SharedLibrary    Kind = "shared-library"
	DiscardedSection Kind = "discarded-section"
	// RetainedSection is one input section the map places into an output
	// section. Section 4.5 needs both halves: an object counts as fully
	// discarded only when the evidence enumerates what was kept as well.
	RetainedSection Kind = "retained-section"
	LinkerScript    Kind = "linker-script"
)

type Record struct {
	Kind    Kind
	Path    string
	Archive string
	Member  string
	Raw     string
}

type Result struct {
	Format  string
	Records []Record
	Err     error
}

var ErrMalformed = errors.New("malformed link evidence")
var ErrInputLimitExceeded = errors.New("input limit exceeded")
var ErrUnknownFormat = errors.New("unknown linker map format")

// ParseMSVCVerbose reads the optional text emitted by link.exe with
// /VERBOSE:REF. It is separate from Parse because the linker writes this
// evidence to the build stream, not to the map file.
func ParseMSVCVerbose(text string) []Record {
	var records []Record
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		const prefix = "Discarded "
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		from := strings.LastIndex(trimmed, " from ")
		if from < 0 {
			continue
		}
		target := strings.TrimSpace(trimmed[from+len(" from "):])
		archive, member, ok := splitArchiveMember(target)
		if !ok {
			continue
		}
		records = append(records, Record{
			Kind:    DiscardedSection,
			Path:    archive + "(" + member + ")",
			Archive: archive,
			Member:  member,
			Raw:     line,
		})
	}
	return records
}

// Sniff identifies the map format from its content.
func Sniff(text string) string {
	hasMemberBlock := strings.Contains(text, "Archive member included")
	switch {
	// Only GNU ld (bfd) emits the linker script and memory map section. Gold
	// writes the same archive-member header but no memory map, which is the
	// only reliable way to tell the two apart in recent binutils.
	case hasMemberBlock && strings.Contains(text, "Linker script and memory map"):
		return FormatGNULD
	case strings.Contains(text, "Memory Configuration"):
		return FormatGNULD
	case hasMemberBlock:
		return FormatGNUGold
	case strings.Contains(text, "Preferred load address is"), strings.Contains(text, " Address         Publics by Value"):
		return FormatMSVC
	case strings.Contains(text, "IAR ELF Linker"):
		return FormatIAR
	}
	// Only a map that carries none of the markers above reaches this loop, so
	// it is the whole file that is walked here, line by line. Walking it with
	// an index rather than strings.Split matters at map sizes: Split builds a
	// header for every line of a 200 MB file before the first one is looked at.
	for offset := 0; offset <= len(text); {
		line := text[offset:]
		if end := strings.IndexByte(line, '\n'); end >= 0 {
			line, offset = line[:end], offset+end+1
		} else {
			offset = len(text) + 1
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "VMA") && strings.Contains(trimmed, "LMA") &&
			strings.Contains(trimmed, "Out") && strings.Contains(trimmed, "In") && strings.Contains(trimmed, "Symbol") {
			return FormatLLD
		}
	}
	return ""
}

// gnuSection names the block a line belongs to in a GNU ld or gold map.
type gnuSection int

const (
	gnuOther gnuSection = iota
	gnuArchiveMembers
	gnuAsNeeded
	gnuDiscarded
	gnuMemoryMap
)

// gnuHeaders maps a section header to the state it starts. Any other
// unindented, non-empty line also ends the current section, which is what
// keeps the archive-member block from swallowing the rest of the file.
var gnuHeaders = map[string]gnuSection{
	"Archive member included to satisfy reference by file (symbol)":    gnuArchiveMembers,
	"Archive member included to satisfy reference by file":             gnuArchiveMembers,
	"As-needed library included to satisfy reference by file (symbol)": gnuAsNeeded,
	"As-needed library included to satisfy reference by file":          gnuAsNeeded,
	"Discarded input sections":                                         gnuDiscarded,
	"Linker script and memory map":                                     gnuMemoryMap,
	"Allocating common symbols":                                        gnuOther,
	"Merging program properties":                                       gnuOther,
	"Memory Configuration":                                             gnuOther,
}

// lldInputColumn matches an entry of lld's "In" column: a file specification
// followed by the section it contributed, such as
// "libcrypto.a(crypto.c.o):(.text)" or "/lib/Scrt1.o:(.text)".
var lldInputColumn = regexp.MustCompile(`^(.+?):\(([^)]*)\)$`)

// Parse reads a map in the given format. An empty format is sniffed from the
// content. Parsing is streaming and tolerates truncation: whatever was read
// before the failure point is returned alongside the error, per section 11.5.
func Parse(r io.Reader, format string) Result {
	result := Result{Format: format}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), MaxLineLength)

	emit := newRecorder(&result)
	section := gnuOther
	msvcState := msvcOther
	var sawMSVCSections, sawMSVCPublics, sawMSVCStatic bool
	var allocated int

	for scanner.Scan() {
		line := lineOf(scanner.Bytes())
		allocated += len(line)
		if allocated > MaxInputSize {
			result.Err = fmt.Errorf("map input exceeds %d bytes: %w", MaxInputSize, ErrInputLimitExceeded)
			return result
		}
		// A field needs a byte of its own, so a line shorter than the limit
		// cannot reach it and is not counted at all. Counting was a second
		// walk over every one of the millions of lines of a large map, to
		// prove each time what its length already says.
		if len(line) > MaxTokensLine && fieldCountExceeds(line, MaxTokensLine) {
			result.Err = fmt.Errorf("map line exceeds %d tokens: %w", MaxTokensLine, ErrInputLimitExceeded)
			return result
		}

		switch result.Format {
		case FormatLLD:
			parseLLDLine(line, emit)
		case FormatMSVC:
			msvcState, sawMSVCSections, sawMSVCPublics, sawMSVCStatic = parseMSVCLine(line, msvcState, emit, sawMSVCSections, sawMSVCPublics, sawMSVCStatic)
		case FormatIAR:
			parseGenericLine(line, emit)
		default:
			section = parseGNULine(line, section, emit)
		}
	}

	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "token too long") {
			result.Err = fmt.Errorf("read linker map: %w: %v", ErrInputLimitExceeded, err)
		} else {
			result.Err = fmt.Errorf("read linker map: %w: %v", ErrMalformed, err)
		}
		return result
	}
	if result.Format == FormatMSVC && (!sawMSVCSections || !sawMSVCPublics || !sawMSVCStatic) {
		result.Err = fmt.Errorf("incomplete MSVC map: %w", ErrMalformed)
		return result
	}
	if len(result.Records) == 0 {
		result.Err = ErrMalformed
	}
	return result
}

// lineOf names the scanner's line without copying it. scanner.Text() copies
// every line onto the heap, which on a 200 MB map is 200 MB of garbage for
// lines that are almost all read once and thrown away. The string here borrows
// the scanner's buffer instead, and is therefore only valid until the next
// scan: nothing may keep it, which is why the recorder copies the few lines
// that do become records.
func lineOf(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return unsafe.String(&raw[0], len(raw))
}

type msvcSection int

const (
	msvcOther msvcSection = iota
	msvcSectionTable
	msvcPublics
	msvcStatic
)

// parseMSVCLine tracks the named sections of link.exe's map instead of
// treating every path-shaped token as evidence. Both symbol sections use the
// same columns; the section state is what makes the interpretation safe.
func parseMSVCLine(line string, section msvcSection, emit func(Record), sawSections, sawPublics, sawStatic bool) (msvcSection, bool, bool, bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "Start") && strings.Contains(trimmed, "Length") && strings.Contains(trimmed, "Name") && strings.Contains(trimmed, "Class") {
		return msvcSectionTable, true, sawPublics, sawStatic
	}
	if strings.Contains(trimmed, "Publics by Value") {
		return msvcPublics, sawSections, true, sawStatic
	}
	if trimmed == "Static symbols" {
		return msvcStatic, sawSections, sawPublics, true
	}
	if section == msvcPublics || section == msvcStatic {
		parseMSVCInputLine(line, emit)
	}
	return section, sawSections, sawPublics, sawStatic
}

func parseMSVCInputLine(line string, emit func(Record)) {
	source, count := lastField(line)
	if count < 5 {
		return
	}
	if source == "<absolute>" || source == "<linker-defined>" {
		return
	}
	if colon := strings.LastIndexByte(source, ':'); colon > 0 {
		library, member := source[:colon], source[colon+1:]
		if strings.HasSuffix(member, ".obj") {
			archive := library
			if !strings.HasSuffix(archive, ".lib") {
				archive += ".lib"
			}
			emit(Record{Kind: StaticArchive, Path: archive, Raw: line})
			emit(Record{Kind: ArchiveMember, Path: archive + "(" + member + ")", Archive: archive, Member: member, Raw: line})
			return
		}
		if looksLikeFile(member) {
			emit(Record{Kind: classify(member), Path: member, Raw: line})
		}
		return
	}
	if looksLikeFile(source) {
		emit(Record{Kind: classify(source), Path: source, Raw: line})
	}
}

// recordKey identifies a record for deduplication. It is a struct rather than
// the three fields joined into one string because the join allocated on every
// emitted record, and a map file emits the same member once per contributed
// section: on a large map that is one throwaway string per placement line.
type recordKey struct {
	kind    Kind
	archive string
	path    string
}

// recorder appends a record unless an identical one was already seen. Map
// files name the same archive member once per contributed section, so without
// deduplication a single member appears a dozen times.
func newRecorder(result *Result) func(Record) {
	seen := map[recordKey]bool{}
	return func(record Record) {
		if record.Path == "" {
			return
		}
		key := recordKey{kind: record.Kind, archive: record.Archive, path: record.Path}
		if seen[key] {
			return
		}
		// A record that is kept outlives the line it was read from, and that
		// line borrows a buffer the scanner overwrites (see lineOf), so the
		// strings taken from it are copied here. Only records that survive
		// deduplication are copied, which on a map is a few thousand of the
		// millions of lines read.
		record.Path = strings.Clone(record.Path)
		record.Archive = strings.Clone(record.Archive)
		record.Member = strings.Clone(record.Member)
		record.Raw = strings.Clone(record.Raw)
		key.archive, key.path = record.Archive, record.Path
		seen[key] = true
		result.Records = append(result.Records, record)
	}
}

func parseGNULine(line string, section gnuSection, emit func(Record)) gnuSection {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return section
	}
	// An unindented line is either a known header or the start of something
	// this parser does not model; either way the previous section ends.
	if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
		if next, known := gnuHeaders[trimmed]; known {
			return next
		}
		if section == gnuMemoryMap {
			// LOAD lines are the direct inputs the linker opened. Not every
			// one names a file: GNU ld writes "LOAD linker stubs" for the
			// veneers it synthesizes for ARM.
			if path, found := strings.CutPrefix(trimmed, "LOAD "); found {
				path = strings.TrimSpace(path)
				if looksLikeFile(path) {
					emit(Record{Kind: classify(path), Path: path, Raw: line})
				}
				return section
			}
			// Output section names and addresses live here too; ignore them.
			return section
		}
		if section == gnuArchiveMembers || section == gnuAsNeeded {
			return parseGNUInclusionLine(line, trimmed, section, emit)
		}
		return gnuOther
	}

	switch section {
	case gnuArchiveMembers, gnuAsNeeded:
		return parseGNUInclusionLine(line, trimmed, section, emit)
	case gnuMemoryMap:
		// " .text   0x0000000000001129   0x17   CMakeFiles/app.dir/main.c.o"
		// A placement line names the object that contributed the section. It
		// is not a LOAD line, which only says the linker opened the file, and
		// not a wildcard pattern from the linker script.
		if name, size, path, ok := placementLine(trimmed); ok {
			if contributesToImage(name, size) {
				emit(Record{Kind: RetainedSection, Path: path, Member: name, Raw: line})
			}
		}
	case gnuDiscarded:
		// " .text.foo   0x0   0x2a   path/to/file.o"
		if candidate, count := lastField(trimmed); count > 0 && looksLikeFile(candidate) {
			emit(Record{Kind: DiscardedSection, Path: candidate, Raw: line})
		}
	}
	return section
}

// placementLine recognizes an input-section placement in the memory map:
// a section name, a hexadecimal address, a hexadecimal size, and the file that
// contributed it. Symbol lines carry two fields, fill lines do not start with
// a section name, and linker-script wildcards start with an asterisk.
func placementLine(trimmed string) (string, uint64, string, bool) {
	name, offset, ok := nextField(trimmed, 0)
	if !ok || !strings.HasPrefix(name, ".") {
		return "", 0, "", false
	}
	address, offset, ok := nextField(trimmed, offset)
	if !ok || !strings.HasPrefix(address, "0x") {
		return "", 0, "", false
	}
	sizeField, offset, ok := nextField(trimmed, offset)
	if !ok || !strings.HasPrefix(sizeField, "0x") {
		return "", 0, "", false
	}
	path, _, ok := nextField(trimmed, offset)
	if !ok || !looksLikeFile(path) {
		return "", 0, "", false
	}
	size, err := strconv.ParseUint(strings.TrimPrefix(sizeField, "0x"), 16, 64)
	if err != nil {
		return "", 0, "", false
	}
	return name, size, path, true
}

// nonImageSections are the section families that never occupy memory in the
// linked image: debug information, tool comments and attribute blobs. They are
// retained by the linker whatever it discards, so counting them as a
// contribution would mean that a build with -g never has a fully discarded
// object at all.
var nonImageSections = []string{".debug", ".comment", ".stab", ".symtab", ".strtab", ".shstrtab", ".gnu.build.attributes"}

// contributesToImage reports whether a retained input section puts bytes into
// the deliverable. A zero-length section does not, whatever its name.
func contributesToImage(name string, size uint64) bool {
	if size == 0 {
		return false
	}
	for _, prefix := range nonImageSections {
		// Prefix, not exact or dot-separated: the families spell their members
		// ".debug_info", ".debug_line_str", ".stabstr".
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return !strings.HasSuffix(name, ".attributes")
}

// parseGNUInclusionLine reads one entry of the archive-member or as-needed
// block: the included file, then the file and symbol that required it.
func parseGNUInclusionLine(line, trimmed string, section gnuSection, emit func(Record)) gnuSection {
	included, _, ok := nextField(trimmed, 0)
	if !ok {
		return section
	}
	if archive, member, ok := splitArchiveMember(included); ok {
		emit(Record{Kind: StaticArchive, Path: archive, Raw: line})
		emit(Record{Kind: ArchiveMember, Path: archive + "(" + member + ")", Archive: archive, Member: member, Raw: line})
		return section
	}
	if !looksLikeFile(included) {
		// Not an entry of this block after all; the block has ended.
		return gnuOther
	}
	kind := SharedLibrary
	if section == gnuArchiveMembers {
		kind = classify(included)
	}
	emit(Record{Kind: kind, Path: included, Raw: line})
	return section
}

// parseLLDLine reads lld's column layout. Only the "In" column names input
// files; the "Out" column names output sections and the symbol column names
// symbols, neither of which is evidence about inputs.
func parseLLDLine(line string, emit func(Record)) {
	for offset := 0; ; {
		token, next, ok := nextField(line, offset)
		if !ok {
			break
		}
		offset = next
		match := lldInputColumn.FindStringSubmatch(token)
		if match == nil {
			continue
		}
		spec := match[1]
		if spec == "" || strings.HasPrefix(spec, "<") {
			// "<internal>" is lld's own synthetic input.
			continue
		}
		if archive, member, ok := splitArchiveMember(spec); ok {
			emit(Record{Kind: StaticArchive, Path: archive, Raw: line})
			emit(Record{Kind: ArchiveMember, Path: archive + "(" + member + ")", Archive: archive, Member: member, Raw: line})
			continue
		}
		if looksLikeFile(spec) {
			emit(Record{Kind: classify(spec), Path: spec, Raw: line})
		}
	}
}

// parseGenericLine is the fallback for the parked IAR format. It is
// deliberately conservative: it records only tokens that are unambiguously
// file paths with a known extension.
func parseGenericLine(line string, emit func(Record)) {
	for offset := 0; ; {
		token, next, ok := nextField(line, offset)
		if !ok {
			break
		}
		offset = next
		token = strings.Trim(token, "[],;")
		if archive, member, ok := splitArchiveMember(token); ok && looksLikeFile(archive) {
			emit(Record{Kind: StaticArchive, Path: archive, Raw: line})
			emit(Record{Kind: ArchiveMember, Path: token, Archive: archive, Member: member, Raw: line})
			continue
		}
		if looksLikeFile(token) {
			emit(Record{Kind: classify(token), Path: token, Raw: line})
		}
	}
}

// A map is read line by line and every line is looked at, so what one line
// costs is multiplied by millions: the generated 200 MB map of section 31 has
// about 2.6 million of them. strings.Fields is the natural way to reach the
// fields of a line and is what this parser used, but it allocates a slice for
// every line, whatever the parser then does with it -- and most lines are
// symbol lines the parser discards after looking at one field. The helpers
// below hand out the same fields without that slice, by walking the line in
// place. They split where strings.Fields splits: a field is a run of
// characters between runs of unicode.IsSpace, so which fields a line has is
// unchanged, and so is every record built from them.

// nextField returns the field that starts at or after offset, and the offset
// to continue from. ok is false once the line holds no further field.
func nextField(line string, offset int) (field string, next int, ok bool) {
	index := offset
	for index < len(line) {
		space, size := spaceAt(line, index)
		if !space {
			break
		}
		index += size
	}
	if index >= len(line) {
		return "", len(line), false
	}
	start := index
	for index < len(line) {
		space, size := spaceAt(line, index)
		if space {
			break
		}
		index += size
	}
	return line[start:index], index, true
}

// lastField returns the final field of a line and how many fields it has. The
// count comes back with it because a caller that wants the last field also
// wants to know whether the line was wide enough to be the line it is looking
// for, and one walk answers both.
func lastField(line string) (string, int) {
	var last string
	var count int
	for offset := 0; ; count++ {
		field, next, ok := nextField(line, offset)
		if !ok {
			return last, count
		}
		last, offset = field, next
	}
}

// fieldCountExceeds reports whether a line has more than limit fields. It
// stops as soon as it knows, so the pathological line the limit exists for
// costs no more than the limit allows.
func fieldCountExceeds(line string, limit int) bool {
	count := 0
	for offset := 0; ; {
		_, next, ok := nextField(line, offset)
		if !ok {
			return false
		}
		count++
		if count > limit {
			return true
		}
		offset = next
	}
}

// asciiSpace is the lookup strings.Fields uses for the byte range a map file
// is almost entirely made of. Reading it is one indexed load where
// unicode.IsSpace is a chain of comparisons, and this runs per byte of a file
// that can be 200 MB.
var asciiSpace = [utf8.RuneSelf]bool{'\t': true, '\n': true, '\v': true, '\f': true, '\r': true, ' ': true}

// spaceAt reports whether the character at index is white space, and how many
// bytes it occupies.
func spaceAt(line string, index int) (bool, int) {
	if c := line[index]; c < utf8.RuneSelf {
		return asciiSpace[c], 1
	}
	r, size := utf8.DecodeRuneInString(line[index:])
	return unicode.IsSpace(r), size
}

// fileExtensions are the suffixes a linker input can carry. A wildcard such as
// *(.gnu.attributes) has none of them and is therefore never mistaken for one.
var fileExtensions = []string{".a", ".lib", ".so", ".dll", ".o", ".obj", ".elf", ".exe", ".ld", ".lds"}

func looksLikeFile(token string) bool {
	if token == "" || strings.ContainsAny(token, "*?") {
		return false
	}
	for _, extension := range fileExtensions {
		if strings.HasSuffix(token, extension) {
			return true
		}
		// Versioned shared objects such as libc.so.6.
		if index := strings.Index(token, extension+"."); index > 0 {
			return true
		}
	}
	return false
}

func classify(path string) Kind {
	switch {
	case strings.HasSuffix(path, ".a"), strings.HasSuffix(path, ".lib"):
		return StaticArchive
	case strings.HasSuffix(path, ".ld"), strings.HasSuffix(path, ".lds"):
		return LinkerScript
	case strings.HasSuffix(path, ".so"), strings.HasSuffix(path, ".dll"), strings.Contains(path, ".so."):
		return SharedLibrary
	default:
		return LinkedObject
	}
}

func splitArchiveMember(token string) (string, string, bool) {
	if !strings.HasSuffix(token, ")") {
		return "", "", false
	}
	open := strings.LastIndexByte(token, '(')
	if open <= 0 {
		return "", "", false
	}
	archive := token[:open]
	member := strings.TrimSuffix(token[open+1:], ")")
	// An archive member is a real object inside a real archive; a linker
	// script wildcard such as *crtbegin.o(.ctors) is neither.
	if !looksLikeFile(archive) || !looksLikeFile(member) {
		return "", "", false
	}
	return archive, member, true
}
