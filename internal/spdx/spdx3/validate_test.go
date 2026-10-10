package spdx3

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The conformance checks (tier a) judge any SPDX 3.0.1 document. Each test
// below breaks a rendering in one place and expects Validate to name what was
// broken -- or, for the cases the specification allows, to accept it.

func small(t *testing.T) []byte {
	t.Helper()
	return render(t, smallDocument(), smallOptions())
}

func TestADuplicateSpdxIdIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		copied := map[string]any{}
		for key, value := range findNode(graph, "component:zlib") {
			copied[key] = value
		}
		return append(graph, copied)
	})
	assertRefused(t, Validate(data), iriOf("component:zlib")+" is defined twice")
}

func TestAnUnresolvedBlankCreationInfoIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["creationInfo"] = "_:creationInfo9"
		return graph
	})
	assertRefused(t, Validate(data), "refers to creation information _:creationInfo9, which the document does not contain")
}

func TestAnInlineCreationInfoIsConformant(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["creationInfo"] = map[string]any{
			"type": "CreationInfo", "specVersion": "3.0.1", "created": "2023-11-14T22:13:20Z",
			"createdBy": []any{iriOf("agent:Acme")},
		}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("an inline creation information is conformant: %v", err)
	}
	assertRefused(t, CheckOwnOutput(data), "does not refer to _:creationInfo1")
}

func TestAnEmptyToIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "relationship:1")["to"] = []any{}
		return graph
	})
	// The schema refuses it first, by its minItems.
	assertRefused(t, Validate(data), "/to")
	if found := conformance(mustParse(t, data)); found.err("x") == nil {
		t.Error("the conformance layer does not refuse an empty to")
	}
}

// TestAProfileUsedButNotClaimedIsConformantButNotSbombOutput: profileConformance
// is a claim of intent, not an inventory -- neither the specification nor the
// SHACL model requires every profile a document uses to be listed -- so
// validate accepts such a document. sbomb's own output promises the claim is
// exactly what is used.
func TestAProfileUsedButNotClaimedIsConformantButNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "document")["profileConformance"] = []any{"core", "software"}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("an unclaimed profile is conformant: %v", err)
	}
	assertRefused(t, CheckOwnOutput(data), "uses the simpleLicensing profile but")
}

// TestAThirdPartyDocumentThatClaimsOnlyCoreIsConformant: a document another
// tool wrote that uses software, security and expandedLicensing terms and
// claims only the Core profile -- the one profile the conformance clause makes
// mandatory -- passes validate. It is shaped the way the official examples are:
// its creator is an agent defined in another document.
func TestAThirdPartyDocumentThatClaimsOnlyCoreIsConformant(t *testing.T) {
	data := []byte(`{
  "@context": "https://spdx.org/rdf/3.0.1/spdx-context.jsonld",
  "@graph": [
    {"type": "CreationInfo", "@id": "_:creationinfo", "createdBy": ["http://spdx.example.com/Agent/Elsewhere"],
     "specVersion": "3.0.1", "created": "2024-03-06T00:00:00Z"},
    {"type": "SpdxDocument", "spdxId": "http://spdx.example.com/Document1", "creationInfo": "_:creationinfo",
     "profileConformance": ["core"], "rootElement": ["http://spdx.example.com/Package1"],
     "element": ["http://spdx.example.com/Package1", "http://spdx.example.com/Vulnerability1",
                 "http://spdx.example.com/Relationship1", "http://spdx.example.com/Relationship2"]},
    {"type": "software_Package", "spdxId": "http://spdx.example.com/Package1", "creationInfo": "_:creationinfo",
     "name": "package1", "software_packageVersion": "1.0"},
    {"type": "security_Vulnerability", "spdxId": "http://spdx.example.com/Vulnerability1", "creationInfo": "_:creationinfo",
     "name": "CVE-2024-0001"},
    {"type": "Relationship", "spdxId": "http://spdx.example.com/Relationship1", "creationInfo": "_:creationinfo",
     "from": "http://spdx.example.com/Package1", "relationshipType": "hasConcludedLicense",
     "to": ["https://spdx.org/rdf/3.0.1/terms/ExpandedLicensing/NoAssertionLicense"]},
    {"type": "Relationship", "spdxId": "http://spdx.example.com/Relationship2", "creationInfo": "_:creationinfo",
     "from": "http://spdx.example.com/Package1", "relationshipType": "hasDeclaredLicense",
     "to": ["expandedlicensing_NoAssertionLicense"]}
  ]
}`)
	if err := Validate(data); err != nil {
		t.Fatalf("a document claiming only core is conformant whatever profiles it uses: %v", err)
	}
}

func TestCreatedByMustNameAnAgentWhenItResolves(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		info := graph[0].(map[string]any)
		info["createdBy"] = []any{iriOf("tool:sbomb")}
		return graph
	})
	assertRefused(t, Validate(data), "which is a Tool and not an Agent")
}

func TestTheSpdxOrganizationIndividualIsAnAgent(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		graph[0].(map[string]any)["createdBy"] = []any{"SpdxOrganization"}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("SpdxOrganization is an Agent: %v", err)
	}
}

func TestAnInlinedElementIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		relationship := findNode(graph, "relationship:1")
		relationship["to"] = []any{map[string]any{
			"type": "software_File", "spdxId": iriOf("file:inline"), "creationInfo": "_:creationInfo1",
		}}
		return graph
	})
	assertRefused(t, conformance(mustParse(t, data)).err("x"), "inlines an object in to")
}

func TestAContextArrayIsRefusedWithAReason(t *testing.T) {
	data := mutate(t, small(t), func(root map[string]any, graph []any) []any {
		root["@context"] = []any{contextURL, map[string]any{"sbomb": "urn:uuid:x#"}}
		return graph
	})
	assertRefused(t, Validate(data), "requires @context to be the plain context URL")
}

func TestAnotherSpecVersionIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		graph[0].(map[string]any)["specVersion"] = "3.0.0"
		return graph
	})
	assertRefused(t, Validate(data), `states specVersion "3.0.0"`)
}

func TestAMalformedLicenceExpressionIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = "MIT And Zlib"
		return graph
	})
	assertRefused(t, Validate(data), `licence expression "MIT And Zlib"`)
}

// TestLowerCaseOperatorsAreConformantButNotSbombOutput: SPDX 3.0.1 annex B
// allows an operator in all upper or all lower case, so a document somebody
// else wrote with "MIT or Apache-2.0" passes validate. sbomb writes upper case
// and holds its own output to it.
func TestLowerCaseOperatorsAreConformantButNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = "MIT or Apache-2.0"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("lower-case operators are conformant: %v", err)
	}
	assertRefused(t, CheckOwnOutput(data), `writes the operators of "MIT or Apache-2.0" in lower case`)
}

// TestAPackageOrFileWithoutANameIsRefused: the 3.0.1 pages of File and
// Package raise name to minCount 1 ("External properties cardinality
// updates"), which neither the schema nor the SHACL model encodes.
func TestAPackageOrFileWithoutANameIsRefused(t *testing.T) {
	for _, local := range []string{"component:zlib", "file:build:_deps/kitchen-src/kitchen.c"} {
		start := small(t)
		if local != "component:zlib" {
			start = renderAllFields(t)
		}
		for _, name := range []any{nil, ""} {
			data := mutate(t, start, func(_ map[string]any, graph []any) []any {
				if name == nil {
					delete(findNode(graph, local), "name")
				} else {
					findNode(graph, local)["name"] = name
				}
				return graph
			})
			assertRefused(t, Validate(data), iriOf(local)+" has no name; SPDX 3.0.1 requires one of every")
		}
	}
}

// A name of white space is a name: name is an xsd:string with no pattern,
// and the cardinality update asks only that there be one. Validate accepts
// it; sbomb never writes one, which CheckOwnOutput holds.
func TestAPackageOrFileNamedByWhiteSpaceIsConformantButNotSbombOutput(t *testing.T) {
	for _, local := range []string{"component:zlib", "file:build:_deps/kitchen-src/kitchen.c"} {
		start := small(t)
		if local != "component:zlib" {
			start = renderAllFields(t)
		}
		data := mutate(t, start, func(_ map[string]any, graph []any) []any {
			findNode(graph, local)["name"] = " "
			return graph
		})
		if err := Validate(data); err != nil {
			t.Errorf("%s: a name of white space is refused: %v", local, err)
		}
		assertRefused(t, CheckOwnOutput(data), iriOf(local)+" has a name of white space only")
	}
}

func TestAnUnlistedLicenceIdIsConformant(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = "Some-Future-Licence-9.0"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("a licence the list of this build does not know yet is conformant: %v", err)
	}
	assertRefused(t, CheckOwnOutput(data), "names Some-Future-Licence-9.0, which is neither on the SPDX licence list")
}

func TestAHashOfTheWrongLengthIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		file := findNode(graph, "file:project:lib/zlib.c")
		file["verifiedUsing"].([]any)[0].(map[string]any)["hashValue"] = "abc"
		return graph
	})
	assertRefused(t, Validate(data), "a sha256 digest must be 64 hex digits")
}

func TestAnUnknownHashAlgorithmLengthIsNotChecked(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		hash := findNode(graph, "file:project:lib/zlib.c")["verifiedUsing"].([]any)[0].(map[string]any)
		hash["algorithm"] = "md6"
		hash["hashValue"] = "abc"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("md6 has no fixed length: %v", err)
	}
}

func TestAReferenceToAnElementDefinedElsewhereIsConformant(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		relationship := findNode(graph, "relationship:1")
		relationship["to"] = append(relationship["to"].([]any), "https://elsewhere.example/doc#pkg")
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("SPDX 3.0.1 lets a document refer to elements defined elsewhere: %v", err)
	}
	assertRefused(t, CheckOwnOutput(data), "https://elsewhere.example/doc#pkg in to, which the document does not define")
}

// The checks of sbomb's own output (tier b). Each case is conformant SPDX and
// still not a document sbomb writes.

func TestADanglingRelationshipTargetIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "relationship:1")["to"] = []any{iriOf("component:gone")}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "component:gone in to, which the document does not define")
}

func TestAnElementUnreachableFromTheRootIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		orphan := map[string]any{"type": "software_Package", "spdxId": iriOf("component:orphan"), "creationInfo": creationInfoID, "name": "orphan"}
		graph = append(graph, orphan)
		document := findNode(graph, "document")
		sbom := findNode(graph, "sbom")
		document["element"] = append(document["element"].([]any), iriOf("component:orphan"))
		sbom["element"] = append(sbom["element"].([]any), iriOf("component:orphan"))
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "component:orphan cannot be reached from the document's root")
}

func TestAnElementMissingFromTheDocumentListIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		document := findNode(graph, "document")
		elements := document["element"].([]any)
		document["element"] = elements[:len(elements)-1]
		return graph
	})
	assertRefused(t, CheckOwnOutput(data), "the SpdxDocument must list every other element exactly once")
}

func TestALicenceRelationshipToAPackageIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		for _, item := range graph {
			node := item.(map[string]any)
			if node["relationshipType"] == "hasConcludedLicense" {
				node["to"] = []any{iriOf("product:app")}
			}
		}
		return graph
	})
	assertRefused(t, CheckOwnOutput(data), "which is not a licence expression")
}

func TestAnUnlistedLicenceIdIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = "Zlib AND Acme-Proprietary-1.0"
		return graph
	})
	assertRefused(t, CheckOwnOutput(data), "names Acme-Proprietary-1.0, which is neither on the SPDX licence list")
}

func TestAnUndefinedSbombLicenseRefIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = "LicenseRef-sbomb-nowhere"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "uses LicenseRef-sbomb-nowhere, which no licence text of the document defines")
}

// The AdditionRef twin of the rule above: an exception reference sbomb minted
// is one the document must define, or the renderer dropped the
// SimpleLicensingText (or its customIdToUri entry) for an unlisted exception
// and the expression names something no reader can resolve. A foreign
// AdditionRef is not held to it (TestAnAdditionRefIsSbombOutput).
func TestAnUndefinedSbombAdditionRefIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = "Zlib WITH AdditionRef-sbomb-nowhere"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "uses AdditionRef-sbomb-nowhere, which no licence text of the document defines")
}

