package generate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/example/sbomb/internal/adapters/pkgmanager"
	"github.com/example/sbomb/internal/domain"
)

// Modification status, section 19.4, in three states. The third one is the
// whole point: an auditor reading "not modified" is entitled to assume that
// somebody looked, so a check that never ran must not answer "false".
//
//	dirty tree at the component root                     -> true
//	package metadata records an applied patch            -> true
//	git root found, clean, standing on its recorded tag  -> false
//	anything else, introspection off included            -> unknown
//
// Comparison against the recorded upstream is not implemented. It needs
// `git rev-list --count <upstream>..HEAD`, which is not on the allowlist of
// section 9.2, and that allowlist is a security boundary rather than a
// convenience. What it would cost is recorded in docs/dev/open-questions.md.

// resolveModification settles the modification status of one component and
// records the evidence behind it, so that the writer can fill
// component.pedigree with the signal that decided the answer rather than with
// the answer alone.
func (r *componentResolver) resolveModification(component *domain.Component, rootInfo componentRootResult) []domain.Finding {
	findings := []domain.Finding{}
	record := domain.ModificationRecord{Status: domain.ModificationUnknown}

	// A patch a manager recorded is the strongest signal there is: it names
	// the change rather than detecting that one happened.
	patches, patchFindings := pkgmanager.Patches(rootInfo.Physical)
	findings = append(findings, patchFindings...)
	if len(patches) > 0 {
		record.Patches = patches
		record.Status = domain.ModificationModified
		record.Signal = fmt.Sprintf("%s records %d applied patch(es)", patches[0].Source, len(patches))
	}

	gitRoot := pkgmanager.HasGitRoot(rootInfo.Physical)
	switch {
	case !gitRoot:
		// Nothing more can be established. A component whose root is not a
		// repository root is not thereby unmodified.
	case r.runner == nil || !r.runner.Features.Git:
		if record.Status == domain.ModificationUnknown {
			record.Signal = "a git root was found at the component root but git introspection is off"
		}
	default:
		r.readGitModification(rootInfo.Physical, &record)
	}

	component.Modification = record
	component.Properties = addProperty(component.Properties, "sbomb:component:modified", string(record.Status))
	if record.Status == domain.ModificationUnknown {
		remediation := "Run with --allow-introspection=git so that the component's checkout can be asked."
		if !gitRoot {
			remediation = "No repository and no package-manager patch record were found at the component root; a curated components[] entry is the only way to state this."
		}
		findings = append(findings, componentFinding("FOSS_MODIFICATION_UNKNOWN", domain.SeverityInfo, component,
			"whether this component was modified could not be established, so it is reported as unknown rather than as unmodified",
			remediation))
	}
	return findings
}

// readGitModification asks the checkout at root what it is, using the two
// commands section 9.2 permits and no others.
//
// `git describe --tags --always --dirty` answers both questions in one call --
// whether the tree is dirty, and whether HEAD stands exactly on a tag -- but
// `--always` falls back to an abbreviated commit for a repository with no tag,
// and a bare hash is not a tag match. `git rev-parse HEAD` is what tells the
// two apart, and its answer is the pedigree's commit either way.
func (r *componentResolver) readGitModification(root string, record *domain.ModificationRecord) {
	described, err := r.runner.Run(r.ctx, "git", "-C", root, "describe", "--tags", "--always", "--dirty")
	if err != nil {
		if record.Signal == "" {
			record.Signal = "the component root is a repository, but git could not describe it"
		}
		return
	}
	value := strings.TrimSpace(string(described))
	if value == "" {
		return
	}
	if commit, err := r.runner.Run(r.ctx, "git", "-C", root, "rev-parse", "HEAD"); err == nil {
		record.Commit = strings.TrimSpace(string(commit))
	}

	if strings.HasSuffix(value, "-dirty") {
		// A patch record already said "modified"; the dirty tree says it
		// again, and the signal names the stronger of the two.
		record.Status = domain.ModificationModified
		if record.Signal == "" {
			record.Signal = "the component root's checkout has uncommitted changes"
		}
		return
	}
	if record.Status == domain.ModificationModified {
		// A clean tree does not undo a recorded patch: the patch was applied
		// before the tree was committed.
		return
	}
	if exactTagDescribe(value, record.Commit) {
		record.Status = domain.ModificationUnmodified
		record.Signal = fmt.Sprintf("the component root's checkout is clean and stands on its recorded tag %s", value)
		return
	}
	// Clean, but not on a tag: either there is no tag at all or HEAD is some
	// number of commits past one, and section 9.2 permits no command that
	// counts them. That is unknown, not unmodified.
	record.Signal = "the component root's checkout is clean but does not stand on a recorded tag, and the distance to one cannot be established"
}

// describeDistance matches the suffix `git describe` appends when HEAD is some
// number of commits past the nearest tag: a dash, the count, and the
// abbreviated commit behind a "g".
//
// The whole suffix is matched, anchored at the end, rather than the "g" that
// introduces the hash. A tag name may contain the two characters "-g" --
// `v1.0-gamma`, `release-gcc13` -- and reading such a tag as a distance suffix
// would report `unknown` for a component standing exactly on it. The three
// package-manager adapters that ask `git describe` for the same reason
// (west.go, fetchcontent.go, submodule.go) still test for the substring; their
// answers feed other sections and other goldens, and changing them belongs to
// those sections.
var describeDistance = regexp.MustCompile(`-[0-9]+-g[0-9a-f]{4,}$`)

// exactTagDescribe reports whether a `git describe --tags --always --dirty`
// answer is a tag and nothing else.
//
// Three shapes come back: a tag, a tag with a commit distance suffix
// (`v1.2.0-4-gdeadbee`), and -- because of `--always` -- an abbreviated commit
// for a repository that has no tag in HEAD's history. The third is why the
// commit is needed: an abbreviated hash is a prefix of the full one, and a tag
// name is not.
func exactTagDescribe(described, commit string) bool {
	if described == "" {
		return false
	}
	if commit != "" && strings.HasPrefix(commit, described) {
		return false
	}
	return !describeDistance.MatchString(described)
}
