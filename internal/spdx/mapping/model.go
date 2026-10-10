// Package mapping turns a format-neutral Document into the statements an SPDX
// document makes, in a form both SPDX versions can spell.
//
// SPDX 3.0.1 and SPDX 2.3 share nothing at the serialization level -- one is a
// JSON-LD graph of elements and relationships, the other a flat list of
// packages and files with typed fields -- but they describe the same things:
// packages, files, which file a package contains, how a deliverable links a
// library, what licence was concluded and what was observed, which files are
// evidence, which patches were applied, which upstream claims a CVE does not
// apply. That is what lives here. Nothing in this package is a spelling:
// there are no relationship type names, no purpose enum values, no hash
// algorithm names, no element identifiers and no relationship numbers. Each
// renderer owns those, and a second renderer costs one more package rather
// than a second mapping.
//
// The model is built once per document and is canonical: every slice is
// sorted by a total key and there are no maps, so that its JSON encoding can
// serve as the digest the reproducible document identity is derived from
// (Digest) and two runs over the same evidence build the same model whatever
// order discovery handed things over in.
//
// What this does not provide for SPDX 2.3 (#3), deliberately: a SHA-1 of every
// file, which 2.3 makes mandatory and which needs a hashing pass below the
// writer seam; a rule for files that cannot be hashed at all (File.Missing and
// the patch origin say which they are, so 2.3 can count or refuse them); and
// a decision about extractedText when no licence text was retained
// (CustomLicense.TextRetained says when that is the case). The model carries
// what those decisions need; it does not make them.
package mapping

import "github.com/example/sbomb/internal/sbommap"

// Property is one sbomb:* name and value (appendix B).
type Property = sbommap.Property

// Options are the serialization choices that change what the model states.
type Options struct {
	// SpecVersion is the version being written. It is part of the model, and
	// therefore of the digest, so that two documents of one build in two
	// versions do not share an identity.
	SpecVersion string
	// LicenseText is the licenseTextInSBOM setting (section 33.1): with
	// "evidence", the retained licence texts are carried, byte for byte, by
	// the licence files they were read from, and define the references sbomb
	// mints for licences the list does not carry.
	LicenseText string
	// Reproducible says the document identity is derived from the model.
	Reproducible bool
	// CustomAdditions says the version being written can name an exception
	// the SPDX list does not carry: SPDX 3.x has AdditionRef for it, SPDX 2.3
	// has nothing. With it, an unlisted exception becomes an AdditionRef
	// sbomb mints and the expression keeps its structure; without it, such an
	// expression -- and one with an AdditionRef the build stated -- becomes
	// one LicenseRef for the whole text. It is the only routing decision that
	// depends on the version, and it is made here rather than in a renderer.
	CustomAdditions bool
}

// Model is everything one SPDX document states.
type Model struct {
	SpecVersion string
	// Created is the creation time, normalised to YYYY-MM-DDThh:mm:ssZ.
	Created string
	// Creator is the organisation that created the document: the tool vendor.
	Creator string
	Run     Run
	// Packages are the product, the artifacts and the components, ordered by
	// role and then by local identity.
	Packages []Package
	// Files are the used files, the retained licence files and the patch
	// files, ordered by local identity. A file that is several of these is
	// one entry, because it is one thing.
	Files []File
	// Edges are the structural statements, ordered by (From, Kind, To).
	Edges []Edge
	// Agents are the distinct organisation names the document mentions --
	// the creator, suppliers and originators -- sorted.
	Agents []string
	// Licenses are the distinct licence expressions the document refers to,
	// sorted by expression. NOASSERTION and NONE are not among them: they are
	// kinds, not expressions, and every SPDX version has its own word for them.
	Licenses []License
	// Custom are the licences and exceptions sbomb had to name itself
	// (LicenseRef-sbomb-*, AdditionRef-sbomb-*), one per reference, sorted by
	// reference.
	Custom []CustomLicense
}

// Run is the run that wrote the document.
type Run struct {
	ToolName    string
	ToolVendor  string
	ToolVersion string
	// Properties are the run's property set, sorted by (name, value).
	Properties []Property
}

// PackageRole says what a package is in the document's structure.
type PackageRole int

const (
	RoleProduct PackageRole = iota
	RoleArtifact
	RoleComponent
)

// Package is the product, an artifact or a component.
type Package struct {
	LocalID     string
	Role        PackageRole
	Name        string
	Version     string
	PURL        string
	CPEs        []string
	Supplier    string
	Originator  string
	Description string
	// Type is the component type exactly as the document carries it (the
	// CycloneDX vocabulary, section 19.1); "" stays "", and is what
	// sbomb:component:type states.
	Type string
	// StatedType is the type every writer states for the package, an unset
	// Type settled once (sbommap.EffectiveType). Each renderer maps it onto
	// its own purpose vocabulary, so the purpose, the BSI triple and the
	// CycloneDX document of the same run all derive from one answer.
	StatedType string
	HeaderOnly bool
	// Copyright is the curated notice alone (section 22.4). What sbomb read
	// sits on the file it was read from.
	Copyright string
	// UnfiledCopyright are copyright statements sbomb read that name no file
	// to sit on, deduplicated and sorted. They are copyright notices all the
	// same, and every version states them as the package's copyright text,
	// after the curated notice.
	UnfiledCopyright []string
	// SourceInfo is the prose that decided the modification status.
	SourceInfo string
	VCSURL     string
	// Concluded is the licence the component is concluded to be under; empty
	// where nothing was concluded (a product without a configured licence).
	Concluded []LicenseUse
	// Declared are the licences observed in the component's files.
	Declared      []LicenseUse
	CVEExclusions []CVEExclusion
	// EvidenceFiles are the local identities of the files that are evidence
	// for the component: its retained licence files and the files its copyright
	// statements were read from.
	EvidenceFiles []string
	Patches       []Patch
	// KnownLeaf says the document knows the package depends on nothing.
	KnownLeaf  bool
	Properties []Property
}

