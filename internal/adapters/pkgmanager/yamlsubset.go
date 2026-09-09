package pkgmanager

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/limits"
)

// This file is not a YAML implementation and must not be reused as one. It
// reads exactly as much of the format as the four files that need it -- the
// ESP-IDF component manager's dependencies.lock and idf_component.yml, and a
// Zephyr workspace's west.yml and zephyr/module.yml -- and refuses everything
// else outright.
//
// Why it exists at all: nothing in this repository parses YAML, and vendor/
// carries no parser. Taking a dependency for four files would put a whole YAML
// engine, its release cadence and its attack surface into a tool whose point
// is that it reads other people's build trees without trusting them. The
// alternative is this: a line-oriented reader for block mappings of scalars
// and block sequences of such mappings, with a written list of what it will
// not do.
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
//   - a sequence nested directly inside a sequence;
//   - a nesting deeper than maxYAMLDepth, or a sequence longer than
//     maxYAMLSequenceItems.
//
// A block sequence whose items open a mapping is read, because west.yml states
// its projects as exactly that and no key above them says what they are. Every
// other unread construct keeps the treatment it had: a block scalar
// (description: |), a flow collection that closes on its own line, and a
// sequence item that is a plain scalar (targets: - esp32) are recorded as
// present with no value, and their lines are skipped by indentation. That
// third state matters -- a caller must be able to tell "the manifest states no
// licence" from "the manifest states one in a shape this reader does not
// read", because only the first is silence. Skipping is structural rather than
// interpretive -- everything indented past the key belongs to that key -- so
// nothing inside a skipped block can be mistaken for an entry of the enclosing
// mapping.
//
// One consequence of reading sequence items is worth stating, because it is a
// widening and not only a keeping: an item is a mapping this reader walks, so
// the refusals above now apply inside one, where a skipped block used to hide
// them. A duplicate key or a reserved indicator under a `- ` refuses the file
// today and was never seen before. That is the direction this reader is allowed
// to move in -- it refuses more, never publishes more -- but it is a shared
// reader, and deviation D39 records the change.

// maxYAMLDepth bounds the nesting of section 30. Every open mapping counts,
// and so does a sequence and each of its items, because both are frames the
// input can push. The files this reader serves nest four or five levels deep
// counted that way; eight leaves room for a manifest that grew without letting
// a crafted file drive the stack.
const maxYAMLDepth = 8

// maxYAMLSequenceItems bounds how many items one sequence may hold. Bytes
// alone do not bound it: a small file can be nothing but two-character items,
// and each read one allocates a node. It is the parser's own last resort and
// deliberately looser than what any caller allows: an adapter states how many
// entries its file may name, and its tighter bound is the one that reports
// INPUT_LIMIT_EXCEEDED against the file.
const maxYAMLSequenceItems = 100_000

// errNotAMappingEntry says a line holds no `key: value`. It is a sentinel
// rather than a message because the sequence reader has to tell that case
// apart from a real refusal: an item that is a plain scalar is skipped, while
// an item stating a merge key or a reserved indicator aborts the file.
var errNotAMappingEntry = errors.New("a line that is not a mapping entry")

// yamlNode is one value of the subset: a scalar, a mapping, a sequence, or a
// value that was recognised as present but deliberately not read (a block
// scalar, a flow collection). A sequence holds the items that opened a mapping
// and nothing else; a sequence of plain scalars is a sequence with no items.
type yamlNode struct {
	scalar   string
	isScalar bool
	mapping  map[string]*yamlNode
	sequence []*yamlNode
}

// child returns the entry of a mapping, or nil.
func (n *yamlNode) child(key string) *yamlNode {
	if n == nil || n.mapping == nil {
		return nil
	}
	return n.mapping[key]
}

// scalarAt walks a path of mapping keys and returns the scalar at its end. A
// missing key, a mapping where a scalar was expected and an unread value all
// answer the empty string: none of them is a value this tool may publish.
func (n *yamlNode) scalarAt(keys ...string) string {
	current := n
	for _, key := range keys {
		current = current.child(key)
	}
	if current == nil || !current.isScalar {
		return ""
	}
	return current.scalar
}

// itemsOf returns a sequence's items in the order the file states them. The
// order is the file's and is kept: two runs over one manifest must publish the
// same packages in the same order, and a sequence has no key to sort by.
func (n *yamlNode) itemsOf() []*yamlNode {
	if n == nil {
		return nil
	}
	return n.sequence
}

