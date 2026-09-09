// Package pkgmanager reads what a package manager recorded about the
// dependencies it installed (specification section 21). It supplies component
// names, versions, purls, supplier and licence hints, the root directories of
// each package, and -- where a manager keeps such a record -- the files it
// installed for it.
//
// It holds two kinds of reader. An adapter enumerates packages and looks for
// them where a manager is known to keep them; an enricher (see enricher.go) is
// handed a directory that already stands as a component root and only describes
// what lies in it.
//
// One rule governs both: a reader here may never add a file to the used set,
// and an enricher may not move a component boundary either. Discovery stays
// evidence-based -- a dependency that was installed but never linked is not
// part of the product -- so this only improves what is known about files the
// evidence chain already reached.
package pkgmanager

import (
	"context"
	"sort"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
)

// Package is one dependency a manager installed.
type Package struct {
	// Name and Roots identify the component; the roots are the directories its
	// files live in. The first one is the identity root -- the anchor, the
	// licence file and any git question all refer to it -- and the others are
	// further trees the same manager filled for this package, such as the
	// build tree FetchContent generates beside the checkout.
	Name  string
	Roots []string

	// Files are the paths the manager itself records as belonging to the
	// package. That is a statement the manager wrote down, not a conclusion
	// drawn from a layout, so it outranks matching a path against a root --
	// vcpkg merges every package into one triplet tree, where no root can tell
	// them apart. It never widens the used set: a file listed here is mapped
	// only when the evidence chain reached it anyway.
	Files []string

	// Version, Supplier, License and PURL are claims rather than bare values:
	// each carries the origin it came from and how strongly that origin counts
	// (section 21.1). Several files can describe one package, and without the
	// origin there is no way to say which of them the document published.
	// Write them only through Take, never directly.
	Version Claim
	// Supplier is filled only when the manager states it. Section 20.5 forbids
	// inferring it from a repository host.
	Supplier Claim
	// License is the manager's declared expression, which section 22.2 ranks
	// above a licence file found in the component root.
	License Claim
	PURL    Claim

	// Superseded holds every claim that lost, with the field it was about. It
	// is filled here and read nowhere in this release; reporting a disagreement
	// between two origins is the next step, and dropping the losers now would
	// leave nothing to report.
	Superseded []Contribution

	// LicenseFile is a licence file the manager itself placed in the package,
	// which is stronger evidence than one found by walking a directory.
	LicenseFile string

	// VCSURL, Commit and Dirty describe the checkout, when the manager used
	// one. The URL is normalized and stripped of credentials (section 19.4).
	VCSURL string
	Commit string
	Dirty  bool

	// Manager names the adapter, for sbomb:component:detectedBy.
	Manager string
	// AnchorKey is the anchor this package should be registered under.
	AnchorKey string
}

// Root is the identity root, or the empty string for a package that names
// none. Nearly every caller wants that one root, and saying so here keeps them
// from spelling out which element of Roots carries the identity.
func (p Package) Root() string {
	if len(p.Roots) == 0 {
		return ""
	}
	return p.Roots[0]
}

// Options is what every adapter needs to look around.
type Options struct {
	// BuildDir is where the build tree is being read from.
	BuildDir string
	// SourceDir is the project source root, as the evidence names it.
	SourceDir string
	// Runner runs introspection commands. It refuses everything when
	// introspection is off, which is the default, so an adapter must work
	// without it and only improve with it.
	Runner *exec.Runner
	// Context bounds the introspection calls.
	Context context.Context
	// Distro is what the image manifests of an embedded-Linux distribution
	// build state, read once per run (distromanifest.go). A nil value means no
	// manifest was configured and nothing happens; it never enumerates a
	// package, and only describes one an adapter already found.
	Distro *DistroMetadata
}

// Adapter reads one package manager's evidence.
type Adapter interface {
	// Manager is the adapter's name.
	Manager() string
	// Discover returns the packages it can prove, and findings for the
	// evidence it expected but could not read.
	Discover(Options) ([]Package, []domain.Finding)
}

