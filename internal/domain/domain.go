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
	DetectedBy    string
	Properties    map[string][]string
	Files         []FileID
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
}

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Finding struct {
	ID           string
	Severity     Severity
	Subject      Subject
	Message      string
	Detail       map[string]any
	Evidence     []string
	Remediation  string
	Waived       bool
	WaiverReason string
}

type Subject struct {
	Kind string
	Ref  string
}
