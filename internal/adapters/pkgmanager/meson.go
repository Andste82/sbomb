package pkgmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// meson reads the wrap files a Meson project declares for its subprojects
// (section 21). It is strategy 2 of section 19.2 -- exact package-manager
// metadata -- for the dependency mechanism Meson projects actually use.
//
// It is an adapter and not an enricher, and the reason is where the file lies.
// A wrap is `<source>/subprojects/<name>.wrap`, while the subproject it
// describes is unpacked into `<source>/subprojects/<directory>/` beside it. An
// enricher is handed a settled component root and may read only what lies
// directly in it -- it does not descend and does not look at a sibling -- so
// the one reader that could see a wrap from the subproject's own root is a
// reader that is not allowed to exist. Reading it as an adapter is also the
// honest description of what it is: a wrap enumerates dependencies, which is
// exactly what an adapter does and what an enricher must not (D40).
//
// One known location, and no search: `<source>/subprojects/*.wrap` and nothing
// else. A project that has no subprojects directory is not opened at all.
//
// What is deliberately not read. A `[wrap-file]` states `source_url` and
// `source_filename` -- a tarball this tool did not download and a file name a
// version could only be guessed out of -- so such a wrap contributes no
// version, and `UNKNOWN_VERSION` says so. A `[wrap-git]` states `revision`,
// which is read exactly as west.go reads a project revision: a commit travels
// in the purl and is not published as a version, while a tag is the version the
// project asked for. `patch_directory`, `diff_files` and the `[provide]`
// section are not read either: they describe how Meson patches and how it maps
// a dependency name, not what the dependency is.
//
// And the rule of this package holds here as everywhere: a wrap whose
// directory is not on disk yields nothing. Meson unpacks a subproject while it
// configures, so a wrap with no directory is a dependency this build never
// obtained, and inventing a root for it would name a component owning nothing.
//
// Four helpers of west.go are called from here -- westSafeSegment,
// westResolvePath, westIsCommit and westVersionOf -- and the prefix is a
// historical name rather than a claim that Zephyr is involved. They are the
// same four questions a wrap raises: may this name stand as a path segment and
// as a component name, does this relative path stay inside the tree it is
// joined onto (section 30.3), is this revision a commit rather than a version
// (section 20.2), and does a leading v belong to the tag convention. Copying
// them under a meson prefix would be four more places for the rules of section
// 30.3 to drift apart.
type meson struct{}

func (meson) Manager() string { return "meson" }

// The entire file surface this adapter can open: wrap files directly in the
// subprojects directory of the source root. They are constants rather than
// literals so that a reviewer can name every path it reads from these lines.
const (
	mesonSubprojectsDir = "subprojects"
	mesonWrapPattern    = "*.wrap"
	mesonWrapSuffix     = ".wrap"
	mesonSource         = "meson"
)

// The bounds of section 30. A wrap is a dozen lines of INI, so a megabyte is
// generous; the number of wraps is bounded separately because a project can
// hold an absurd number of small files.
const (
	maxMesonWrapBytes = 1 << 20
	maxMesonWraps     = 10_000
)

func (a meson) Discover(options Options) ([]Package, []domain.Finding) {
	source := options.SourceDir
	if source == "" || source == "." {
		return nil, nil
	}
	subprojects := filepath.Join(source, mesonSubprojectsDir)
	if !dirExists(subprojects) {
		// A project that declares no subprojects has nothing missing about it,
		// so nothing is opened and nothing is reported.
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(subprojects, mesonWrapPattern))
	if err != nil || len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > maxMesonWraps {
		// Refused whole rather than in part: half the wraps would attribute
		// some subprojects and leave the rest looking as though the project
		// never declared them.
		return nil, []domain.Finding{mesonEvidenceFinding("INPUT_LIMIT_EXCEEDED", subprojects,
			"the project declares more wrap files than the parser limit of section 30, so no subproject was read")}
	}
	// filepath.Glob sorts its matches, so two runs over one tree agree on
	// which wrap claimed a directory first.

	findings := make([]domain.Finding, 0)
	packages := make([]Package, 0, len(matches))
	for _, path := range matches {
		entry, ok := a.packageFor(subprojects, path, &findings)
		if !ok {
			continue
		}
		packages = append(packages, entry)
	}
	return packages, findings
}

