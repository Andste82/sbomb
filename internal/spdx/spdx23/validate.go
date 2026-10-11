package spdx23

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/example/sbomb/internal/licenselist"
	"github.com/example/sbomb/internal/spdx/mapping"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// parsed is one decoding of a document: the generic value the schema is
// checked against, and the typed form the rules read.
type parsed struct {
	instance any
	doc      document
}

func parse(data []byte) (*parsed, error) {
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("the document is not JSON: %w", err)
	}
	out := &parsed{instance: instance}
	if err := json.Unmarshal(data, &out.doc); err != nil {
		// The schema says what is wrong in its own words.
		if schemaErr := validateAgainstSchema(instance); schemaErr != nil {
			return nil, schemaErr
		}
		return nil, fmt.Errorf("the document cannot be read as SPDX 2.3: %w", err)
	}
	return out, nil
}

// Validate is what `sbomb validate` holds any SPDX 2.3 document to: the
// official v2.3 schema, then the rules the specification states and the
// schema cannot express (section 28.11.11, tier a). A correct document
// somebody else wrote passes: it may refer to other documents, describe its
// root with documentDescribes, or carry fields sbomb never writes.
func Validate(data []byte) error {
	p, err := parse(data)
	if err != nil {
		return err
	}
	return validateParsed(p)
}

// CheckOwnOutput holds a document sbomb wrote to the promises of its own
// output (tier b). None of them is a rule of the specification.
func CheckOwnOutput(data []byte) error {
	p, err := parse(data)
	if err != nil {
		return err
	}
	return checkParsed(p)
}

// ValidateOwnOutput is Validate and then CheckOwnOutput over one decoding.
func ValidateOwnOutput(data []byte) error {
	p, err := parse(data)
	if err != nil {
		return err
	}
	if err := validateParsed(p); err != nil {
		return err
	}
	return checkParsed(p)
}

// issues collects what a check found.
type issues struct{ lines []string }

func (i *issues) addf(format string, args ...any) {
	i.lines = append(i.lines, "  "+fmt.Sprintf(format, args...))
}

func (i *issues) err(heading string) error {
	if len(i.lines) == 0 {
		return nil
	}
	return fmt.Errorf("%s:\n%s", heading, capped(i.lines))
}

var (
	createdPattern   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)
	creatorPattern   = regexp.MustCompile(`^(Person|Organization|Tool): \S.*$`)
	elementIDPattern = regexp.MustCompile(`^SPDXRef-[A-Za-z0-9.-]+$`)
	// Clause 6.6 lets the idstring of a DocumentRef hold "+", which an
	// SPDXRef (clause 7.2) and a LicenseRef (clause 10.1) do not. A
	// relationship end names a declared DocumentRef whatever it holds; a
	// licence expression cannot carry the "+" form, because annex D's idstring
	// excludes it and reads a "+" as "or later".
	documentRefID = regexp.MustCompile(`^DocumentRef-[A-Za-z0-9.+-]+$`)
	licenseRefID  = regexp.MustCompile(`^LicenseRef-[A-Za-z0-9.-]+$`)
	hexPattern    = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

// elementKinds is every element identifier of a document and what it is.
func elementKinds(doc *document) map[string]string {
	kinds := map[string]string{}
	for _, p := range doc.Packages {
		kinds[p.SPDXID] = "package"
	}
	for _, f := range doc.Files {
		kinds[f.SPDXID] = "file"
	}
	for _, s := range doc.Snippets {
		kinds[s.SPDXID] = "snippet"
	}
	return kinds
}

func validateParsed(p *parsed) error {
	if err := validateAgainstSchema(p.instance); err != nil {
		return err
	}
	return conformance(&p.doc).err("SPDX 2.3 conformance check failed")
}

