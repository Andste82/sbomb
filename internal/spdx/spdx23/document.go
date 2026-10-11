package spdx23

import "encoding/json"

// The types below are the JSON serialization of SPDX 2.3, in the member
// order sbomb writes. The renderer fills only what sbomb states; the decoder
// of the validator reads the same types, so the fields a document somebody
// else wrote may carry -- documentDescribes, externalDocumentRefs, hasFiles,
// packageVerificationCode, licenseInfoFromFiles, snippets -- are here too,
// and are never set by the renderer.

type document struct {
	SPDXVersion                string                `json:"spdxVersion"`
	DataLicense                string                `json:"dataLicense"`
	SPDXID                     string                `json:"SPDXID"`
	Name                       string                `json:"name"`
	DocumentNamespace          string                `json:"documentNamespace"`
	ExternalDocumentRefs       []externalDocumentRef `json:"externalDocumentRefs,omitempty"`
	CreationInfo               creationInfo          `json:"creationInfo"`
	Annotations                []annotation          `json:"annotations,omitempty"`
	DocumentDescribes          []string              `json:"documentDescribes,omitempty"`
	Packages                   []pkg                 `json:"packages,omitempty"`
	Files                      []file                `json:"files,omitempty"`
	Snippets                   []snippet             `json:"snippets,omitempty"`
	HasExtractedLicensingInfos []extractedLicense    `json:"hasExtractedLicensingInfos,omitempty"`
	Relationships              []relationship        `json:"relationships,omitempty"`
}

type externalDocumentRef struct {
	ExternalDocumentID string          `json:"externalDocumentId"`
	SPDXDocument       string          `json:"spdxDocument"`
	Checksum           json.RawMessage `json:"checksum"`
}

type creationInfo struct {
	Created            string   `json:"created"`
	Creators           []string `json:"creators"`
	LicenseListVersion string   `json:"licenseListVersion,omitempty"`
}

type annotation struct {
	AnnotationDate string `json:"annotationDate"`
	AnnotationType string `json:"annotationType"`
	Annotator      string `json:"annotator"`
	Comment        string `json:"comment"`
}

type pkg struct {
	SPDXID                  string          `json:"SPDXID"`
	Name                    string          `json:"name"`
	VersionInfo             string          `json:"versionInfo,omitempty"`
	PackageFileName         string          `json:"packageFileName,omitempty"`
	Supplier                string          `json:"supplier,omitempty"`
	Originator              string          `json:"originator,omitempty"`
	DownloadLocation        string          `json:"downloadLocation"`
	FilesAnalyzed           *bool           `json:"filesAnalyzed,omitempty"`
	PackageVerificationCode json.RawMessage `json:"packageVerificationCode,omitempty"`
	Checksums               []checksum      `json:"checksums,omitempty"`
	SourceInfo              string          `json:"sourceInfo,omitempty"`
	LicenseConcluded        string          `json:"licenseConcluded,omitempty"`
	LicenseInfoFromFiles    []string        `json:"licenseInfoFromFiles,omitempty"`
	LicenseDeclared         string          `json:"licenseDeclared,omitempty"`
	CopyrightText           string          `json:"copyrightText,omitempty"`
	Description             string          `json:"description,omitempty"`
	Comment                 string          `json:"comment,omitempty"`
	ExternalRefs            []externalRef   `json:"externalRefs,omitempty"`
	HasFiles                []string        `json:"hasFiles,omitempty"`
	PrimaryPackagePurpose   string          `json:"primaryPackagePurpose,omitempty"`
	Annotations             []annotation    `json:"annotations,omitempty"`
}

type checksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type externalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

type file struct {
	SPDXID             string       `json:"SPDXID"`
	FileName           string       `json:"fileName"`
	FileTypes          []string     `json:"fileTypes,omitempty"`
	Checksums          []checksum   `json:"checksums"`
	LicenseConcluded   string       `json:"licenseConcluded,omitempty"`
	LicenseInfoInFiles []string     `json:"licenseInfoInFiles,omitempty"`
	CopyrightText      string       `json:"copyrightText,omitempty"`
	NoticeText         string       `json:"noticeText,omitempty"`
	Comment            string       `json:"comment,omitempty"`
	Annotations        []annotation `json:"annotations,omitempty"`
}

type snippet struct {
	SPDXID           string `json:"SPDXID"`
	SnippetFromFile  string `json:"snippetFromFile"`
	LicenseConcluded string `json:"licenseConcluded,omitempty"`
}

type extractedLicense struct {
	LicenseID     string `json:"licenseId"`
	Name          string `json:"name,omitempty"`
	ExtractedText string `json:"extractedText"`
	Comment       string `json:"comment,omitempty"`
}

type relationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
	Comment            string `json:"comment,omitempty"`
}
