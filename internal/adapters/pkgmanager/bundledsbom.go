package pkgmanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/domain"
)

// bundledSBOM reads an SBOM the upstream shipped inside a package: a
// CycloneDX *.cdx.json or an SPDX 2.x *.spdx.json lying directly in a
// directory that already stands as a component root.
//
// It is an enricher and not an adapter, and that difference is the point.
// Such a document is the strongest declared origin this tool has -- rank 4 of
// section 21.1, above every manifest and every install state, because the
// people who ship the package wrote it and their tooling checked it, instead
// of us interpreting a manifest format -- and it is still only a description.
// It cannot make a directory a component and it cannot add a file to the used
// set. The dependencies it lists are no evidence that any of them was linked,
// which is why only the one component the document is about is read.
type bundledSBOM struct{}

func (bundledSBOM) Source() string { return "bundled-sbom" }

// bundledSBOMPatterns are the two conventional endings of the two formats.
// They are globs rather than a fixed list of names because the name of such a
// document is not standardised -- upstreams ship sbom.cdx.json, <name>.cdx.json
// and <name>-<version>.spdx.json alike -- and this reads one directory that
// something else has already settled as a component root, which is not the
// downward search for files that is forbidden everywhere else.
var bundledSBOMPatterns = []string{"*.cdx.json", "*.spdx.json"}

// vcpkgDocumentName is the one document this reader must not touch. vcpkg
// writes it itself for every port it installs, so it is that manager's install
// state (rank 3) and not something an upstream shipped. Reading it here would
// let a generic reader outrank, at rank 4, the very adapter that installed the
// package, and quietly relabel the origin of every vcpkg component.
const vcpkgDocumentName = "vcpkg.spdx.json"

// maxBundledDocuments bounds how many documents one root may contribute
// (section 30). A component root holding more than a handful of SBOMs is not a
// package describing itself; the first few by name are read and the rest is
// reported rather than parsed.
const maxBundledDocuments = 4

func (b bundledSBOM) Enrich(root ComponentRoot) ([]Contribution, []domain.Finding) {
	candidates, findings := b.documents(root.Path)
	contributions := make([]Contribution, 0)
	for _, path := range candidates {
		found, readFindings := b.read(path)
		contributions = append(contributions, found...)
		findings = append(findings, readFindings...)
	}
	if len(contributions) == 0 && len(findings) == 0 {
		// A component root that carries no SBOM is the ordinary case, and
		// silence is the contract: there is nothing missing about a package
		// that simply did not ship one.
		return nil, nil
	}
	return contributions, findings
}

// documents lists the SBOMs lying directly in a root, in a fixed order.
// Sorting by path is behaviour and not presentation: Take keeps the first of
// two equally ranked claims, so two documents disagreeing about a version are
// settled by this order and by nothing else.
func (bundledSBOM) documents(dir string) ([]string, []domain.Finding) {
	candidates := make([]string, 0)
	for _, pattern := range bundledSBOMPatterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			continue
		}
		for _, path := range matches {
			if filepath.Base(path) == vcpkgDocumentName {
				continue
			}
			candidates = append(candidates, path)
		}
	}
	sort.Strings(candidates)
	if len(candidates) <= maxBundledDocuments {
		return candidates, nil
	}
	return candidates[:maxBundledDocuments], []domain.Finding{bundledSBOMFinding("INPUT_LIMIT_EXCEEDED", dir,
		"the component root holds more SBOM documents than the parser limit of section 30 allows, so the ones after the first few by name were not read")}
}

// read applies one document whole or refuses it whole. Half a document is not
// a weaker answer, it is an invented one: a version taken out of a file whose
// remainder could not be parsed would be published at rank 4 with nothing
// behind it.
func (b bundledSBOM) read(path string) ([]Contribution, []domain.Finding) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		// A name the glob returned that is not a readable file is not evidence
		// somebody meant to leave here, so there is nothing to report.
		return nil, nil
	}
	if info.Size() > maxSPDXBytes {
		return nil, []domain.Finding{bundledSBOMFinding("INPUT_LIMIT_EXCEEDED", path,
			"the bundled SBOM is larger than the parser limit of section 30, so nothing was read from it")}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []domain.Finding{bundledSBOMFinding("EVIDENCE_UNREADABLE", path,
			"the bundled SBOM could not be read, so nothing was taken from it")}
	}
	if strings.HasSuffix(path, ".cdx.json") {
		return b.readCycloneDX(path, data)
	}
	return b.readSPDX(path, data)
}

