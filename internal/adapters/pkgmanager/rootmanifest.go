package pkgmanager

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// This file holds four enrichers, one per build system that keeps its
// declaration in a single file at the root of the thing it builds: a CMake
// package-version file, a build2 package manifest, an xmake description and a
// Bazel module. Each is a reader of well under a hundred lines and all four
// share the same bounded line reader below, so they live together rather than
// in four files of their own -- the convention of one file per format
// (cmsispack.go, bundledsbom.go) is broken here deliberately, because four
// files would each hold a dozen lines of reader and a copy of the same
// plumbing.
//
// What they have in common is more than their size. None of them interprets
// the language its file is written in: CMake, Lua and Starlark are programming
// languages, and evaluating one to learn a version would mean running somebody
// else's code, which section 30 and the architecture forbid outright. Each
// reader therefore looks for exactly one assignment in exactly one shape, and a
// file that does not hold that shape is not read rather than approximated.
//
// All four contribute at rank 2 of section 21.1, the manifest the owning
// manager declares. That is a deliberate understatement for the CMake file,
// which a project generates while installing and which section 21.1 would
// otherwise call install state (rank 3): a claim ranked too low can only lose
// to a better origin, while one ranked too high would overrule the manager
// that really installed the package. D40 records the choice.
//
// None of them contributes a name. A name has no home in the Contribution
// contract -- Field names four values and none of them is the name -- and a
// component keeps the name that was settled before its files were grouped
// (D33). The same holds for a build2 `summary:`, which has no home at all.

// The paths these readers can open, each one directly in the root they were
// handed. They are constants rather than literals so that a reviewer can name
// the entire file surface of this file by reading these lines.
const (
	// Both spellings CMake produces: write_basic_package_version_file writes
	// whatever name the project passes, and the two conventions in the wild
	// are <Pkg>ConfigVersion.cmake and <pkg>-config-version.cmake.
	cmakeConfigVersionPattern    = "*ConfigVersion.cmake"
	cmakeConfigVersionAltPattern = "*-config-version.cmake"
	build2ManifestName           = "manifest"
	xmakeManifestName            = "xmake.lua"
	bazelModuleName              = "MODULE.bazel"
)

// The origins these readers publish, as component.VersionSource names them.
// Every one of them is mapped in internal/cyclonedx techniqueForVersionSource;
// a source that is not mapped there falls silently to "other".
const (
	cmakeConfigVersionSource = "cmake-config-version"
	build2Source             = "build2"
	xmakeSource              = "xmake"
	bazelSource              = "bazel"
)

// The bounds of section 30, one per format, because each file has a different
// honest shape. A generated CMake version file and a build2 manifest are a few
// dozen lines; an xmake.lua and a MODULE.bazel are hand-written build
// descriptions that can legitimately run to some thousands. Bytes and lines are
// bounded separately: a file that is small in bytes can still be a million
// empty lines, and each line costs a scan whatever it holds.
const (
	maxCMakeConfigVersionBytes = 1 << 20
	maxCMakeConfigVersionLines = 20_000
	maxBuild2ManifestBytes     = 1 << 20
	maxBuild2ManifestLines     = 20_000
	maxXmakeBytes              = 4 << 20
	maxXmakeLines              = 100_000
	maxBazelModuleBytes        = 4 << 20
	maxBazelModuleLines        = 100_000
	// The module() call of a MODULE.bazel is a handful of keyword arguments.
	// A call that runs longer than this is not the shape this reader knows,
	// and following it further would be reading Starlark rather than one
	// declaration out of it.
	maxBazelModuleCallLines = 200
)

// cmakeConfigVersion reads the package-version file CMake writes beside a
// package's config file. It states one thing this tool wants -- PACKAGE_VERSION
// -- and states it as a literal, which is the only reason it can be read
// without interpreting CMake.
//
// It will fire less often than it looks like it should, and that is worth
// knowing rather than discovering: an installed package puts this file under
// lib/cmake/<Pkg>/, which is not the directory that stands as the component
// root, and an enricher may not descend to go and find it. Where it does lie
// in the root -- a small project that installs its config files flat, a
// checkout that generated one in place -- it is often the only declaration
// there is.
type cmakeConfigVersion struct{}

