package spdx23

import (
	"errors"
	"io"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/example/sbomb/internal/jsonstream"
	"github.com/example/sbomb/internal/licenselist"
	"github.com/example/sbomb/internal/spdx/mapping"
)

const (
	specVersion = "SPDX-2.3"
	dataLicense = "CC0-1.0"
	noAssertion = "NOASSERTION"
	none        = "NONE"
)

// Render writes the model as an SPDX 2.3 JSON document whose namespace is
// urn:uuid:<namespace> (section 28.11.11).
//
// The document is flat, as 2.3 is: the creation information, one annotation
// with the run's properties, the packages -- the product, the artifacts and
// the components, then every file that has no SHA-1, as a package of its own
// -- the files, the licences sbomb had to define, and the relationships. It
// is pretty-printed with two-space indentation and one trailing newline, like
// every document sbomb writes.
func Render(w io.Writer, model *mapping.Model, namespace string) error {
	if model == nil {
		return errors.New("no model to render")
	}
	if namespace == "" {
		return errors.New("an SPDX 2.3 document needs a namespace")
	}
	doc := build(model, namespace)
	members := []jsonstream.Member{
		{Name: "spdxVersion", Value: doc.SPDXVersion},
		{Name: "dataLicense", Value: doc.DataLicense},
		{Name: "SPDXID", Value: doc.SPDXID},
		{Name: "name", Value: doc.Name},
		{Name: "documentNamespace", Value: doc.DocumentNamespace},
		{Name: "creationInfo", Value: doc.CreationInfo},
	}
	if len(doc.Annotations) > 0 {
		members = append(members, jsonstream.Elements("annotations", doc.Annotations))
	}
	if len(doc.Packages) > 0 {
		members = append(members, jsonstream.Elements("packages", doc.Packages))
	}
	if len(doc.Files) > 0 {
		members = append(members, jsonstream.Elements("files", doc.Files))
	}
	if len(doc.HasExtractedLicensingInfos) > 0 {
		members = append(members, jsonstream.Elements("hasExtractedLicensingInfos", doc.HasExtractedLicensingInfos))
	}
	if len(doc.Relationships) > 0 {
		members = append(members, jsonstream.Elements("relationships", doc.Relationships))
	}
	return jsonstream.WriteObject(w, members)
}

type renderer struct {
	model     *mapping.Model
	annotator string
	files     map[string]mapping.File
	rels      map[relationship]bool
}

func build(model *mapping.Model, namespace string) document {
	r := &renderer{model: model, files: map[string]mapping.File{}, rels: map[relationship]bool{}}
	creators := []string{"Organization: " + model.Creator}
	r.annotator = creators[0]
	if model.Run.ToolName != "" {
		tool := "Tool: " + model.Run.ToolName
		if model.Run.ToolVersion != "" {
			tool += "-" + model.Run.ToolVersion
		}
		creators = append(creators, tool)
		r.annotator = tool
	}
	doc := document{
		SPDXVersion:       specVersion,
		DataLicense:       dataLicense,
		SPDXID:            documentID,
		Name:              model.ProductName(),
		DocumentNamespace: "urn:uuid:" + namespace,
		CreationInfo:      creationInfo{Created: model.Created, Creators: creators},
	}
	for _, license := range model.Licenses {
		if license.ListedIDs {
			doc.CreationInfo.LicenseListVersion = listVersion(licenselist.Version)
			break
		}
	}
	doc.Annotations = r.propertyAnnotation(model.Run.Properties)

	for _, f := range model.Files {
		r.files[f.LocalID] = f
	}
	for _, p := range model.Packages {
		doc.Packages = append(doc.Packages, r.packageElement(p))
		r.packageRelationships(p)
		if p.Role == mapping.RoleProduct {
			r.add(documentID, "DESCRIBES", elementID(p.LocalID), "")
		}
	}
	for _, f := range model.Files {
		if hasSHA1(f) {
			doc.Files = append(doc.Files, r.fileElement(f))
		} else {
			doc.Packages = append(doc.Packages, r.filePackage(f))
		}
		if f.KnownLeaf {
			r.add(elementID(f.LocalID), "DEPENDS_ON", none, "")
		}
	}
	for _, edge := range model.Edges {
		spelling := edgeSpellings[edge.Kind]
		from, to := elementID(edge.From), elementID(edge.To)
		if spelling.inverse {
			from, to = to, from
		}
		r.add(from, spelling.relationshipType, to, "")
	}
	for _, custom := range model.Custom {
		doc.HasExtractedLicensingInfos = append(doc.HasExtractedLicensingInfos, extractedLicenseOf(custom))
	}
	doc.Relationships = r.relationships()
	return doc
}

