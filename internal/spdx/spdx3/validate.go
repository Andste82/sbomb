package spdx3

import (
	"fmt"
	"slices"
	"strings"

	"github.com/example/sbomb/internal/spdx/mapping"
)

// Validate is what `sbomb validate` holds any SPDX 3.0.1 document to: the
// official schema, then the conformance rules the specification states and a
// schema cannot express (section 28.11.9, tier a).
//
// Only rules the specification states are here. A document somebody else
// wrote need not look like one sbomb writes: it may refer to elements defined
// in another document -- every official example does -- inline its creation
// information, or name a licence the list this build carries does not know
// yet. Those are conformant, and holding them to sbomb's own habits would make
// validate reject correct documents. sbomb's own output is held to more
// (CheckOwnOutput).
func Validate(data []byte) error {
	parsed, err := parseDocument(data)
	if err != nil {
		return err
	}
	// Before the schema, because the schema only says the value is wrong: an
	// @context array is how 3.0.1 documents declare prefixes, and the reason
	// it is refused deserves a sentence.
	switch context := parsed.context.(type) {
	case string:
		if context != contextURL {
			return fmt.Errorf("@context is %q; an SPDX 3.0.1 document names %q", context, contextURL)
		}
	case []any:
		return fmt.Errorf("@context is an array; the SPDX 3.0.1 schema requires @context to be the plain context URL %q, so prefixes declared beside it make the document invalid", contextURL)
	case nil:
		return fmt.Errorf("the document has no @context; an SPDX 3.0.1 document names %q", contextURL)
	default:
		return fmt.Errorf("@context is not a string; an SPDX 3.0.1 document names %q", contextURL)
	}
	if err := ValidateAgainstSchema(data); err != nil {
		return err
	}
	return conformance(parsed).err("SPDX 3.0.1 conformance check failed")
}

// referenceKinds is what a reference must resolve to when it resolves inside
// the document. A reference to an IRI the document does not define is not an
// error here: 3.0.1 lets a document refer to elements defined elsewhere.
var referenceKinds = map[string]string{
	"from":         "Element",
	"to":           "Element",
	"element":      "Element",
	"rootElement":  "Element",
	"createdBy":    "Agent",
	"suppliedBy":   "Agent",
	"originatedBy": "Agent",
	"createdUsing": "Tool",
}

func conformance(parsed *parsedDocument) *issues {
	found := &issues{}
	seen := map[string]bool{}
	documents := 0

	for _, node := range parsed.nodes {
		class := nodeType(node)
		if class == "" {
			found.addf("%s has no type", describe(node))
			continue
		}
		if id := nodeID(node); id != "" {
			if seen[id] {
				found.addf("%s is defined twice", id)
			}
			seen[id] = true
		}
		if isElementClass(class) {
			id := stringValue(node, "spdxId")
			switch {
			case id == "":
				found.addf("the %s %s has no spdxId", class, describe(node))
			case strings.HasPrefix(id, "_:"):
				found.addf("%s is a blank node; an Element needs an IRI", id)
			}
			checkCreationInfo(parsed, node, found)
			checkName(class, node, found)
		}
		if class == "Relationship" || isA(class, "Relationship") {
			checkTargets(node, found)
			checkVex(parsed, class, node, found)
		}
		if class == "SpdxDocument" {
			documents++
		}
		walkObjects(node, func(object map[string]any) {
			// References are checked wherever they stand, not only on the
			// top-level node: an inline CreationInfo names its creators and
			// tools as well, and they are held to the same kinds.
			checkReferences(parsed, object, found)
			checkValues(object, found)
		})
	}
	checkCreationInfoNodes(parsed, found)
	// Core/SpdxDocument: "Any instance of serialization of SPDX data MUST NOT
	// contain more than one SpdxDocument element definition." None is
	// allowed: a serialization need not carry one at all.
	if documents > 1 {
		found.addf("the document defines %d SpdxDocument elements; a serialization of SPDX data must not contain more than one", documents)
	}
	// That every profile a document uses is listed in its profileConformance
	// is deliberately not a rule here. Neither the specification nor the SHACL
	// model states it: profileConformance names the profiles the creator
	// "intends to conform to", a claim and not an inventory, and the
	// conformance clause makes only the Core profile mandatory. A document
	// that points at expandedlicensing_NoAssertionLicense and claims core and
	// simpleLicensing is valid SPDX. sbomb's own documents are held to the
	// stricter promise -- the claim is exactly what is used -- in
	// CheckOwnOutput.
	return found
}