// The list's spelling of an identifier is the one sbomb writes (deviation
// D55): a lower-case "zlib" is the listed Zlib to a reader, who matches
// without case, but sbomb's own output promises the canonical form.
func TestAListedIdentifierInAnotherCaseIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = "zlib"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), `writes "zlib", which the SPDX licence list spells "Zlib"`)
}

// The same holds for an exception: "classpath-exception-2.0" is the listed
// Classpath-exception-2.0 to a reader, but not the spelling sbomb writes.
func TestAListedExceptionInAnotherCaseIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = "Zlib WITH classpath-exception-2.0"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), `writes "classpath-exception-2.0", which the SPDX licence list spells "Classpath-exception-2.0"`)
}

func TestAPropertyOutsideAppendixBIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		zlib := findNode(graph, "component:zlib")
		entries := zlib["extension"].([]any)[0].(map[string]any)["extension_cdxProperty"].([]any)
		entries[0].(map[string]any)["extension_cdxPropName"] = "finding"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), `property name "finding" must start with sbomb:`)
}

func TestAProfileClaimBeyondWhatIsUsedIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "document")["profileConformance"] = []any{"build", "core", "simpleLicensing", "software"}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "profileConformance is")
}

func TestAGapInTheRelationshipNumbersIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		old := iriOf("relationship:1")
		renamed := iriOf("relationship:99")
		findNode(graph, "relationship:1")["spdxId"] = renamed
		for _, key := range []string{"document", "sbom"} {
			elements := findNode(graph, key)["element"].([]any)
			for index, element := range elements {
				if element == old {
					elements[index] = renamed
				}
			}
		}
		return graph
	})
	assertRefused(t, CheckOwnOutput(data), "is numbered out of order; the next relationship is relationship:1")
}

func TestAPackageURLThatIsNoneIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["software_packageUrl"] = "https://zlib.net"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), `has a package URL "https://zlib.net"`)
}

// The schema is applied node by node (schemaSet says why); these hold that
// path to what the published schema refuses.

func TestAKeyTheClassDoesNotDefineIsRefusedWhereItIs(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["software_fileKind"] = "file"
		return graph
	})
	err := ValidateAgainstSchema(data)
	assertRefused(t, err, "/@graph/")
	assertRefused(t, err, "software_fileKind")
}

// One wrong value is one line. The branch that would have evaluated the
// node's other keys fails with it, so unevaluatedProperties reports every
// correct key of the node as a "false schema" as well; those consequences are
// left out, so that the cause of every broken node stays under the cap.
func TestOneWrongValueIsOneLine(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["software_packageVersion"] = 1
		return graph
	})
	err := ValidateAgainstSchema(data)
	if err == nil {
		t.Fatal("a number as the package version passed the schema")
	}
	lines := strings.Split(err.Error(), "\n")[1:]
	if len(lines) != 1 || !strings.Contains(lines[0], "software_packageVersion: got number, want string") {
		t.Errorf("ValidateAgainstSchema = %v; want the one wrong value alone", err)
	}
}

// With more broken nodes than the cap would hold at eight lines each, every
// node's cause is still reported.
func TestTheCauseOfEveryBrokenNodeIsReported(t *testing.T) {
	ids := []string{"component:kitchen", "file:build:_deps/kitchen-src/kitchen.c", "product:sentinel-product"}
	data := mutate(t, renderAllFields(t), func(_ map[string]any, graph []any) []any {
		for _, id := range ids {
			findNode(graph, id)["name"] = 1
		}
		return graph
	})
	err := ValidateAgainstSchema(data)
	if err == nil {
		t.Fatal("numbers as names passed the schema")
	}
	if got := strings.Count(err.Error(), "/name: got number, want string"); got != len(ids) || strings.Contains(err.Error(), "false schema") {
		t.Errorf("ValidateAgainstSchema = %v; want each of the %d wrong names and nothing else", err, len(ids))
	}
}

func TestANodeOfNoClassIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		return append(graph, map[string]any{"type": "software_Widget", "spdxId": iriOf("widget"), "creationInfo": creationInfoID})
	})
	if err := ValidateAgainstSchema(data); err == nil {
		t.Fatal("a node of a class the model does not have passed the schema")
	}
}

func TestAKeyBesideTheGraphIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(root map[string]any, graph []any) []any {
		root["comment"] = "not allowed here"
		return graph
	})
	assertRefused(t, ValidateAgainstSchema(data), "comment")
}

func TestASingleNodeDocumentIsCheckedAsAWhole(t *testing.T) {
	document := []byte(`{"@context": "https://spdx.org/rdf/3.0.1/spdx-context.jsonld", "type": "software_File",
		"spdxId": "https://example.com/f", "creationInfo": {"type": "CreationInfo", "specVersion": "3.0.1",
		"created": "2024-01-01T00:00:00Z", "createdBy": ["https://example.com/me"]}, "name": "f"}`)
	if err := Validate(document); err != nil {
		t.Fatalf("a one-node document is conformant: %v", err)
	}
	broken := []byte(`{"@context": "https://spdx.org/rdf/3.0.1/spdx-context.jsonld", "type": "software_File", "name": "f"}`)
	if err := ValidateAgainstSchema(broken); err == nil {
		t.Fatal("an element without spdxId and creationInfo passed the schema")
	}
}