// packageFor turns one wrap file into a package, or explains why it could not.
func (a meson) packageFor(subprojects, path string, findings *[]domain.Finding) (Package, bool) {
	name := strings.TrimSuffix(filepath.Base(path), mesonWrapSuffix)
	if !westSafeSegment(name) {
		// The wrap's file name is the subproject name Meson addresses it by, so
		// a name that cannot stand as one path segment cannot name a component
		// either. This is the rule espidf.go and west.go apply to a key out of
		// somebody else's file, for the same reason.
		*findings = append(*findings, mesonEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the wrap file is named %q, which is not a name this tool can resolve to a subproject, so it was skipped", name)))
		return Package{}, false
	}
	values, section, ok := a.read(path, findings)
	if !ok {
		return Package{}, false
	}
	if section == "" {
		// A file with this extension that declares no wrap section is not a
		// wrap. Reporting it keeps the subproject traceable to the file rather
		// than leaving it absent without explanation.
		*findings = append(*findings, mesonEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the wrap file declares no wrap section, so it describes no subproject"))
		return Package{}, false
	}

	// Meson defaults a subproject's directory to the wrap's own name, and
	// resolves either inside the subprojects directory.
	directory := values["directory"]
	if directory == "" {
		directory = name
	}
	root, resolved := westResolvePath(subprojects, directory)
	if !resolved {
		// Section 30.3: a wrap is somebody else's file, and a directory of
		// "/etc" or "../.." must not be able to point a component root out of
		// the source tree.
		*findings = append(*findings, mesonEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the wrap file puts its subproject at %q, which does not lie inside the subprojects directory, so it was skipped", directory)))
		return Package{}, false
	}
	if !dirExists(root) {
		// Meson unpacks a subproject while it configures, and a wrap whose
		// directory is not there describes a dependency this build never
		// obtained. That is silence rather than missing evidence, exactly as
		// west.go treats a project it never cloned.
		return Package{}, false
	}

	entry := Package{
		Name:      name,
		Roots:     []string{root},
		Manager:   a.Manager(),
		AnchorKey: "pkg:meson/" + name,
	}
	// Only a git wrap states a repository. `source_url` of a file wrap is
	// where a tarball was downloaded from, which is not a VCS URL and must not
	// be published as one.
	if section == "wrap-git" {
		entry.VCSURL = NormalizeVCSURL(values["url"])
		revision := values["revision"]
		if westIsCommit(revision) {
			// Section 20.2 point 5: a commit is published as a version only
			// when the configuration asks for it. It travels in the purl and
			// in the vcsCommit property instead.
			entry.Commit = revision
		} else {
			entry.Take(FieldVersion, Claim{
				Value: westVersionOf(revision), Source: mesonSource, Rank: RankDeclaredManifest,
				// Section 20.3: a package manager states an exact declared
				// version.
				Confidence: domain.ConfidenceHigh,
			})
		}
		// The purl restates whichever version claim won, so it is taken with
		// that claim's standing; where the wrap pinned a commit there is no
		// version and the purl rests on the wrap alone.
		purlRank := entry.Version.Rank
		if purlRank == RankNone {
			purlRank = RankDeclaredManifest
		}
		entry.Take(FieldPURL, Claim{
			Value:  GenericPURL(entry.Name, entry.Version.Value, entry.VCSURL, entry.Commit),
			Source: a.Manager(), Rank: purlRank,
		})
	}
	if entry.Version.Value == "" {
		message := "the wrap file states no revision for this subproject"
		switch {
		case entry.Commit != "":
			message = "the wrap file pins this subproject to a commit, which section 20.2 does not publish as a version"
		case section != "wrap-git":
			message = "the wrap file names an archive rather than a revision, and a version read out of a file name would be a guess"
		}
		*findings = append(*findings, domain.Finding{
			ID: "UNKNOWN_VERSION", Severity: domain.SeverityWarning,
			Subject:     domain.Subject{Kind: "component", Ref: entry.Name},
			Message:     message,
			Remediation: "Set components[].version for this subproject.",
		})
	}
	return entry, true
}

// read reads one wrap file and returns its keys and the wrap section it
// declares. The format is the git-config subset .gitmodules and .west/config
// are written in, so it is read the same way submodule.go and west.go read
// those: a section in brackets, `key = value` beneath it, and nothing else.
//
// Only the first wrap section is read. A file declaring `[wrap-file]` and
// `[wrap-git]` both is a contradiction about how the subproject was obtained,
// and the keys of the two sections mean different things, so folding them
// together would describe a subproject neither section states.
func (meson) read(path string, findings *[]domain.Finding) (map[string]string, string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		// A name the glob returned that is not a readable file is not evidence
		// somebody meant to leave here.
		return nil, "", false
	}
	if info.Size() > maxMesonWrapBytes {
		// Asked before the file is opened, so a wrap over the ceiling costs a
		// stat rather than a megabyte of allocation.
		*findings = append(*findings, mesonEvidenceFinding("INPUT_LIMIT_EXCEEDED", path,
			"the wrap file is larger than the parser limit of section 30, so nothing was read from it"))
		return nil, "", false
	}
	file, err := os.Open(path)
	if err != nil {
		*findings = append(*findings, mesonEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the wrap file could not be read, so nothing was taken from it"))
		return nil, "", false
	}
	defer func() { _ = file.Close() }()

	values := map[string]string{}
	section := ""
	inWrap := false
	scanner := limits.Scanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			header := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			if section == "" && strings.HasPrefix(header, "wrap-") {
				section = header
				inWrap = true
				continue
			}
			// Every other section belongs to somebody else: `[provide]` maps
			// dependency names, and a second wrap section contradicts the
			// first.
			inWrap = false
			continue
		}
		if !inWrap {
			continue
		}
		key, value, split := strings.Cut(line, "=")
		if !split {
			continue
		}
		key = strings.TrimSpace(key)
		if _, taken := values[key]; taken {
			// The first statement wins, as everywhere else here, so that two
			// runs over one file agree.
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		*findings = append(*findings, mesonEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the wrap file could not be read to its end, so nothing was taken from it"))
		return nil, "", false
	}
	return values, section, true
}

// mesonEvidenceFinding reports a wrap that was found but could not be used. The
// subject is the file rather than the component: what could not be read is a
// piece of evidence, and there may not be a component to name yet.
func mesonEvidenceFinding(id, path, message string) domain.Finding {
	return domain.Finding{
		ID: id, Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: path},
		Message: message,
	}
}
