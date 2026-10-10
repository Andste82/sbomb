package spdx3

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/example/sbomb/internal/licenselist"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// Render writes the model as an SPDX 3.0.1 JSON-LD document whose element
// IRIs live in the namespace urn:uuid:<namespace> (section 28.11.1).
//
// The document is one @graph: the creation information every element shares,
// the SpdxDocument and the Sbom it roots, the agents and the tool, the
// packages, the files, the vulnerabilities, the licence texts and expressions,
// and then every relationship by number. It is pretty-printed with two-space
// indentation and one trailing newline, like every document sbomb writes --
// not the canonical serialization of the SPDX specification, which is a form
// for signing, minified and with integer timestamps, and would make the
// documents unreadable for a reviewer.
func Render(w io.Writer, model *mapping.Model, namespace string) error {
	if model == nil {
		return errors.New("no model to render")
	}
	if namespace == "" {
		return errors.New("an SPDX 3.0.1 document needs a namespace for its element identifiers")
	}
	r := &renderer{model: model, namespace: namespace, relationships: newRelationshipSet()}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(r.document())
}

type renderer struct {
	model         *mapping.Model
	namespace     string
	relationships *relationshipSet
	// usesExpandedLicensing records a reference to the NOASSERTION or NONE
	// licence individuals, which belong to the ExpandedLicensing profile.
	usesExpandedLicensing bool
}

func (r *renderer) iri(local string) string { return iriFor(r.namespace, local) }

func (r *renderer) document() document {
	model := r.model
	creatorIRI := r.iri("agent:" + model.Creator)
	creation := node{
		Type:        "CreationInfo",
		ID:          creationInfoID,
		SpecVersion: specVersion,
		Created:     model.Created,
		CreatedBy:   []string{creatorIRI},
	}

	var agents, tools, packages, files, vulnerabilities, texts, expressions []node
	for _, name := range model.Agents {
		agents = append(agents, r.element("Organization", "agent:"+name, func(n *node) { n.Name = name }))
	}
	if model.Run.ToolName != "" {
		creation.CreatedUsing = []string{r.iri("tool:" + model.Run.ToolName)}
		tools = append(tools, r.element("Tool", "tool:"+model.Run.ToolName, func(n *node) {
			n.Name = model.Run.ToolName
			n.Extension = extensionOf([]mapping.Property{{Name: "sbomb:run:toolVersion", Value: model.Run.ToolVersion}})
		}))
	}

	product := ""
	vulnerabilitySeen := map[string]bool{}
	for _, pkg := range model.Packages {
		if pkg.Role == mapping.RoleProduct {
			product = r.iri(pkg.LocalID)
		}
		packages = append(packages, r.packageNode(pkg))
		r.packageRelationships(pkg)
		for _, exclusion := range pkg.CVEExclusions {
			if vulnerabilitySeen[exclusion.ID] {
				continue
			}
			vulnerabilitySeen[exclusion.ID] = true
			vulnerabilities = append(vulnerabilities, r.vulnerabilityNode(exclusion.ID))
		}
	}
	for _, file := range model.Files {
		files = append(files, r.fileNode(file))
		r.fileRelationships(file)
	}
	for _, edge := range model.Edges {
		spelling := edgeSpellings[edge.Kind]
		r.relationships.add(relationship{
			class:            spelling.class,
			from:             r.iri(edge.From),
			relationshipType: spelling.relationshipType,
			to:               []string{r.iri(edge.To)},
			scope:            spelling.scope,
		})
	}
	defined := map[string]bool{}
	for _, custom := range model.Custom {
		defined[custom.Ref] = true
		texts = append(texts, r.licenseTextNode(custom))
	}
	for _, license := range model.Licenses {
		expressions = append(expressions, r.expressionNode(license, defined))
	}

	var relationshipNodes []node
	for index, rel := range r.relationships.numbered() {
		relationshipNodes = append(relationshipNodes, r.relationshipNode(index+1, rel))
	}

	byLocal := func(nodes []node) []node {
		sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].SpdxID < nodes[j].SpdxID })
		return nodes
	}
	var elements []node
	for _, group := range [][]node{
		byLocal(agents), tools, byLocal(packages), byLocal(files), byLocal(vulnerabilities),
		byLocal(texts), byLocal(expressions), relationshipNodes,
	} {
		elements = append(elements, group...)
	}
	members := make([]string, 0, len(elements)+1)
	members = append(members, r.iri("sbom"))
	for _, element := range elements {
		members = append(members, element.SpdxID)
	}

	sbom := r.element("software_Sbom", "sbom", func(n *node) {
		n.Element = members[1:]
		if product != "" {
			n.RootElement = []string{product}
		}
		n.SbomType = []string{"build"}
	})
	spdxDocument := r.element("SpdxDocument", "document", func(n *node) {
		n.Name = productName(model)
		n.Extension = extensionOf(model.Run.Properties)
		n.Element = members
		n.ProfileConformance = r.profiles(len(texts)+len(expressions) > 0, len(vulnerabilities) > 0)
		n.RootElement = []string{r.iri("sbom")}
	})

	graph := make([]node, 0, len(elements)+3)
	graph = append(graph, creation, spdxDocument, sbom)
	graph = append(graph, elements...)
	return document{Context: contextURL, Graph: graph}
}