// conformance checks the rules of the specification text, and of the v2.3.1
// errata schema, that the v2.3 schema does not express.
func conformance(doc *document) *issues {
	found := &issues{}
	if doc.SPDXVersion != specVersion {
		found.addf("spdxVersion is %q; this check reads %q", doc.SPDXVersion, specVersion)
	}
	if doc.DataLicense != dataLicense {
		found.addf("dataLicense is %q; clause 6.2 requires %q", doc.DataLicense, dataLicense)
	}
	if doc.SPDXID != documentID {
		found.addf("the document SPDXID is %q; clause 6.3 requires %q", doc.SPDXID, documentID)
	}
	if doc.DocumentNamespace == "" {
		found.addf("the document has no documentNamespace; clause 6.5 requires one")
	} else if parsedURI, err := url.Parse(doc.DocumentNamespace); err != nil || !parsedURI.IsAbs() || strings.Contains(doc.DocumentNamespace, "#") {
		found.addf("documentNamespace %q is not an absolute URI without a fragment (clause 6.5)", doc.DocumentNamespace)
	}
	if !createdPattern.MatchString(doc.CreationInfo.Created) {
		found.addf("created %q is not YYYY-MM-DDThh:mm:ssZ (clause 6.9)", doc.CreationInfo.Created)
	}
	for _, creator := range doc.CreationInfo.Creators {
		if !creatorPattern.MatchString(creator) {
			found.addf("creator %q is not \"Person: \", \"Organization: \" or \"Tool: \" and a name (clause 6.8)", creator)
		}
	}

	documentRefs := map[string]bool{}
	for _, ref := range doc.ExternalDocumentRefs {
		if !documentRefID.MatchString(ref.ExternalDocumentID) {
			found.addf("externalDocumentId %q is not DocumentRef-[idstring] (clause 6.6)", ref.ExternalDocumentID)
		}
		documentRefs[ref.ExternalDocumentID] = true
	}

	// Element identifiers: the pattern of the errata schema, and unique
	// across every kind of element (clause 7.2, 8.2, 9.1).
	seen := map[string]bool{}
	checkID := func(id, what string) {
		if !elementIDPattern.MatchString(id) || id == documentID {
			found.addf("%s SPDXID %q is not SPDXRef-[idstring]", what, id)
		}
		if seen[id] {
			found.addf("SPDXID %q is used by more than one element", id)
		}
		seen[id] = true
	}
	for _, p := range doc.Packages {
		checkID(p.SPDXID, "package")
		checkChecksums(p.Checksums, "package "+p.SPDXID, found)
		if p.FilesAnalyzed != nil && !*p.FilesAnalyzed {
			if len(p.PackageVerificationCode) > 0 {
				found.addf("package %s has filesAnalyzed false and a packageVerificationCode (clause 7.9)", p.SPDXID)
			}
			if len(p.LicenseInfoFromFiles) > 0 {
				found.addf("package %s has filesAnalyzed false and licenseInfoFromFiles (clause 7.14)", p.SPDXID)
			}
			if len(p.HasFiles) > 0 {
				found.addf("package %s has filesAnalyzed false and lists files (clause 7.8: it shall not contain any files)", p.SPDXID)
			}
		}
	}
	for _, f := range doc.Files {
		checkID(f.SPDXID, "file")
		checkChecksums(f.Checksums, "file "+f.SPDXID, found)
		sha1 := 0
		for _, sum := range f.Checksums {
			if sum.Algorithm == "SHA1" {
				sha1++
			}
		}
		if sha1 != 1 {
			found.addf("file %s has %d SHA1 checksums; clause 8.4 requires exactly one", f.SPDXID, sha1)
		}
	}
	for _, s := range doc.Snippets {
		checkID(s.SPDXID, "snippet")
	}

	// Relationships: both ends resolve.
	kinds := elementKinds(doc)
	resolves := func(id string, related bool) bool {
		if id == documentID || kinds[id] != "" {
			return true
		}
		if related && (id == none || id == noAssertion) {
			return true
		}
		if document, _, found := strings.Cut(id, ":"); found && strings.HasPrefix(document, "DocumentRef-") {
			return documentRefs[document]
		}
		return false
	}
	notAnalyzed := map[string]bool{}
	for _, p := range doc.Packages {
		if p.FilesAnalyzed != nil && !*p.FilesAnalyzed {
			notAnalyzed[p.SPDXID] = true
		}
	}
	describes := false
	for _, rel := range doc.Relationships {
		if !resolves(rel.SPDXElementID, false) {
			found.addf("relationship %s %s %s: %q is no element of the document", rel.SPDXElementID, rel.RelationshipType, rel.RelatedSPDXElement, rel.SPDXElementID)
		}
		if !resolves(rel.RelatedSPDXElement, true) {
			found.addf("relationship %s %s %s: %q is no element of the document", rel.SPDXElementID, rel.RelationshipType, rel.RelatedSPDXElement, rel.RelatedSPDXElement)
		}
		// Clause 11.1: "A DESCRIBED_BY SPDXRef-DOCUMENT" says the same as
		// "SPDXRef-DOCUMENT DESCRIBES A".
		if rel.SPDXElementID == documentID && rel.RelationshipType == "DESCRIBES" ||
			rel.RelatedSPDXElement == documentID && rel.RelationshipType == "DESCRIBED_BY" {
			describes = true
		}
		switch {
		case rel.RelationshipType == "CONTAINS" && notAnalyzed[rel.SPDXElementID] && kinds[rel.RelatedSPDXElement] == "file":
			found.addf("package %s has filesAnalyzed false and contains file %s (clause 7.8: it shall not contain any files)", rel.SPDXElementID, rel.RelatedSPDXElement)
		case rel.RelationshipType == "CONTAINED_BY" && notAnalyzed[rel.RelatedSPDXElement] && kinds[rel.SPDXElementID] == "file":
			found.addf("package %s has filesAnalyzed false and contains file %s (clause 7.8: it shall not contain any files)", rel.RelatedSPDXElement, rel.SPDXElementID)
		}
	}
	for _, described := range doc.DocumentDescribes {
		if !resolves(described, false) {
			found.addf("documentDescribes names %q, which is no element of the document", described)
		}
	}
	if len(doc.Packages)+len(doc.Files) > 1 && !describes && len(doc.DocumentDescribes) == 0 {
		found.addf("the document has more than one package or file and says which it describes neither by a DESCRIBES or DESCRIBED_BY relationship nor by documentDescribes (clause 11.1)")
	}

	licences(doc, documentRefs, found)
	return found
}

