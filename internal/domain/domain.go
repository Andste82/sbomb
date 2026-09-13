package domain

import (
	"fmt"
	"strings"
)

type AnchorKey string

type FileID struct {
	Anchor  AnchorKey
	RelPath string
}

func (f FileID) Canonical() string {
	if f.Anchor == "" {
		return f.RelPath
	}
	return fmt.Sprintf("%s:%s", f.Anchor, strings.TrimPrefix(f.RelPath, "/"))
}

type NodeKind string

const (
	NodeProduct         NodeKind = "product"
	NodeArtifact        NodeKind = "artifact"
	NodePackage         NodeKind = "package"
	NodeImage           NodeKind = "image"
	NodeArchive         NodeKind = "archive"
	NodeArchiveMember   NodeKind = "archive-member"
	NodeObject          NodeKind = "object"
	NodeTranslationUnit NodeKind = "translation-unit"
	NodeSource          NodeKind = "source"
	NodeHeader          NodeKind = "header"
	NodeAsset           NodeKind = "asset"
	NodeGenerator       NodeKind = "generator"
	NodeGeneratorInput  NodeKind = "generator-input"
	NodeToolchainFile   NodeKind = "toolchain-file"
)

type EvidenceType string

type Strength string

type Confidence string

const (
	ConfidenceHigh    Confidence = "high"
	ConfidenceMedium  Confidence = "medium"
	ConfidenceLow     Confidence = "low"
	ConfidenceUnknown Confidence = "unknown"
)

func (c Confidence) Float() float64 {
	switch c {
	case ConfidenceHigh:
		return 0.9
	case ConfidenceMedium:
		return 0.6
	case ConfidenceLow:
		return 0.3
	default:
		return 0.1
	}
}

func (c Confidence) Downgrade() Confidence {
	switch c {
	case ConfidenceHigh:
		return ConfidenceMedium
	case ConfidenceMedium:
		return ConfidenceLow
	case ConfidenceLow:
		return ConfidenceUnknown
	default:
		return ConfidenceUnknown
	}
}

type NodeID string

type Node struct {
	ID         NodeID
	Kind       NodeKind
	File       *FileID
	Attributes map[string]string
}

type Edge struct {
	From       NodeID
	To         NodeID
	Type       EvidenceType
	Strength   Strength
	Confidence Confidence
	Source     string
	Adapter    string
	Raw        string
	Attributes map[string]string
	Downgrades []string
}

type FileClass string

const (
	FileClassSource          FileClass = "source"
	FileClassHeader          FileClass = "header"
	FileClassGeneratedSource FileClass = "generated-source"
	FileClassGeneratedHeader FileClass = "generated-header"
	FileClassObject          FileClass = "object"
	FileClassArchive         FileClass = "archive"
	FileClassSharedLibrary   FileClass = "shared-library"
	FileClassAsset           FileClass = "asset"
	FileClassUnknown         FileClass = "unknown"
)

type UsedFile struct {
	ID          FileID
	Class       FileClass
	HeaderClass string
	Hashes      map[string]string
	SizeBytes   int64
	Missing     bool
	ComponentID string
	Properties  map[string][]string
}

