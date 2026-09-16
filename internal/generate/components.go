package generate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/example/sbomb/internal/adapters/pkgmanager"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/componentmap"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/limits"
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
	// The YAML an upstream writes instead, which internal/adapters/pkgmanager
	// reads on a settled root. Without it here a dependency whose only marker
	// is that file is never bounded as a component, so the reader never sees
	// the very document that would have described it.
	"sbom.yml",
}

// componentResolver maps used files onto components, following the priority
// order of section 19.2. Only strategies 1, 6, 7 and 8 exist so far; package
// managers, SDK layouts and submodule boundaries are later work (D7).
type componentResolver struct {
	curated     *componentmap.Mapper
	curatedByID map[string]config.Component
	// limits is the run's own bounds, used wherever this resolver opens a
	// file: section 30.5 refuses a symbolic link under --strict-symlinks and
	// the input ceiling refuses an oversized file before allocating for it.
	limits limits.Config
	// declaredIdentifiers is the SPDX expression each file stated about
	// itself, keyed by canonical identity (section 22.5).
	declaredIdentifiers map[string]string
	physical            map[string]string
	projectName         string
	anchorRoots         map[string]string
	logger              *Logger
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
	// licenseBoundaries is what licenseBoundaryIn answered for one directory,
	// keyed by that directory, with the empty string for a directory that
	// carries no licence grant. The upward walk of strategy 6 asks about the
	// same ancestor directories once per used file, and section 31 does not
	// pay for the same answer twice.
	licenseBoundaries map[string]string
	// packageManifests and bundledSBOMs are what the fixed-name markers of the
	// same walk answered for one directory, keyed by that directory, with the
	// empty string for a directory that carries none. They are memoized for
	// the reason licenseBoundaries above is: the upward walk of strategy 6
	// asks the same ancestor directories about the same twelve names once per
	// used file, and section 31 does not pay for the same answer twice.
	//
	// What is kept is the answer for a directory, not the outcome of a walk.
	// Whether a marker found here actually bounds a component depends on the
	// file the walk started from -- a licence or a bundled SBOM marks nothing
	// at the walker's own anchor root -- and a directory reached from two
	// anchors would otherwise be told the wrong thing by whichever file asked
	// first. The stat result belongs to the directory alone, so it is what the
	// memo holds; the anchor exception stays in the walk, where it can see
	// which anchor is asking.
	packageManifests map[string]string
	bundledSBOMs     map[string]string
	// licenseReads counts how often retention opened each path. It holds the
	// recognized licence files of the mapped component roots and nothing else,
	// so it is bounded by the retention limit per component rather than by the
	// used-file count. Reading is the only cost retention has, and a claim
	// about reads that nothing counts is not a claim.
	licenseReads map[string]int
	// retainedByRoot is what retention already read for one component root,
	// keyed by that root's identity and its physical directory. Two components
	// resolving to one root read its licence files once between them, which is
	// what makes the claim of section 22.9 -- one read per file per root --
	// hold without depending on how the roots were settled.
	retainedByRoot map[string]retainedLicenses
	// copyrights are the statements of section 22.10 that the hashing pass
	// observed, keyed by canonical file identity. They arrive from above
	// rather than being read here: extraction rides on the read the hash
	// already needed (decision Q9).
	copyrights map[string][]string
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

func newComponentResolver(cfg config.Config, physical map[string]string, anchorRoots map[string]string, bounds limits.Config, logger *Logger) *componentResolver {
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
		limits:          bounds,
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
	// A file that could not be read names no directory to walk from. The
	// evidence still records it and the document still carries it, but the
	// path it carries describes another machine: its parents exist here only by
	// coincidence, and a marker found in one of them belongs to whatever sits
	// at that path now. Section 19.2 settles a component boundary from a file
	// somebody put somewhere, so the walk starts from a file that is there.
	if file.Missing {
		return "", "", false
	}
	path := r.physical[file.ID.Canonical()]
	if path == "" {
		return "", "", false
	}
	boundary := r.anchorRoots[string(file.ID.Anchor)]
	dir := filepath.Dir(path)
	for depth := 0; depth < 64 && dir != "" && dir != "/" && dir != "."; depth++ {
		atBoundary := boundary != "" && filepath.Clean(dir) == filepath.Clean(boundary)
		if name, ok := r.packageManifestIn(dir); ok {
			return dir, name, true
		}
		// A licence file marks a boundary too, but never at the anchor root
		// itself: a project's own top-level licence describes the project, not
		// a dependency inside it, and treating it as a marker would rename the
		// project's own component after its directory. A bundled SBOM is the
		// same kind of statement under the same exception.
		if !atBoundary {
			if name, ok := r.licenseBoundaryIn(dir); ok {
				return dir, name, true
			}
			if name, ok := r.bundledSBOMIn(dir); ok {
				return dir, name, true
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

// packageManifestIn names the package manifest that makes dir the root of a
// distinct component, if it carries one, and bundledSBOMIn does the same for a
// bundled SBOM. Both answer from a memo, because the walk of nearestPackageRoot
// asks about the same ancestor directories once per used file below them and
// the answer depends on nothing but the directory.
//
// The two lists stay separate calls rather than one pass over both, so that a
// directory the walk may not take a licence or an SBOM from -- the anchor root
// of the file that is asking -- is never stat-ed for names that could not have
// bounded anything there.
func (r *componentResolver) packageManifestIn(dir string) (string, bool) {
	if cached, known := r.packageManifests[dir]; known {
		return cached, cached != ""
	}
	marker := firstFileIn(dir, packageMetadataFiles)
	if r.packageManifests == nil {
		r.packageManifests = map[string]string{}
	}
	r.packageManifests[dir] = marker
	return marker, marker != ""
}

func (r *componentResolver) bundledSBOMIn(dir string) (string, bool) {
	if cached, known := r.bundledSBOMs[dir]; known {
		return cached, cached != ""
	}
	marker := firstFileIn(dir, bundledSBOMFiles)
	if r.bundledSBOMs == nil {
		r.bundledSBOMs = map[string]string{}
	}
	r.bundledSBOMs[dir] = marker
	return marker, marker != ""
}

// firstFileIn names the first of names that exists in dir as something other
// than a directory, in the order given, which is the order the marker lists
// state their precedence in. It stats rather than lists the directory: the
// names are fixed, so a stat each answers what a listing would, and it follows
// a symbolic link the way the licence match deliberately does.
func firstFileIn(dir string, names []string) string {
	for _, name := range names {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return name
		}
	}
	return ""
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
	// CPE (section 20.4): only from an origin that states it outright. A
	// curated value is the operator's, and it settles the question -- an
	// upstream's cpe can be wrong about its own product, and this is the way
	// to say so.
	if hasCurated && curated.CPE != "" {
		component.CPE = cpeVersion(curated.CPE, component.Version)
	} else if isManaged && managed.CPE.Value != "" {
		component.CPE = cpeVersion(managed.CPE.Value, component.Version)
	} else if described.CPE.Value != "" {
		component.CPE = cpeVersion(described.CPE.Value, component.Version)
	}

	metadata := described
	if isManaged {
		metadata = managed
	}
	component.Originator = metadata.Originator.Value
	component.Description = metadata.Description.Value
	for _, exclusion := range metadata.CVEExclusions {
		component.CVEExclusions = append(component.CVEExclusions, domain.CVEExclusion{CVE: exclusion.CVE, Reason: exclusion.Reason})
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
	licenseFindings := r.resolveComponentLicense(component, files, curated, hasCurated, rootInfo, described.License)
	findings = append(findings, licenseFindings...)

	// Copyright statements (section 22.10). After the licences, because the
	// artifacts section 22.9 retained are one of the two sources: for MIT and
	// BSD the holder is inside the licence text.
	findings = append(findings, r.resolveComponentCopyright(component, files, curated, hasCurated)...)

	// Modification status (section 19.4), tri-state. It needs the component
	// root, which is why it comes after it is settled.
	findings = append(findings, r.resolveModification(component, rootInfo, declaredRevisionOf(managed, isManaged))...)

	// What the licence asks for beyond attribution (section 32.6). After the
	// licences, because it classifies the expression they resolved, and after
	// the attributes of section 24.5, because a build-time-only component
	// triggered nothing.
	findings = append(findings, resolveSourceObligations(component)...)

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

// cpeVersion fills the {} a cpe leaves in place of the version. A placeholder
// that cannot be filled takes the cpe with it: "{}" is no version, and a
// consumer matching the string against a vulnerability feed would find nothing
// while the document reads as though a version had been stated.
func cpeVersion(cpe, version string) string {
	if cpe == "" {
		return ""
	}
	if !strings.Contains(cpe, "{}") {
		return cpe
	}
	if version == "" {
		return ""
	}
	return strings.Replace(cpe, "{}", version, 1)
}

// resolveComponentLicense applies the priority order of section 22.2, limited
// to the sources that exist today: curated configuration, an SPDX identifier
// in a used file, what a reader found in the component root, what the package
// manager stated, and a recognized license file in that root.
func (r *componentResolver) resolveComponentLicense(component *domain.Component, files []domain.UsedFile, curated config.Component, hasCurated bool, rootInfo componentRootResult, described pkgmanager.Claim) []domain.Finding {
	findings := []domain.Finding{}

	// Retention first, and independently of what decides the identifier
	// (section 22.9). The bytes are the deliverable, so they are kept whether
	// the identifier came from a header, a manifest or one of these files --
	// and a component whose header says MIT still owes its recipients the
	// text. Identification below reads no file again: it consumes this.
	retained := r.retainLicenseArtifacts(rootInfo.ID, rootInfo.Physical)
	component.LicenseArtifacts = sortedLicenseArtifacts(retained.artifacts)
	for _, artifact := range component.LicenseArtifacts {
		name := "sbomb:component:licenseFile"
		if artifact.Kind != domain.LicenseArtifactLicense {
			// A NOTICE and a COPYRIGHT are the same obligation -- attribution
			// material to reproduce -- and the canonical path in the value
			// says which of the two a given entry is.
			name = "sbomb:component:noticeFile"
		}
		component.Properties = addProperty(component.Properties, name,
			artifact.File.Canonical()+"@sha256:"+artifact.SHA256)
	}
	if len(retained.dropped) > 0 {
		findings = append(findings, componentFinding("FOSS_LICENSE_ARTIFACT_LIMIT", domain.SeverityInfo, component,
			fmt.Sprintf("the component root carries more recognized licence files than section 22.9 retains, or a larger one: %s",
				strings.Join(retained.dropped, ", ")),
			"Nothing was truncated. Reduce the licence files in the component root, or retain the named file by hand."))
	}

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
		// The identity of the file, never the path it was read from. Where the
		// bytes are is a property of this machine -- and since section 7.9 of
		// which directory --source-dir named -- while the document states what
		// was read, in canonical form (section 7.8, appendix B).
		ref := file.ID.Canonical()
		// Both questions below are asked of these same bytes -- what licence
		// is this, and which licence texts are in it -- and both compare
		// against the same normalized form of them. Preparing the text once
		// means that form is computed once instead of once per question, and
		// the bytes become a string once instead of twice.
		examined := license.Prepare(string(data))
		if found := examined.Resolve(ref); found.Expression != "" {
			fromFiles = found
			observed = nil
			break
		}
		if len(observed) == 0 {
			observed = examined.ObserveFindings(ref)
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
		if found, ok := licenseFromRetained(retained); ok {
			fromFiles = found
		}
	}
	if fromFiles.Expression == "" && len(observed) == 0 {
		observed = licenseEvidenceFromRetained(retained)
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
	findings = append(findings, r.licenseCompletenessFindings(component)...)
	findings = append(findings, r.divergenceFindings(component, files)...)
	return findings
}

// divergenceFindings reports the identifiers this component's files declared
// that its own resolved licence does not account for (section 22.5).
//
// Two sources disagreeing about one licence is a conflict and is reported as
// one. Files declaring different identifiers is not that: a directory
// assembled from two upstreams carries two licences, and both readings are
// right. What makes it worth saying is only the part the component did not
// state -- a dependency that resolved to "MIT OR Apache-2.0" whose files
// declare MIT and Apache-2.0 has said what it is, and repeating it would put
// an info finding on every dual-licensed dependency there is.
func (r *componentResolver) divergenceFindings(component *domain.Component, files []domain.UsedFile) []domain.Finding {
	if len(r.declaredIdentifiers) == 0 || len(component.Licenses) == 0 {
		return nil
	}
	resolved := component.Licenses[0].Expression
	if resolved == "" {
		// Nothing was resolved, and section 22.7 already says so with a reason
		// code. Listing what the files declared beside it would be a second
		// answer to a question that was answered "none".
		return nil
	}
	accounted := map[string]bool{}
	for _, id := range license.IdentifiersIn(resolved) {
		accounted[id] = true
	}
	unaccounted := map[string]bool{}
	for _, file := range files {
		declared := r.declaredIdentifiers[file.ID.Canonical()]
		if declared == "" {
			continue
		}
		for _, id := range license.IdentifiersIn(declared) {
			if !accounted[id] {
				unaccounted[id] = true
			}
		}
	}
	if len(unaccounted) == 0 {
		return nil
	}
	names := make([]string, 0, len(unaccounted))
	for id := range unaccounted {
		names = append(names, id)
	}
	sort.Strings(names)
	return []domain.Finding{componentFinding("FOSS_PER_FILE_LICENSE_DIVERGENCE", domain.SeverityInfo, component,
		fmt.Sprintf("the component resolved to %s, and its files also declare %s",
			resolved, strings.Join(names, ", ")),
		"Check whether the component is one work or two; where it is two, map them with components[] so each is described under its own licence.")}
}

// licenseCompletenessFindings is requirement R10 for the licence text: where a
// component's licence was settled and its own text was not retained, the
// document says so rather than leaving a reader to notice.
func (r *componentResolver) licenseCompletenessFindings(component *domain.Component) []domain.Finding {
	// Section 22.9 and requirement R10: an identifier without the text is the
	// one case where the attribution obligation cannot be satisfied from what
	// the tool saw, and it is said at the point where the entry would have
	// been. No canonical SPDX text is ever put there instead -- for MIT and
	// the BSD family that text carries a placeholder where the rights holder
	// belongs, so substituting it ships a template where a notice was owed.
	//
	// The question is asked of a distributed component alone (section 24.5,
	// open question Q11). A build-time-only code generator with a licence
	// identifier and no retained text is not an attribution gap: nothing of
	// it is shipped, so there is no notice to reproduce. Until the role was a
	// resolved fact this could not be narrowed without guessing, and the
	// finding over-reported on purpose.
	// And it is asked only where there was somewhere to look. A component the
	// pkg-config metadata named has no component root at all (section 19.2,
	// strategy 6a): a .pc file names where a package installed its libraries,
	// not a directory the component owns, and section 22.1 forbids searching a
	// sysroot for licence files. Reporting "no licence text was retained"
	// against a root nobody was allowed to read would put the finding on every
	// system library of every distribution build -- and those are distributed,
	// so the role does not narrow it.
	if component.Root != nil &&
		component.DistributionRole == domain.RoleDistributed &&
		hasResolvedLicense(component.Licenses) && !hasRetainedGrant(component.LicenseArtifacts) {
		return []domain.Finding{componentFinding("FOSS_LICENSE_TEXT_MISSING", domain.SeverityInfo, component,
			fmt.Sprintf("the licence is %s, and no licence text was retained for this component",
				renderedLicense(component.Licenses[0])),
			"Place the component's own licence file in its root, or record the text with the component by hand; sbomb never substitutes a canonical SPDX text.")}
	}
	return nil

}

// hasResolvedLicense says whether a licence was actually settled, as opposed
// to the NOASSERTION of section 22.7, which is the tool saying it does not
// know.
func hasResolvedLicense(licenses []domain.LicenseFinding) bool {
	if len(licenses) == 0 {
		return false
	}
	return licenses[0].Expression != "" || licenses[0].SPDXID != "" ||
		(licenses[0].Name != "" && licenses[0].Name != "NOASSERTION")
}

// hasRetainedGrant says whether the component carries the bytes of a licence
// grant. A NOTICE alone does not satisfy the obligation the grant states, so
// it does not answer this.
func hasRetainedGrant(artifacts []domain.LicenseArtifact) bool {
	for _, artifact := range artifacts {
		if artifact.Kind == domain.LicenseArtifactLicense {
			return true
		}
	}
	return false
}

// renderedLicense names a licence finding the way a message should: the
// expression, else the identifier, else the name.
func renderedLicense(finding domain.LicenseFinding) string {
	switch {
	case finding.Expression != "":
		return finding.Expression
	case finding.SPDXID != "":
		return finding.SPDXID
	default:
		return finding.Name
	}
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
	// The file name, not the path it was read from. ResolveFile names the path
	// it opened, which is where a package cache happens to sit on this machine
	// -- section 7.8 keeps such a path out of the document, and publishing it
	// as sbomb:license:source made two runs on two machines produce two
	// documents from one build. licenseFromRetained says the same thing about
	// the same field a few lines below.
	found.Source = filepath.Base(managed.LicenseFile)
	found.Evidence = "component-level"
	return found, true
}

// Retention limits of section 22.9, which are the section 30 point 9 bounds on
// untrusted input applied to a licence file. The count bounds the list and the
// size bounds one entry; neither ever truncates a retained file, because a
// truncated licence is not a licence and a truncated notice is not a notice.
const (
	maxLicenseArtifacts     = 8
	maxLicenseArtifactBytes = 1 << 20
)

// retainedLicenses is what section 22.9 kept for one component root, in the
// consultation order of section 22.3 -- grants first -- because that order
// decides which file answers step 5 of section 22.2 and it is not the order
// the artifacts are stored in.
type retainedLicenses struct {
	artifacts []domain.LicenseArtifact
	// detected is index-aligned with artifacts and holds the whole finding
	// detection produced, of which domain.LicenseArtifact keeps the
	// identifier and the technique. Carrying it here is what lets step 5 of
	// section 22.2 answer from the retained bytes instead of reading and
	// detecting a second time.
	detected []domain.LicenseFinding
	// dropped names what a limit excluded, for FOSS_LICENSE_ARTIFACT_LIMIT.
	dropped []string
	// unretained and unretainedObserved are what a file too large to keep
	// still stated. The bound of section 30 is on the bytes this document
	// carries, not on what the tool may conclude: before retention existed,
	// step 5 of section 22.2 read a licence file of any size, and a bound on
	// keeping must not turn a resolved licence into NOASSERTION.
	unretained         domain.LicenseFinding
	unretainedObserved []domain.LicenseFinding
}

// retainLicenseArtifacts keeps the bytes of every recognized licence file the
// component root carries (section 22.9).
//
// An SPDX identifier is not a deliverable. MIT and the BSD family name the
// rights holder inside the licence text, and the text SPDX publishes for MIT
// has a placeholder where that holder belongs -- so the component's own file
// is what an attribution obligation is satisfied with, and the identifier is
// an index into a catalogue. Nothing here is ever substituted for a text a
// component does not carry.
//
// Section 22.1 still bounds where this may look: the settled component root
// itself, listed once, and nothing above or below it.
func (r *componentResolver) retainLicenseArtifacts(rootID domain.FileID, root string) retainedLicenses {
	if root == "" {
		return retainedLicenses{}
	}
	key := rootID.Canonical() + "|" + root
	if cached, known := r.retainedByRoot[key]; known {
		return cached
	}
	var retained retainedLicenses
	for _, name := range licenseFilesIn(root) {
		if len(retained.artifacts) >= maxLicenseArtifacts {
			retained.dropped = append(retained.dropped,
				fmt.Sprintf("%s (past the limit of %d artifacts)", name, maxLicenseArtifacts))
			continue
		}
		path := filepath.Join(root, name)
		// The size is asked before the bytes are, so that a licence file the
		// size of a disk image is refused rather than allocated for. The run's
		// own ceiling still applies below and is what bounds the read.
		if info, err := r.limits.Stat(path); err == nil && info.Size() > maxLicenseArtifactBytes {
			retained.dropped = append(retained.dropped,
				fmt.Sprintf("%s (%d bytes, over the limit of %d)", name, info.Size(), maxLicenseArtifactBytes))
			r.identifyUnretained(&retained, path, name)
			continue
		}
		data, err := r.readForLicense(path)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		artifact := domain.LicenseArtifact{
			Kind:   licenseArtifactKind(name),
			File:   domain.FileID{Anchor: rootID.Anchor, RelPath: joinRelPath(rootID.RelPath, name)},
			SHA256: hex.EncodeToString(sum[:]),
			Bytes:  data,
		}
		found := domain.LicenseFinding{}
		// Only a grant is detected on. A NOTICE quoting a licence is evidence
		// of what must be reproduced and not of what applies, and an
		// identifier recorded beside its bytes is one inference away from
		// becoming the component's licence (section 22.2).
		if artifact.Kind == domain.LicenseArtifactLicense {
			found = license.ResolveFromText(string(data), name)
			if found.Expression != "" {
				artifact.DetectedID = found.Expression
				artifact.Technique = found.Technique
			}
		}
		retained.artifacts = append(retained.artifacts, artifact)
		retained.detected = append(retained.detected, found)
	}
	if r.retainedByRoot == nil {
		r.retainedByRoot = map[string]retainedLicenses{}
	}
	r.retainedByRoot[key] = retained
	return retained
}

// readForLicense reads one licence file of a component root and counts the
// read. Section 22.9 permits one read per file per root; the counter is how
// that is checked rather than promised. A failed open counts too: the point is
// which paths this run touches.
func (r *componentResolver) readForLicense(path string) ([]byte, error) {
	if r.licenseReads == nil {
		r.licenseReads = map[string]int{}
	}
	r.licenseReads[path]++
	// Through the run's limits, not around them: section 30.5 refuses a
	// symbolic link in the final component under --strict-symlinks, and the
	// input ceiling refuses a file before allocating for it. A LICENSE that is
	// a link to somewhere else, or a FIFO that os.Stat reports as empty, would
	// otherwise be read and its digest published on every run.
	return r.limits.ReadFile(path)
}

// identifyUnretained asks a licence file that is too large to keep what it
// states. Section 22.2 does not know about retention, and a component whose
// LICENSE is a concatenation of bundled texts resolved before section 22.9
// existed: letting the retention bound answer NOASSERTION would make a limit on
// what the document carries into a statement about the component.
//
// The bytes are released as soon as the question is answered. Only a grant is
// asked, for the reason section 22.2 gives: a NOTICE that quotes a licence is
// not evidence of what applies.
func (r *componentResolver) identifyUnretained(retained *retainedLicenses, path, name string) {
	if licenseArtifactKind(name) != domain.LicenseArtifactLicense {
		return
	}
	data, err := r.readForLicense(path)
	if err != nil {
		return
	}
	// Which of the two questions are still open is settled before the bytes
	// are touched. Both are then asked of one prepared text, so the normal
	// form they share is computed once -- and a file with neither question
	// left is not converted to a string at all, which matters here because
	// this is the path for a licence file too large to keep.
	identify, observe := retained.unretained.Expression == "", len(retained.unretainedObserved) == 0
	if !identify && !observe {
		return
	}
	examined := license.Prepare(string(data))
	if identify {
		if found := examined.Resolve(name); found.Expression != "" {
			found.Evidence = "component-level"
			found.Source = name
			retained.unretained = found
		}
	}
	if observe {
		retained.unretainedObserved = examined.ObserveFindings(name)
	}
}

// licenseArtifactKind maps a recognized file name onto the kind of obligation
// it carries (section 22.9).
func licenseArtifactKind(name string) string {
	rank, ok := recognizedLicenseFile(name)
	if !ok {
		return ""
	}
	switch rank {
	case licenseRankNotice:
		return domain.LicenseArtifactNotice
	case licenseRankCopyright:
		return domain.LicenseArtifactCopyright
	}
	return domain.LicenseArtifactLicense
}

// joinRelPath puts a file name below a root's relative path. A root that is
// the anchor itself has none, and "/name" is not an identity.
func joinRelPath(root, name string) string {
	if root == "" {
		return name
	}
	return strings.TrimSuffix(root, "/") + "/" + name
}

// sortedLicenseArtifacts is the stored order of section 22.9: (kind, canonical
// path). It is deliberately not the consultation order of section 22.3 -- a
// document is read, and a resolution order is executed.
func sortedLicenseArtifacts(artifacts []domain.LicenseArtifact) []domain.LicenseArtifact {
	if len(artifacts) == 0 {
		return nil
	}
	sorted := make([]domain.LicenseArtifact, len(artifacts))
	copy(sorted, artifacts)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Kind != sorted[j].Kind {
			return sorted[i].Kind < sorted[j].Kind
		}
		return sorted[i].File.Canonical() < sorted[j].File.Canonical()
	})
	return sorted
}

// licenseFromRetained is step 5 of section 22.2, answered from the bytes
// section 22.9 already kept. Only a grant answers it, and the first one that
// detection settled -- in the order of section 22.3 -- wins.
func licenseFromRetained(retained retainedLicenses) (domain.LicenseFinding, bool) {
	for i, artifact := range retained.artifacts {
		if artifact.Kind != domain.LicenseArtifactLicense || artifact.DetectedID == "" {
			continue
		}
		found := retained.detected[i]
		found.Evidence = "component-level"
		// The file name, not the path it was read from: where the bytes are is
		// a property of this machine (section 7.9), and the identity of the
		// artifact is recorded beside its hash in sbomb:component:licenseFile.
		found.Source = baseNameOf(artifact.File.RelPath)
		return found, true
	}
	if retained.unretained.Expression != "" {
		return retained.unretained, true
	}
	return domain.LicenseFinding{}, false
}

// licenseEvidenceFromRetained observes the complete licence texts in a grant
// that is not itself one licence -- two of them one after the other, or one
// with material around it. It is evidence (section 22.4), never a conclusion.
//
// A NOTICE is not consulted: it is retained for reproduction and left out of
// the identification chain entirely (section 22.2), so a NOTICE reciting the
// Apache licence cannot make the component's licence NOASSERTION-with-review
// either.
func licenseEvidenceFromRetained(retained retainedLicenses) []domain.LicenseFinding {
	for _, artifact := range retained.artifacts {
		if artifact.Kind != domain.LicenseArtifactLicense {
			continue
		}
		if observed := license.ObserveFindings(string(artifact.Bytes), baseNameOf(artifact.File.RelPath)); len(observed) > 0 {
			return observed
		}
	}
	return retained.unretainedObserved
}

// baseNameOf is the last segment of a canonical relative path, which is always
// slash-separated (section 7.7).
func baseNameOf(relPath string) string {
	if index := strings.LastIndex(relPath, "/"); index >= 0 {
		return relPath[index+1:]
	}
	return relPath
}

// licenseNames lists the identifiers of a set of observations, for a message.
func licenseNames(findings []domain.LicenseFinding) []string {
	names := make([]string, 0, len(findings))
	for _, finding := range findings {
		names = append(names, finding.Expression)
	}
	return names
}

// The licence file names of section 22.3 are recognized case-insensitively,
// with an optional .txt or .md extension, and LICENSE-<id> is recognized as a
// family rather than as a name -- that is how a component carrying LICENSE-MIT
// and LICENSE-APACHE side by side says it holds both. Neither rule can be
// expressed as a fixed list of paths to stat, so a component root is listed
// once and its entries are matched instead.
//
// That is affordable here and nowhere else. A component root is read once per
// component, after something else settled it, while the boundary markers of
// section 19.2 are stat-ed once per used file per ancestor directory and a
// directory listing there would be paid tens of thousands of times over
// against the budget of section 31.
//
// LICENCE-<id> is deliberately absent: section 22.3 names the British spelling
// as a whole file name and the -<id> form only for LICENSE.
const (
	licenseRankLicense = iota
	licenseRankLicenseID
	licenseRankCopying
	licenseRankNotice
	licenseRankCopyright
)

// recognizedLicenseFile says whether a directory entry is a licence file of
// section 22.3, and ranks it. The rank orders the candidates a single root
// carries, so that a component holding both a LICENSE and a NOTICE is decided
// by its licence rather than by whichever name the filesystem returned first.
// LICENSE and LICENCE share a rank: a spelling is not a statement.
func recognizedLicenseFile(name string) (int, bool) {
	// Every recognized name -- LICENSE, LICENCE, COPYING, NOTICE, COPYRIGHT
	// and the LICENSE-<id> family -- begins with L, C or N whatever its case,
	// and stripping an extension never changes the first byte. Deciding that
	// here keeps the upper-casing below off every other file in the
	// directory: a component root of twelve thousand entries reached it once
	// per entry and allocated for each.
	//
	// Only an ASCII first byte is answered this way. A leading byte outside
	// ASCII falls through to the full path, so the cheap answer is never a
	// different answer.
	if name == "" {
		return 0, false
	}
	if first := name[0]; first < utf8.RuneSelf {
		switch first {
		case 'L', 'l', 'C', 'c', 'N', 'n':
		default:
			return 0, false
		}
	}

	stem := name
	if ext := filepath.Ext(stem); strings.EqualFold(ext, ".txt") || strings.EqualFold(ext, ".md") {
		stem = strings.TrimSuffix(stem, ext)
	}
	upper := strings.ToUpper(stem)
	switch upper {
	case "LICENSE", "LICENCE":
		return licenseRankLicense, true
	case "COPYING":
		return licenseRankCopying, true
	case "NOTICE":
		return licenseRankNotice, true
	case "COPYRIGHT":
		return licenseRankCopyright, true
	}
	if strings.HasPrefix(upper, "LICENSE-") && len(upper) > len("LICENSE-") {
		// `LICENSE-<id>` is a licence file; `license-header.txt`,
		// `license-check.py` and `LICENSE-scanner.sh` are the tooling a
		// licence *checker* ships, and they begin with the same eight
		// characters. Only .txt and .md are recognized extensions, so a stem
		// that still ends in one after those were stripped is a file of some
		// other kind -- and section 19.2 would otherwise make its directory a
		// component of its own and section 22.9 publish the script as that
		// component's licence text.
		//
		// Two questions, because one does not settle it. A final dot-segment
		// of letters alone is an extension rather than a version, which
		// excludes `license-check.py`. And what follows the dash has to name a
		// licence -- `MIT` is one, `APACHE` opens `Apache-2.0`, and `HEADER`
		// opens nothing -- which is what excludes the `license-header.txt` a
		// licence-header checker ships.
		if !endsInAnUnrecognizedExtension(stem) && license.NamesALicence(stem[len("LICENSE-"):]) {
			return licenseRankLicenseID, true
		}
	}
	return 0, false
}

// licenseGrant says whether a rank of section 22.3 belongs to a file that
// grants a licence, as opposed to one that reproduces attribution material.
// Only a grant marks a component boundary: a directory that carries its own
// licence is a distinct work by convention, while a directory that carries
// only a NOTICE or a COPYRIGHT is not thereby a separate work.
func licenseGrant(rank int) bool {
	switch rank {
	case licenseRankLicense, licenseRankLicenseID, licenseRankCopying:
		return true
	}
	return false
}

// licenseBoundaryIn names the licence file that makes dir the root of a
// distinct component, if it carries one. A library that was simply copied into
// the source tree usually has nothing else: no conanfile, no vcpkg.json, and a
// CMakeLists.txt that cannot be read without interpreting CMake.
//
// The names are the ones section 22.3 recognizes, matched the way that section
// matches them -- case-insensitively, with an optional .txt or .md, and
// LICENSE-<id> as a family -- rather than as a list of exact names to stat. A
// component carrying LICENSE-MIT and LICENSE-APACHE side by side carries no
// file called LICENSE at all, and a fixed list left it invisible: its sources
// dissolved into whatever enclosed them, and the attribution export named the
// enclosing project as the holder of both texts.
//
// Matching a name requires listing the directory, which section 31 would
// otherwise pay for once per used file per ancestor directory. It is paid once
// per directory instead, because the answer is cached: the walk asks about the
// same ancestors for every file under them, so the listing is amortized to
// fewer reads than the nine stats it replaces. The fixed-name stat lists that
// remain -- package manifests and bundled SBOMs -- keep their form, because
// neither has a matching rule that a name alone cannot express.
func (r *componentResolver) licenseBoundaryIn(dir string) (string, bool) {
	if cached, known := r.licenseBoundaries[dir]; known {
		return cached, cached != ""
	}
	marker := ""
	for _, name := range licenseFilesIn(dir) {
		if rank, ok := recognizedLicenseFile(name); ok && licenseGrant(rank) {
			marker = name
			break
		}
	}
	if r.licenseBoundaries == nil {
		r.licenseBoundaries = map[string]string{}
	}
	r.licenseBoundaries[dir] = marker
	return marker, marker != ""
}

// endsInAnUnrecognizedExtension reports whether what is left of a name after
// the recognized extensions were stripped still carries one. Section 22.3
// recognizes .txt and .md and nothing else, so anything else after the last dot
// says the file is not the licence it is named after.
func endsInAnUnrecognizedExtension(stem string) bool {
	dot := strings.LastIndexByte(stem, '.')
	if dot < 0 || dot == len(stem)-1 {
		return false
	}
	for _, r := range stem[dot+1:] {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// statForLicenseFiles is the fallback for a directory that cannot be listed. It
// asks for the fixed names of section 22.3 and nothing else: the LICENSE-<id>
// form is a family and cannot be enumerated, so a root that carries only one of
// those and refuses a listing is not recognized -- which is a narrower loss
// than losing the root altogether.
func statForLicenseFiles(root string) []string {
	var names []string
	for _, name := range []string{
		"LICENSE", "LICENSE.txt", "LICENSE.md",
		"LICENCE", "LICENCE.txt", "LICENCE.md",
		"COPYING", "COPYING.txt", "COPYING.md",
		"NOTICE", "COPYRIGHT",
	} {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && !info.IsDir() {
			names = append(names, name)
		}
	}
	sort.SliceStable(names, func(i, j int) bool {
		left, _ := recognizedLicenseFile(names[i])
		right, _ := recognizedLicenseFile(names[j])
		if left != right {
			return left < right
		}
		return names[i] < names[j]
	})
	return names
}

// licenseFilesIn lists the recognized licence files a component root carries,
// in the order they are to be consulted: by rank, then by name. A directory is
// read in no defined order, and section 29 requires the same evidence to
// produce the same document, so the order is imposed here rather than
// inherited from the filesystem.
func licenseFilesIn(root string) []string {
	if root == "" {
		return nil
	}
	// Read without os.ReadDir's sort. It orders every name in the directory,
	// and a component root can hold twelve thousand of them, while what is
	// kept here is a handful that the rank-then-name sort below puts in order
	// anyway. That sort settles the result completely -- a directory cannot
	// hold two entries of the same name -- so the order the filesystem hands
	// the names over in does not reach it.
	directory, err := os.Open(root)
	var entries []os.DirEntry
	if err == nil {
		entries, err = directory.ReadDir(-1)
		directory.Close()
	}
	if err != nil {
		// A directory can be searchable and not listable -- mode 0711 is
		// ordinary for a vendored tree unpacked under a restrictive umask --
		// and a stat of a child needs only the search bit. Falling back to
		// asking for the names of section 22.3 keeps such a root a component
		// instead of dissolving it into whatever encloses it, which is the
		// failure this listing was introduced to fix rather than to cause.
		return statForLicenseFiles(root)
	}
	type candidate struct {
		rank int
		name string
	}
	candidates := make([]candidate, 0, 4)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// os.Stat followed a symbolic link and DirEntry.Type does not, so a
		// LICENSE that links to a directory, or one whose target is gone --
		// which is what a harvested or relocated tree leaves behind (section
		// 7.9) -- would mark a component boundary here and then be unreadable
		// at its root. The link is followed for the same answer stat gave.
		if entry.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(root, entry.Name()))
			if err != nil || info.IsDir() {
				continue
			}
		}
		if rank, ok := recognizedLicenseFile(entry.Name()); ok {
			candidates = append(candidates, candidate{rank: rank, name: entry.Name()})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return candidates[i].name < candidates[j].name
	})
	names := make([]string, 0, len(candidates))
	for _, found := range candidates {
		names = append(names, found.name)
	}
	return names
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
		// Same reason as in nearestPackageRoot: a file that is not there
		// contributes no directory. Including it would pull the common
		// directory towards a path that exists on another machine, and the
		// result is published as sbomb:component:root and read from for a
		// licence.
		if file.Missing {
			continue
		}
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
