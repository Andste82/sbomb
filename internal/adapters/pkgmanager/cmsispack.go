package pkgmanager

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// cmsisPack reads the pack descriptor an MCU vendor ships inside a
// CMSIS-Pack: a single `*.pdsc` lying directly in a directory that already
// stands as a component root.
//
// It is an enricher and not an adapter, and the difference is not a detail.
// A descriptor states the vendor, the pack name and the release history --
// nearly every field section 1.5(1) asks for, and for a vendor pack it is
// usually the only place any of them exist. It states none of that about a
// build: a pack is installed into CMSIS_PACK_ROOT or a per-user cache, this
// tool reads no environment variable to find such a place, and searching for
// one would be the downward scan the architecture refuses. So this reader
// describes a root something else already settled, and enumerates nothing.
type cmsisPack struct{}

func (cmsisPack) Source() string { return "cmsis-pack" }

// cmsisPackPattern is the only path this reader can ever open: one descriptor
// directly in the root it was handed. It is a glob rather than a fixed name
// because the name of a descriptor is the vendor's -- ARM.CMSIS.pdsc,
// Keil.STM32F4xx_DFP.pdsc -- and there is no name to stat for. Reading one
// already settled directory is not the downward search that is forbidden
// everywhere else, for the reason bundledsbom.go gives for its own globs.
const cmsisPackPattern = "*.pdsc"

// The bounds of section 30 for the descriptor. A vendor pack descriptor is
// mostly <devices>, <conditions> and <components> this tool has no use for,
// and it comes out of a tree this tool did not build, so all three shapes of
// excess are bounded: the file that is large, the file that is deep and the
// file that is short in bytes but pathological in structure.
//
// maxPDSCDepth is the recursion bound of section 30 point 2 and it has to be
// imposed here: Go's XML decoder keeps its element stack in the input and
// bounds the nesting by nothing, so a document nested two hundred thousand
// levels deep parses without complaint unless somebody counts.
// maxPDSCElements carries section 30's per-line token bound over to a format
// that has no lines.
const (
	maxPDSCBytes    = 4 << 20
	maxPDSCDepth    = limits.MaxDepth
	maxPDSCElements = 100_000
)

var (
	// errPDSCTooDeep and errPDSCTooManyElements are the two bounds the decoder
	// cannot report itself. They are values rather than messages so that the
	// caller can tell a breached limit from a document that is simply broken:
	// the two are different findings.
	errPDSCTooDeep         = errors.New("the pack descriptor nests deeper than the limit of section 30")
	errPDSCTooManyElements = errors.New("the pack descriptor holds more elements than the limit of section 30")
)

// pdscDocument is everything this reader takes out of a descriptor: who
// published the pack, what it is called, and the version of its newest
// release. Nothing else is kept, because nothing else may be published --
// see the licence note in read below.
type pdscDocument struct {
	vendor  string
	name    string
	version string
}

func (c cmsisPack) Enrich(root ComponentRoot) ([]Contribution, []domain.Finding) {
	path, findings := c.descriptor(root.Path)
	if path == "" {
		// Either there is no descriptor -- the ordinary case, and silence is
		// the contract: there is nothing missing about a component that is no
		// CMSIS pack -- or there were several and the finding says so.
		return nil, findings
	}
	document, readFindings := c.read(path)
	findings = append(findings, readFindings...)
	contributions := c.contributions(path, document)
	if len(contributions) == 0 && len(findings) == 0 {
		return nil, nil
	}
	return contributions, findings
}

// descriptor is the one descriptor lying directly in a root, or nothing.
//
// A directory holding two of them is refused rather than settled: a pack is
// one descriptor, so two mean this directory is a pack index or a cache of
// several packs, and reading either one would attribute somebody else's pack
// to this component. That is the same refusal the bundled-SBOM reader makes
// for a document that names several packages it describes.
func (cmsisPack) descriptor(dir string) (string, []domain.Finding) {
	matches, err := filepath.Glob(filepath.Join(dir, cmsisPackPattern))
	if err != nil || len(matches) == 0 {
		return "", nil
	}
	if len(matches) > 1 {
		return "", []domain.Finding{cmsisPackFinding("EVIDENCE_UNREADABLE", dir,
			"the component root holds more than one CMSIS pack descriptor, so which pack it is would be a guess and none of them was read")}
	}
	return matches[0], nil
}

