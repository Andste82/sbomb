// Package anchors assembles the anchor registry from the evidence a build
// directory offers, in the registration order of specification section 7.4,
// and classifies files by the anchor they resolved to.
//
// It sits above both the path model and the adapters: the path model knows how
// to match a path against registered roots, and the adapters know where those
// roots are, but only this package knows the priority between them.
package anchors

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/pathmodel"
)

// Scope is the origin category a file belongs to, derived from its anchor.
// It drives the inclusion defaults of section 24.1 and becomes the
// sbomb:component:scope property of section 24.2.
type Scope string

const (
	ScopeProject    Scope = "project"
	ScopeBuild      Scope = "build"
	ScopeThirdParty Scope = "third-party"
	ScopeSDK        Scope = "sdk"
	ScopeToolchain  Scope = "toolchain"
	ScopeSystem     Scope = "system"
	ScopeUnknown    Scope = "unknown"
)

// Options describes the evidence available for anchor assembly.
type Options struct {
	Flavor pathmodel.Flavor

	// ProjectRoot and BuildRoot come from configuration or the CLI and take
	// precedence over what the File API reports.
	ProjectRoot string
	BuildRoot   string

	// ConfigAnchors are the explicit anchors of the configuration file. They
	// are the highest authority for naming (section 7.4 step 2).
	ConfigAnchors []config.Anchor

	// Model is the CMake File API reply, when one could be read. It supplies
	// the source and build roots, the toolchain installation root and the
	// sysroot.
	Model *cmakeapi.Model

	// Packages are the roots the package-manager adapters proved, registered
	// under their own anchor keys (section 21). Without them the files of an
	// installed dependency keep the absolute path of a package cache, which is
	// neither portable nor stable between machines.
	Packages []PackageAnchor

	// CompileFlags are compiler command-line fragments, scanned for --sysroot.
	CompileFlags []string

	// Runner and Ctx are the run's introspection gateway. They are used only
	// when the File API reported no toolchain at all: with a reply in hand the
	// answer is already there and no process is started (section 9.2).
	Runner *exec.Runner
	Ctx    context.Context

	// Compilers are the compiler executables the build evidence named. They
	// are what the compiler probes may be run against; nothing else is.
	Compilers []string

	// Redact replaces unanchored paths with a digest (section 7.5).
	Redact bool
}

// PackageAnchor is one package root and the key it is registered under.
type PackageAnchor struct {
	Key  string
	Root string
}

// Result is the assembled registry plus what classification needs.
type Result struct {
	Registry *pathmodel.Registry

	// ImplicitIncludeDirs are the compiler's own system include directories as
	// reported by toolchains-v1. Section 14.4 requires classification to use
	// these rather than a hardcoded list of paths.
	ImplicitIncludeDirs []string

	// ImplicitLinkDirs are the directories the linker searches by default.
	// Section 24.1 treats what lies under them as distribution libraries, a
	// separate category from system headers.
	ImplicitLinkDirs []string

	Findings []domain.Finding
}

