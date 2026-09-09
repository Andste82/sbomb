package pkgmanager

import (
	"strings"
	"testing"

	"github.com/example/sbomb/internal/limits"
)

// The reader of yamlsubset.go is written in this repository rather than vendored,
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
			document, err := parseYAMLSubset([]byte(testCase.content))
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
	document, err := parseYAMLSubset([]byte("targets:\n  - esp32\ndescription: |\n  text\nexclude: [a, b]\nlicense: MIT\n"))
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
			document, err := parseYAMLSubset([]byte(testCase.content))
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
		for level := 0; level <= maxYAMLDepth; level++ {
			builder.WriteString(strings.Repeat("  ", level))
			builder.WriteString("k:\n")
		}
		if _, err := parseYAMLSubset([]byte(builder.String())); err == nil {
			t.Fatal("a mapping past the nesting limit was accepted")
		} else if !strings.Contains(err.Error(), "nests deeper") {
			t.Errorf("error = %q, want it to name the nesting limit", err)
		}
	})

	t.Run("a mapping at the limit is still read", func(t *testing.T) {
		var builder strings.Builder
		for level := 0; level < maxYAMLDepth-1; level++ {
			builder.WriteString(strings.Repeat("  ", level))
			builder.WriteString("k:\n")
		}
		builder.WriteString(strings.Repeat("  ", maxYAMLDepth-1))
		builder.WriteString("license: MIT\n")
		document, err := parseYAMLSubset([]byte(builder.String()))
		if err != nil {
			t.Fatalf("a file inside the bound was refused: %v", err)
		}
		keys := make([]string, 0, maxYAMLDepth)
		for level := 0; level < maxYAMLDepth-1; level++ {
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
		if _, err := parseYAMLSubset([]byte(content)); err == nil {
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
	document, err := parseYAMLSubset([]byte("dependencies:\n  z/one:\n    version: 1\n  a/two:\n    version: 2\n  m/three:\n    version: 3\n"))
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
	document, err := parseYAMLSubset([]byte("version: 1.0.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := document.scalarAt("version", "deeper"); got != "" {
		t.Errorf("a walk through a scalar = %q, want nothing", got)
	}
	if got := document.scalarAt("absent"); got != "" {
		t.Errorf("a missing key = %q, want nothing", got)
	}
	if got := (*yamlNode)(nil).scalarAt("anything"); got != "" {
		t.Errorf("a walk from no document = %q, want nothing", got)
	}
}

// A block sequence of mappings is the shape a west manifest states its projects
// in, and no key above them says what they are, so the reader has to read the
// sequence itself. These are the shapes a real manifest has.
func TestTheReaderReadsASequenceOfMappings(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		want    []string
	}{
		{
			name: "items indented past their key",
			content: "manifest:\n  projects:\n" +
				"    - name: hal_nordic\n      revision: v3.5.0\n" +
				"    - name: mcuboot\n      revision: v1.10.0\n",
			want: []string{"hal_nordic", "mcuboot"},
		},
		{
			name: "items at their key's own column",
			content: "manifest:\n  projects:\n" +
				"  - name: hal_nordic\n    revision: v3.5.0\n" +
				"  - name: mcuboot\n    revision: v1.10.0\n",
			want: []string{"hal_nordic", "mcuboot"},
		},
		{
			name: "an item whose keys start on the line after the dash",
			content: "manifest:\n  projects:\n" +
				"    -\n      name: hal_nordic\n" +
				"    -\n      name: mcuboot\n",
			want: []string{"hal_nordic", "mcuboot"},
		},
		{
			name: "an item holding a nested mapping and a nested sequence",
			content: "manifest:\n  projects:\n" +
				"    - name: hal_nordic\n      submodules:\n        - foo\n      import:\n        file: west.yml\n" +
				"    - name: mcuboot\n      groups:\n        - optional\n",
			want: []string{"hal_nordic", "mcuboot"},
		},
		{
			name: "an item holding a block scalar and a flow collection",
			content: "manifest:\n  projects:\n" +
				"    - description: |\n        name: NOT-THIS-ONE\n      name: hal_nordic\n      groups: [a, b]\n" +
				"    - name: mcuboot\n",
			want: []string{"hal_nordic", "mcuboot"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			document, err := parseYAMLSubset([]byte(testCase.content))
			if err != nil {
				t.Fatalf("the reader refused a file it must read: %v", err)
			}
			items := document.child("manifest").child("projects").itemsOf()
			if len(items) != len(testCase.want) {
				t.Fatalf("items = %d, want %d", len(items), len(testCase.want))
			}
			for index, want := range testCase.want {
				// The order is the file's: a sequence has no key to sort by,
				// and the packages built from it must come out the same twice.
				if got := items[index].scalarAt("name"); got != want {
					t.Errorf("item %d is %q, want %q", index, got, want)
				}
			}
		})
	}
}

// What follows a sequence still belongs to the mapping the sequence's own key
// belongs to, in either shape. A key lost here would silently drop a manifest's
// defaults.
func TestAKeyAfterASequenceIsNotLost(t *testing.T) {
	for _, content := range []string{
		"manifest:\n  projects:\n    - name: a\n  defaults:\n    revision: main\n",
		"manifest:\n  projects:\n  - name: a\n  defaults:\n    revision: main\n",
		"manifest:\n  projects:\n    -\n      name: a\n  defaults:\n    revision: main\n",
	} {
		document, err := parseYAMLSubset([]byte(content))
		if err != nil {
			t.Fatalf("the reader refused a file it must read: %v\n%s", err, content)
		}
		if got := document.scalarAt("manifest", "defaults", "revision"); got != "main" {
			t.Errorf("revision = %q, want the key stated after the sequence\n%s", got, content)
		}
		if got := document.child("manifest").child("projects").itemsOf(); len(got) != 1 {
			t.Errorf("items = %d, want the one project\n%s", len(got), content)
		}
	}
}

// A sequence of plain scalars keeps the treatment it had: recorded as present,
// with no value and no item. No caller asks for such a list, and reading one
// would mean deciding what a bare word means with no key above it to say.
func TestASequenceOfScalarsIsStillNotRead(t *testing.T) {
	document, err := parseYAMLSubset([]byte("targets:\n  - esp32\n  - esp32s3\nlicense: MIT\n"))
	if err != nil {
		t.Fatal(err)
	}
	targets := document.child("targets")
	if targets == nil || targets.isScalar {
		t.Fatalf("targets = %#v, want a value recorded as present and not read", targets)
	}
	if len(targets.itemsOf()) != 0 {
		t.Errorf("items = %#v, want none out of a sequence of scalars", targets.itemsOf())
	}
	if document.scalarAt("license") != "MIT" {
		t.Error("the key after the sequence was lost")
	}
}

// The refusals listed at the top of the reader hold inside a sequence item as
// well: an item is a mapping like any other, and nothing may be read out of a
// file whose remainder was not.
func TestTheReaderRefusesWhatItDoesNotImplementInsideASequence(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		says    string
	}{
		{name: "an anchor in an item", content: "projects:\n  - name: &a\n", says: "reserved indicator"},
		{name: "an alias in an item", content: "projects:\n  - name: *a\n", says: "reserved indicator"},
		{name: "a tag in an item", content: "projects:\n  - name: !!str a\n", says: "reserved indicator"},
		{name: "a merge key in an item", content: "projects:\n  - <<: a\n", says: "merge key"},
		{name: "a tab in an item", content: "projects:\n  - name: a\n\tpath: b\n", says: "tab in the indentation"},
		{name: "a duplicate key in one item", content: "projects:\n  - name: a\n    name: b\n", says: "twice"},
		{name: "a dedent inside an item to a column no mapping starts at", content: "projects:\n  - name: a\n     path: b\n", says: "no enclosing mapping"},
		{name: "a flow collection left open in an item", content: "projects:\n  - groups: [a\n", says: "does not close"},
		{name: "a flow item left open", content: "projects:\n  - [a\n", says: "does not close"},
		{name: "a sequence nested directly in a sequence", content: "projects:\n  - - a\n", says: "nested directly in a sequence"},
		{name: "a key at the column of a sequence's dashes", content: "a:\n  - name: b\n  c: 1\n", says: "no enclosing mapping"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			document, err := parseYAMLSubset([]byte(testCase.content))
			if err == nil {
				t.Fatalf("the file was accepted, want it refused whole: %#v", document)
			}
			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("error = %q, want it to name the construct (%q)", err, testCase.says)
			}
		})
	}
}

// The two bounds a sequence adds, section 30: a sequence and each of its items
// is a frame the input can push, and a file that is small in bytes can still
// state an absurd number of items.
func TestASequencePastItsBoundsIsRefused(t *testing.T) {
	t.Run("nested deeper than the limit", func(t *testing.T) {
		content := "a:\n  - b:\n      c:\n        d:\n          e:\n            f:\n              g:\n                h: 1\n"
		if _, err := parseYAMLSubset([]byte(content)); err == nil {
			t.Fatal("a document past the nesting limit was accepted")
		} else if !strings.Contains(err.Error(), "nests deeper") {
			t.Errorf("error = %q, want it to name the nesting limit", err)
		}
	})

	t.Run("more items than the limit", func(t *testing.T) {
		var builder strings.Builder
		builder.WriteString("a:\n")
		for index := 0; index <= maxYAMLSequenceItems; index++ {
			builder.WriteString("  - b\n")
		}
		if _, err := parseYAMLSubset([]byte(builder.String())); err == nil {
			t.Fatal("a sequence past the item limit was accepted")
		} else if !strings.Contains(err.Error(), "more items than the parser limit") {
			t.Errorf("error = %q, want it to name the item limit", err)
		}
	})
}