func (cmakeConfigVersion) Source() string { return cmakeConfigVersionSource }

// cmakePackageVersionPattern is the one assignment this reader understands.
// The value is deliberately narrow: a bare word or a quoted string, and
// nothing that could be a variable reference. conan.go's pattern accepts
// `${PROJECT_VERSION}` and would publish the fragment `${PROJECT_VERSION` as a
// version, which is a value nobody stated; this reader refuses such a file
// instead of inheriting that.
var cmakePackageVersionPattern = regexp.MustCompile(`^\s*set\s*\(\s*PACKAGE_VERSION\s+"?([^"\s)]+)"?\s*\)`)

func (c cmakeConfigVersion) Enrich(root ComponentRoot) ([]Contribution, []domain.Finding) {
	path, findings := singleMatch(root.Path,
		"the component root holds more than one CMake package-version file, so which package it describes would be a guess and none of them was read",
		cmakeConfigVersionPattern, cmakeConfigVersionAltPattern)
	if path == "" {
		return nil, findings
	}
	lines, readFindings, ok := readManifestLines(path, maxCMakeConfigVersionBytes, maxCMakeConfigVersionLines,
		"CMake package-version file")
	if !ok {
		return nil, readFindings
	}

	version := ""
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		match := cmakePackageVersionPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		value := match[1]
		if strings.ContainsAny(value, "${}") {
			// A generated file states a literal. One that assigns a variable
			// here is a template nobody expanded, and the text of the
			// reference is not a version.
			return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
				"the CMake package-version file assigns PACKAGE_VERSION a value this tool cannot resolve without interpreting CMake, so nothing was taken from it")}
		}
		if version != "" && version != value {
			// Two assignments and no way to say which one the consumer of this
			// file would have seen: that is CMake control flow, and reading it
			// would be interpreting the language.
			return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
				"the CMake package-version file assigns PACKAGE_VERSION more than once with different values, so nothing was taken from it")}
		}
		version = value
	}
	if version == "" {
		// A file matching the name that states no version is not this kind of
		// file at all, and there is nothing missing about a component that has
		// none.
		return nil, nil
	}
	return []Contribution{{Field: FieldVersion, Claim: Claim{
		Value: version, Rank: RankDeclaredManifest,
		// Section 20.3: a package manager states an exact declared version.
		Confidence: domain.ConfidenceHigh,
	}}}, nil
}

// build2Manifest reads the package manifest of a build2 project: a file simply
// called `manifest`, holding `name: value` lines.
//
// That name belongs to half the world -- a Yocto image manifest, a container
// manifest, a text file somebody called manifest -- so the format line build2
// requires as the first thing in the file is what identifies it, and a file
// without that line is passed over in silence. Claiming every file of that name
// would attribute a stranger's data to a component.
type build2Manifest struct{}

func (build2Manifest) Source() string { return build2Source }

// build2FormatVersion is the manifest format version build2 writes at the top
// of every manifest it produces, as the line `: 1`. This reader knows version 1
// and refuses to guess at a later one.
const build2FormatVersion = "1"