// element starts a node of an Element class: its type, its IRI and the shared
// creation information, and whatever fill adds.
func (r *renderer) element(class, local string, fill func(*node)) node {
	n := node{Type: class, SpdxID: r.iri(local), CreationInfo: creationInfoID}
	fill(&n)
	return n
}

// profiles is the profileConformance of the document (section 28.11.1),
// computed from what the document uses: core and software always, the two
// licensing profiles and security when their classes or individuals appear.
//
// "extension" is never claimed although every document uses the
// CdxPropertiesExtension. The 3.0.1 context defines a term "extension" (the
// Element property), and a term wins over the vocabulary profileConformance
// is expanded against, so the value "extension" expands to the property IRI
// and fails the SHACL model, while the full profile IRI fails the schema's
// enum. No spelling satisfies both validators; the claim is left out rather
// than written in a form one of them rejects (deviation D49b).
func (r *renderer) profiles(simpleLicensing, security bool) []string {
	profiles := []string{"core", "software"}
	if simpleLicensing {
		profiles = append(profiles, "simpleLicensing")
	}
	if r.usesExpandedLicensing {
		profiles = append(profiles, "expandedLicensing")
	}
	if security {
		profiles = append(profiles, "security")
	}
	sort.Strings(profiles)
	return profiles
}

func productName(model *mapping.Model) string {
	for _, pkg := range model.Packages {
		if pkg.Role == mapping.RoleProduct {
			return pkg.Name
		}
	}
	return ""
}

func (r *renderer) packageNode(pkg mapping.Package) node {
	return r.element("software_Package", pkg.LocalID, func(n *node) {
		n.Name = pkg.Name
		n.Description = pkg.Description
		if pkg.VCSURL != "" {
			n.ExternalRef = []externalRef{{Type: "ExternalRef", ExternalRefType: "vcs", Locator: []string{pkg.VCSURL}}}
		}
		n.ExternalIdentifier = packageIdentifiers(pkg)
		n.Extension = extensionOf(pkg.Properties)
		if pkg.Originator != "" {
			n.OriginatedBy = []string{r.iri("agent:" + pkg.Originator)}
		}
		if pkg.Supplier != "" {
			n.SuppliedBy = r.iri("agent:" + pkg.Supplier)
		}
		n.AdditionalPurpose = packageAdditionalPurposes(pkg)
		n.CopyrightText = copyrightText(pkg)
		n.PrimaryPurpose = packagePurpose(pkg)
		n.PackageURL = pkg.PURL
		n.PackageVersion = pkg.Version
		n.SourceInfo = pkg.SourceInfo
	})
}

