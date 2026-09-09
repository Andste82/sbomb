package pkgmanager

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// This file reads the pkg-config metadata a system library installs beside
// itself: <libdir>/pkgconfig/<module>.pc, a few lines naming the module, its
// version and the flags a consumer needs. It is the file-based half of the
// answer section 24.3 otherwise expects `dpkg -S` to give, and it costs no
// permission: no process starts, no directory is listed, and nothing outside
// the file's own anchor is opened.
//
// It is a fourth kind of reader, and none of the three that exist would carry
// it. An adapter enumerates: every .pc file in a sysroot would become a
// package, each one claiming files by path prefix and each one that matched
// nothing reported as PACKAGE_NOT_LINKED -- the noise D36 describes for an
// image manifest, in a directory that holds a thousand entries. An adapter also
// runs before the anchors are assembled, so it does not yet know where the
// sysroot is. An enricher is handed a directory that already stands as a
// component root and reads what lies directly in it; a .pc file lies in
// <libdir>/pkgconfig rather than in the root, and for a system file the settled
// root today is the sysroot anchor itself, under which every system file of the
// run falls into a single component. A reader of the third kind is keyed by
// name, and the name a system file's component carries is the anchor's, which
// no module is called.
//
// So this reader does the one thing the other three cannot: it says which
// package a system file belongs to, by walking up from that file -- bounded by
// its anchor, never downwards -- and addressing a .pc file by name, derived
// from the used file's own name. libfoo.so.3 asks for foo.pc and libfoo.pc and
// for nothing else. No directory is listed and no set of .pc files is searched:
// an index over a pkgconfig directory is the scan D30 refused for
// /var/lib/dpkg/info, and it would let the size of the sysroot decide what the
// document says. The price is that zlib.pc is never found for libz.so, and
// silence there is the right answer (D37).
//
// A candidate is believed only once the file it came from verifies against the
// used file: a libdir it names has to be the directory the library really lies
// in, and one of its -l names has to name that library's file; for a header an
// includedir has to contain it. Two candidates that verify and disagree map
// nothing at all and are reported as COMPONENT_MAPPING_CONFLICT, because two
// statements are no statement (section 19.2).
//
// Like an enricher, the return type is the whole guarantee: a module name and
// contributions, with no path, no root and no anchor anywhere in it. A .pc file
// therefore cannot add a file to the used set, cannot register an anchor and
// cannot move a boundary, however much a later reader might want it to.

// pkgConfigSource is the origin these claims are published under. It reaches
// the document through component.VersionSource, where section 20.3 maps it onto
// the closed CycloneDX technique vocabulary, so it is output rather than an
// internal label.
const pkgConfigSource = "pkg-config"

// The bounds of section 30 for this format. The largest .pc file on the machine
// this was written against is systemd.pc at 3983 bytes, and it defines sixty
// variables, so both ceilings sit two orders of magnitude above the measured
// reality -- they exist to stop a hostile file, not to judge a real one.
//
// The expansion depth is the one bound a real file comes near: systemd.pc
// chains root_prefix -> rootprefix -> prefix, so several levels are ordinary.
// The candidate-directory bound keeps the upward walk from paying for a deeply
// nested sysroot; nothing observed needs more than three levels.
const (
	maxPkgConfigBytes          = 256 << 10
	maxPkgConfigVariables      = 512
	maxPkgConfigExpansionDepth = 8
	maxPkgConfigCandidateDirs  = 16
)

// pkgConfigDirs are the two places below a directory where a .pc file is kept:
// a library's own <libdir>/pkgconfig, and the architecture-independent
// <prefix>/share/pkgconfig. Both are observed on a plain Debian install --
// /usr/lib/x86_64-linux-gnu/pkgconfig and /usr/share/pkgconfig -- and the order
// is fixed, because it decides which of two identical files answers.
var pkgConfigDirs = [][]string{{"pkgconfig"}, {"share", "pkgconfig"}}

