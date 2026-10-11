// Package spdx is the registered writer "spdx-json": SPDX documents in their
// JSON serialization, one writer for every SPDX version this build knows.
//
// A consumer asks for a format, not for a shape, so SPDX 3.0.1 and SPDX 2.3
// are one writer. The two versions share nothing at the serialization level,
// so "one writer" is not one function with a switch: Write maps the document
// once into the version-neutral model of package mapping, and dispatches to
// the renderer of the requested version. Adding a version is a renderer, its
// checks, and one entry of the dispatch table.
package spdx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/sbommap"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/spdx/mapping"
	"github.com/example/sbomb/internal/spdx/spdx23"
	"github.com/example/sbomb/internal/spdx/spdx3"
	"github.com/google/uuid"
)

// ID is the registry key of this writer.
const ID = "spdx-json"

// Version301 is SPDX 3.0.1.
const Version301 = "3.0.1"

// Version23 is SPDX 2.3.
const Version23 = "2.3"

// Writer writes and reads SPDX JSON documents.
type Writer struct{}

func init() { sbomwriter.Register(Writer{}) }

func (Writer) ID() string { return ID }

// Versions lists what this writer can emit: the versions of the dispatch
// table, in order.
func (Writer) Versions() []string {
	versions := make([]string, 0, len(renderers))
	for version := range renderers {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	return versions
}

// DefaultVersion is the newest SPDX this build writes. Unlike CycloneDX there
// is no compliance target that pins an older one.
func (Writer) DefaultVersion() string { return Version301 }

// renderer is everything about one SPDX version: how it is written, the two
// tiers of checks, the schema, how a component's files are read back, and the
// things the shared mapping and the run have to know about the version --
// whether it can name a custom licence exception
// (mapping.Options.CustomAdditions), which digests every file must carry
// beyond SHA-256 (FileDigests, mapping.Options.Digests), and whether every
// licence reference must be defined in the document itself
// (mapping.Options.SelfContainedLicenseRefs).
type renderer struct {
	render            func(io.Writer, *mapping.Model, string) error
	validate          func([]byte) error
	check             func([]byte) error
	validateOwn       func([]byte) error
	schema            func() []byte
	read              func([]byte, string) ([]string, error)
	customAdditions   bool
	fileDigests       []string
	selfContainedRefs bool
}

// renderers is the dispatch table: one entry per version this build writes
// and reads. Versions, the version a document is read as, and the version a
// document is written in all come from here and from nowhere else.
var renderers = map[string]renderer{
	Version301: {
		render:          spdx3.Render,
		validate:        spdx3.Validate,
		check:           spdx3.CheckOwnOutput,
		validateOwn:     spdx3.ValidateOwnOutput,
		schema:          spdx3.SchemaJSON,
		read:            spdx3.ComponentFiles,
		customAdditions: true,
	},
	// SPDX 2.3 has no AdditionRef, requires a SHA-1 of every file (clause
	// 8.4), and resolves a LicenseRef only through its own
	// hasExtractedLicensingInfos or a checksummed external document.
	Version23: {
		render:            spdx23.Render,
		validate:          spdx23.Validate,
		check:             spdx23.CheckOwnOutput,
		validateOwn:       spdx23.ValidateOwnOutput,
		schema:            spdx23.SchemaJSON,
		read:              spdx23.ComponentFiles,
		customAdditions:   false,
		fileDigests:       []string{"SHA-1"},
		selfContainedRefs: true,
	},
}

func rendererFor(version string) (renderer, string, error) {
	if version == "" {
		version = Writer{}.DefaultVersion()
	}
	r, known := renderers[version]
	if !known {
		return renderer{}, "", fmt.Errorf("unsupported SPDX version %q; supported: %s", version, strings.Join(Writer{}.Versions(), ", "))
	}
	return r, version, nil
}

// Write maps the document and renders it in the requested version.
//
// The document namespace -- the UUID every element IRI shares -- is the
// document's identity. Under reproducible it is derived from the canonical
// encoding of the model (section 28.11.2), which contains the creation time,
// the spec version and the tool version: the same evidence pinned to the same
// SOURCE_DATE_EPOCH by the same build gives the same document, byte for byte,
// and anything else gives another identity. Otherwise it is random, as a
// CycloneDX serial number is.
func (Writer) Write(w io.Writer, document *sbomwriter.Document, options sbomwriter.Options) error {
	r, version, err := rendererFor(options.SpecVersion)
	if err != nil {
		return err
	}
	if options.TLP != "" {
		// Preflight refuses this before discovery; refusing again here keeps
		// a caller that skipped it from writing a document that silently
		// lost its distribution constraint.
		return tlpRefusal(version)
	}
	model, err := mapping.Build(document, mapping.Options{
		SpecVersion:              version,
		LicenseText:              options.LicenseText,
		Reproducible:             options.Reproducible,
		CustomAdditions:          r.customAdditions,
		Digests:                  r.fileDigests,
		SelfContainedLicenseRefs: r.selfContainedRefs,
	})
	if err != nil {
		return err
	}
	namespace := uuid.NewString()
	if options.Reproducible {
		namespace = sbommap.DocumentUUID(mapping.Digest(model))
	}
	return r.render(w, model, namespace)
}

// Validate checks a document against the specification it declares: the
// schema and the conformance rules of that version (tier a). It does not hold
// the document to how sbomb writes; a correct document somebody else wrote
// passes.
func (w Writer) Validate(reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	r, err := w.readerFor(data)
	if err != nil {
		return err
	}
	return r.validate(data)
}

// CheckOutput holds a document sbomb wrote to the invariants of its own
// output (tier b), on top of Validate.
func (w Writer) CheckOutput(data []byte) error {
	r, err := w.readerFor(data)
	if err != nil {
		return err
	}
	return r.check(data)
}

// ValidateOutput is Validate and CheckOutput over one decoding of a document
// sbomb wrote.
func (w Writer) ValidateOutput(data []byte) error {
	r, err := w.ownReaderFor(data)
	if err != nil {
		return err
	}
	if r.validateOwn == nil {
		if err := r.validate(data); err != nil {
			return err
		}
		return r.check(data)
	}
	return r.validateOwn(data)
}

// ComponentFiles reads the files a component contains back out of a document.
func (w Writer) ComponentFiles(data []byte, name string) ([]string, error) {
	r, err := w.readerFor(data)
	if err != nil {
		return nil, err
	}
	return r.read(data, name)
}

// FileDigests names the digests a document of this version must state for
// every file beyond SHA-256 (sbomwriter.DigestRequirer): a SHA-1 for SPDX 2.3,
// nothing for 3.0.1. An unknown version needs nothing; Write refuses it.
func (Writer) FileDigests(version string) []string {
	r, _, err := rendererFor(version)
	if err != nil {
		return nil
	}
	return append([]string(nil), r.fileDigests...)
}

// ownReaderFor is readerFor for a document sbomb wrote. sbomb writes the
// version first -- @context in 3.0.1, spdxVersion in 2.3 -- so the version is
// read from the head of the document without scanning the rest, which Detect
// does to read a document of any shape. A document that does not start that
// way is detected as any other.
func (w Writer) ownReaderFor(data []byte) (renderer, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if open, err := decoder.Token(); err == nil && open == json.Delim('{') {
		key, err := decoder.Token()
		version, ok := "", false
		switch {
		case err != nil:
		case key == "@context":
			var context json.RawMessage
			if decoder.Decode(&context) == nil {
				version, ok = contextVersion(context)
			}
		case key == "spdxVersion":
			var declared string
			if decoder.Decode(&declared) == nil {
				if match := spdx2Pattern.FindStringSubmatch(declared); match != nil {
					version, ok = match[1], true
				}
			}
		}
		if r, known := renderers[version]; ok && known {
			return r, nil
		}
	}
	return w.readerFor(data)
}

// LocalIdentity turns an element identifier of sbomb's SPDX documents back
// into the local identity it was built from: an SPDX 2.3 identifier
// (SPDXRef-...), or an SPDX 3.0.1 IRI (urn:uuid:<U>#<local>). Anything else
// is returned as it is. It is what the command line reads a reference a
// person typed with, so that every version is decoded by the rule its reader
// uses.
func LocalIdentity(reference string) string {
	if local, ok := spdx23.LocalIdentity(reference); ok {
		return local
	}
	return spdx3.LocalIdentity(reference)
}

// readerFor is the renderer of the version a document declares, or a refusal
// that names what the document is.
func (w Writer) readerFor(data []byte) (renderer, error) {
	version, recognised := w.Detect(data)
	if !recognised {
		return renderer{}, fmt.Errorf("the document is not an SPDX JSON document this build can recognise")
	}
	// The table is consulted first: a version that has a renderer is read
	// by it, whatever its major version. The two refusals below are only for
	// what the table does not hold.
	if r, known := renderers[version]; known {
		return r, nil
	}
	if spdx2Version.MatchString(version) {
		return renderer{}, fmt.Errorf("SPDX %s documents are not read by this build; it reads SPDX %s", version, strings.Join(w.Versions(), ", "))
	}
	return renderer{}, fmt.Errorf("the document is SPDX %s; this build reads SPDX %s", version, strings.Join(w.Versions(), ", "))
}

var (
	contextPattern = regexp.MustCompile(`^https://spdx\.org/rdf/([0-9]+\.[0-9]+(?:\.[0-9]+)?)/spdx-context\.jsonld$`)
	spdx2Pattern   = regexp.MustCompile(`^SPDX-([0-9]+\.[0-9]+)$`)
	spdx2Version   = regexp.MustCompile(`^2\.`)
)

// Detect recognises an SPDX JSON document and reports the version it
// declares, including one this build cannot read, so that validate can say
// "SPDX 3.0.0" or "SPDX 2.3" rather than "unrecognised":
//
//   - an SPDX 3 document by its @context, the context URL of its version --
//     as a string, or as the first entry of an array, which is how 3.0
//     documents declare prefixes and which the 3.0.1 schema then refuses;
//   - an SPDX 2 document by its spdxVersion, "SPDX-2.3".
//
// The two cannot be confused with CycloneDX, which is recognised by a
// bomFormat field neither has.
func (Writer) Detect(data []byte) (string, bool) {
	var header struct {
		Context     json.RawMessage `json:"@context"`
		SpdxVersion string          `json:"spdxVersion"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return "", false
	}
	if version, ok := contextVersion(header.Context); ok {
		return version, true
	}
	if match := spdx2Pattern.FindStringSubmatch(header.SpdxVersion); match != nil {
		return match[1], true
	}
	return "", false
}

func contextVersion(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}
	var context string
	if json.Unmarshal(raw, &context) != nil {
		var entries []json.RawMessage
		if json.Unmarshal(raw, &entries) != nil || len(entries) == 0 || json.Unmarshal(entries[0], &context) != nil {
			return "", false
		}
	}
	match := contextPattern.FindStringSubmatch(context)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// Label is how a person names the format.
func (Writer) Label() string { return "SPDX" }

// Extension is the conventional suffix of an SPDX JSON document.
func (Writer) Extension() string { return ".spdx.json" }

// OmitsTimestamp is false at every version: SPDX requires a creation time, so
// a reproducible document states SOURCE_DATE_EPOCH rather than dropping the
// time, and REPRODUCIBLE_MODE_OMITS_TIMESTAMP is not true of it.
func (Writer) OmitsTimestamp(version string, options sbomwriter.Options) bool { return false }

// EmbeddedSchema serves the schema a document of one version is checked
// against.
func (Writer) EmbeddedSchema(version string) ([]byte, error) {
	r, _, err := rendererFor(version)
	if err != nil {
		return nil, err
	}
	return r.schema(), nil
}

// Preflight refuses, before discovery, the two requests no SPDX document can
// honour, in either version:
//
//   - a TLP classification. Neither SPDX 2.3 nor 3.0.1 has a field that
//     constrains the distribution of the document. The nearest, 3.0.1's
//     dataset_confidentialityLevel, classifies a dataset package and not the
//     SBOM; carrying the value in an
//     extension or an annotation would mark a document AMBER that every tool
//     treats as unmarked, which is worse than refusing.
//   - reproducible mode (--reproducible or output.reproducible) without a
//     readable SOURCE_DATE_EPOCH. The creation time is mandatory, and the
//     only time a second run would state again is the pinned one; the Unix
//     epoch would state a creation date that is never true, in a document
//     produced for compliance.
func (Writer) Preflight(version string, options sbomwriter.Options) error {
	if version == "" {
		version = Writer{}.DefaultVersion()
	}
	if options.TLP != "" {
		return tlpRefusal(version)
	}
	if options.Reproducible {
		pinned, err := sbomwriter.SourceDateEpoch()
		if err != nil || pinned == "" {
			// Reproducible mode comes from the flag or from the project's
			// configuration, and Preflight cannot tell which; the message
			// names both, so that it never tells a user to drop a flag they
			// did not pass.
			message := "SPDX " + version + " requires a creation time, and reproducible mode (--reproducible or output.reproducible) without a readable SOURCE_DATE_EPOCH has none to state; " +
				"set SOURCE_DATE_EPOCH (for example to the commit time) or turn reproducible mode off"
			if err != nil {
				message += " (" + err.Error() + ")"
			}
			return &sbomwriter.RefusalError{ID: "REPRODUCIBLE_CREATION_TIME_MISSING", Message: message}
		}
	}
	return nil
}

func tlpRefusal(version string) error {
	return fmt.Errorf("output.tlp cannot be carried by SPDX %s, which has no field for a distribution constraint on the document; write CycloneDX 1.7 to carry it", version)
}