// packageIdentifiers carries the purl twice -- software_packageUrl is the
// Package field, the external identifier is what a generic consumer searches
// -- and every CPE in the form it is written in.
func packageIdentifiers(pkg mapping.Package) []externalIdentifier {
	var identifiers []externalIdentifier
	if pkg.PURL != "" {
		identifiers = append(identifiers, externalIdentifier{Type: "ExternalIdentifier", ExternalIdentifierType: "packageUrl", Identifier: pkg.PURL})
	}
	for _, cpe := range pkg.CPEs {
		kind := "cpe23"
		if strings.HasPrefix(cpe, "cpe:/") {
			kind = "cpe22"
		}
		identifiers = append(identifiers, externalIdentifier{Type: "ExternalIdentifier", ExternalIdentifierType: kind, Identifier: cpe})
	}
	sort.SliceStable(identifiers, func(i, j int) bool {
		if identifiers[i].ExternalIdentifierType != identifiers[j].ExternalIdentifierType {
			return identifiers[i].ExternalIdentifierType < identifiers[j].ExternalIdentifierType
		}
		return identifiers[i].Identifier < identifiers[j].Identifier
	})
	return identifiers
}

func (r *renderer) packageRelationships(pkg mapping.Package) {
	from := r.iri(pkg.LocalID)
	for _, use := range pkg.Concluded {
		r.licenseRelationship(from, "hasConcludedLicense", use)
	}
	for _, use := range pkg.Declared {
		r.licenseRelationship(from, "hasDeclaredLicense", use)
	}
	if len(pkg.EvidenceFiles) > 0 {
		r.relationships.add(relationship{class: classRelationship, from: from, relationshipType: "hasEvidence", to: r.iris(pkg.EvidenceFiles)})
	}
	if len(pkg.Patches) > 0 {
		locals := make([]string, 0, len(pkg.Patches))
		for _, patch := range pkg.Patches {
			locals = append(locals, patch.LocalID)
		}
		r.relationships.add(relationship{class: classRelationship, from: from, relationshipType: "patchedBy", to: r.iris(locals)})
	}
	for _, exclusion := range pkg.CVEExclusions {
		statement := exclusion.Reason
		if statement == "" {
			statement = "the upstream lists this vulnerability as not applying to the version it shipped"
		}
		supplier := ""
		if pkg.Supplier != "" {
			supplier = r.iri("agent:" + pkg.Supplier)
		}
		r.relationships.add(relationship{
			class:            classVexNotAffected,
			from:             r.iri("vulnerability:" + exclusion.ID),
			relationshipType: "doesNotAffect",
			to:               []string{from},
			impactStatement:  statement,
			suppliedBy:       supplier,
		})
	}
	if pkg.KnownLeaf {
		r.knownLeaf(from)
	}
}

// knownLeaf states that an element depends on nothing. In 3.0.1 an absent
// relationship is no assertion at all, so the statement CycloneDX makes with
// an empty dependsOn needs a relationship of its own: to NoneElement, "a set
// of Elements with cardinality zero", with a completeness that says the list
// is known to be exhaustive.
func (r *renderer) knownLeaf(from string) {
	r.relationships.add(relationship{
		class: classRelationship, from: from, relationshipType: "dependsOn",
		to: []string{noneElement}, completeness: "complete",
	})
}

func (r *renderer) licenseRelationship(from, relationshipType string, use mapping.LicenseUse) {
	r.relationships.add(relationship{
		class: classRelationship, from: from, relationshipType: relationshipType,
		to: []string{r.licenseTarget(use.License)}, properties: use.Properties,
	})
}

// licenseTarget is what a licence relationship points at: the expression
// element, or one of the two individuals the model defines for "no assertion"
// and "none".
func (r *renderer) licenseTarget(license mapping.License) string {
	switch license.Kind {
	case mapping.LicenseNoAssertion:
		r.usesExpandedLicensing = true
		return noAssertionLic
	case mapping.LicenseNone:
		r.usesExpandedLicensing = true
		return noneLic
	default:
		return r.iri("license:" + license.Expression)
	}
}