// SystemFile is one used file to be described: where its bytes are, the anchor
// root the upward walk may not leave, and the identity a finding names it by.
//
// It is input and not output. Nothing in it reaches the caller again, and the
// result type below carries none of it, so a path handed in here cannot come
// back out as a component root.
type SystemFile struct {
	// Path is the file on disk, absolute and local to this run.
	Path string
	// Boundary is the root of the file's anchor. The walk stops there, every
	// directory a .pc file names is resolved inside it, and no .pc file outside
	// it is opened. An empty boundary describes nothing: without one the walk
	// has no bound, and section 22.1 does not allow an unbounded one.
	Boundary string
	// Ref is the file's canonical identity, for the subject of a finding. A
	// report never carries an absolute path (section 7.5).
	Ref string
}

// SystemPackage is what a .pc file states about the system file it was found
// for. Module is the name pkg-config itself keeps the package under -- the
// file's base name -- and it becomes the component's name and identity.
//
// There is deliberately nothing else in it: no path, no root, no anchor and no
// file list. A reviewer can read off the type that this reader improves what is
// known about a file the evidence chain already reached and can do nothing
// else, which is the rule of this package kept structurally rather than by
// discipline.
type SystemPackage struct {
	Module        string
	Contributions []Contribution
}

// Described reports whether a package was identified at all.
func (p SystemPackage) Described() bool { return p.Module != "" }

// PkgConfigReader describes system files from the pkg-config metadata beside
// them. It holds a cache for one run because an include directory has hundreds
// of files in it that all derive the same candidate: without the cache the same
// .pc file would be opened and parsed once per used file, which is the cost
// section 31 bounds.
type PkgConfigReader struct {
	// parsed is what came of one .pc path, keyed by that path. A file that
	// could not be read is cached too, so that a broken file is reported once
	// rather than once per file that asked for it.
	parsed map[string]*pkgConfigResult
}

// NewPkgConfigReader returns a reader with an empty cache. One per run: the
// cache is a memo of the filesystem as it was during that run and must not
// outlive it.
func NewPkgConfigReader() *PkgConfigReader {
	return &PkgConfigReader{parsed: map[string]*pkgConfigResult{}}
}

// pkgConfigResult is one .pc file as it was read, and the findings reading it
// produced. The findings are kept beside the file so that a second caller gets
// the same file and no second report about it: reading one .pc file is one
// event, however many used files derive it as a candidate.
type pkgConfigResult struct {
	file     *pkgConfigFile
	findings []domain.Finding
}

// pkgConfigFile is a .pc file reduced to what this reader uses. The module is
// the file's base name and not its Name: line, because pkg-config keeps the
// package under the file name and the two differ often enough to matter --
// xkeyboard-config.pc says "Name: XKeyboardConfig", and libcrypt.pc is a
// symlink to libxcrypt.pc whose Name: says libxcrypt.
type pkgConfigFile struct {
	module      string
	path        string
	name        string
	version     string
	libDirs     []string
	libNames    []string
	includeDirs []string
}

