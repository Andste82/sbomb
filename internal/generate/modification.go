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
//	package metadata records an applied patch               -> true
//	dirty tree at the component root                        -> true
//	declared revision resolves to the commit HEAD stands on -> false
//	declared revision resolves to a different commit        -> true
//	anything else, introspection off included               -> unknown
//
// What makes the third line a positive check is that the declared revision
// does not come from the checkout. A tag does: git cannot tell a release tag
// from any other, so `v1.2.3`, `acme-1` and `poc-dingsbums` are one kind of
// object to it, and a maintainer who tags their own fix stands on a tag at
// distance zero. Standing on *some* tag was what this used to answer "false"
// to, and it was not evidence.
//
// The comparison is by object name rather than by history, which is what keeps
// the positive answer reachable in the shallow clone a CI job checks out:
// `rev-parse HEAD` and `show-ref --tags` both answer without a history to
// walk. The distance is a refinement on top and not the foundation.

// resolveModification settles the modification status of one component and
// records the evidence behind it, so that the writer can fill
// component.pedigree with the signal that decided the answer rather than with
// the answer alone.
func (r *componentResolver) resolveModification(component *domain.Component, rootInfo componentRootResult, declared string) []domain.Finding {
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
		r.readGitModification(rootInfo.Physical, declared, &record)
	}

	component.Modification = record
	component.Properties = addProperty(component.Properties, "sbomb:component:modified", string(record.Status))
	if declared != "" {
		// Section 19.4: recorded whatever the status came out as. It is a fact
		// a consumer can check against an upstream it has, where the status is
		// a conclusion drawn from it.
		component.Properties = addProperty(component.Properties, "sbomb:component:declaredRevision", declared)
	}
	if record.Commit != "" && component.VCS == nil {
		// The same reasoning for the commit. It reaches the document through
		// the pedigree for a settled status and through the VCS record where a
		// manager found a repository -- and through neither for a component
		// that is `unknown` and that no manager owns, which is exactly the
		// case where a consumer most needs the fact the answer was missing
		// from. An unknown status may fill no pedigree (section 28), so this
		// is where it goes.
		component.Properties = addProperty(component.Properties, "sbomb:component:vcsCommit", record.Commit)
	}
	if record.Status == domain.ModificationUnknown {
		remediation := record.Remediation
		switch {
		case remediation != "":
		case !gitRoot:
			remediation = "The component root is not a repository checkout and no package manager recorded a patch for it, so there is nothing here to compare; nothing sbomb may read can settle it."
		default:
			remediation = "Run with --allow-introspection=git so that the component's checkout can be asked."
		}
		// The signal says which of the several unknowns this is, and an
		// unknown status fills no pedigree (section 28) -- so without it here
		// the reason would not reach the document at all.
		message := "whether this component was modified could not be established, so it is reported as unknown rather than as unmodified"
		if record.Signal != "" {
			message += ": " + record.Signal
		}
		findings = append(findings, componentFinding("FOSS_MODIFICATION_UNKNOWN", domain.SeverityInfo, component,
			message, remediation))
	}
	return findings
}

