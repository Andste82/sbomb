package pkgmanager

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// This file reads the manifest an embedded-Linux distribution build writes
// about the image it produced: Yocto's license.manifest and Buildroot's
// legal-info/manifest.csv. For an image that is the most complete statement
// there is -- name, version and licence for every package, written by the
// build itself.
//
// It is deliberately neither an adapter nor an enricher, and the reason is
// what these files are. They describe an **image**, not a CMake project. An
// adapter enumerates packages into the run: every entry would become a package
// with a root, would claim files by path prefix and would be reported as
// PACKAGE_NOT_LINKED when nothing matched it -- 797 findings for the 800-entry
// manifest of an image in which this build links three libraries. Discover
// drops a package that names no root, so a rootless metadata-only entry cannot
// take that door either. An enricher is closer, but it is defined as reading
// the files lying in a directory that already stands as a component root,
// while this reads a file the user named, somewhere outside the build tree,
// and matches it against a component by name.
//
// So this is a third kind of reader: a second origin, keyed by name, for
// components that already exist. It returns Contribution values and nothing
// else -- Field plus Claim, with no path, no root and no anchor anywhere in
// the type -- so an image manifest cannot add a file to the used set, cannot
// create a component and cannot move a boundary. The guarantee is structural,
// as it is for an enricher (D33), and not a matter of discipline.
//
// An entry that matches no component is silence. PACKAGE_NOT_LINKED exists for
// a manager that installed a dependency for this build; an image manifest
// describes a whole image, where hundreds of unmatched entries are the normal
// case rather than a fault, and a finding per entry would drown the report.
// The counts go to the log instead (section 39.3).

// The sources these two formats are published under. They reach the reader
// through component.VersionSource, where section 20.3 maps them onto the
// closed CycloneDX technique vocabulary, so they are output rather than an
// internal label.
const (
	yoctoSource     = "yocto"
	buildrootSource = "buildroot"
)

// The bounds of section 30. An image manifest is one short line or one short
// block per package, so eight megabytes is generous for the thousands of
// packages a distribution image holds; the entry count is bounded separately
// because a file that is small in bytes can still name an absurd number of
// packages.
const (
	maxDistroManifestBytes   = 8 << 20
	maxDistroManifestEntries = 100_000
)

// distroEntry is one package as an image manifest describes it. The name is
// the one the manifest states for the package. A Yocto entry carries aliases
// as well -- the recipe the package was built from -- because a component in a
// build tree may be named after either, but an alias is this reader's own
// addition and is weighed as such below.
type distroEntry struct {
	name    string
	aliases []string
	version string
	license string
}

// distroClaim is one value together with the file it was read from. The path
// is kept only to report a disagreement: two entries that state different
// versions for one name have to be able to say where each of them came from.
type distroClaim struct {
	value  string
	source string
	path   string
}

// DistroSource is what one manifest contributed, for the run's log. Section
// 39.3 keeps counts in the log rather than in findings, and a user who
// configured a manifest wants to see that it was read and how much was in it.
type DistroSource struct {
	Path    string
	Entries int
}

// DistroMetadata is what the configured image manifests state, keyed by
// package name. It is built once per run and shared: the same index answers
// the package-manager discovery and, later, every component the resolver did
// not resolve through a manager.
type DistroMetadata struct {
	byName  map[string][]Contribution
	sources []DistroSource
}

// Describe returns what the manifests state about a component of this name, or
// nothing for a name none of them mentions -- which is the ordinary case and
// says nothing about the component.
//
// The nil receiver is meaningful: no manifest was configured, so nothing is
// described, and no caller has to guard the call.
func (d *DistroMetadata) Describe(name string) []Contribution {
	if d == nil || name == "" {
		return nil
	}
	return d.byName[name]
}

// Sources reports what each manifest contributed, in the order the manifests
// were read.
func (d *DistroMetadata) Sources() []DistroSource {
	if d == nil {
		return nil
	}
	return d.sources
}