// Describe names the package a used system file belongs to, or nothing.
//
// Nothing is the ordinary answer, and it carries no finding: a system file with
// no .pc file beside it is a file whose distribution installed none, which says
// nothing about the file and is not evidence anybody expected.
func (r *PkgConfigReader) Describe(file SystemFile) (SystemPackage, []domain.Finding) {
	if r == nil || file.Path == "" || file.Boundary == "" {
		return SystemPackage{}, nil
	}
	modules := pkgConfigModuleCandidates(file.Path)
	if len(modules) == 0 {
		// Neither a library nor a header, so there is no name to derive a
		// module from and nothing to verify a candidate against.
		return SystemPackage{}, nil
	}

	findings := make([]domain.Finding, 0)
	// Every candidate that exists and verifies, in the fixed order the
	// candidates were generated in. All of them are collected rather than the
	// first one taken, because a second one that disagrees has to be able to
	// stop the mapping.
	var accepted []*pkgConfigFile
	for _, dir := range r.candidateDirs(file) {
		for _, module := range modules {
			parsed, parsedFindings := r.read(filepath.Join(dir, module+".pc"), file.Boundary)
			findings = append(findings, parsedFindings...)
			if parsed == nil {
				continue
			}
			if !parsed.describes(file) {
				// The file exists and was read, and it says the library lies
				// somewhere else. That is an answer rather than a fault: a name
				// that merely happens to match is exactly what verification is
				// for, and dropping it in silence is the point.
				continue
			}
			accepted = append(accepted, parsed)
		}
	}
	if len(accepted) == 0 {
		return SystemPackage{}, findings
	}
	if conflict, contested := pkgConfigConflict(file, accepted); contested {
		return SystemPackage{}, append(findings, conflict...)
	}
	return accepted[0].describe(), findings
}

