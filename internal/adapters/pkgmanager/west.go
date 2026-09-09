package pkgmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// west reads the manifest a Zephyr workspace declares (section 21). It is
// strategy 2 of section 19.2 -- exact package-manager metadata -- for the
// manager Zephyr projects actually use to assemble their tree.
//
// Two known locations, and no search. A workspace is the directory holding
// .west/config, and that file names the repository the manifest was cloned
// into and the file inside it; the manifest then names every project, where
// west put it and which revision was asked for. A project's path is relative
// to the workspace root -- not to the manifest repository -- because that is
// what west resolves it against, and in the usual Zephyr layout the two are
// different directories.
//
// The workspace root is looked for upwards from the source root and nowhere
// else, one stat of <dir>/.west/config per level, bounded by limits.MaxDepth
// and by the filesystem root. That is the one place this adapter looks above
// the project anchor, and it is defensible for the reason west itself walks
// up: an application in a workspace is a subdirectory of it, so the workspace
// is above the source root or it does not exist. Without a .west directory
// nothing is read at all, even where a west.yml lies beside the sources: a
// manifest repository nobody ran `west init` on has no projects on disk, and
// resolving its paths against a guessed root is what section 20.1 forbids.
//
// What is not done here: `import:` is not followed. A manifest that imports
// another repository's manifest is resolved by west in memory, without writing
// the result anywhere this tool could read, and reading the imported file would
// mean walking into other people's trees for a list of projects. Coverage for
// such a workspace is therefore honestly partial -- the projects the top-level
// manifest states, and no others -- which deviation D39 records.
//
// Two further things are deliberately not read. The `self:` mapping describes
// the manifest repository itself, and west gives it no name of its own -- it
// calls it "manifest" -- so publishing a component from it would mean naming a
// dependency after a keyword. And west falls back to the revision "master"
// where neither the project nor the manifest's defaults state one; that is
// west's fallback rather than a declaration, so nothing is published for it and
// UNKNOWN_VERSION says so.
//
// The key names below -- manifest, defaults, remotes, projects, and a project's
// name, path, revision, url, remote and repo-path -- were checked against
// west's own manifest schema and resolver (west 1.5.0, which docs/dev/README.md
// pins in the development container): a project's path is relative to the
// workspace root, its URL is the remote's url-base joined with repo-path or the
// name, and both default as described above. The name in zephyr/module.yml is
// Zephyr's own file rather than west's and could not be checked against
// anything here, so it is read only where it is a usable single segment and
// otherwise ignored. Everything here is written to fail into silence rather
// than into a value: a shape that is not what was expected produces no package,
// and never an invented one.
type west struct{}

func (west) Manager() string { return "west" }

// The entire file surface this adapter can open. They are constants rather
// than literals because a reviewer must be able to name every path it reads by
// reading these lines: the workspace's own config, the manifest that config
// names, and the module descriptor lying directly in a project the manifest
// states.
const (
	westDir            = ".west"
	westConfigName     = "config"
	westManifestFile   = "west.yml"
	westModuleDir      = "zephyr"
	westModuleName     = "module.yml"
	westModuleAltName  = "module.yaml"
	westConfigSection  = "manifest"
	westSource         = "west"
	westDescribeSource = "git-describe"
)

// The bounds of section 30 for the files this adapter reads. A workspace
// config is a handful of lines and a module descriptor describes one module,
// so a megabyte each is generous; the project count is bounded separately
// because a manifest that is small in bytes can still name an absurd number of
// projects.
const (
	maxWestConfigBytes   = 1 << 20
	maxWestManifestBytes = 1 << 20
	maxWestModuleBytes   = 1 << 20
	maxWestProjects      = 10_000
)