// keysOf returns a mapping's keys in a fixed order. Map iteration in Go is
// deliberately random, and the packages this reader feeds must come out the
// same on every run.
func (n *yamlNode) keysOf() []string {
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

// yamlFrame is one open collection during parsing, with the column it starts
// at: for a mapping the column of its keys, for a sequence the column of its
// dashes.
type yamlFrame struct {
	indent     int
	node       *yamlNode
	isSequence bool
	// items counts what the sequence has taken, including the items that were
	// recognised but not read, so that the bound cannot be walked around by
	// stating a sequence of scalars.
	items int
}

// parseYAMLSubset reads the subset described at the top of this file. The error
// it returns names the construct it refused, so that the caller's
// EVIDENCE_UNREADABLE can say what was in the way rather than only that
// something was.
func parseYAMLSubset(data []byte) (*yamlNode, error) {
	root := &yamlNode{mapping: map[string]*yamlNode{}}
	// The root mapping's column is unknown until the first key is seen: a file
	// may indent its top level, and only the file itself says so.
	rootIndentKnown := false
	stack := []yamlFrame{{indent: 0, node: root}}

	// pending is the key whose value was left empty, and therefore the only key
	// a more indented line may belong to. A sequence item written as a bare `-`
	// is pending in the same way, and pendingIsItem tells the two apart: a
	// following dash is that item's sibling, never a sequence inside it.
	var pending *yamlNode
	pendingIndent := 0
	pendingIsItem := false

	// While skipping, lines belong to a block scalar or to a sequence item this
	// reader does not read, and are not interpreted at all.
	skipping := false
	skipIndent := 0

	sawDocumentStart := false
	sawContent := false

	scanner := limits.Scanner(bytes.NewReader(data))
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSuffix(scanner.Text(), "\r")
		indent, hasTab := yamlIndentOf(raw)
		if indent == len(raw) {
			// A blank or whitespace-only line ends nothing, inside a skipped
			// block or outside one.
			continue
		}
		if skipping {
			if indent > skipIndent {
				continue
			}
			skipping = false
		}
		if hasTab {
			return nil, fmt.Errorf("line %d: a tab in the indentation, which YAML does not allow", line)
		}
		content, err := yamlStripComment(raw[indent:])
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

		if yamlIsSequenceItem(content) {
			sawContent = true
			if pending != nil && !pendingIsItem && indent >= pendingIndent {
				// The key above opens a sequence. Both shapes are accepted:
				// the items of `projects:` may start at a deeper column or at
				// its own.
				pending.mapping = nil
				pending.sequence = make([]*yamlNode, 0)
				stack = append(stack, yamlFrame{indent: indent, node: pending, isSequence: true})
				pending, pendingIsItem = nil, false
				if len(stack) > maxYAMLDepth {
					return nil, fmt.Errorf("line %d: the document nests deeper than the parser limit of section 30", line)
				}
			} else {
				pending, pendingIsItem = nil, false
				for len(stack) > 1 && indent < stack[len(stack)-1].indent {
					stack = stack[:len(stack)-1]
				}
				top := stack[len(stack)-1]
				if !top.isSequence || top.indent != indent {
					return nil, fmt.Errorf("line %d: a sequence where this reader expects a mapping", line)
				}
			}
			frame := &stack[len(stack)-1]
			frame.items++
			if frame.items > maxYAMLSequenceItems {
				return nil, fmt.Errorf("line %d: the sequence holds more items than the parser limit of section 30", line)
			}

			rest := strings.TrimLeft(content[1:], " ")
			if rest == "" {
				// A bare dash opens an item whose keys start on the next line.
				item := &yamlNode{mapping: map[string]*yamlNode{}}
				frame.node.sequence = append(frame.node.sequence, item)
				pending, pendingIndent, pendingIsItem = item, indent, true
				continue
			}
			if yamlIsSequenceItem(rest) {
				return nil, fmt.Errorf("line %d: a sequence nested directly in a sequence, which this reader does not implement", line)
			}
			if rest[0] == '[' || rest[0] == '{' {
				if !yamlFlowCloses(rest) {
					return nil, fmt.Errorf("line %d: a flow collection that does not close on its own line", line)
				}
				continue
			}
			key, value, err := yamlSplitKey(rest)
			if errors.Is(err, errNotAMappingEntry) {
				// An item that is a plain scalar -- `targets:` states a list of
				// them -- is recorded as nothing, and whatever is indented
				// under it belongs to it and is skipped. No caller of this
				// reader asks for such a list, and reading one would mean
				// deciding what a bare word in a sequence means without a key
				// above it to say.
				skipping, skipIndent = true, indent
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			item := &yamlNode{mapping: map[string]*yamlNode{}}
			frame.node.sequence = append(frame.node.sequence, item)
			// The item's keys start where its first key does, not at the dash:
			// `- name: hal` puts the mapping at the column of `name`, and its
			// siblings on the following lines line up there.
			itemIndent := indent + len(content) - len(rest)
			stack = append(stack, yamlFrame{indent: itemIndent, node: item})
			if len(stack) > maxYAMLDepth {
				return nil, fmt.Errorf("line %d: the document nests deeper than the parser limit of section 30", line)
			}
			opened, skip, err := yamlAssign(item, key, value)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			if opened != nil {
				pending, pendingIndent, pendingIsItem = opened, itemIndent, false
			}
			if skip {
				skipping, skipIndent = true, itemIndent
			}
			continue
		}

		key, value, err := yamlSplitKey(content)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		sawContent = true

		if pending != nil && indent > pendingIndent {
			stack = append(stack, yamlFrame{indent: indent, node: pending})
			pending, pendingIsItem = nil, false
		} else {
			pending, pendingIsItem = nil, false
			for len(stack) > 1 && indent < stack[len(stack)-1].indent {
				stack = stack[:len(stack)-1]
			}
			// A sequence holds items and no keys, so a key at the column of its
			// dashes ends it: `projects:` and its items may share a column, and
			// a key that follows them belongs to the mapping `projects` itself
			// belongs to.
			for len(stack) > 1 && stack[len(stack)-1].isSequence && stack[len(stack)-1].indent == indent {
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
		if len(stack) > maxYAMLDepth {
			return nil, fmt.Errorf("line %d: the document nests deeper than the parser limit of section 30", line)
		}

		frame := stack[len(stack)-1]
		if frame.isSequence {
			// A key at a column where a sequence started, with items of that
			// sequence still open above it, has no mapping to belong to.
			return nil, fmt.Errorf("line %d: the line starts at a column no enclosing mapping starts at", line)
		}
		opened, skip, err := yamlAssign(frame.node, key, value)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if opened != nil {
			pending, pendingIndent, pendingIsItem = opened, indent, false
		}
		if skip {
			skipping, skipIndent = true, indent
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("the file could not be read to its end: %w", err)
	}
	return root, nil
}

// yamlAssign records one `key: value` in a mapping. It returns the node a key
// with nothing after the colon opened -- the only node a more indented line may
// belong to -- and whether the value was a block scalar whose lines are to be
// skipped.
func yamlAssign(node *yamlNode, key, value string) (opened *yamlNode, skip bool, err error) {
	if _, duplicate := node.mapping[key]; duplicate {
		return nil, false, fmt.Errorf("the key %q appears twice in one mapping", key)
	}
	switch {
	case value == "":
		// A key with nothing after the colon opens a mapping -- or a sequence,
		// or a block scalar, which the next line decides.
		child := &yamlNode{mapping: map[string]*yamlNode{}}
		node.mapping[key] = child
		return child, false, nil
	case yamlIsBlockScalarHeader(value):
		node.mapping[key] = &yamlNode{}
		return nil, true, nil
	case value[0] == '[' || value[0] == '{':
		if !yamlFlowCloses(value) {
			return nil, false, fmt.Errorf("a flow collection that does not close on its own line")
		}
		node.mapping[key] = &yamlNode{}
		return nil, false, nil
	default:
		scalar, err := yamlScalar(value)
		if err != nil {
			return nil, false, err
		}
		node.mapping[key] = &yamlNode{scalar: scalar, isScalar: true}
		return nil, false, nil
	}
}

// yamlIndentOf returns how many leading whitespace characters a line has, and
// whether a tab is among them.
func yamlIndentOf(line string) (int, bool) {
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

// yamlIsSequenceItem reports whether a line's content opens a block sequence
// item.
func yamlIsSequenceItem(content string) bool {
	return content == "-" || strings.HasPrefix(content, "- ")
}

// yamlIsBlockScalarHeader reports whether a value is | or > with the chomping
// and indentation indicators YAML allows after them. The content is then
// skipped by indentation rather than read.
func yamlIsBlockScalarHeader(value string) bool {
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

// yamlFlowCloses reports whether every bracket a line opened is closed again on
// it. A flow collection spanning lines would make every following line mean
// something this reader cannot see, which is why that one is refused rather
// than skipped.
func yamlFlowCloses(value string) bool {
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

// yamlStripComment removes a comment from a line. A # opens one only at the
// start of the content or after a space, so a URL such as
// https://example.invalid/a#b keeps its fragment.
func yamlStripComment(content string) (string, error) {
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

// yamlSplitKey splits `key: value` at the colon that ends the key -- the first
// one outside quotes that is followed by a space or by nothing. That rule is
// what lets `url: https://example.invalid/x` keep its scheme.
func yamlSplitKey(content string) (string, string, error) {
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
			key, err := yamlScalar(strings.TrimRight(content[:index], " "))
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
	return "", "", errNotAMappingEntry
}

// yamlScalar reads one scalar. The quoted forms are unquoted; a plain scalar is
// taken as it stands, except that the indicators YAML reserves are refused
// rather than read as text.
func yamlScalar(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	switch value[0] {
	case '\'':
		return yamlSingleQuoted(value)
	case '"':
		return yamlDoubleQuoted(value)
	case '&', '*', '!', '%', '@', '`':
		return "", fmt.Errorf("the reserved indicator %q, which this reader does not implement", value[0])
	}
	return value, nil
}

// yamlSingleQuoted unquotes a single-quoted scalar, in which a doubled quote
// stands for one quote.
func yamlSingleQuoted(value string) (string, error) {
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

// yamlDoubleQuoted unquotes a double-quoted scalar. Only the escapes these
// files can plausibly carry are translated, and any other escape keeps its
// letter: refusing a file over an escape sequence in a description would throw
// away a licence that is stated plainly two lines further down.
func yamlDoubleQuoted(value string) (string, error) {
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
