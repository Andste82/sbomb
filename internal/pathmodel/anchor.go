package pathmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// AnchorKind is the leading component of an anchor key, per specification
// section 7.2. A key is either a bare kind ("project") or a kind with a name
// ("pkg:conan/mbedtls").
type AnchorKind string

const (
	AnchorProject   AnchorKind = "project"
	AnchorBuild     AnchorKind = "build"
	AnchorSDK       AnchorKind = "sdk"
	AnchorPackage   AnchorKind = "pkg"
	AnchorToolchain AnchorKind = "toolchain"
	AnchorSysroot   AnchorKind = "sysroot"
	AnchorExtern    AnchorKind = "extern"
	// AnchorAbs is the fallback for files under no other anchor. It is never
	// registered; Resolve produces it when nothing matches.
	AnchorAbs AnchorKind = "abs"
)

// namedKinds require a name after the colon; bare kinds must not carry one.
var namedKinds = map[AnchorKind]bool{
	AnchorSDK:       true,
	AnchorPackage:   true,
	AnchorToolchain: true,
	AnchorSysroot:   true,
	AnchorExtern:    true,
}

var bareKinds = map[AnchorKind]bool{
	AnchorProject: true,
	AnchorBuild:   true,
}

// Anchor is a named, absolute directory that gives files below it a portable
// identity. Anchors are registered in the priority order of section 7.4.
type Anchor struct {
	Key    string // canonical anchor key, e.g. "project" or "toolchain:gcc-13"
	Root   string // normalized absolute directory
	Source string // where the anchor came from, for diagnostics
}

// preparedRoot is an anchor root reduced to the two things resolution asks of
// it. Under the Windows flavor the comparison form is a lowercased copy of the
// root, so deriving it per path would allocate a string for every anchor of
// every file; the segment count would mean a walk over the root under both
// flavors. Derived once at registration, neither costs anything afterwards.
type preparedRoot struct {
	comparison string // the root in the registry's comparison form
	segments   int    // how many non-empty segments the root has
}

// Registry resolves absolute paths to (anchor, relative path) identities per
// section 7.3. A registry is bound to one path flavor, because prefix
// comparison is case-sensitive under POSIX and case-insensitive under Windows.
type Registry struct {
	flavor Flavor
	// anchors keeps registration order; resolution sorts by root length.
	anchors []Anchor
	// prepared holds what resolution needs of each anchor's root, in the same
	// order as anchors. Resolution asks for both of these once per anchor for
	// every path it is given, and neither can change after the anchor is
	// registered, so both are derived where the anchor is added.
	prepared []preparedRoot
	// byRoot deduplicates on the comparison form of the root, so that a later
	// registration cannot override an earlier one for the same directory.
	byRoot map[string]string
	// byKey rejects the same key pointing at two different directories.
	byKey map[string]string
	// redactUnanchored replaces abs relative paths with a salt-free digest.
	redactUnanchored bool
}

// NewRegistry returns an empty registry for the given path flavor.
func NewRegistry(flavor Flavor) *Registry {
	if flavor == nil {
		flavor = DefaultFlavor()
	}
	return &Registry{
		flavor: flavor,
		byRoot: map[string]string{},
		byKey:  map[string]string{},
	}
}

// Flavor reports the path flavor this registry resolves with.
func (r *Registry) Flavor() Flavor { return r.flavor }

// SetRedactUnanchored controls whether unanchored paths are replaced by
// redacted/<sha256(relPath)[:16]> per section 7.5. The original path is then
// not recoverable from any output.
func (r *Registry) SetRedactUnanchored(redact bool) { r.redactUnanchored = redact }

// Register adds an anchor. It returns false when the directory is already
// registered under another key, because section 7.4 requires that later
// registrations do not override earlier ones for the same directory. An empty
// root is ignored, which lets callers register optional anchors unconditionally.
func (r *Registry) Register(key, root, source string) (bool, error) {
	if root == "" {
		return false, nil
	}
	if err := ValidateAnchorKey(key); err != nil {
		return false, err
	}
	normalized := r.flavor.Normalize(root)
	comparison := r.comparisonForm(normalized)
	if existing, taken := r.byRoot[comparison]; taken {
		if existing == key {
			return false, nil
		}
		return false, nil
	}
	if existingRoot, taken := r.byKey[key]; taken {
		return false, fmt.Errorf("anchor %q is already registered at %s", key, existingRoot)
	}
	r.byRoot[comparison] = key
	r.byKey[key] = normalized
	r.anchors = append(r.anchors, Anchor{Key: key, Root: normalized, Source: source})
	r.prepared = append(r.prepared, preparedRoot{comparison: comparison, segments: countSegments(normalized)})
	return true, nil
}