// TestTheNodePathAgreesWithTheWholeSchema: for documents that pass and for
// documents that break the schema in several ways, checking node by node
// gives the verdict the published schema gives on the whole document.
func TestTheNodePathAgreesWithTheWholeSchema(t *testing.T) {
	set, err := compiled()
	if err != nil {
		t.Fatal(err)
	}
	base := renderAllFields(t)
	cases := map[string][]byte{
		"valid": base,
		"unknown key": mutate(t, base, func(_ map[string]any, graph []any) []any {
			findNode(graph, "component:kitchen")["software_fileKind"] = "file"
			return graph
		}),
		"enum": mutate(t, base, func(_ map[string]any, graph []any) []any {
			findNode(graph, "relationship:1")["relationshipType"] = "HAS_STATIC_LINK"
			return graph
		}),
		"scalar sbomType": mutate(t, base, func(_ map[string]any, graph []any) []any {
			findNode(graph, "sbom")["software_sbomType"] = "build"
			return graph
		}),
		"nested": mutate(t, base, func(_ map[string]any, graph []any) []any {
			hash := findNode(graph, "file:build:_deps/kitchen-src/kitchen.c")["verifiedUsing"].([]any)[0].(map[string]any)
			hash["algorithm"] = "SHA256"
			return graph
		}),
	}
	for name, data := range cases {
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		whole := set.document.Validate(instance) == nil
		nodes := ValidateAgainstSchema(data) == nil
		if whole != nodes || whole != (name == "valid") {
			t.Errorf("%s: the whole schema says %v, the node path %v", name, whole, nodes)
		}
	}
}

// Tier a, rule by rule (section 28.11.9). The cases above are the ones the
// design named first; the ones below complete the list, so that every rule
// Validate holds a document to has a document that breaks it and, where the
// specification allows a variant, a document that uses the variant and passes.

// Each wrong @context is refused with a sentence of its own, before the
// schema, whose const error would only say the value is wrong. The test holds
// each case to its own wording, so that a case left to the schema fails it.
func TestAContextOfAnotherDocumentIsRefused(t *testing.T) {
	for name, want := range map[string]string{
		"another URL": `@context is "https://spdx.org/rdf/3.0.0/spdx-context.jsonld"; an SPDX 3.0.1 document names`,
		"no context":  "the document has no @context; an SPDX 3.0.1 document names",
		"an object":   "@context is not a string; an SPDX 3.0.1 document names",
	} {
		context := map[string]any{
			"another URL": "https://spdx.org/rdf/3.0.0/spdx-context.jsonld",
			"no context":  nil,
			"an object":   map[string]any{"spdx": "https://spdx.org/rdf/3.0.1/terms/"},
		}[name]
		data := mutate(t, small(t), func(root map[string]any, graph []any) []any {
			if context == nil {
				delete(root, "@context")
			} else {
				root["@context"] = context
			}
			return graph
		})
		if err := Validate(data); err == nil || !strings.HasPrefix(err.Error(), want) || !strings.Contains(err.Error(), contextURL) {
			t.Errorf("%s: Validate = %v; want %q naming %s", name, err, want, contextURL)
		}
	}
}

func TestANodeWithoutATypeIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		delete(findNode(graph, "component:zlib"), "type")
		return graph
	})
	// The schema refuses it as well; the conformance layer must say which
	// node it is, whatever the schema reports.
	if err := ValidateAgainstSchema(data); err == nil {
		t.Error("the schema accepted a node without a type")
	}
	assertRefused(t, conformance(mustParse(t, data)).err("x"), iriOf("component:zlib")+" has no type")
}

func TestAnElementWithoutAnSpdxIdIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		delete(findNode(graph, "component:zlib"), "spdxId")
		return graph
	})
	assertRefused(t, Validate(data), "/@graph/")
	assertRefused(t, conformance(mustParse(t, data)).err("x"), "the software_Package a software_Package node has no spdxId")
}

func TestAnElementNamedByABlankNodeIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["spdxId"] = "_:zlib"
		return graph
	})
	if err := ValidateAgainstSchema(data); err == nil {
		t.Error("the schema accepted an Element named by a blank node")
	}
	assertRefused(t, conformance(mustParse(t, data)).err("x"), "_:zlib is a blank node; an Element needs an IRI")
}

func TestADuplicateBlankNodeIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		copied := map[string]any{}
		for key, value := range graph[0].(map[string]any) {
			copied[key] = value
		}
		return append(graph, copied)
	})
	assertRefused(t, Validate(data), creationInfoID+" is defined twice")
}

func TestAnElementWithoutCreationInfoIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		delete(findNode(graph, "component:zlib"), "creationInfo")
		return graph
	})
	if err := ValidateAgainstSchema(data); err == nil {
		t.Error("the schema accepted an Element without creationInfo")
	}
	assertRefused(t, conformance(mustParse(t, data)).err("x"), iriOf("component:zlib")+" has no creationInfo")
}

func TestACreationInfoReferenceToAnotherKindOfNodeIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["creationInfo"] = iriOf("agent:Acme")
		return graph
	})
	assertRefused(t, Validate(data), "as its creation information, which is a Organization")
}

func TestAnInlineCreationInfoOfAnotherVersionIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["creationInfo"] = map[string]any{
			"type": "CreationInfo", "specVersion": "3.0.0", "created": "2023-11-14T22:13:20Z",
			"createdBy": []any{iriOf("agent:Acme")},
		}
		return graph
	})
	assertRefused(t, Validate(data), `states specVersion "3.0.0"`)
}

// TestAnInlineCreationInfoIsHeldToTheSameKinds: an inline CreationInfo names
// its creators as a node of its own does, and a creator that resolves to a
// Tool is as wrong there.
func TestAnInlineCreationInfoIsHeldToTheSameKinds(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["creationInfo"] = map[string]any{
			"type": "CreationInfo", "specVersion": "3.0.1", "created": "2023-11-14T22:13:20Z",
			"createdBy": []any{iriOf("tool:sbomb")},
		}
		return graph
	})
	assertRefused(t, Validate(data), "names "+iriOf("tool:sbomb")+" in createdBy, which is a Tool and not an Agent")
}

func TestCreatedUsingMustNameAToolWhenItResolves(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		graph[0].(map[string]any)["createdUsing"] = []any{iriOf("agent:Acme")}
		return graph
	})
	assertRefused(t, Validate(data), "in createdUsing, which is an Organization and not a Tool")
}

func TestSuppliedByMustNameAnAgentWhenItResolves(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["suppliedBy"] = iriOf("tool:sbomb")
		return graph
	})
	assertRefused(t, Validate(data), "in suppliedBy, which is a Tool and not an Agent")
}