// read applies one descriptor whole or refuses it whole. Half a document is
// not a weaker answer, it is an invented one: a vendor taken out of a file
// whose remainder could not be parsed would be published with nothing behind
// it.
//
// The `<license>` element is deliberately not among the values read. In this
// format it holds a path to a licence file inside the pack -- LICENSE.txt --
// and not an SPDX expression, so publishing it as one would put a filename
// where every consumer, and our own `licenses[].expression`, expects SPDX. Its
// only honest home would be Package.LicenseFile, which is a path, and the
// Enricher signature carries no paths by design (D33). The component keeps the
// licence the file in its root already gives it.
func (c cmsisPack) read(path string) (pdscDocument, []domain.Finding) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		// A name the glob returned that is not a readable file is not evidence
		// somebody meant to leave here, so there is nothing to report.
		return pdscDocument{}, nil
	}
	if info.Size() > maxPDSCBytes {
		// Asked before the file is opened, so a descriptor over the ceiling
		// costs a stat rather than four megabytes of allocation.
		return pdscDocument{}, []domain.Finding{cmsisPackFinding("INPUT_LIMIT_EXCEEDED", path,
			"the CMSIS pack descriptor is larger than the parser limit of section 30, so nothing was read from it")}
	}
	file, err := os.Open(path)
	if err != nil {
		return pdscDocument{}, []domain.Finding{cmsisPackFinding("EVIDENCE_UNREADABLE", path,
			"the CMSIS pack descriptor could not be read, so nothing was taken from it")}
	}
	defer func() { _ = file.Close() }()

	// The size above is the bound; the reader is limited as well so that the
	// ceiling still holds for a file that is being written while it is read.
	document, err := parsePDSC(io.LimitReader(file, maxPDSCBytes))
	switch {
	case errors.Is(err, errPDSCTooDeep), errors.Is(err, errPDSCTooManyElements):
		return pdscDocument{}, []domain.Finding{cmsisPackFinding("INPUT_LIMIT_EXCEEDED", path,
			"the CMSIS pack descriptor breaches a parser limit of section 30, so nothing was taken from it")}
	case err != nil:
		return pdscDocument{}, []domain.Finding{cmsisPackFinding("EVIDENCE_UNREADABLE", path,
			"the CMSIS pack descriptor could not be parsed as XML, so nothing was taken from it")}
	}
	if document.vendor == "" && document.version == "" {
		// A file with this ending that states neither a vendor nor a release
		// is not a pack descriptor. Reporting it keeps the missing values
		// traceable to the file rather than leaving them absent without
		// explanation, exactly as the bundled-SBOM reader reports a document
		// that describes no package.
		return pdscDocument{}, []domain.Finding{cmsisPackFinding("EVIDENCE_UNREADABLE", path,
			"the CMSIS pack descriptor states neither a vendor nor a release, so it describes no pack")}
	}
	return document, nil
}

// parsePDSC walks the document and keeps the three values this reader
// publishes. It streams tokens rather than unmarshalling into a struct for two
// reasons: only streaming can count the nesting depth section 30 bounds, and a
// real vendor descriptor is megabytes of device and component definitions that
// would otherwise be materialised for nothing.
//
// Four fields of the decoder are never assigned, and every one of them is a
// safety property rather than an omission:
//
//   - Entity stays nil, so the only entities that resolve are the five XML
//     predefines. An entity a document declares itself -- the nine nested
//     definitions of a billion-laughs bomb -- is then an undefined entity and
//     a syntax error, and nothing expands.
//   - Strict stays true, so that refusal is an error rather than the entity
//     being passed through as text.
//   - CharsetReader stays nil, so a document declaring an encoding other than
//     UTF-8 is refused instead of being reinterpreted under a guess.
//   - AutoClose stays nil, so no element is closed that the document did not
//     close itself.
//
// Setting any of them would silently reopen an expansion or a file read. The
// tests in cmsispack_test.go assert the refusals rather than the values, so
// that such an edit fails there.
func parsePDSC(reader io.Reader) (pdscDocument, error) {
	decoder := xml.NewDecoder(reader)
	var (
		document pdscDocument
		// path is the chain of element names from the root down to the token
		// being read. It is what makes /package/vendor different from a
		// <vendor> buried in a device definition.
		path []string
		// capture points at the value the text of the current element belongs
		// to, and captureDepth is the depth it was opened at, so that a child
		// element's text is never folded into its parent's value.
		capture      *string
		captureDepth int
		text         strings.Builder
		elements     int
		sawRelease   bool
	)
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return document, nil
		}
		if err != nil {
			return pdscDocument{}, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			elements++
			if elements > maxPDSCElements {
				return pdscDocument{}, errPDSCTooManyElements
			}
			if len(path) >= maxPDSCDepth {
				return pdscDocument{}, errPDSCTooDeep
			}
			path = append(path, element.Name.Local)
			switch {
			case document.vendor == "" && at(path, "package", "vendor"):
				capture, captureDepth = &document.vendor, len(path)
				text.Reset()
			case document.name == "" && at(path, "package", "name"):
				capture, captureDepth = &document.name, len(path)
				text.Reset()
			case !sawRelease && at(path, "package", "releases", "release"):
				// The newest release is the first <release> element and not
				// the largest version string: the pack schema requires the
				// history in descending order, so the first element is a
				// stated fact, while ordering these strings myself would mean
				// inventing comparison semantics for a scheme nothing here
				// defines. The version is an attribute; the element's text is
				// the release note.
				sawRelease = true
				for _, attribute := range element.Attr {
					if attribute.Name.Local == "version" {
						document.version = strings.TrimSpace(attribute.Value)
					}
				}
			}
		case xml.CharData:
			if capture != nil && len(path) == captureDepth {
				text.Write(element)
			}
		case xml.EndElement:
			if len(path) == 0 {
				// Unreachable through the strict decoder, which refuses an end
				// tag that closes nothing. It is guarded anyway, because the
				// alternative to a guard here is a panic on input this tool
				// did not write.
				return pdscDocument{}, errors.New("the pack descriptor closes an element that was never opened")
			}
			if capture != nil && len(path) == captureDepth {
				*capture = strings.TrimSpace(text.String())
				capture = nil
			}
			path = path[:len(path)-1]
		}
	}
}