func (a west) Discover(options Options) ([]Package, []domain.Finding) {
	source := options.SourceDir
	if source == "" || source == "." {
		return nil, nil
	}
	topdir, found := westTopdir(source)
	if !found {
		// A project that is not in a west workspace has nothing missing about
		// it, so nothing is opened and nothing is reported.
		return nil, nil
	}

	findings := make([]domain.Finding, 0)
	manifestPath, ok := a.manifestPath(topdir, &findings)
	if !ok {
		return nil, findings
	}
	document, ok := a.readYAML(manifestPath, maxWestManifestBytes, "west manifest", &findings)
	if !ok {
		return nil, findings
	}
	manifest := document.child("manifest")
	if manifest == nil {
		findings = append(findings, westEvidenceFinding("EVIDENCE_UNREADABLE", manifestPath,
			"the west manifest states no manifest mapping, so no project was read from it"))
		return nil, findings
	}
	projects := manifest.child("projects")
	if projects == nil || projects.sequence == nil {
		findings = append(findings, westEvidenceFinding("EVIDENCE_UNREADABLE", manifestPath,
			"the west manifest states no projects sequence, so no project was read from it"))
		return nil, findings
	}
	items := projects.itemsOf()
	if len(items) == 0 {
		// A `projects:` holding plain words rather than mappings parses: the
		// reader records the sequence and reads no item out of it, because a
		// bare word in a sequence has no key above it to say what it means.
		// Passing that in silence would make a manifest this tool cannot read
		// look exactly like a workspace that declares nothing, so it is refused
		// like every other shape that is not what was expected.
		findings = append(findings, westEvidenceFinding("EVIDENCE_UNREADABLE", manifestPath,
			"the west manifest states no project this reader could read, so nothing was taken from it"))
		return nil, findings
	}
	if len(items) > maxWestProjects {
		// Refused whole rather than in part: half a manifest would attribute
		// some projects and leave the rest looking as though the workspace
		// never declared them.
		findings = append(findings, westEvidenceFinding("INPUT_LIMIT_EXCEEDED", manifestPath,
			"the west manifest states more projects than the parser limit of section 30, so no project was read from it"))
		return nil, findings
	}

	defaults := manifest.child("defaults")
	remotes := westRemotes(manifest)
	packages := make([]Package, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		entry, ok := a.projectFor(options, topdir, source, manifestPath, item, defaults, remotes, &findings)
		if !ok {
			continue
		}
		if seen[entry.Name] {
			// One anchor key per package (section 21), and a key registered
			// twice aborts the run. Two projects resolving to one name is a
			// contradiction in the manifest, and the first is kept.
			continue
		}
		seen[entry.Name] = true
		packages = append(packages, entry)
	}
	return packages, findings
}