// candidateDirs are the directories that may hold a .pc file for this file:
// <dir>/pkgconfig and <dir>/share/pkgconfig at every level from the file's own
// directory up to the anchor root, deepest first.
//
// Upwards only, and never past the boundary: section 22.1 allows walking up
// from a file the evidence chain reached and forbids searching downwards for
// one.
func (r *PkgConfigReader) candidateDirs(file SystemFile) []string {
	boundary := filepath.Clean(file.Boundary)
	dirs := make([]string, 0, maxPkgConfigCandidateDirs)
	dir := filepath.Dir(file.Path)
	for depth := 0; depth < limits.MaxDepth && len(dirs) < maxPkgConfigCandidateDirs; depth++ {
		if !withinPkgConfigBoundary(dir, boundary) {
			break
		}
		for _, suffix := range pkgConfigDirs {
			dirs = append(dirs, filepath.Join(append([]string{dir}, suffix...)...))
		}
		if filepath.Clean(dir) == boundary {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if len(dirs) > maxPkgConfigCandidateDirs {
		dirs = dirs[:maxPkgConfigCandidateDirs]
	}
	return dirs
}

// read returns the parsed .pc file at a path, or nil for one that is not there
// or could not be used. The result is cached for the run, and its findings come
// with it exactly once.
func (r *PkgConfigReader) read(path, boundary string) (*pkgConfigFile, []domain.Finding) {
	if cached, known := r.parsed[path]; known {
		// The file was read once and reported once. A second caller gets what
		// was read and no second report.
		return cached.file, nil
	}
	result := &pkgConfigResult{}
	r.parsed[path] = result

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		// A .pc file that is not there is the ordinary case: most system files
		// have none. Absence is silence.
		return nil, nil
	}
	if info.Size() > maxPkgConfigBytes {
		// Checked before the read, so a file over the bound is never allocated.
		result.findings = []domain.Finding{pkgConfigEvidenceFinding("INPUT_LIMIT_EXCEEDED", path, boundary,
			"the pkg-config file is larger than the parser limit of section 30, so nothing was read from it")}
		return nil, result.findings
	}
	data, err := os.ReadFile(path)
	if err != nil {
		result.findings = []domain.Finding{pkgConfigEvidenceFinding("EVIDENCE_UNREADABLE", path, boundary,
			"the pkg-config file could not be read, so nothing was taken from it")}
		return nil, result.findings
	}
	parsed, err := parsePkgConfig(path, data)
	if err != nil {
		id := "EVIDENCE_UNREADABLE"
		if errors.Is(err, limits.ErrInputLimitExceeded) {
			id = "INPUT_LIMIT_EXCEEDED"
		}
		result.findings = []domain.Finding{pkgConfigEvidenceFinding(id, path, boundary,
			fmt.Sprintf("the pkg-config file %s, so nothing was taken from it", err.Error()))}
		return nil, result.findings
	}
	result.file = parsed
	return parsed, nil
}

// describes reports whether this .pc file really is the one that describes the
// used file. This is the whole difference between reading a name and asserting
// a mapping, and nothing is mapped without it.
//
// A library has to lie in a directory the file names as a library directory and
// carry a name one of its -l entries names. Either alone would be far too
// little: every package in /usr/lib shares that directory, and -lfoo says
// nothing about where foo lies.
//
// A header has only an include directory to go on, because a .pc file names no
// headers. That is weaker, and it is why the module candidate for a header is
// derived from the directory the header sits in before its own name.
func (f *pkgConfigFile) describes(file SystemFile) bool {
	dir := filepath.Clean(filepath.Dir(file.Path))
	base := filepath.Base(file.Path)
	if isPkgConfigLibrary(base) {
		if !pkgConfigNames(f.libNames, base) {
			return false
		}
		for _, named := range f.libDirs {
			for _, resolved := range resolvePkgConfigDir(named, file.Boundary) {
				if resolved == dir {
					return true
				}
			}
		}
		return false
	}
	if isPkgConfigHeader(base) {
		for _, named := range f.includeDirs {
			for _, resolved := range resolvePkgConfigDir(named, file.Boundary) {
				if withinPkgConfigBoundary(dir, resolved) {
					return true
				}
			}
		}
	}
	return false
}

// describe turns a verified .pc file into what the component publishes. Only
// the version travels: a .pc file names no licence at all, states no supplier,
// and belongs to no package ecosystem a purl could name, so those three stay
// unanswered and the findings that say so stay in the report.
//
// The Name: line is not published either. A component's name settles before its
// files are grouped, and the module the mapping used is that name already;
// putting Name: on top of it would rename a component after a second string in
// the same file, which D33 already decided against for a bundled SBOM.
func (f *pkgConfigFile) describe() SystemPackage {
	described := SystemPackage{Module: f.module}
	if f.version != "" {
		described.Contributions = append(described.Contributions, Contribution{
			Field: FieldVersion,
			Claim: Claim{
				Value:  f.version,
				Source: pkgConfigSource,
				// Rank 3 of section 21.1: a .pc file is the generated
				// configuration a package writes while installing, which is the
				// same category as a lock file or an installed-file list. It
				// names what is on disk rather than what was asked for.
				Rank: RankInstallState,
				// Section 20.3 has a confidence table for the version alone, and
				// a version stated outright in a file the package installed is
				// as good as a package manager's.
				Confidence: domain.ConfidenceHigh,
			},
		})
	}
	return described
}

// pkgConfigConflict reports two verified .pc files that describe the used file
// differently. The comparison is on what a component would carry away -- the
// module it is named after, the name the file states and the version -- because
// those are what a wrong choice would publish.
//
// Two candidate directories holding the same file is not a disagreement:
// /usr/lib/pkgconfig and /usr/share/pkgconfig can carry one package's .pc file
// between them, and two identical answers are one answer.
func pkgConfigConflict(file SystemFile, accepted []*pkgConfigFile) ([]domain.Finding, bool) {
	first := accepted[0]
	sides := []domain.ConflictSide{}
	for _, candidate := range accepted[1:] {
		if candidate.module == first.module && candidate.name == first.name && candidate.version == first.version {
			continue
		}
		if len(sides) == 0 {
			sides = append(sides, pkgConfigSide(first, file.Boundary))
		}
		sides = append(sides, pkgConfigSide(candidate, file.Boundary))
	}
	if len(sides) == 0 {
		return nil, false
	}
	conflict := domain.Conflict{
		Field:   "package a pkg-config file describes this system file as",
		Subject: domain.Subject{Kind: "file", Ref: file.Ref},
		Sides:   sortedPkgConfigSides(sides),
		Reason: "two statements are no statement, so the file keeps the component its anchor " +
			"gives it (section 19.2)",
	}
	if finding, ok := conflict.Finding("COMPONENT_MAPPING_CONFLICT", domain.SeverityInfo); ok {
		return []domain.Finding{finding}, true
	}
	return nil, true
}

// pkgConfigSide names one contesting file by where it is and what it said.
func pkgConfigSide(file *pkgConfigFile, boundary string) domain.ConflictSide {
	value := file.module
	if file.version != "" {
		value = file.module + " " + file.version
	}
	return domain.ConflictSide{Source: pkgConfigRef(file.path, boundary), Value: value}
}

// sortedPkgConfigSides puts the sides in a fixed order: the same two files found
// in the other order are the same disagreement, and a report that read
// differently for it would look like a second one.
func sortedPkgConfigSides(sides []domain.ConflictSide) []domain.ConflictSide {
	sorted := append([]domain.ConflictSide(nil), sides...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Source != sorted[j].Source {
			return sorted[i].Source < sorted[j].Source
		}
		return sorted[i].Value < sorted[j].Value
	})
	return sorted
}

