package generate

import (
	"fmt"
	"strings"

	"github.com/example/sbomb/internal/adapters/pkgmanager"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/version"
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
	for _, finding := range patchFindings {
		// The reader works on a directory and has no component to name, so it
		// leaves the subject to the caller. Naming it after the directory --
		// which is what it used to do -- states a physical path of section 7.9
		// in a finding a relocated run must state identically, and it resolves
		// to no component in the document either.
		finding.Subject = domain.Subject{Kind: "component", Ref: component.ID}
		findings = append(findings, finding)
	}
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

	checkout := version.DescribeCheckout(value, record.Commit)
	if checkout.Dirty {
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
	switch {
	case checkout.Ambiguous:
		// HEAD could not be read, so the answer cannot be told from the
		// abbreviated commit `--always` falls back to: it may be a tag and it
		// may be a hash. `false` requires a positive check and this is not
		// one -- which is the whole reason the third state exists. The four
		// other readers of this answer lower their confidence here; this one
		// has no confidence to lower, so it answers unknown.
		record.Signal = "the component root's checkout is clean, and HEAD could not be read, so a tag cannot be told from a commit"
	case checkout.Tagged && checkout.Distance == 0:
		record.Status = domain.ModificationUnmodified
		record.Signal = fmt.Sprintf("the component root's checkout is clean and stands on its recorded tag %s", checkout.Tag)
	case checkout.Tagged:
		// Clean, and some commits past the tag. The count is in the answer
		// already -- `git describe` writes `<tag>-<n>-g<hash>` -- so nothing
		// has to be counted and section 9.2 needs no new command. A tree
		// somebody fixed and committed is the commonest shape of a modified
		// dependency, and reporting it as unknown was reporting "not checked"
		// for something that had been checked.
		//
		// What stays unstated is *which* tag: `git describe` names the nearest
		// reachable one, and a tag the maintainer added after their own change
		// is nearer than the upstream release. Deviation D47.
		record.Status = domain.ModificationModified
		record.Signal = fmt.Sprintf("the component root's checkout is clean and stands %d commit(s) past its tag %s",
			checkout.Distance, checkout.Tag)
	default:
		// No reachable tag at all, so there is nothing to have deviated from.
		// Unknown is the answer that cannot be wrong.
		record.Signal = "the component root's checkout is clean and has no reachable tag to compare against"
	}
}
