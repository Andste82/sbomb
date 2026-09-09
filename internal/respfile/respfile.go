// Package respfile expands the response files that compile and link command
// lines hide their arguments behind (specification section 9.3). Windows
// toolchains use them for almost every link, and large POSIX links use them
// once the command line outgrows ARG_MAX, so a parser that stops at "@file"
// simply does not see what was linked.
package respfile

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/pathmodel"
)

// The bounds of section 9.3.
const (
	MaxDepth = 8
	MaxBytes = 64 << 20
)

var (
	// ErrDepthExceeded reports a response file chain deeper than the limit,
	// which is either a cycle or something not worth following.
	ErrDepthExceeded = errors.New("response file nesting exceeded the depth limit")
	// ErrSizeExceeded reports that the expansion outgrew its byte budget.
	ErrSizeExceeded = errors.New("response file expansion exceeded the size limit")
)

// Quoting selects the tokenizer. The two families disagree about what a
// backslash means, which matters because Windows paths are full of them.
type Quoting string

const (
	// GNU is gcc, clang and GNU ld: a backslash escapes the next character
	// anywhere, and both quote characters group.
	GNU Quoting = "gnu"
	// MSVC is cl.exe and link.exe: a backslash is literal unless it precedes
	// a double quote, so "C:\dir\" is a path, not an escape.
	MSVC Quoting = "msvc"
)

// Options describes how to expand.
type Options struct {
	// Dir resolves a relative response-file path, as the compiler would.
	Dir string
	// Quoting selects the tokenizer; GNU when unset.
	Quoting Quoting
	// MaxDepth and MaxBytes override the defaults of section 9.3.
	MaxDepth int
	MaxBytes int64
	// ReadFile is overridable for tests.
	ReadFile func(string) ([]byte, error)
}

// QuotingForCompiler picks the tokenizer from the compiler the build evidence
// named. Section 9.3 ties the choice to the detected toolchain rather than to
// the host, because a cross build on Linux may still drive a MSVC-style tool.
func QuotingForCompiler(compiler string) Quoting {
	// The compiler path comes from the build evidence, which may be a Windows
	// path being read on a POSIX host, so the separator cannot be assumed.
	normalized := strings.ReplaceAll(compiler, "\\", "/")
	base := strings.ToLower(path.Base(normalized))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "cl", "link", "lib", "rc", "clang-cl", "lld-link":
		return MSVC
	}
	return GNU
}

// Expand replaces every @file argument with the arguments it contains,
// recursively. Arguments that are not response-file references are returned
// unchanged and in order, so the result can be parsed exactly like a command
// line that never used one.
func Expand(args []string, options Options) ([]string, error) {
	if options.MaxDepth <= 0 {
		options.MaxDepth = MaxDepth
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = MaxBytes
	}
	if options.ReadFile == nil {
		options.ReadFile = os.ReadFile
	}
	if options.Quoting == "" {
		options.Quoting = GNU
	}
	var budget int64
	return expand(args, options, 0, &budget)
}

func expand(args []string, options Options, depth int, budget *int64) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		reference, isReference := strings.CutPrefix(arg, "@")
		if !isReference || reference == "" {
			out = append(out, arg)
			continue
		}
		if depth+1 > options.MaxDepth {
			return out, fmt.Errorf("%w: %s", ErrDepthExceeded, arg)
		}
		path := reference
		if !pathmodel.IsAbsolute(path) && options.Dir != "" {
			path = filepath.Join(options.Dir, path)
		}
		data, err := options.ReadFile(path)
		if err != nil {
			// A response file that cannot be read is not evidence of
			// anything; the reference is kept so the caller can report it.
			out = append(out, arg)
			continue
		}
		*budget += int64(len(data))
		if *budget > options.MaxBytes {
			return out, fmt.Errorf("%w: %s", ErrSizeExceeded, path)
		}
		nested, err := expand(Tokenize(string(data), options.Quoting), options, depth+1, budget)
		out = append(out, nested...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// Tokenize splits response-file content into arguments.
func Tokenize(content string, quoting Quoting) []string {
	if quoting == MSVC {
		return tokenizeMSVC(content)
	}
	return tokenizeGNU(content)
}

// tokenizeGNU implements the GNU rules: whitespace separates, a backslash
// escapes the following character anywhere, and either quote character groups
// until its match.
func tokenizeGNU(content string) []string {
	var args []string
	var current strings.Builder
	var quote byte
	var started bool
	var escaped bool

	flush := func() {
		if started {
			args = append(args, current.String())
			current.Reset()
			started = false
		}
	}
	// Bytes, not runes. A path in a build tree is an arbitrary byte string --
	// a Latin-1 filename, a Windows path in the local code page -- and
	// decoding it as UTF-8 would replace what it cannot decode with U+FFFD,
	// turning the file's name into one that matches nothing. Every character
	// this tokenizer acts on is ASCII, so bytes are also all it needs.
	for index := 0; index < len(content); index++ {
		c := content[index]
		switch {
		case escaped:
			current.WriteByte(c)
			started = true
			escaped = false
		case c == '\\':
			escaped = true
			started = true
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				current.WriteByte(c)
			}
		case c == '"' || c == '\'':
			quote = c
			started = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		default:
			current.WriteByte(c)
			started = true
		}
	}
	flush()
	return args
}

// tokenizeMSVC implements the Microsoft rules: a backslash is an ordinary
// character unless it precedes a double quote, and a run of backslashes before
// a quote halves. Without this, every Windows path in a response file loses
// its separators.
func tokenizeMSVC(content string) []string {
	var args []string
	var current strings.Builder
	var inQuotes bool
	var started bool
	var backslashes int

	flushBackslashes := func(half bool) {
		count := backslashes
		if half {
			count /= 2
		}
		for i := 0; i < count; i++ {
			current.WriteByte('\\')
		}
		backslashes = 0
	}
	flush := func() {
		if started {
			args = append(args, current.String())
			current.Reset()
			started = false
		}
	}
	// Bytes for the same reason as the GNU tokenizer above.
	for index := 0; index < len(content); index++ {
		c := content[index]
		switch {
		case c == '\\':
			backslashes++
			started = true
		case c == '"':
			odd := backslashes%2 == 1
			flushBackslashes(true)
			started = true
			if odd {
				current.WriteByte('"')
			} else {
				inQuotes = !inQuotes
			}
		case !inQuotes && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			flushBackslashes(false)
			flush()
		default:
			flushBackslashes(false)
			current.WriteByte(c)
			started = true
		}
	}
	flushBackslashes(false)
	flush()
	return args
}
