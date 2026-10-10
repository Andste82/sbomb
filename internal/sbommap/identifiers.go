// Package sbommap holds the derivations every writer shares: the local
// identity of each component and file (section 28.4), the sbomb:* property
// set of each of them (appendix B), whether a component's licence is a
// curated conclusion, and the document identity under reproducible. A rule
// that only one format's vocabulary has -- CycloneDX's split of a licence
// file into "declared" and "concluded", for one -- stays with that writer.
//
// They live below the writers because they must be the same in every format.
// An SPDX element and the CycloneDX component of the same run describe one
// thing, and a consumer holding both matches them by identity: if the SPDX
// writer derived "component:zlib@1.3" where the CycloneDX writer derived
// "component:zlib", two documents of one build would disagree about what is
// in it. A property a reviewer looks up in appendix B has to carry the same
// value whichever document it was read from, for the same reason. Nothing in
// here knows a format's spelling -- no bom-ref field, no IRI, no element type.
package sbommap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/sbomwriter"
)

// Table is the local identity of every component and file of one document,
// in the scheme of section 28.4. CycloneDX writes them as bom-refs, SPDX as the
// fragment of an element's IRI; the string is the same.
type Table struct {
	product       string
	byComponentID map[string]string
	byFileID      map[string]string
	nameCounts    map[string]int
	// taken is every component identity handed out so far, so that a
	// decoration which still collides is noticed and escalated rather than
	// emitted twice.
	taken map[string]bool
}

// Identifiers builds the whole table of section 28.4 in one fixed order: the
// product, the artifacts, the components in the order the document lists
// them, then the files. The order is part of the scheme rather than an
// implementation detail -- the first component of a name keeps the
// undecorated identity, so walking the components in any other order would
// rename them -- which is why the table is built once, here, and never
// assembled piecemeal by a writer.
//
// Two artifacts or two files with the same identity are a defect upstream of
// every writer, and are reported rather than emitted: section 28.4 makes them
// impossible by construction, and an implementation must still assert it
// rather than write a document whose references are ambiguous.
func Identifiers(document *sbomwriter.Document) (*Table, error) {
	table := newTable()
	table.product = table.forProduct(document.Product)
	seen := map[string]bool{table.product: true}
	claim := func(id string) error {
		if seen[id] {
			return fmt.Errorf("identity collision on %q", id)
		}
		seen[id] = true
		return nil
	}
	for _, artifact := range document.Artifacts {
		if err := claim(table.forArtifact(artifact)); err != nil {
			return nil, err
		}
	}
	for _, component := range document.Components {
		if err := claim(table.forComponent(component)); err != nil {
			return nil, err
		}
	}
	for _, file := range document.Files {
		if err := claim(table.forFile(file)); err != nil {
			return nil, err
		}
	}
	return table, nil
}

func newTable() *Table {
	return &Table{
		byComponentID: map[string]string{},
		byFileID:      map[string]string{},
		nameCounts:    map[string]int{},
		taken:         map[string]bool{},
	}
}

// Product is the identity of the document's root.
func (t *Table) Product() string { return t.product }

// Component is the identity of the product, an artifact or a component, by
// its document identity; the empty string when the document has none such.
func (t *Table) Component(id string) string { return t.byComponentID[id] }

// File is the identity of a file by its canonical path.
func (t *Table) File(canonical string) string { return t.byFileID[canonical] }

// Resolve maps a component or file identity, as a Relation names it, onto its
// local identity. A component identity wins over a file identity, which is the
// order every writer has always resolved them in.
func (t *Table) Resolve(id string) (string, bool) {
	if ref, ok := t.byComponentID[id]; ok {
		return ref, true
	}
	ref, ok := t.byFileID[id]
	return ref, ok
}

func (t *Table) forProduct(product domain.Component) string {
	ref := "product:" + pathmodel.Slug(product.Name, 64)
	t.byComponentID[product.ID] = ref
	return ref
}

func (t *Table) forArtifact(artifact domain.Component) string {
	ref := "artifact:" + artifact.ID
	t.byComponentID[artifact.ID] = ref
	return ref
}

func (t *Table) forComponent(component domain.Component) string {
	base := "component:" + pathmodel.Slug(component.Name, 64)
	ref := base
	// Disambiguate by version, then by a digest of the component root, exactly
	// as section 28.4 prescribes.
	if t.nameCounts[base] > 0 {
		if component.Version != "" {
			ref += "@" + pathmodel.Slug(component.Version, 32)
		} else if component.Root != nil {
			ref += "#" + ShortDigest(component.Root.Canonical())
		} else {
			ref += "#" + ShortDigest(component.ID)
		}
	}
	// Two components of the same name and the same version decorate to the
	// same identity, and an identity must be unique in the document. Section
	// 28.4 escalates in a fixed order -- the root digest, the identity digest,
	// then a counter in walk order -- so the outcome depends only on the
	// document, and the first component of a name keeps the undecorated
	// identity it always had.
	if t.taken[ref] && component.Root != nil {
		ref = base + "#" + ShortDigest(component.Root.Canonical())
	}
	if t.taken[ref] {
		ref = base + "#" + ShortDigest(component.ID)
	}
	if t.taken[ref] {
		stem := ref
		for n := 2; t.taken[ref]; n++ {
			ref = fmt.Sprintf("%s~%d", stem, n)
		}
	}
	t.nameCounts[base]++
	t.taken[ref] = true
	t.byComponentID[component.ID] = ref
	return ref
}

func (t *Table) forFile(file domain.UsedFile) string {
	canonical := file.ID.Canonical()
	ref := FileIdentity(canonical)
	t.byFileID[canonical] = ref
	return ref
}

// FileIdentity is the identity of a file by its canonical path. It is exported
// because a writer also names files the document does not list as used -- a
// retained licence file is one -- and those must be named the same way, so
// that a licence file that is also a used file is one thing and not two.
func FileIdentity(canonical string) string { return "file:" + canonical }

// ShortDigest is the twelve-character content digest section 28.4 uses to
// disambiguate two components that share a name and have no version.
func ShortDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}