// projectFor turns one manifest entry into a package, or explains why it could
// not. Everything published about it comes from the manifest, from the module
// descriptor in the project, and from the checkout where introspection is
// allowed to ask it -- and from nothing else.
func (a west) projectFor(options Options, topdir, source, manifestPath string, item, defaults *yamlNode,
	remotes map[string]string, findings *[]domain.Finding) (Package, bool) {
	name := item.scalarAt("name")
	if !westSafeSegment(name) {
		*findings = append(*findings, westEvidenceFinding("EVIDENCE_UNREADABLE", manifestPath,
			fmt.Sprintf("the west manifest states a project named %q, which is not a name this tool can resolve to a directory, so it was skipped", name)))
		return Package{}, false
	}
	// west defaults a project's path to its name, and resolves either against
	// the workspace root.
	relative := item.scalarAt("path")
	if relative == "" {
		relative = name
	}
	root, ok := westResolvePath(topdir, relative)
	if !ok {
		*findings = append(*findings, westEvidenceFinding("EVIDENCE_UNREADABLE", manifestPath,
			fmt.Sprintf("the west manifest puts the project %q at %q, which does not lie inside the workspace, so it was skipped", name, relative)))
		return Package{}, false
	}
	if !dirExists(root) {
		// west clones only the projects whose groups are enabled, so a manifest
		// entry with no directory is the ordinary case rather than missing
		// evidence -- unlike an ESP-IDF lock entry, which records a component
		// that really was resolved and unpacked. A warning per uncloned project
		// would report the size of the manifest instead of the state of the
		// build.
		return Package{}, false
	}
	if westCovers(root, source) {
		// A project whose directory is the source root, or holds it, would
		// rename the project's own component after a manifest entry. Section
		// 19.2 refuses a marker file at the anchor root itself for the same
		// reason.
		return Package{}, false
	}

	entry := Package{
		Name:      name,
		Roots:     []string{root},
		Manager:   a.Manager(),
		AnchorKey: "pkg:west/" + name,
		VCSURL:    westProjectURL(item, defaults, remotes),
	}
	if module, ok := a.moduleName(root, findings); ok {
		// A Zephyr module states its own name, and that is the name the build
		// system and every other module refer to it by. The manifest's entry
		// name is west's handle for the repository, which is the weaker of the
		// two where they differ.
		entry.Name = module
		entry.AnchorKey = "pkg:west/" + module
	}

	revision := item.scalarAt("revision")
	if revision == "" && defaults != nil {
		revision = defaults.scalarAt("revision")
	}
	if westIsCommit(revision) {
		// Section 20.2 point 5: a commit is published as a version only when
		// the configuration asks for it. A manifest that pins a project to a
		// SHA therefore states no version at all -- the SHA travels in the purl
		// and in the vcsCommit property, and UNKNOWN_VERSION says the rest.
		entry.Commit = revision
	} else {
		entry.Take(FieldVersion, Claim{
			Value: westVersionOf(revision), Source: westSource, Rank: RankDeclaredManifest,
			// Section 20.3: a package manager states an exact declared version.
			Confidence: domain.ConfidenceHigh,
		})
	}
	a.refineFromGit(options, &entry, findings)

	if entry.Version.Value == "" {
		message := "the west manifest states no revision for this project"
		if entry.Commit != "" {
			message = "the west manifest pins this project to a commit, which section 20.2 does not publish as a version"
		}
		*findings = append(*findings, domain.Finding{
			ID: "UNKNOWN_VERSION", Severity: domain.SeverityWarning,
			Subject:     domain.Subject{Kind: "component", Ref: entry.Name},
			Message:     message + "; run with --allow-introspection=git to read it from the checkout",
			Remediation: "Enable git introspection, or set components[].version for this project.",
		})
	}
	// The purl restates whichever version claim won, so it is taken with that
	// claim's standing; where the manifest pinned a commit there is no version
	// and the purl rests on the manifest alone.
	purlRank := entry.Version.Rank
	if purlRank == RankNone {
		purlRank = RankDeclaredManifest
	}
	entry.Take(FieldPURL, Claim{
		Value:  GenericPURL(entry.Name, entry.Version.Value, entry.VCSURL, entry.Commit),
		Source: a.Manager(), Rank: purlRank,
	})
	return entry, true
}

// refineFromGit asks the checkout what it is. The manifest says which revision
// was asked for; the checkout says which one is there, including the edits
// somebody has since made to it, which is why it supersedes at rank 5.
//
// It repeats what submodule.go and fetchcontent.go do rather than sharing it:
// each of the three names its own subject in VCS_DIRTY and stands on its own
// declared origin, and a shared helper would have to be told both. What it must
// not repeat is a way around the allowlist -- these are the same three shapes
// of section 9.2 and nothing else.
//
// One honest limit: the runner refuses a path outside the anchors registered
// for the run, which are the project and build trees. In the usual Zephyr
// layout the workspace root lies above the application, so a project outside
// the source tree cannot be asked and the manifest's claims stand alone. That
// is silent by design -- no command runs, so nothing failed -- and deviation
// D39 records it.
func (west) refineFromGit(options Options, found *Package, findings *[]domain.Finding) {
	if options.Runner == nil || !options.Runner.Features.Git {
		return
	}
	if remote, err := options.Runner.Run(options.Context, "git", "-C", found.Root(),
		"config", "--get", "remote.origin.url"); err == nil {
		if value := strings.TrimSpace(string(remote)); value != "" {
			found.VCSURL = NormalizeVCSURL(value)
		}
	}
	described, err := options.Runner.Run(options.Context, "git", "-C", found.Root(),
		"describe", "--tags", "--always", "--dirty")
	if err != nil {
		return
	}
	value := strings.TrimSpace(string(described))
	if value == "" {
		return
	}
	found.Dirty = strings.HasSuffix(value, "-dirty")
	trimmed := strings.TrimSuffix(value, "-dirty")
	exact := !found.Dirty && !strings.Contains(trimmed, "-g")
	confidence := domain.ConfidenceHigh
	if !exact {
		// Section 20.3: a describe with distance or a dirty tree is weaker
		// evidence than an exact tag.
		confidence = domain.ConfidenceMedium
	}
	found.Take(FieldVersion, Claim{
		Value:      westVersionOf(trimmed),
		Source:     westDescribeSource,
		Rank:       RankObservedCheckout,
		Confidence: confidence,
	})
	if commit, err := options.Runner.Run(options.Context, "git", "-C", found.Root(), "rev-parse", "HEAD"); err == nil {
		if value := strings.TrimSpace(string(commit)); value != "" {
			found.Commit = value
		}
	}
	if found.Dirty {
		*findings = append(*findings, domain.Finding{
			ID: "VCS_DIRTY", Severity: domain.SeverityInfo,
			Subject: domain.Subject{Kind: "component", Ref: found.Name},
			Message: "the west project has uncommitted changes, so its version does not identify its content",
		})
	}
}