func (b build2Manifest) Enrich(root ComponentRoot) ([]Contribution, []domain.Finding) {
	path := filepath.Join(root.Path, build2ManifestName)
	if !fileExists(path) {
		return nil, nil
	}
	lines, findings, ok := readManifestLines(path, maxBuild2ManifestBytes, maxBuild2ManifestLines, "build2 manifest")
	if !ok {
		return nil, findings
	}

	values := map[string]string{}
	format := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !format {
			// The first thing that is neither blank nor a comment must be the
			// format line, or this is not a build2 manifest.
			if !strings.HasPrefix(line, ":") || strings.TrimSpace(line[1:]) != build2FormatVersion {
				return nil, nil
			}
			format = true
			continue
		}
		name, value, split := strings.Cut(line, ":")
		if !split || name == "" || strings.ContainsAny(name, " \t") {
			// Every other line of this format is `name: value`. Anything else
			// is a shape this reader does not know -- a multi-line value, a
			// continuation, a second manifest appended to the first -- and a
			// value picked out of a file whose remainder could not be read is
			// invented. D40 records that a manifest using build2's multi-line
			// value syntax is refused here rather than read in part.
			return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
				"the build2 manifest holds a line this tool cannot read as `name: value`, so nothing was taken from it")}
		}
		name = strings.ToLower(name)
		value = strings.TrimSpace(value)
		if previous, stated := values[name]; stated && previous != value {
			return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
				"the build2 manifest states "+name+" twice with different values, so nothing was taken from it")}
		}
		values[name] = value
	}
	if !format {
		return nil, nil
	}

	contributions := make([]Contribution, 0, 2)
	if version := values["version"]; version != "" {
		contributions = append(contributions, Contribution{Field: FieldVersion, Claim: Claim{
			Value: version, Rank: RankDeclaredManifest, Confidence: domain.ConfidenceHigh,
		}})
	}
	// build2 allows a licence that is not an SPDX expression at all -- the
	// `other: <description>` form, and free text besides -- while
	// licenses[].expression is defined as one. A value that is not shaped like
	// an expression is therefore not published, and that is silence rather
	// than a refusal: the file was read, it simply states nothing this document
	// has a field for, exactly as a CMSIS descriptor's <license> path does.
	if license := values["license"]; isSPDXExpressionShaped(license) {
		contributions = append(contributions, Contribution{Field: FieldLicense, Claim: Claim{
			Value: license, Rank: RankDeclaredManifest,
		}})
	}
	if len(contributions) == 0 {
		return nil, nil
	}
	return contributions, nil
}

// xmakeManifest reads the version out of an xmake.lua.
//
// xmake.lua is Lua, and Lua is not interpreted here: the reader looks for a
// single `set_version("...")` call with a literal argument and knows nothing
// else about the file. A description that computes its version, or states it
// twice under different conditions, is refused rather than resolved -- picking
// one branch of somebody else's script is exactly the guess section 20.1
// forbids.
type xmakeManifest struct{}

func (xmakeManifest) Source() string { return xmakeSource }

// xmakeVersionPattern is the one call this reader understands. The argument
// list may go on -- set_version("1.0", {build = "%Y%m%d"}) is ordinary -- so
// only the first argument is captured and the rest of the line is ignored.
var xmakeVersionPattern = regexp.MustCompile(`^\s*set_version\s*\(\s*"([^"]*)"`)

func (x xmakeManifest) Enrich(root ComponentRoot) ([]Contribution, []domain.Finding) {
	return singleLiteralVersion(root.Path, xmakeManifestName, "xmake description", "Lua",
		maxXmakeBytes, maxXmakeLines, stripLuaComments, xmakeVersionPattern)
}

// bazelModule reads the version out of the module() call of a MODULE.bazel.
//
// MODULE.bazel is Starlark and is not interpreted either. The module()
// declaration is the first statement of the file by Bazel's own rule, it takes
// keyword arguments, and `version = "..."` among them is a literal. That is the
// whole of what is read: the bazel_dep() calls below it are dependencies
// nothing has proven this build linked, and reading them would put components
// into the document that no evidence chain reached.
type bazelModule struct{}

func (bazelModule) Source() string { return bazelSource }

var (
	// The opening of the declaration, at the start of a line: a module( inside
	// an argument list of something else is not the module declaration.
	bazelModuleCallPattern = regexp.MustCompile(`^\s*module\s*\(`)
	// One keyword argument with a literal string value.
	bazelVersionPattern = regexp.MustCompile(`(^|[\s,(])version\s*=\s*"([^"]*)"`)
)

