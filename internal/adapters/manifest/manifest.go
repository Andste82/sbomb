// Package manifest parses native package and image manifests.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const MaxInputSize = 64 << 20

var ErrInputLimitExceeded = errors.New("input limit exceeded")

type Manifest struct {
	SchemaVersion int      `json:"schemaVersion"`
	Outputs       []Output `json:"outputs"`
}

type Output struct {
	Path   string  `json:"path"`
	Kind   string  `json:"kind"`
	Inputs []Input `json:"inputs"`
}

type Input struct {
	Path          string   `json:"path"`
	Role          string   `json:"role"`
	GeneratedFrom []string `json:"generatedFrom,omitempty"`
	Generator     string   `json:"generator,omitempty"`
}

func ParseFile(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	if len(data) > MaxInputSize {
		return Manifest{}, fmt.Errorf("manifest exceeds %d bytes: %w", MaxInputSize, ErrInputLimitExceeded)
	}
	return Parse(data)
}

func Parse(data []byte) (Manifest, error) {
	if len(data) > MaxInputSize {
		return Manifest{}, fmt.Errorf("manifest exceeds %d bytes: %w", MaxInputSize, ErrInputLimitExceeded)
	}
	var result Manifest
	if err := json.Unmarshal(data, &result); err != nil {
		return Manifest{}, err
	}
	if result.SchemaVersion != 1 {
		return Manifest{}, fmt.Errorf("unsupported manifest schema version %d", result.SchemaVersion)
	}
	for outputIndex, output := range result.Outputs {
		if err := validatePath(output.Path); err != nil {
			return Manifest{}, fmt.Errorf("output %d path: %w", outputIndex, err)
		}
		for inputIndex, input := range output.Inputs {
			if err := validatePath(input.Path); err != nil {
				return Manifest{}, fmt.Errorf("output %d input %d path: %w", outputIndex, inputIndex, err)
			}
			for generatedIndex, path := range input.GeneratedFrom {
				if err := validatePath(path); err != nil {
					return Manifest{}, fmt.Errorf("output %d input %d generatedFrom %d: %w", outputIndex, inputIndex, generatedIndex, err)
				}
			}
		}
	}
	return result, nil
}

// validatePath applies appendix E: a path is resolved against the project root
// unless it is absolute, and absolute paths are explicitly permitted -- a build
// that writes its own manifest names what it produced by full path. What the
// specification rejects is a path that escapes every anchor, and whether it
// does cannot be decided here, where no anchor is known: a relative path that
// climbs above its own root is refused, and anything else is identified later
// and reported as unanchored if it matches nothing.
func validatePath(path string) error {
	if path == "" {
		return errors.New("path is empty")
	}
	normalized := strings.ReplaceAll(path, "\\", "/")
	if strings.HasPrefix(normalized, "/") || isWindowsAbsolute(normalized) {
		return nil
	}
	depth := 0
	for _, part := range strings.Split(normalized, "/") {
		switch part {
		case "", ".":
		case "..":
			if depth == 0 {
				return fmt.Errorf("path escapes the project root: %w", ErrInputLimitExceeded)
			}
			depth--
		default:
			depth++
		}
	}
	return nil
}

func isWindowsAbsolute(path string) bool {
	if len(path) >= 2 && path[1] == ':' && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) {
		return true
	}
	return strings.HasPrefix(path, "//")
}
