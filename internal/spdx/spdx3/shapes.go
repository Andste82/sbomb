package spdx3

import (
	"fmt"
)

// Most of a large document is two kinds of node, a file and a relationship,
// every one written in the same few shapes. Walking each of them through the
// schema cost most of the time and memory validation takes, although the
// schema can only ever say yes to them. shapeTable recognises those shapes by
// hand and vouches for a node only when the schema would accept it; any other
// node -- another class, an unexpected key, a value of the wrong kind -- is
// left to the schema, which also words every refusal. The table reads every
// enum and pattern it needs from the embedded schema, so it cannot disagree
// with the schema about which values are allowed.
//
// What each shape has to satisfy, read from the 3.0.1 schema:
//
//   - The node wrapper of schemaSet: an object, no key the class leaves
//     unevaluated. Every key a shape admits is a property of the class.
//   - The class definition: "type" is the class, "spdxId" an IRI.
//   - SHACLClass: "type" is listed in its enum (both classes are).
//   - Element_props: "creationInfo" is present and is CreationInfo_derived, of
//     which a blank node or IRI string is the second branch; "name" is a
//     string; "verifiedUsing" an array of IntegrityMethod_derived, each a Hash
//     object or a blank node or IRI; "extension" an array of
//     extension_Extension_derived (conformingCdxExtension).
//   - software_SoftwareArtifact_props and software_File_props: the purpose and
//     file kind are in their enums, the copyright text is a string.
//   - Relationship_props: "from" and "relationshipType" are present, "to" is a
//     non-empty array, the ends are Element_derived (a named individual such
//     as NoneElement, or an IRI or blank node, are branches of it), the type
//     and completeness are in their enums; a relationship is an Element, so
//     it may carry extensions as a file does.
//   - Hash: "type" is "Hash", "algorithm" in its enum, "hashValue" a string,
//     and nothing else, because Hash_derived refuses unevaluated keys.
type shapeTable struct {
	iri, blankNode    matcher
	relationshipType  map[string]bool
	completeness      map[string]bool
	primaryPurpose    map[string]bool
	fileKind          map[string]bool
	hashAlgorithm     map[string]bool
	classes, fileKeys map[string]bool
	relationshipKeys  map[string]bool
	// individuals are the named elements Element_derived admits beside a
	// reference, such as NoneElement, which states a known leaf.
	individuals map[string]bool
	// elidable are the array properties elide thins, with the items it may
	// leave out.
	elidable []elidableArray
}

type elidableArray struct {
	key      string
	accepted func(any) bool
}

type matcher interface{ MatchString(string) bool }

