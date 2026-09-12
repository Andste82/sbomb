// Package foss renders the FOSS attribution documents of specification
// section 32.6.
//
// It is a writer and nothing else (decision Q21): it reads a resolved
// sbomwriter.Document and touches no evidence, opens no file of the build, and
// decides nothing about a licence that the component mapping has not already
// decided. Everything it prints was collected by the run that produced the
// document, which is why `generate --foss-out` and `sbomb foss` are one code
// path and neither runs discovery twice.
//
// Four files, one of which ships with the product:
//
//	THIRD-PARTY-NOTICES.txt   the shippable attribution document
//	foss-review.txt           the internal record, human-readable
//	foss-review.json          the same facts, machine-readable
//	source-obligations.txt    which components owe source material, and why
//
// What it does not do is as much of the design as what it does. There is no
// status column, no OK, no compliance verdict, and no exit code depends on
// licence content: sbomb produces data and the manufacturer performs the
// assessment.
package foss

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

// The four file names, spelled once. They are constants rather than
// parameters because the structural guarantee below is stated over this list.
const (
	NoticesFile     = "THIRD-PARTY-NOTICES.txt"
	ReviewTextFile  = "foss-review.txt"
	ReviewJSONFile  = "foss-review.json"
	ObligationsFile = "source-obligations.txt"
)

// Files is the four names, in the order the documents are written. A caller
// that has to say what a run produced reads them from here.
func Files() []string {
	return []string{NoticesFile, ReviewTextFile, ReviewJSONFile, ObligationsFile}
}

// Format values for the --format flag of section 32.6.
const (
	FormatText     = "text"
	FormatMarkdown = "markdown"
)

// forbiddenSuffixes is the structural guarantee of section 32.6, enforced
// rather than promised: no source file, archive or patch is ever written into
// the output directory.
//
// Corresponding Source is the complete source of the work plus the scripts
// that control compilation and installation. An evidence-derived subset would
// look like a source offer while being materially incomplete, which turns the
// tool's precision into a compliance defect (requirement R6). The list is
// checked in the one function that creates a file here, so a future output
// cannot slip past it by being added somewhere else.
var forbiddenSuffixes = []string{".c", ".h", ".cpp", ".hpp", ".s", ".zip", ".patch"}

// View is what the licence view of section 32.6 adds over the view the
// document was built from. The renderer is handed it rather than computing it:
// reading a narrowed header is evidence work, and this package does none.
type View struct {
	// Deltas is the difference per component, ordered by component name.
	Deltas []ViewDelta
	// NarrowedTotal is every header DWARF narrowing removed in the run,
	// including the components no FOSS output mentions. It is the
	// "licence view vs. SBOM view" number of the completeness block.
	NarrowedTotal int
}

// ViewDelta is one component's share of the difference between the two views.
type ViewDelta struct {
	Component  string
	Narrowed   int
	Copyrights []domain.CopyrightStatement
}

func (v View) deltaFor(name string) ViewDelta {
	for _, delta := range v.Deltas {
		if delta.Component == name {
			return delta
		}
	}
	return ViewDelta{}
}

// Input is everything the four documents describe. It is the same shape for
// both entry points, which is what makes them byte-identical.
type Input struct {
	// Document is the resolved document the run produced. The FOSS outputs
	// never change it, and this package never writes to it.
	Document *sbomwriter.Document
	// Findings are the run's findings after policy annotation, so that a
	// waived finding arrives here with its reason. A waiver never changes
	// THIRD-PARTY-NOTICES.txt (section 26.3); it is visible in the review
	// record and nowhere else.
	Findings []domain.Finding
	// View is the licence-view delta, zero when the caller did not compute it.
	View View
	// Profile, Mode and HeaderEvidence are the run block of the review
	// record. There is no build directory among them on purpose: see
	// runBlock.
	Profile        string
	Mode           string
	HeaderEvidence string
	// Format selects the rendering of the three text documents.
	Format string
	// SpecVersion and TLP are handed to the writer registry for
	// foss-review.json; Reproducible is the run's mode.
	SpecVersion  string
	TLP          string
	Reproducible bool
}

