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
	"strings"
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

// Options are the serialization choices a writer honours. An empty
// SpecVersion means the writer's DefaultVersion; it is never a licence to
// guess.
type Options struct {
	SpecVersion string
	// TLP is the Traffic Light Protocol classification the document carries,
	// empty when it carries none. It is a distribution constraint on the
	// document rather than a fact about the build, which is why it is a
	// serialization choice and not part of the Document.
	TLP          string
	Reproducible bool
}

// Writer serializes a Document into one SBOM format.
//
// One writer per serialization format; the specification versions live inside
// it. A consumer asks for a format, not for a shape, so CycloneDX 1.6 and 1.7
// are one writer. Where two versions of a format share nothing at the document
// level, "one writer" must not become one function with a switch at the top:
// it dispatches to a renderer per version over a shared mapping layer, and
// that mapping is the reuse rather than the serialization.
type Writer interface {
	// ID names the format, e.g. "cyclonedx-json".
	ID() string
	// Versions lists the specification versions this writer can emit.
	Versions() []string
	// DefaultVersion is the version written when the caller has no opinion.
	// It is stated rather than derived: agreeing that Versions()[0] is special
	// breaks the first time somebody reorders a slice.
	DefaultVersion() string
	// Write serializes the document.
	Write(w io.Writer, document *Document, options Options) error
	// Validate checks a previously written document.
	Validate(r io.Reader) error
}

// Detector is implemented by a writer that can recognise a document in its own
// format. It is what lets `validate` read a file somebody sent without being
// told what is in it.
type Detector interface {
	// Detect reports whether the document is in this writer's format and, if
	// so, which specification version it declares. The version is reported
	// even when it is one this writer cannot emit, so that a caller can say
	// "CycloneDX 1.4" rather than "unrecognised".
	Detect(data []byte) (version string, ok bool)
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
		return nil, fmt.Errorf("unknown output format %q; available: %s", id, commaList(sortedIDs()))
	}
	if version == "" {
		return writer, nil
	}
	for _, supported := range writer.Versions() {
		if supported == version {
			return writer, nil
		}
	}
	return nil, fmt.Errorf("format %q does not support version %q; supported: %s", id, version, commaList(writer.Versions()))
}

// Resolve returns the writer for an identifier and the specification version
// to write, filling in the writer's default when the caller named none. Every
// call site goes through here, so there is exactly one place where "no version
// given" becomes a version.
func Resolve(id, version string) (Writer, string, error) {
	writer, err := Get(id, version)
	if err != nil {
		return nil, "", err
	}
	if version == "" {
		version = writer.DefaultVersion()
	}
	return writer, version, nil
}

// DetectFormat returns the writer whose format a document is in, and the
// specification version the document declares. A document in no registered
// format is a usage error that names what can be read.
func DetectFormat(data []byte) (Writer, string, error) {
	registryMutex.RLock()
	ids := sortedIDs()
	writers := make([]Writer, 0, len(ids))
	for _, id := range ids {
		writers = append(writers, registry[id])
	}
	registryMutex.RUnlock()

	for _, writer := range writers {
		detector, canDetect := writer.(Detector)
		if !canDetect {
			continue
		}
		if version, recognized := detector.Detect(data); recognized {
			return writer, version, nil
		}
	}
	return nil, "", fmt.Errorf("the document is in no format this build can read; readable: %s", commaList(ids))
}

// IDs lists the registered format identifiers, sorted.
func IDs() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()
	return sortedIDs()
}

// sortedIDs lists the registered identifiers. The caller holds the lock.
func sortedIDs() []string {
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// commaList renders what a usage error offers instead -- format identifiers or
// specification versions.
func commaList(values []string) string {
	if len(values) == 0 {
		return "(none registered)"
	}
	return strings.Join(values, ", ")
}
