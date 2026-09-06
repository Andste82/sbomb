package manifest

import (
	"errors"
	"strings"
	"testing"
)

func TestParseValidManifest(t *testing.T) {
	input := []byte(`{"schemaVersion":1,"outputs":[{"path":"build/image.bin","kind":"image","inputs":[{"path":"assets/index.html","role":"asset"},{"path":"build/generated.bin","role":"generated-asset","generatedFrom":["config/input.yaml"]}]}]}`)
	result, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(result.Outputs) != 1 || len(result.Outputs[0].Inputs) != 2 {
		t.Fatalf("Parse() = %#v", result)
	}
}

func TestParseRejectsPathsThatClimbOutOfTheirRoot(t *testing.T) {
	tests := []string{
		`{"schemaVersion":1,"outputs":[{"path":"../image.bin"}]}`,
		`{"schemaVersion":1,"outputs":[{"path":"build/image.bin","inputs":[{"path":"assets/a","generatedFrom":["../../config.yaml"]}]}]}`,
	}
	for _, input := range tests {
		if _, err := Parse([]byte(input)); err == nil {
			t.Errorf("Parse(%q) accepted a path that escapes its root", input)
		}
	}
}

// Appendix E: "All paths are resolved relative to the project root unless
// absolute." Rejecting absolute paths made the manifest unusable for a build
// that writes one naming what it produced, which is the common case.
func TestParseAcceptsAbsolutePaths(t *testing.T) {
	for _, input := range []string{
		`{"schemaVersion":1,"outputs":[{"path":"/build/image.bin","kind":"image","inputs":[{"path":"/src/index.html","role":"asset"}]}]}`,
		`{"schemaVersion":1,"outputs":[{"path":"C:/build/image.bin"}]}`,
	} {
		if _, err := Parse([]byte(input)); err != nil {
			t.Errorf("Parse(%q) = %v, want the absolute path accepted", input, err)
		}
	}
}

func TestParseRejectsOversizedInput(t *testing.T) {
	_, err := Parse([]byte(strings.Repeat("x", MaxInputSize+1)))
	if !errors.Is(err, ErrInputLimitExceeded) {
		t.Fatalf("Parse() error = %v, want input limit exceeded", err)
	}
}

func TestParseRejectsUnsupportedSchema(t *testing.T) {
	if _, err := Parse([]byte(`{"schemaVersion":2,"outputs":[]}`)); err == nil {
		t.Fatal("Parse() accepted unsupported schema")
	}
}
