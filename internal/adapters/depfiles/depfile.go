package depfiles

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const MaxLineLength = 1 << 20

var ErrInputLimitExceeded = errors.New("input limit exceeded")

// Rule represents a Make-style depfile rule.
type Rule struct {
	Targets []string
	Prereqs []string
}

// Parse parses a Make-format depfile, including escaped spaces, line continuations,
// and Ninja's $: form used for Windows paths like C$:/src.
func Parse(text string) ([]Rule, error) {
	if err := validateInput(text); err != nil {
		return nil, err
	}
	lines := splitLogicalLines(text)
	out := make([]Rule, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := findRuleSeparator(line)
		if idx < 0 {
			continue
		}
		targets := splitTokens(line[:idx])
		prereqs := splitTokens(line[idx+1:])
		if len(targets) == 0 {
			continue
		}
		out = append(out, Rule{
			Targets: uniqueStrings(targets),
			Prereqs: uniqueStrings(prereqs),
		})
	}
	return out, nil
}

func validateInput(text string) error {
	lineLength := 0
	for index := 0; index < len(text); index++ {
		if text[index] == '\\' && index+1 < len(text) && (text[index+1] == '\n' || text[index+1] == '\r') {
			lineLength++
			if lineLength > MaxLineLength {
				return fmt.Errorf("depfile line exceeds %d bytes: %w", MaxLineLength, ErrInputLimitExceeded)
			}
			continue
		}
		if text[index] == '\n' || text[index] == '\r' {
			lineLength = 0
			continue
		}
		lineLength++
		if lineLength > MaxLineLength {
			return fmt.Errorf("depfile line exceeds %d bytes: %w", MaxLineLength, ErrInputLimitExceeded)
		}
	}
	return nil
}

func splitLogicalLines(text string) []string {
	var out []string
	var line strings.Builder
	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch ch {
		case '\\':
			if i+1 < len(text) && (text[i+1] == '\n' || text[i+1] == '\r') {
				if text[i+1] == '\r' && i+2 < len(text) && text[i+2] == '\n' {
					i += 2
				} else {
					i++
				}
				continue
			}
			line.WriteByte(ch)
		case '\n':
			out = append(out, line.String())
			line.Reset()
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			out = append(out, line.String())
			line.Reset()
		default:
			line.WriteByte(ch)
		}
	}
	if line.Len() > 0 || len(out) == 0 {
		out = append(out, line.String())
	}
	return out
}

func findRuleSeparator(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ':' {
			continue
		}
		if isDriveColon(line, i) {
			continue
		}
		return i
	}
	return -1
}

func isDriveColon(line string, idx int) bool {
	if idx == 0 || idx+1 >= len(line) {
		return false
	}
	if !isAlpha(rune(line[idx-1])) {
		return false
	}
	if line[idx+1] != '/' && line[idx+1] != '\\' {
		return false
	}
	return true
}

func splitTokens(s string) []string {
	var tokens []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case ' ', '\t', '#', ':', '\\':
				cur.WriteByte(next)
				i++
				continue
			default:
				cur.WriteByte('\\')
				continue
			}
		}
		if ch == '$' && i+1 < len(s) {
			switch s[i+1] {
			case '$':
				cur.WriteByte('$')
				i++
				continue
			case ':':
				cur.WriteByte(':')
				i++
				continue
			}
		}
		if unicode.IsSpace(rune(ch)) {
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
			continue
		}
		if ch == '#' && cur.Len() == 0 {
			break
		}
		cur.WriteByte(ch)
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func isAlpha(r rune) bool {
	return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
}
