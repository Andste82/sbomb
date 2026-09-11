package generate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/adapters/pkgmanager"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/componentmap"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/version"
)

// packageMetadataFiles are the manifests that mark a directory as the root of
// a distinct software component, used by strategy 6 of section 19.2.
//
// Most of these ecosystems now have an adapter of their own, and none of the
// markers became redundant because of it. An adapter finds a package where that
// manager left an install record behind: a lock file, a file list, a populated
// _deps tree. A library somebody copied into the source tree carries the
// manifest and nothing else -- no manager installed it, so no adapter can claim
// it -- and the marker is then the only thing that gives it a boundary of its
// own. Dropping an entry here because an adapter exists would lose components,
// not sharpen them: the adapter would keep describing what it installs, and the
// copied-in directory would dissolve into whatever encloses it.
//
// The order of the two strategies already settles the overlap. Strategy 2 runs
// first, so where an adapter did claim the directory the manifest here is never
// consulted, and the weaker marker can never overrule the manager.
var packageMetadataFiles = []string{
	"conanfile.txt", "conanfile.py", "conandata.yml",
	"vcpkg.json", "CONTROL",
	"idf_component.yml",
	"Cargo.toml",
	"west.yml",
}

// licenseBoundaryFiles mark a directory as the root of a distinct component
// even when it carries no package manifest. A library that was simply copied
// into the source tree usually has nothing else: no conanfile, no vcpkg.json,
// and a CMakeLists.txt that cannot be read without interpreting CMake.
//
// NOTICE and COPYRIGHT are deliberately absent. They are attribution material,
// not a licence grant, and a directory that carries only a NOTICE is not
// thereby a separate work.
var licenseBoundaryFiles = []string{
	"LICENSE", "LICENSE.txt", "LICENSE.md",
	"LICENCE", "LICENCE.txt", "LICENCE.md",
	"COPYING", "COPYING.txt", "COPYING.md",
}

// bundledSBOMFiles mark a directory as the root of a distinct component in the
// same way a licence file does: a dependency that ships its own SBOM says with
// it that it is a separate piece of software, and it is often the only thing
// such a dependency ships besides its sources.
//
// The names are fixed, unlike the globs the reader in internal/adapters/
// pkgmanager uses on a root that is already settled. This list is consulted
// with os.Stat once per used file per ancestor directory, so a directory
// listing here would be paid tens of thousands of times over against the
// budget of section 31, while a settled root is read once per component.
//
// Like a licence file, and for the same reason, these never mark a boundary at
// the anchor root itself: a project's own SBOM at its own root describes the
// project, and taking it for a marker would rename the project's component
// after its source directory.
var bundledSBOMFiles = []string{
	"sbom.cdx.json", "bom.cdx.json",
	"sbom.spdx.json", "bom.spdx.json",
}

// componentResolver maps used files onto components, following the priority
// order of section 19.2. Only strategies 1, 6, 7 and 8 exist so far; package
// managers, SDK layouts and submodule boundaries are later work (D7).
type componentResolver struct {
	curated     *componentmap.Mapper
	curatedByID map[string]config.Component
	physical    map[string]string
	projectName string
	anchorRoots map[string]string
	logger      *Logger
	// packages are what the package-manager adapters proved, keyed by the
	// canonical identity prefix their roots correspond to. This is strategy 2
	// of section 19.2, which outranks everything except curated configuration.
	// A package with several roots has one entry per root.
	packages []resolvedPackage
	// packageFiles maps a file identity to the package that names that exact
	// file in its own installed-file list. A manager that says "this file is
	// mine" is the same strategy 2 evidence as its root, only precise, so it
	// is consulted before any prefix.
	packageFiles map[string]resolvedPackage
	// targets maps a file identity to the CMake target that owns it, and
	// curatedByTarget maps a target name to the component the configuration
	// assigns it to. Together they are strategy 5.
	targets         map[string]string
	curatedByTarget map[string]config.Component
	// runner and ctx are how a versionFrom rule reaches git. The zero value
	// runs nothing, which is what a run without introspection must do.
	runner *exec.Runner
	ctx    context.Context
	// enrich describes a component root no package manager owns. It is a field
	// rather than a direct call so that a test can put its own reader in
	// without the production code exporting a hook for it -- the same reason
	// setPackages takes its path resolution as packagePaths.
	enrich func(pkgmanager.ComponentRoot) (pkgmanager.Package, []domain.Finding)
	// distro is what the image manifests of an embedded-Linux distribution
	// build state, keyed by package name and read once for the whole run. It
	// describes a component; it never maps a file, so it appears nowhere in
	// resolve and nowhere in r.packages.
	distro *pkgmanager.DistroMetadata
	// pkgConfig reads the pkg-config metadata a system library installed beside
	// itself, which is the strategy between 6 and 7 of section 19.2. It is a
	// field for the reason enrich is one: a test can put its own reader in
	// without the production code exporting a hook for it.
	pkgConfig func(pkgmanager.SystemFile) (pkgmanager.SystemPackage, []domain.Finding)
	// systemPackages is what the reader answered for one used file, keyed by
	// that file's identity. resolve is asked about a file more than once in a
	// run -- once while narrowing is counted, once while the files are grouped
	// -- and re-reading the metadata each time would pay for the same
	// directories twice (section 31).
	systemPackages map[string]systemPackageResult
	// systemPackageFindings is what reading that metadata reported. resolve
	// answers with a component and has nowhere to put a finding, so they are
	// collected here and drained once the files are grouped.
	systemPackageFindings []domain.Finding
	// pkgConfigByComponent is what the .pc files that named a component stated
	// about it, keyed by the component name. It describes a component; like the
	// image manifest above it maps no file, so nothing in it can widen the used
	// set.
	pkgConfigByComponent map[string]*pkgConfigComponent
}

// systemPackageResult is what the pkg-config reader answered about one used
// file: the module it belongs to, or the empty string for the ordinary case of
// a file no .pc file describes, what that answer stated about the module, and
// the findings the answer produced.
type systemPackageResult struct {
	module        string
	contributions []pkgmanager.Contribution
	findings      []domain.Finding
}

// pkgConfigComponent collects what every used file of one module was described
// as. One module can be described more than once in a run: a sysroot may hold
// two packages of the same name -- a distribution library in /usr/lib and a
// hand-built one in /opt/lib, both called libfoo -- and each file is then
// verified against the .pc file beside itself. Each of those answers is right
// for its own file, but they name one component, and if they state different
// things nobody can say which of them the component carries.
//
// So the answers are compared rather than overwritten: agreement is one answer
// given twice, and disagreement drops the values and is reported. Keeping the
// last one written would publish a version that is wrong for one of the files
// and say nothing about it, which is the guess this reader exists to avoid.
type pkgConfigComponent struct {
	contributions []pkgmanager.Contribution
	// stated is the answer in one comparable string, so that two answers can be
	// held against each other without knowing which fields a .pc file may fill.
	stated string
	// sides is one entry per distinct answer, named after the first file that
	// gave it, in the order the files were mapped in. Per answer and not per
	// file, because a module can describe hundreds of headers and a report that
	// named every one of them would say the same two things over and over.
	sides []domain.ConflictSide
	// disputed says two files of this module were described differently. The
	// contributions are dropped then and the component keeps what the rest of
	// the run knows about it, which for a system library is a name and no
	// version.
	disputed bool
}