// moduleName reads the name a Zephyr module states about itself, from the
// zephyr/module.yml lying directly in the project. Both spellings of the
// extension are looked for and the first that exists is read; a descriptor that
// cannot be read leaves the project with the manifest's name, because the
// manifest is evidence in its own right and losing the module name is not
// losing the project.
func (a west) moduleName(root string, findings *[]domain.Finding) (string, bool) {
	for _, file := range []string{westModuleName, westModuleAltName} {
		path := filepath.Join(root, westModuleDir, file)
		if !fileExists(path) {
			continue
		}
		document, ok := a.readYAML(path, maxWestModuleBytes, "Zephyr module descriptor", findings)
		if !ok {
			return "", false
		}
		name := document.scalarAt("name")
		if !westSafeSegment(name) {
			return "", false
		}
		return name, true
	}
	return "", false
}

// manifestPath reads .west/config and returns the manifest it names. The
// config is the only statement there is about where the manifest lives, so a
// config this tool cannot read ends the adapter rather than sending it looking.
func (a west) manifestPath(topdir string, findings *[]domain.Finding) (string, bool) {
	path := filepath.Join(topdir, westDir, westConfigName)
	section, ok := a.readConfig(path, findings)
	if !ok {
		return "", false
	}
	relative, file := section["path"], section["file"]
	if file == "" {
		// west's own default when the config states no file.
		file = westManifestFile
	}
	// The file may name a path inside the manifest repository and not only a
	// file name -- west records whatever `--manifest-file` was given -- so it
	// is validated as a relative path and joined like one.
	if relative == "" || !westSafeRelative(relative) || !westSafeRelative(file) {
		*findings = append(*findings, westEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the workspace config puts its manifest at %q/%q, which is not a path inside the workspace, so no project was read", relative, file)))
		return "", false
	}
	manifestDir, ok := westResolvePath(topdir, relative)
	if !ok {
		*findings = append(*findings, westEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the workspace config puts its manifest repository at %q, which does not lie inside the workspace, so no project was read", relative)))
		return "", false
	}
	manifestPath, ok := westResolvePath(manifestDir, file)
	if !ok {
		*findings = append(*findings, westEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the workspace config names %q as its manifest, which does not lie inside the manifest repository, so no project was read", file)))
		return "", false
	}
	if !fileExists(manifestPath) {
		*findings = append(*findings, domain.Finding{
			ID: "MISSING_PACKAGE_EVIDENCE", Severity: domain.SeverityWarning,
			Subject: domain.Subject{Kind: "evidence", Ref: manifestPath},
			Message: "the workspace config names this file as its manifest, but it does not exist, so no project of this workspace could be read",
		})
		return "", false
	}
	return manifestPath, true
}

// readConfig reads the [manifest] section of .west/config. It is the git-config
// subset .gitmodules is written in as well, so it is read the same way
// submodule.go reads that one: sections in brackets, `key = value` beneath
// them, and nothing else.
func (a west) readConfig(path string, findings *[]domain.Finding) (map[string]string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if info.Size() > maxWestConfigBytes {
		*findings = append(*findings, westEvidenceFinding("INPUT_LIMIT_EXCEEDED", path,
			"the workspace config is larger than the parser limit of section 30, so nothing was read from it"))
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		*findings = append(*findings, westEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the workspace config could not be read, so nothing was taken from it"))
		return nil, false
	}
	defer file.Close()

	section := map[string]string{}
	inManifest := false
	scanner := limits.Scanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			// Exactly the section, not a name that begins with it: every other
			// section of this file belongs to somebody else's setting.
			inManifest = line == "["+westConfigSection+"]"
			continue
		}
		if !inManifest {
			continue
		}
		key, value, split := strings.Cut(line, "=")
		if !split {
			continue
		}
		key = strings.TrimSpace(key)
		if _, taken := section[key]; taken {
			// The first statement wins, as everywhere else here, so that two
			// runs over one file agree.
			continue
		}
		section[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		*findings = append(*findings, westEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the workspace config could not be read to its end, so nothing was taken from it"))
		return nil, false
	}
	if len(section) == 0 {
		*findings = append(*findings, westEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the workspace config states no manifest section, so no manifest could be found"))
		return nil, false
	}
	return section, true
}

// readYAML reads one bounded file through the subset reader of yamlsubset.go.
// A file over its bound or one the reader refuses contributes nothing at all: a
// value taken out of a file whose remainder could not be read would be
// published with nothing behind it.
func (west) readYAML(path string, maxBytes int64, kind string, findings *[]domain.Finding) (*yamlNode, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if info.Size() > maxBytes {
		*findings = append(*findings, westEvidenceFinding("INPUT_LIMIT_EXCEEDED", path,
			fmt.Sprintf("the %s is larger than the parser limit of section 30, so nothing was read from it", kind)))
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		*findings = append(*findings, westEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the %s could not be read, so nothing was taken from it", kind)))
		return nil, false
	}
	document, err := parseYAMLSubset(data)
	if err != nil {
		*findings = append(*findings, westEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the %s holds %s, so nothing was taken from it", kind, err.Error())))
		return nil, false
	}
	return document, true
}

// westEvidenceFinding reports a file that was found but could not be used. The
// subject is the file rather than the component: what could not be read is a
// piece of evidence, and there may not be a component to name yet.
func westEvidenceFinding(id, path, message string) domain.Finding {
	return domain.Finding{
		ID: id, Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: path},
		Message: message,
	}
}

// westTopdir finds the workspace the source root lies in: the nearest directory
// at or above it holding .west/config. It stats one fixed relative path per
// level, lists nothing, and stops at the filesystem root or at the recursion
// bound of section 30, whichever comes first.
func westTopdir(source string) (string, bool) {
	dir := filepath.Clean(source)
	for level := 0; level < limits.MaxDepth; level++ {
		if fileExists(filepath.Join(dir, westDir, westConfigName)) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
	return "", false
}

// westRemotes reads the remotes a manifest declares into a lookup of name to
// URL base. The first statement of a name wins, so that a manifest naming one
// remote twice cannot make the document depend on map order.
func westRemotes(manifest *yamlNode) map[string]string {
	remotes := map[string]string{}
	for _, item := range manifest.child("remotes").itemsOf() {
		name, base := item.scalarAt("name"), item.scalarAt("url-base")
		if name == "" || base == "" {
			continue
		}
		if _, taken := remotes[name]; taken {
			continue
		}
		remotes[name] = base
	}
	return remotes
}

// westProjectURL is where the manifest says the project came from. A project
// may state its URL outright, or name a remote whose URL base is joined with
// the repository path -- which defaults to the project's name. The explicit URL
// is asked first, because a project that states one is not using the remote at
// all.
func westProjectURL(item, defaults *yamlNode, remotes map[string]string) string {
	if url := item.scalarAt("url"); url != "" {
		return NormalizeVCSURL(url)
	}
	remote := item.scalarAt("remote")
	if remote == "" && defaults != nil {
		remote = defaults.scalarAt("remote")
	}
	base, known := remotes[remote]
	if !known {
		return ""
	}
	path := item.scalarAt("repo-path")
	if path == "" {
		path = item.scalarAt("name")
	}
	if path == "" {
		return ""
	}
	return NormalizeVCSURL(strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(path, "/"))
}

// westResolvePath joins a path out of somebody else's manifest onto the
// workspace root (section 30.3). It refuses anything that does not end up
// strictly inside the workspace: an absolute path, a path climbing out with
// .., and the workspace root itself, which as a project root would claim every
// file in the tree.
func westResolvePath(topdir, relative string) (string, bool) {
	if !westSafeRelative(relative) {
		return "", false
	}
	root := filepath.Clean(filepath.Join(topdir, filepath.FromSlash(relative)))
	if !strings.HasPrefix(root, strings.TrimSuffix(filepath.Clean(topdir), string(filepath.Separator))+string(filepath.Separator)) {
		return "", false
	}
	return root, true
}

// westCovers reports whether a project directory is the source root or holds
// it.
func westCovers(root, source string) bool {
	root = filepath.Clean(root)
	source = filepath.Clean(source)
	if root == source {
		return true
	}
	return strings.HasPrefix(source, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}

// westSafeRelative reports whether a path may be joined onto the workspace
// root. Section 30.3: a manifest is somebody else's file, and a project placed
// at "/etc" or at "../.." must not be able to point a component root out of the
// workspace.
func westSafeRelative(value string) bool {
	if value == "" || value == "." {
		return false
	}
	if strings.ContainsAny(value, "\x00\n\r") {
		return false
	}
	// Both separators are refused, not only this platform's, and so is a drive
	// letter: a manifest written on Windows is read on Linux and the other way
	// round, and filepath.IsAbs answers for the platform it is compiled for.
	if filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) {
		return false
	}
	if len(value) >= 2 && value[1] == ':' {
		return false
	}
	for _, segment := range strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return false
		}
	}
	return true
}

// westSafeSegment reports whether a name may stand as one path segment and as a
// component name. It is the rule espidf.go applies to a lock file's dependency
// key, for the same reason: a project named ".." or "a/b" must not decide where
// a component root lies.
func westSafeSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	if strings.HasPrefix(segment, ".") {
		return false
	}
	if strings.ContainsAny(segment, `/\`) {
		return false
	}
	return !strings.ContainsAny(segment, "\x00\n\r")
}

// westIsCommit reports whether a revision is a commit rather than something to
// publish as a version. A full SHA-1 or SHA-256 is one whatever its digits are;
// a shorter revision counts only when it carries a hex letter, because a tag
// made of digits alone -- 20240612, the date convention -- is far more likely
// to be a release than an abbreviated commit, and calling it a commit would
// throw away the only version the manifest states.
func westIsCommit(revision string) bool {
	if revision == "" {
		return false
	}
	letter := false
	for index := 0; index < len(revision); index++ {
		switch char := revision[index]; {
		case char >= '0' && char <= '9':
		case char >= 'a' && char <= 'f', char >= 'A' && char <= 'F':
			letter = true
		default:
			return false
		}
	}
	if len(revision) == 40 || len(revision) == 64 {
		return true
	}
	return letter && len(revision) >= 7
}

// westVersionOf is the version a revision states. A leading v before a digit is
// the tag convention rather than part of the version -- v3.5.0 is 3.5.0 -- but
// a branch called "vendor" keeps its name: trimming a letter off a word would
// invent a version rather than read one.
func westVersionOf(revision string) string {
	if len(revision) >= 2 && revision[0] == 'v' && revision[1] >= '0' && revision[1] <= '9' {
		return revision[1:]
	}
	return revision
}