// checkChecksums holds every digest to the length and alphabet of its
// algorithm.
func checkChecksums(sums []checksum, what string, found *issues) {
	for _, sum := range sums {
		if !hexPattern.MatchString(sum.ChecksumValue) {
			found.addf("%s: the %s checksum %q is not hexadecimal", what, sum.Algorithm, sum.ChecksumValue)
			continue
		}
		if length, fixed := checksumLengths[sum.Algorithm]; fixed && len(sum.ChecksumValue) != length {
			found.addf("%s: the %s checksum has %d hex digits, not %d", what, sum.Algorithm, len(sum.ChecksumValue), length)
		}
	}
}

// licenceFieldsOf calls visit with every licence field of the document.
func licenceFieldsOf(doc *document, visit func(where, value string)) {
	for _, p := range doc.Packages {
		visit("package "+p.SPDXID+" licenseConcluded", p.LicenseConcluded)
		visit("package "+p.SPDXID+" licenseDeclared", p.LicenseDeclared)
		for _, value := range p.LicenseInfoFromFiles {
			visit("package "+p.SPDXID+" licenseInfoFromFiles", value)
		}
	}
	for _, f := range doc.Files {
		visit("file "+f.SPDXID+" licenseConcluded", f.LicenseConcluded)
		for _, value := range f.LicenseInfoInFiles {
			visit("file "+f.SPDXID+" licenseInfoInFiles", value)
		}
	}
	for _, s := range doc.Snippets {
		visit("snippet "+s.SPDXID+" licenseConcluded", s.LicenseConcluded)
	}
}