// newShapeTable reads the enums and patterns the shapes use from the parsed
// schema document.
func newShapeTable(document any) (*shapeTable, error) {
	root, _ := document.(map[string]any)
	definitions, _ := root["$defs"].(map[string]any)
	definition := func(name string) (map[string]any, error) {
		found, isObject := definitions[name].(map[string]any)
		if !isObject {
			return nil, fmt.Errorf("the SPDX schema has no definition %s", name)
		}
		return found, nil
	}
	enum := func(name string) (map[string]bool, error) {
		found, err := definition(name)
		if err != nil {
			return nil, err
		}
		values, isArray := found["enum"].([]any)
		if !isArray {
			return nil, fmt.Errorf("the SPDX schema definition %s is not an enum", name)
		}
		set := make(map[string]bool, len(values))
		for _, value := range values {
			if text, isString := value.(string); isString {
				set[text] = true
			}
		}
		return set, nil
	}
	pattern := func(name string) (matcher, error) {
		found, err := definition(name)
		if err != nil {
			return nil, err
		}
		text, isString := found["pattern"].(string)
		if !isString || found["type"] != "string" {
			return nil, fmt.Errorf("the SPDX schema definition %s is not a string pattern", name)
		}
		compiled, err := schemaRegexp(text)
		if err != nil {
			return nil, err
		}
		return compiled.(matcher), nil
	}

	table := &shapeTable{
		fileKeys: map[string]bool{
			"type": true, "spdxId": true, "creationInfo": true, "name": true,
			"verifiedUsing": true, "extension": true, "software_fileKind": true,
			"software_primaryPurpose": true, "software_copyrightText": true,
		},
		relationshipKeys: map[string]bool{
			"type": true, "spdxId": true, "creationInfo": true, "from": true,
			"to": true, "relationshipType": true, "completeness": true,
			"extension": true,
		},
	}
	var err error
	if table.iri, err = pattern("IRI"); err != nil {
		return nil, err
	}
	if table.blankNode, err = pattern("BlankNode"); err != nil {
		return nil, err
	}
	for target, name := range map[*map[string]bool]string{
		&table.relationshipType: "prop_Relationship_relationshipType",
		&table.completeness:     "prop_Relationship_completeness",
		&table.primaryPurpose:   "prop_software_SoftwareArtifact_software_primaryPurpose",
		&table.fileKind:         "prop_software_File_software_fileKind",
		&table.hashAlgorithm:    "prop_Hash_algorithm",
	} {
		if *target, err = enum(name); err != nil {
			return nil, err
		}
	}
	shacl, err := definition("SHACLClass")
	if err != nil {
		return nil, err
	}
	types, _ := shacl["properties"].(map[string]any)["type"].(map[string]any)
	branches, _ := types["anyOf"].([]any)
	table.classes = map[string]bool{}
	for _, branch := range branches {
		values, _ := branch.(map[string]any)["enum"].([]any)
		for _, value := range values {
			if text, isString := value.(string); isString {
				table.classes[text] = true
			}
		}
	}
	element, err := definition("Element_derived")
	if err != nil {
		return nil, err
	}
	table.individuals = map[string]bool{}
	elementBranches, _ := element["anyOf"].([]any)
	for _, branch := range elementBranches {
		if value, isString := branch.(map[string]any)["const"].(string); isString {
			table.individuals[value] = true
		}
	}
	if !table.individuals["NoneElement"] {
		return nil, fmt.Errorf("the SPDX schema does not admit NoneElement as an element")
	}
	table.elidable = []elidableArray{
		{"extension", conformingCdxExtension},
		{"element", table.isElement},
		{"rootElement", table.isElement},
	}
	for _, class := range []string{"software_File", "Relationship", "Hash"} {
		if !table.classes[class] {
			return nil, fmt.Errorf("the SPDX schema does not list %s in SHACLClass", class)
		}
	}
	return table, nil
}

// conforms reports whether the schema accepts node because it has one of the
// shapes above. False says nothing: the node is then checked by the schema.
func (t *shapeTable) conforms(node any) bool {
	object, isObject := node.(map[string]any)
	if !isObject {
		return false
	}
	switch object["type"] {
	case "software_File":
		return t.file(object)
	case "Relationship":
		return t.relationship(object)
	}
	return false
}

func (t *shapeTable) file(object map[string]any) bool {
	for key, value := range object {
		if !t.fileKeys[key] {
			return false
		}
		switch key {
		case "spdxId":
			if !t.isIRI(value) {
				return false
			}
		case "creationInfo":
			if !t.isReference(value) {
				return false
			}
		case "name", "software_copyrightText":
			if _, isString := value.(string); !isString {
				return false
			}
		case "software_fileKind":
			if !inEnum(t.fileKind, value) {
				return false
			}
		case "software_primaryPurpose":
			if !inEnum(t.primaryPurpose, value) {
				return false
			}
		case "verifiedUsing":
			items, isArray := value.([]any)
			if !isArray {
				return false
			}
			for _, item := range items {
				if !t.isReference(item) && !t.hash(item) {
					return false
				}
			}
		case "extension":
			if !conformingExtensions(value) {
				return false
			}
		}
	}
	_, hasID := object["spdxId"]
	_, hasCreationInfo := object["creationInfo"]
	return hasID && hasCreationInfo
}

func (t *shapeTable) relationship(object map[string]any) bool {
	for key, value := range object {
		if !t.relationshipKeys[key] {
			return false
		}
		switch key {
		case "spdxId":
			if !t.isIRI(value) {
				return false
			}
		case "creationInfo":
			if !t.isReference(value) {
				return false
			}
		case "from":
			if !t.isElement(value) {
				return false
			}
		case "to":
			items, isArray := value.([]any)
			if !isArray || len(items) == 0 {
				return false
			}
			for _, item := range items {
				if !t.isElement(item) {
					return false
				}
			}
		case "relationshipType":
			if !inEnum(t.relationshipType, value) {
				return false
			}
		case "completeness":
			if !inEnum(t.completeness, value) {
				return false
			}
		case "extension":
			if !conformingExtensions(value) {
				return false
			}
		}
	}
	for _, required := range []string{"spdxId", "creationInfo", "from", "relationshipType", "to"} {
		if _, present := object[required]; !present {
			return false
		}
	}
	return true
}

