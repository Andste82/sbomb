package cyclonedx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const version = "0.0.0-milestone01"

// BOM is a minimal CycloneDX 1.6 BOM for milestone 01.
type BOM struct {
	BomFormat    string       `json:"bomFormat"`
	SpecVersion  string       `json:"specVersion"`
	Version      int          `json:"version"`
	SerialNumber string       `json:"serialNumber,omitempty"`
	Metadata     *Metadata    `json:"metadata,omitempty"`
	Components   []Component  `json:"components,omitempty"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

type Metadata struct {
	Timestamp string `json:"timestamp,omitempty"`
	Tools     []Tool `json:"tools,omitempty"`
}

type Tool struct {
	Vendor  string `json:"vendor,omitempty"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type Component struct {
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

type Dependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

func MarshalEmpty(reproducible bool) (string, error) {
	serial := ""
	if reproducible {
		serial = reproducibleSerialNumber()
	} else {
		id, err := uuid.NewRandom()
		if err != nil {
			return "", err
		}
		serial = "urn:uuid:" + id.String()
	}
	bom := BOM{
		BomFormat:    "CycloneDX",
		SpecVersion:  "1.6",
		Version:      1,
		SerialNumber: serial,
		Metadata: &Metadata{
			Tools: []Tool{{Vendor: "sbomb", Name: "sbomb", Version: version}},
		},
	}
	if !reproducible {
		bom.Metadata.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	out, err := json.MarshalIndent(bom, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

func reproducibleSerialNumber() string {
	const namespace = "6ba7b811-9dad-11d1-80b4-00c04fd430c8"
	canonical := BOM{
		BomFormat:   "CycloneDX",
		SpecVersion: "1.6",
		Version:     1,
		Metadata: &Metadata{
			Tools: []Tool{{Vendor: "sbomb", Name: "sbomb", Version: version}},
		},
	}
	body, _ := json.Marshal(canonical)
	hash := sha256.Sum256(body)
	name := "sbomb:" + hex.EncodeToString(hash[:])
	ns, err := uuid.Parse(namespace)
	if err != nil {
		return "urn:uuid:" + uuid.NewString()
	}
	return "urn:uuid:" + uuid.NewSHA1(ns, []byte(name)).String()
}

func WriteEmpty(path string, reproducible bool) error {
	out, err := MarshalEmpty(reproducible)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepathDir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

func filepathDir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}

func Validate(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var bom BOM
	if err := json.Unmarshal(data, &bom); err != nil {
		return err
	}
	if bom.BomFormat != "CycloneDX" {
		return fmt.Errorf("invalid bomFormat")
	}
	if bom.SpecVersion != "1.6" {
		return fmt.Errorf("unsupported specVersion %q", bom.SpecVersion)
	}
	if bom.Version < 1 {
		return fmt.Errorf("invalid version")
	}
	if bom.Metadata == nil {
		return fmt.Errorf("metadata is required")
	}
	if len(bom.Metadata.Tools) == 0 {
		return fmt.Errorf("metadata.tools is required")
	}
	return nil
}

func versionString() string { return version }