// Write renders the four documents into a directory, creating it if it is not
// there.
//
// It overwrites its four names and nothing else: a directory that already
// holds other files is not refused and nothing in it is deleted, exactly as
// --output overwrites an SBOM (decision Q14). A CI job whose output directory
// already exists is the normal case, not an error.
func Write(directory string, in Input) error {
	if in.Document == nil {
		return fmt.Errorf("foss: no document to render")
	}
	format := in.Format
	if format == "" {
		format = FormatText
	}
	if format != FormatText && format != FormatMarkdown {
		return fmt.Errorf("foss: invalid format %q; use %s or %s", in.Format, FormatText, FormatMarkdown)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	model := build(in)
	rendering := style{markdown: format == FormatMarkdown}

	// The names do not depend on the format. --format chooses how the three
	// text documents read, exactly as --report-format does for the review
	// report, and a CI job that collects <out>/THIRD-PARTY-NOTICES.txt keeps
	// working when somebody switches the rendering.
	if err := writeDocument(directory, NoticesFile, renderNotices(model, rendering)); err != nil {
		return err
	}
	if err := writeDocument(directory, ReviewTextFile, renderReview(model, rendering)); err != nil {
		return err
	}
	if err := writeDocument(directory, ObligationsFile, renderObligations(model, rendering)); err != nil {
		return err
	}
	return writeReviewJSON(directory, in)
}

// writeReviewJSON renders the document through the writer registry, with the
// retained licence texts included.
//
// It is deliberately not a schema of sbomb's own: section 36.1 requires the
// writer layer to be format-agnostic so that SPDX can be added without
// touching anything below it, and a hand-rolled JSON review format beside it
// would violate exactly that rule. When the SPDX writer arrives, this file is
// produced by it and this intermediate form is retired rather than kept.
func writeReviewJSON(directory string, in Input) error {
	writer, specVersion, err := sbomwriter.Resolve("cyclonedx-json", in.SpecVersion)
	if err != nil {
		return err
	}
	var rendered strings.Builder
	// The retained text always reaches the FOSS outputs and reaches the
	// document only when the policy says so (decision B1). This is a FOSS
	// output, so it carries the text whatever licenseTextInSBOM says.
	err = writer.Write(&rendered, in.Document, sbomwriter.Options{
		SpecVersion:  specVersion,
		TLP:          in.TLP,
		LicenseText:  sbomwriter.LicenseTextEvidence,
		Reproducible: in.Reproducible,
	})
	if err != nil {
		return err
	}
	return writeDocument(directory, ReviewJSONFile, rendered.String())
}

// writeDocument is the only function in this package that creates a file, and
// the only place the structural guarantee has to hold. A name outside the four
// is an internal error rather than a silent write: the guarantee is about what
// the directory contains, so it is checked where the containing happens.
func writeDocument(directory, name, content string) error {
	if err := permittedName(name); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, name), []byte(content), 0o644)
}

// permittedName refuses anything that is not one of the documents this
// package writes, and anything that looks like source, an archive or a patch.
func permittedName(name string) error {
	switch name {
	case NoticesFile, ReviewTextFile, ReviewJSONFile, ObligationsFile:
	default:
		return fmt.Errorf("foss: %q is not one of the documents of section 32.6", name)
	}
	lower := strings.ToLower(name)
	for _, suffix := range forbiddenSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return fmt.Errorf("foss: refusing to write %q: the output is not a source offer (section 32.6)", name)
		}
	}
	if strings.Contains(lower, ".tar") {
		return fmt.Errorf("foss: refusing to write %q: the output is not a source offer (section 32.6)", name)
	}
	return nil
}