// resolvedPackage is one package-manager result expressed in identity terms,
// so that mapping a file needs no filesystem access.
type resolvedPackage struct {
	// id is one package root as an identity. Matching happens on the anchor
	// and the relative path separately: a package that received its own anchor
	// has an empty relative path, and string surgery on the canonical form
	// would have to special-case that.
	id  domain.FileID
	pkg pkgmanager.Package
	// primary marks the entry made from the package's identity root. It is the
	// one that answers where the component begins; a further root -- the build
	// tree beside a FetchContent checkout, say -- holds files of the package
	// but is not the place its licence or its version is read from.
	primary bool
}

func newComponentResolver(cfg config.Config, physical map[string]string, anchorRoots map[string]string, logger *Logger) *componentResolver {
	rules := make([]componentmap.Rule, 0, len(cfg.Components))
	curatedByID := make(map[string]config.Component, len(cfg.Components))
	curatedByTarget := map[string]config.Component{}
	for _, entry := range cfg.Components {
		name := componentNameFor(entry)
		if entry.Path != "" || entry.Match != "" {
			rules = append(rules, componentmap.Rule{
				Path:  entry.Path,
				Match: entry.Match,
				Name:  name,
				Type:  entry.Type,
			})
		}
		for _, target := range entry.Targets {
			curatedByTarget[target] = entry
		}
		curatedByID["component:"+name] = entry
	}
	projectName := cfg.Project.Name
	if projectName == "" {
		projectName = "project"
	}
	return &componentResolver{
		curated:         componentmap.NewMapper(rules),
		curatedByID:     curatedByID,
		curatedByTarget: curatedByTarget,
		physical:        physical,
		projectName:     projectName,
		anchorRoots:     anchorRoots,
		logger:          logger,
		enrich:          pkgmanager.Enrich,
		// One reader per resolver, because the reader holds the run's memo of
		// which .pc files were there and what was in them.
		pkgConfig:            pkgmanager.NewPkgConfigReader().Describe,
		systemPackages:       map[string]systemPackageResult{},
		pkgConfigByComponent: map[string]*pkgConfigComponent{},
	}
}

// componentNameFor is the name a configured component carries: the stated one,
// else the last segment of its path, else the first target it names. A
// component configured by target alone has no path to be named after.
func componentNameFor(entry config.Component) string {
	if entry.Name != "" {
		return entry.Name
	}
	if entry.Path != "" {
		return filepath.Base(strings.TrimSuffix(entry.Path, "/"))
	}
	if len(entry.Targets) > 0 {
		return entry.Targets[0]
	}
	return ""
}

// setTargets records which CMake target owns which file, for strategy 5. A
// file two targets both claim is left out: the build system said two things,
// and picking one would be a guess.
func (r *componentResolver) setTargets(byFile map[string]string) {
	r.targets = byFile
}

// setIntrospection hands over the runner a versionFrom rule may ask git with.
// It is the run's own runner, carrying its anchors and its log, so that every
// command sbomb starts is bounded and recorded in one place (section 9.2).
func (r *componentResolver) setIntrospection(runner *exec.Runner, ctx context.Context) {
	r.runner = runner
	r.ctx = ctx
}

// setDistroMetadata hands over what the configured image manifests state. It
// is a field rather than a package-level call for the reason enrich is: a test
// can put its own index in without the production code exporting a hook.
func (r *componentResolver) setDistroMetadata(distro *pkgmanager.DistroMetadata) {
	r.distro = distro
}

// resolve names the component a file belongs to and records which strategy
// found it, so that the review report can show how a mapping was reached.
func (r *componentResolver) resolve(file domain.UsedFile) (id, name, componentType, scope, detectedBy string) {
	// Strategy 1: curated configuration is the highest authority.
	if mapped, ok := r.curated.MapFile(file.ID); ok {
		return "component:" + mapped.Name, mapped.Name, componentTypeOrDefault(mapped.Type), string(anchors.ScopeThirdParty), "curated"
	}

	// Strategy 2: exact package-manager metadata. A manager states the name,
	// the version and often the licence, which is better evidence than any
	// directory layout, so it comes before every heuristic below.
	if found, ok := r.packageFor(file); ok {
		return "component:" + found.pkg.Name, found.pkg.Name, "library",
			string(anchors.ScopeThirdParty), found.pkg.Manager
	}

	// Strategy 5: an explicit CMake target named in the configuration. The
	// File API states which sources a target owns, so this is the build
	// system's own answer rather than an inference about a directory layout.
	if target, owned := r.targets[file.ID.Canonical()]; owned {
		if entry, mapped := r.curatedByTarget[target]; mapped {
			name := componentNameFor(entry)
			return "component:" + name, name, componentTypeOrDefault(entry.Type),
				string(anchors.ScopeThirdParty), "cmake-target:" + target
		}
	}

	anchorKey := string(file.ID.Anchor)
	kind, anchorName, _ := strings.Cut(anchorKey, ":")

	// Strategy 6: the nearest ancestor directory carrying package metadata.
	// Only inside the file's own anchor, so that the search cannot walk out of
	// the project (section 22.1).
	if root, manifest, found := r.nearestPackageRoot(file); found {
		componentName := filepath.Base(root)
		return "component:" + componentName, componentName, "library", string(anchors.ScopeThirdParty), "package-metadata:" + manifest
	}

	// Between 6 and 7 (section 19.2): the pkg-config metadata a system library
	// installed beside itself names the package the file belongs to. It sits
	// here because it is weaker evidence than a marker file somebody put in a
	// directory -- a .pc file is addressed by a name derived from this file and
	// then has to verify against it -- and stronger than the anchor below,
	// which folds every file of a sysroot into one component and could not
	// carry a version for any of them.
	//
	// Only for a sysroot or a toolchain anchor: everywhere else the strategies
	// above already answer, and asking a project tree for pkg-config metadata
	// would spend stat calls on directories that never hold any.
	if kind == "sysroot" || kind == "toolchain" {
		if module, found := r.systemPackageFor(file, anchorKey); found {
			// The scope is the anchor's and does not move. Section 24.1 keeps
			// system and toolchain files out of the product's dependencies, and
			// naming one of them does not make it one: the component still
			// hangs under build-environment.
			scope := anchors.ScopeSystem
			if kind == "toolchain" {
				scope = anchors.ScopeToolchain
			}
			return "component:" + module, module, "library", string(scope),
				pkgConfigDetectedByPrefix + module + ".pc"
		}
	}

	// Strategy 7: the anchor root itself.
	switch kind {
	case "project", "build":
		return "component:" + r.projectName, r.projectName, "application", string(anchors.ScopeProject), "anchor:project"
	case "pkg", "sdk", "extern":
		return anchorKey, anchorName, "library", scopeForAnchorKind(kind), "anchor:" + kind
	case "toolchain":
		return anchorKey, anchorName, "library", string(anchors.ScopeToolchain), "anchor:toolchain"
	case "sysroot":
		return anchorKey, anchorName, "library", string(anchors.ScopeSystem), "anchor:sysroot"
	}

	// Strategy 8: unknown, emitted and flagged rather than dropped (19.3).
	segment := file.ID.RelPath
	if index := strings.IndexByte(segment, '/'); index >= 0 {
		segment = segment[:index]
	}
	unknown := "unknown:" + anchorKey + "/" + segment
	return unknown, unknown, "library", string(anchors.ScopeUnknown), "unresolved"
}