func TestOriginatedByMustNameAnAgentWhenItResolves(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["originatedBy"] = []any{iriOf("component:zlib")}
		return graph
	})
	assertRefused(t, Validate(data), "in originatedBy, which is a software_Package and not an Agent")
}

func TestAPredefinedElementOtherThanSpdxOrganizationIsNoAgent(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		graph[0].(map[string]any)["createdBy"] = []any{noneElement}
		return graph
	})
	// The schema refuses it by createdBy's own pattern; the conformance
	// layer says why in the words of the model.
	if err := ValidateAgainstSchema(data); err == nil {
		t.Error("the schema accepted NoneElement as a creator")
	}
	assertRefused(t, conformance(mustParse(t, data)).err("x"), "names NoneElement in createdBy, which is not an Agent")
}

func TestARelationshipMustConnectElements(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "relationship:1")["to"] = []any{creationInfoID}
		return graph
	})
	assertRefused(t, conformance(mustParse(t, data)).err("x"), "names "+creationInfoID+" in to, which is a CreationInfo and not an Element")

	data = mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "relationship:1")["from"] = creationInfoID
		return graph
	})
	assertRefused(t, conformance(mustParse(t, data)).err("x"), "names "+creationInfoID+" in from, which is a CreationInfo and not an Element")
}

func TestThePredefinedElementsAreElements(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "relationship:3")["to"] = []any{noneElement}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("NoneElement is an Element: %v", err)
	}
}

func TestACollectionMustListElements(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		document := findNode(graph, "document")
		document["rootElement"] = []any{creationInfoID}
		return graph
	})
	assertRefused(t, conformance(mustParse(t, data)).err("x"), "in rootElement, which is a CreationInfo and not an Element")

	data = mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		sbom := findNode(graph, "sbom")
		sbom["element"] = append(sbom["element"].([]any), creationInfoID)
		return graph
	})
	assertRefused(t, conformance(mustParse(t, data)).err("x"), "in element, which is a CreationInfo and not an Element")
}

func TestAnInlinedCreatorIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		graph[0].(map[string]any)["createdBy"] = []any{map[string]any{
			"type": "Organization", "spdxId": iriOf("agent:Inline"), "creationInfo": creationInfoID, "name": "Inline",
		}}
		return graph
	})
	assertRefused(t, conformance(mustParse(t, data)).err("x"), "inlines an object in createdBy")
}

func TestAnIllegalEnumValueIsRefusedWhereItIs(t *testing.T) {
	for key, value := range map[string]string{
		"relationshipType": "HAS_STATIC_LINK",
		"completeness":     "partial",
	} {
		data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
			findNode(graph, "relationship:1")[key] = value
			return graph
		})
		err := Validate(data)
		assertRefused(t, err, "/@graph/")
		assertRefused(t, err, key)
	}
}

func TestEveryUsedProfileMustBeClaimedInSbombOutput(t *testing.T) {
	data := mutate(t, renderAllFields(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "document")["profileConformance"] = []any{"core", "expandedLicensing", "simpleLicensing", "software"}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("an unclaimed profile is conformant: %v", err)
	}
	assertRefused(t, CheckOwnOutput(data), "uses the security profile but")

	// expandedLicensing is used through the licence individuals alone, which
	// are values and not keys; a document of sbomb's that names one claims it.
	data = mutate(t, renderAllFields(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "document")["profileConformance"] = []any{"core", "security", "simpleLicensing", "software"}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("an unclaimed profile is conformant: %v", err)
	}
	assertRefused(t, CheckOwnOutput(data), "uses the expandedLicensing profile but")
}

// TestTheExtensionProfileNeedNotBeClaimed: sbomb's properties use the
// extension profile, and the profile cannot be claimed in a form both the
// schema and the model accept (deviation D49b), so leaving it out is not held
// against any document.
func TestTheExtensionProfileNeedNotBeClaimed(t *testing.T) {
	data := renderAllFields(t)
	if !bytes.Contains(data, []byte(`"extension_cdxPropName"`)) {
		t.Fatal("the synthetic document uses no extension")
	}
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
}

func TestADocumentWithoutAnSpdxDocumentClaimsNothing(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		var out []any
		for _, item := range graph {
			if item.(map[string]any)["type"] != "SpdxDocument" {
				out = append(out, item)
			}
		}
		return out
	})
	if err := Validate(data); err != nil {
		t.Fatalf("a graph without an SpdxDocument has no claims to check: %v", err)
	}
}

func TestEveryFormOfTheExpressionGrammarIsConformant(t *testing.T) {
	for _, expression := range []string{
		"MIT",
		"GPL-2.0+",
		"MIT OR Apache-2.0 AND BSD-3-Clause",
		"(MIT OR Apache-2.0) AND Zlib",
		"GPL-2.0-or-later WITH Classpath-exception-2.0",
		"LicenseRef-custom",
		"DocumentRef-other:LicenseRef-custom",
		"Apache-2.0 WITH AdditionRef-custom",
		"MIT or Apache-2.0",
		"(MIT and Zlib) or GPL-2.0-or-later with Classpath-exception-2.0",
	} {
		data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
			findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = expression
			return graph
		})
		if err := Validate(data); err != nil {
			t.Errorf("%q is a licence expression: %v", expression, err)
		}
	}
	for _, expression := range []string{"MIT AND", "(MIT", "MIT)", "MIT WITH", "WITH MIT", "MIT OR OR Zlib", "MIT Zlib", "MIT/Zlib", "MIT Or Zlib", "MIT aNd Zlib", "GPL-2.0 +"} {
		data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
			findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = expression
			return graph
		})
		assertRefused(t, Validate(data), fmt.Sprintf("licence expression %q", expression))
	}
}

