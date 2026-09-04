package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type FixtureManifest struct {
	Toolchain  string `json:"toolchain"`
	Project    string `json:"project"`
	Host       string `json:"host"`
	SourceRoot string `json:"sourceRoot"`
	BuildRoot  string `json:"buildRoot"`
	Generated  string `json:"generatedAt,omitempty"`
}

func LoadFixtureManifest(path string) (FixtureManifest, error) {
	var m FixtureManifest
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, nil
}

func FixtureDir(toolchain, project string) string {
	return filepath.Join("testdata", "fixtures", toolchain, project)
}