func (r *renderer) iris(locals []string) []string {
	out := make([]string, 0, len(locals))
	for _, local := range locals {
		out = append(out, r.iri(local))
	}
	return out
}

func (r *renderer) fileNode(file mapping.File) node {
	return r.element("software_File", file.LocalID, func(n *node) {
		n.Name = file.Name
		n.Description = file.Description
		if file.Hashable() {
			for _, hash := range file.Hashes {
				algorithm, known := hashAlgorithm(hash.Key)
				entry := hashNode{Type: "Hash", Algorithm: algorithm, HashValue: hash.Value}
				if !known {
					entry.Comment = hash.Key
				}
				n.VerifiedUsing = append(n.VerifiedUsing, entry)
			}
			sort.SliceStable(n.VerifiedUsing, func(i, j int) bool {
				if n.VerifiedUsing[i].Algorithm != n.VerifiedUsing[j].Algorithm {
					return n.VerifiedUsing[i].Algorithm < n.VerifiedUsing[j].Algorithm
				}
				return n.VerifiedUsing[i].HashValue < n.VerifiedUsing[j].HashValue
			})
		}
		n.ExternalRef = licenseTextRefs(file.LicenseText)
		n.Extension = extensionOf(file.Properties)
		n.AdditionalPurpose = fileAdditionalPurposes(file)
		n.CopyrightText = strings.Join(file.Copyright, "\n")
		n.PrimaryPurpose = filePurpose(file)
		n.FileKind = "file"
	})
}

// copyrightText is a package's copyright notices, one per line: the curated
// notice first, then every statement sbomb read that names no file to sit on.
// software_copyrightText is "the text of one or more copyright notices", so
// both belong there; software_attributionText is for acknowledgements a
// distributor adds, which neither is.
func copyrightText(pkg mapping.Package) string {
	lines := make([]string, 0, len(pkg.UnfiledCopyright)+1)
	if pkg.Copyright != "" {
		lines = append(lines, pkg.Copyright)
	}
	lines = append(lines, pkg.UnfiledCopyright...)
	return strings.Join(lines, "\n")
}

