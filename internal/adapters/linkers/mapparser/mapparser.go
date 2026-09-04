// Package mapparser contains the bounded, format-neutral parts of linker map parsing.
package mapparser

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	MaxLineLength = 1 << 20
	MaxInputSize  = 2 << 30
	MaxTokensLine = 100000
)

type Kind string

const (
	LinkedObject     Kind = "object"
	StaticArchive    Kind = "archive"
	ArchiveMember    Kind = "archive-member"
	SharedLibrary    Kind = "shared-library"
	DiscardedSection Kind = "discarded-section"
	LinkerScript     Kind = "linker-script"
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

func Sniff(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Archive member included to satisfy reference by file"):
			return "gnu-gold"
		case strings.HasPrefix(trimmed, "Archive member included") || strings.HasPrefix(trimmed, "Memory Configuration"):
			return "gnu-ld"
		case strings.Contains(trimmed, "Preferred load address is"), strings.Contains(line, " Address         Publics by Value"):
			return "msvc"
		case strings.HasPrefix(trimmed, "VMA") && strings.Contains(trimmed, "LMA") && strings.Contains(trimmed, "Out") && strings.Contains(trimmed, "In") && strings.Contains(trimmed, "Symbol"):
			return "lld"
		case strings.Contains(trimmed, "IAR ELF Linker") || (strings.Contains(text, "MODULE SUMMARY") && strings.HasPrefix(trimmed, "***")):
			return "iar"
		}
	}
	return ""
}

var ErrMalformed = errors.New("malformed link evidence")
var ErrInputLimitExceeded = errors.New("input limit exceeded")

func Parse(r io.Reader, format string) Result {
	result := Result{Format: format}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), MaxLineLength)
	inDiscarded := false
	var allocated int
	for scanner.Scan() {
		line := scanner.Text()
		allocated += len(line)
		if allocated > MaxInputSize {
			result.Err = fmt.Errorf("map input exceeds %d bytes: %w", MaxInputSize, ErrInputLimitExceeded)
			break
		}
		trimmed := strings.TrimSpace(line)
		if len(strings.Fields(line)) > MaxTokensLine {
			result.Err = fmt.Errorf("map line exceeds %d tokens: %w", MaxTokensLine, ErrInputLimitExceeded)
			break
		}
		if strings.HasPrefix(trimmed, "Discarded input sections") || strings.HasPrefix(trimmed, "Discarded sections") {
			inDiscarded = true
			continue
		}
		if inDiscarded && trimmed != "" {
			if record, ok := discardedRecord(line); ok {
				result.Records = append(result.Records, record)
				continue
			}
			inDiscarded = false
		}
		for _, token := range paths(line) {
			if archive, member, ok := splitArchiveMember(token); ok {
				result.Records = append(result.Records,
					Record{Kind: StaticArchive, Path: archive, Raw: line},
					Record{Kind: ArchiveMember, Path: member, Archive: archive, Member: member, Raw: line})
			} else {
				result.Records = append(result.Records, Record{Kind: classify(token), Path: token, Raw: line})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "token too long") {
			result.Err = fmt.Errorf("read linker map: %w: %v", ErrInputLimitExceeded, err)
		} else {
			result.Err = fmt.Errorf("read linker map: %w: %v", ErrMalformed, err)
		}
	}
	if result.Err == nil && len(result.Records) == 0 {
		result.Err = ErrMalformed
	}
	return result
}

func paths(line string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, token := range strings.Fields(line) {
		token = strings.Trim(token, "[],:;")
		if !hasFileExtension(token) || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

func hasFileExtension(token string) bool {
	for _, extension := range []string{".a", ".so", ".o", ".obj", ".lib", ".dll", ".elf", ".ld", ".lds"} {
		if strings.Contains(token, extension) {
			return true
		}
	}
	return false
}

func classify(path string) Kind {
	switch {
	case strings.HasSuffix(path, ".a"), strings.HasSuffix(path, ".lib"):
		return StaticArchive
	case strings.HasSuffix(path, ".so"), strings.HasSuffix(path, ".dll"):
		return SharedLibrary
	default:
		return LinkedObject
	}
}

func discardedRecord(line string) (Record, bool) {
	for _, path := range paths(line) {
		return Record{Kind: DiscardedSection, Path: path, Raw: line}, true
	}
	return Record{}, false
}

func splitArchiveMember(token string) (string, string, bool) {
	open := strings.LastIndexByte(token, '(')
	if open <= 0 || !strings.HasSuffix(token, ")") {
		return "", "", false
	}
	return token[:open], strings.TrimSuffix(token[open+1:], ")"), true
}
