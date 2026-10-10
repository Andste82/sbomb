package sbomwriter

import "errors"

// The interfaces below are optional, and type-asserted like Detector. Each one
// is a question the command line has to be able to ask about a format without
// naming it: what a person calls it, which schema a document is checked
// against, whether a request can be honoured at all, and what a document says
// a component contains. Keeping them off Writer means a format answers only
// the questions it has an answer to, and a caller that asks one of a writer
// that has none says so rather than guessing.

// Describer names a format the way a person does, and states the conventions
// that differ between formats without being part of either document.
type Describer interface {
	// Label is the name a person writes, e.g. "CycloneDX" or "SPDX". The
	// validate output and the log lines use it rather than the registry key.
	Label() string
	// Extension is the file-name suffix of a document in this format, e.g.
	// ".cdx.json". The default output names are built from it.
	Extension() string
	// OmitsTimestamp reports whether a reproducible document of this version
	// states no creation time. It decides REPRODUCIBLE_MODE_OMITS_TIMESTAMP:
	// the finding says the document is not a CRA deliverable, which is true
	// only of a format that drops the time to be reproducible.
	OmitsTimestamp(version string, options Options) bool
}

// SchemaProvider serves the schema a document of one version is checked
// against, so that a user can see exactly what that is. An empty version is
// the writer's DefaultVersion.
type SchemaProvider interface {
	EmbeddedSchema(version string) ([]byte, error)
}

// Preflighter refuses, before discovery, a request the writer cannot honour,
// so that nothing is read on the strength of an argument that fails at the
// end. It is given the resolved version and the options the run will write
// with, and needs nothing from the run itself.
type Preflighter interface {
	Preflight(version string, options Options) error
}

// ComponentReader answers the format-specific half of explain --component
// --sbom: which file identities a document says the component of this name
// contains. Reading the file, recognising its format and every message to the
// user stay with the caller, so that both formats are explained in the same
// words.
//
// A document that does not name the component answers ErrNoSuchComponent, so
// that "not in there" stays distinguishable from "cannot be read".
type ComponentReader interface {
	ComponentFiles(data []byte, name string) ([]string, error)
}

// ErrNoSuchComponent is what a ComponentReader answers for a name the
// document does not carry.
var ErrNoSuchComponent = errors.New("the document names no such component")

// OutputChecker holds a writer's own output to the invariants of how it
// writes, beyond what the format requires of any document. WriteFile and the
// golden tests call it; validate does not, because a conformant document
// somebody else wrote need not have been written the way sbomb writes.
type OutputChecker interface {
	CheckOutput(data []byte) error
}

// RefusalError is a Preflight refusal that carries an appendix A finding, so
// that the command line can print the identifier a user looks up rather than
// only the sentence.
type RefusalError struct {
	ID      string
	Message string
}

func (e *RefusalError) Error() string { return e.ID + ": " + e.Message }
