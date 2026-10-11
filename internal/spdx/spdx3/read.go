package spdx3

import (
	"strings"

	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// ComponentFiles answers explain --component --sbom for a 3.0.1 document: the
// file identities the package of this name contains.
//
// A name is looked up among exactly the elements the CycloneDX document of the
// same run lists as components, so that the same question about the same run
// gets the same answer in either format: the artifacts and the grouping and
// toolchain components, which are packages here, and the used files. The
// product is not among them -- CycloneDX states it as metadata.component, not
// as a component -- and nor is a file that is only evidence or a patch, which
// CycloneDX carries as evidence and pedigree rather than as a component. A used
// file is recognised by the statement both formats make about it alone: that
// it depends on nothing (dependsOn NoneElement here, an empty dependsOn there).
//
// The lookup is the CycloneDX reader's: by local identity first, as the review
// report prints it, then by name, as a person types it -- a file by its base
// name, which is its CycloneDX component name. The files of the match are the
// targets of its contains relationships that are files of sbomb's file:
// scheme; a file's local identity is its canonical path behind that prefix
// (section 28.4), the key the evidence graph uses, so the answer needs no
// translation.
func ComponentFiles(data []byte, name string) ([]string, error) {
	parsed, err := parseDocument(data)
	if err != nil {
		return nil, err
	}
	roots := map[string]bool{}
	leaves := map[string]bool{}
	for _, node := range parsed.nodes {
		if nodeType(node) == "software_Sbom" {
			values, _ := stringList(node, "rootElement")
			for _, value := range values {
				roots[value] = true
			}
		}
		if stringValue(node, "relationshipType") == "dependsOn" {
			if targets, _ := stringList(node, "to"); len(targets) == 1 && targets[0] == noneElement {
				leaves[stringValue(node, "from")] = true
			}
		}
	}
	var ids []string
	var candidates []mapping.Candidate
	for _, node := range parsed.nodes {
		id := nodeID(node)
		switch nodeType(node) {
		case "software_Package":
			if !roots[id] {
				ids = append(ids, id)
				candidates = append(candidates, mapping.Candidate{LocalID: localOf(id), Name: stringValue(node, "name")})
			}
		case "software_File":
			if strings.HasPrefix(localOf(id), "file:") && leaves[id] {
				ids = append(ids, id)
				candidates = append(candidates, mapping.Candidate{LocalID: localOf(id), Name: mapping.BaseName(stringValue(node, "name"))})
			}
		}
	}
	match, found := mapping.FindComponent(candidates, name)
	if !found {
		return nil, sbomwriter.ErrNoSuchComponent
	}
	id := ids[match]

	files := make([]string, 0)
	for _, node := range parsed.nodes {
		if stringValue(node, "relationshipType") != "contains" || stringValue(node, "from") != id {
			continue
		}
		targets, _ := stringList(node, "to")
		for _, target := range targets {
			if nodeType(parsed.byID[target]) != "software_File" {
				continue
			}
			if identity, found := strings.CutPrefix(localOf(target), "file:"); found {
				files = append(files, identity)
			}
		}
	}
	return files, nil
}
