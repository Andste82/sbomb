package version

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
)

var macroPattern = regexp.MustCompile(`(?m)^\s*#define\s+([A-Za-z_][A-Za-z0-9_]*)\s+(.+?)\s*$`)

// commitPattern is what a commit SHA looks like. A rev-parse that answered
// with anything else is not evidence of a commit, and a version built from it
// would be an invention.
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Options is what resolving a version needs in order to look around, in the
// shape the package-manager adapters already use: a nil Runner, or one whose
// git group is off, means the git rules ask nothing and yield nothing rather
// than degrading into a guess.
type Options struct {
	// Runner runs introspection commands. It refuses everything when
	// introspection is off, which is the default, so a rule must survive its
	// absence and only improve with it.
	Runner *exec.Runner
	// Context bounds the introspection calls.
	Context context.Context
}

// Result is one resolved version together with what it is worth. Dirty travels
// with it because a version read out of a modified working tree does not
// identify the content it names, and the caller has to be able to say so.
type Result struct {
	Version    string
	Source     string
	Confidence domain.Confidence
	Dirty      bool
}

// Resolve applies the versionFrom rules of section 20.2 in the order given.
// Priorities 1 to 3 -- curated configuration, package-manager metadata and SDK
// metadata -- are settled by the caller before it gets here, so this answers
// points 4 to 6 only. The rule list is a parameter rather than a field of the
// component, because the configuration is where it is written and a second
// copy of it in the domain model would be a second thing to keep true.
func Resolve(rules []string, root string, options Options) (Result, bool) {
	for _, rule := range rules {
		switch {
		case rule == "git":
			if result, ok := resolveGit(root, options); ok {
				return result, true
			}
		case strings.HasPrefix(rule, "header:"):
			if resolved, ok := resolveHeaderMacro(root, rule); ok {
				return Result{Version: resolved, Source: "header", Confidence: domain.ConfidenceMedium}, true
			}
		case rule == "commit":
			if result, ok := resolveGitCommit(root, options); ok {
				return result, true
			}
		}
	}
	return Result{Confidence: domain.ConfidenceUnknown}, false
}

func resolveHeaderMacro(root, rule string) (string, bool) {
	payload := strings.TrimPrefix(rule, "header:")
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return "", false
	}
	filePath := filepath.Clean(filepath.Join(root, parts[0]))
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", false
	}
	macro := parts[1]
	for _, line := range strings.Split(string(data), "\n") {
		m := macroPattern.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		if m[1] != macro {
			continue
		}
		value := strings.TrimSpace(m[2])
		value = strings.TrimSuffix(value, "L")
		if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			return strings.Trim(value, "\""), true
		}
		if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
			return strings.Trim(value, "'"), true
		}
		if _, err := fmt.Sscanf(value, "%d", new(int)); err == nil {
			return value, true
		}
	}
	return "", false
}

// resolveGit asks the checkout what it is (section 20.2, point 4). It asks git
// rather than looking for a .git directory: a submodule and a linked worktree
// both carry a .git *file*, and the presence of either says nothing about a
// tag anyway. Without the git introspection group there is no answer to be
// had, and none is invented.
func resolveGit(root string, options Options) (Result, bool) {
	described, ok := askGit(root, options, "describe", "--tags", "--always", "--dirty")
	if !ok {
		return Result{}, false
	}
	return gitVersion(described, func() (string, bool) {
		return askGit(root, options, "rev-parse", "HEAD")
	})
}

// resolveGitCommit records the commit as a version of its own (section 20.2,
// point 5), which only an explicit versionFrom rule may ask for: a commit
// names the content but not the release, so it is never a fallback.
func resolveGitCommit(root string, options Options) (Result, bool) {
	sha, ok := askGit(root, options, "rev-parse", "HEAD")
	if !ok {
		return Result{}, false
	}
	return commitVersion(sha)
}

// askGit puts one of the allowlisted questions of section 9.2 to the checkout
// at root and returns the trimmed answer. A nil runner, a disabled git group,
// a root outside the registered anchors and a command that failed are one
// outcome here: nothing was learned, so nothing is reported.
func askGit(root string, options Options, args ...string) (string, bool) {
	if root == "" || options.Runner == nil || !options.Runner.Features.Git {
		return "", false
	}
	out, err := options.Runner.Run(options.Context, "git", append([]string{"-C", root}, args...)...)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// gitVersion is the whole decision the git rule makes once git has answered.
// head reads the full SHA of HEAD and is consulted lazily, because it changes
// the outcome in exactly one case: with no tag in sight --always answers with
// a bare abbreviation of HEAD, and that is a commit rather than the exact tag
// on a clean tree to which section 20.3 reserves high confidence.
func gitVersion(described string, head func() (string, bool)) (Result, bool) {
	if described == "" {
		return Result{}, false
	}
	dirty := strings.HasSuffix(described, "-dirty")
	tag := strings.TrimSuffix(described, "-dirty")
	if tag == "" {
		return Result{}, false
	}
	result := Result{
		Version:    strings.TrimPrefix(tag, "v"),
		Source:     "git-describe",
		Confidence: domain.ConfidenceMedium,
		Dirty:      dirty,
	}
	// A modified tree does not hold the content the tag names, and a
	// "-g<sha>" suffix means the tag is some commits behind this one. Either
	// way the answer describes this commit only approximately.
	if dirty || strings.Contains(tag, "-g") {
		return result, true
	}
	// When the commit cannot be read, a tag and an abbreviation cannot be told
	// apart, and the lower rating is the honest one.
	if sha, ok := head(); ok && !strings.HasPrefix(sha, tag) {
		result.Confidence = domain.ConfidenceHigh
	}
	return result, true
}

// commitVersion shapes a commit into the version of section 20.2 point 5: the
// SHA cut to twelve characters, below a 0.0.0 that cannot be mistaken for a
// release. An answer that is not a full SHA is no evidence of a commit, so
// nothing is built from it.
func commitVersion(sha string) (Result, bool) {
	if !commitPattern.MatchString(sha) {
		return Result{}, false
	}
	return Result{
		Version:    "0.0.0-git." + sha[:12],
		Source:     "git-commit",
		Confidence: domain.ConfidenceMedium,
	}, true
}

// PURL builds a package URL of section 20.4. The type may be given with or
// without the scheme; the result always carries it, because a "purl" without
// "pkg:" is not one and the document validator rejects it.
func PURL(pkgType, name, version string) string {
	if !strings.HasPrefix(pkgType, "pkg:") {
		pkgType = "pkg:" + pkgType
	}
	encodedName := strings.ReplaceAll(name, "%", "%25")
	encodedName = strings.ReplaceAll(encodedName, "+", "%2B")
	encodedName = strings.ReplaceAll(encodedName, "@", "%40")
	encodedName = strings.ReplaceAll(encodedName, "/", "%2F")
	if version == "" {
		return fmt.Sprintf("%s/%s", pkgType, encodedName)
	}
	return fmt.Sprintf("%s/%s@%s", pkgType, encodedName, version)
}