// listVersion is the licence list version as 2.3 states it: "M.N" (clause
// 6.7), the list's major and minor version.
func listVersion(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return version
	}
	return parts[0] + "." + parts[1]
}

// annotation is one annotation of sbomb's, from its lines.
func (r *renderer) annotation(lines []string) annotation {
	return annotation{
		AnnotationDate: r.model.Created,
		AnnotationType: annotationType,
		Annotator:      r.annotator,
		Comment:        strings.Join(lines, "\n"),
	}
}

// propertyAnnotation is a property set as one annotation, or none for an
// empty set.
func (r *renderer) propertyAnnotation(properties []mapping.Property) []annotation {
	if len(properties) == 0 {
		return nil
	}
	return []annotation{r.annotation(propertyLines(properties))}
}

// statementAnnotations are the qualified licence statements of an element:
// one annotation per statement that carries properties.
func (r *renderer) statementAnnotations(field string, uses []mapping.LicenseUse) []annotation {
	var out []annotation
	for _, use := range uses {
		if len(use.Properties) == 0 {
			continue
		}
		lines := append([]string{field + ": " + licenseString(use.License)}, propertyLines(use.Properties)...)
		out = append(out, r.annotation(lines))
	}
	return out
}

func (r *renderer) packageElement(p mapping.Package) pkg {
	out := pkg{
		SPDXID:                elementID(p.LocalID),
		Name:                  p.Name,
		VersionInfo:           p.Version,
		Supplier:              organization(p.Supplier),
		Originator:            organization(p.Originator),
		DownloadLocation:      downloadLocation(p),
		SourceInfo:            p.SourceInfo,
		LicenseConcluded:      licenseField(p.Concluded),
		LicenseDeclared:       licenseField(p.Declared),
		CopyrightText:         p.CopyrightText(),
		Description:           p.Description,
		ExternalRefs:          externalRefs(p),
		PrimaryPackagePurpose: packagePurpose(p),
	}
	if out.LicenseConcluded == "" {
		out.LicenseConcluded = noAssertion
	}
	out.Annotations = r.propertyAnnotation(p.Properties)
	out.Annotations = append(out.Annotations, r.statementAnnotations("licenseConcluded", p.Concluded)...)
	out.Annotations = append(out.Annotations, r.statementAnnotations("licenseDeclared", p.Declared)...)
	for _, exclusion := range p.CVEExclusions {
		statement := exclusion.Reason
		if statement == "" {
			statement = "the upstream lists this vulnerability as not applying to the version it shipped"
		}
		out.Annotations = append(out.Annotations, r.annotation([]string{notAffectedPrefix + exclusion.ID + ": " + escapeValue(statement)}))
	}
	return out
}

func organization(name string) string {
	if name == "" {
		return ""
	}
	return "Organization: " + name
}

// externalRefs are the package URL, every CPE and the VCS location. A value
// that is not of the shape its type requires, or a VCS URL with white space,
// which an OTHER locator may not hold, is left to the annotation lines rather
// than written in a form a reader refuses.
func externalRefs(p mapping.Package) []externalRef {
	var refs []externalRef
	if strings.HasPrefix(p.PURL, "pkg:") && !strings.ContainsAny(p.PURL, " \t\r\n") {
		refs = append(refs, externalRef{ReferenceCategory: "PACKAGE-MANAGER", ReferenceType: "purl", ReferenceLocator: p.PURL})
	}
	for _, cpe := range p.CPEs {
		switch {
		case strings.HasPrefix(cpe, "cpe:2.3:"):
			refs = append(refs, externalRef{ReferenceCategory: "SECURITY", ReferenceType: "cpe23Type", ReferenceLocator: cpe})
		case strings.HasPrefix(strings.ToLower(cpe), "cpe:/"):
			refs = append(refs, externalRef{ReferenceCategory: "SECURITY", ReferenceType: "cpe22Type", ReferenceLocator: cpe})
		}
	}
	if p.VCSURL != "" && !strings.ContainsAny(p.VCSURL, " \t\r\n") {
		refs = append(refs, externalRef{ReferenceCategory: "OTHER", ReferenceType: "vcs", ReferenceLocator: p.VCSURL})
	}
	return refs
}