// adapters is the registry, in a fixed order so that two runs agree.
var adapters = []Adapter{
	conan{},
	vcpkg{},
	fetchContent{},
	// espidf comes before submodule, and the position is behaviour rather than
	// taste: the first adapter to claim a root keeps it. A directory the ESP-IDF
	// component manager downloaded and unpacked is better described by that
	// manager than by a bare .gitmodules line naming the same path, while the
	// trees conan, vcpkg and FetchContent fill can never be managed_components.
	espidf{},
	// west comes before submodule for the same reason, and the position is
	// behaviour again: west clones the projects of a Zephyr workspace itself,
	// often as plain checkouts a .gitmodules line somewhere else happens to
	// name too. A directory west put there is described by the manifest that
	// asked for it -- with a name, a revision and a repository -- while
	// .gitmodules states a path and a URL and nothing more.
	west{},
	// meson comes before submodule, and the position is behaviour once more: a
	// Meson subproject is very often a git submodule as well -- the wrap and
	// the .gitmodules entry name the same directory -- and the wrap is the
	// stronger statement. It names the subproject the way Meson addresses it,
	// says which revision was asked for, and says where it came from, while
	// .gitmodules states a path and a URL and nothing more.
	meson{},
	submodule{},
}

// claim records who holds a root, so that turning a second claimant away can
// say whom it lost to. A bare "taken" would leave the report unable to name the
// winner, and a winner nobody names is not a report.
type claim struct {
	manager string
	name    string
}

// Discover runs every adapter and returns the union, sorted by name so the
// result is deterministic. A package claimed by two managers keeps the first,
// which is the order above, and the one turned away is reported rather than
// dropped in silence. Every package that survives that is then offered to the
// enrichers, which describe its root without changing what it covers.
//
// That is a rejection and not a merge, deliberately: the ranking of section
// 21.1 orders the origins one manager knows about, and two managers claiming
// one directory is a different problem -- they disagree about who owns the
// package, not about what its version is. Folding the second one's claims into
// the first would publish metadata for a package the winning manager never
// installed.
func Discover(options Options) ([]Package, []domain.Finding) {
	if options.Context == nil {
		options.Context = context.Background()
	}
	packages := make([]Package, 0)
	findings := make([]domain.Finding, 0)
	claimed := map[string]claim{}
	for _, adapter := range adapters {
		found, adapterFindings := adapter.Discover(options)
		findings = append(findings, adapterFindings...)
		for _, entry := range found {
			if entry.Root() == "" {
				continue
			}
			if holder, taken := claimed[entry.Root()]; taken {
				conflict := domain.Conflict{
					Field:   "package the root belongs to",
					Subject: domain.Subject{Kind: "file", Ref: entry.Root()},
					Sides: []domain.ConflictSide{
						{Source: holder.manager, Value: holder.name},
						{Source: adapter.Manager(), Value: entry.Name},
					},
					Winner: holder.manager,
					Reason: "it was asked first in this tool's fixed adapter order, and merging the two " +
						"would publish metadata for a package the winning manager never installed",
				}
				if finding, ok := conflict.Finding("COMPONENT_MAPPING_CONFLICT", domain.SeverityInfo); ok {
					findings = append(findings, finding)
				}
				continue
			}
			// Every root is marked, not only the identity one, so that a later
			// adapter cannot claim a tree an earlier package already covers as
			// a package of its own.
			for _, root := range entry.Roots {
				claimed[root] = claim{manager: adapter.Manager(), name: entry.Name}
			}
			// Whatever else describes the package lies beside it. Only the
			// identity root is offered: the further roots are build trees --
			// the one FetchContent has CMake fill beside the checkout -- and a
			// manifest read there would describe generated output rather than
			// the package, which is the reason resolveRoot reads a licence and
			// a version from the identity root alone.
			findings = append(findings, applyEnrichment(&entry, ComponentRoot{Path: entry.Root(), Name: entry.Name})...)
			// What a distribution image manifest says about a package of this
			// name, folded in last for the reason enrichment is folded in
			// after the adapter: an image manifest and the manager that
			// installed the package rank alike (section 21.1), and where
			// neither origin outranks the other the manager that put the
			// package there is the one to believe. Nothing but claims arrives
			// here -- no root, no file, no anchor key.
			for _, contribution := range options.Distro.Describe(entry.Name) {
				entry.Take(contribution.Field, contribution.Claim)
			}
			packages = append(packages, entry)
		}
	}
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Name != packages[j].Name {
			return packages[i].Name < packages[j].Name
		}
		return packages[i].Root() < packages[j].Root()
	})
	return packages, findings
}
