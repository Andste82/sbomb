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

// Registry resolves absolute paths to (anchor, relative path) identities per
// section 7.3. A registry is bound to one path flavor, because prefix
// comparison is case-sensitive under POSIX and case-insensitive under Windows.
type Registry struct {
	flavor Flavor
	// anchors keeps registration order; resolution sorts by root length.
	anchors []Anchor
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
	for index, anchor := range r.anchors {
		root := r.comparisonForm(anchor.Root)
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
			RelPath: relativeTo(normalized, r.anchors[best].Root),
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
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}

// relativeTo returns path expressed relative to root, in POSIX form and never
// containing "..", because both are already lexically normalized.
//
// It drops whole segments rather than trimming a string prefix: under the
// Windows flavor the match is case-insensitive, so the root and the path may
// disagree in case, and a byte-wise trim would leave the path untouched.
func relativeTo(path, root string) string {
	rootSegments := splitSegments(root)
	pathSegments := splitSegments(path)
	if len(pathSegments) <= len(rootSegments) {
		return "."
	}
	return strings.Join(pathSegments[len(rootSegments):], "/")
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