// ReadDistroManifests reads the manifests a run was configured with. The paths
// are the ones to open, already resolved by the caller against the project
// root, because where a relative path is anchored is the configuration's
// question and not this reader's.
//
// Nothing is searched for: every file opened here was named by the user. A
// path that is not there is reported, because a typo in a configured path must
// be visible rather than looking like an image with nothing in it.
func ReadDistroManifests(paths []string) (*DistroMetadata, []domain.Finding) {
	if len(paths) == 0 {
		return nil, nil
	}
	metadata := &DistroMetadata{byName: map[string][]Contribution{}}
	findings := make([]domain.Finding, 0)

	// A package name is what a manifest states about itself; a Yocto recipe
	// name is this reader's own second key for the same entry. The two are not
	// weighed alike, and they are therefore collected apart.
	stated := newDistroIndex()
	aliased := newDistroIndex()

	seen := map[string]bool{}
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		entries, source, manifestFindings := readDistroManifest(path)
		findings = append(findings, manifestFindings...)
		if source == "" {
			continue
		}
		metadata.sources = append(metadata.sources, DistroSource{Path: path, Entries: len(entries)})
		for _, entry := range entries {
			stated.take(entry.name, FieldVersion, entry.version, source, path)
			stated.take(entry.name, FieldLicense, entry.license, source, path)
			for _, alias := range entry.aliases {
				aliased.take(alias, FieldVersion, entry.version, source, path)
				aliased.take(alias, FieldLicense, entry.license, source, path)
			}
		}
	}

	// Sorted, because a map is iterated in a different order on every run and
	// two runs over one image have to produce the same document and the same
	// findings in the same order.
	names := make([]string, 0, len(stated.claims)+len(aliased.claims))
	for name := range stated.claims {
		names = append(names, name)
	}
	for name := range aliased.claims {
		if _, both := stated.claims[name]; !both {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		// The fields are folded in a fixed order for the same reason.
		for _, field := range []Field{FieldVersion, FieldLicense} {
			if sides := stated.contested[name][field]; len(sides) > 0 {
				// A manifest that states two versions for one package name
				// contradicts itself, and that is worth reporting: the name the
				// report is about is one the manifest itself put there.
				findings = append(findings, distroConflictFinding(name, field, sides)...)
				continue
			}
			claim, taken := stated.claims[name][field]
			if !taken {
				if len(aliased.contested[name][field]) > 0 {
					// A contested alias is dropped without a word. Two packages
					// of one Yocto recipe stating two licences -- what
					// LICENSE:${PN} does, and ordinary in a real image -- is no
					// contradiction in the manifest at all: the disagreement
					// would be manufactured by this reader's own recipe key, and
					// the name it is about need not exist in the build at all. A
					// finding for it would let the size of the image rather than
					// the size of the build decide how long the report is.
					continue
				}
				claim, taken = aliased.claims[name][field]
			}
			if !taken {
				continue
			}
			metadata.byName[name] = append(metadata.byName[name], Contribution{
				Field: field,
				Claim: Claim{
					Value:  claim.value,
					Source: claim.source,
					// Rank 3 of section 21.1: the manifest is what the image
					// build recorded after building and installing, which is
					// the same category as a lock file or an installed-file
					// list. It therefore loses to an SBOM the upstream shipped
					// and to the checkout itself, and at equal rank to the
					// manager that installed the package, because this reader
					// is asked after the adapter.
					Rank: RankInstallState,
					// Section 20.3 has a confidence table for the version and
					// for no other field, so the other one stays empty rather
					// than inventing a value for it.
					Confidence: confidenceForDistroField(field),
				},
			})
		}
	}
	return metadata, findings
}

// confidenceForDistroField is the confidence of section 20.3, which exists for
// the version alone.
func confidenceForDistroField(field Field) domain.Confidence {
	if field == FieldVersion {
		return domain.ConfidenceHigh
	}
	return ""
}

