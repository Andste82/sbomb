// Package jsonstream writes a large JSON object member by member and its
// large arrays element by element, byte for byte as encoding/json writes the
// whole value with two-space indentation and HTML escaping off.
//
// json.Encoder encodes a value in one piece: into a buffer the size of the
// output, then into a second one when it indents. For the documents sbomb
// writes on a large build -- an SBOM, the evidence dump -- that is the output
// held twice more in memory at the moment the run is largest. Writing element
// by element holds one element at a time, and the output is the same.
package jsonstream

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

// Member is one member of the object, in the order it is written. A member
// is either a Value, encoded whole, or an array of Len elements, each of which
// Element returns when it is written. Null writes an array member as null, as
// encoding/json writes a nil slice.
type Member struct {
	Name    string
	Value   any
	Array   bool
	Len     int
	Element func(index int) any
	Null    bool
}

// Elements is an array member over a slice.
func Elements[T any](name string, items []T) Member {
	return Member{
		Name: name, Array: true, Len: len(items), Null: items == nil,
		Element: func(index int) any { return items[index] },
	}
}

// WriteObject writes the members as one indented object and a newline, as
// json.Encoder with SetIndent("", "  ") and SetEscapeHTML(false) writes the
// equivalent struct.
func WriteObject(w io.Writer, members []Member) error {
	out := bufio.NewWriterSize(w, 1<<16)
	var encoded bytes.Buffer
	encode := func(value any, prefix string) error {
		encoded.Reset()
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent(prefix, "  ")
		if err := encoder.Encode(value); err != nil {
			return err
		}
		// Encode ends each value with a newline the enclosing value does not
		// have at that point.
		out.Write(bytes.TrimSuffix(encoded.Bytes(), []byte("\n")))
		return nil
	}
	if len(members) == 0 {
		out.WriteString("{}\n")
		return out.Flush()
	}
	out.WriteString("{\n")
	for index, member := range members {
		if index > 0 {
			out.WriteString(",\n")
		}
		name, err := json.Marshal(member.Name)
		if err != nil {
			return err
		}
		out.WriteString("  ")
		out.Write(name)
		out.WriteString(": ")
		switch {
		case !member.Array:
			if err := encode(member.Value, "  "); err != nil {
				return err
			}
		case member.Null:
			out.WriteString("null")
		case member.Len == 0:
			out.WriteString("[]")
		default:
			out.WriteString("[\n")
			for element := 0; element < member.Len; element++ {
				if element > 0 {
					out.WriteString(",\n")
				}
				out.WriteString("    ")
				if err := encode(member.Element(element), "    "); err != nil {
					return err
				}
			}
			out.WriteString("\n  ]")
		}
	}
	out.WriteString("\n}\n")
	return out.Flush()
}