// licenseTextRefs attaches the licence texts retained from a file to that
// file, as external references of type "license" whose locator is a base64
// data: URI of the bytes. software_attributionText is not the place: 3.0.1
// says an attribution text "is not meant to include the software Package,
// File or Snippet's actual complete license text". The data: URI carries the
// file's bytes unaltered whether or not they are UTF-8 -- a licence text that
// was altered is not the licence text -- and states the character set where
// it is known.
func licenseTextRefs(texts [][]byte) []externalRef {
	var refs []externalRef
	for _, text := range texts {
		mediaType := "text/plain"
		if utf8.Valid(text) {
			mediaType += ";charset=utf-8"
		}
		refs = append(refs, externalRef{
			Type: "ExternalRef", ExternalRefType: "license", ContentType: "text/plain",
			Locator: []string{"data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(text)},
			Comment: licenseTextComment,
		})
	}
	return refs
}

// licenseTextComment says what a licence text reference is, so that a reader
// who meets an opaque data: URI knows what decoding it gives.
const licenseTextComment = "the licence text sbomb retained from this file (licenseTextInSBOM: evidence), byte for byte"

func (r *renderer) fileRelationships(file mapping.File) {
	from := r.iri(file.LocalID)
	for _, use := range file.Detected {
		// Declared is that the file was found to contain the licence, by any
		// detection technique; concluded is the curated case (section 28.11.5).
		relationshipType := "hasDeclaredLicense"
		if use.Acknowledgement == "concluded" {
			relationshipType = "hasConcludedLicense"
		}
		r.licenseRelationship(from, relationshipType, use)
	}
	if file.KnownLeaf {
		r.knownLeaf(from)
	}
}

func (r *renderer) vulnerabilityNode(id string) node {
	return r.element("security_Vulnerability", "vulnerability:"+id, func(n *node) {
		n.Name = id
		kind := "securityOther"
		if strings.HasPrefix(id, "CVE-") {
			kind = "cve"
		}
		n.ExternalIdentifier = []externalIdentifier{{Type: "ExternalIdentifier", ExternalIdentifierType: kind, Identifier: id}}
	})
}

// licenseTextNode defines one LicenseRef-sbomb-* or AdditionRef-sbomb-*
// (SimpleLicensingText is "a license or addition that is not listed"): the
// text the reference stands for, so that a reader who meets the reference can
// look it up. When no licence text was retained the definition is the name
// sbomb observed, and the comment says so -- a licence text is never invented.
// When a text was retained but is not UTF-8, it cannot be this element's text
// field without being altered; the definition is the name, and the comment
// says where the bytes are, so that a reader is not told there is no text when
// there is one.
func (r *renderer) licenseTextNode(custom mapping.CustomLicense) node {
	return r.element("simplelicensing_SimpleLicensingText", "license-text:"+custom.Ref, func(n *node) {
		n.Name = custom.Name
		switch {
		case custom.TextRetained:
		case custom.Addition:
			n.Comment = "sbomb observed only this licence exception name, after WITH; the SPDX exception list does not carry it and no text of it was retained"
		case custom.TextFile != "":
			n.Comment = "sbomb observed this licence name; the licence text was retained but is not UTF-8 text, so it is attached byte for byte, " +
				"as a base64 data: URI external reference, to the file " + custom.TextFile
		default:
			n.Comment = "sbomb observed only this licence name; no licence text was retained (licenseTextInSBOM is off, or no licence file was read)"
		}
		n.LicenseText = custom.Text
	})
}

// expressionNode is one licence expression element. An expression that names a
// licence or an exception sbomb had to define maps each such reference to its
// definition (customIdToUri, "a LicenseRef or AdditionRef string for a Custom
// License or a Custom License Addition"), and one that names anything from the
// SPDX licence list says which version of the list it was checked against.
func (r *renderer) expressionNode(license mapping.License, defined map[string]bool) node {
	return r.element("simplelicensing_LicenseExpression", "license:"+license.Expression, func(n *node) {
		if parsed, err := mapping.ParseExpression(license.Expression); err == nil {
			seen := map[string]bool{}
			for _, id := range append(append([]string(nil), parsed.Licenses...), parsed.Additions...) {
				if defined[id] && !seen[id] {
					seen[id] = true
					n.CustomIDToURI = append(n.CustomIDToURI, dictionaryEntry{Type: "DictionaryEntry", Key: id, Value: r.iri("license-text:" + id)})
				}
			}
			sort.Slice(n.CustomIDToURI, func(i, j int) bool { return n.CustomIDToURI[i].Key < n.CustomIDToURI[j].Key })
		}
		n.LicenseExpression = license.Expression
		if license.ListedIDs {
			n.LicenseListVersion = licenselist.Version
		}
	})
}

func (r *renderer) relationshipNode(number int, rel relationship) node {
	return r.element(rel.class, relationshipLocal(number), func(n *node) {
		n.Extension = extensionOf(rel.properties)
		n.From = rel.from
		n.RelationshipType = rel.relationshipType
		n.To = rel.to
		n.Completeness = rel.completeness
		n.Scope = rel.scope
		n.SuppliedBy = rel.suppliedBy
		n.ImpactStatement = rel.impactStatement
	})
}

// extensionOf carries a property set as the one extension class 3.0.1 defines
// for exactly this: name and value pairs in the shape of CycloneDX properties.
// An empty set is no extension at all; the class requires at least one entry.
func extensionOf(properties []mapping.Property) []extension {
	if len(properties) == 0 {
		return nil
	}
	entries := make([]propertyEntry, 0, len(properties))
	for _, property := range properties {
		entries = append(entries, propertyEntry{Type: "extension_CdxPropertyEntry", Name: property.Name, Value: property.Value})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Value < entries[j].Value
	})
	return []extension{{Type: "extension_CdxPropertiesExtension", Property: entries}}
}