// distroIndex is what the manifests state for each name and field, together
// with the fields a second entry contradicted. A contested field is dropped
// from the index entirely rather than settled by whichever entry came first:
// two statements are no statement, which is why section 19.2 drops a file two
// packages claim as well.
type distroIndex struct {
	claims    map[string]map[Field]distroClaim
	contested map[string]map[Field][]domain.ConflictSide
}

func newDistroIndex() *distroIndex {
	return &distroIndex{
		claims:    map[string]map[Field]distroClaim{},
		contested: map[string]map[Field][]domain.ConflictSide{},
	}
}

// take records what one entry states about one field of one name.
func (i *distroIndex) take(name string, field Field, value, source, path string) {
	if name == "" || value == "" {
		return
	}
	if i.claims[name] == nil {
		i.claims[name] = map[Field]distroClaim{}
	}
	previous, seen := i.claims[name][field]
	if !seen {
		i.claims[name][field] = distroClaim{value: value, source: source, path: path}
		return
	}
	if previous.value == value {
		// Two entries saying the same thing are not a disagreement. A Yocto
		// recipe is named once per package it produced, and nearly always with
		// one version for all of them.
		return
	}
	if i.contested[name] == nil {
		i.contested[name] = map[Field][]domain.ConflictSide{}
	}
	if len(i.contested[name][field]) == 0 {
		i.contested[name][field] = append(i.contested[name][field],
			domain.ConflictSide{Source: previous.path, Value: previous.value})
	}
	i.contested[name][field] = append(i.contested[name][field],
		domain.ConflictSide{Source: path, Value: value})
}

// distroConflictFinding reports a field two entries state differently for one
// package name. It returns a slice because a conflict with nothing to say
// produces no finding at all.
func distroConflictFinding(name string, field Field, sides []domain.ConflictSide) []domain.Finding {
	// The sides are put in a fixed order rather than in the order the manifests
	// were configured in: the same two manifests named the other way round are
	// the same disagreement, and a report that reads differently for it would
	// look like a second one.
	sort.Slice(sides, func(i, j int) bool {
		if sides[i].Source != sides[j].Source {
			return sides[i].Source < sides[j].Source
		}
		return sides[i].Value < sides[j].Value
	})
	conflict := domain.Conflict{
		Field:   string(field) + " an image manifest states for this package",
		Subject: domain.Subject{Kind: "component", Ref: name},
		Sides:   sides,
		Reason: "two statements are no statement, so the image manifest describes nothing here " +
			"and the component keeps whatever its own evidence said",
	}
	if finding, ok := conflict.Finding("COMPONENT_MAPPING_CONFLICT", domain.SeverityInfo); ok {
		return []domain.Finding{finding}
	}
	return nil
}

