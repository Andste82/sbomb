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

func TestParseRejectsUnsafePaths(t *testing.T) {
	tests := []string{
		`{"schemaVersion":1,"outputs":[{"path":"../image.bin"}]}`,
		`{"schemaVersion":1,"outputs":[{"path":"/tmp/image.bin"}]}`,
		`{"schemaVersion":1,"outputs":[{"path":"build/image.bin","inputs":[{"path":"assets/a","generatedFrom":["../../config.yaml"]}]}]}`,
		`{"schemaVersion":1,"outputs":[{"path":"C:/image.bin"}]}`,
	}
	for _, input := range tests {
		if _, err := Parse([]byte(input)); err == nil {
			t.Errorf("Parse(%q) accepted unsafe path", input)
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
