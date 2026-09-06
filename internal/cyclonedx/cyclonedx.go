package cyclonedx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/example/sbomb/internal/buildinfo"
	"github.com/google/uuid"
)

// BOM is the in-memory CycloneDX 1.6 document used by the SBOM writer.
type BOM struct {
	BomFormat    string       `json:"bomFormat"`
	SpecVersion  string       `json:"specVersion"`
	Version      int          `json:"version"`
	SerialNumber string       `json:"serialNumber,omitempty"`
	Metadata     *Metadata    `json:"metadata,omitempty"`
	Components   []Component  `json:"components,omitempty"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
	Properties   []Property   `json:"properties,omitempty"`
}

type Metadata struct {
	Timestamp  string     `json:"timestamp,omitempty"`
	Tools      []Tool     `json:"tools,omitempty"`
	Component  *Component `json:"component,omitempty"`
	Properties []Property `json:"properties,omitempty"`
}

type Tool struct {
	Vendor  string `json:"vendor,omitempty"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type Component struct {
	Type       string                `json:"type,omitempty"`
	Name       string                `json:"name,omitempty"`
	Version    string                `json:"version,omitempty"`
	BomRef     string                `json:"bom-ref,omitempty"`
	PURL       string                `json:"purl,omitempty"`
	Supplier   *OrganizationalEntity `json:"supplier,omitempty"`
	Hashes     []Hash                `json:"hashes,omitempty"`
	Licenses   []License             `json:"licenses,omitempty"`
	Properties []Property            `json:"properties,omitempty"`
	Evidence   *Evidence             `json:"evidence,omitempty"`
}

type OrganizationalEntity struct {
	Name string `json:"name,omitempty"`
}

type Hash struct {
	Alg   string `json:"alg"`
	Value string `json:"content"`
}

type License struct {
	License    *LicenseIdentifier `json:"license,omitempty"`
	Expression string             `json:"expression,omitempty"`
}

type LicenseIdentifier struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type Property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Evidence struct {
	Identity    []IdentityEvidence `json:"identity,omitempty"`
	Occurrences []Occurrence       `json:"occurrences,omitempty"`
}

type IdentityEvidence struct {
	Field      string   `json:"field"`
	Value      string   `json:"value"`
	Confidence float64  `json:"confidence,omitempty"`
	Methods    []Method `json:"methods,omitempty"`
}

type Method struct {
	Technique  string  `json:"technique"`
	Value      string  `json:"value,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type Occurrence struct {
	BomRef   string `json:"bom-ref,omitempty"`
	Location string `json:"location"`
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
			Tools: []Tool{{Vendor: buildinfo.Vendor, Name: buildinfo.Name, Version: buildinfo.Version}},
		},
	}
	if !reproducible {
		bom.Metadata.Timestamp = timestamp()
	}
	return MarshalBOM(bom)
}

func MarshalBOM(bom BOM) (string, error) {
	canonicalizeBOM(&bom)
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(bom); err != nil {
		return "", err
	}
	return out.String(), nil
}

func canonicalizeBOM(bom *BOM) {
	for i := range bom.Components {
		sort.SliceStable(bom.Components[i].Hashes, func(a, b int) bool {
			return bom.Components[i].Hashes[a].Alg < bom.Components[i].Hashes[b].Alg
		})
		sort.SliceStable(bom.Components[i].Properties, func(a, b int) bool {
			if bom.Components[i].Properties[a].Name == bom.Components[i].Properties[b].Name {
				return bom.Components[i].Properties[a].Value < bom.Components[i].Properties[b].Value
			}
			return bom.Components[i].Properties[a].Name < bom.Components[i].Properties[b].Name
		})
		if bom.Components[i].Evidence != nil {
			sort.SliceStable(bom.Components[i].Evidence.Identity, func(a, b int) bool {
				if bom.Components[i].Evidence.Identity[a].Field == bom.Components[i].Evidence.Identity[b].Field {
					return bom.Components[i].Evidence.Identity[a].Value < bom.Components[i].Evidence.Identity[b].Value
				}
				return bom.Components[i].Evidence.Identity[a].Field < bom.Components[i].Evidence.Identity[b].Field
			})
			for j := range bom.Components[i].Evidence.Identity {
				sort.SliceStable(bom.Components[i].Evidence.Identity[j].Methods, func(a, b int) bool {
					if bom.Components[i].Evidence.Identity[j].Methods[a].Technique == bom.Components[i].Evidence.Identity[j].Methods[b].Technique {
						return bom.Components[i].Evidence.Identity[j].Methods[a].Value < bom.Components[i].Evidence.Identity[j].Methods[b].Value
					}
					return bom.Components[i].Evidence.Identity[j].Methods[a].Technique < bom.Components[i].Evidence.Identity[j].Methods[b].Technique
				})
			}
			sort.SliceStable(bom.Components[i].Evidence.Occurrences, func(a, b int) bool {
				return bom.Components[i].Evidence.Occurrences[a].Location < bom.Components[i].Evidence.Occurrences[b].Location
			})
		}
		if len(bom.Components[i].Licenses) > 1 {
			sort.SliceStable(bom.Components[i].Licenses, func(a, b int) bool {
				if bom.Components[i].Licenses[a].Expression != "" && bom.Components[i].Licenses[b].Expression != "" {
					return bom.Components[i].Licenses[a].Expression < bom.Components[i].Licenses[b].Expression
				}
				if bom.Components[i].Licenses[a].License != nil && bom.Components[i].Licenses[b].License != nil {
					if bom.Components[i].Licenses[a].License.ID != "" && bom.Components[i].Licenses[b].License.ID != "" {
						return bom.Components[i].Licenses[a].License.ID < bom.Components[i].Licenses[b].License.ID
					}
					if bom.Components[i].Licenses[a].License.Name != "" && bom.Components[i].Licenses[b].License.Name != "" {
						return bom.Components[i].Licenses[a].License.Name < bom.Components[i].Licenses[b].License.Name
					}
				}
				return fmt.Sprintf("%v", bom.Components[i].Licenses[a]) < fmt.Sprintf("%v", bom.Components[i].Licenses[b])
			})
		}
	}
	sort.SliceStable(bom.Components, func(a, b int) bool {
		if bom.Components[a].Type != bom.Components[b].Type {
			return bom.Components[a].Type < bom.Components[b].Type
		}
		return bom.Components[a].BomRef < bom.Components[b].BomRef
	})
	sort.SliceStable(bom.Dependencies, func(a, b int) bool {
		return bom.Dependencies[a].Ref < bom.Dependencies[b].Ref
	})
	for i := range bom.Dependencies {
		sort.Strings(bom.Dependencies[i].DependsOn)
	}
	sort.SliceStable(bom.Properties, func(a, b int) bool {
		if bom.Properties[a].Name == bom.Properties[b].Name {
			return bom.Properties[a].Value < bom.Properties[b].Value
		}
		return bom.Properties[a].Name < bom.Properties[b].Name
	})
}

func reproducibleSerialNumber() string {
	return ReproducibleSerialNumber(BOM{BomFormat: "CycloneDX", SpecVersion: "1.6", Version: 1, Metadata: &Metadata{Tools: []Tool{{Vendor: buildinfo.Vendor, Name: buildinfo.Name, Version: buildinfo.Version}}}})
}

// ReproducibleSerialNumber derives the UUIDv5 serial from the canonical BOM
// with volatile fields removed.
func ReproducibleSerialNumber(bom BOM) string {
	const namespace = "6ba7b811-9dad-11d1-80b4-00c04fd430c8"
	bom.SerialNumber = ""
	if bom.Metadata != nil {
		metadata := *bom.Metadata
		metadata.Timestamp = ""
		bom.Metadata = &metadata
	}
	canonicalizeBOM(&bom)
	body, _ := json.Marshal(bom)
	hash := sha256.Sum256(body)
	name := "sbomb:" + hex.EncodeToString(hash[:])
	ns, err := uuid.Parse(namespace)
	if err != nil {
		return ""
	}
	return "urn:uuid:" + uuid.NewSHA1(ns, []byte(name)).String()
}

func timestamp() string {
	if value := os.Getenv("SOURCE_DATE_EPOCH"); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
		}
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func WriteEmpty(path string, reproducible bool) error {
	out, err := MarshalEmpty(reproducible)
	if err != nil {
		return err
	}
	// Validation runs on the exact bytes that will be written, before the
	// temporary file is renamed into place (section 32.5).
	if err := Validate([]byte(out)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sbomb-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// WriteBOM serializes and validates a populated document using the same atomic
// write path as the empty-document compatibility helper.
func WriteBOM(path string, bom BOM) error {
	out, err := MarshalBOM(bom)
	if err != nil {
		return err
	}
	// Validation runs on the exact bytes that will be written, before the
	// temporary file is renamed into place (section 32.5).
	if err := Validate([]byte(out)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sbomb-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// ValidateFile runs both validation layers over a document on disk.
func ValidateFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return Validate(data)
}

func ValidateDocument(data []byte) error {
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
	if bom.Metadata.Timestamp != "" && !isRFC3339Timestamp(bom.Metadata.Timestamp) {
		return fmt.Errorf("metadata.timestamp must be RFC3339")
	}
	if err := validateRootComponent(bom); err != nil {
		return err
	}
	if err := validateUniqueRefs(bom); err != nil {
		return err
	}
	if err := validateDependencies(bom); err != nil {
		return err
	}
	if err := validateHashes(bom); err != nil {
		return err
	}
	if err := validatePURLs(bom); err != nil {
		return err
	}
	if err := validateProperties(bom); err != nil {
		return err
	}
	return nil
}

func validateUniqueRefs(bom BOM) error {
	seen := make(map[string]struct{}, len(bom.Components)+1)
	for _, comp := range bom.Components {
		if comp.BomRef == "" {
			continue
		}
		if _, ok := seen[comp.BomRef]; ok {
			return fmt.Errorf("duplicate bom-ref: %s", comp.BomRef)
		}
		seen[comp.BomRef] = struct{}{}
	}
	if bom.Metadata != nil && bom.Metadata.Component != nil && bom.Metadata.Component.BomRef != "" {
		if _, ok := seen[bom.Metadata.Component.BomRef]; ok {
			return fmt.Errorf("duplicate bom-ref: %s", bom.Metadata.Component.BomRef)
		}
		seen[bom.Metadata.Component.BomRef] = struct{}{}
	}
	dependencyRefs := make(map[string]struct{}, len(bom.Dependencies))
	for _, dep := range bom.Dependencies {
		if dep.Ref == "" {
			continue
		}
		if _, ok := dependencyRefs[dep.Ref]; ok {
			return fmt.Errorf("duplicate dependency ref: %s", dep.Ref)
		}
		dependencyRefs[dep.Ref] = struct{}{}
	}
	return nil
}

func validateDependencies(bom BOM) error {
	refs := make(map[string]struct{}, len(bom.Components)+len(bom.Dependencies))
	for _, comp := range bom.Components {
		if comp.BomRef == "" {
			continue
		}
		refs[comp.BomRef] = struct{}{}
	}
	if bom.Metadata != nil && bom.Metadata.Component != nil && bom.Metadata.Component.BomRef != "" {
		refs[bom.Metadata.Component.BomRef] = struct{}{}
	}
	for _, dep := range bom.Dependencies {
		if _, ok := refs[dep.Ref]; !ok {
			return fmt.Errorf("dangling dependency ref: %s", dep.Ref)
		}
		for _, target := range dep.DependsOn {
			if _, ok := refs[target]; !ok {
				return fmt.Errorf("dangling dependency target %q in %q", target, dep.Ref)
			}
		}
	}
	// Section 28.5: every bom-ref must appear exactly once as a dependency
	// entry, even when it depends on nothing, so that a consumer can close the
	// graph. Checking the component refs against themselves, as this did
	// before, proves nothing.
	declared := make(map[string]struct{}, len(bom.Dependencies))
	for _, dep := range bom.Dependencies {
		declared[dep.Ref] = struct{}{}
	}
	for ref := range refs {
		if _, ok := declared[ref]; !ok {
			return fmt.Errorf("bom-ref %q has no dependencies entry; section 28.5 requires one for every component", ref)
		}
	}
	return nil
}

// validateRootComponent enforces section 28.2 and 28.3: the document has a
// root component, and it is not repeated in the flat component array.
func validateRootComponent(bom BOM) error {
	if bom.Metadata == nil || bom.Metadata.Component == nil {
		return fmt.Errorf("metadata.component is missing; the document does not say what it describes")
	}
	if bom.Metadata.Component.BomRef == "" {
		return fmt.Errorf("metadata.component has no bom-ref")
	}
	for _, comp := range bom.Components {
		if comp.BomRef == bom.Metadata.Component.BomRef {
			return fmt.Errorf("the root component %q also appears in components[]", comp.BomRef)
		}
	}
	return nil
}

func validateHashes(bom BOM) error {
	for _, comp := range bom.Components {
		for _, h := range comp.Hashes {
			if err := validateHashValue(h.Alg, h.Value); err != nil {
				return fmt.Errorf("component %q hash invalid: %w", comp.BomRef, err)
			}
		}
	}
	return nil
}

func validateHashValue(alg, value string) error {
	want := hashLengthFor(alg)
	if want == 0 {
		return nil
	}
	if len(value) != want {
		return fmt.Errorf("hash length for %s is %d but got %d", alg, want, len(value))
	}
	return nil
}

func hashLengthFor(alg string) int {
	switch strings.ToUpper(alg) {
	case "MD5":
		return 32
	case "SHA-1", "SHA1":
		return 40
	case "SHA-256", "SHA256":
		return 64
	case "SHA-384", "SHA384":
		return 96
	case "SHA-512", "SHA512":
		return 128
	case "BLAKE2B-256":
		return 64
	case "BLAKE2B-384":
		return 96
	case "BLAKE2B-512":
		return 128
	case "BLAKE3":
		return 64
	default:
		return 0
	}
}

func validatePURLs(bom BOM) error {
	for _, comp := range bom.Components {
		if comp.PURL == "" {
			continue
		}
		if !strings.HasPrefix(comp.PURL, "pkg:") {
			return fmt.Errorf("bad purl: %s", comp.PURL)
		}
		if strings.Contains(comp.PURL, " ") {
			return fmt.Errorf("bad purl: %s", comp.PURL)
		}
	}
	return nil
}

func validateProperties(bom BOM) error {
	seen := map[string]struct{}{}
	for _, p := range bom.Properties {
		if err := validatePropertyName(p.Name); err != nil {
			return err
		}
		if _, ok := seen[p.Name+"="+p.Value]; ok {
			continue
		}
		seen[p.Name+"="+p.Value] = struct{}{}
	}
	for _, comp := range bom.Components {
		for _, p := range comp.Properties {
			if err := validatePropertyName(p.Name); err != nil {
				return fmt.Errorf("component %q: %w", comp.BomRef, err)
			}
		}
	}
	return nil
}

func validatePropertyName(name string) error {
	if name == "" {
		return fmt.Errorf("empty property name")
	}
	if !strings.HasPrefix(name, "sbomb:") {
		return fmt.Errorf("property name %q must start with sbomb:", name)
	}
	allowed := map[string]struct{}{
		"sbomb:cdx:archiveProperty":    {},
		"sbomb:cdx:executableProperty": {},
		"sbomb:cdx:structuredProperty": {},
		"sbomb:evidence:artifacts":     {},
		"sbomb:license:reason":         {},
		"sbomb:license:review":         {},
		"sbomb:run:timestamp":          {},
		"sbomb:run:sourceDateEpoch":    {},
	}
	if _, ok := allowed[name]; ok {
		return nil
	}
	if strings.Contains(name, ":") {
		return nil
	}
	return fmt.Errorf("property name %q is not in the sbomb namespace", name)
}

func isRFC3339Timestamp(v string) bool {
	if v == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339, v)
	return err == nil
}