// ValidateAnchorKey checks a key against the grammar of section 7.2, so that a
// typo becomes an error rather than a silently unmatchable anchor.
func ValidateAnchorKey(key string) error {
	if key == "" {
		return fmt.Errorf("anchor key must not be empty")
	}
	kind, name, hasName := strings.Cut(key, ":")
	switch {
	case bareKinds[AnchorKind(kind)]:
		if hasName {
			return fmt.Errorf("anchor kind %q does not take a name, got %q", kind, key)
		}
	case namedKinds[AnchorKind(kind)]:
		if !hasName || name == "" {
			return fmt.Errorf("anchor kind %q requires a name, got %q", kind, key)
		}
	case AnchorKind(kind) == AnchorAbs:
		return fmt.Errorf("anchor key %q is reserved for unanchored files", key)
	default:
		return fmt.Errorf("unknown anchor kind %q in key %q", kind, key)
	}
	return nil
}

// Anchors returns the registered anchors in registration order.
func (r *Registry) Anchors() []Anchor {
	out := make([]Anchor, len(r.anchors))
	copy(out, r.anchors)
	return out
}

// Resolve maps an absolute path to its portable identity. Relative paths are
// returned relative to no anchor; use ResolveIn to supply the directory a
// relative path is understood against.
func (r *Registry) Resolve(path string) domain.FileID {
	return r.ResolveIn("", path)
}

// ResolveIn maps a path to its portable identity, resolving a relative path
// against base first. Build evidence mixes both forms freely: compile
// databases carry a "directory" field, and depfiles name objects relative to
// the build directory.
func (r *Registry) ResolveIn(base, path string) domain.FileID {
	if path == "" {
		return domain.FileID{Anchor: domain.AnchorKey(AnchorAbs), RelPath: ""}
	}
	normalized := r.flavor.Normalize(path)
	if base != "" && !r.isAbsolute(normalized) {
		normalized = r.flavor.Normalize(joinPaths(r.flavor.Normalize(base), path))
	}
	comparison := r.comparisonForm(normalized)

	// Longest matching root wins (section 7.3 step 2). Roots are deduplicated
	// at registration, so the longest match is unique.
	best := -1
	bestLen := -1
	for index := range r.prepared {
		root := r.prepared[index].comparison
		if !hasSegmentPrefix(comparison, root) {
			continue
		}
		if len(root) > bestLen {
			best, bestLen = index, len(root)
		}
	}
	if best >= 0 {
		return domain.FileID{
			Anchor:  domain.AnchorKey(r.anchors[best].Key),
			RelPath: relativeTo(normalized, r.prepared[best].segments),
		}
	}
	return domain.FileID{Anchor: domain.AnchorKey(AnchorAbs), RelPath: r.unanchoredRelPath(normalized)}
}

// unanchoredRelPath produces the abs-anchor relative path of section 7.5: the
// absolute path without its leading separator, with a Windows drive letter
// becoming the first segment.
func (r *Registry) unanchoredRelPath(normalized string) string {
	rel := normalized
	if index := strings.Index(rel, ":/"); index >= 0 && index <= 2 {
		// "C:/Users/x" becomes "C/Users/x".
		rel = rel[:index] + rel[index+1:]
	}
	rel = strings.TrimLeft(rel, "/")
	if r.redactUnanchored && rel != "" {
		digest := sha256.Sum256([]byte(rel))
		return "redacted/" + hex.EncodeToString(digest[:])[:16]
	}
	return rel
}

func (r *Registry) comparisonForm(path string) string {
	if r.flavor.CaseSensitive() {
		return path
	}
	return strings.ToLower(path)
}