// readGitModification asks the checkout at root what it is, using the three
// commands section 9.2 permits and no others.
//
// `git describe --tags --always --dirty` answers the dirty state and, where
// HEAD stands past a tag, the distance to it. `git rev-parse HEAD` is the
// commit the checkout stands on, and `git show-ref --tags` is what turns a
// declared tag name into a commit so the two can be compared.
func (r *componentResolver) readGitModification(root, declared string, record *domain.ModificationRecord) {
	described, err := r.runner.Run(r.ctx, "git", "-C", root, "describe", "--tags", "--always", "--dirty")
	if err != nil {
		if record.Signal == "" {
			record.Signal = "the component root is a repository, but git could not describe it"
			record.Remediation = "Check that the component root is a readable repository; git answered nothing that could be compared."
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
	if record.Commit == "" {
		// Without HEAD there is nothing to compare against, whatever anybody
		// declared. This is also the case `--always` produces: the answer may
		// be a tag and may be an abbreviated commit, and only HEAD tells them
		// apart.
		record.Signal = "the component root's checkout is clean, and HEAD could not be read, so it could not be compared against anything"
		record.Remediation = "Check that the checkout has a readable HEAD; without it there is no commit to compare a declared revision to."
		return
	}
	if declared == "" {
		// Nothing but the checkout says anything about this component, and the
		// checkout cannot corroborate itself. Section 19.4: a tag alone is not
		// a positive check, and without one there is no second statement to
		// hold it against.
		record.Signal = "the component root's checkout is clean, and no package manager declared a revision to compare it against"
		record.Remediation = "Declare the revision in the build -- a FetchContent GIT_TAG, a west revision, a CPM lock entry -- so that the checkout has something to be held against. A tag alone is not evidence: git cannot tell a release tag from any other."
		return
	}
	target, ok := r.resolveDeclaredRevision(root, declared)
	if !ok {
		// The ordinary state of a shallow clone: the tag was never fetched, so
		// there is nothing here that the declared name points at.
		record.Signal = fmt.Sprintf("the declared revision %s names no commit in this checkout, so it could not be compared", declared)
		record.Remediation = "Fetch the revision the declaration names -- a full clone, or --branch <tag> where the clone is shallow -- so that it can be resolved to a commit."
		return
	}
	if target == record.Commit {
		record.Status = domain.ModificationUnmodified
		record.Signal = fmt.Sprintf("the component root's checkout is clean and stands on the declared revision %s", declared)
		return
	}
	// Not the revision that was asked for. Whether somebody patched it or the
	// manifest has fallen behind a newer upstream cannot be told apart from
	// inside this repository, so the signal states what was compared and
	// attributes the difference to nobody.
	record.Status = domain.ModificationModified
	if declaredTagOf(checkout.Tag, declared) && checkout.Distance > 0 {
		// The distance is already in the describe answer, and it is about the
		// declared tag rather than merely the nearest one -- so it says how
		// far, not just that.
		record.Signal = fmt.Sprintf("the component root's checkout is clean and stands %d commit(s) past the declared revision %s",
			checkout.Distance, declared)
		return
	}
	record.Signal = fmt.Sprintf("the component root's checkout is clean and stands on a commit the declared revision %s does not name", declared)
}

// resolveDeclaredRevision turns what a package manager declared into the
// commit it names, or reports that this checkout does not carry it.
//
// A full object name is that commit and needs no lookup. Anything else is a
// tag name, and the only two spellings tried are the declared name and the
// same name with a leading `v`: the `v` of `v1.2.3` is the one convention
// universal enough to state, and matching by pattern beyond it would be the
// guessing section 19.4 exists to refuse.
func (r *componentResolver) resolveDeclaredRevision(root, declared string) (string, bool) {
	if isObjectName(declared) {
		return strings.ToLower(declared), true
	}
	listed, err := r.runner.Run(r.ctx, "git", "-C", root, "show-ref", "--tags")
	if err != nil {
		return "", false
	}
	wanted := map[string]bool{
		"refs/tags/" + declared:          true,
		"refs/tags/v" + declared:         true,
		"refs/tags/" + declared + "^{}":  true,
		"refs/tags/v" + declared + "^{}": true,
	}
	for _, line := range strings.Split(string(listed), "\n") {
		commit, ref, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found {
			continue
		}
		if wanted[ref] {
			return strings.ToLower(commit), true
		}
	}
	return "", false
}

// declaredTagOf says whether the tag `git describe` answered with is the one
// that was declared, in either of the two spellings section 19.4 permits. A
// distance measured against any other tag says nothing about the declared
// revision.
func declaredTagOf(tag, declared string) bool {
	return tag == declared || tag == "v"+declared
}

// isObjectName says whether a declared revision is a full commit rather than a
// tag name. Only the full form counts: an abbreviation would have to be
// matched as a prefix, and a prefix match is an assumption about how many
// characters somebody meant to be significant.
func isObjectName(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// declaredRevisionOf is the revision the owning package manager named for this
// component, or the empty string. A component no manager owns has nobody to
// declare one: what a marker file or a directory walk found is the checkout
// again, and the checkout cannot corroborate itself (section 19.4).
func declaredRevisionOf(managed pkgmanager.Package, isManaged bool) string {
	if !isManaged {
		return ""
	}
	return managed.DeclaredRevision
}