func (b bazelModule) Enrich(root ComponentRoot) ([]Contribution, []domain.Finding) {
	path := filepath.Join(root.Path, bazelModuleName)
	if !fileExists(path) {
		return nil, nil
	}
	lines, findings, ok := readManifestLines(path, maxBazelModuleBytes, maxBazelModuleLines, "Bazel module file")
	if !ok {
		return nil, findings
	}

	version := ""
	calls := 0
	for index := 0; index < len(lines); index++ {
		if !bazelModuleCallPattern.MatchString(stripLineComment(lines[index], "#")) {
			continue
		}
		calls++
		if calls > 1 {
			// A file declaring two modules is not a module file this tool
			// understands, and choosing between them would be a guess.
			return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
				"the Bazel module file holds more than one module() declaration, so nothing was taken from it")}
		}
		call, end, closed := bazelModuleCall(lines, index)
		if !closed {
			return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
				"the Bazel module file does not close its module() declaration within the parser limit of section 30, so nothing was taken from it")}
		}
		if match := bazelVersionPattern.FindStringSubmatch(call); match != nil {
			version = match[2]
		}
		index = end
	}
	if version == "" {
		return nil, nil
	}
	if strings.ContainsAny(version, "${}") {
		return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
			"the Bazel module file states a version this tool cannot resolve without interpreting Starlark, so nothing was taken from it")}
	}
	return []Contribution{{Field: FieldVersion, Claim: Claim{
		Value: version, Rank: RankDeclaredManifest, Confidence: domain.ConfidenceHigh,
	}}}, nil
}

// bazelModuleCall joins the lines of one module() call into a single string and
// says where it ended. Parentheses are counted rather than matched against a
// grammar, because the argument values are string literals and the only nesting
// that occurs is a list or a dict among them; a call that has not closed within
// maxBazelModuleCallLines is reported as unclosed rather than followed to the
// end of a file this reader is not parsing.
func bazelModuleCall(lines []string, start int) (call string, end int, closed bool) {
	var builder strings.Builder
	depth := 0
	for index := start; index < len(lines) && index-start < maxBazelModuleCallLines; index++ {
		line := stripLineComment(lines[index], "#")
		builder.WriteString(line)
		builder.WriteString("\n")
		depth += parenthesisDepth(line)
		if depth <= 0 {
			return builder.String(), index, true
		}
	}
	return "", start, false
}

// singleLiteralVersion is the shape xmake shares with any later reader of a
// scripted build description: one file with a known name, one call with a
// literal argument, and a refusal wherever the file states that call more than
// once with different values.
//
// The uncomment function removes what the file states in a comment before any
// of it is matched, and it is a parameter rather than a marker string because
// no two of these languages spell a comment alike. It has to run first: a call
// somebody commented out is not a declaration, and reading one would publish a
// version nobody meant to state, which is the one failure this whole file is
// arranged to prevent.
func singleLiteralVersion(dir, name, kind, language string, maxBytes int64, maxLines int,
	uncomment func([]string) []string, pattern *regexp.Regexp) ([]Contribution, []domain.Finding) {
	path := filepath.Join(dir, name)
	if !fileExists(path) {
		return nil, nil
	}
	lines, findings, ok := readManifestLines(path, maxBytes, maxLines, kind)
	if !ok {
		return nil, findings
	}

	version := ""
	for _, line := range uncomment(lines) {
		match := pattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		value := strings.TrimSpace(match[1])
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, "${}") {
			return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
				"the "+kind+" states a version this tool cannot resolve without interpreting "+language+", so nothing was taken from it")}
		}
		if version != "" && version != value {
			return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
				"the "+kind+" states more than one version, so nothing was taken from it")}
		}
		version = value
	}
	if version == "" {
		return nil, nil
	}
	return []Contribution{{Field: FieldVersion, Claim: Claim{
		Value: version, Rank: RankDeclaredManifest, Confidence: domain.ConfidenceHigh,
	}}}, nil
}

// singleMatch is the one file a set of globs finds directly in a root, or
// nothing. Two matches are refused rather than settled, for the reason
// cmsispack.go refuses two descriptors: two files describing one root are two
// statements, and choosing between them would attribute one package's version
// to another.
func singleMatch(dir, conflict string, patterns ...string) (string, []domain.Finding) {
	found := make([]string, 0, 1)
	seen := map[string]bool{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			// A glob answers with names, and a directory carrying one of these
			// names is not evidence somebody meant to leave here.
			if seen[match] || !fileExists(match) {
				continue
			}
			seen[match] = true
			found = append(found, match)
		}
	}
	switch len(found) {
	case 0:
		return "", nil
	case 1:
		return found[0], nil
	default:
		return "", []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", dir, conflict)}
	}
}

