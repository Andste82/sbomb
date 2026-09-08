package pkgmanager

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// fetchContent reads CMake's FetchContent layout (section 21). Two evidence
// paths, deliberately:
//
//   - Without introspection, which is the default, the name comes from the
//     _deps/<name>-src layout and the tag and repository from the populate
//     script CMake generates. Reading a generated build file is what every
//     other adapter here already does.
//   - With introspection, the checkout's own git metadata answers instead,
//     which is authoritative and also reports a dirty tree.
type fetchContent struct{}

func (fetchContent) Manager() string { return "fetchcontent" }

// maxPopulateScriptBytes bounds the generated script the adapter reads
// (section 30).
const maxPopulateScriptBytes = 1 << 20

var (
	// checkout "v1.2.0" --
	// CMake puts the subcommand on a continuation line of the execute_process
	// call, but the shape varies between versions, so the match is anchored on
	// the subcommand rather than on the line beginning.
	gitTagPattern = regexp.MustCompile(`(?m)\bcheckout\s+"([^"]+)"`)
	// clone --no-checkout --config "..." "<url>" "<dir>"
	gitClonePattern = regexp.MustCompile(`(?m)\bclone\s.*?"([^"]+)"\s+"[^"]+"\s*$`)
)

func (a fetchContent) Discover(options Options) ([]Package, []domain.Finding) {
	depsDir := filepath.Join(options.BuildDir, "_deps")
	entries, err := os.ReadDir(depsDir)
	if err != nil {
		return nil, nil
	}
	// FetchContent creates <name>-src, <name>-build and <name>-subbuild. Either
	// of the first and the last is enough to name the package: a build tree
	// being analysed somewhere other than where it was produced may carry the
	// generated scripts without the checked-out sources, and the source root is
	// needed as an identity, not as something to read.
	names := make([]string, 0)
	seen := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, suffix := range []string{"-src", "-subbuild"} {
			if name, ok := strings.CutSuffix(entry.Name(), suffix); ok && name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)

	packages := make([]Package, 0, len(names))
	findings := make([]domain.Finding, 0)
	for _, name := range names {
		// The checkout is the identity: git and the licence file are there, and
		// it is what the anchor names. The build tree beside it belongs to the
		// same package too -- a header CMake wrote with configure_file, and the
		// libraries built from the checkout, live there and nowhere else -- so
		// it is a second root rather than a second package. It is claimed only
		// when it exists, because a directory nobody built is not evidence.
		roots := []string{filepath.Join(depsDir, name+"-src")}
		if buildTree := filepath.Join(depsDir, name+"-build"); dirExists(buildTree) {
			roots = append(roots, buildTree)
		}
		found := Package{
			Name:      name,
			Roots:     roots,
			Manager:   a.Manager(),
			AnchorKey: "pkg:fetchcontent/" + name,
		}

		tag, repository := a.populateInfo(options.BuildDir, name)
		if repository != "" {
			found.VCSURL = NormalizeVCSURL(repository)
		}
		if tag != "" {
			found.Version = strings.TrimPrefix(tag, "v")
			found.VersionSource = "fetchcontent"
			// The declared tag is exact package-manager metadata (section 20.3).
			found.VersionConfidence = domain.ConfidenceHigh
		}

		a.refineFromGit(options, &found, &findings)

		if found.Version == "" {
			findings = append(findings, domain.Finding{
				ID: "UNKNOWN_VERSION", Severity: domain.SeverityWarning,
				Subject: domain.Subject{Kind: "component", Ref: name},
				Message: "FetchContent populated this dependency but named no tag or commit for it",
			})
		}
		found.PURL = GenericPURL(found.Name, found.Version, found.VCSURL, found.Commit)
		packages = append(packages, found)
	}
	return packages, findings
}

// populateInfo reads the tag and repository out of the script CMake generates
// for the populate step.
func (fetchContent) populateInfo(buildDir, name string) (string, string) {
	path := filepath.Join(buildDir, "_deps", name+"-subbuild",
		name+"-populate-prefix", "tmp", name+"-populate-gitclone.cmake")
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxPopulateScriptBytes {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var tag, repository string
	if match := gitTagPattern.FindSubmatch(data); match != nil {
		tag = string(match[1])
	}
	if match := gitClonePattern.FindSubmatch(data); match != nil {
		repository = string(match[1])
	}
	return tag, repository
}

// refineFromGit replaces the declared tag with what the checkout actually is,
// when introspection is allowed. A tag says what was asked for; the repository
// says what is there, including whether someone has since edited it.
func (fetchContent) refineFromGit(options Options, found *Package, findings *[]domain.Finding) {
	if options.Runner == nil || !options.Runner.Features.Git {
		return
	}
	described, err := options.Runner.Run(options.Context, "git", "-C", found.Root(),
		"describe", "--tags", "--always", "--dirty")
	if err != nil {
		return
	}
	value := strings.TrimSpace(string(described))
	if value == "" {
		return
	}
	found.Dirty = strings.HasSuffix(value, "-dirty")
	exact := !found.Dirty && !strings.Contains(value, "-g")
	found.Version = strings.TrimPrefix(strings.TrimSuffix(value, "-dirty"), "v")
	found.VersionSource = "git-describe"
	if exact {
		found.VersionConfidence = domain.ConfidenceHigh
	} else {
		// Section 20.3: a describe with distance or a dirty tree is weaker
		// evidence than an exact tag.
		found.VersionConfidence = domain.ConfidenceMedium
	}
	if commit, err := options.Runner.Run(options.Context, "git", "-C", found.Root(), "rev-parse", "HEAD"); err == nil {
		found.Commit = strings.TrimSpace(string(commit))
	}
	if remote, err := options.Runner.Run(options.Context, "git", "-C", found.Root(),
		"config", "--get", "remote.origin.url"); err == nil {
		if value := strings.TrimSpace(string(remote)); value != "" {
			found.VCSURL = NormalizeVCSURL(value)
		}
	}
	if found.Dirty {
		*findings = append(*findings, domain.Finding{
			ID: "VCS_DIRTY", Severity: domain.SeverityInfo,
			Subject: domain.Subject{Kind: "component", Ref: found.Name},
			Message: "the checkout has uncommitted changes, so its version does not identify its content",
		})
	}
}

// NormalizeVCSURL applies section 19.4: the scp-like git form becomes https,
// and credentials are stripped. A repository URL with a password in it must
// never reach the document.
func NormalizeVCSURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	// git@host:org/repo.git -> https://host/org/repo
	if !strings.Contains(value, "://") {
		if at := strings.Index(value, "@"); at >= 0 {
			if colon := strings.Index(value[at:], ":"); colon >= 0 {
				host := value[at+1 : at+colon]
				path := value[at+colon+1:]
				value = "https://" + host + "/" + strings.TrimPrefix(path, "/")
			}
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return strings.TrimSuffix(value, ".git")
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimSuffix(parsed.String(), ".git")
}

// GenericPURL builds the purl of section 20.4 for a git-derived package. A
// version alone is not enough to identify a checkout, so the repository and
// commit travel with it.
func GenericPURL(name, version, vcsURL, commit string) string {
	if name == "" {
		return ""
	}
	purl := "pkg:generic/" + url.PathEscape(name)
	if version != "" {
		purl += "@" + url.PathEscape(version)
	}
	if vcsURL == "" {
		return purl
	}
	qualifier := "git+" + vcsURL
	if commit != "" {
		qualifier += "@" + commit
	}
	return fmt.Sprintf("%s?vcs_url=%s", purl, url.QueryEscape(qualifier))
}