// TestEveryFixedLengthDigestIsHeldToItsLength: each algorithm whose digest
// length is fixed accepts exactly that many hex digits, in either case, and
// refuses one more, one fewer, and a non-hex digit.
func TestEveryFixedLengthDigestIsHeldToItsLength(t *testing.T) {
	withHash := func(algorithm, value string) []byte {
		return mutate(t, small(t), func(_ map[string]any, graph []any) []any {
			hash := findNode(graph, "file:project:lib/zlib.c")["verifiedUsing"].([]any)[0].(map[string]any)
			hash["algorithm"] = algorithm
			hash["hashValue"] = value
			return graph
		})
	}
	for _, algorithm := range sortedKeys(hashLengths) {
		length := hashLengths[algorithm]
		if err := Validate(withHash(algorithm, strings.Repeat("a", length))); err != nil {
			t.Errorf("%s: %d hex digits refused: %v", algorithm, length, err)
		}
		if err := Validate(withHash(algorithm, strings.Repeat("F", length))); err != nil {
			t.Errorf("%s: upper-case hex refused: %v", algorithm, err)
		}
		for _, value := range []string{strings.Repeat("a", length+1), strings.Repeat("a", length-1), "g" + strings.Repeat("a", length-1)} {
			if err := Validate(withHash(algorithm, value)); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("a %s digest must be %d hex digits", algorithm, length)) {
				t.Errorf("%s: %q = %v", algorithm, value, err)
			}
		}
	}
	for _, algorithm := range []string{"other", "md6", "adler32", "blake3", "crystalsKyber", "crystalsDilithium", "falcon"} {
		if err := Validate(withHash(algorithm, "abc")); err != nil {
			t.Errorf("%s has no fixed length: %v", algorithm, err)
		}
	}
}

func TestAPackageVerificationCodeIsHeldToItsLength(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["verifiedUsing"] = []any{map[string]any{
			"type": "PackageVerificationCode", "algorithm": "sha1", "hashValue": "abc",
		}}
		return graph
	})
	assertRefused(t, Validate(data), "a sha1 digest must be 40 hex digits")
}

func TestAPackageURLOfAnotherSchemeIsConformant(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "component:zlib")["software_packageUrl"] = "https://some.purl"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("the specification's own example carries such a package URL: %v", err)
	}
}

// TestManyIssuesAreSortedAndCapped: a document is reported with everything
// wrong with it, in a fixed order, and a long list is cut at twenty lines with
// the number left out, as the CycloneDX validator does.
func TestManyIssuesAreSortedAndCapped(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		for index := 0; index < 25; index++ {
			graph = append(graph, map[string]any{
				"type": "Relationship", "spdxId": iriOf(fmt.Sprintf("broken:%02d", index)), "creationInfo": creationInfoID,
				"from": iriOf("component:zlib"), "relationshipType": "contains", "to": []any{creationInfoID},
			})
		}
		return graph
	})
	err := conformance(mustParse(t, data)).err("heading")
	if err == nil {
		t.Fatal("no issues")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 1+maxReportedIssues+1 || lines[len(lines)-1] != "  ... and 5 more" {
		t.Fatalf("the report is not capped at %d:\n%v", maxReportedIssues, err)
	}
	if !sort.StringsAreSorted(lines[1 : 1+maxReportedIssues]) {
		t.Errorf("the issues are not sorted:\n%v", err)
	}
	again := conformance(mustParse(t, data)).err("heading")
	if again.Error() != err.Error() {
		t.Error("two checks of the same document report differently")
	}
}

// Tier b, rule by rule. Each document below is conformant -- Validate accepts
// it -- and is still not one sbomb writes.

func TestASingleNodeDocumentIsNotSbombOutput(t *testing.T) {
	document := []byte(`{"@context": "https://spdx.org/rdf/3.0.1/spdx-context.jsonld", "type": "software_File",
		"spdxId": "https://example.com/f", "creationInfo": {"type": "CreationInfo", "specVersion": "3.0.1",
		"created": "2024-01-01T00:00:00Z", "createdBy": ["https://example.com/me"]}, "name": "f"}`)
	if err := Validate(document); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(document), "the document is not exactly @context and @graph")
}

func TestASecondCreationInfoIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		second := map[string]any{}
		for key, value := range graph[0].(map[string]any) {
			second[key] = value
		}
		second["@id"] = "_:creationInfo2"
		findNode(graph, "component:zlib")["creationInfo"] = "_:creationInfo2"
		return append(graph, second)
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "the document must have exactly one creation information, "+creationInfoID)
}

func TestACreationInfoUnderAnotherNameIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		graph[0].(map[string]any)["@id"] = "_:info"
		for _, item := range graph {
			if node := item.(map[string]any); node["creationInfo"] == creationInfoID {
				node["creationInfo"] = "_:info"
			}
		}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "exactly one creation information, "+creationInfoID)
}

func TestACreationTimeOfAnotherShapeIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		graph[0].(map[string]any)["created"] = "2023-11-14T22:13:20+01:00"
		return graph
	})
	// The schema's DateTime pattern refuses it too; the invariant does not
	// rely on the schema having been run.
	assertRefused(t, CheckOwnOutput(data), `created "2023-11-14T22:13:20+01:00" is not YYYY-MM-DDThh:mm:ssZ`)
}

func TestACreationInfoNamingNobodyIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		delete(graph[0].(map[string]any), "createdBy")
		return graph
	})
	assertRefused(t, CheckOwnOutput(data), "the creation information names nobody in createdBy")
}

func TestACustomIdMappedOutsideTheDocumentIsNotSbombOutput(t *testing.T) {
	const expression = "license:LicenseRef-sbomb-Sentinel-Custom-Licence-2ce69ab6-d9c0bbf6"
	data := mutate(t, renderAllFields(t), func(_ map[string]any, graph []any) []any {
		entry := findNode(graph, expression)["simplelicensing_customIdToUri"].([]any)[0].(map[string]any)
		entry["value"] = "https://elsewhere.example/licence"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	err := CheckOwnOutput(data)
	assertRefused(t, err, "to https://elsewhere.example/licence, which the document does not define")
	assertRefused(t, err, "uses LicenseRef-sbomb-Sentinel-Custom-Licence-2ce69ab6-d9c0bbf6, which no licence text of the document defines")
}

func TestASecondSbomIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		second := map[string]any{}
		for key, value := range findNode(graph, "sbom") {
			second[key] = value
		}
		second["spdxId"] = iriOf("sbom2")
		document := findNode(graph, "document")
		document["element"] = append(document["element"].([]any), iriOf("sbom2"))
		return append(graph, second)
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "exactly one SpdxDocument and one software_Sbom, not 1 and 2")
}