type Component struct {
	ID            string
	BomRef        string
	Name          string
	Version       string
	VersionSource string
	VersionConf   Confidence
	Type          string
	PURL          string
	CPE           string
	Supplier      string
	Root          *FileID
	Scope         string
	Licenses      []LicenseFinding
	// LicenseEvidence is what was observed rather than concluded: the
	// complete licence texts found in the component's files. Section 22.4
	// requires evidence and assumption to be distinguishable, and CycloneDX
	// carries it in component.evidence.licenses.
	LicenseEvidence []LicenseFinding
	DetectedBy      string
	// VCS is the repository a package manager recorded for this component. It
	// is a resolved fact rather than a rendered property: CycloneDX has a
	// field for it (externalReferences), so the writer decides how to say it.
	VCS *VCSRecord
	// EnvironmentProvided says the target expects this component to be there
	// rather than carrying it: a system library the artifact links against
	// dynamically. It is not the same as system scope, which says only where
	// the files live -- a system archive linked statically is bundled into the
	// artifact and is not provided by anything.
	EnvironmentProvided bool
	// DistributionRole says whether the component is inside what is
	// distributed or only helped build it (section 24.5). It is derived from
	// the evidence graph alone: "distributed" unless every path from an
	// artifact to every one of the component's files runs through a
	// generator-input, generator-output or toolchain edge.
	DistributionRole string
	// LinkageForms is how the component's files reach the artifact (section
	// 24.5), sorted and deduplicated. A component may carry several: a
	// library can be a static archive member in one artifact and a header in
	// another.
	LinkageForms []string
	// ArchiveMembersUsed is "<used>/<total>" for a component the linker took
	// members out of a static archive for, and empty when no archive index
	// could be read. It is the number that makes section 12 visible in the
	// document: three members compiled, one extracted.
	ArchiveMembersUsed string
	// HeaderOnly says the component contributed no object code at all. It is
	// a conclusion about the component and not about one file, which is why
	// it is a field and not one of LinkageForms.
	HeaderOnly bool
	// Modification is what section 19.4 could establish about whether this
	// component was modified. Three states, because absence of information is
	// not evidence of an unmodified tree.
	Modification ModificationRecord
	// LicenseArtifacts are the recognized license files the component root
	// carries, kept verbatim (section 22.9). They are the deliverable an
	// attribution obligation is satisfied with: for MIT and the BSD family
	// the rights holder is inside the text, so the identifier in Licenses is
	// an index into a catalogue and these are the bytes. Ordered by
	// (Kind, File).
	LicenseArtifacts []LicenseArtifact
	// Copyrights are the copyright statements the component's own files and
	// retained artifacts state (section 22.10), verbatim and deduplicated,
	// ordered by text. They are an observation: MIT and the BSD family
	// require the notice to be reproduced, and the holder is named in the
	// notice rather than in the identifier.
	Copyrights []CopyrightStatement
	// SourceObligations are the obligation names the component's licence
	// triggers, sorted (section 32.6). They are a statement about the
	// licence and never about compliance: sbomb cannot observe whether an
	// obligation was discharged, and it does not produce the material.
	SourceObligations []string
	// Copyright is the single concluded copyright notice of the component,
	// set from curated configuration alone. CycloneDX has one string field
	// for the conclusion and an array for the observation, and section 22.4
	// keeps the two apart: nothing sbomb read is ever promoted into this.
	Copyright  string
	Properties map[string][]string
	Files      []FileID
}

// ModificationStatus is the tri-state of section 19.4. The third state is the
// point of it: an absent answer must never be published as "not modified",
// because that turns a check nobody ran into a claim.
type ModificationStatus string

const (
	// ModificationUnknown is the default, and the value a component keeps
	// whenever no positive check ran -- including when introspection is off.
	ModificationUnknown ModificationStatus = "unknown"
	// ModificationModified was established: a dirty tree, or package metadata
	// recording an applied patch.
	ModificationModified ModificationStatus = "true"
	// ModificationUnmodified was established: a git root at the component
	// root, clean, standing exactly on the tag it records.
	ModificationUnmodified ModificationStatus = "false"
)

// ModificationRecord is the evidence behind a modification status, so that the
// document can carry the signal that decided the answer rather than the answer
// alone. CycloneDX has a field for exactly this -- component.pedigree -- and
// what is collected here is what fills it.
type ModificationRecord struct {
	// Status is the tri-state answer. The zero value is the empty string,
	// which callers read as ModificationUnknown; the derivation that fills
	// this record -- resolveModification in internal/generate -- always sets
	// one of the three.
	Status ModificationStatus
	// Signal names, in one line of prose, what decided the answer. It becomes
	// pedigree.notes, because a pedigree without it says that something was
	// established and not what.
	Signal string
	// Remediation is what would let the question be answered, for the finding
	// an unknown status raises. It travels with the signal because the two are
	// about the same gap, and which gap it is decides both.
	Remediation string
	// Commit is the commit the component root's checkout stands on, when it
	// was read. It becomes pedigree.commits[].uid.
	Commit string
	// Patches are the patches a package manager recorded as applied. They
	// become pedigree.patches[], and any one of them makes Status "true".
	Patches []Patch
}

// Patch is one patch a package manager recorded as applied to a component.
// sbomb records that a patch was applied, never its content: pedigree.diff
// stays empty, because a diff is material sbomb does not own (requirement R6).
type Patch struct {
	// File is the patch as the metadata names it -- a file name, not a path
	// that has been resolved against anything.
	File string
	// Type is the CycloneDX patches[].type enum. A patch a manager applied is
	// "unofficial" unless the manager itself says which kind it is.
	Type string
	// Description is what the metadata says about the patch, when it says
	// anything. It does not reach pedigree.patches[].resolves -- that field is
	// for the issues a patch closes -- and is carried into pedigree.notes
	// beside the file name instead.
	Description string
	// Source is the file the record was read from, so that a reviewer can
	// check it.
	Source string
}

