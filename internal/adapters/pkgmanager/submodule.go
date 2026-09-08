package pkgmanager

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
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

	packages := make([]Package, 0)
	findings := make([]domain.Finding, 0)
	visitedDirs := make(map[string]bool)
	claimedKeys := make(map[string]bool)

	type scanTarget struct {
		dir string
	}
	queue := []scanTarget{{dir: root}}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if visitedDirs[curr.dir] {
			continue
		}
		visitedDirs[curr.dir] = true

		gitmodulesPath := filepath.Join(curr.dir, ".gitmodules")
		entries, err := parseGitmodules(gitmodulesPath)
		if err != nil || len(entries) == 0 {
			continue
		}

		for _, entry := range entries {
			name := filepath.Base(entry.path)
			if name == "" || name == "." {
				continue
			}
			submoduleRoot := filepath.Join(curr.dir, filepath.FromSlash(entry.path))
			anchorKey := "extern:" + name
			if claimedKeys[anchorKey] {
				anchorKey = "extern:" + strings.ReplaceAll(strings.Trim(entry.path, "/"), "/", "-")
			}
			claimedKeys[anchorKey] = true

			found := Package{
				Name:      name,
				Roots:     []string{submoduleRoot},
				Manager:   a.Manager(),
				AnchorKey: anchorKey,
				VCSURL:    NormalizeVCSURL(entry.url),
			}
			a.refineFromGit(options, &found, &findings)
			if found.Version.Value == "" {
				findings = append(findings, domain.Finding{
					ID: "UNKNOWN_VERSION", Severity: domain.SeverityWarning,
					Subject:     domain.Subject{Kind: "component", Ref: name},
					Message:     "the submodule declares no version; run with --allow-introspection=git to read it from the checkout",
					Remediation: "Enable git introspection, or set components[].version for this submodule.",
				})
			}
			// The purl restates whichever version claim won, so it is taken
			// with that claim's standing; without introspection there is no
			// version and the purl rests on .gitmodules alone.
			purlRank := found.Version.Rank
			if purlRank == RankNone {
				purlRank = RankDeclaredManifest
			}
			found.Take(FieldPURL, Claim{
				Value:  GenericPURL(found.Name, found.Version.Value, found.VCSURL, found.Commit),
				Source: a.Manager(), Rank: purlRank,
			})
			packages = append(packages, found)

			if !visitedDirs[submoduleRoot] {
				if _, err := os.Stat(filepath.Join(submoduleRoot, ".gitmodules")); err == nil {
					queue = append(queue, scanTarget{dir: submoduleRoot})
				}
			}
		}
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
	if remote, err := options.Runner.Run(options.Context, "git", "-C", found.Root(), "config", "--get", "remote.origin.url"); err == nil {
		if u := strings.TrimSpace(string(remote)); u != "" {
			found.VCSURL = NormalizeVCSURL(u)
		}
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
	trimmed := strings.TrimSuffix(value, "-dirty")
	exact := !found.Dirty && !strings.Contains(trimmed, "-g")
	confidence := domain.ConfidenceHigh
	if !exact {
		confidence = domain.ConfidenceMedium
	}
	// .gitmodules names no revision at all, so the checkout is the only origin
	// there is for a version here -- and it would outrank a declaration anyway,
	// because it reports what the tree holds rather than what was asked for.
	found.Take(FieldVersion, Claim{
		Value:      strings.TrimPrefix(trimmed, "v"),
		Source:     "git-describe",
		Rank:       RankObservedCheckout,
		Confidence: confidence,
	})
	if commit, err := options.Runner.Run(options.Context, "git", "-C", found.Root(), "rev-parse", "HEAD"); err == nil {
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
	scanner := limits.Scanner(file)
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
