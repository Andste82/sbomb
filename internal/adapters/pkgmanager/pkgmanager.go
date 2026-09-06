// Package pkgmanager reads what a package manager recorded about the
// dependencies it installed (specification section 21). It supplies component
// names, versions, purls, supplier and licence hints, and the root directory of
// each package.
//
// One rule governs the whole package: an adapter here may never add a file to
// the used set. Discovery stays evidence-based -- a dependency that was
// installed but never linked is not part of the product -- so this only
// improves what is known about files the evidence chain already reached.
package pkgmanager

import (
	"context"
	"sort"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
)

// Package is one dependency a manager installed.
type Package struct {
	// Name and Root identify the component; Root is where its files live.
	Name string
	Root string

	Version           string
	VersionSource     string
	VersionConfidence domain.Confidence

	// Supplier is filled only when the manager states it. Section 20.5 forbids
	// inferring it from a repository host.
	Supplier string
	// License is the manager's declared expression, which section 22.2 ranks
	// above a licence file found in the component root.
	License string
	PURL    string

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
	fetchContent{},
}

// Discover runs every adapter and returns the union, sorted by name so the
// result is deterministic. A package claimed by two managers keeps the first,
// which is the order above.
func Discover(options Options) ([]Package, []domain.Finding) {
	if options.Context == nil {
		options.Context = context.Background()
	}
	packages := make([]Package, 0)
	findings := make([]domain.Finding, 0)
	claimed := map[string]bool{}
	for _, adapter := range adapters {
		found, adapterFindings := adapter.Discover(options)
		findings = append(findings, adapterFindings...)
		for _, entry := range found {
			if entry.Root == "" || claimed[entry.Root] {
				continue
			}
			claimed[entry.Root] = true
			packages = append(packages, entry)
		}
	}
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Name != packages[j].Name {
			return packages[i].Name < packages[j].Name
		}
		return packages[i].Root < packages[j].Root
	})
	return packages, findings
}