// PatchType values, which are the CycloneDX enum and nothing else.
const (
	PatchUnofficial = "unofficial"
	PatchMonkey     = "monkey"
	PatchBackport   = "backport"
	PatchCherryPick = "cherry-pick"
)

// Distribution roles of section 24.5.
const (
	RoleDistributed   = "distributed"
	RoleBuildTimeOnly = "build-time-only"
)

// Linkage forms of section 24.5. The list is closed: a form nothing can
// establish is not emitted rather than guessed.
const (
	LinkageStaticArchiveMember = "static-archive-member"
	LinkageStaticObject        = "static-object"
	LinkageDynamic             = "dynamic"
	LinkageHeaderOnly          = "header-only"
	LinkageEmbeddedAsset       = "embedded-asset"
	LinkageGeneratedSource     = "generated-source"
	LinkageBuildTool           = "build-tool"
)

// CopyrightStatement is one copyright notice as a file states it (section
// 22.10). Nothing in Text has been rewritten: no year range is reformatted,
// no holder is normalized, no whitespace is collapsed. Deduplication compares
// a normalized key that is never stored here and never displayed.
type CopyrightStatement struct {
	// Text is the statement, a verbatim substring of the line it was found
	// in: for a conventional notice it begins at the word Copyright or the
	// marker before it, and for a REUSE tag it begins after the tag.
	Text string
	// File is the identity of the file the statement was read from, never the
	// path it was read from (section 7.8).
	File FileID
}

// LicenseArtifact is one retained license file of a component root, per
// section 22.9. The bytes are unmodified -- line endings included -- because a
// reproduced notice that was reformatted is not the notice the license said to
// reproduce.
type LicenseArtifact struct {
	// Kind is "license" for a grant (LICENSE, LICENCE, COPYING,
	// LICENSE-<id>), "notice" for NOTICE, "copyright" for COPYRIGHT. A grant
	// answers section 22.2; the other two answer only what must be
	// reproduced.
	Kind string
	// File is the identity of the file, never the path it was read from
	// (section 7.8): the component root's identity with the file name below
	// it.
	File FileID
	// SHA256 is the hex digest of Bytes, so that a consumer can check the
	// text it received against what the tool saw.
	SHA256 string
	Bytes  []byte
	// DetectedID is the SPDX identifier detection settled on, empty when none
	// of the techniques of section 22.3 did. It stays empty for a "notice" or
	// "copyright" artifact on purpose: an identifier recorded there is one
	// inference away from becoming the component's license, which section
	// 22.2 forbids.
	DetectedID string
	// Technique names which technique of section 22.3 produced DetectedID.
	Technique string
}

// LicenseArtifactKind values, spelled once.
const (
	LicenseArtifactLicense   = "license"
	LicenseArtifactNotice    = "notice"
	LicenseArtifactCopyright = "copyright"
)

// VCSRecord is where a component's source is kept, as the manager recorded it.
// It is never a stand-in for a supplier (section 20.5).
type VCSRecord struct {
	URL    string
	Commit string
	Dirty  bool
}

type LicenseFinding struct {
	Expression string
	SPDXID     string
	Name       string
	Evidence   string
	Confidence Confidence
	Source     string
	Reason     string
	Conflicts  []string
	// Technique names which of the detection techniques of section 22.3
	// produced this, so that a reviewer can tell an SPDX identifier read out
	// of a file from a digest match from a template match.
	Technique string
}

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Finding is the machine-readable diagnostic of section 26.1. The JSON field
// names are normative: a consumer greps for "id" and "severity", not for Go
// field names.
type Finding struct {
	ID           string         `json:"id"`
	Severity     Severity       `json:"severity"`
	Subject      Subject        `json:"subject"`
	Message      string         `json:"message"`
	Detail       map[string]any `json:"detail,omitempty"`
	Evidence     []string       `json:"evidence,omitempty"`
	Remediation  string         `json:"remediation,omitempty"`
	Waived       bool           `json:"waived"`
	WaiverReason string         `json:"waiverReason,omitempty"`
}

type Subject struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}
