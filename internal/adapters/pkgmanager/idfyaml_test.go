package pkgmanager

import (
	"strings"
	"testing"

	"github.com/example/sbomb/internal/limits"
)

// The reader of idfyaml.go is written in this repository rather than vendored,
// so what it accepts and what it refuses is a decision of this tool and has to
// be stated in tests. The two tables below are that statement: the first says
// which shapes of the two ESP-IDF files are read, the second says that every
// construct the reader does not implement aborts the whole file instead of
// yielding a partial answer.

func TestTheReaderReadsTheShapesTheseTwoFilesReallyHave(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		path    []string
		want    string
	}{
		{
			name:    "a plain scalar",
			content: "version: 2.5.3\n",
			path:    []string{"version"}, want: "2.5.3",
		},
		{
			name:    "a double quoted scalar",
			content: "version: \"2.5.3\"\n",
			path:    []string{"version"}, want: "2.5.3",
		},
		{
			name:    "a single quoted scalar with a doubled quote in it",
			content: "description: 'it''s here'\n",
			path:    []string{"description"}, want: "it's here",
		},
		{
			name:    "an escape inside a double quoted scalar",
			content: "description: \"one\\ntwo\"\n",
			path:    []string{"description"}, want: "one\ntwo",
		},
		{
			name:    "a nested mapping, which is how the lock states a source",
			content: "dependencies:\n  espressif/led_strip:\n    source:\n      type: service\n",
			path:    []string{"dependencies", "espressif/led_strip", "source", "type"}, want: "service",
		},
		{
			// The colon of a URL is not the colon that ends a key: only one
			// followed by a space or by nothing is.
			name:    "a URL keeps its scheme",
			content: "url: https://components.espressif.com/components/espressif/led_strip\n",
			path:    []string{"url"}, want: "https://components.espressif.com/components/espressif/led_strip",
		},
		{
			name:    "a fragment in a URL is not a comment",
			content: "url: https://example.invalid/a#b # but this is\n",
			path:    []string{"url"}, want: "https://example.invalid/a#b",
		},
		{
			name:    "a comment line and a blank line between entries",
			content: "# what this component is\n\nlicense: Apache-2.0\n\n# trailing\n",
			path:    []string{"license"}, want: "Apache-2.0",
		},
		{
			name:    "CRLF line endings, which a checkout on Windows leaves",
			content: "version: 2.5.3\r\nlicense: MIT\r\n",
			path:    []string{"license"}, want: "MIT",
		},
		{
			name:    "a licence stated after a block scalar the reader skips",
			content: "description: |\n  Driver for addressable LEDs.\n  license: NOT-THIS-ONE\nlicense: Apache-2.0\n",
			path:    []string{"license"}, want: "Apache-2.0",
		},
		{
			name:    "a licence stated after a folded scalar",
			content: "description: >-\n  one long line\nlicense: MIT\n",
			path:    []string{"license"}, want: "MIT",
		},
		{
			name:    "a sequence whose items are indented past their key",
			content: "targets:\n  - esp32\n  - esp32s3\nlicense: MIT\n",
			path:    []string{"license"}, want: "MIT",
		},
		{
			name:    "a sequence whose items start at their key's own column",
			content: "targets:\n- esp32\n- esp32s3\nlicense: MIT\n",
			path:    []string{"license"}, want: "MIT",
		},
		{
			name:    "a flow collection that closes on its own line",
			content: "files:\n  exclude: [\"**/test/**\", \"*.md\"]\nlicense: MIT\n",
			path:    []string{"license"}, want: "MIT",
		},
		{
			name:    "a single document start marker",
			content: "---\nlicense: MIT\n",
			path:    []string{"license"}, want: "MIT",
		},
		{
			// A file may indent its own top level; only the file says where
			// that level is.
			name:    "a top level that is itself indented",
			content: "  version: 1.0.0\n  license: MIT\n",
			path:    []string{"license"}, want: "MIT",
		},
		{
			name:    "an empty file states nothing",
			content: "",
			path:    []string{"license"}, want: "",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			document, err := parseIDFYAML([]byte(testCase.content))
			if err != nil {
				t.Fatalf("the reader refused a file it must read: %v", err)
			}
			if got := document.scalarAt(testCase.path...); got != testCase.want {
				t.Errorf("%v = %q, want %q", testCase.path, got, testCase.want)
			}
		})
	}
}