func TestAnSbomThatDoesNotListEveryElementIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		sbom := findNode(graph, "sbom")
		elements := sbom["element"].([]any)
		sbom["element"] = elements[1:]
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "the software_Sbom must list every element but itself and the SpdxDocument")
}

func TestADocumentListedTwiceIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		document := findNode(graph, "document")
		elements := document["element"].([]any)
		document["element"] = append(elements, elements[0])
		return graph
	})
	assertRefused(t, CheckOwnOutput(data), "the SpdxDocument must list every other element exactly once")
}

func TestADocumentRootedElsewhereThanTheSbomIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "document")["rootElement"] = []any{iriOf("product:app")}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "the SpdxDocument's root must be the software_Sbom")
}

func TestAnSbomWithTwoRootsIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		sbom := findNode(graph, "sbom")
		sbom["rootElement"] = append(sbom["rootElement"].([]any), iriOf("component:zlib"))
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "the software_Sbom must have exactly one root, the product")
}

// TestARelationshipAgainstItsDirectionIsNotSbombOutput: closure follows what
// a relationship says in the direction it says it. Turning the product's
// static link around leaves zlib's subtree reachable only backwards.
func TestARelationshipAgainstItsDirectionIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		link := findNode(graph, "relationship:4")
		link["from"], link["to"] = iriOf("component:zlib"), []any{iriOf("product:app")}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), iriOf("component:zlib")+" cannot be reached from the document's root")
}

func TestALicenceOfSomethingThatIsNoArtifactIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "relationship:2")["from"] = iriOf("agent:Acme")
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "states a licence of something that is not a package or a file")
}

func TestALicenceIndividualIsALicenceTarget(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "relationship:2")["to"] = []any{noAssertionLic}
		findNode(graph, "document")["profileConformance"] = []any{"core", "expandedLicensing", "simpleLicensing", "software"}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	// The expression is now unused and unreachable; the licence relationship
	// itself is not what is refused.
	err := CheckOwnOutput(data)
	if err == nil || strings.Contains(err.Error(), "not a licence expression") {
		t.Errorf("CheckOwnOutput = %v; want only the orphaned expression refused", err)
	}
}

// The doesNotAffect type "is constrained to
// VexNotAffectedVulnAssessmentRelationship classed relationships": a plain
// Relationship that uses it is not valid SPDX, and sbomb's own check refuses
// it on its own account as well.
func TestDoesNotAffectOutsideAVexAssessmentIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "relationship:1")["relationshipType"] = "doesNotAffect"
		return graph
	})
	assertRefused(t, Validate(data), iriOf("relationship:1")+" uses doesNotAffect, which only a security_VexNotAffectedVulnAssessmentRelationship may use")
	err := CheckOwnOutput(data)
	assertRefused(t, err, "uses doesNotAffect outside a VEX not-affected assessment")
	assertRefused(t, err, "is a VEX statement that is not from a vulnerability")
}

// mutateVex changes the first VEX not-affected statement of the synthetic
// document and returns its identifier with the result. The statement is found
// by its class rather than by its number, so that a change elsewhere in the
// synthetic document does not move it.
func mutateVex(t *testing.T, change func(vex map[string]any)) (string, []byte) {
	t.Helper()
	var vex string
	data := mutate(t, renderAllFields(t), func(_ map[string]any, graph []any) []any {
		for _, item := range graph {
			if node := item.(map[string]any); node["type"] == "security_VexNotAffectedVulnAssessmentRelationship" {
				vex = node["spdxId"].(string)
				change(node)
				break
			}
		}
		return graph
	})
	if vex == "" {
		t.Fatal("the synthetic document has no VEX statement")
	}
	return vex, data
}

// "The from: end of the relationship must be a /Security/Vulnerability
// classed element": a rule of the class page, so Validate refuses it, and
// sbomb's own check keeps its own wording of it.
func TestAVexStatementFromAPackageIsRefused(t *testing.T) {
	vex, data := mutateVex(t, func(node map[string]any) {
		node["from"] = iriOf("product:sentinel-product")
	})
	assertRefused(t, Validate(data), vex+" is a VEX statement from "+iriOf("product:sentinel-product")+", which is a software_Package and not a security_Vulnerability")
	assertRefused(t, CheckOwnOutput(data), vex+" is a VEX statement that is not from a vulnerability")
}

// A VEX statement from a vulnerability another document defines is
// conformant: the reference is not followed, as no reference is.
func TestAVexStatementFromAVulnerabilityDefinedElsewhereIsConformant(t *testing.T) {
	_, data := mutateVex(t, func(node map[string]any) {
		node["from"] = "https://example.com/other-document#vulnerability"
	})
	if err := Validate(data); err != nil {
		t.Fatalf("a VEX statement from an element defined elsewhere is conformant: %v", err)
	}
}

// The class is "restricted to the doesNotAffect relationship type".
func TestAVexStatementOfAnotherRelationshipTypeIsRefused(t *testing.T) {
	vex, data := mutateVex(t, func(node map[string]any) {
		node["relationshipType"] = "contains"
	})
	assertRefused(t, Validate(data), vex+` is a security_VexNotAffectedVulnAssessmentRelationship with relationship type "contains"; the class is restricted to doesNotAffect`)
}

