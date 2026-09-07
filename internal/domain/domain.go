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
	VersionFrom   []string
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
	Properties          map[string][]string
	Files               []FileID
}

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