// checkTargets holds a relationship's to to the two rules the Relationship
// class states for it. It is not empty, which the schema and the SHACL model
// check as well. And NoneElement stands alone: "To explicitly assert that no
// such relationships exist, the `to` property should contain the NoneElement
// individual and no other elements. A relationship that contains NoneElement
// and additional elements in the `to` property is not valid." Neither the
// schema nor the SHACL model can say "alone", so this does.
//
// NoAssertionElement beside other elements is not refused here: the
// specification states no such rule for it, and "these, and possibly others
// nobody asserts" is a reading a document may intend. sbomb never writes it
// that way, which CheckOwnOutput holds.
func checkTargets(node map[string]any, found *issues) {
	to, _ := stringList(node, "to")
	if len(to) == 0 {
		found.addf("relationship %s has no target", describe(node))
		return
	}
	if len(to) > 1 && slices.Contains(to, noneElement) {
		found.addf("relationship %s names NoneElement beside other elements in to; NoneElement asserts that there are none and must stand alone", describe(node))
	}
}

// vexRelationshipTypes is the relationship type each VEX assessment class is
// restricted to, and the one class each of those types is restricted to. The
// class pages of the 3.0.1 Security profile say both directions: the class
// is "restricted to the doesNotAffect relationship type", and the type page
// says "the use of the doesNotAffect is constrained to
// VexNotAffectedVulnAssessmentRelationship classed relationships".
var vexRelationshipTypes = map[string]string{
	"security_VexAffectedVulnAssessmentRelationship":           "affects",
	"security_VexFixedVulnAssessmentRelationship":              "fixedIn",
	"security_VexNotAffectedVulnAssessmentRelationship":        "doesNotAffect",
	"security_VexUnderInvestigationVulnAssessmentRelationship": "underInvestigationFor",
}

// checkVex holds a VEX statement to the rules its class pages state and the
// schema cannot express:
//
//   - its relationship type is the one its class is restricted to, and that
//     type is used by no other class;
//   - "The from: end of the relationship must be a /Security/Vulnerability
//     classed element" -- checked when the document defines what from names,
//     as every other reference is;
//   - a not-affected statement says why: of justificationType and
//     impactStatement, "to produce a valid VEX not_affected statement, one of
//     them MUST be defined". The schema makes both optional, since either
//     will do.
func checkVex(parsed *parsedDocument, class string, node map[string]any, found *issues) {
	relationshipType := stringValue(node, "relationshipType")
	vexType := ""
	for vexClass, candidate := range vexRelationshipTypes {
		inClass := isA(class, vexClass)
		if inClass {
			vexType = candidate
		}
		if relationshipType == candidate && !inClass {
			found.addf("%s uses %s, which only a %s may use", describe(node), candidate, vexClass)
		}
	}
	if vexType == "" {
		return
	}
	if relationshipType != vexType {
		found.addf("%s is a %s with relationship type %q; the class is restricted to %s", describe(node), class, relationshipType, vexType)
	}
	from := stringValue(node, "from")
	if target, resolves := parsed.byID[from]; resolves && !isA(nodeType(target), "security_Vulnerability") {
		found.addf("%s is a VEX statement from %s, which is %s and not a security_Vulnerability", describe(node), from, withArticle(nodeType(target)))
	} else if predefinedIndividuals[from] {
		found.addf("%s is a VEX statement from %s, which is not a security_Vulnerability", describe(node), from)
	}
	if isA(class, "security_VexNotAffectedVulnAssessmentRelationship") &&
		node["security_justificationType"] == nil && node["security_impactStatement"] == nil {
		found.addf("%s is a VEX not-affected statement with neither security_justificationType nor security_impactStatement; one of them must be defined", describe(node))
	}
}

// usedProfiles is the set of profiles a document uses: the profile of every
// class, of every property name and of every predefined individual named in
// from or to, wherever they stand in the graph.
func usedProfiles(parsed *parsedDocument) map[string]bool {
	used := map[string]bool{}
	for _, node := range parsed.nodes {
		walkObjects(node, func(object map[string]any) {
			if objectClass := nodeType(object); objectClass != "" {
				used[profileOfName(objectClass)] = true
			}
			for key := range object {
				used[profileOfName(key)] = true
			}
			for _, key := range []string{"from", "to"} {
				values, _ := stringList(object, key)
				for _, value := range values {
					if predefinedIndividuals[value] {
						used[profileOfName(value)] = true
					}
				}
			}
		})
	}
	return used
}

// namedClasses are the classes whose pages in the 3.0.1 model raise name
// from Core/Element to "minCount 1" under "External properties cardinality
// updates". The SHACL model does not encode that update, so neither the
// schema nor a SHACL validator holds a document to it; this does. A subclass
// -- a dataset or an AI package -- inherits it.
var namedClasses = []string{"software_Package", "software_File"}

