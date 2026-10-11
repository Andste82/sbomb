package spdx23

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// ComponentFiles answers explain --component --sbom for a 2.3 document: the
// file identities the package of this name contains.
//
// The candidates are exactly the elements the CycloneDX and the 3.0.1
// document of the same run list as components, so that the same question
// gets the same answer in every format: every package but the described root
// and the packages that stand for patches or files, and the used files -- a
// file element or a package that stands for a file, of sbomb's file: scheme,
// that depends on nothing (DEPENDS_ON NONE, the statement CycloneDX makes
// with an empty dependsOn). The lookup is the one every SPDX reader shares
// (mapping.FindComponent). The files of the match are the targets of its
// CONTAINS relationships of the file: scheme, by local identity: the
// canonical path behind that prefix.
func ComponentFiles(data []byte, name string) ([]string, error) {
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("the document cannot be read as SPDX 2.3: %w", err)
	}
	roots := map[string]bool{}
	for _, described := range doc.DocumentDescribes {
		roots[described] = true
	}
	leaves := map[string]bool{}
	for _, rel := range doc.Relationships {
		switch {
		case rel.SPDXElementID == documentID && rel.RelationshipType == "DESCRIBES":
			roots[rel.RelatedSPDXElement] = true
		case rel.RelationshipType == "DEPENDS_ON" && rel.RelatedSPDXElement == none:
			leaves[rel.SPDXElementID] = true
		}
	}
	localOf := func(id string) string {
		if local, ok := LocalIdentity(id); ok {
			return local
		}
		return id
	}
	isFile := map[string]bool{}
	var ids []string
	var candidates []mapping.Candidate
	for _, p := range doc.Packages {
		local := localOf(p.SPDXID)
		standsForFile := p.FilesAnalyzed != nil && !*p.FilesAnalyzed && (strings.HasPrefix(local, "file:") || strings.HasPrefix(local, "patch:"))
		switch {
		case roots[p.SPDXID]:
		case standsForFile:
			isFile[p.SPDXID] = true
			if strings.HasPrefix(local, "file:") && leaves[p.SPDXID] {
				ids = append(ids, p.SPDXID)
				candidates = append(candidates, mapping.Candidate{LocalID: local, Name: mapping.BaseName(p.Name)})
			}
		default:
			ids = append(ids, p.SPDXID)
			candidates = append(candidates, mapping.Candidate{LocalID: local, Name: p.Name})
		}
	}
	for _, f := range doc.Files {
		isFile[f.SPDXID] = true
		local := localOf(f.SPDXID)
		if strings.HasPrefix(local, "file:") && leaves[f.SPDXID] {
			ids = append(ids, f.SPDXID)
			candidates = append(candidates, mapping.Candidate{LocalID: local, Name: mapping.BaseName(f.FileName)})
		}
	}
	match, found := mapping.FindComponent(candidates, name)
	if !found {
		return nil, sbomwriter.ErrNoSuchComponent
	}
	id := ids[match]
	files := make([]string, 0)
	for _, rel := range doc.Relationships {
		if rel.RelationshipType != "CONTAINS" || rel.SPDXElementID != id || !isFile[rel.RelatedSPDXElement] {
			continue
		}
		if identity, found := strings.CutPrefix(localOf(rel.RelatedSPDXElement), "file:"); found {
			files = append(files, identity)
		}
	}
	return files, nil
}
