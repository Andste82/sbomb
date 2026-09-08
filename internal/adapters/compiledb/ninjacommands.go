package compiledb

import (
	"fmt"
	"io"
	"strings"
)

// maxNinjaCommandsSize bounds the output `ninja -t commands` may produce. It
// is the same bound response file expansion uses, for the same reason: the
// text comes from a build tree sbomb does not own.
const maxNinjaCommandsSize = 64 << 20

// shellOperators are the tokens that make a printed command line a composite
// rather than one argv. Ninja hands such a line to a shell, so its parts do
// not necessarily run in the build directory, and nothing here could say
// where they do.
var shellOperators = map[string]bool{"&&": true, "||": true, ";": true, "|": true}

// ParseNinjaCommands reads the command lines `ninja -t commands <target>`
// prints and returns the compilations among them. It is the fallback of
// section 9.2 for a build directory without compile_commands.json.
//
// Unlike the compile database, the output carries no directory and no file
// field: it is the command line and nothing else. Directory is therefore the
// build directory, because that is where ninja runs its commands -- a
// statement of the build system rather than a derivation -- and the
// translation unit is taken only from an explicit "-c <path>". CMake's Ninja
// generator writes the source there; a line that has no such argument is
// skipped rather than searched for something source-shaped, because naming
// the wrong file is worse than naming none.
func ParseNinjaCommands(r io.Reader, directory string) ([]Command, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxNinjaCommandsSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxNinjaCommandsSize {
		return nil, fmt.Errorf("ninja commands output exceeds %d bytes", maxNinjaCommandsSize)
	}
	commands := make([]Command, 0)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		args, splitErr := shellSplit(line)
		if splitErr != nil || len(args) < 2 {
			// One command line that cannot be read is a gap in the evidence,
			// not a reason to discard the rest of the target's commands.
			continue
		}
		if hasShellOperator(args) {
			continue
		}
		source := compiledSource(args)
		if source == "" {
			continue
		}
		expanded, responseFiles, expandErr := expandResponseFiles(args, directory)
		if expandErr != nil {
			continue
		}
		// The source is taken from the unexpanded argv on purpose: a response
		// file holds flags, and a "-c" inside one would name a translation
		// unit no compile line stated.
		commands = append(commands, Command{
			Directory:     directory,
			File:          resolvePath(directory, source),
			Output:        resolvePath(directory, outputPath("", expanded)),
			Arguments:     expanded,
			ResponseFiles: responseFiles,
		})
	}
	return commands, nil
}

func hasShellOperator(args []string) bool {
	for _, arg := range args {
		if shellOperators[arg] {
			return true
		}
	}
	return false
}

// compiledSource returns the translation unit an explicit "-c" names, or "".
func compiledSource(args []string) string {
	for index, arg := range args {
		if arg != "-c" || index+1 >= len(args) {
			continue
		}
		candidate := args[index+1]
		if candidate == "" || strings.HasPrefix(candidate, "-") || strings.HasPrefix(candidate, "@") {
			return ""
		}
		return candidate
	}
	return ""
}
