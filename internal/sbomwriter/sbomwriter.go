// Package sbomwriter is the format-neutral hand-off from discovery to an SBOM
// serializer, per specification section 36.1.
//
// A Document holds resolved facts only. It carries no format vocabulary: no
// bom-ref strings, no CycloneDX property keys, no spec-version conditionals.
// Identifiers a format needs are derived by that format's writer from the
// canonical paths and component identities here, so that adding SPDX or a
// later CycloneDX revision touches nothing below this package.
package sbomwriter

import (
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/example/sbomb/internal/domain"
)

// Relation is one edge of the product structure: which component or file a
// component is composed of. Both ends are component or file identities, never
// format-specific references.
type Relation struct {
	From string
	To   []string
}

// RunMetadata describes the run that produced the document.
type RunMetadata struct {
	ToolName    string
	ToolVendor  string
	ToolVersion string

	// Timestamp is RFC 3339 UTC, or empty in reproducible mode.
	Timestamp     string
	Reproducible  bool
	PolicyProfile string
	BuildConfig   string
	Generator     string
	Adapters      []string
}

// Document is everything a writer needs. Product is the root; Artifacts is
// non-empty only in assembly mode, where the product is a set of deliverables.
type Document struct {
	Product    domain.Component
	Artifacts  []domain.Component
	Components []domain.Component
	Files      []domain.UsedFile
	Relations  []Relation
	Findings   []domain.Finding
	Run        RunMetadata
}

// Options are the serialization choices a writer honours.
type Options struct {
	SpecVersion  string
	Reproducible bool
}

// Writer serializes a Document into one SBOM format.
type Writer interface {
	// ID names the format, e.g. "cyclonedx-json".
	ID() string
	// Versions lists the specification versions this writer can emit.
	Versions() []string
	// Write serializes the document.
	Write(w io.Writer, document *Document, options Options) error
	// Validate checks a previously written document.
	Validate(r io.Reader) error
}

var (
	registryMutex sync.RWMutex
	registry      = map[string]Writer{}
)

// Register makes a writer available to Get. Registering the same identifier
// twice is a programming error.
func Register(writer Writer) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	if _, taken := registry[writer.ID()]; taken {
		panic("sbomwriter: duplicate writer " + writer.ID())
	}
	registry[writer.ID()] = writer
}

// Get returns the writer for an identifier and specification version. An
// unknown value is a usage error that names what is available (section 36.1).
func Get(id, version string) (Writer, error) {
	registryMutex.RLock()
	defer registryMutex.RUnlock()
	writer, known := registry[id]
	if !known {
		return nil, fmt.Errorf("unknown output format %q; available: %s", id, joinIDs())
	}
	if version == "" {
		return writer, nil
	}
	for _, supported := range writer.Versions() {
		if supported == version {
			return writer, nil
		}
	}
	return nil, fmt.Errorf("format %q does not support version %q; supported: %v", id, version, writer.Versions())
}

// IDs lists the registered format identifiers, sorted.
func IDs() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func joinIDs() string {
	ids := IDs()
	if len(ids) == 0 {
		return "(none registered)"
	}
	out := ids[0]
	for _, id := range ids[1:] {
		out += ", " + id
	}
	return out
}