// Of justificationType and impactStatement, "to produce a valid VEX
// not_affected statement, one of them MUST be defined". Either alone will do.
func TestAVexNotAffectedStatementMustSayWhy(t *testing.T) {
	vex, data := mutateVex(t, func(node map[string]any) {
		delete(node, "security_justificationType")
		delete(node, "security_impactStatement")
	})
	assertRefused(t, Validate(data), vex+" is a VEX not-affected statement with neither security_justificationType nor security_impactStatement")
	for _, kept := range []string{"security_justificationType", "security_impactStatement"} {
		_, data := mutateVex(t, func(node map[string]any) {
			node["security_justificationType"] = "componentNotPresent"
			node["security_impactStatement"] = "The vulnerable code is not compiled in."
			for _, key := range []string{"security_justificationType", "security_impactStatement"} {
				if key != kept {
					delete(node, key)
				}
			}
		})
		if err := Validate(data); err != nil {
			t.Errorf("a VEX not-affected statement with only %s is conformant: %v", kept, err)
		}
	}
}

// "Any instance of serialization of SPDX data MUST NOT contain more than one
// SpdxDocument element definition."
func TestASecondSpdxDocumentIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		copied := map[string]any{}
		for key, value := range findNode(graph, "document") {
			copied[key] = value
		}
		copied["spdxId"] = iriOf("document2")
		return append(graph, copied)
	})
	assertRefused(t, Validate(data), "the document defines 2 SpdxDocument elements")
}

// A relationship "that contains NoneElement and additional elements in the
// `to` property is not valid" (Core/Relationship).
func TestNoneElementBesideOtherTargetsIsRefused(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "relationship:1")["to"] = []any{"NoneElement", iriOf("component:zlib")}
		return graph
	})
	assertRefused(t, Validate(data), "relationship "+iriOf("relationship:1")+" names NoneElement beside other elements in to")
	if err := ValidateAgainstSchema(data); err != nil {
		t.Errorf("the schema refuses it on its own, so the rule is not one only Go holds: %v", err)
	}
}

// NoAssertionElement beside other elements breaks no rule the specification
// states, so Validate accepts it; sbomb writes a predefined element only
// alone, which CheckOwnOutput holds.
func TestNoAssertionElementBesideOtherTargetsIsConformantButNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "relationship:1")["to"] = []any{"NoAssertionElement", iriOf("component:zlib")}
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatalf("NoAssertionElement beside other elements is conformant: %v", err)
	}
	assertRefused(t, CheckOwnOutput(data), iriOf("relationship:1")+" names NoAssertionElement beside other elements")
}

func TestAnUnlistedExceptionIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = "Zlib WITH Not-An-Exception"
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "names the exception Not-An-Exception, which is not on the SPDX licence list")
}

func TestAnAdditionRefIsSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "license:Zlib")["simplelicensing_licenseExpression"] = "Zlib WITH AdditionRef-local"
		return graph
	})
	if err := CheckOwnOutput(data); err != nil {
		t.Fatalf("an AdditionRef is not on the list and need not be: %v", err)
	}
}

func TestAListedExpressionWithoutItsListVersionIsNotSbombOutput(t *testing.T) {
	for name, version := range map[string]any{"missing": nil, "another": "3.20.0"} {
		data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
			expression := findNode(graph, "license:Zlib")
			if version == nil {
				delete(expression, "simplelicensing_licenseListVersion")
			} else {
				expression["simplelicensing_licenseListVersion"] = version
			}
			return graph
		})
		if err := Validate(data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		assertRefused(t, CheckOwnOutput(data), "names listed licences but states licence list version")
	}
}

// An exception is as much from the list as a licence is: an expression whose
// only listed part is the exception after WITH still owes the list version.
func TestAListedExceptionWithoutItsListVersionIsNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		expression := findNode(graph, "license:Zlib")
		expression["simplelicensing_licenseExpression"] = "LicenseRef-acme WITH Classpath-exception-2.0"
		delete(expression, "simplelicensing_licenseListVersion")
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), "names listed licences but states licence list version")
}

func TestAProfileClaimShortOfWhatIsUsedIsNotSbombOutput(t *testing.T) {
	data := mutate(t, renderAllFields(t), func(_ map[string]any, graph []any) []any {
		findNode(graph, "document")["profileConformance"] = []any{"core", "expandedLicensing", "simpleLicensing", "software"}
		return graph
	})
	assertRefused(t, CheckOwnOutput(data), "profileConformance is [core expandedLicensing simpleLicensing software]; the document uses exactly [core expandedLicensing security simpleLicensing software]")
}

// TestAPropertyOutsideTheSbombNamespaceIsNotSbombOutput: the property rule is
// the CycloneDX writer's (sbommap.ValidatePropertyName), so that the two
// formats refuse the same leaks: an empty name and a name of another
// namespace, beside the internal key of the test above.
func TestAPropertyOutsideTheSbombNamespaceIsNotSbombOutput(t *testing.T) {
	for name, want := range map[string]string{
		"":            "empty property name",
		"acme:secret": `property name "acme:secret" must start with sbomb:`,
	} {
		data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
			zlib := findNode(graph, "component:zlib")
			entries := zlib["extension"].([]any)[0].(map[string]any)["extension_cdxProperty"].([]any)
			entries[0].(map[string]any)["extension_cdxPropName"] = name
			return graph
		})
		if err := Validate(data); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		assertRefused(t, CheckOwnOutput(data), want)
	}
}

func TestRelationshipsOutOfGraphOrderAreNotSbombOutput(t *testing.T) {
	data := mutate(t, small(t), func(_ map[string]any, graph []any) []any {
		// Swap the first two relationships in the graph without renaming
		// them: the numbers are a permutation, not a gap.
		var at []int
		for index, item := range graph {
			if item.(map[string]any)["type"] == "Relationship" {
				at = append(at, index)
			}
		}
		graph[at[0]], graph[at[1]] = graph[at[1]], graph[at[0]]
		return graph
	})
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, CheckOwnOutput(data), iriOf("relationship:2")+" is numbered out of order; the next relationship is relationship:1")
}

// TestEveryRuleOfTheOwnOutputHoldsForBothRenderings: the two documents the
// tests here mutate pass both tiers as rendered, so that every refusal above
// is caused by the mutation and not by the starting point.
func TestEveryRuleOfTheOwnOutputHoldsForBothRenderings(t *testing.T) {
	for name, data := range map[string][]byte{"small": small(t), "all fields": renderAllFields(t)} {
		if err := Validate(data); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if err := CheckOwnOutput(data); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