// readManifestLines reads one bounded file whole and hands back its lines
// (section 30). The size is asked before the file is opened, so a file over the
// ceiling costs a stat rather than an allocation, and the reader is limited as
// well so that the ceiling still holds for a file being written while it is
// read.
//
// A file that is not there is not an answer of any kind: no lines, no finding,
// and the caller returns silence. Everything else that goes wrong is reported,
// because a file that exists and could not be read is evidence somebody left
// behind that this tool failed to use.
func readManifestLines(path string, maxBytes int64, maxLines int, kind string) ([]string, []domain.Finding, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, nil, false
	}
	if info.Size() > maxBytes {
		return nil, []domain.Finding{rootManifestFinding("INPUT_LIMIT_EXCEEDED", path,
			"the "+kind+" is larger than the parser limit of section 30, so nothing was read from it")}, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
			"the "+kind+" could not be read, so nothing was taken from it")}, false
	}
	defer func() { _ = file.Close() }()

	lines := make([]string, 0, 64)
	scanner := limits.Scanner(io.LimitReader(file, maxBytes))
	for scanner.Scan() {
		if len(lines) >= maxLines {
			return nil, []domain.Finding{rootManifestFinding("INPUT_LIMIT_EXCEEDED", path,
				"the "+kind+" holds more lines than the parser limit of section 30, so nothing was taken from it")}, false
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, []domain.Finding{rootManifestFinding("INPUT_LIMIT_EXCEEDED", path,
				"the "+kind+" holds a line longer than the parser limit of section 30, so nothing was taken from it")}, false
		}
		return nil, []domain.Finding{rootManifestFinding("EVIDENCE_UNREADABLE", path,
			"the "+kind+" could not be read to its end, so nothing was taken from it")}, false
	}
	return lines, nil, true
}

// parenthesisDepth is how much deeper a line leaves a call, counting only the
// parentheses that are part of the syntax. A parenthesis inside a string
// literal is a character of somebody's version or description -- `version =
// "1.0 (rc1"`, a smiley in a name -- and counting it would close the call at
// the wrong line, so the reader would either lose a stated version or report a
// perfectly ordinary file as unclosed.
func parenthesisDepth(line string) int {
	depth := 0
	quote := byte(0)
	for index := 0; index < len(line); index++ {
		char := line[index]
		switch {
		case quote != 0:
			if char == '\\' {
				index++
				continue
			}
			if char == quote {
				quote = 0
			}
		case char == '"' || char == '\'':
			quote = char
		case char == '(':
			depth++
		case char == ')':
			depth--
		}
	}
	return depth
}

// stripLuaComments blanks out everything an xmake.lua states in a comment.
//
// The line comment is what stripLineComment does elsewhere, but Lua also has
// the long comment -- `--[[ ... ]]`, with any number of equals signs between
// the brackets -- and that one spans lines. It matters more than it looks: the
// lines inside such a comment carry no marker of their own, so a reader that
// only skipped lines beginning with `--` would read `set_version("9.9")` out of
// a release somebody commented out years ago and publish it as the version.
// That is the one shape in this file where a value nobody meant to state could
// otherwise reach the document.
//
// This is lexing rather than interpreting: quotes are tracked so that a `--`
// inside a string is not a comment, and nothing here evaluates anything. A long
// *string* (`[[ ... ]]` without the dashes) is left alone -- it holds text, and
// a set_version inside one is not a call either way.
func stripLuaComments(lines []string) []string {
	stripped := make([]string, 0, len(lines))
	// The level of the long comment currently open -- the number of equals
	// signs in its opening bracket -- or -1 when none is.
	level := -1
	for _, line := range lines {
		var kept strings.Builder
		quote := byte(0)
		for index := 0; index < len(line); {
			if level >= 0 {
				closer := "]" + strings.Repeat("=", level) + "]"
				at := strings.Index(line[index:], closer)
				if at < 0 {
					break
				}
				index += at + len(closer)
				level = -1
				continue
			}
			char := line[index]
			switch {
			case quote != 0:
				kept.WriteByte(char)
				if char == '\\' && index+1 < len(line) {
					kept.WriteByte(line[index+1])
					index += 2
					continue
				}
				if char == quote {
					quote = 0
				}
			case char == '"' || char == '\'':
				quote = char
				kept.WriteByte(char)
			case strings.HasPrefix(line[index:], "--"):
				if open, width, long := luaLongBracket(line, index+2); long {
					level = open
					index += 2 + width
					continue
				}
				// An ordinary comment runs to the end of the line.
				index = len(line)
				continue
			default:
				kept.WriteByte(char)
			}
			index++
		}
		stripped = append(stripped, kept.String())
	}
	return stripped
}