func (r *Registry) isAbsolute(normalized string) bool {
	if strings.HasPrefix(normalized, "/") {
		return true
	}
	// Windows drive-qualified paths normalize to "C:/...".
	index := strings.Index(normalized, ":/")
	return index == 1
}

// hasSegmentPrefix reports whether path lies at or below prefix, comparing at
// a path-segment boundary so that /usr/libx is not treated as being under
// /usr/lib.
func hasSegmentPrefix(path, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		// The filesystem root anchors everything below it.
		return strings.HasPrefix(path, "/")
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	// The prefix is a segment boundary when the path ends there or the byte
	// after it opens a new segment. Reading that byte in place answers the
	// same question as matching against prefix+"/" without building that
	// string, which resolution would otherwise do for every anchor of every
	// file it identifies.
	return len(path) == len(prefix) || path[len(prefix)] == '/'
}

// relativeTo returns path with the leading rootSegments segments dropped, in
// POSIX form and never containing "..", because both the path and the anchor
// root it is measured against are already lexically normalized. rootSegments
// is how many non-empty segments that root has, which the registry counted
// when the anchor was registered.
//
// It drops whole segments rather than trimming a string prefix: under the
// Windows flavor the match is case-insensitive, so the root and the path may
// disagree in case, and a byte-wise trim would leave the path untouched. The
// prefix is not even the same length in general, because lowercasing is not
// obliged to preserve the width of a rune.
//
// It does not have to materialize those segments to drop them, though, and it
// is called once per file per anchor, so it does not. The only thing it needs
// of the path is where the segment after the dropped ones begins, and the
// answer is then the tail of the path exactly as it already stands, which
// costs nothing to return. The join over materialized segments is kept for the
// paths where that tail is not the answer: one carrying an empty segment or a
// trailing separator joins to something shorter than its own tail, and
// resolution is asked about paths taken from build evidence, which is under no
// obligation to be tidy.
func relativeTo(path string, rootSegments int) string {
	start := segmentStart(path, rootSegments)
	if start < 0 {
		return "."
	}
	if tail := path[start:]; isJoinedForm(tail) {
		return tail
	}
	return strings.Join(splitSegments(path)[rootSegments:], "/")
}

// countSegments reports how many non-empty segments a path has, which is the
// length splitSegments would have returned for it. Only registration asks
// this, once per anchor.
func countSegments(path string) int {
	count := 0
	for index := 0; index < len(path); {
		for index < len(path) && path[index] == '/' {
			index++
		}
		if index == len(path) {
			break
		}
		count++
		for index < len(path) && path[index] != '/' {
			index++
		}
	}
	return count
}

// segmentStart returns the byte offset at which the segment after the first
// skip non-empty segments begins, or -1 when the path holds no further
// segment -- the case where the path is the anchor root itself, or lies above
// it, and relativeTo answers ".".
//
// The separators are found with IndexByte rather than by a loop over the
// bytes, because IndexByte is the assembly routine that reads a machine word
// at a time and this runs for every file the run identifies.
func segmentStart(path string, skip int) int {
	index := 0
	for skipped := 0; skipped < skip; skipped++ {
		for index < len(path) && path[index] == '/' {
			index++
		}
		if index == len(path) {
			return -1
		}
		next := strings.IndexByte(path[index:], '/')
		if next < 0 {
			return -1
		}
		index += next
	}
	for index < len(path) && path[index] == '/' {
		index++
	}
	if index == len(path) {
		return -1
	}
	return index
}

// isJoinedForm reports whether a non-empty tail that begins with a segment is
// already spelled the way joining its segments with "/" would spell it: every
// separator in it stands between two segments, rather than doubling another or
// dangling at the end.
func isJoinedForm(tail string) bool {
	return !strings.Contains(tail, "//") && tail[len(tail)-1] != '/'
}

func splitSegments(path string) []string {
	parts := strings.Split(path, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			segments = append(segments, part)
		}
	}
	return segments
}

func joinPaths(base, path string) string {
	if base == "" {
		return path
	}
	return strings.TrimSuffix(base, "/") + "/" + path
}

// SortAnchors returns anchors ordered by key, for deterministic reporting.
func SortAnchors(anchors []Anchor) []Anchor {
	out := make([]Anchor, len(anchors))
	copy(out, anchors)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