// readCycloneDX reads the document's own root component and nothing else.
//
// metadata.component is what the document says it is about; components[] is
// what that package depends on. Taking the latter would put components into
// this run that no link, no object and no header ever reached -- the one rule
// this package exists to keep -- so BOM.Components is never looked at, however
// complete it may be.
func (b bundledSBOM) readCycloneDX(path string, data []byte) ([]Contribution, []domain.Finding) {
	var bom cyclonedx.BOM
	if err := json.Unmarshal(data, &bom); err != nil {
		return nil, []domain.Finding{bundledSBOMFinding("EVIDENCE_UNREADABLE", path,
			"the bundled SBOM is not a CycloneDX JSON document, so nothing was taken from it")}
	}
	// What makes a document CycloneDX is the writer's question, and it already
	// answers it for `validate`. The specification version it reports is not
	// checked: the four fields read below are spelled the same way in every
	// version of the format, so refusing a 1.4 document over a schema this
	// reader never consults would throw away evidence for nothing.
	if _, ok := (cyclonedx.Writer{}).Detect(data); !ok {
		return nil, []domain.Finding{bundledSBOMFinding("EVIDENCE_UNREADABLE", path,
			"the document does not declare bomFormat CycloneDX, so it was not read as one")}
	}
	if bom.Metadata == nil || bom.Metadata.Component == nil || bom.Metadata.Component.Name == "" {
		return nil, []domain.Finding{bundledSBOMFinding("EVIDENCE_UNREADABLE", path,
			"the bundled SBOM names no metadata.component, so it describes no package of its own")}
	}
	component := bom.Metadata.Component
	var supplier string
	if component.Supplier != nil {
		supplier = component.Supplier.Name
	}
	// The name is read to get this far and then dropped. A component keeps the
	// name that was settled before its files were grouped (D33), and a document
	// lying in a directory called dep/mbedtls-3.4 while calling itself mbedtls
	// is the normal case, not a mismatch worth refusing over.
	return b.contributions(component.Version, cycloneDXLicense(component.Licenses), supplier, component.PURL), nil
}

// cycloneDXLicense is the SPDX expression a CycloneDX licence array states, or
// the empty string where it states something this tool will not interpret.
//
// An `expression` is an answer outright, and so is a single licence object's
// `id`, which the format defines as an SPDX identifier. A `name` is not:
// CycloneDX defines that field as the licence that has no SPDX identifier, so
// passing it on would put free text where a consumer expects SPDX -- the value
// travels as a claim on FieldLicense, and everything downstream, up to our own
// `licenses[].expression`, which both bundled schemas describe as a valid SPDX
// expression, treats such a claim as one. Several licence objects without an
// expression are no answer either: the format does not say whether they apply
// together or the recipient chooses one, and section 22.3 forbids deciding that
// by inspection. Such a document then contributes no licence, which is the same
// silence as a document that stated none.
func cycloneDXLicense(licenses []cyclonedx.License) string {
	for _, entry := range licenses {
		if entry.Expression != "" {
			return entry.Expression
		}
	}
	if len(licenses) != 1 || licenses[0].License == nil {
		return ""
	}
	return licenses[0].License.ID
}

// bundledSPDXDocument is the part of an SPDX 2.x document this reader needs.
// It is a type of its own rather than the one vcpkg.go reads with, because
// this one has to work out which package the document is about and vcpkg's
// does not: vcpkg's own layout guarantees that the port comes first, and a
// foreign document guarantees nothing. The package entries are the same shape
// in both, so that shape is shared.
type bundledSPDXDocument struct {
	SPDXVersion       string         `json:"spdxVersion"`
	DocumentDescribes []string       `json:"documentDescribes"`
	Packages          []spdxPackage  `json:"packages"`
	Relationships     []spdxRelation `json:"relationships"`
}

// spdxRelation is one edge of the document graph. Only DESCRIBES from the
// document element is read, and only to answer which package the document is
// about.
type spdxRelation struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}

