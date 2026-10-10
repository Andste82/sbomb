package pathmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Flavor controls the platform-specific rules used for logical path identity.
type Flavor interface {
	Normalize(string) string
	CaseSensitive() bool
}

type PosixFlavor struct{}

func (PosixFlavor) Normalize(path string) string { return normalizePosix(path) }
func (PosixFlavor) CaseSensitive() bool          { return true }

type WindowsFlavor struct{}

func (WindowsFlavor) Normalize(path string) string { return normalizeWindows(path) }
func (WindowsFlavor) CaseSensitive() bool          { return false }

// IsAbsolute reports whether a path is absolute under the host OS or in a
// spelling carried by cross-platform build evidence: a Windows drive or UNC
// path, or a POSIX path from a leading slash. The evidence names paths of the
// machine that built the project, not of the one reading it, and a POSIX
// absolute path is absolute there whatever host sbomb runs on. filepath.IsAbs
// alone said otherwise on a Windows host, so a compilation database's
// /__fixture_build__ was made absolute against the current drive and its
// sources were joined under it -- the same evidence read on Windows produced
// other identities than on Linux.
func IsAbsolute(path string) bool {
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return true
	}
	return len(path) >= 3 && isASCIIAlpha(path[0]) && path[1] == ':' && (path[2] == '/' || path[2] == '\\') || isUNC(path)
}

