package pkgmanager

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/limits"
)

// This file is not a YAML implementation and must not be reused as one. It
// reads exactly as much of the format as the two files of the ESP-IDF
// component manager need -- dependencies.lock and idf_component.yml -- and
// refuses everything else outright.
//
// Why it exists at all: nothing in this repository parses YAML, and vendor/
// carries no parser. Taking a dependency for two files would put a whole YAML
// engine, its release cadence and its attack surface into a tool whose point
// is that it reads other people's build trees without trusting them. The
// alternative is this: a line-oriented reader for block mappings of scalars,
// with a written list of what it will not do.
//
// The rule that keeps it honest is refusal. Every construct below is one this
// reader does not implement, and meeting one aborts the whole file rather than
// producing a partial answer:
//
//   - anchors, aliases and merge keys (&a, *a, <<:), which make a value depend
//     on another part of the document;
//   - tags and the other reserved indicators (!, %, @, backtick);
//   - a tab in the indentation, which YAML forbids and which would otherwise
//     make the indent arithmetic below mean something different;
//   - a flow collection that does not close on the line it opened on;
//   - a second document in one file, and the ... end marker;
//   - a duplicate key in one mapping, where the value published would depend
//     on which of the two this reader happened to keep;
//   - a dedent to a column that no enclosing mapping starts at;
//   - a nesting deeper than maxIDFYAMLDepth.
//
// Two constructs are neither read nor refused, because refusing them would
// throw away good files: a block sequence (targets: in a real manifest) and a
// block scalar (description: |). Their key is recorded as present with no
// scalar value, and their lines are skipped by indentation. Skipping is
// structural rather than interpretive -- everything indented past the key
// belongs to that key -- so nothing inside them can be mistaken for an entry of
// the enclosing mapping. A flow collection that opens and closes on one line is
// handled the same way, because its extent is then unambiguous.

// maxIDFYAMLDepth bounds the nesting of section 30. The two files this reader
// serves nest three levels deep; eight leaves room for a manifest that grew
// without letting a crafted file drive the stack.
const maxIDFYAMLDepth = 8

// idfNode is one value of the subset: a scalar, a mapping, or a value that was
// recognised as present but deliberately not read (a sequence, a block scalar,
// a flow collection). That third state matters -- a caller must be able to tell
// "the manifest states no licence" from "the manifest states one in a shape
// this reader does not read", because only the first is silence.
type idfNode struct {
	scalar   string
	isScalar bool
	mapping  map[string]*idfNode
}

// child returns the entry of a mapping, or nil.
func (n *idfNode) child(key string) *idfNode {
	if n == nil || n.mapping == nil {
		return nil
	}
	return n.mapping[key]
}

// scalarAt walks a path of mapping keys and returns the scalar at its end. A
// missing key, a mapping where a scalar was expected and an unread value all
// answer the empty string: none of them is a value this tool may publish.
func (n *idfNode) scalarAt(keys ...string) string {
	current := n
	for _, key := range keys {
		current = current.child(key)
	}
	if current == nil || !current.isScalar {
		return ""
	}
	return current.scalar
}