// A key whose value is a sequence, a block scalar or a flow collection is
// recorded as present without a scalar. The distinction matters: a caller must
// be able to tell "the manifest states no licence" from "it states one in a
// shape this reader does not read", because only the first is silence.
func TestAValueThatIsNotReadIsStillNotAScalar(t *testing.T) {
	document, err := parseIDFYAML([]byte("targets:\n  - esp32\ndescription: |\n  text\nexclude: [a, b]\nlicense: MIT\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"targets", "description", "exclude"} {
		child := document.child(key)
		if child == nil {
			t.Errorf("%q was dropped, want it recorded as present", key)
			continue
		}
		if child.isScalar {
			t.Errorf("%q came back as the scalar %q, want no value at all", key, child.scalar)
		}
	}
	if document.scalarAt("license") != "MIT" {
		t.Error("a key after the unread ones was lost")
	}
}

// Refusal is what keeps this reader honest. Every construct it does not
// implement aborts the file, so nothing is ever published out of a file whose
// remainder could not be read -- and the error names what was in the way, so
// the EVIDENCE_UNREADABLE built from it can say so too.
func TestTheReaderRefusesWhatItDoesNotImplement(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		says    string
	}{
		{name: "an anchor", content: "dependencies: &all\n", says: "reserved indicator"},
		{name: "an alias", content: "dependencies: *all\n", says: "reserved indicator"},
		{name: "a tag", content: "version: !!str 1.0\n", says: "reserved indicator"},
		{name: "a merge key", content: "a:\n  <<: b\n", says: "merge key"},
		{name: "a tab in the indentation", content: "a:\n\tb: c\n", says: "tab in the indentation"},
		{name: "a flow collection left open", content: "exclude: [a, b\n", says: "does not close"},
		{name: "a quoted scalar left open", content: "version: \"2.5.3\n", says: "not closed"},
		{name: "text after a closing quote", content: "version: \"2.5.3\" and more\n", says: "after a closing quote"},
		{name: "a second document", content: "a: 1\n---\nb: 2\n", says: "second document"},
		{name: "a document end marker", content: "a: 1\n...\n", says: "document end marker"},
		{name: "a duplicate key", content: "a: 1\na: 2\n", says: "twice"},
		{name: "a dedent to a column no mapping starts at", content: "a:\n    b: 1\n  c: 2\n", says: "no enclosing mapping"},
		{name: "a line that is not a mapping entry", content: "just some text\n", says: "not a mapping entry"},
		{name: "a mapping entry without a key", content: ": value\n", says: "without a key"},
		{name: "a sequence where a mapping was expected", content: "- esp32\n", says: "sequence where"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			document, err := parseIDFYAML([]byte(testCase.content))
			if err == nil {
				t.Fatalf("the file was accepted, want it refused whole: %#v", document)
			}
			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("error = %q, want it to name the construct (%q)", err, testCase.says)
			}
		})
	}
}

// The two bounds the reader carries itself, section 30: how deep a mapping may
// nest, and how long one line may be. Neither is checked by the byte bound of
// espidf.go, which is why they are proven here.
func TestTheReaderRefusesAnInputOverItsOwnBounds(t *testing.T) {
	t.Run("a mapping nested deeper than the limit", func(t *testing.T) {
		var builder strings.Builder
		for level := 0; level <= maxIDFYAMLDepth; level++ {
			builder.WriteString(strings.Repeat("  ", level))
			builder.WriteString("k:\n")
		}
		if _, err := parseIDFYAML([]byte(builder.String())); err == nil {
			t.Fatal("a mapping past the nesting limit was accepted")
		} else if !strings.Contains(err.Error(), "nests deeper") {
			t.Errorf("error = %q, want it to name the nesting limit", err)
		}
	})

	t.Run("a mapping at the limit is still read", func(t *testing.T) {
		var builder strings.Builder
		for level := 0; level < maxIDFYAMLDepth-1; level++ {
			builder.WriteString(strings.Repeat("  ", level))
			builder.WriteString("k:\n")
		}
		builder.WriteString(strings.Repeat("  ", maxIDFYAMLDepth-1))
		builder.WriteString("license: MIT\n")
		document, err := parseIDFYAML([]byte(builder.String()))
		if err != nil {
			t.Fatalf("a file inside the bound was refused: %v", err)
		}
		keys := make([]string, 0, maxIDFYAMLDepth)
		for level := 0; level < maxIDFYAMLDepth-1; level++ {
			keys = append(keys, "k")
		}
		if got := document.scalarAt(append(keys, "license")...); got != "MIT" {
			t.Errorf("license = %q, want the file at the bound to be read", got)
		}
	})

	t.Run("a line longer than the line bound", func(t *testing.T) {
		// limits.Scanner refuses rather than growing without bound, so a file
		// with no newline in it cannot make this reader allocate at will.
		content := "license: " + strings.Repeat("x", limits.MaxLine) + "\n"
		if _, err := parseIDFYAML([]byte(content)); err == nil {
			t.Fatal("a line past the line bound was accepted")
		} else if !strings.Contains(err.Error(), "could not be read to its end") {
			t.Errorf("error = %q, want it to say the file was not read to its end", err)
		}
	})
}

// Map iteration in Go is deliberately random, so the keys a caller walks must
// be ordered by the reader rather than by the runtime: the packages built from
// them have to come out the same on every run.
func TestTheKeysOfAMappingComeBackSorted(t *testing.T) {
	document, err := parseIDFYAML([]byte("dependencies:\n  z/one:\n    version: 1\n  a/two:\n    version: 2\n  m/three:\n    version: 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	keys := document.child("dependencies").keysOf()
	want := []string{"a/two", "m/three", "z/one"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", keys, want)
	}
}

// A path that leads through a scalar, or through nothing, answers the empty
// string. Neither is a value this tool may publish, and both must be told apart
// from a stated one only by the caller's own check.
func TestALookupThroughSomethingThatIsNotAMappingIsEmpty(t *testing.T) {
	document, err := parseIDFYAML([]byte("version: 1.0.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := document.scalarAt("version", "deeper"); got != "" {
		t.Errorf("a walk through a scalar = %q, want nothing", got)
	}
	if got := document.scalarAt("absent"); got != "" {
		t.Errorf("a missing key = %q, want nothing", got)
	}
	if got := (*idfNode)(nil).scalarAt("anything"); got != "" {
		t.Errorf("a walk from no document = %q, want nothing", got)
	}
}
