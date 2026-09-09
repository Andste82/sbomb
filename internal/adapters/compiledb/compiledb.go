// Package compiledb parses compile_commands.json files.
package compiledb

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/respfile"
	"unicode"
)

const (
	maxResponseDepth = 8
	maxExpandedSize  = 64 << 20
)

// Command is one compile_commands.json entry with its command line expanded.
// Paths are absolute when the entry provides a relative path and Directory is available.
type Command struct {
	Directory     string
	File          string
	Output        string
	Arguments     []string
	ResponseFiles []string
}

type rawCommand struct {
	Directory string   `json:"directory"`
	File      string   `json:"file"`
	Command   string   `json:"command"`
	Arguments []string `json:"arguments"`
	Output    string   `json:"output"`
}

// ParseFile reads and parses a compile_commands.json file.
func ParseFile(path string) ([]Command, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse parses compile_commands.json data. Entries without an output remain
// valid because some generators omit it; callers can match them by directory and file.
func Parse(data []byte) ([]Command, error) {
	var raw []rawCommand
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse compile_commands.json: %w", err)
	}

	commands := make([]Command, 0, len(raw))
	for index, entry := range raw {
		if entry.File == "" {
			return nil, fmt.Errorf("compile command %d has no file", index)
		}
		args, err := commandArguments(entry)
		if err != nil {
			return nil, fmt.Errorf("compile command %d: %w", index, err)
		}
		directory := entry.Directory
		if directory != "" {
			directory, err = filepath.Abs(directory)
			if err != nil {
				return nil, fmt.Errorf("compile command %d directory: %w", index, err)
			}
		}
		args, responseFiles, err := expandResponseFiles(args, directory)
		if err != nil {
			return nil, fmt.Errorf("compile command %d: %w", index, err)
		}
		commands = append(commands, Command{
			Directory:     directory,
			File:          resolvePath(directory, entry.File),
			Output:        resolvePath(directory, outputPath(entry.Output, args)),
			Arguments:     args,
			ResponseFiles: responseFiles,
		})
	}
	return commands, nil
}

func commandArguments(entry rawCommand) ([]string, error) {
	var args []string
	if len(entry.Arguments) > 0 {
		args = append([]string(nil), entry.Arguments...)
	} else {
		if entry.Command == "" {
			return nil, errors.New("has neither arguments nor command")
		}
		split, err := shellSplit(entry.Command)
		if err != nil {
			return nil, err
		}
		args = split
	}
	return args, nil
}

func outputPath(explicit string, args []string) string {
	if explicit != "" {
		return explicit
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-o" || arg == "/Fo":
			if index+1 < len(args) {
				return args[index+1]
			}
		case strings.HasPrefix(arg, "-o") && len(arg) > 2:
			return strings.TrimPrefix(arg, "-o")
		case strings.HasPrefix(arg, "/Fo") && len(arg) > 3:
			return strings.TrimPrefix(arg, "/Fo")
		}
	}
	return ""
}

func resolvePath(directory, path string) string {
	if path == "" {
		return ""
	}
	if pathmodel.IsAbsolute(path) || directory == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(directory, path))
}

func expandResponseFiles(args []string, directory string) ([]string, []string, error) {
	var compiler string
	if len(args) > 0 {
		compiler = args[0]
	}
	quoting := respfile.QuotingForCompiler(compiler)
	var files []string
	var size int
	var expand func([]string, string, int) ([]string, error)
	expand = func(current []string, currentDirectory string, depth int) ([]string, error) {
		if depth > maxResponseDepth {
			return nil, errors.New("response file expansion exceeded depth limit")
		}
		out := make([]string, 0, len(current))
		for _, arg := range current {
			if len(arg) < 2 || arg[0] != '@' || arg == "@@" {
				out = append(out, arg)
				size += len(arg)
				if size > maxExpandedSize {
					return nil, errors.New("response file expansion exceeded size limit")
				}
				continue
			}
			path := resolvePath(currentDirectory, arg[1:])
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read response file %q: %w", path, err)
			}
			size += len(data)
			if size > maxExpandedSize {
				return nil, errors.New("response file expansion exceeded size limit")
			}
			files = append(files, path)
			// Section 9.3 ties the quoting rules to the detected toolchain,
			// not to the host: under GNU rules every backslash in a Windows
			// path would be read as an escape and the path would fall apart.
			nested := respfile.Tokenize(string(data), quoting)
			expanded, err := expand(nested, filepath.Dir(path), depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, expanded...)
		}
		return out, nil
	}
	expanded, err := expand(args, directory, 0)
	return expanded, files, err
}

func shellSplit(input string) ([]string, error) {
	var args []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}
	for _, char := range input {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
		} else if unicode.IsSpace(char) {
			flush()
		} else {
			current.WriteRune(char)
		}
	}
	if escaped {
		current.WriteByte('\\')
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	flush()
	return args, nil
}