// packagePaths is how setPackages turns the paths an adapter reported into
// identities. A root and a listed file are resolved differently on purpose:
// register also records where a root's bytes are, because later steps read
// them, while a listed file is only a claim -- most of what a package manager
// installed no evidence chain ever reached -- and recording those would grow
// the physical map with the size of the installation tree rather than with the
// number of used files (section 31).
type packagePaths struct {
	register func(string) domain.FileID
	lookup   func(string) domain.FileID
}

// setPackages records what the package-manager adapters found, expressed as
// canonical identity prefixes. The longest prefix wins, so a package nested
// inside another maps to the inner one (section 19.2). Every root of a package
// becomes an entry, because a package's files need not all live under one of
// them -- FetchContent puts the checkout in _deps/<name>-src and everything
// CMake generated for it in _deps/<name>-build, and both are the package.
//
// A file two packages both name is dropped, and that is reported rather than
// logged: the file then falls through to the strategies below package
// metadata, which can land it in a third component, and a reviewer who never
// turns debug logging on would otherwise never learn that two managers had
// disagreed about it.
func (r *componentResolver) setPackages(packages []pkgmanager.Package, paths packagePaths) []domain.Finding {
	r.packages = make([]resolvedPackage, 0, len(packages))
	r.packageFiles = map[string]resolvedPackage{}
	// A file two packages both name is dropped rather than given to one of
	// them, exactly as setTargets does for two targets: two statements are no
	// statement, and choosing between them would be a guess.
	contested := map[string][]domain.ConflictSide{}
	for _, entry := range packages {
		var identity resolvedPackage
		for index, root := range entry.Roots {
			id := paths.register(root)
			if id.Anchor == "" {
				continue
			}
			// A path that is its own anchor root comes back with "." as the
			// relative part; the package then covers everything under the anchor.
			if id.RelPath == "." || id.RelPath == "/" {
				id.RelPath = ""
			}
			r.logger.Debug("Package %s maps to identity %s", entry.Name, id.Canonical())
			resolved := resolvedPackage{id: id, pkg: entry, primary: index == 0}
			if resolved.primary {
				identity = resolved
			}
			r.packages = append(r.packages, resolved)
		}
		if identity.id.Anchor == "" {
			continue
		}
		for _, file := range entry.Files {
			id := paths.lookup(file)
			if id.Anchor == "" {
				continue
			}
			key := id.Canonical()
			if other, claimed := r.packageFiles[key]; claimed && other.pkg.Name != entry.Name {
				// The package that claimed it first is a side of the dispute
				// too, and a file three packages list has three sides.
				if len(contested[key]) == 0 {
					contested[key] = append(contested[key],
						domain.ConflictSide{Source: other.pkg.Manager, Value: other.pkg.Name})
				}
				contested[key] = append(contested[key],
					domain.ConflictSide{Source: entry.Manager, Value: entry.Name})
				continue
			}
			r.packageFiles[key] = identity
		}
	}
	findings := make([]domain.Finding, 0, len(contested))
	// Sorted, because a map is iterated in a different order on every run and
	// the findings of two runs over one build have to agree byte for byte.
	for _, key := range sortedKeys(contested) {
		delete(r.packageFiles, key)
		conflict := domain.Conflict{
			Field:   "package that owns this file",
			Subject: domain.Subject{Kind: "file", Ref: key},
			Sides:   sortedSides(contested[key]),
			Reason: "two statements are no statement, so the file falls through to the mapping " +
				"strategies below package metadata (section 19.2)",
		}
		if finding, ok := conflict.Finding("COMPONENT_MAPPING_CONFLICT", domain.SeverityInfo); ok {
			findings = append(findings, finding)
		}
	}
	// The order has to be total, not merely longest-first: with several entries
	// per package two roots of equal length are ordinary, and a comparison that
	// calls them equal would let the sort decide which package a file belongs
	// to differently on the next run.
	sort.Slice(r.packages, func(i, j int) bool {
		a, b := r.packages[i], r.packages[j]
		if len(a.id.RelPath) != len(b.id.RelPath) {
			return len(a.id.RelPath) > len(b.id.RelPath)
		}
		if a.id.Anchor != b.id.Anchor {
			return a.id.Anchor < b.id.Anchor
		}
		if a.id.RelPath != b.id.RelPath {
			return a.id.RelPath < b.id.RelPath
		}
		return a.pkg.Name < b.pkg.Name
	})
	return findings
}

// sortedSides puts the sides of a conflict in a fixed order and drops the
// repetitions the evidence produces -- several roots of one package, one target
// listed in two build configurations -- so that the same evidence always yields
// the same sentence.
func sortedSides(sides []domain.ConflictSide) []domain.ConflictSide {
	sorted := append([]domain.ConflictSide(nil), sides...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Source != sorted[j].Source {
			return sorted[i].Source < sorted[j].Source
		}
		return sorted[i].Value < sorted[j].Value
	})
	unique := sorted[:0]
	for index, side := range sorted {
		if index == 0 || side != sorted[index-1] {
			unique = append(unique, side)
		}
	}
	return unique
}

// packageFor finds the package a file belongs to. A manager that listed the
// file by name is asked first: that is a record of what it installed, while a
// root is only where it usually puts things -- and inside a vcpkg triplet tree
// every package shares the same include and lib directories, so the root can
// answer nothing there. Only then do the roots decide, matching on whole path
// segments so that a sibling directory with a shared prefix cannot claim a file.
func (r *componentResolver) packageFor(file domain.UsedFile) (resolvedPackage, bool) {
	if entry, claimed := r.packageFiles[file.ID.Canonical()]; claimed {
		return entry, true
	}
	for _, entry := range r.packages {
		if file.ID.Anchor != entry.id.Anchor {
			continue
		}
		root := entry.id.RelPath
		// An empty relative path means the package is the anchor root, so
		// everything under that anchor belongs to it.
		if root == "" || file.ID.RelPath == root || strings.HasPrefix(file.ID.RelPath, root+"/") {
			return entry, true
		}
	}
	return resolvedPackage{}, false
}