// pkgConfigGenericDirs are directory names that name no package. A header in
// one of them has to be asked about under its own name, because the directory
// only says where headers live.
var pkgConfigGenericDirs = map[string]bool{
	"include": true, "usr": true, "local": true, "share": true, "lib": true,
	"lib32": true, "lib64": true, ".": true, "/": true, "": true,
}

// pkgConfigModuleCandidates are the module names a used file's own name allows
// this reader to ask for, in a fixed order. Nothing else is ever opened.
//
// For a library the name is the linker's: libfoo.so.3 is -lfoo, so foo.pc and
// libfoo.pc are the two spellings a distribution gives it. For a header the
// directory it sits in comes first -- <includedir>/foo/bar.h belongs to package
// foo far more often than to a package bar -- and the header's own stem second,
// for a package that installs a single header at the top of an include
// directory.
func pkgConfigModuleCandidates(path string) []string {
	base := filepath.Base(path)
	var stems []string
	switch {
	case isPkgConfigLibrary(base):
		stems = []string{pkgConfigLibraryName(base)}
	case isPkgConfigHeader(base):
		if parent := filepath.Base(filepath.Dir(path)); !pkgConfigGenericDirs[parent] {
			stems = append(stems, parent)
		}
		stems = append(stems, strings.TrimSuffix(base, filepath.Ext(base)))
	default:
		return nil
	}
	candidates := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, stem := range stems {
		if stem == "" {
			continue
		}
		for _, name := range []string{stem, "lib" + stem} {
			if !seen[name] && pkgConfigModuleName(name) {
				seen[name] = true
				candidates = append(candidates, name)
			}
		}
	}
	return candidates
}

// pkgConfigLibraryName is the name the linker knows a library file by: the base
// name up to the first dot, with a leading "lib" removed.
func pkgConfigLibraryName(base string) string {
	stem := base
	if index := strings.IndexByte(stem, '.'); index > 0 {
		stem = stem[:index]
	}
	if strings.HasPrefix(stem, "lib") && len(stem) > 3 {
		stem = stem[3:]
	}
	return stem
}