// vcsHost is a host name a download location may name: labels of letters and
// digits, joined by "." or "-", ending in a top-level domain. It is what the
// SPDX reference implementation accepts, and a narrower set than a URL may
// hold, so that a VCS location is either written in a form every reader
// takes or not written at all.
var vcsHost = regexp.MustCompile(`(?i)^[a-z0-9]+([-.][a-z0-9]+)*\.[a-z]{2,5}$`)

// downloadLocation is where the package can be fetched from (clause 7.7): the
// VCS form "git+<url>[@<commit>]" when sbomb knows the repository and it is a
// network location, and NOASSERTION otherwise -- never NONE, which asserts
// that no location exists, something sbomb does not know. The commit is the
// one the package's properties state, when they state exactly one.
func downloadLocation(p mapping.Package) string {
	location := strings.TrimPrefix(p.VCSURL, "git+")
	if location == "" || strings.ContainsAny(location, " \t\r\n") {
		return noAssertion
	}
	parsed, err := url.Parse(location)
	if err != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(parsed.Path, "@") ||
		parsed.Path != "" && !strings.HasPrefix(parsed.Path, "/") {
		return noAssertion
	}
	switch parsed.Scheme {
	case "http", "https", "ssh", "git":
	default:
		return noAssertion
	}
	if !vcsHost.MatchString(parsed.Hostname()) {
		return noAssertion
	}
	var commits []string
	for _, property := range p.Properties {
		if property.Name == "sbomb:component:vcsCommit" && !slices.Contains(commits, property.Value) {
			commits = append(commits, property.Value)
		}
	}
	result := "git+" + location
	if len(commits) == 1 && !strings.ContainsAny(commits[0], " \t\r\n@#") {
		result += "@" + commits[0]
	}
	return result
}

// hasSHA1 reports whether a file can be a File element: SPDX 2.3 requires a
// SHA-1 of every file, and one that has none is stated as a package
// (filePackage).
func hasSHA1(f mapping.File) bool {
	if !f.Hashable() {
		return false
	}
	for _, hash := range f.Hashes {
		if algorithm, _ := checksumAlgorithm(hash.Key); algorithm == "SHA1" {
			return true
		}
	}
	return false
}

// checksums are a file's digests that 2.3 names an algorithm for, sorted, and
// the comment lines of those it does not.
func checksums(f mapping.File) ([]checksum, []string) {
	if !f.Hashable() {
		return nil, nil
	}
	var out []checksum
	var unnamed []string
	for _, hash := range f.Hashes {
		algorithm, known := checksumAlgorithm(hash.Key)
		if !known {
			unnamed = append(unnamed, hash.Key+":"+hash.Value)
			continue
		}
		out = append(out, checksum{Algorithm: algorithm, ChecksumValue: hash.Value})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Algorithm != out[j].Algorithm {
			return out[i].Algorithm < out[j].Algorithm
		}
		return out[i].ChecksumValue < out[j].ChecksumValue
	})
	sort.Strings(unnamed)
	return out, unnamed
}

// unnamedDigestsComment says which digests have no 2.3 algorithm, so that
// they are stated all the same.
func unnamedDigestsComment(unnamed []string) string {
	if len(unnamed) == 0 {
		return ""
	}
	return "digests SPDX 2.3 names no algorithm for: " + strings.Join(unnamed, ", ")
}

// fileLicences splits a file's detected licences into the concluded field
// and the licences found in the file.
func fileLicences(f mapping.File) (concluded []mapping.LicenseUse, declared []mapping.LicenseUse) {
	for _, use := range f.Detected {
		if use.Acknowledgement == "concluded" {
			concluded = append(concluded, use)
		} else {
			declared = append(declared, use)
		}
	}
	return concluded, declared
}