// at reports whether the element path is exactly the one named. Namespace
// prefixes do not enter into it: Go reports local names, so a descriptor
// written with a prefix reads the same as one without.
func at(path []string, names ...string) bool {
	if len(path) != len(names) {
		return false
	}
	for i, name := range names {
		if path[i] != name {
			return false
		}
	}
	return true
}

// contributions turns the descriptor into claims.
//
// The version has two possible origins and they are ranked differently. What
// the release history declares is a manifest (rank 2). What the pack installer
// actually put on disk is install state (rank 3), and CMSIS states it in the
// layout: <Vendor>/<Name>/<version>/<Vendor>.<Name>.pdsc. Where both are known
// and they differ, both are made, so that the ranking decides and the loser is
// kept for the report; where they agree there is nothing to record twice, so
// one claim is made and it carries the stronger origin.
//
// The vendor becomes the supplier, which section 20.5 permits because a
// manager states it -- a descriptor states it outright rather than it being
// inferred from a repository host. No purl is built at all: the purl
// specification registers no type for CMSIS packs, and inventing one would put
// an identifier into the document that nothing can resolve (section 20.4,
// D38). Such a component keeps UNKNOWN_PURL.
func (c cmsisPack) contributions(path string, document pdscDocument) []Contribution {
	contributions := make([]Contribution, 0, 3)
	add := func(field Field, value string, rank Rank, confidence domain.Confidence) {
		if value == "" {
			return
		}
		contributions = append(contributions, Contribution{
			Field: field,
			Claim: Claim{
				Value: value,
				// Source stays empty: every claim of this reader has the same
				// origin, and the wiring publishes it under Source(), so the
				// name is spelled in one place.
				Rank:       rank,
				Confidence: confidence,
			},
		})
	}
	installed := c.installedVersion(path, document)
	add(FieldVersion, installed, RankInstallState, domain.ConfidenceHigh)
	if document.version != installed {
		add(FieldVersion, document.version, RankDeclaredManifest, domain.ConfidenceHigh)
	}
	add(FieldSupplier, document.vendor, RankDeclaredManifest, "")
	return contributions
}

// installedVersion is the version the pack installer wrote into the layout, or
// the empty string where the layout does not state one.
//
// It reads the settled root's own path and nothing else: no directory is
// listed, no file is opened and nothing is walked. The three names have to
// agree with what the descriptor says about itself before a single directory
// name counts as evidence -- a pack unpacked into some other directory
// structure states nothing about its version by lying there.
func (cmsisPack) installedVersion(path string, document pdscDocument) string {
	if document.vendor == "" || document.name == "" {
		return ""
	}
	if filepath.Base(path) != document.vendor+"."+document.name+".pdsc" {
		return ""
	}
	versionDir := filepath.Dir(path)
	nameDir := filepath.Dir(versionDir)
	vendorDir := filepath.Dir(nameDir)
	if filepath.Base(nameDir) != document.name || filepath.Base(vendorDir) != document.vendor {
		return ""
	}
	version := filepath.Base(versionDir)
	if version == "." || version == string(filepath.Separator) || version == document.name {
		return ""
	}
	return version
}

// cmsisPackFinding reports a descriptor that was found but not used. The
// subject is the descriptor rather than the component: what could not be read
// is a piece of evidence, and the component is still described by whatever
// else states it.
func cmsisPackFinding(id, path, message string) domain.Finding {
	return domain.Finding{
		ID: id, Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: path},
		Message: message,
	}
}