// luaLongBracket reads the opening bracket of a Lua long comment at index --
// `[`, any number of `=`, `[` -- and reports its level and how many bytes it
// spans. Anything else is not a long bracket, and the caller treats it as an
// ordinary comment to the end of the line.
func luaLongBracket(line string, index int) (level, width int, ok bool) {
	if index >= len(line) || line[index] != '[' {
		return 0, 0, false
	}
	end := index + 1
	for end < len(line) && line[end] == '=' {
		end++
	}
	if end >= len(line) || line[end] != '[' {
		return 0, 0, false
	}
	return end - index - 1, end - index + 1, true
}

// stripLineComment drops what follows a comment marker outside a string
// literal. Quotes are tracked because a '#' inside a version string is not a
// comment, and dropping the rest of that line would turn a stated version into
// a truncated one.
func stripLineComment(line, marker string) string {
	quote := byte(0)
	for index := 0; index < len(line); index++ {
		char := line[index]
		switch {
		case quote != 0:
			if char == '\\' {
				index++
				continue
			}
			if char == quote {
				quote = 0
			}
		case char == '"' || char == '\'':
			quote = char
		case strings.HasPrefix(line[index:], marker):
			return line[:index]
		}
	}
	return line
}

// isSPDXExpressionShaped reports whether a value can be published as
// licenses[].expression. There is no list of valid identifiers to check
// against here, so this checks the shape and nothing more: identifiers
// alternating with the three SPDX operators, and balanced parentheses. That is
// enough to keep build2's `other: <description>` form and any free-text licence
// out of a field every consumer reads as SPDX, which is the whole point --
// section 22.3 forbids deciding by inspection what a free-text licence means.
//
// The alternation is what does the work. Without it "All rights reserved"
// passes: three words made of nothing but letters, which is a sentence and not
// an expression. An expression joins its identifiers with an operator or holds
// exactly one.
func isSPDXExpressionShaped(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(strings.ToLower(value), "other:") {
		return false
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '(' || r == ')'
	})
	if len(fields) == 0 || len(fields) > 32 {
		return false
	}
	wantOperator := false
	for _, field := range fields {
		switch field {
		case "AND", "OR", "WITH":
			if !wantOperator {
				return false
			}
			wantOperator = false
			continue
		}
		if wantOperator {
			return false
		}
		for index := 0; index < len(field); index++ {
			switch char := field[index]; {
			case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
			case char == '.', char == '-', char == '+':
			default:
				return false
			}
		}
		if field[0] == '-' || field[0] == '+' || field[0] == '.' {
			return false
		}
		wantOperator = true
	}
	if !wantOperator {
		// The value ended on an operator, so an operand is missing.
		return false
	}
	// Parentheses have to balance, or the expression is not one.
	depth := 0
	for _, char := range value {
		switch char {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

// rootManifestFinding reports a manifest that was found but not used. The
// subject is the file rather than the component: what could not be read is a
// piece of evidence, and the component is still described by whatever else
// states it.
func rootManifestFinding(id, path, message string) domain.Finding {
	return domain.Finding{
		ID: id, Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "evidence", Ref: path},
		Message: message,
	}
}