// Origin says how a file came to be in the document. A file can be several.
type Origin uint8

const (
	// OriginUsed: the build used the file.
	OriginUsed Origin = 1 << iota
	// OriginEvidence: the file is evidence for a component -- a retained
	// licence, notice or copyright file, or a file a copyright statement was
	// read from.
	OriginEvidence
	// OriginPatch: the file is a recorded patch.
	OriginPatch
	// OriginLicenseFile: the file is one of the licence, notice or copyright
	// files section 22.9 retained -- documentation by what it is. A source
	// file a copyright line was read from is evidence, but not this: reading
	// a notice out of kitchen.c does not make kitchen.c documentation.
	OriginLicenseFile
)

// File is a used file, a retained licence or copyright file, or a patch.
type File struct {
	LocalID string
	// Canonical is the canonical path; empty for a patch, which is named only
	// by the metadata that recorded it.
	Canonical string
	// Name is the canonical path for a used or evidence file -- the base name
	// alone cannot tell the many LICENSE files of a build apart -- and the
	// patch file as its metadata names it for a patch.
	Name        string
	Description string
	Origin      Origin
	// Class is the file class of discovery verbatim; "" for a file that is
	// only evidence or only a patch.
	Class  string
	Hashes []Hash
	// Missing says no bytes of the file were read.
	Missing   bool
	SizeBytes int64
	// Copyright is every copyright statement any component read from this
	// file, deduplicated and sorted.
	Copyright []string
	// LicenseText are the licence texts section 22.9 retained from this file,
	// when the run asked for them, deduplicated and sorted. They are the
	// file's own bytes, not text a distributor adds: SPDX 3.0.1 rules a
	// complete licence text out of attributionText explicitly, so a renderer
	// attaches them to the file rather than spelling them as attribution.
	LicenseText [][]byte
	// Detected are the licences detected in this file, with the
	// acknowledgement that decides whether they were declared or concluded.
	Detected []LicenseUse
	// PatchFor is the local identity of the component a patch applies to.
	PatchFor   string
	KnownLeaf  bool
	Properties []Property
}

// Hashable reports whether the file's bytes were read, so that a digest of it
// can be stated.
func (f File) Hashable() bool { return !f.Missing && f.Origin&OriginPatch == 0 }

// Hash is one digest, keyed by the algorithm name discovery used ("SHA-256").
type Hash struct {
	Key   string
	Value string
}

// LicenseKind separates the two statements that are not expressions.
type LicenseKind int

const (
	LicenseExpression LicenseKind = iota
	LicenseNoAssertion
	LicenseNone
)

// License is one licence target.
type License struct {
	Kind       LicenseKind
	Expression string
	// ListedIDs says the expression names at least one identifier of the SPDX
	// licence list, so that the list version it was checked against is worth
	// stating.
	ListedIDs bool
}

// LicenseUse is one licence statement about a package or file.
type LicenseUse struct {
	License License
	// Acknowledgement is "declared" or "concluded" for a licence detected in a
	// retained licence file: declared for whatever a detection technique
	// found in it, concluded where the component's licence is curated
	// (fileAcknowledgement). Empty elsewhere.
	Acknowledgement string
	// Properties qualify this one statement: where the licence was read, by
	// which technique, with what confidence.
	Properties []Property
}

// CustomLicense defines a LicenseRef-sbomb-* or AdditionRef-sbomb-* reference.
type CustomLicense struct {
	Ref string
	// Addition says the reference names an exception (an AdditionRef), not a
	// licence.
	Addition bool
	// Name is the text the reference stands for, exactly as it was observed.
	Name string
	// Text is the retained licence text when there was one, and the observed
	// name otherwise; TextRetained says which.
	Text         string
	TextRetained bool
	// TextFile is the canonical path of the evidence file whose retained
	// licence text is not UTF-8, and so cannot be Text: the bytes were
	// retained and travel on that file, byte for byte. Empty otherwise.
	TextFile string
}

// CVEExclusion is one upstream claim that a CVE does not apply.
type CVEExclusion struct {
	ID     string
	Reason string
}

// Patch is one applied patch, with the local identity of the file element
// that represents it.
type Patch struct {
	LocalID     string
	File        string
	Type        string
	Source      string
	Description string
}

// EdgeKind is what a structural statement says, in words neither SPDX version
// uses.
type EdgeKind string

const (
	// EdgeContains: the From element contains each To element -- a component
	// its files, the product its artifacts, a grouping its members.
	EdgeContains EdgeKind = "contains"
	// EdgeEmbeds: an asset is embedded into the deliverable.
	EdgeEmbeds EdgeKind = "embeds"
	// EdgeStaticLink: the deliverable statically links the component.
	EdgeStaticLink EdgeKind = "static-link"
	// EdgeDynamicLink: the deliverable dynamically links the component.
	EdgeDynamicLink EdgeKind = "dynamic-link"
	// EdgeProvidedDependency: the deliverable expects the environment to
	// provide the component rather than carrying it.
	EdgeProvidedDependency EdgeKind = "provided-dependency"
	// EdgeTool: the component was used as a tool to build the deliverable.
	EdgeTool EdgeKind = "tool"
	// EdgeDependsOn: the From element depends on the To element, and nothing
	// more specific was established.
	EdgeDependsOn EdgeKind = "depends-on"
)

// Edge is one structural statement between two local identities.
type Edge struct {
	From string
	Kind EdgeKind
	To   string
}