// elide returns the node to hand to the schema when it has none of the shapes:
// the node itself, or a shallow copy whose arrays leave out the items the
// schema can only accept. Those are the entries of "extension" that
// conformingCdxExtension vouches for, and the references in "element" and
// "rootElement" -- a SpdxDocument and its Sbom list every element of the
// document, which on a large document is hundreds of thousands of strings.
//
// Leaving them out cannot change the verdict. Each of the three properties is
// defined once in the schema (Element_props, ElementCollection_props), as an
// array with no bound on its length whose items are judged on their own; the
// key itself stays, so unevaluatedProperties sees the same keys; and an item
// left out is one the items schema accepts.
func (t *shapeTable) elide(node any) any {
	object, isObject := node.(map[string]any)
	if !isObject {
		return node
	}
	var copied map[string]any
	for _, array := range t.elidable {
		items, isArray := object[array.key].([]any)
		if !isArray {
			continue
		}
		kept := make([]any, 0)
		for _, item := range items {
			if !array.accepted(item) {
				kept = append(kept, item)
			}
		}
		if len(kept) == len(items) {
			continue
		}
		if copied == nil {
			copied = make(map[string]any, len(object))
			for name, value := range object {
				copied[name] = value
			}
		}
		copied[array.key] = kept
	}
	if copied == nil {
		return node
	}
	return copied
}

// conformingCdxExtension reports whether an extension entry has exactly the
// shape of the CycloneDX property extension and nothing else:
//
//	{"type": "extension_CdxPropertiesExtension",
//	 "extension_cdxProperty": [{"type": "extension_CdxPropertyEntry",
//	   "extension_cdxPropName": "<string>", "extension_cdxPropValue": "<string>"}, ...]}
//
// with at least one property entry, each with a name and at most a value.
// Every such entry passes the schema's extension_Extension_derived through its
// first branch: both types are listed in SHACLClass, the array meets minItems,
// every entry meets extension_CdxPropertyEntry_props, and no entry has a key
// its unevaluatedProperties would refuse. The answer is only ever "the schema
// accepts this"; false says nothing, and the entry is then left to the schema.
func conformingCdxExtension(extension any) bool {
	object, isObject := extension.(map[string]any)
	if !isObject || len(object) != 2 || object["type"] != "extension_CdxPropertiesExtension" {
		return false
	}
	entries, isArray := object["extension_cdxProperty"].([]any)
	if !isArray || len(entries) == 0 {
		return false
	}
	for _, entry := range entries {
		fields, isObject := entry.(map[string]any)
		if !isObject || fields["type"] != "extension_CdxPropertyEntry" {
			return false
		}
		if _, named := fields["extension_cdxPropName"].(string); !named {
			return false
		}
		expected := 2
		if value, hasValue := fields["extension_cdxPropValue"]; hasValue {
			if _, isString := value.(string); !isString {
				return false
			}
			expected = 3
		}
		if len(fields) != expected {
			return false
		}
	}
	return true
}

// conformingExtensions is an extension array every entry of which
// conformingCdxExtension vouches for.
func conformingExtensions(value any) bool {
	items, isArray := value.([]any)
	if !isArray {
		return false
	}
	for _, item := range items {
		if !conformingCdxExtension(item) {
			return false
		}
	}
	return true
}

// hash is a Hash object with nothing but the keys Hash_derived evaluates.
func (t *shapeTable) hash(item any) bool {
	object, isObject := item.(map[string]any)
	if !isObject || len(object) != 3 || object["type"] != "Hash" || !inEnum(t.hashAlgorithm, object["algorithm"]) {
		return false
	}
	_, isString := object["hashValue"].(string)
	return isString
}

func (t *shapeTable) isIRI(value any) bool {
	text, isString := value.(string)
	return isString && t.iri.MatchString(text)
}

// isReference is BlankNodeOrIRI.
func (t *shapeTable) isReference(value any) bool {
	text, isString := value.(string)
	return isString && (t.iri.MatchString(text) || t.blankNode.MatchString(text))
}

// isElement is the string branches of Element_derived.
func (t *shapeTable) isElement(value any) bool {
	text, isString := value.(string)
	return isString && (t.individuals[text] || t.isReference(text))
}

func inEnum(set map[string]bool, value any) bool {
	text, isString := value.(string)
	return isString && set[text]
}