// pkgConfigModuleName refuses a name that could address anything but a file in
// the candidate directory. A module is one path segment and nothing else
// (section 30.3).
func pkgConfigModuleName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\`) && !strings.ContainsRune(name, 0)
}

// pkgConfigNames reports whether one of a Libs line's -l names names this file.
// -lfoo is libfoo.so, libfoo.so.3, libfoo.a, libfoo.dylib or foo.dll; it is not
// libfoobar.so, which is why the comparison is on a whole name rather than on a
// prefix.
func pkgConfigNames(names []string, base string) bool {
	forms := []string{base}
	if strings.HasPrefix(base, "lib") && len(base) > 3 {
		forms = append(forms, base[3:])
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		for _, form := range forms {
			if form == name || strings.HasPrefix(form, name+".") {
				return true
			}
		}
	}
	return false
}

// libraryExtensions and headerExtensions decide from a file's own name which
// kind of file is being described. A .pc file names libraries and include
// directories and nothing else, so a file that is neither cannot be verified
// against one and is never asked about.
var (
	libraryExtensions = []string{".so", ".a", ".dylib", ".dll", ".lib", ".tbd"}
	headerExtensions  = []string{".h", ".hh", ".hpp", ".hxx", ".h++", ".inc", ".ipp", ".tcc"}
)

func isPkgConfigLibrary(base string) bool {
	for _, ext := range libraryExtensions {
		// A shared library carries its soname after the extension --
		// libfoo.so.3.1 -- so the extension is a segment of the name rather
		// than its end.
		if strings.HasSuffix(base, ext) || strings.Contains(base, ext+".") {
			return true
		}
	}
	return false
}

func isPkgConfigHeader(base string) bool {
	ext := filepath.Ext(base)
	for _, known := range headerExtensions {
		if strings.EqualFold(ext, known) {
			return true
		}
	}
	// A C++ standard library header carries no extension at all.
	return ext == ""
}

// resolvePkgConfigDir turns a directory a .pc file names into the directories on
// this machine it could mean, or nothing for one that may not be looked at.
//
// A .pc file states absolute paths as the package will be installed --
// libdir=/usr/lib -- while in a cross build the whole tree lies under a sysroot.
// Prefixing the anchor root is what PKG_CONFIG_SYSROOT_DIR does, and without it
// nothing would ever match in a cross build. The unprefixed form is accepted as
// well, for a toolchain that rewrote the paths itself, but only while it stays
// inside the anchor.
//
// A path with a ".." segment is refused outright rather than cleaned: section
// 30.3 does not let a file address its way out of the tree it was found in, and
// cleaning it first would hide the attempt.
func resolvePkgConfigDir(named, boundary string) []string {
	if named == "" || !strings.HasPrefix(named, "/") {
		// A relative directory names nothing this reader can check: pkg-config
		// resolves it against the caller's working directory, which is not
		// where the build happened.
		return nil
	}
	for _, segment := range strings.Split(named, "/") {
		if segment == ".." {
			return nil
		}
	}
	cleaned := filepath.Clean(filepath.FromSlash(named))
	boundary = filepath.Clean(boundary)
	prefixed := filepath.Join(boundary, cleaned)
	resolved := make([]string, 0, 2)
	if withinPkgConfigBoundary(prefixed, boundary) {
		resolved = append(resolved, prefixed)
	}
	if cleaned != prefixed && withinPkgConfigBoundary(cleaned, boundary) {
		resolved = append(resolved, cleaned)
	}
	return resolved
}

// withinPkgConfigBoundary reports whether a path is the boundary or lies under
// it, comparing whole segments so that a sibling directory with a shared prefix
// cannot pass for one inside.
func withinPkgConfigBoundary(path, boundary string) bool {
	if boundary == "" {
		return false
	}
	path, boundary = filepath.Clean(path), filepath.Clean(boundary)
	if path == boundary {
		return true
	}
	separator := string(filepath.Separator)
	return strings.HasPrefix(path, strings.TrimSuffix(boundary, separator)+separator)
}

// pkgConfigRef names a .pc file the way a report may: relative to the anchor
// root it was found under, because section 7.5 keeps absolute paths out of the
// output.
func pkgConfigRef(path, boundary string) string {
	if relative, err := filepath.Rel(boundary, path); err == nil && !strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}

// pkgConfigEvidenceFinding reports a .pc file that was found but could not be
// used. The subject is the file rather than a component: what could not be read
// is a piece of evidence, and the system file it would have described is in the
// document either way, under the component its anchor gives it.
func pkgConfigEvidenceFinding(id, path, boundary, message string) domain.Finding {
	return domain.Finding{
		ID: id, Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: pkgConfigRef(path, boundary)},
		Message: message,
	}
}

// parsePkgConfig reads one .pc file: "name=value" variable definitions,
// "Key: value" keyword lines, "#" comments and blank lines.
//
// A line that is none of those makes the file's structure something this reader
// does not understand, so the whole file is refused rather than the one line --
// the same rule the Yocto manifest reader follows, and for the same reason: the
// lines after it would then be read in a context that was guessed.
func parsePkgConfig(path string, data []byte) (*pkgConfigFile, error) {
	file := &pkgConfigFile{
		module: strings.TrimSuffix(filepath.Base(path), ".pc"),
		path:   path,
	}
	// The variable definitions as written, expanded on demand below. They are
	// kept raw rather than substituted as they are read so that the expansion
	// depth is a bound this reader enforces rather than an accident of the
	// order the file happens to define them in.
	variables := map[string]string{}
	defined := make([]string, 0, 8)
	keys := map[string]string{}

	scanner := limits.Scanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		equals := strings.IndexByte(line, '=')
		switch {
		case equals > 0 && (colon < 0 || equals < colon):
			name := strings.TrimSpace(line[:equals])
			if name == "" {
				return nil, errors.New("defines a variable with no name")
			}
			if _, seen := variables[name]; !seen {
				if len(defined) >= maxPkgConfigVariables {
					return nil, limits.ErrInputLimitExceeded
				}
				defined = append(defined, name)
			}
			// The last definition wins, which is what pkg-config does and what
			// a --define-variable override relies on.
			variables[name] = strings.TrimSpace(line[equals+1:])
		case colon > 0:
			// A keyword this reader does not know is skipped rather than
			// refused: Requires, Conflicts and URL are ordinary parts of the
			// format, and reading them would be interpreting it.
			keys[strings.TrimSpace(line[:colon])] = strings.TrimSpace(line[colon+1:])
		default:
			return nil, fmt.Errorf("holds a line that is neither %q nor %q", "name=value", "Key: value")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("could not be read to its end (%v)", err)
	}

	name, hasName := keys["Name"]
	version, hasVersion := keys["Version"]
	if !hasName && !hasVersion {
		// Name and Version are the two fields the format requires, so a file
		// stating neither is not a pkg-config file in any useful sense.
		return nil, errors.New("states neither a Name nor a Version")
	}

	expand := newPkgConfigExpander(variables)
	var err error
	if file.name, err = expand.value(name); err != nil {
		return nil, err
	}
	if file.version, err = expand.value(version); err != nil {
		return nil, err
	}
	libdir, err := expand.variable("libdir")
	if err != nil {
		return nil, err
	}
	if libdir != "" {
		file.libDirs = append(file.libDirs, libdir)
	}
	includedir, err := expand.variable("includedir")
	if err != nil {
		return nil, err
	}
	if includedir != "" {
		file.includeDirs = append(file.includeDirs, includedir)
	}
	libs, err := expand.value(keys["Libs"])
	if err != nil {
		return nil, err
	}
	cflags, err := expand.value(keys["Cflags"])
	if err != nil {
		return nil, err
	}
	dirs, names := parsePkgConfigFlags(libs, "-L", "-l")
	file.libDirs = append(file.libDirs, dirs...)
	file.libNames = names
	includes, _ := parsePkgConfigFlags(cflags, "-I", "")
	file.includeDirs = append(file.includeDirs, includes...)
	return file, nil
}

// parsePkgConfigFlags picks the two flags this reader understands out of a Libs
// or Cflags line, in both the attached and the separated spelling. Everything
// else on the line is somebody else's compiler flag and is left alone.
func parsePkgConfigFlags(line, dirFlag, nameFlag string) (dirs, names []string) {
	fields := strings.Fields(line)
	if limits.TooManyTokens(len(fields)) {
		return nil, nil
	}
	for index := 0; index < len(fields); index++ {
		field := fields[index]
		for _, flag := range []struct {
			prefix string
			into   *[]string
		}{{dirFlag, &dirs}, {nameFlag, &names}} {
			if flag.prefix == "" || !strings.HasPrefix(field, flag.prefix) {
				continue
			}
			if len(field) > len(flag.prefix) {
				*flag.into = append(*flag.into, field[len(flag.prefix):])
			} else if index+1 < len(fields) {
				index++
				*flag.into = append(*flag.into, fields[index])
			}
			break
		}
	}
	return dirs, names
}

// pkgConfigExpander resolves ${name} references in a value, bounded in depth and
// refusing a cycle.
//
// pkg-config substitutes a variable as it reads the file, so a reference to a
// variable defined further down stays empty there and resolves here. That
// difference is deliberate, and it can only ever make this reader see more
// rather than less: whatever it resolves still has to survive verification
// against the file on disk before anything is mapped (D37).
type pkgConfigExpander struct {
	variables map[string]string
	resolved  map[string]string
	visiting  map[string]bool
}

func newPkgConfigExpander(variables map[string]string) *pkgConfigExpander {
	return &pkgConfigExpander{
		variables: variables,
		resolved:  map[string]string{},
		visiting:  map[string]bool{},
	}
}

// variable is the expanded value of one definition, or the empty string for one
// nothing defines or one that could not be resolved.
func (e *pkgConfigExpander) variable(name string) (string, error) {
	raw, defined := e.variables[name]
	if !defined {
		return "", nil
	}
	if value, done := e.resolved[name]; done {
		return value, nil
	}
	value, err := e.value(raw)
	if err != nil {
		return "", err
	}
	e.resolved[name] = value
	return value, nil
}

// value expands a keyword's value. An undefined reference makes the whole value
// unusable rather than partly substituted: half a directory is a directory that
// does not exist, and comparing a file against it would be nonsense.
func (e *pkgConfigExpander) value(raw string) (string, error) {
	expanded, err := e.expand(raw, 0)
	if errors.Is(err, errPkgConfigUndefined) {
		return "", nil
	}
	return expanded, err
}

// errPkgConfigUndefined marks a value naming a variable nothing defines. It
// never leaves this file: the caller turns it into an empty value, which is
// silence, because a .pc file with an override this run does not supply is not
// a broken file.
var errPkgConfigUndefined = errors.New("names an undefined variable")

func (e *pkgConfigExpander) expand(raw string, depth int) (string, error) {
	if raw == "" {
		return "", nil
	}
	if depth >= maxPkgConfigExpansionDepth {
		return "", limits.ErrInputLimitExceeded
	}
	var out strings.Builder
	for index := 0; index < len(raw); index++ {
		if raw[index] != '$' {
			out.WriteByte(raw[index])
			continue
		}
		if index+1 < len(raw) && raw[index+1] == '$' {
			// "$$" is how the format writes a literal dollar sign.
			out.WriteByte('$')
			index++
			continue
		}
		if index+1 >= len(raw) || raw[index+1] != '{' {
			// A bare dollar sign is not a reference, and pkg-config leaves it be.
			out.WriteByte('$')
			continue
		}
		end := strings.IndexByte(raw[index+2:], '}')
		if end < 0 {
			return "", errors.New("holds an unterminated variable reference")
		}
		name := raw[index+2 : index+2+end]
		index += 2 + end
		nested, defined := e.variables[name]
		if !defined {
			return "", errPkgConfigUndefined
		}
		if e.visiting[name] {
			// A cycle is not a value at any depth, and following it is what the
			// bound of section 30 exists to stop.
			return "", limits.ErrInputLimitExceeded
		}
		e.visiting[name] = true
		value, err := e.expand(nested, depth+1)
		delete(e.visiting, name)
		if err != nil {
			return "", err
		}
		out.WriteString(value)
	}
	return out.String(), nil
}
