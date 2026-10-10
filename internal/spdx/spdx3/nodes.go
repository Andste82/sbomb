package spdx3

// The JSON shapes of the nodes sbomb writes. One struct serves every class:
// the schema forbids every key a class does not define, and omitempty leaves
// out every key a node does not set, so a node carries exactly the keys of its
// class that have something to say. The field order is the key order of the
// output -- type and identity first, then the Element keys, the Artifact keys,
// the class keys -- so a reader finds the same thing in the same place in every
// node, and the order never depends on a map.

const (
	contextURL       = "https://spdx.org/rdf/3.0.1/spdx-context.jsonld"
	creationInfoID   = "_:creationInfo1"
	specVersion      = "3.0.1"
	noneElement      = "NoneElement"
	noAssertionLic   = "expandedlicensing_NoAssertionLicense"
	noneLic          = "expandedlicensing_NoneLicense"
	noAssertionElem  = "NoAssertionElement"
	spdxOrganization = "SpdxOrganization"
)

// predefinedIndividuals are the elements the model itself defines. A document
// refers to them by name and never defines them.
var predefinedIndividuals = map[string]bool{
	noneElement:      true,
	noAssertionElem:  true,
	noAssertionLic:   true,
	noneLic:          true,
	spdxOrganization: true,
}

type document struct {
	Context string `json:"@context"`
	Graph   []node `json:"@graph"`
}

type node struct {
	Type         string `json:"type"`
	SpdxID       string `json:"spdxId,omitempty"`
	ID           string `json:"@id,omitempty"`
	CreationInfo string `json:"creationInfo,omitempty"`

	// CreationInfo.
	SpecVersion  string   `json:"specVersion,omitempty"`
	Created      string   `json:"created,omitempty"`
	CreatedBy    []string `json:"createdBy,omitempty"`
	CreatedUsing []string `json:"createdUsing,omitempty"`

	// Element.
	Name               string               `json:"name,omitempty"`
	Description        string               `json:"description,omitempty"`
	Comment            string               `json:"comment,omitempty"`
	VerifiedUsing      []hashNode           `json:"verifiedUsing,omitempty"`
	ExternalRef        []externalRef        `json:"externalRef,omitempty"`
	ExternalIdentifier []externalIdentifier `json:"externalIdentifier,omitempty"`
	Extension          []extension          `json:"extension,omitempty"`

	// Artifact.
	OriginatedBy []string `json:"originatedBy,omitempty"`
	SuppliedBy   string   `json:"suppliedBy,omitempty"`

	// Relationship, LifecycleScopedRelationship and the VEX assessment.
	From             string   `json:"from,omitempty"`
	RelationshipType string   `json:"relationshipType,omitempty"`
	To               []string `json:"to,omitempty"`
	Completeness     string   `json:"completeness,omitempty"`
	Scope            string   `json:"scope,omitempty"`
	ImpactStatement  string   `json:"security_impactStatement,omitempty"`

	// ElementCollection, SpdxDocument and software_Sbom.
	Element            []string `json:"element,omitempty"`
	ProfileConformance []string `json:"profileConformance,omitempty"`
	RootElement        []string `json:"rootElement,omitempty"`
	SbomType           []string `json:"software_sbomType,omitempty"`

	// software_SoftwareArtifact.
	AdditionalPurpose []string `json:"software_additionalPurpose,omitempty"`
	CopyrightText     string   `json:"software_copyrightText,omitempty"`
	PrimaryPurpose    string   `json:"software_primaryPurpose,omitempty"`

	// software_File.
	FileKind string `json:"software_fileKind,omitempty"`

	// software_Package.
	PackageURL     string `json:"software_packageUrl,omitempty"`
	PackageVersion string `json:"software_packageVersion,omitempty"`
	SourceInfo     string `json:"software_sourceInfo,omitempty"`

	// simplelicensing_LicenseExpression and simplelicensing_SimpleLicensingText.
	CustomIDToURI      []dictionaryEntry `json:"simplelicensing_customIdToUri,omitempty"`
	LicenseExpression  string            `json:"simplelicensing_licenseExpression,omitempty"`
	LicenseListVersion string            `json:"simplelicensing_licenseListVersion,omitempty"`
	LicenseText        string            `json:"simplelicensing_licenseText,omitempty"`
}

type hashNode struct {
	Type      string `json:"type"`
	Algorithm string `json:"algorithm"`
	HashValue string `json:"hashValue"`
	Comment   string `json:"comment,omitempty"`
}

type externalIdentifier struct {
	Type                   string `json:"type"`
	ExternalIdentifierType string `json:"externalIdentifierType"`
	Identifier             string `json:"identifier"`
}

type externalRef struct {
	Type            string   `json:"type"`
	ExternalRefType string   `json:"externalRefType"`
	Locator         []string `json:"locator"`
	ContentType     string   `json:"contentType,omitempty"`
	Comment         string   `json:"comment,omitempty"`
}

type extension struct {
	Type     string          `json:"type"`
	Property []propertyEntry `json:"extension_cdxProperty"`
}

// propertyEntry is one sbomb:* property. The value is always written, even
// when empty: CycloneDX writes an empty value too, and the two documents carry
// the same property set.
type propertyEntry struct {
	Type  string `json:"type"`
	Name  string `json:"extension_cdxPropName"`
	Value string `json:"extension_cdxPropValue"`
}

type dictionaryEntry struct {
	Type  string `json:"type"`
	Key   string `json:"key"`
	Value string `json:"value"`
}
