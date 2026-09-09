package pkgmanager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// This file reads the lock file CPM.cmake writes, and it is deliberately not
// an adapter (section 21).
//
// CPM is a CMake-level wrapper around FetchContent: it calls
// FetchContent_MakeAvailable, so what lands in the build tree is FetchContent's
// own evidence -- the generated populate script and _deps/<name>-src beside
// _deps/<name>-build. The fetchcontent adapter therefore already finds every
// CPM package, and a second adapter claiming the same roots would not be a
// second opinion but a second owner: Discover turns the later claimant away
// whole and reports COMPONENT_MAPPING_CONFLICT, which would fire once per
// dependency in every CPM project and throw away everything the loser knew.
// Whichever way round the two were registered, something true would be lost --
// the lock's version, or the checkout's own answer that outranks both.
//
// What the lock actually is, is a second origin for a package that already
// exists, which is the case section 21.1 is built for: the claims are ranked
// per field, the winner is published and the loser is kept in Superseded. So
// fetchcontent.Discover reads this file once per run and offers what it says
// to the packages it found.
//
// The file read is <build>/cpm-package-lock.cmake and nothing else. That path
// is fixed in CPM.cmake and rewritten on every configure, so it is install
// state. The copy a project commits to its source tree -- the one
// CPMUsePackageLock names -- is not read: its path is whatever the project
// chose, and its content is allowed to be older than the build tree in front
// of us.
//
// CMake is not interpreted here. The reader knows the seven keys below and
// ignores every other line, because interpreting a build system to read a
// declaration out of it is a much larger promise than this file makes.

// cpmLockName is the whole surface this reader touches. A reviewer can name
// every path it can open by reading this line.
const cpmLockName = "cpm-package-lock.cmake"

// The bounds of section 30. One short block per package makes a megabyte
// generous; the entry count is bounded separately because a file that is small
// in bytes can still name an absurd number of packages.
const (
	maxCPMLockBytes   = 1 << 20
	maxCPMLockEntries = 10_000
)

// cpmEntry is one CPMDeclarePackage block, as far as this reader cares about
// it. The repository is whichever of the four repository keys the block
// carried, already expanded to a URL.
//
// The tag is kept but never published. The populate script beside the checkout
// says which revision CMake actually checked out, which is the same question
// answered from install state rather than from a declaration, so the lock's tag
// would add nothing over it -- but two blocks for one package that agree on
// everything except the tag still contradict each other, and holding the tag
// here is what lets that be seen.
type cpmEntry struct {
	name       string
	version    string
	gitTag     string
	repository string
}