// Assemble registers anchors in the order of section 7.4. Later sources cannot
// take over a directory an earlier source already claimed.
func Assemble(options Options) (*Result, error) {
	flavor := options.Flavor
	if flavor == nil {
		flavor = pathmodel.DefaultFlavor()
	}
	registry := pathmodel.NewRegistry(flavor)
	registry.SetRedactUnanchored(options.Redact)
	result := &Result{Registry: registry, Findings: []domain.Finding{}}

	// 1. project and build, from configuration or the CLI, falling back to the
	//    roots the File API reports.
	projectRoot := options.ProjectRoot
	buildRoot := options.BuildRoot
	if options.Model != nil {
		if projectRoot == "" || projectRoot == "." {
			projectRoot = options.Model.SourceRoot
		}
		if buildRoot == "" {
			buildRoot = options.Model.BuildRoot
		}
	}
	if _, err := registry.Register("project", projectRoot, "configuration"); err != nil {
		return nil, err
	}
	if _, err := registry.Register("build", buildRoot, "configuration"); err != nil {
		return nil, err
	}

	// 2. Explicit anchors from the configuration.
	for _, anchor := range options.ConfigAnchors {
		key := NormalizeConfigKey(anchor.Key)
		if err := pathmodel.ValidateAnchorKey(key); err != nil {
			return nil, err
		}
		if _, err := registry.Register(key, anchor.Path, "configuration anchors[]"); err != nil {
			return nil, err
		}
	}

	// 3. and 4. Package-manager and SDK adapters. They come after the explicit
	//    configuration, which stays the highest authority for naming, and
	//    before toolchain probing.
	for _, pkg := range sortedPackages(options.Packages) {
		if pkg.Key == "" || pkg.Root == "" {
			continue
		}
		if err := pathmodel.ValidateAnchorKey(pkg.Key); err != nil {
			return nil, err
		}
		if _, err := registry.Register(pkg.Key, pkg.Root, "package manager"); err != nil {
			return nil, err
		}
	}

	// 5. Toolchain installation roots.
	registeredToolchains := 0
	if options.Model != nil {
		for _, toolchain := range sortedToolchains(options.Model.Toolchains) {
			root := cmakeapi.ToolchainRoot(toolchain.CompilerPath)
			if root == "" {
				continue
			}
			key := "toolchain:" + cmakeapi.ToolchainID(toolchain)
			// Two languages of one compiler share an installation root; the
			// registry keeps the first and reports the duplicate as skipped.
			if _, err := registry.Register(key, root, "toolchains-v1"); err != nil {
				continue
			}
			registeredToolchains++
			result.ImplicitIncludeDirs = append(result.ImplicitIncludeDirs, toolchain.ImplicitIncludeDirs...)
			result.ImplicitLinkDirs = append(result.ImplicitLinkDirs, toolchain.ImplicitLinkDirs...)
		}
	}

	// 5b. With no toolchains-v1 reply, the compiler itself can be asked where
	//     it is installed and which directories it hands the linker (section
	//     9.2). This runs only when the reply said nothing: one toolchain from
	//     the File API and no process is started.
	//
	//     It answers less than the reply does. -print-search-dirs reports the
	//     installation, the program and the library directories, and no
	//     include directories at all, so ImplicitIncludeDirs stays empty and
	//     TOOLCHAIN_LAYOUT_UNKNOWN below still fires. What it does supply is
	//     the link directories section 24.1 recognises distribution libraries
	//     by, and a toolchain anchor for the compiler's own files.
	if registeredToolchains == 0 && options.Runner != nil && options.Runner.Features.Compiler {
		ctx := options.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		for _, compiler := range uniqueCompilers(options.Compilers) {
			probe, ok := probeCompiler(ctx, options.Runner, compiler)
			if !ok {
				continue
			}
			key := probe.anchorKey()
			if err := pathmodel.ValidateAnchorKey(key); err != nil {
				continue
			}
			// Two languages of one compiler installation share a root; the
			// registry keeps the first and reports the duplicate as skipped.
			if _, err := registry.Register(key, probe.Root, "compiler probe"); err != nil {
				continue
			}
			result.ImplicitLinkDirs = append(result.ImplicitLinkDirs, probe.LinkDirs...)
		}
	}

	// 6. Sysroot, from the cache or from --sysroot on the compile line.
	sysroot := ""
	if options.Model != nil {
		sysroot = options.Model.Sysroot()
	}
	if sysroot == "" {
		sysroot = SysrootFromFlags(options.CompileFlags)
	}
	if sysroot != "" {
		key := "sysroot:" + sysrootID(sysroot)
		if _, err := registry.Register(key, sysroot, "sysroot"); err != nil {
			return nil, err
		}
	}

	result.ImplicitIncludeDirs = dedupeSorted(result.ImplicitIncludeDirs)
	result.ImplicitLinkDirs = dedupeSorted(result.ImplicitLinkDirs)
	if len(result.ImplicitIncludeDirs) == 0 {
		result.Findings = append(result.Findings, domain.Finding{
			ID:       "TOOLCHAIN_LAYOUT_UNKNOWN",
			Severity: domain.SeverityWarning,
			Subject:  domain.Subject{Kind: "build", Ref: buildRoot},
			Message:  "no implicit include directories are known; system headers are classified by path heuristics with low confidence",
			// The compiler probes are named second and with what they can do,
			// because -print-search-dirs reports library directories but no
			// include directories: they narrow this gap without closing it.
			Remediation: "Enable the CMake File API so that toolchains-v1 reports the compiler's own include directories. " +
				"--allow-introspection=compiler supplies the implicit link directories only; the include directories come from the File API.",
		})
	}
	return result, nil
}

// NormalizeConfigKey maps a configuration anchor key onto the grammar of
// section 7.2. A bare name such as "shared" becomes "extern:shared", which is
// what a user writing a plain name means; a key that already names a kind is
// taken verbatim.
func NormalizeConfigKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return key
	}
	if kind, _, hasName := strings.Cut(key, ":"); hasName {
		switch pathmodel.AnchorKind(kind) {
		case pathmodel.AnchorSDK, pathmodel.AnchorPackage, pathmodel.AnchorToolchain,
			pathmodel.AnchorSysroot, pathmodel.AnchorExtern:
			return key
		}
		return key
	}
	switch pathmodel.AnchorKind(key) {
	case pathmodel.AnchorProject, pathmodel.AnchorBuild:
		return key
	}
	return "extern:" + key
}