// licences: every field is an expression of annex D or one of the two
// keywords; every local LicenseRef is defined in the document, with a text
// (clause 10.1, 10.2); every DocumentRef names a declared document.
func licences(doc *document, documentRefs map[string]bool, found *issues) {
	defined := map[string]bool{}
	for _, info := range doc.HasExtractedLicensingInfos {
		if !licenseRefID.MatchString(info.LicenseID) {
			found.addf("licenseId %q is not LicenseRef-[idstring] (clause 10.1)", info.LicenseID)
		}
		if defined[info.LicenseID] {
			found.addf("licenseId %q is defined more than once", info.LicenseID)
		}
		defined[info.LicenseID] = true
		if strings.TrimSpace(info.ExtractedText) == "" {
			found.addf("licenseId %q has no extractedText (clause 10.2)", info.LicenseID)
		}
	}
	licenceFieldsOf(doc, func(where, value string) {
		if value == "" || value == none || value == noAssertion {
			return
		}
		expression, err := mapping.ParseExpression(value)
		if err != nil {
			found.addf("%s: %v", where, err)
			return
		}
		for _, id := range expression.Additions {
			if mapping.IsAdditionRef(id) {
				found.addf("%s names %s; SPDX 2.3 has no AdditionRef (annex D)", where, id)
			}
		}
		for _, id := range expression.Licenses {
			if !mapping.IsLicenseRef(id) {
				continue
			}
			if document, _, qualified := strings.Cut(id, ":"); qualified {
				if !documentRefs[document] {
					found.addf("%s names %s, but the document declares no %s in externalDocumentRefs", where, id, document)
				}
				continue
			}
			if !defined[id] {
				found.addf("%s names %s, which hasExtractedLicensingInfos does not define (clause 10.1)", where, id)
			}
		}
	})
}

// checkParsed holds a document to sbomb's own promises (tier b):
//
//   - it is self-contained: no external document, no DocumentRef anywhere;
//   - it describes exactly one package, by one DESCRIBES relationship and not
//     by documentDescribes, and that package comes first;
//   - every package and file can be reached from it over the relationships;
//   - every element identifier is one elementID wrote: it decodes into a
//     local identity of sbomb's scheme and encodes back to itself;
//   - every package and file has a name that is not white space alone;
//   - a download location is NOASSERTION or the VCS form;
//   - the licence list version is stated exactly where a listed identifier is
//     used, as "M.N";
//   - every annotation is one of sbomb's three kinds, well-formed, by the tool
//     that created the document;
//   - the packages that stand for files follow every other package, and they
//     and the files are in the order of their local identities; the
//     relationships are in their total order, without repeats.
func checkParsed(p *parsed) error {
	doc := &p.doc
	found := &issues{}
	if len(doc.ExternalDocumentRefs) > 0 {
		found.addf("the document names %d external documents; sbomb's documents are self-contained", len(doc.ExternalDocumentRefs))
	}
	if len(doc.DocumentDescribes) > 0 {
		found.addf("the document uses documentDescribes; sbomb states its root by a DESCRIBES relationship")
	}
	kinds := elementKinds(doc)
	var roots []string
	for _, rel := range doc.Relationships {
		for _, end := range []string{rel.SPDXElementID, rel.RelatedSPDXElement} {
			if strings.HasPrefix(end, "DocumentRef-") {
				found.addf("relationship %s %s %s refers to another document", rel.SPDXElementID, rel.RelationshipType, rel.RelatedSPDXElement)
			}
		}
		if rel.RelationshipType == "DESCRIBES" {
			if rel.SPDXElementID != documentID || kinds[rel.RelatedSPDXElement] != "package" {
				found.addf("relationship %s DESCRIBES %s is not the document describing a package", rel.SPDXElementID, rel.RelatedSPDXElement)
				continue
			}
			roots = append(roots, rel.RelatedSPDXElement)
		}
	}
	if len(roots) != 1 {
		found.addf("the document describes %d packages; sbomb's describe exactly one", len(roots))
	} else {
		if len(doc.Packages) == 0 || doc.Packages[0].SPDXID != roots[0] {
			found.addf("the described package %s is not the first package", roots[0])
		}
		reachability(doc, roots[0], found)
	}

	for id := range kinds {
		local, ok := LocalIdentity(id)
		if !ok || elementID(local) != id || !strings.Contains(local, ":") {
			found.addf("SPDXID %q is not one sbomb's encoding writes", id)
		}
	}
	for _, pk := range doc.Packages {
		if strings.TrimSpace(pk.Name) == "" {
			found.addf("package %s has no name", pk.SPDXID)
		}
		if pk.DownloadLocation != noAssertion && !strings.HasPrefix(pk.DownloadLocation, "git+") {
			found.addf("package %s has the download location %q; sbomb writes NOASSERTION or a git+ VCS location", pk.SPDXID, pk.DownloadLocation)
		}
	}
	for _, f := range doc.Files {
		if strings.TrimSpace(f.FileName) == "" {
			found.addf("file %s has no name", f.SPDXID)
		}
	}

	listed := false
	licenceFieldsOf(doc, func(_, value string) {
		if expression, err := mapping.ParseExpression(value); err == nil {
			for _, id := range append(append([]string(nil), expression.Licenses...), expression.Additions...) {
				if licenselist.Known(id) || licenselist.KnownException(id) {
					listed = true
				}
			}
		}
	})
	switch version := doc.CreationInfo.LicenseListVersion; {
	case listed && version != listVersion(licenselist.Version):
		found.addf("licenseListVersion is %q; a document naming a listed licence states %q", version, listVersion(licenselist.Version))
	case !listed && version != "":
		found.addf("licenseListVersion %q is stated, but no listed licence is named", version)
	}

	annotators := map[string]bool{}
	for _, creator := range doc.CreationInfo.Creators {
		annotators[creator] = true
	}
	checkAnnotations := func(where string, list []annotation) {
		for _, a := range list {
			if a.AnnotationType != annotationType || !annotators[a.Annotator] || a.AnnotationDate != doc.CreationInfo.Created {
				found.addf("%s: an annotation is not of type %s, by a creator of the document, at its creation time", where, annotationType)
			}
			if _, err := annotationKind(a.Comment); err != nil {
				found.addf("%s: %v", where, err)
			}
		}
	}
	checkAnnotations("the document", doc.Annotations)
	for _, pk := range doc.Packages {
		checkAnnotations("package "+pk.SPDXID, pk.Annotations)
	}
	for _, f := range doc.Files {
		checkAnnotations("file "+f.SPDXID, f.Annotations)
	}

	order(doc, found)
	return found.err("the SPDX 2.3 document breaks an invariant of sbomb's own output")
}