// checkName holds a package or a file to having a name. The rule is the
// cardinality alone: name is an xsd:string with no pattern, so a name of white
// space is a name here. That sbomb never writes one is its own habit, held by
// CheckOwnOutput.
func checkName(class string, node map[string]any, found *issues) {
	for _, named := range namedClasses {
		if isA(class, named) && stringValue(node, "name") == "" {
			found.addf("%s has no name; SPDX 3.0.1 requires one of every %s", describe(node), named)
			return
		}
	}
}

// checkCreationInfo holds an element's creationInfo to what it may be: a
// CreationInfo object inline, or a reference to a CreationInfo node of the
// document.
func checkCreationInfo(parsed *parsedDocument, node map[string]any, found *issues) {
	switch value := node["creationInfo"].(type) {
	case string:
		target, resolves := parsed.byID[value]
		if !resolves {
			found.addf("%s refers to creation information %s, which the document does not contain", describe(node), value)
		} else if nodeType(target) != "CreationInfo" {
			found.addf("%s refers to %s as its creation information, which is a %s", describe(node), value, nodeType(target))
		}
	case map[string]any:
		if nodeType(value) != "CreationInfo" {
			found.addf("%s has creation information of type %q", describe(node), nodeType(value))
		}
		checkSpecVersion(value, found)
	case nil:
		found.addf("%s has no creationInfo", describe(node))
	default:
		found.addf("%s has a creationInfo that is neither a reference nor an object", describe(node))
	}
}

func checkCreationInfoNodes(parsed *parsedDocument, found *issues) {
	for _, node := range parsed.nodes {
		if nodeType(node) == "CreationInfo" {
			checkSpecVersion(node, found)
		}
	}
}

func checkSpecVersion(creationInfo map[string]any, found *issues) {
	if version := stringValue(creationInfo, "specVersion"); version != specVersion {
		found.addf("creation information states specVersion %q; this is an SPDX %s check", version, specVersion)
	}
}

// checkReferences holds every reference of a node to two rules: it is an IRI
// and not an inlined element (jsonld.md: "Inlining/Embedding of Element nodes
// into other nodes is not allowed"), and when the document defines what it
// names, that is of the kind the property requires.
func checkReferences(parsed *parsedDocument, node map[string]any, found *issues) {
	for _, key := range sortedKeys(referenceKinds) {
		kind := referenceKinds[key]
		values, allStrings := stringList(node, key)
		if !allStrings {
			found.addf("%s inlines an object in %s; an element is referred to by its IRI", describe(node), key)
		}
		for _, value := range values {
			if predefinedIndividuals[value] {
				if kind != "Element" && !(kind == "Agent" && value == spdxOrganization) {
					found.addf("%s names %s in %s, which is not %s", describe(node), value, key, withArticle(kind))
				}
				continue
			}
			target, resolves := parsed.byID[value]
			if !resolves {
				continue
			}
			if class := nodeType(target); !isA(class, kind) {
				found.addf("%s names %s in %s, which is %s and not %s", describe(node), value, key, withArticle(class), withArticle(kind))
			}
		}
	}
}

// checkValues holds the values a schema can only check as strings: a licence
// expression must parse, and a digest of a fixed-length algorithm must have
// that length in hex. That a package URL starts with pkg: is held only against
// sbomb's own output: the official example package1.json carries
// "https://some.purl" there, and validate must not refuse what the
// specification's own examples do.
func checkValues(object map[string]any, found *issues) {
	if expression, ok := object["simplelicensing_licenseExpression"].(string); ok {
		if _, err := mapping.ParseExpression(expression); err != nil {
			found.addf("%s: %v", describe(object), err)
		}
	}
	// A PackageVerificationCode states its digest the way a Hash does, with
	// the same algorithm vocabulary, so the same lengths hold for it.
	if class := nodeType(object); class == "Hash" || class == "PackageVerificationCode" {
		algorithm := stringValue(object, "algorithm")
		value := stringValue(object, "hashValue")
		if want, fixed := hashLengths[algorithm]; fixed {
			if len(value) != want || !isHex(value) {
				found.addf("a %s digest must be %d hex digits, not %q", algorithm, want, value)
			}
		}
	}
}

// withArticle puts the indefinite article before a class name, so that a
// message reads "a Tool" and "an Agent".
func withArticle(class string) string {
	if class == "" {
		return "a node without a type"
	}
	if strings.ContainsRune("AEIOU", rune(class[0])) {
		return "an " + class
	}
	return "a " + class
}

func isHex(value string) bool {
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return value != ""
}
