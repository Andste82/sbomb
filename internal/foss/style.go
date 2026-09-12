package foss

import (
	"fmt"
	"strings"
)

// style is the difference between the two renderings of section 32.6. One
// renderer, two styles: a second set of render functions for markdown would be
// two documents to keep in agreement, and the content is the deliverable while
// the markup is not.
type style struct {
	markdown bool
}

// wrapAt is where prose is wrapped. Verbatim content is never wrapped --
// a reformatted notice is not the notice the licence said to reproduce
// (requirement R12) -- so this applies to sbomb's own sentences alone.
const wrapAt = 76

func (s style) title(b *strings.Builder, text string) {
	if s.markdown {
		fmt.Fprintf(b, "# %s\n", text)
		return
	}
	fmt.Fprintf(b, "%s\n%s\n", text, strings.Repeat("=", len(text)))
}

func (s style) section(b *strings.Builder, text string) {
	b.WriteString("\n")
	if s.markdown {
		fmt.Fprintf(b, "## %s\n\n", text)
		return
	}
	fmt.Fprintf(b, "%s\n\n", text)
}

// componentHeading opens one component's block. In text form it is the rule
// the milestone's layout shows; in markdown it is a heading.
func (s style) componentHeading(b *strings.Builder, text string) {
	if s.markdown {
		fmt.Fprintf(b, "### %s\n\n", text)
		return
	}
	fmt.Fprintf(b, "%s\n%s\n", strings.Repeat("=", 64), text)
}

// field is a labelled value. The label column is fixed so that two runs of the
// same build produce the same bytes and a reader's eye finds the same column.
func (s style) field(b *strings.Builder, key, value string) {
	if s.markdown {
		fmt.Fprintf(b, "- **%s:** %s\n", key, value)
		return
	}
	fmt.Fprintf(b, "%-*s %s\n", 28, key+":", value)
}

// plain is a labelled value without the column alignment. The notices
// document is read as prose and has three of them; the review record is
// scanned as a table and has thirty.
func (s style) plain(b *strings.Builder, key, value string) {
	if s.markdown {
		fmt.Fprintf(b, "- **%s:** %s\n", key, value)
		return
	}
	fmt.Fprintf(b, "%s: %s\n", key, value)
}

// count is the completeness block's shape: a label and a number, or a number
// over a total.
func (s style) count(b *strings.Builder, key, value string) {
	if s.markdown {
		fmt.Fprintf(b, "| %s | %s |\n", key, value)
		return
	}
	fmt.Fprintf(b, "  %-*s %s\n", 30, key, value)
}

func (s style) countHeader(b *strings.Builder) {
	if s.markdown {
		b.WriteString("| | |\n|---|---|\n")
	}
}

// bullet is one item of a list.
func (s style) bullet(b *strings.Builder, text string) {
	if s.markdown {
		fmt.Fprintf(b, "- %s\n", text)
		return
	}
	fmt.Fprintf(b, "- %s\n", text)
}

// prose writes sbomb's own sentences, wrapped, with an optional indent.
func (s style) prose(b *strings.Builder, indent, text string) {
	for _, line := range wrap(text, wrapAt-len(indent)) {
		fmt.Fprintf(b, "%s%s\n", indent, line)
	}
}

// verbatim reproduces retained bytes under a label. The bytes are written
// unchanged -- line endings included -- because that is the whole point of
// retaining them (requirement R2). The only thing added is a newline when the
// file does not end with one, which is a separator and not a rewrite: without
// it the next label would sit on the last line of the licence.
func (s style) verbatim(b *strings.Builder, label, note string, bytes []byte) {
	if s.markdown {
		fmt.Fprintf(b, "\n#### %s\n", label)
		if note != "" {
			fmt.Fprintf(b, "\n%s\n", note)
		}
		// No bytes means this entry points at a text printed elsewhere: an
		// empty fence would say that the file is empty.
		if len(bytes) == 0 {
			return
		}
		b.WriteString("\n```text\n")
		b.Write(bytes)
		if bytes[len(bytes)-1] != '\n' {
			b.WriteString("\n")
		}
		b.WriteString("```\n")
		return
	}
	fmt.Fprintf(b, "\n--- %s ---\n", label)
	if note != "" {
		fmt.Fprintf(b, "%s\n", note)
	}
	b.Write(bytes)
	if len(bytes) > 0 && bytes[len(bytes)-1] != '\n' {
		b.WriteString("\n")
	}
}

// marker is where a missing text or a missing copyright line is stated. The
// alternatives are both worse: a silent omission looks complete, and a
// substituted canonical text looks correct (requirement R10).
func (s style) marker(b *strings.Builder, text string) {
	if s.markdown {
		fmt.Fprintf(b, "\n> %s\n", text)
		return
	}
	fmt.Fprintf(b, "\n%s\n", text)
}

// wrap breaks a sentence at word boundaries. It is deterministic and
// locale-free: the same text wraps the same way on every platform, which
// section 29 requires of every byte sbomb writes.
func wrap(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{}
	current := words[0]
	for _, word := range words[1:] {
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = word
			continue
		}
		current += " " + word
	}
	return append(lines, current)
}