// reachability: every package and file is connected to the described
// package. Relationships are followed in both directions, because 2.3 states
// a build tool, a provided dependency, a patch and documentation from their
// own end.
func reachability(doc *document, root string, found *issues) {
	adjacent := map[string][]string{}
	for _, rel := range doc.Relationships {
		adjacent[rel.SPDXElementID] = append(adjacent[rel.SPDXElementID], rel.RelatedSPDXElement)
		adjacent[rel.RelatedSPDXElement] = append(adjacent[rel.RelatedSPDXElement], rel.SPDXElementID)
	}
	reached := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[id] {
			if next == documentID || next == none || next == noAssertion || reached[next] {
				continue
			}
			reached[next] = true
			queue = append(queue, next)
		}
	}
	for _, pk := range doc.Packages {
		if !reached[pk.SPDXID] {
			found.addf("package %s cannot be reached from the described package", pk.SPDXID)
		}
	}
	for _, f := range doc.Files {
		if !reached[f.SPDXID] {
			found.addf("file %s cannot be reached from the described package", f.SPDXID)
		}
	}
}

// order: the canonical order the renderer writes.
func order(doc *document, found *issues) {
	fileStart := len(doc.Packages)
	for index, pk := range doc.Packages {
		if pk.FilesAnalyzed != nil && !*pk.FilesAnalyzed {
			fileStart = index
			break
		}
	}
	previous := ""
	for _, pk := range doc.Packages[fileStart:] {
		if pk.FilesAnalyzed == nil || *pk.FilesAnalyzed {
			found.addf("package %s follows a package that stands for a file", pk.SPDXID)
			continue
		}
		local, _ := LocalIdentity(pk.SPDXID)
		if local <= previous {
			found.addf("package %s is out of order", pk.SPDXID)
		}
		previous = local
	}
	previous = ""
	for _, f := range doc.Files {
		local, _ := LocalIdentity(f.SPDXID)
		if local <= previous {
			found.addf("file %s is out of order", f.SPDXID)
		}
		previous = local
	}
	for index := 1; index < len(doc.Relationships); index++ {
		if !relationshipLess(doc.Relationships[index-1], doc.Relationships[index]) {
			r := doc.Relationships[index]
			found.addf("relationship %s %s %s is out of order or repeated", r.SPDXElementID, r.RelationshipType, r.RelatedSPDXElement)
		}
	}
}