func (r *renderer) fileElement(f mapping.File) file {
	sums, unnamed := checksums(f)
	concluded, declared := fileLicences(f)
	out := file{
		SPDXID:             elementID(f.LocalID),
		FileName:           f.Name,
		FileTypes:          fileTypes(f),
		Checksums:          sums,
		LicenseConcluded:   licenseField(concluded),
		LicenseInfoInFiles: licenseList(declared),
		CopyrightText:      strings.Join(f.Copyright, "\n"),
	}
	var comments []string
	if f.Description != "" {
		comments = append(comments, f.Description)
	}
	var notices []string
	for _, text := range f.LicenseText {
		if utf8.Valid(text) {
			notices = append(notices, string(text))
		} else {
			comments = append(comments, nonUTF8TextComment)
		}
	}
	out.NoticeText = strings.Join(notices, "\n\n")
	if comment := unnamedDigestsComment(unnamed); comment != "" {
		comments = append(comments, comment)
	}
	out.Comment = strings.Join(comments, "\n")
	out.Annotations = r.propertyAnnotation(f.Properties)
	out.Annotations = append(out.Annotations, r.statementAnnotations("licenseConcluded", concluded)...)
	out.Annotations = append(out.Annotations, r.statementAnnotations("licenseInfoInFiles", declared)...)
	return out
}

// nonUTF8TextComment says that a licence text retained from a file is not
// carried, and why.
const nonUTF8TextComment = "a licence text was retained from this file (licenseTextInSBOM: evidence), but it is not UTF-8, and SPDX 2.3 has no field that carries it unaltered; it is in the FOSS outputs"

// File package comments: why a file is stated as a package.
const (
	missingFileComment  = "sbomb states this file as a package because SPDX 2.3 requires a SHA-1 of every file, and no bytes of it were read; it is not present where the build named it"
	patchFileComment    = "sbomb states this patch as a package because SPDX 2.3 requires a SHA-1 of every file, and the patch is known only from the metadata that recorded it; its bytes were not read"
	noSHA1FileComment   = "sbomb states this file as a package because SPDX 2.3 requires a SHA-1 of every file, and the document it was given states none for it"
	retainedTextComment = "a licence text was retained from this file (licenseTextInSBOM: evidence); a package has no field for it, and it is in the FOSS outputs"
)

// filePackage states a file without a SHA-1 as a package of one file
// (section 28.11.11, deviation D57): filesAnalyzed false, purpose FILE, the
// download location unasserted, the digests it has as package checksums --
// which 2.3 does not require -- and a comment that says why. The file is not
// dropped and no digest is invented; every relationship still points at it.
func (r *renderer) filePackage(f mapping.File) pkg {
	sums, unnamed := checksums(f)
	concluded, declared := fileLicences(f)
	analyzed := false
	out := pkg{
		SPDXID:                elementID(f.LocalID),
		Name:                  f.Name,
		PackageFileName:       f.Canonical,
		DownloadLocation:      noAssertion,
		FilesAnalyzed:         &analyzed,
		Checksums:             sums,
		LicenseConcluded:      licenseField(concluded),
		LicenseDeclared:       licenseField(declared),
		CopyrightText:         strings.Join(f.Copyright, "\n"),
		Description:           f.Description,
		PrimaryPackagePurpose: "FILE",
	}
	comments := []string{noSHA1FileComment}
	switch {
	case f.Origin&mapping.OriginPatch != 0:
		comments[0] = patchFileComment
	case f.Missing:
		comments[0] = missingFileComment
	}
	if len(f.LicenseText) > 0 {
		comments = append(comments, retainedTextComment)
	}
	if comment := unnamedDigestsComment(unnamed); comment != "" {
		comments = append(comments, comment)
	}
	out.Comment = strings.Join(comments, "\n")
	out.Annotations = r.propertyAnnotation(f.Properties)
	out.Annotations = append(out.Annotations, r.statementAnnotations("licenseConcluded", concluded)...)
	out.Annotations = append(out.Annotations, r.statementAnnotations("licenseDeclared", declared)...)
	return out
}