// SysrootFromFlags extracts a --sysroot value from compiler flags, in both the
// "--sysroot=/path" and "--sysroot /path" forms.
func SysrootFromFlags(flags []string) string {
	for index, flag := range flags {
		if value, found := strings.CutPrefix(flag, "--sysroot="); found {
			return value
		}
		if flag == "--sysroot" && index+1 < len(flags) {
			return flags[index+1]
		}
		if value, found := strings.CutPrefix(flag, "-isysroot"); found && value != "" {
			return value
		}
	}
	return ""
}

func sysrootID(path string) string {
	base := filepath.Base(strings.TrimSuffix(filepath.Clean(path), "/"))
	if base == "" || base == "." || base == "/" {
		return "root"
	}
	return pathmodel.Slug(strings.ToLower(base), 64)
}

// Scope reports the origin category of an identified file.
func (r *Result) Scope(id domain.FileID) Scope {
	kind, _, _ := strings.Cut(string(id.Anchor), ":")
	switch pathmodel.AnchorKind(kind) {
	case pathmodel.AnchorProject:
		return ScopeProject
	case pathmodel.AnchorBuild:
		return ScopeBuild
	case pathmodel.AnchorPackage, pathmodel.AnchorExtern:
		return ScopeThirdParty
	case pathmodel.AnchorSDK:
		return ScopeSDK
	case pathmodel.AnchorToolchain:
		return ScopeToolchain
	case pathmodel.AnchorSysroot:
		return ScopeSystem
	}
	return ScopeUnknown
}

// ScopeOfPath resolves a path and reports its scope. An unanchored path that
// lies under a compiler-reported implicit include directory, or under one of
// the conventional system include roots of section 14.4, is system scope
// rather than unknown.
func (r *Result) ScopeOfPath(base, path string) (domain.FileID, Scope) {
	id := r.Registry.ResolveIn(base, path)
	scope := r.Scope(id)
	if scope != ScopeUnknown {
		return id, scope
	}
	if r.isSystemPath(path) {
		return id, ScopeSystem
	}
	return id, ScopeUnknown
}

// conventionalSystemRoots is the fallback of section 14.4, used when the
// toolchain did not report a directory covering the path. The loader lives in
// /lib64 on x86_64 Linux and is named by every dynamic link, but no compiler
// reports that directory, so it has to be listed here.
var conventionalSystemRoots = []string{
	"/usr/include", "/usr/local/include",
	"/usr/lib", "/usr/lib64", "/lib", "/lib64",
}

func (r *Result) isSystemPath(path string) bool {
	normalized := r.Registry.Flavor().Normalize(path)
	for _, set := range [][]string{r.ImplicitIncludeDirs, r.ImplicitLinkDirs} {
		for _, dir := range set {
			if underDirectory(normalized, r.Registry.Flavor().Normalize(dir)) {
				return true
			}
		}
	}
	for _, dir := range conventionalSystemRoots {
		if underDirectory(normalized, dir) {
			return true
		}
	}
	return false
}

func underDirectory(path, dir string) bool {
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		return false
	}
	return path == dir || strings.HasPrefix(path, dir+"/")
}

// IncludedByDefault reports whether a scope is part of the SBOM under the
// default policy of sections 14.4 and 24.1. Toolchain and system files are
// evidence, but are not ordinary project dependencies.
func IncludedByDefault(scope Scope) bool {
	switch scope {
	case ScopeProject, ScopeBuild, ScopeThirdParty, ScopeSDK:
		return true
	case ScopeToolchain, ScopeSystem:
		return false
	default:
		// Unknown is included and flagged, never silently dropped (section 14.4).
		return true
	}
}

// UnanchoredFinding builds the UNANCHORED_FILE finding required by section 7.5
// for every file that matched no anchor.
func UnanchoredFinding(id domain.FileID) domain.Finding {
	return domain.Finding{
		ID:          "UNANCHORED_FILE",
		Severity:    domain.SeverityWarning,
		Subject:     domain.Subject{Kind: "file", Ref: id.Canonical()},
		Message:     "the file lies under no registered anchor and was identified by absolute path",
		Remediation: "Add an anchors[] entry for the directory this file belongs to, so its identity stays portable.",
	}
}

func sortedToolchains(toolchains []cmakeapi.Toolchain) []cmakeapi.Toolchain {
	out := make([]cmakeapi.Toolchain, len(toolchains))
	copy(out, toolchains)
	// Deterministic registration order regardless of reply ordering.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Language != out[j].Language {
			return out[i].Language < out[j].Language
		}
		return out[i].CompilerPath < out[j].CompilerPath
	})
	return out
}

func dedupeSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	out := values[:0]
	var previous string
	for index, value := range values {
		if index > 0 && value == previous {
			continue
		}
		previous = value
		out = append(out, value)
	}
	return out
}

// sortedPackages fixes the registration order, because section 7.4 says an
// earlier source keeps a directory an later one also claims.
func sortedPackages(packages []PackageAnchor) []PackageAnchor {
	out := append([]PackageAnchor{}, packages...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Root < out[j].Root
	})
	return out
}
