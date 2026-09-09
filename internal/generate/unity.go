package generate

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
	"github.com/example/sbomb/internal/pathmodel"
)

// maxUnityFileBytes bounds what the unity parser will read (section 30). A
// unity source is generated aggregation glue; anything larger is not one.
const maxUnityFileBytes = 4 << 20

// unityTU is one detected unity translation unit and the sources it aggregates.
type unityTU struct {
	object string
	source string
	// constituents are the canonical identities of the aggregated sources.
	constituents []string
	// strategy names how they were recovered, for the edge's source field.
	strategy string
	// confidence follows section 17.1: high for the generated file's own
	// include list, medium for the depfile.
	confidence domain.Confidence
}

// looksLikeUnityPath applies the path signal of section 17.1: CMake writes its
// aggregation sources into a Unity directory below the target's build files.
func looksLikeUnityPath(path string) bool {
	slashed := filepath.ToSlash(path)
	if !strings.Contains(slashed, "/Unity/") {
		return false
	}
	return strings.HasPrefix(strings.ToLower(filepath.Base(slashed)), "unity_")
}

// unityIncludes reads a generated unity source and returns the files it
// includes. Section 17.1 permits this explicitly: it is deterministic, requires
// no execution, and is the only evidence the generated file carries. The file
// qualifies only when it contains nothing but comments and include directives,
// which is what distinguishes aggregation glue from ordinary source.
func unityIncludes(path string) ([]string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxUnityFileBytes {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	includes := make([]string, 0)
	scanner := limits.Scanner(file)
	inBlockComment := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if inBlockComment {
			if index := strings.Index(line, "*/"); index >= 0 {
				inBlockComment = false
				line = strings.TrimSpace(line[index+2:])
			} else {
				continue
			}
		}
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "/*") {
			if !strings.Contains(line, "*/") {
				inBlockComment = true
			}
			continue
		}
		included, ok := includedPath(line)
		if !ok {
			// Anything that is neither a comment nor an include means this is
			// not aggregation glue, and guessing would be worse than failing.
			return nil, false
		}
		includes = append(includes, included)
	}
	if scanner.Err() != nil {
		return nil, false
	}
	return includes, len(includes) > 0
}

// includedPath extracts the file of a single #include directive.
func includedPath(line string) (string, bool) {
	if !strings.HasPrefix(line, "#") {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "#"))
	if !strings.HasPrefix(rest, "include") {
		return "", false
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "include"))
	// An empty target names no file. Left in, it would be joined with the
	// including file's directory and identified as one.
	switch {
	case strings.HasPrefix(rest, "\""):
		if end := strings.Index(rest[1:], "\""); end > 0 {
			return rest[1 : 1+end], true
		}
	case strings.HasPrefix(rest, "<"):
		if end := strings.Index(rest, ">"); end > 1 {
			return rest[1:end], true
		}
	}
	return "", false
}

// resolveUnityTU recovers the constituent sources of one unity translation unit
// in the order of section 17.1.
func resolveUnityTU(b *builder, object, source string, depfileHeaders []string, physicalSource string) unityTU {
	tu := unityTU{object: object, source: source}

	if includes, ok := unityIncludes(physicalSource); ok {
		base := filepath.Dir(physicalSource)
		for _, include := range includes {
			path := include
			if !pathmodel.IsAbsolute(path) {
				path = filepath.Join(base, path)
			}
			if !isSourcePath(path) {
				continue
			}
			canonical, _ := b.identify(path)
			tu.constituents = append(tu.constituents, canonical)
		}
		if len(tu.constituents) > 0 {
			tu.strategy = "unity-includes"
			tu.confidence = domain.ConfidenceHigh
			return tu
		}
	}

	// Fallback: the depfile of the unity unit, filtered to source extensions.
	for _, candidate := range depfileHeaders {
		if isSourcePath(candidate) {
			tu.constituents = append(tu.constituents, candidate)
		}
	}
	if len(tu.constituents) > 0 {
		tu.strategy = "unity-depfile"
		tu.confidence = domain.ConfidenceMedium
	}
	return tu
}

// unityUnresolvedFinding reports a unity unit whose constituents no permitted
// strategy could recover (section 17.1).
func unityUnresolvedFinding(object string) domain.Finding {
	return domain.Finding{
		ID: "UNITY_SOURCE_UNRESOLVED", Severity: domain.SeverityWarning,
		Subject:     domain.Subject{Kind: "file", Ref: object},
		Message:     "a unity build aggregates sources that no evidence source names",
		Remediation: "Keep the generated unity source in the build tree, or disable UNITY_BUILD for this target.",
	}
}