// readDistroManifest reads one manifest whole. The second result names the
// format that was recognized, and is empty for a file nothing was taken from,
// so that a file which could not be read is not counted as an image with no
// packages in it.
//
// Everything is refused whole or taken whole. Half a manifest would give some
// components the version the image build recorded and leave the rest with
// whatever their own evidence said, with nothing in the document saying which
// is which -- which is an invented answer rather than a weaker one.
func readDistroManifest(path string) ([]distroEntry, string, []domain.Finding) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// The same report a configured native manifest gets when it is not
			// there: the path was named by the user, so a typo has to be
			// visible instead of passing as an image nobody described.
			return nil, "", []domain.Finding{{
				ID: "MISSING_PACKAGE_EVIDENCE", Severity: domain.SeverityWarning,
				Subject: domain.Subject{Kind: "configuration", Ref: path},
				Message: "the configured image manifest was not found, so nothing was read from it",
			}}
		}
		return nil, "", []domain.Finding{distroEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the configured image manifest could not be opened, so nothing was taken from it")}
	}
	if info.IsDir() {
		return nil, "", []domain.Finding{distroEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the configured image manifest is a directory, so nothing was taken from it")}
	}
	if info.Size() > maxDistroManifestBytes {
		// Checked before the read, so a file over the bound is never allocated.
		return nil, "", []domain.Finding{distroEvidenceFinding("INPUT_LIMIT_EXCEEDED", path,
			"the image manifest is larger than the parser limit of section 30, so nothing was read from it")}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", []domain.Finding{distroEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the image manifest could not be read, so nothing was taken from it")}
	}

	// The format is recognized from the file's first line rather than from its
	// name, so that a manifest copied out of a deploy directory under another
	// name still works.
	source := distroFormat(data)
	var entries []distroEntry
	switch source {
	case yoctoSource:
		entries, err = parseYoctoLicenseManifest(data)
	case buildrootSource:
		entries, err = parseBuildrootManifest(data)
	default:
		return nil, "", []domain.Finding{distroEvidenceFinding("EVIDENCE_UNREADABLE", path,
			"the file is neither a Yocto license manifest nor a Buildroot legal-info manifest, so nothing was taken from it")}
	}
	if err != nil {
		if errors.Is(err, limits.ErrInputLimitExceeded) {
			return nil, "", []domain.Finding{distroEvidenceFinding("INPUT_LIMIT_EXCEEDED", path,
				"the image manifest names more packages than the parser limit of section 30, so nothing was read from it")}
		}
		return nil, "", []domain.Finding{distroEvidenceFinding("EVIDENCE_UNREADABLE", path,
			fmt.Sprintf("the image manifest %s, so nothing was taken from it", err.Error()))}
	}
	return entries, source, nil
}

// distroFormat names the format of a manifest from its first non-empty line: a
// CSV header naming a package and a version column is Buildroot's, a
// "PACKAGE NAME:" line is Yocto's, and anything else is neither.
func distroFormat(data []byte) string {
	line := firstNonEmptyLine(data)
	if line == "" {
		return ""
	}
	if header, err := csv.NewReader(strings.NewReader(line)).Read(); err == nil && len(header) <= limits.MaxTokensPerLine {
		columns := map[string]bool{}
		for _, field := range header {
			columns[strings.ToUpper(strings.TrimSpace(field))] = true
		}
		if columns["PACKAGE"] && columns["VERSION"] {
			return buildrootSource
		}
	}
	if key, _, found := strings.Cut(line, ":"); found && strings.TrimSpace(key) == yoctoPackageName {
		return yoctoSource
	}
	return ""
}

// firstNonEmptyLine returns the first line of a file that holds anything, with
// the trailing carriage return of a manifest written on Windows removed. A
// line longer than the bound of section 30 makes the file one this reader will
// not recognize, which is the refusal the caller reports.
func firstNonEmptyLine(data []byte) string {
	scanner := limits.Scanner(bytes.NewReader(data))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			return line
		}
	}
	return ""
}

// The four keys a Yocto license manifest states per package. Every other key
// is ignored, in the way the CPM lock reader ignores every line that is not
// one of its seven: reading a key this file does not know would be
// interpreting a format rather than reading it.
const (
	yoctoPackageName    = "PACKAGE NAME"
	yoctoPackageVersion = "PACKAGE VERSION"
	yoctoRecipeName     = "RECIPE NAME"
	yoctoLicense        = "LICENSE"
)