// NormalizeSeparators converts Windows and POSIX separators to slash form for
// host-side path joining and comparison.
func NormalizeSeparators(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

// isUNC reports whether p is a UNC path as Windows evidence spells it, from
// two backslashes. Two leading slashes are not: POSIX reads "//usr" as "/usr",
// and a Makefile that joins $(PREFIX)/ with PREFIX=/ writes exactly that.
func isUNC(p string) bool {
	return strings.HasPrefix(p, `\\`)
}

// CleanEvidence cleans a path as build evidence records it. A path that is
// absolute in the host's own spelling is cleaned by the host's rules, exactly
// like the paths sbomb joins onto a directory it reads, so that one file has
// one spelling on that host. A path in a foreign spelling -- a POSIX path read
// on Windows, a Windows path read on Linux -- is cleaned in slash form, by the
// same rule on every host: filepath would clean it by the rules of the machine
// reading it, and on a Windows host /usr/bin/cc became \usr\bin\cc, so the
// same evidence produced other identities than on Linux. A drive root keeps
// its slash and a UNC path its second leading one, which path.Clean alone
// would drop; two leading forward slashes are one, as POSIX reads them.
func CleanEvidence(p string) string {
	if p == "" {
		return ""
	}
	if isUNC(p) {
		if runtime.GOOS == "windows" {
			return filepath.Clean(p)
		}
		return "/" + path.Clean(NormalizeSeparators(p))
	}
	if strings.HasPrefix(p, "//") {
		return path.Clean(NormalizeSeparators(p))
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	clean := path.Clean(NormalizeSeparators(p))
	if len(clean) == 2 && clean[1] == ':' && isASCIIAlpha(clean[0]) {
		return clean + "/"
	}
	return clean
}

// JoinEvidence joins a path recorded in build evidence to the evidence
// directory it is relative to -- a compilation database's directory, a debug
// unit's compilation directory. Both are paths of the build machine; when the
// directory is absolute in the host's spelling the two are joined by the
// host's rules, otherwise in slash form, as CleanEvidence would clean the
// result. Either way a backslash in the name is a separator, as it always was
// here. An absolute path stands on its own.
func JoinEvidence(dir, p string) string {
	if IsAbsolute(p) || dir == "" {
		return CleanEvidence(p)
	}
	if isUNC(dir) {
		return CleanEvidence(dir + `\` + p)
	}
	if filepath.IsAbs(dir) {
		return filepath.Clean(filepath.Join(dir, NormalizeSeparators(p)))
	}
	return CleanEvidence(NormalizeSeparators(dir) + "/" + NormalizeSeparators(p))
}

// ResolveEvidence resolves a path recorded in build evidence against the
// directory on this host it is relative to. An absolute path is a path of the
// build machine and is cleaned as evidence; a relative one names a file below
// hostDir that is about to be read here, and is joined by the host's rules.
func ResolveEvidence(hostDir, p string) string {
	if IsAbsolute(p) || hostDir == "" {
		return CleanEvidence(p)
	}
	return filepath.Clean(filepath.Join(hostDir, NormalizeSeparators(p)))
}

// DirEvidence is the directory of a path as build evidence records it, taken
// apart by the rules CleanEvidence cleaned it by.
func DirEvidence(p string) string {
	clean := CleanEvidence(p)
	switch {
	case isUNC(p) && runtime.GOOS != "windows":
		// Before the host's rules: filepath takes //server for an absolute
		// POSIX path here and would fold its two slashes into one.
		return "/" + path.Dir(clean[1:])
	case filepath.IsAbs(clean):
		return filepath.Dir(clean)
	case len(clean) >= 3 && clean[1] == ':' && isASCIIAlpha(clean[0]) && !strings.Contains(clean[3:], "/"):
		return clean[:3]
	}
	return path.Dir(clean)
}

// BaseEvidence is the last element of a path as build evidence records it,
// taken apart by the rules CleanEvidence cleaned it by.
func BaseEvidence(p string) string {
	clean := CleanEvidence(p)
	if filepath.IsAbs(clean) {
		return filepath.Base(clean)
	}
	return path.Base(clean)
}

func isASCIIAlpha(value byte) bool {
	return (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}

// DefaultFlavor returns the logical path flavor for the current host.
func DefaultFlavor() Flavor {
	if runtime.GOOS == "windows" {
		return WindowsFlavor{}
	}
	return PosixFlavor{}
}

// Resolve returns the canonical identity for a path in the form anchor:relpath.
func Resolve(path string, projectRoot string, buildRoot string) string {
	return ResolveWithFlavor(path, projectRoot, buildRoot, DefaultFlavor())
}

// ResolveWithFlavor returns a canonical identity using the supplied path rules.
func ResolveWithFlavor(path string, projectRoot string, buildRoot string, flavor Flavor) string {
	if path == "" {
		return "abs:"
	}
	clean := flavor.Normalize(path)
	project := flavor.Normalize(projectRoot)
	build := flavor.Normalize(buildRoot)
	if !flavor.CaseSensitive() {
		clean = strings.ToLower(clean)
		project = strings.ToLower(project)
		build = strings.ToLower(build)
	}

	for _, anchor := range []struct {
		key  string
		root string
	}{
		{key: "project", root: project},
		{key: "build", root: build},
	} {
		if anchor.root == "" {
			continue
		}
		if hasPathPrefix(clean, anchor.root, flavor) {
			rel := strings.TrimPrefix(clean, anchor.root)
			rel = strings.TrimPrefix(rel, flavor.Normalize("/"))
			if rel == "" {
				return anchor.key + ":."
			}
			return anchor.key + ":" + rel
		}
	}
	return "abs:" + strings.TrimLeft(clean, "/\\")
}

// Base returns the final logical path component using the supplied flavor.
func Base(path string, flavor Flavor) string {
	if flavor == nil {
		flavor = DefaultFlavor()
	}
	clean := flavor.Normalize(path)
	clean = strings.TrimRight(clean, "/\\")
	if index := strings.LastIndexAny(clean, "/\\"); index >= 0 {
		return clean[index+1:]
	}
	return clean
}

func normalizePosix(p string) string {
	if p == "" {
		return "/"
	}
	return cleanPath(strings.ReplaceAll(p, "\\", "/"), "/")
}

func normalizeWindows(p string) string {
	if p == "" {
		return "/"
	}
	p = strings.ReplaceAll(p, "/", "\\")
	// A relative path stays relative, exactly as it does under the POSIX
	// flavor. Rooting it here would make "imgapp.exe" read as "/imgapp.exe",
	// and every caller that joins a relative path onto its base directory
	// asks first whether the path is absolute: the answer would be yes, the
	// join would be skipped, and the file would be identified against no root
	// at all (section 7.3). Only a path that names a root keeps one.
	root := ""
	if len(p) >= 3 && p[1] == '$' && p[2] == ':' {
		p = p[:1] + p[2:]
	}
	if strings.HasPrefix(p, "\\\\") {
		root = "//"
	} else if strings.HasPrefix(p, "\\") {
		root = "/"
	} else if len(p) >= 2 && p[1] == ':' {
		root = strings.ToUpper(p[:1]) + ":/"
		p = p[2:]
	}
	clean := strings.TrimPrefix(cleanPath(p, "\\"), "\\")
	// A relative path that cleans to nothing -- ".", "a\.." -- is the
	// directory it is relative to, and the POSIX flavor spells that "/".
	// Trimming the separator cleanPath answers with left it empty here, an
	// identity of no file at all, so the two flavors disagreed about ".".
	if root == "" && clean == "" {
		return "/"
	}
	return strings.ReplaceAll(root+clean, "\\", "/")
}

func cleanPath(p, separator string) string {
	// Normalization is on the path of every file identity the run forms, and
	// nearly every path handed to it is already in the form it would produce:
	// compilers, depfiles and linker maps mostly name files without an empty,
	// "." or ".." segment in them. Recognizing that costs one pass over the
	// bytes and lets the path be returned as it came, where the general route
	// below would split it into a slice, filter that into a second slice and
	// join the survivors into a third string. The general route still handles
	// everything the check declines.
	if isLexicallyClean(p, separator) {
		return p
	}
	absolute := strings.HasPrefix(p, separator)
	parts := strings.Split(p, separator)
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(cleaned) > 0 && cleaned[len(cleaned)-1] != ".." {
				cleaned = cleaned[:len(cleaned)-1]
			} else if !absolute {
				cleaned = append(cleaned, part)
			}
		default:
			cleaned = append(cleaned, part)
		}
	}
	result := strings.Join(cleaned, separator)
	if absolute || result == "" {
		return separator + result
	}
	return result
}

// isLexicallyClean reports whether cleanPath would return p unchanged: p is
// non-empty, no segment of it is empty, "." or "..", and it carries no
// trailing separator, apart from the root itself, which is nothing but one.
// It is deliberately conservative -- a false answer only means the general
// route runs, never that a path is cleaned wrongly.
//
// The separators are found with IndexByte, the assembly routine that reads a
// machine word at a time, rather than by a loop over the bytes: normalization
// walks every path the run sees, and the segments between the separators are
// most of what it walks.
func isLexicallyClean(p, separator string) bool {
	if len(separator) != 1 || p == "" {
		return false
	}
	sep := separator[0]
	rest := p
	if rest[0] == sep {
		rest = rest[1:]
		if rest == "" {
			// The root is its own clean form.
			return true
		}
	}
	for {
		next := strings.IndexByte(rest, sep)
		if next < 0 {
			return isCleanSegment(rest)
		}
		if !isCleanSegment(rest[:next]) {
			return false
		}
		rest = rest[next+1:]
		if rest == "" {
			// A trailing separator is dropped, so p is not its own form.
			return false
		}
	}
}

// isCleanSegment reports whether a path segment survives cleaning as itself,
// which the empty, current-directory and parent-directory segments do not.
func isCleanSegment(segment string) bool {
	switch segment {
	case "", ".", "..":
		return false
	}
	return true
}

func hasPathPrefix(path, prefix string, flavor Flavor) bool {
	separator := "/"
	if prefix == separator || strings.HasSuffix(prefix, ":\\") {
		return true
	}
	path = strings.TrimSuffix(path, separator)
	prefix = strings.TrimSuffix(prefix, separator)
	if path == prefix {
		return true
	}
	if strings.HasPrefix(path, prefix+separator) {
		return true
	}
	return false
}

// Slug normalizes a string into a stable identifier, per the definition in
// specification section 28.4: lowercase, every character outside
// [a-z0-9._-] replaced by a hyphen, runs of hyphens collapsed, leading and
// trailing hyphens trimmed, and truncation to maxLen with a digest suffix so
// that two long names cannot collide.
func Slug(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(s))
	previousHyphen := false
	for _, r := range strings.ToLower(s) {
		keep := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !keep {
			r = '-'
		}
		if r == '-' {
			if previousHyphen {
				continue
			}
			previousHyphen = true
		} else {
			previousHyphen = false
		}
		builder.WriteRune(r)
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		slug = "unnamed"
	}
	if len(slug) <= maxLen {
		return slug
	}
	digest := sha256.Sum256([]byte(s))
	suffix := "-" + hex.EncodeToString(digest[:])[:8]
	keep := maxLen - len(suffix)
	if keep < 1 {
		keep = 1
	}
	return strings.Trim(slug[:keep], "-") + suffix
}