// pkgConfigDetectedByPrefix opens the detectedBy of a component the pkg-config
// metadata named, and the .pc file's own name follows it. resolveRoot reads the
// prefix back, so the two strings that make the strategy visible are one
// constant rather than two literals that can drift apart.
const pkgConfigDetectedByPrefix = "pkg-config:"

// systemPackageFor asks the pkg-config metadata beside a system file which
// package it belongs to. The answer is memoized per file, because resolve is
// asked about one file more than once in a run and the reader would otherwise
// walk the same directories again.
//
// The boundary handed over is the file's own anchor root, so the upward walk
// can never leave it (section 22.1). A file whose bytes this run never located,
// or whose anchor has no root, is not asked about at all: there would be
// nothing to walk up from and nothing to stop at.
func (r *componentResolver) systemPackageFor(file domain.UsedFile, anchorKey string) (string, bool) {
	key := file.ID.Canonical()
	cached, known := r.systemPackages[key]
	if !known {
		if r.pkgConfig == nil {
			return "", false
		}
		path, boundary := r.physical[key], r.anchorRoots[anchorKey]
		if path == "" || boundary == "" {
			return "", false
		}
		described, findings := r.pkgConfig(pkgmanager.SystemFile{Path: path, Boundary: boundary, Ref: key})
		cached = systemPackageResult{
			module:        described.Module,
			contributions: described.Contributions,
			findings:      findings,
		}
		r.systemPackages[key] = cached
		if described.Described() {
			r.logger.Debug("System file %s is described by pkg-config module %s", key, described.Module)
		}
	}
	r.systemPackageFindings = append(r.systemPackageFindings, cached.findings...)
	if cached.module == "" {
		return "", false
	}
	// Recorded on every answer and not only on the first read of the file,
	// because what a component carries has to be built from the files this pass
	// really mapped. The memo above keeps the filesystem from being read twice;
	// it must not keep a file that was mapped from being counted.
	r.recordSystemPackage(cached.module, key, cached.contributions)
	return cached.module, true
}

// recordSystemPackage holds what one used file was described as against what
// the other files of the same module were described as. Two files that agree
// are one answer; two that disagree leave the component with neither, and the
// disagreement is reported by takePkgConfigConflicts.
func (r *componentResolver) recordSystemPackage(module, fileRef string, contributions []pkgmanager.Contribution) {
	stated := statedPkgConfig(contributions)
	entry, known := r.pkgConfigByComponent[module]
	if !known {
		r.pkgConfigByComponent[module] = &pkgConfigComponent{
			contributions: contributions,
			stated:        stated,
			sides:         []domain.ConflictSide{{Source: fileRef, Value: stated}},
		}
		return
	}
	if entry.stated == stated {
		return
	}
	entry.disputed = true
	entry.contributions = nil
	// One side per distinct answer rather than per file: a module can describe
	// hundreds of headers, and a report that named every one of them would say
	// the same two things over and over. The first file to state an answer is
	// the one that stands for it.
	for _, side := range entry.sides {
		if side.Value == stated {
			return
		}
	}
	entry.sides = append(entry.sides, domain.ConflictSide{Source: fileRef, Value: stated})
}