// parseYoctoLicenseManifest reads the blocks of a license.manifest: "KEY: value"
// lines, one blank line between packages.
//
// A line that is neither blank nor a key with a value makes the file's
// structure something this reader does not understand, so the whole file is
// refused rather than the one line: the lines after it would then be read in a
// context that was guessed, and a version guessed out of a misread file is
// exactly what section 20.1 forbids. A block that names no package is the same
// case -- the block boundaries are what the format is made of, and a block
// without a name means they were not where this reader took them to be.
func parseYoctoLicenseManifest(data []byte) ([]distroEntry, error) {
	entries := make([]distroEntry, 0)
	var current distroEntry
	var recipe string
	open := false

	finish := func() error {
		if !open {
			return nil
		}
		if current.name == "" {
			return errors.New("holds a block that names no package")
		}
		if len(entries) >= maxDistroManifestEntries {
			// The same refusal the CSV reader makes, under the identifier the
			// caller maps to INPUT_LIMIT_EXCEEDED: a bound of section 30 was
			// reached, which is a different report from a file this reader
			// cannot make sense of.
			return limits.ErrInputLimitExceeded
		}
		if recipe != "" && recipe != current.name {
			// Indexed under the recipe as well as the package: a component in
			// a build tree may be named after either, and the two differ
			// whenever a recipe produces more than one package. It is an alias
			// and not a second name, because several packages of one recipe
			// land on it and may legitimately state different licences.
			current.aliases = append(current.aliases, recipe)
		}
		entries = append(entries, current)
		current, recipe, open = distroEntry{}, "", false
		return nil
	}

	scanner := limits.Scanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" {
			if err := finish(); err != nil {
				return nil, err
			}
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("holds a line that is not a %q entry", "KEY: value")
		}
		open = true
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case yoctoPackageName:
			if value != "" {
				current.name = value
			}
		case yoctoPackageVersion:
			current.version = value
		case yoctoRecipeName:
			recipe = value
		case yoctoLicense:
			current.license = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("could not be read to its end (%v)", err)
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return entries, nil
}

// The three columns of a Buildroot legal-info manifest this reader takes. The
// rest -- the licence files, the source archive, the site it came from --
// names paths and URLs, and this reader opens no path a manifest names and
// reaches no network.
const (
	buildrootPackageColumn = "PACKAGE"
	buildrootVersionColumn = "VERSION"
	buildrootLicenseColumn = "LICENSE"
)

// parseBuildrootManifest reads legal-info/manifest.csv with encoding/csv
// rather than by splitting on commas. A licence expression routinely contains
// one -- "GPL-2.0+, GPL-3.0+ with exceptions" -- and splitting would cut it in
// half and shift every column after it.
//
// The columns are read by their header name, not by position, and the field
// count is fixed by the header, so a row with a field too few is an error and
// not a shorter record. Such a row refuses the whole file: the row after it is
// then being read against a header that no longer describes it.
func parseBuildrootManifest(data []byte) ([]distroEntry, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	// A quote inside an unquoted field is a broken file rather than a literal
	// quote, and FieldsPerRecord left at zero makes the header fix the width
	// every following row has to have.
	reader.LazyQuotes = false
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, errors.New("has no header row")
	}
	columns := map[string]int{}
	for index, field := range header {
		columns[strings.ToUpper(strings.TrimSpace(field))] = index
	}
	name, hasName := columns[buildrootPackageColumn]
	if !hasName {
		return nil, fmt.Errorf("has no %q column", buildrootPackageColumn)
	}
	version, hasVersion := columns[buildrootVersionColumn]
	license, hasLicense := columns[buildrootLicenseColumn]

	entries := make([]distroEntry, 0)
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("could not be read as CSV (%v)", err)
		}
		if len(entries) >= maxDistroManifestEntries {
			return nil, limits.ErrInputLimitExceeded
		}
		entry := distroEntry{name: strings.TrimSpace(record[name])}
		if hasVersion {
			entry.version = strings.TrimSpace(record[version])
		}
		if hasLicense {
			entry.license = strings.TrimSpace(record[license])
		}
		// A row naming no package can be joined to no component. It is dropped
		// and not reported: the file was read as it is written, and nothing
		// about it was refused.
		if entry.name == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// distroEvidenceFinding reports a manifest that was found but could not be
// used. The subject is the file rather than a component: what could not be
// read is a piece of evidence, and the components it would have described are
// in the document either way, described by their own evidence.
func distroEvidenceFinding(id, path, message string) domain.Finding {
	return domain.Finding{
		ID: id, Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: path},
		Message: message,
	}
}
