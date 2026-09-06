package pkgmanager

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// submodule reads .gitmodules, which is strategy 3 of section 19.2: a git
// submodule is a component boundary the project itself declared.
//
// Section 19.4 governs what may be done with that: git metadata may describe a
// component but must never expand the used-file set. A checked-out submodule
// that nothing links is not a dependency of the product, and because adapters
// here only annotate files the evidence chain already reached, that holds by
// construction.
type submodule struct{}

func (submodule) Manager() string { return "git-submodule" }

const maxGitmodulesBytes = 1 << 20

func (a submodule) Discover(options Options) ([]Package, []domain.Finding) {
	root := options.SourceDir
	if root == "" || root == "." {
		return nil, nil
	}
	entries, err := parseGitmodules(filepath.Join(root, ".gitmodules"))
	if err != nil || len(entries) == 0 {
		return nil, nil
	}
	packages := make([]Package, 0, len(entries))
	findings := make([]domain.Finding, 0)
	for _, entry := range entries {
		name := filepath.Base(entry.path)
		if name == "" || name == "." {
			continue
		}
		found := Package{
			Name:      name,
			Root:      filepath.Join(root, filepath.FromSlash(entry.path)),
			Manager:   a.Manager(),
			AnchorKey: "extern:" + name,
			VCSURL:    NormalizeVCSURL(entry.url),
		}
		a.refineFromGit(options, &found, &findings)
		if found.Version == "" {
			findings = append(findings, domain.Finding{
				ID: "UNKNOWN_VERSION", Severity: domain.SeverityWarning,
				Subject:     domain.Subject{Kind: "component", Ref: name},
				Message:     "the submodule declares no version; run with --allow-introspection=git to read it from the checkout",
				Remediation: "Enable git introspection, or set components[].version for this submodule.",
			})
		}
		found.PURL = GenericPURL(found.Name, found.Version, found.VCSURL, found.Commit)
		packages = append(packages, found)
	}
	return packages, findings
}

// refineFromGit asks the checkout what it is. Without introspection a
// submodule contributes a boundary and a URL but no version: .gitmodules
// records neither a tag nor a commit, and guessing one from a directory name
// is what section 20.1 forbids.
func (submodule) refineFromGit(options Options, found *Package, findings *[]domain.Finding) {
	if options.Runner == nil || !options.Runner.Features.Git {
		return
	}
	described, err := options.Runner.Run(options.Context, "git", "-C", found.Root,
		"describe", "--tags", "--always", "--dirty")
	if err != nil {
		return
	}
	value := strings.TrimSpace(string(described))
	if value == "" {
		return
	}
	found.Dirty = strings.HasSuffix(value, "-dirty")
	trimmed := strings.TrimSuffix(value, "-dirty")
	exact := !found.Dirty && !strings.Contains(trimmed, "-g")
	found.Version = strings.TrimPrefix(trimmed, "v")
	found.VersionSource = "git-describe"
	if exact {
		found.VersionConfidence = domain.ConfidenceHigh
	} else {
		found.VersionConfidence = domain.ConfidenceMedium
	}
	if commit, err := options.Runner.Run(options.Context, "git", "-C", found.Root, "rev-parse", "HEAD"); err == nil {
		found.Commit = strings.TrimSpace(string(commit))
	}
	if found.Dirty {
		*findings = append(*findings, domain.Finding{
			ID: "VCS_DIRTY", Severity: domain.SeverityInfo,
			Subject: domain.Subject{Kind: "component", Ref: found.Name},
			Message: "the submodule has uncommitted changes, so its version does not identify its content",
		})
	}
}

// gitmoduleEntry is one submodule declaration.
type gitmoduleEntry struct {
	path string
	url  string
}

// parseGitmodules reads the git config subset .gitmodules uses. It is a
// declared boundary, not a heuristic: the project states which directories are
// separate repositories.
func parseGitmodules(path string) ([]gitmoduleEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxGitmodulesBytes {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make([]gitmoduleEntry, 0)
	var current *gitmoduleEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[submodule") {
			if current != nil && current.path != "" {
				entries = append(entries, *current)
			}
			current = &gitmoduleEntry{}
			continue
		}
		if current == nil {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "path":
			current.path = strings.TrimSpace(value)
		case "url":
			current.url = strings.TrimSpace(value)
		}
	}
	if current != nil && current.path != "" {
		entries = append(entries, *current)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries, nil
}