// statedPkgConfig writes what a .pc file contributed as one comparable string.
// Only a version ever arrives today, but comparing the whole set rather than
// that one field means a later contribution cannot slip past the comparison.
func statedPkgConfig(contributions []pkgmanager.Contribution) string {
	if len(contributions) == 0 {
		return "nothing"
	}
	parts := make([]string, 0, len(contributions))
	for _, contribution := range contributions {
		parts = append(parts, fmt.Sprintf("%s %s", contribution.Field, contribution.Claim.Value))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// takePkgConfigConflicts reports every module whose files were described
// differently. It is asked once the files have been grouped, because only then
// is it known which files reached the document at all.
func (r *componentResolver) takePkgConfigConflicts() []domain.Finding {
	findings := []domain.Finding{}
	// Sorted, because a map is iterated in a different order on every run and
	// two runs over one build have to report the same thing in the same order.
	modules := make([]string, 0, len(r.pkgConfigByComponent))
	for module := range r.pkgConfigByComponent {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	for _, module := range modules {
		entry := r.pkgConfigByComponent[module]
		if !entry.disputed {
			continue
		}
		conflict := domain.Conflict{
			Field:   "version the pkg-config metadata states for this component",
			Subject: domain.Subject{Kind: "component", Ref: "component:" + module},
			Sides:   entry.sides,
			Reason: "two packages of one module name were described differently, and two statements " +
				"are no statement, so the component keeps no value from either (section 19.2)",
		}
		if finding, ok := conflict.Finding("COMPONENT_MAPPING_CONFLICT", domain.SeverityInfo); ok {
			findings = append(findings, finding)
		}
	}
	return findings
}

// forgetSystemPackages drops what reading pkg-config metadata produced before
// the files were grouped. resolve is also asked about headers that narrowing
// removed from the used set, and neither a finding nor a description that came
// from a file the document does not contain belongs in it. What survives is the
// memo of which .pc files were there, so nothing is read a second time.
func (r *componentResolver) forgetSystemPackages() {
	r.systemPackageFindings = nil
	r.pkgConfigByComponent = map[string]*pkgConfigComponent{}
}

// takeSystemPackageFindings hands over what reading pkg-config metadata
// reported and empties the collection.
//
// It is emptied rather than only read because resolve is also asked about
// headers that narrowing removed from the used set, and a finding about a file
// the document does not contain would name nothing a reader could look at. The
// memo survives the emptying, so nothing is read a second time, and a file that
// is in the document replays its own findings when it is mapped. The one thing
// this costs is a .pc file whose only reader was a narrowed-away header: its
// findings are dropped with that file's, because the file they would have been
// attached to is not in the document either.
func (r *componentResolver) takeSystemPackageFindings() []domain.Finding {
	findings := r.systemPackageFindings
	r.systemPackageFindings = nil
	return findings
}

// unusedPackageFindings reports every package that no used file belongs to.
// Leaving it out of the document is correct -- a dependency that was installed
// but never linked is not part of the product -- but until now it happened
// without a word, and "the SBOM does not list the library I installed" has to
// be answerable from the findings rather than from the source.
func (r *componentResolver) unusedPackageFindings(files []domain.UsedFile) []domain.Finding {
	if len(r.packages) == 0 {
		return nil
	}
	// Whether a package was reached is asked of packageFor alone, not of
	// resolve: a file that curated configuration or a CMake target assigned
	// elsewhere still belongs to the package, and calling such a package
	// unlinked would be false.
	linked := map[string]bool{}
	for _, file := range files {
		if entry, ok := r.packageFor(file); ok {
			linked[entry.pkg.Name] = true
		}
	}
	findings := []domain.Finding{}
	reported := map[string]bool{}
	for _, entry := range r.packages {
		name := entry.pkg.Name
		if name == "" || linked[name] || reported[name] {
			continue
		}
		reported[name] = true
		findings = append(findings, domain.Finding{
			ID: "PACKAGE_NOT_LINKED", Severity: domain.SeverityInfo,
			Subject: domain.Subject{Kind: "component", Ref: name},
			Message: fmt.Sprintf("%s installed this dependency, but no used file belongs to it, so it is not part of the product and not in this document",
				entry.pkg.Manager),
		})
	}
	return findings
}

// packageByName finds the package behind a component, for enrichment.
func (r *componentResolver) packageByName(name string) (pkgmanager.Package, bool) {
	for _, entry := range r.packages {
		if entry.pkg.Name == name {
			return entry.pkg, true
		}
	}
	return pkgmanager.Package{}, false
}

// nearestPackageRoot walks up from a file looking for a package manifest,
// stopping at the anchor root so the search never leaves the component tree.
func (r *componentResolver) nearestPackageRoot(file domain.UsedFile) (root, manifest string, found bool) {
	path := r.physical[file.ID.Canonical()]
	if path == "" {
		return "", "", false
	}
	boundary := r.anchorRoots[string(file.ID.Anchor)]
	dir := filepath.Dir(path)
	for depth := 0; depth < 64 && dir != "" && dir != "/" && dir != "."; depth++ {
		atBoundary := boundary != "" && filepath.Clean(dir) == filepath.Clean(boundary)
		for _, name := range packageMetadataFiles {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
				return dir, name, true
			}
		}
		// A licence file marks a boundary too, but never at the anchor root
		// itself: a project's own top-level licence describes the project, not
		// a dependency inside it, and treating it as a marker would rename the
		// project's own component after its directory. A bundled SBOM is the
		// same kind of statement under the same exception.
		if !atBoundary {
			for _, name := range licenseBoundaryFiles {
				if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
					return dir, name, true
				}
			}
			for _, name := range bundledSBOMFiles {
				if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
					return dir, name, true
				}
			}
		}
		if atBoundary {
			return "", "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
	return "", "", false
}

func componentTypeOrDefault(value string) string {
	if value == "" {
		return "library"
	}
	return value
}

// enrichComponent fills in the CRA fields of section 1.5(1): version,
// supplier, license and purl. Each one is either resolved from an authorized
// source or reported as missing; nothing is guessed from a directory name.
func (r *componentResolver) enrichComponent(component *domain.Component, files []domain.UsedFile) []domain.Finding {
	findings := []domain.Finding{}
	curated, hasCurated := r.curatedByID[component.ID]
	managed, isManaged := r.packageByName(component.Name)

	// Where the component begins (section 19.2). Settled once, here, because
	// the licence file and the version header are both read from it.
	rootInfo := r.resolveRoot(component, files)
	if rootInfo.ID.Anchor != "" {
		root := rootInfo.ID
		component.Root = &root
		component.Properties = addProperty(component.Properties, "sbomb:component:root", root.Canonical())
	}
	if rootInfo.Source == rootSourceUsedFiles {
		findings = append(findings, componentFinding("COMPONENT_ROOT_UNRESOLVED", domain.SeverityInfo, component,
			"no configuration, package manager or marker file named this component's root, so it was taken to be the deepest common directory of the files that were used",
			"Add components[].path for this component, or place a licence file at its root."))
	}
	r.logger.Debug("Component '%s': root %s (%s)", component.Name, rootInfo.ID.Canonical(), rootInfo.Source)

	// A component that only a marker file found carries nothing but its
	// directory name: no version, no supplier, no purl. Whatever describes it
	// lies in that directory, so it is read here -- and only here. A root a
	// package manager owns was already described while it was discovered, and
	// a root taken from the used files is a guess, where reading anything
	// would promote that guess to a source. Enrichment describes; it never
	// writes to r.packages or r.packageFiles and never touches the component's
	// name or identity, because both settled before the files were grouped.
	var described pkgmanager.Package
	if !isManaged {
		if rootInfo.Physical != "" && strings.HasPrefix(rootInfo.Source, rootSourceMarkerPrefix) {
			found, enrichmentFindings := r.enrich(pkgmanager.ComponentRoot{Path: rootInfo.Physical, Name: component.Name})
			described = found
			findings = append(findings, enrichmentFindings...)
		}
		// What the .pc file that named this component stated about it. It is
		// folded before the image manifest because the two rank alike (section
		// 21.1) and Take keeps the first: this file was found by walking up
		// from a file of this very component and had to verify against it,
		// while a manifest matched nothing but a name. Only a version ever
		// arrives -- a .pc file names no licence, no supplier and no package
		// ecosystem -- so the findings that say those are missing stay.
		if stated, known := r.pkgConfigByComponent[component.Name]; known {
			for _, contribution := range stated.contributions {
				described.Take(contribution.Field, contribution.Claim)
			}
		}
		// An image manifest of an embedded-Linux distribution build answers
		// here too, and it has to be asked separately from the reader above:
		// it is keyed by name rather than by directory, so it can describe a
		// component that a marker file never bounded -- one named after an
		// anchor, or after nothing at all -- which is what a file out of a
		// Yocto sysroot usually becomes. It is folded through Take, so the
		// ranking of section 21.1 decides and the switches below need no
		// branch of their own. A component a manager owns is not asked: its
		// package was described while it was discovered.
		for _, contribution := range r.distro.Describe(component.Name) {
			described.Take(contribution.Field, contribution.Claim)
		}
	}

	// Version (section 20.2): curated first, then exact package-manager
	// metadata, then what curated versionFrom permits, and only after all
	// three what the component root itself states.
	switch {
	case hasCurated && curated.Version != "":
		component.Version = curated.Version
		component.VersionSource = "curated"
		component.VersionConf = domain.ConfidenceHigh
	case isManaged && managed.Version.Value != "":
		// Several files of one manager can state a version; what is published
		// here is the claim that won its ranking (section 21.1), so the source
		// beside the version names the origin the value really came from.
		component.Version = managed.Version.Value
		component.VersionSource = managed.Version.Source
		component.VersionConf = managed.Version.Confidence
	case hasCurated && len(curated.VersionFrom) > 0:
		if resolved, ok := version.Resolve(curated.VersionFrom, rootInfo.Physical,
			version.Options{Runner: r.runner, Context: r.ctx}); ok {
			component.Version = resolved.Version
			component.VersionSource = resolved.Source
			component.VersionConf = resolved.Confidence
			if resolved.Dirty {
				// A version read out of a modified tree does not identify the
				// content it names, so it is published with that fact beside
				// it rather than as if the tree were clean.
				findings = append(findings, componentFinding("VCS_DIRTY", domain.SeverityInfo, component,
					"the component's checkout has uncommitted changes, so its version does not identify its content",
					"Commit or stash the changes before generating a deliverable SBOM."))
			}
		}
	case described.Version.Value != "":
		// Behind versionFrom, not in front of it: versionFrom is the user
		// saying where this component's version is to be read from, and a
		// manifest that overtook that instruction would be a step backwards
		// even when the rule it overtook found nothing.
		component.Version = described.Version.Value
		component.VersionSource = described.Version.Source
		component.VersionConf = described.Version.Confidence
	}
	if component.Version == "" {
		message := "no authorized source supplied a version"
		remediation := "Add components[].version, or components[].versionFrom naming where the version can be read."
		if hasCurated && needsGitIntrospection(curated.VersionFrom) && (r.runner == nil || !r.runner.Features.Git) {
			// The rule named git, and git was never asked. Saying so is the
			// difference between a missing version and a missing permission.
			message = "the component's versionFrom names git, but git introspection is off; run with --allow-introspection=git to read the version from the checkout"
			remediation = "Enable git introspection, or set components[].version for this component."
		}
		findings = append(findings, componentFinding("UNKNOWN_VERSION", domain.SeverityWarning, component,
			message, remediation))
	}

	// Supplier (section 20.5): curated or package metadata only. Deriving it
	// from a repository URL host is explicitly forbidden.
	if hasCurated && curated.Supplier != "" {
		component.Supplier = curated.Supplier
	} else if isManaged && managed.Supplier.Value != "" {
		component.Supplier = managed.Supplier.Value
	} else if described.Supplier.Value != "" {
		component.Supplier = described.Supplier.Value
	}
	if component.Supplier == "" {
		findings = append(findings, componentFinding("MISSING_SUPPLIER", domain.SeverityWarning, component,
			"the component has no supplier, which BSI TR-03183-2 requires",
			"Add components[].supplier for this component."))
	}

	// PURL (section 20.4): only when a package type and name can be asserted.
	if hasCurated && curated.PURL != "" {
		component.PURL = curated.PURL
	} else if isManaged && managed.PURL.Value != "" {
		component.PURL = managed.PURL.Value
	} else if described.PURL.Value != "" {
		// Before the anchor fallback: a purl a manifest states names the
		// package, while one built from an anchor key only names where the
		// files were found.
		component.PURL = described.PURL.Value
	} else if kind, name, ok := purlFromAnchor(component.ID); ok {
		component.PURL = version.PURL(kind, name, component.Version)
	}
	if isManaged {
		// Section 20.5: the repository URL belongs in externalReferences, and
		// it is recorded only because the manager recorded it -- never as a
		// stand-in for a supplier. Where it goes in the document is the
		// writer's decision, so the fact is handed over rather than rendered.
		if managed.VCSURL != "" || managed.Commit != "" || managed.Dirty {
			component.VCS = &domain.VCSRecord{URL: managed.VCSURL, Commit: managed.Commit, Dirty: managed.Dirty}
		}
	}
	if component.PURL == "" {
		findings = append(findings, componentFinding("UNKNOWN_PURL", domain.SeverityInfo, component,
			"no package type could be asserted, so no purl is emitted", ""))
	}

	// Licenses (section 22.2).
	licenseFindings := r.resolveComponentLicense(component, files, curated, hasCurated, rootInfo.Physical, described.License)
	findings = append(findings, licenseFindings...)

	// A component with no hashable file cannot carry a component hash
	// (section 1.5(1) via the MISSING_COMPONENT_HASH gate).
	var hashed int
	for _, file := range files {
		if len(file.Hashes) > 0 {
			hashed++
		}
	}
	if hashed == 0 {
		findings = append(findings, componentFinding("MISSING_COMPONENT_HASH", domain.SeverityWarning, component,
			"no file of this component could be hashed", ""))
	}

	if strings.HasPrefix(component.Name, "unknown:") {
		findings = append(findings, componentFinding("UNKNOWN_COMPONENT", domain.SeverityWarning, component,
			fmt.Sprintf("%d file(s) could not be mapped to a known component", len(files)),
			"Add a components[] entry covering these paths."))
	}
	return findings
}

// resolveComponentLicense applies the priority order of section 22.2, limited
// to the sources that exist today: curated configuration, an SPDX identifier
// in a used file, what a reader found in the component root, what the package
// manager stated, and a recognized license file in that root.
func (r *componentResolver) resolveComponentLicense(component *domain.Component, files []domain.UsedFile, curated config.Component, hasCurated bool, root string, described pkgmanager.Claim) []domain.Finding {
	findings := []domain.Finding{}

	// Section 22.2 in order: an SPDX identifier in a used file (2), then the
	// component's own manifest (3), then what the package manager declared or
	// placed in the package (4), then a licence file found in the component
	// root (5).
	var fromFiles domain.LicenseFinding
	// observed holds the complete licence texts found in a file that is not
	// itself one licence -- two of them one after the other, or one with
	// material around it. It is evidence, never a conclusion.
	var observed []domain.LicenseFinding
	for _, file := range files {
		path := r.physical[file.ID.Canonical()]
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if found := license.ResolveFromText(string(data), path); found.Expression != "" {
			fromFiles = found
			observed = nil
			break
		}
		if len(observed) == 0 {
			observed = license.ObserveFindings(string(data), path)
		}
	}
	if fromFiles.Expression == "" && described.Value != "" {
		// Point 3 of the order, which the specification puts above what a
		// package manager says. The two can never both answer today, because a
		// root a manager owns is described while it is discovered and its
		// claims are ranked there (section 21.1); the order is written out
		// anyway, so that the day one of them changes the code already says
		// which one the specification meant to win.
		//
		// Source stays empty for the same reason it does below: a licence
		// finding's Source names the file a licence text was read from, and
		// here an expression was declared rather than a text read.
		fromFiles = domain.LicenseFinding{
			Expression: described.Value,
			Evidence:   "component-level",
			Confidence: domain.ConfidenceHigh,
		}
	}
	if fromFiles.Expression == "" {
		if found, ok := r.licenseFromPackageManager(component.Name); ok {
			fromFiles = found
		}
	}
	if fromFiles.Expression == "" {
		if found, ok := r.licenseFromComponentRoot(root); ok {
			fromFiles = found
		}
	}
	if fromFiles.Expression == "" && len(observed) == 0 {
		observed = r.licenseEvidenceFromComponentRoot(root)
	}

	switch {
	case hasCurated && curated.License != "":
		effective := domain.LicenseFinding{
			Expression: curated.License,
			Evidence:   "component-level",
			Confidence: domain.ConfidenceHigh,
			Source:     "curated",
		}
		// Section 22.5: a conflict is never resolved silently. The curated
		// value stays effective and the conflicting one is recorded.
		if conflict, differs := license.ResolveConflict(curated.License, fromFiles.Expression, fromFiles.Source); differs {
			component.Licenses = []domain.LicenseFinding{conflict}
			component.Properties = addProperty(component.Properties, "sbomb:license:conflictingValue", fromFiles.Expression)
			component.Properties = addProperty(component.Properties, "sbomb:review:required", "true")
			findings = append(findings, componentFinding("LICENSE_CONFLICT", domain.SeverityWarning, component,
				fmt.Sprintf("configuration says %q but the component's own files say %q", curated.License, fromFiles.Expression),
				"Resolve the disagreement, then update components[].license."))
			return findings
		}
		component.Licenses = []domain.LicenseFinding{effective}
		component.LicenseEvidence = observed
	case fromFiles.Expression != "":
		component.Licenses = []domain.LicenseFinding{fromFiles}
	case len(observed) > 0:
		// The file holds complete licence texts without being one of them.
		// Which licences are present is recorded; whether they apply together
		// or the recipient chooses is written in the prose between them, and
		// reading that would be the keyword heuristic section 22.3 forbids.
		component.LicenseEvidence = observed
		component.Licenses = []domain.LicenseFinding{{
			Name:     "NOASSERTION",
			Evidence: "unknown",
			Reason:   license.ReasonLicenseCompositionUnresolved,
		}}
		component.Properties = addProperty(component.Properties, "sbomb:license:review", "true")
		component.Properties = addProperty(component.Properties, "sbomb:license:reason", license.ReasonLicenseCompositionUnresolved)
		findings = append(findings, componentFinding("UNKNOWN_LICENSE", domain.SeverityWarning, component,
			fmt.Sprintf("the licence file contains the text of %s, but is not any one of them; how they combine is not machine-readable",
				strings.Join(licenseNames(observed), " and ")),
			"Read the file and record the relationship in components[].license, for example \"MIT OR Apache-2.0\"."))
	default:
		component.Licenses = []domain.LicenseFinding{{
			Name:     "NOASSERTION",
			Evidence: "unknown",
			Reason:   license.ReasonNoEvidence,
		}}
		component.Properties = addProperty(component.Properties, "sbomb:license:review", "true")
		component.Properties = addProperty(component.Properties, "sbomb:license:reason", license.ReasonNoEvidence)
		findings = append(findings, componentFinding("UNKNOWN_LICENSE", domain.SeverityWarning, component,
			"no license evidence was found for this component",
			"Add components[].license, or place a recognized license file in the component root."))
	}

	if len(component.Licenses) > 0 {
		component.Properties = addProperty(component.Properties, "sbomb:license:evidenceClass", component.Licenses[0].Evidence)
		if component.Licenses[0].Technique != "" {
			// Which of the techniques of section 22.3 answered. A reviewer
			// checking a licence needs to know whether it came from a
			// declaration in the file, an exact digest or a template match.
			component.Properties = addProperty(component.Properties, "sbomb:license:technique", component.Licenses[0].Technique)
		}
		if component.Licenses[0].Source != "" {
			component.Properties = addProperty(component.Properties, "sbomb:license:source", component.Licenses[0].Source)
		}
	}
	return findings
}

// licenseFromPackageManager uses what the manager declared, or the licence
// file it placed in the package itself. Both are stronger than walking a
// directory looking for something licence-shaped: the manager put it there and
// says which package it belongs to.
func (r *componentResolver) licenseFromPackageManager(name string) (domain.LicenseFinding, bool) {
	managed, ok := r.packageByName(name)
	if !ok {
		return domain.LicenseFinding{}, false
	}
	if managed.License.Value != "" {
		// The claim knows which file stated the expression, but a licence
		// finding's Source names the file a licence text was read from, and no
		// text was read here. Publishing the manifest's name there would say
		// something the evidence does not support.
		return domain.LicenseFinding{
			Expression: managed.License.Value,
			Evidence:   "component-level",
			Confidence: domain.ConfidenceHigh,
		}, true
	}
	if managed.LicenseFile == "" {
		return domain.LicenseFinding{}, false
	}
	found, err := license.ResolveFile(managed.LicenseFile)
	if err != nil || found.Expression == "" {
		return domain.LicenseFinding{}, false
	}
	found.Evidence = "component-level"
	return found, true
}

// licenseFromComponentRoot looks for a recognized license file, but only in
// the component root itself. Section 22.1 forbids scanning the repository for
// license files outside mapped component roots.
func (r *componentResolver) licenseFromComponentRoot(root string) (domain.LicenseFinding, bool) {
	if root == "" {
		return domain.LicenseFinding{}, false
	}
	for _, name := range recognizedLicenseFiles {
		found, err := license.ResolveFile(filepath.Join(root, name))
		if err == nil && found.Expression != "" {
			found.Evidence = "component-level"
			found.Source = name
			return found, true
		}
	}
	return domain.LicenseFinding{}, false
}

// licenseEvidenceFromComponentRoot observes the licence texts in the component
// root's licence file when nothing there resolved to one licence.
func (r *componentResolver) licenseEvidenceFromComponentRoot(root string) []domain.LicenseFinding {
	if root == "" {
		return nil
	}
	for _, name := range recognizedLicenseFiles {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		if observed := license.ObserveFindings(string(data), name); len(observed) > 0 {
			return observed
		}
	}
	return nil
}

// licenseNames lists the identifiers of a set of observations, for a message.
func licenseNames(findings []domain.LicenseFinding) []string {
	names := make([]string, 0, len(findings))
	for _, finding := range findings {
		names = append(names, finding.Expression)
	}
	return names
}

// recognizedLicenseFiles is the exhaustive list of section 22.3.
var recognizedLicenseFiles = []string{
	"LICENSE", "LICENSE.txt", "LICENSE.md",
	"LICENCE", "LICENCE.txt", "LICENCE.md",
	"COPYING", "COPYING.txt", "COPYING.md",
	"NOTICE", "COPYRIGHT",
}

// componentRootResult is where a component begins, and how that was
// established. Section 19.2 makes the root a resolved fact: the licence file
// and the version header live there, so a root that moves with whichever files
// the linker happened to keep makes both of them move with it.
type componentRootResult struct {
	// ID is the root as an identity, for sbomb:component:root. A property
	// never carries an absolute path.
	ID domain.FileID
	// Physical is where the bytes are, for reading a licence or a version.
	Physical string
	// Source names the strategy that settled it, for the review report.
	Source string
}

// rootSourceUsedFiles is the last resort: the deepest common directory of the
// files that were used. It is a guess, and it is the only value of Source that
// makes COMPONENT_ROOT_UNRESOLVED fire.
const rootSourceUsedFiles = "used-files"

// rootSourceMarkerPrefix opens the Source of a root that strategy 6 found, and
// the marker's file name follows it. A marker is a file somebody put there, so
// unlike a root computed from the used files this one is a statement and can be
// read from.
const rootSourceMarkerPrefix = "marker:"

// rootSourcePkgConfig is the source of a component the pkg-config metadata
// named. Such a component has no root at all: see resolveRoot for why that is
// the answer rather than a gap.
const rootSourcePkgConfig = "pkg-config"

// resolveRoot settles where a component begins, following the same priority
// order that named it (section 19.2). Each branch mirrors one strategy, so the
// root and the name can never come from two different places.
func (r *componentResolver) resolveRoot(component *domain.Component, files []domain.UsedFile) componentRootResult {
	if len(files) == 0 {
		return componentRootResult{Source: rootSourceUsedFiles}
	}

	// Strategy 1: the configured path is the root by definition.
	if rule, ok := r.curated.Match(files[0].ID); ok && rule.Path != "" {
		id := domain.FileID{Anchor: files[0].ID.Anchor, RelPath: strings.Trim(filepath.ToSlash(rule.Path), "/")}
		if physical := r.physicalForRoot(id, files); physical != "" {
			return componentRootResult{ID: id, Physical: physical, Source: "curated"}
		}
	}

	// Strategy 2: the package manager stated where its package lives. Only the
	// identity root answers this: the further roots hold files of the package,
	// but the licence and the version are read from the checkout, not from the
	// tree the build wrote beside it.
	for _, entry := range r.packages {
		if entry.primary && entry.pkg.Name == component.Name && entry.pkg.Root() != "" {
			return componentRootResult{ID: entry.id, Physical: entry.pkg.Root(), Source: "package-manager"}
		}
	}

	// Strategy 6: the marker the mapping walk already found. It walked up from
	// the file and stopped at the anchor; this only reads back its answer,
	// which until now was reduced to the directory's base name.
	for _, file := range files {
		root, marker, found := r.nearestPackageRoot(file)
		if !found {
			continue
		}
		if id, ok := identityForRoot(file.ID, r.physical[file.ID.Canonical()], root); ok {
			return componentRootResult{ID: id, Physical: root, Source: rootSourceMarkerPrefix + marker}
		}
	}

	// The pkg-config strategy settles no root, and that is deliberate. A .pc
	// file names where the package installed its libraries, not a directory
	// this component owns: taking /usr/lib for a component root would start a
	// licence search across the whole sysroot for every distribution library in
	// the document. The source is its own so that COMPONENT_ROOT_UNRESOLVED
	// does not fire for a root nobody was looking for.
	if strings.HasPrefix(component.DetectedBy, pkgConfigDetectedByPrefix) {
		return componentRootResult{Source: rootSourcePkgConfig}
	}

	// Strategy 7: the component is the anchor, so the anchor root is its root.
	if strings.HasPrefix(component.DetectedBy, "anchor:") {
		key := component.ID
		switch strings.TrimPrefix(component.DetectedBy, "anchor:") {
		case "project", "build":
			key = "project"
		}
		if root := r.anchorRoots[key]; root != "" {
			return componentRootResult{ID: domain.FileID{Anchor: domain.AnchorKey(key)}, Physical: root, Source: "anchor"}
		}
	}

	// Nothing named a root, so the files have to. This is where a component
	// whose sources sit in src/ loses its licence file, and the finding says
	// so rather than leaving it to be discovered.
	physical := r.componentRoot(files)
	id, _ := identityForRoot(files[0].ID, r.physical[files[0].ID.Canonical()], physical)
	return componentRootResult{ID: id, Physical: physical, Source: rootSourceUsedFiles}
}

// physicalForRoot maps a root identity to a directory on disk, by trimming
// from one of the component's files the part that lies below the root.
func (r *componentResolver) physicalForRoot(root domain.FileID, files []domain.UsedFile) string {
	prefix := root.RelPath
	for _, file := range files {
		if file.ID.Anchor != root.Anchor {
			continue
		}
		rel := strings.Trim(file.ID.RelPath, "/")
		if prefix != "" && !strings.HasPrefix(rel, prefix+"/") && rel != prefix {
			continue
		}
		physical := r.physical[file.ID.Canonical()]
		if physical == "" {
			continue
		}
		below := strings.TrimPrefix(strings.TrimPrefix(rel, prefix), "/")
		depth := 0
		if below != "" {
			depth = len(strings.Split(below, "/"))
		}
		dir := physical
		for i := 0; i < depth; i++ {
			dir = filepath.Dir(dir)
		}
		return dir
	}
	return ""
}

// identityForRoot expresses a directory as an identity, by dropping from a
// file of the component as many trailing segments as the directory drops from
// that file's physical path. Both paths share those segments, so no assumption
// about either root is needed.
func identityForRoot(file domain.FileID, physical, root string) (domain.FileID, bool) {
	if physical == "" || root == "" {
		return domain.FileID{}, false
	}
	rel, err := filepath.Rel(root, physical)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return domain.FileID{}, false
	}
	below := strings.Split(filepath.ToSlash(rel), "/")
	segments := strings.Split(strings.Trim(file.RelPath, "/"), "/")
	if len(segments) < len(below) {
		return domain.FileID{}, false
	}
	return domain.FileID{Anchor: file.Anchor, RelPath: strings.Join(segments[:len(segments)-len(below)], "/")}, true
}

// componentRoot is the deepest directory containing every file of a component,
// which is where its license file and version header would live.
func (r *componentResolver) componentRoot(files []domain.UsedFile) string {
	var common string
	for _, file := range files {
		path := r.physical[file.ID.Canonical()]
		if path == "" {
			continue
		}
		dir := filepath.Dir(path)
		if common == "" {
			common = dir
			continue
		}
		common = commonDirectory(common, dir)
	}
	return common
}

func commonDirectory(a, b string) string {
	aParts := strings.Split(filepath.Clean(a), string(filepath.Separator))
	bParts := strings.Split(filepath.Clean(b), string(filepath.Separator))
	var shared []string
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		if aParts[i] != bParts[i] {
			break
		}
		shared = append(shared, aParts[i])
	}
	joined := strings.Join(shared, string(filepath.Separator))
	if joined == "" {
		return string(filepath.Separator)
	}
	return joined
}