// keysOf returns a mapping's keys in a fixed order. Map iteration in Go is
// deliberately random, and the packages this reader feeds must come out the
// same on every run.
func (n *idfNode) keysOf() []string {
	if n == nil || n.mapping == nil {
		return nil
	}
	keys := make([]string, 0, len(n.mapping))
	for key := range n.mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// idfYAMLFrame is one open mapping during parsing, with the column its keys
// start at.
type idfYAMLFrame struct {
	indent int
	node   *idfNode
}

// parseIDFYAML reads the subset described at the top of this file. The error it
// returns names the construct it refused, so that the caller's
// EVIDENCE_UNREADABLE can say what was in the way rather than only that
// something was.
func parseIDFYAML(data []byte) (*idfNode, error) {
	root := &idfNode{mapping: map[string]*idfNode{}}
	// The root mapping's column is unknown until the first key is seen: a file
	// may indent its top level, and only the file itself says so.
	rootIndentKnown := false
	stack := []idfYAMLFrame{{indent: 0, node: root}}

	// pending is the key whose value was left empty, and therefore the only key
	// a more indented line may belong to.
	var pending *idfNode
	pendingIndent := 0

	// While skipping, lines belong to a sequence or a block scalar and are not
	// interpreted at all.
	skipping := false
	skipIndent := 0
	skipSequence := false

	sawDocumentStart := false
	sawContent := false

	scanner := limits.Scanner(bytes.NewReader(data))
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSuffix(scanner.Text(), "\r")
		indent, hasTab := idfIndentOf(raw)
		if indent == len(raw) {
			// A blank or whitespace-only line ends nothing, inside a skipped
			// block or outside one.
			continue
		}
		if skipping {
			if indent > skipIndent || (skipSequence && indent == skipIndent && idfIsSequenceItem(raw[indent:])) {
				continue
			}
			skipping = false
		}
		if hasTab {
			return nil, fmt.Errorf("line %d: a tab in the indentation, which YAML does not allow", line)
		}
		content, err := idfStripComment(raw[indent:])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		content = strings.TrimRight(content, " ")
		if content == "" {
			continue
		}
		if content == "---" {
			if sawContent || sawDocumentStart {
				return nil, fmt.Errorf("line %d: a second document in one file", line)
			}
			sawDocumentStart = true
			continue
		}
		if content == "..." {
			return nil, fmt.Errorf("line %d: a document end marker, which this reader does not follow", line)
		}
		if idfIsSequenceItem(content) {
			// A sequence belongs to the key above it, which stays recorded as
			// present but unread. Both shapes are accepted: the items of
			// `targets:` may start at a deeper column or at its own.
			if pending == nil || indent < pendingIndent {
				return nil, fmt.Errorf("line %d: a sequence where this reader expects a mapping", line)
			}
			pending.mapping = nil
			pending = nil
			skipping, skipIndent, skipSequence = true, indent, true
			sawContent = true
			continue
		}

		key, value, err := idfSplitKey(content)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		sawContent = true

		if pending != nil && indent > pendingIndent {
			stack = append(stack, idfYAMLFrame{indent: indent, node: pending})
			pending = nil
		} else {
			pending = nil
			for len(stack) > 1 && indent < stack[len(stack)-1].indent {
				stack = stack[:len(stack)-1]
			}
			if indent != stack[len(stack)-1].indent {
				if len(stack) == 1 && !rootIndentKnown {
					stack[0].indent = indent
				} else {
					return nil, fmt.Errorf("line %d: the line starts at a column no enclosing mapping starts at", line)
				}
			}
		}
		rootIndentKnown = true
		if len(stack) > maxIDFYAMLDepth {
			return nil, fmt.Errorf("line %d: the mapping nests deeper than the parser limit of section 30", line)
		}

		frame := stack[len(stack)-1]
		if _, duplicate := frame.node.mapping[key]; duplicate {
			return nil, fmt.Errorf("line %d: the key %q appears twice in one mapping", line, key)
		}

		switch {
		case value == "":
			// A key with nothing after the colon opens a mapping -- or a
			// sequence, or a block scalar, which the next line decides.
			child := &idfNode{mapping: map[string]*idfNode{}}
			frame.node.mapping[key] = child
			pending = child
			pendingIndent = indent
		case idfIsBlockScalarHeader(value):
			frame.node.mapping[key] = &idfNode{}
			skipping, skipIndent, skipSequence = true, indent, false
		case value[0] == '[' || value[0] == '{':
			if !idfFlowCloses(value) {
				return nil, fmt.Errorf("line %d: a flow collection that does not close on its own line", line)
			}
			frame.node.mapping[key] = &idfNode{}
		default:
			scalar, err := idfScalar(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			frame.node.mapping[key] = &idfNode{scalar: scalar, isScalar: true}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("the file could not be read to its end: %w", err)
	}
	return root, nil
}

// idfIndentOf returns how many leading whitespace characters a line has, and
// whether a tab is among them.
func idfIndentOf(line string) (int, bool) {
	indent, hasTab := 0, false
	for indent < len(line) {
		switch line[indent] {
		case ' ':
			indent++
		case '\t':
			hasTab = true
			indent++
		default:
			return indent, hasTab
		}
	}
	return indent, hasTab
}

// idfIsSequenceItem reports whether a line's content opens a block sequence
// item.
func idfIsSequenceItem(content string) bool {
	return content == "-" || strings.HasPrefix(content, "- ")
}

// idfIsBlockScalarHeader reports whether a value is | or > with the chomping
// and indentation indicators YAML allows after them. The content is then
// skipped by indentation rather than read.
func idfIsBlockScalarHeader(value string) bool {
	if value[0] != '|' && value[0] != '>' {
		return false
	}
	for index := 1; index < len(value); index++ {
		switch value[index] {
		case '+', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		default:
			return false
		}
	}
	return true
}

// idfFlowCloses reports whether every bracket a line opened is closed again on
// it. A flow collection spanning lines would make every following line mean
// something this reader cannot see, which is why that one is refused rather
// than skipped.
func idfFlowCloses(value string) bool {
	depth := 0
	var quote byte
	for index := 0; index < len(value); index++ {
		char := value[index]
		if quote != 0 {
			if char == '\\' && quote == '"' {
				index++
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return quote == 0 && depth == 0
}

// idfStripComment removes a comment from a line. A # opens one only at the
// start of the content or after a space, so a URL such as
// https://example.invalid/a#b keeps its fragment.
func idfStripComment(content string) (string, error) {
	var quote byte
	for index := 0; index < len(content); index++ {
		char := content[index]
		if quote != 0 {
			if char == '\\' && quote == '"' {
				index++
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '#':
			if index == 0 || content[index-1] == ' ' || content[index-1] == '\t' {
				return strings.TrimRight(content[:index], " \t"), nil
			}
		}
	}
	if quote != 0 {
		return "", fmt.Errorf("a quoted scalar that is not closed on its own line")
	}
	return content, nil
}

// idfSplitKey splits `key: value` at the colon that ends the key -- the first
// one outside quotes that is followed by a space or by nothing. That rule is
// what lets `url: https://example.invalid/x` keep its scheme.
func idfSplitKey(content string) (string, string, error) {
	var quote byte
	for index := 0; index < len(content); index++ {
		char := content[index]
		if quote != 0 {
			if char == '\\' && quote == '"' {
				index++
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case ':':
			if index+1 < len(content) && content[index+1] != ' ' {
				continue
			}
			key, err := idfScalar(strings.TrimRight(content[:index], " "))
			if err != nil {
				return "", "", err
			}
			if key == "" {
				return "", "", fmt.Errorf("a mapping entry without a key")
			}
			if key == "<<" {
				return "", "", fmt.Errorf("a merge key, which makes a mapping depend on another part of the document")
			}
			return key, strings.TrimLeft(content[index+1:], " "), nil
		}
	}
	return "", "", fmt.Errorf("a line that is not a mapping entry")
}

// idfScalar reads one scalar. The quoted forms are unquoted; a plain scalar is
// taken as it stands, except that the indicators YAML reserves are refused
// rather than read as text.
func idfScalar(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	switch value[0] {
	case '\'':
		return idfSingleQuoted(value)
	case '"':
		return idfDoubleQuoted(value)
	case '&', '*', '!', '%', '@', '`':
		return "", fmt.Errorf("the reserved indicator %q, which this reader does not implement", value[0])
	}
	return value, nil
}

// idfSingleQuoted unquotes a single-quoted scalar, in which a doubled quote
// stands for one quote.
func idfSingleQuoted(value string) (string, error) {
	var out strings.Builder
	for index := 1; index < len(value); index++ {
		if value[index] != '\'' {
			out.WriteByte(value[index])
			continue
		}
		if index+1 < len(value) && value[index+1] == '\'' {
			out.WriteByte('\'')
			index++
			continue
		}
		if strings.TrimSpace(value[index+1:]) != "" {
			return "", fmt.Errorf("text after a closing quote")
		}
		return out.String(), nil
	}
	return "", fmt.Errorf("a quoted scalar that is not closed on its own line")
}

// idfDoubleQuoted unquotes a double-quoted scalar. Only the escapes these two
// files can plausibly carry are translated, and any other escape keeps its
// letter: refusing a file over an escape sequence in a description would throw
// away a licence that is stated plainly two lines further down.
func idfDoubleQuoted(value string) (string, error) {
	var out strings.Builder
	for index := 1; index < len(value); index++ {
		char := value[index]
		if char == '\\' {
			index++
			if index >= len(value) {
				return "", fmt.Errorf("a quoted scalar that is not closed on its own line")
			}
			switch value[index] {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			default:
				out.WriteByte(value[index])
			}
			continue
		}
		if char == '"' {
			if strings.TrimSpace(value[index+1:]) != "" {
				return "", fmt.Errorf("text after a closing quote")
			}
			return out.String(), nil
		}
		out.WriteByte(char)
	}
	return "", fmt.Errorf("a quoted scalar that is not closed on its own line")
}
