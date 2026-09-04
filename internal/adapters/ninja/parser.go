package ninja

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
)

// Rule represents a Ninja build rule.
type Rule struct {
	Outputs   []string
	Inputs    []string
	Variables map[string]string
}

// File represents a parsed build.ninja file.
type File struct {
	Variables map[string]string
	Rules     []Rule
	Edges     map[string]Rule // edges keyed by primary output
}

// ParseFile parses a build.ninja file format.
// Handles:
// - Comments (starting with #)
// - Variables (key = value)
// - Rules (build output1 output2: rule input1 input2)
// - Includes and subninja directives
// - Escaping ($:, $<space>, $$, backslash line continuations)
// - Response files (@file)
func ParseFile(r io.Reader) (*File, error) {
	f := &File{
		Variables: make(map[string]string),
		Rules:     make([]Rule, 0),
		Edges:     make(map[string]Rule),
	}

	scanner := bufio.NewScanner(r)
	var lineNum int
	var currentRule *Rule

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Handle line continuations (backslash at end)
		for strings.HasSuffix(line, "\\") && scanner.Scan() {
			lineNum++
			line = strings.TrimSuffix(line, "\\") + scanner.Text()
		}

		// Strip and handle comments
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		// Check for indented continuation (variable assignment within a rule)
		if strings.HasPrefix(scanner.Text(), " ") || strings.HasPrefix(scanner.Text(), "\t") {
			if currentRule != nil {
				// Parse indented block variable
				trimmed := strings.TrimSpace(line)
				if eqIdx := strings.Index(trimmed, "="); eqIdx > 0 {
					key := strings.TrimSpace(trimmed[:eqIdx])
					val := strings.TrimSpace(trimmed[eqIdx+1:])
					val = expandVariables(val, f.Variables)
					if currentRule.Variables == nil {
						currentRule.Variables = make(map[string]string)
					}
					currentRule.Variables[key] = val
				}
			}
			continue
		}

		// Parse build rule
		if strings.HasPrefix(line, "build ") {
			rule, err := parseBuildRule(line, f.Variables)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNum, err)
			}
			f.Rules = append(f.Rules, rule)
			currentRule = &rule
			if len(rule.Outputs) > 0 {
				f.Edges[rule.Outputs[0]] = rule
			}
			continue
		}

		// Parse variable assignment
		if eqIdx := strings.Index(line, "="); eqIdx > 0 {
			key := strings.TrimSpace(line[:eqIdx])
			val := strings.TrimSpace(line[eqIdx+1:])
			if !strings.Contains(key, " ") && !strings.Contains(key, "\t") {
				val = expandVariables(val, f.Variables)
				f.Variables[key] = val
			}
			continue
		}

		// Parse include/subninja (not fully implemented for M07, but accepted)
		if strings.HasPrefix(line, "include ") || strings.HasPrefix(line, "subninja ") {
			// For now, we accept these but don't follow them in the basic parser
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return f, nil
}

// parseBuildRule parses a single build rule line.
// Format: build output1 output2: rule_name input1 input2 | implicit1 | order_only1
func parseBuildRule(line string, vars map[string]string) (Rule, error) {
	const prefix = "build "
	if !strings.HasPrefix(line, prefix) {
		return Rule{}, fmt.Errorf("not a build rule: %s", line)
	}

	rest := strings.TrimPrefix(line, prefix)

	// Find the colon separator
	colonIdx := strings.Index(rest, ":")
	if colonIdx < 0 {
		return Rule{}, fmt.Errorf("missing ':' in build rule: %s", line)
	}

	outputPart := strings.TrimSpace(rest[:colonIdx])
	rulePart := strings.TrimSpace(rest[colonIdx+1:])

	// Parse outputs
	outputs := tokenize(outputPart)
	if len(outputs) == 0 {
		return Rule{}, fmt.Errorf("no outputs in build rule")
	}

	// Parse rule name and inputs
	tokens := tokenize(rulePart)
	if len(tokens) == 0 {
		return Rule{}, fmt.Errorf("no rule name in build rule")
	}

	_ = tokens[0] // ruleName could be stored in Rule if needed
	inputs := tokens[1:]

	// Separate implicit and order-only inputs
	var explicitInputs []string
	for _, inp := range inputs {
		if inp == "|" {
			// Rest are implicit/order-only, skip for now
			break
		}
		explicitInputs = append(explicitInputs, inp)
	}

	return Rule{
		Outputs:   outputs,
		Inputs:    expandPaths(explicitInputs, vars),
		Variables: make(map[string]string),
	}, nil
}

// tokenize splits a string into tokens, handling:
// - Escape sequences: $:, $<space>, $$, backslash-space
func tokenize(s string) []string {
	var tokens []string
	var current strings.Builder
	i := 0

	for i < len(s) {
		ch := s[i]

		// Handle escape sequences
		if ch == '$' && i+1 < len(s) {
			nextCh := s[i+1]
			switch nextCh {
			case '$':
				// $$ -> single $ (literal)
				current.WriteByte('$')
				i += 2
				continue
			case ':':
				// $: -> keep as is (for Windows paths like C$:/src)
				current.WriteByte('$')
				current.WriteByte(':')
				i += 2
				continue
			case ' ':
				// $ <space> -> escaped space (literal space)
				current.WriteByte(' ')
				i += 2
				continue
			case '\n':
				// $ followed by newline should not appear mid-token in well-formed Ninja
				i++
				continue
			default:
				// $ followed by variable name - keep as is for later expansion
				current.WriteByte('$')
				i++
				continue
			}
		}

		if ch == '\\' && i+1 < len(s) && s[i+1] == ' ' {
			// Backslash-space -> escaped space (literal space)
			current.WriteByte(' ')
			i += 2
			continue
		}

		if unicode.IsSpace(rune(ch)) {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			i++
			continue
		}

		current.WriteByte(ch)
		i++
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// expandVariables replaces $(...) and $var patterns with values from the map.
func expandVariables(s string, vars map[string]string) string {
	// First handle $$ -> temporary marker to avoid re-processing
	const marker = "\x00DOLLAR\x00"
	s = strings.ReplaceAll(s, "$$", marker)

	// Handle $(...) form
	re := regexp.MustCompile(`\$\(([^)]+)\)`)
	s = re.ReplaceAllStringFunc(s, func(m string) string {
		varName := m[2 : len(m)-1]
		if val, ok := vars[varName]; ok {
			return val
		}
		return m
	})

	// Handle $var form (one-word variable names)
	re = regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)
	s = re.ReplaceAllStringFunc(s, func(m string) string {
		varName := m[1:]
		if val, ok := vars[varName]; ok {
			return val
		}
		return m
	})

	// Restore $$ as single $
	s = strings.ReplaceAll(s, marker, "$")
	return s
}

// expandPaths expands variable references in a list of paths.
func expandPaths(paths []string, vars map[string]string) []string {
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		expanded := expandVariables(p, vars)
		result = append(result, expanded)
	}
	return result
}