// purlFromAnchor asserts a package type from an anchor key, which is the only
// place a package type is currently known (section 20.4).
func purlFromAnchor(componentID string) (kind, name string, ok bool) {
	prefix, rest, found := strings.Cut(componentID, ":")
	if !found || prefix != "pkg" {
		return "", "", false
	}
	packageType, packageName, split := strings.Cut(rest, "/")
	if !split {
		return "", "", false
	}
	return packageType, packageName, true
}

// needsGitIntrospection reports whether any of the rules can only be answered
// by asking git, so that a missing version can name the permission it lacked.
func needsGitIntrospection(rules []string) bool {
	for _, rule := range rules {
		if rule == "git" || rule == "commit" {
			return true
		}
	}
	return false
}

func componentFinding(id string, severity domain.Severity, component *domain.Component, message, remediation string) domain.Finding {
	return domain.Finding{
		ID:          id,
		Severity:    severity,
		Subject:     domain.Subject{Kind: "component", Ref: component.ID},
		Message:     message,
		Remediation: remediation,
	}
}

func sortFindings(findings []domain.Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].ID != findings[j].ID {
			return findings[i].ID < findings[j].ID
		}
		if findings[i].Subject.Kind != findings[j].Subject.Kind {
			return findings[i].Subject.Kind < findings[j].Subject.Kind
		}
		return findings[i].Subject.Ref < findings[j].Subject.Ref
	})
}