// readCPMLock reads the lock file into a lookup keyed by the lower-cased
// package name, because that is the name FetchContent gives the directories in
// _deps and therefore the only name the two sides have in common.
//
// A missing lock is not reported: a project that does not use CPM has nothing
// missing about it, and CPM itself writes a header-only file whenever
// CPMUsePackageLock was never called, which is the common case and must stay
// silent too.
func readCPMLock(buildDir string) (map[string]cpmEntry, []domain.Finding) {
	if buildDir == "" {
		return nil, nil
	}
	path := filepath.Join(buildDir, cpmLockName)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, nil
	}
	if info.Size() > maxCPMLockBytes {
		// Refused whole rather than in part, as the vcpkg file list and the
		// ESP-IDF lock are: half a lock file would give some packages the
		// version CPM recorded and leave the rest with the tag the populate
		// script happens to name, with nothing in the document saying why.
		return nil, []domain.Finding{cpmEvidenceFinding("INPUT_LIMIT_EXCEEDED", path,
			"the CPM package lock is larger than the parser limit of section 30, so nothing was read from it")}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, []domain.Finding{cpmEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the CPM package lock could not be opened, so nothing was taken from it")}
	}
	defer file.Close()

	entries, err := parseCPMLock(file)
	if err != nil {
		return nil, []domain.Finding{cpmEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the CPM package lock %s, so nothing was taken from it", err.Error()))}
	}
	if len(entries) > maxCPMLockEntries {
		return nil, []domain.Finding{cpmEvidenceFinding("INPUT_LIMIT_EXCEEDED", path,
			"the CPM package lock names more packages than the parser limit of section 30, so nothing was read from it")}
	}

	locked := make(map[string]cpmEntry, len(entries))
	contested := map[string]bool{}
	for _, entry := range entries {
		key := strings.ToLower(entry.name)
		if contested[key] {
			continue
		}
		previous, seen := locked[key]
		if seen && previous != entry {
			// Two blocks describing one package differently are not two
			// statements but none: section 19.2 drops a file two packages claim
			// for the same reason, and picking the earlier one here would make
			// the published version depend on the order CPM happened to
			// configure in.
			delete(locked, key)
			contested[key] = true
			continue
		}
		locked[key] = entry
	}
	return locked, nil
}

// parseCPMLock reads the blocks out of an open lock file. It is a line reader
// and not a CMake parser: a block starts at CPMDeclarePackage(, ends at the
// closing parenthesis on its own line, and inside it only the seven known keys
// are read.
//
// A block that is never closed makes the file's structure something this
// reader does not understand, so the whole file is refused rather than the one
// block: the following lines are then being read in a context that was guessed,
// and a version guessed out of a misread file is exactly what section 20.1
// forbids.
func parseCPMLock(file io.Reader) ([]cpmEntry, error) {
	entries := make([]cpmEntry, 0)
	var current *cpmEntry
	scanner := limits.Scanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// A comment carries no claim. That also settles the blocks CPM writes
		// for a package it could not version -- "# <name> (unversioned)" with
		// the whole declaration commented out beneath it -- which are a record
		// of what CPM could not say, not a claim it made.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if current == nil {
			if rest, ok := strings.CutPrefix(line, "CPMDeclarePackage("); ok {
				current = &cpmEntry{name: strings.TrimSpace(rest)}
			}
			continue
		}
		if line == ")" {
			// A block that names no package cannot be joined to a directory in
			// _deps, so it is dropped. It is not reported: the file was read
			// as it is written, and nothing about it was refused.
			if current.name != "" {
				entries = append(entries, *current)
			}
			current = nil
			continue
		}
		key, value, hasValue := strings.Cut(line, " ")
		if !hasValue {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if value == "" {
			continue
		}
		switch key {
		case "NAME":
			current.name = value
		case "VERSION":
			current.version = value
		case "GIT_TAG":
			current.gitTag = value
		case "GIT_REPOSITORY":
			current.repository = NormalizeVCSURL(value)
		case "GITHUB_REPOSITORY":
			current.repository = NormalizeVCSURL("https://github.com/" + value)
		case "GITLAB_REPOSITORY":
			current.repository = NormalizeVCSURL("https://gitlab.com/" + value)
		case "BITBUCKET_REPOSITORY":
			current.repository = NormalizeVCSURL("https://bitbucket.org/" + value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("could not be read to its end (%v)", err)
	}
	if current != nil {
		return nil, fmt.Errorf("ends inside a CPMDeclarePackage block")
	}
	return entries, nil
}

// cpmEvidenceFinding reports a lock file that was found but could not be used.
// The subject is the file rather than a component: what could not be read is a
// piece of evidence, and the packages it would have described are found by the
// FetchContent adapter either way.
func cpmEvidenceFinding(id, path, message string) domain.Finding {
	return domain.Finding{
		ID: id, Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: path},
		Message: message,
	}
}

// cpmVersionSource is the source string a version claim from the lock carries.
// It reaches the document beside the technique of section 20.3, so it is
// output rather than an internal label, and internal/cyclonedx maps it onto
// manifest-analysis.
const cpmVersionSource = "cpm"

// cpmManager is what a package the lock names reports as detectedBy. The lock
// is the record of the tool that actually fetched the dependency, and naming
// FetchContent there would credit the mechanism rather than the manager the
// project uses.
const cpmManager = "cpm"
