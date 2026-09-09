package pkgmanager

import "github.com/example/sbomb/internal/domain"

// This file holds the second kind of reader in this package. An adapter
// enumerates packages and looks for them in the places a manager is known to
// use; an enricher is handed a directory that already stands as a component
// root and only describes what lies directly in it. The distinction matters
// because the two are allowed different things: searching is how a package is
// found at all, while an enricher must not search -- everything it could find
// that way would be a directory nothing has proven to be a component.

// ComponentRoot is a directory that has already been settled as where a
// component begins -- either the identity root of a discovered package or the
// root section 19.2 resolved for a component. An enricher reads the files
// directly in it and nothing else: it does not descend, does not walk up, and
// does not look at a sibling.
//
// It is deliberately not called Target, because that word already names a
// build-system target throughout this tool (cmakeapi.Target, make.Target) and
// a second meaning would make both unreadable.
type ComponentRoot struct {
	// Path is the directory on disk. It is where the bytes are, so it is
	// absolute and local to this run.
	Path string
	// Name is the component name already resolved for this root, as a hint for
	// a file that describes more than one package -- a bundled SBOM listing a
	// whole dependency tree, say. It may be empty, and an enricher has to work
	// when it is: a reader that answers only when it is told the name would
	// stay silent at exactly the roots that carry no metadata today.
	Name string
}

// Enricher reads metadata out of a directory that is already known to be a
// component root.
//
// The return type is the whole contract: contributions and findings, no paths
// and no root. An enricher therefore cannot add a file to the used set and
// cannot move a component boundary, however much a later reader might want to
// -- there is nowhere in this signature to say either. That is the rule of this
// package, kept structurally rather than by discipline.
type Enricher interface {
	// Source is the enricher's name, as the document would name the origin.
	// A claim that names no origin of its own is published under this, so that
	// no value can reach the document without something to attribute it to.
	Source() string
	// Enrich returns what the directory states, each value carrying the rank
	// of the file it was read from (section 21.1), and findings for evidence
	// it expected but could not read. A directory holding no file it
	// recognizes is the ordinary case and yields nothing at all -- neither a
	// claim nor a finding, because there is nothing missing about a component
	// that simply has no manifest.
	Enrich(ComponentRoot) ([]Contribution, []domain.Finding)
}

// enrichers is the registry, in a fixed order so that two runs agree. The
// order is behaviour and not presentation: Take keeps the first of two equally
// ranked claims, so moving a line here changes which value a document
// publishes.
//
// bundledSBOM is first because it is the strongest declared origin there is
// (rank 4 of section 21.1): a document the upstream shipped and its own
// tooling checked, rather than our reading of a manifest format. A manifest
// reader added later belongs after it.
var enrichers = []Enricher{
	bundledSBOM{},
}

// applyEnrichment lets every enricher describe a root and folds what they say
// into a package. It runs during discovery, while the package is still being
// built, because Take may only be called then.
//
// Enrichment runs after the owning manager has made its own claims, so an
// equally ranked enricher loses to the manager that installed the package.
// That is the intended order: both describe the same package, and the one that
// put it there is the one to believe where neither origin outranks the other.
func applyEnrichment(pkg *Package, root ComponentRoot) []domain.Finding {
	// A root nobody settled is no root, and a registry with nothing in it
	// touches no directory at all. Both are cheap guards in front of the only
	// file reads this file causes.
	if len(enrichers) == 0 || pkg == nil || root.Path == "" {
		return nil
	}
	findings := make([]domain.Finding, 0)
	for _, enricher := range enrichers {
		contributions, enricherFindings := enricher.Enrich(root)
		findings = append(findings, enricherFindings...)
		for _, contribution := range contributions {
			claim := contribution.Claim
			if claim.Source == "" {
				claim.Source = enricher.Source()
			}
			pkg.Take(contribution.Field, claim)
		}
	}
	return findings
}

// Enrich describes a component root no package manager owns, which is the case
// section 19.2 strategy 6 leaves behind: a library copied into the tree, found
// only by the licence file beside it, named after its directory and otherwise
// blank. A manifest or a bundled SBOM lying next to that licence file holds the
// version, and this is how it gets read.
//
// The package returned is a carrier for the four claims and nothing else. It
// has no name, no roots, no files and no manager, and it must never be put into
// a package list: such a list records what a manager installed, and nothing was
// installed here. It is built fresh rather than copied from a discovered
// package because Take may only be called on a package still being built.
func Enrich(root ComponentRoot) (Package, []domain.Finding) {
	var described Package
	findings := applyEnrichment(&described, root)
	return described, findings
}