func (b bundledSBOM) readSPDX(path string, data []byte) ([]Contribution, []domain.Finding) {
	var document bundledSPDXDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, []domain.Finding{bundledSBOMFinding("EVIDENCE_UNREADABLE", path,
			"the bundled SBOM is not an SPDX JSON document, so nothing was taken from it")}
	}
	// SPDX 3.0 is JSON-LD and has a different shape; reading it by pattern
	// matching on an @graph would be precisely the guess this tool refuses. A
	// document of any other version is refused and reported, so that the
	// missing values are traceable to the version rather than to silence.
	if !strings.HasPrefix(document.SPDXVersion, "SPDX-2.") {
		return nil, []domain.Finding{bundledSBOMFinding("EVIDENCE_UNREADABLE", path,
			"the bundled SBOM declares an SPDX version this tool does not read; only SPDX-2.x is read")}
	}
	entry, ok := document.described()
	if !ok {
		return nil, []domain.Finding{bundledSBOMFinding("EVIDENCE_UNREADABLE", path,
			"the bundled SBOM does not name exactly one package it describes, so which package it is about would be a guess")}
	}
	// Both spellings are stripped, because SPDX writes the supplier as a typed
	// actor. NOASSERTION is the document saying it does not know, which is not
	// a supplier.
	supplier := strings.TrimPrefix(strings.TrimPrefix(entry.Supplier, "Organization: "), "Person: ")
	if supplier == "NOASSERTION" || supplier == "NONE" {
		supplier = ""
	}
	var purl string
	for _, reference := range entry.ExternalRefs {
		if reference.ReferenceType == "purl" && reference.ReferenceLocator != "" {
			purl = reference.ReferenceLocator
			break
		}
	}
	return b.contributions(entry.VersionInfo,
		firstNonNoAssertion(entry.LicenseDeclared, entry.LicenseConcluded), supplier, purl), nil
}

// described is the one package the document is about, taken from
// documentDescribes or from a DESCRIBES relationship of the document element.
// A document that names several of them, or names one that is not among its
// packages, is refused: it may well be a product SBOM that happens to lie in
// this directory, and picking one package out of it would attribute a whole
// delivery to a single dependency.
func (d bundledSPDXDocument) described() (spdxPackage, bool) {
	identifiers := make([]string, 0, len(d.DocumentDescribes)+len(d.Relationships))
	identifiers = append(identifiers, d.DocumentDescribes...)
	for _, relationship := range d.Relationships {
		if relationship.RelationshipType == "DESCRIBES" && relationship.SPDXElementID == "SPDXRef-DOCUMENT" {
			identifiers = append(identifiers, relationship.RelatedSPDXElement)
		}
	}
	distinct := make([]string, 0, len(identifiers))
	for _, identifier := range identifiers {
		known := false
		for _, seen := range distinct {
			if seen == identifier {
				known = true
			}
		}
		if !known {
			distinct = append(distinct, identifier)
		}
	}
	switch {
	case len(distinct) == 1:
		for _, entry := range d.Packages {
			if entry.SPDXID == distinct[0] && entry.Name != "" {
				return entry, true
			}
		}
	case len(distinct) == 0 && len(d.Packages) == 1 && d.Packages[0].Name != "":
		// A document holding a single package and saying nothing about
		// relationships still describes that package unambiguously. It is the
		// only inference made here, and it cannot go wrong: there is nothing
		// else it could have meant.
		return d.Packages[0], true
	}
	return spdxPackage{}, false
}

// contributions turns the four values into claims. They all carry rank 4:
// they came out of one document, so nothing distinguishes them, and only the
// version carries a confidence, because section 20.3 assigns one to versions
// alone.
//
// A value the document did not state contributes nothing. Take would drop an
// empty value anyway; leaving it out here is what keeps this reader consistent
// with the rest of the package, where a claim is made only when there is
// something to claim.
func (bundledSBOM) contributions(version, license, supplier, purl string) []Contribution {
	contributions := make([]Contribution, 0, 4)
	add := func(field Field, value string, confidence domain.Confidence) {
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
				Rank:       RankBundledSBOM,
				Confidence: confidence,
			},
		})
	}
	add(FieldVersion, version, domain.ConfidenceHigh)
	add(FieldLicense, license, "")
	add(FieldSupplier, supplier, "")
	add(FieldPURL, purl, "")
	return contributions
}

// bundledSBOMFinding reports a document that was found but not used. The
// subject is the document rather than the component: what could not be read is
// a piece of evidence, and the package is still described by whatever else
// states it.
func bundledSBOMFinding(id, path, message string) domain.Finding {
	return domain.Finding{
		ID: id, Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: path},
		Message: message,
	}
}
