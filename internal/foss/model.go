package foss

import (
	"encoding/hex"
	"path"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/license"
)

// model is what the four documents are rendered from. It is derived once, so
// that the notices document, the review record and the obligations document
// cannot disagree about a component: three renderings of one model, not three
// readings of one document.
type model struct {
	product string
	version string
	// distributed are the entries of THIRD-PARTY-NOTICES.txt: every grouping
	// component whose distribution role is "distributed" and whose CycloneDX
	// type is not "application" (section 32.6). Ordered by (name, bom-ref).
	distributed []entry
	// buildTimeOnly are the components that only helped build the product.
	// They are in the review record and in neither of the other two
	// documents: naming a component the product does not contain invites an
	// obligation that was never triggered (requirement R1).
	buildTimeOnly []entry
	// texts are the unique retained texts, keyed by the SHA-256 of the bytes,
	// so that a text several components share is printed once with those
	// components listed under it (decision Q13).
	texts map[string]*sharedText
	// roleUnestablished are the components whose distribution role the graph
	// derivation never answered, which is not a third value of section 24.5
	// but the absence of a derivation: section 24.2's synthetic
	// build-environment grouping has no files of its own, so no chain from an
	// artifact reaches it. They are in the review record and in neither
	// shippable document -- a grouping node of sbomb's own is not a
	// third-party component -- and they are named rather than dropped,
	// because a component that is in none of the three lists above would
	// otherwise disappear without a word.
	roleUnestablished []entry
	// byArtifact maps a deliverable to the components reaching it, in
	// assembly mode and only there (decision Q10). It is empty in
	// single-artifact mode, where it would restate the component list.
	byArtifact map[string][]string
	// view is the licence-view delta of section 32.6.
	view View
	// run is the review record's run block.
	run runBlock
	// findings are the FOSS findings of the run, waived ones included.
	findings []domain.Finding
}

type runBlock struct {
	profile string
	// No build directory, and deliberately: the only form of it a renderer
	// could print is the directory the evidence was read from, which is a
	// host path that section 7.8 keeps out of every human-facing output and
	// that would make two runs over two copies of one build produce two
	// records.
	mode           string
	headerEvidence string
	reproducible   bool
}

// entry is one component as the documents describe it.
type entry struct {
	name    string
	bomRef  string
	version string
	// license is the expression the component resolved to, or the empty
	// string when it resolved to none. NOASSERTION is "none" spelled the way
	// section 22.7 spells it, and is normalized away here so that no renderer
	// has to know the spelling.
	license        string
	licenseClass   string
	origin         string
	role           string
	linkage        []string
	modified       string
	modifiedSignal string
	usedFiles      int
	obligations    []string
	artifacts      []domain.LicenseArtifact
	copyrights     []domain.CopyrightStatement
	// narrowed is how many headers of this component the SBOM view dropped
	// and the licence view keeps.
	narrowed int
	// addedCopyrights is how many statements came from those headers alone.
	addedCopyrights int
	// assessment is what the committed obligation list says about this
	// component's licence, derived once so that the review record and
	// source-obligations.txt cannot disagree about it.
	assessment Assessment
}

// sharedText is one unique retained text and the components that carry it.
type sharedText struct {
	digest string
	// owner is the component that prints the bytes, and label the name of the
	// file it printed them from.
	owner string
	label string
	// carriers are every component carrying these bytes, in entry order.
	carriers []string
}

// build derives the model from the document.
func build(in Input) model {
	m := model{
		product: in.Document.Product.Name,
		version: in.Document.Product.Version,
		texts:   map[string]*sharedText{},
		view:    in.View,
		run: runBlock{
			profile:        in.Profile,
			mode:           in.Mode,
			headerEvidence: in.HeaderEvidence,
			reproducible:   in.Document.Run.Reproducible,
		},
		findings: fossFindings(in.Findings),
	}

	usedFiles := map[string]int{}
	for _, relation := range in.Document.Relations {
		usedFiles[relation.From] = len(relation.To)
	}

	for _, component := range in.Document.Components {
		// Section 32.6: the criterion is the component type, not the anchor
		// scope. A library copied into the source tree carries scope=project
		// and would be dropped by the scope, which is exactly backwards.
		if component.Type == "application" {
			continue
		}
		candidate := newEntry(component, usedFiles[component.ID], in.View)
		switch {
		case Reported(component):
			m.distributed = append(m.distributed, candidate)
		case component.DistributionRole == domain.RoleBuildTimeOnly:
			m.buildTimeOnly = append(m.buildTimeOnly, candidate)
		default:
			m.roleUnestablished = append(m.roleUnestablished, candidate)
		}
	}
	sortEntries(m.distributed)
	sortEntries(m.buildTimeOnly)
	sortEntries(m.roleUnestablished)
	m.byArtifact = componentsPerArtifact(in)

	// Which component prints which text is decided after the order is fixed,
	// so that the same run always prints the same bytes under the same
	// component.
	for _, candidate := range m.distributed {
		for _, artifact := range candidate.artifacts {
			shared, known := m.texts[artifact.SHA256]
			if !known {
				m.texts[artifact.SHA256] = &sharedText{
					digest:   artifact.SHA256,
					owner:    candidate.name,
					label:    path.Base(artifact.File.RelPath),
					carriers: []string{candidate.name},
				}
				continue
			}
			if shared.carriers[len(shared.carriers)-1] != candidate.name {
				shared.carriers = append(shared.carriers, candidate.name)
			}
		}
	}
	return m
}

// newEntry is one component's row, with the licence view merged in.
func newEntry(component domain.Component, usedFiles int, view View) entry {
	delta := view.deltaFor(component.Name)
	row := entry{
		name:           component.Name,
		bomRef:         bomRefOf(component),
		version:        component.Version,
		license:        resolvedLicense(component),
		licenseClass:   licenseClass(component),
		origin:         origin(component),
		role:           component.DistributionRole,
		linkage:        component.LinkageForms,
		modified:       modificationOf(component),
		modifiedSignal: component.Modification.Signal,
		usedFiles:      usedFiles,
		obligations:    component.SourceObligations,
		artifacts:      component.LicenseArtifacts,
		narrowed:       delta.Narrowed,
	}
	// The union view of section 32.6 is the only place the two views differ,
	// and this is where the difference lands: the statements the narrowed
	// headers of this component state, merged into the ones the document
	// carries, deduplicated and ordered by text like every other list.
	merged := append([]domain.CopyrightStatement{}, component.Copyrights...)
	merged = append(merged, delta.Copyrights...)
	row.copyrights = license.DedupeCopyright(merged)
	row.addedCopyrights = len(row.copyrights) - len(component.Copyrights)
	row.assessment = Assess(row.license, component.LinkageForms)
	return row
}

// sortEntries is the order of section 29: by name, then by bom-ref. The
// bom-ref is version-free, so a diff between two releases stays readable.
func sortEntries(entries []entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].name != entries[j].name {
			return entries[i].name < entries[j].name
		}
		return entries[i].bomRef < entries[j].bomRef
	})
}

// bomRefOf is the component's reference as the document will carry it. The
// writer derives the bom-ref from the identity (section 28.4), so the
// document handed to a renderer usually carries the identity and not the
// rendered reference -- and for a grouping component the two are the same
// string. The identity is therefore the tiebreaker section 29 means: it is
// version-free, so a diff between two releases stays readable.
func bomRefOf(component domain.Component) string {
	if component.BomRef != "" {
		return component.BomRef
	}
	return component.ID
}

// resolvedLicense is the component's licence expression, or the empty string
// when nothing was resolved.
func resolvedLicense(component domain.Component) string {
	for _, finding := range component.Licenses {
		value := finding.Expression
		if value == "" {
			value = finding.SPDXID
		}
		if value == "" {
			value = finding.Name
		}
		value = strings.TrimSpace(value)
		if value == "" || value == "NOASSERTION" {
			continue
		}
		return value
	}
	return ""
}

// licenseClass is the evidence class of section 22.4 behind the licence, so a
// reviewer can tell a curated conclusion from a file-level observation.
func licenseClass(component domain.Component) string {
	for _, finding := range component.Licenses {
		if finding.Evidence != "" {
			return finding.Evidence
		}
	}
	return ""
}

// origin is where the component's source is kept, as the package manager
// recorded it. It is a URL and a commit and never a path: the notices document
// carries no filesystem path at all (decision Q8).
func origin(component domain.Component) string {
	if component.VCS == nil || component.VCS.URL == "" {
		return ""
	}
	if component.VCS.Commit == "" {
		return component.VCS.URL
	}
	return component.VCS.URL + " @ " + component.VCS.Commit
}

// modificationOf is the tri-state of section 19.4, with the zero value read
// as "unknown": absence of information is never "not modified".
func modificationOf(component domain.Component) string {
	if component.Modification.Status == "" {
		return string(domain.ModificationUnknown)
	}
	return string(component.Modification.Status)
}

// grants are the retained artifacts that are a licence grant, which is what
// the attribution obligation of requirement R2 is about. A NOTICE is retained
// for reproduction (requirement R4) and is not a grant.
func (e entry) grants() []domain.LicenseArtifact {
	out := make([]domain.LicenseArtifact, 0, len(e.artifacts))
	for _, artifact := range e.artifacts {
		if artifact.Kind == domain.LicenseArtifactLicense {
			out = append(out, artifact)
		}
	}
	return out
}

// fossFindings are the findings of the FOSS view, waived ones included. The
// prefix is the selector because every finding this track added carries it,
// and the two that predate it are named explicitly.
func fossFindings(findings []domain.Finding) []domain.Finding {
	out := make([]domain.Finding, 0, len(findings))
	for _, finding := range findings {
		switch {
		case strings.HasPrefix(finding.ID, "FOSS_"):
		case finding.ID == "UNKNOWN_LICENSE", finding.ID == "COMPONENT_ROOT_UNRESOLVED",
			finding.ID == "UNKNOWN_COMPONENT", finding.ID == "SOURCE_TREE_UNAVAILABLE",
			finding.ID == "LICENSE_CONFLICT", finding.ID == "VCS_DIRTY":
			// The four signals that say the mapping or the source tree, and
			// therefore the attribution, went wrong (section 32.6).
		default:
			continue
		}
		out = append(out, finding)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Subject.Ref < out[j].Subject.Ref
	})
	return out
}

// componentsPerArtifact groups the reported components by the deliverable
// their files reach, which is what section 6.3 already records per file: a
// file shared by two artifacts names both.
//
// It answers only in assembly mode. In single-artifact mode every component
// reaches the one artifact, and a section saying so would be noise.
func componentsPerArtifact(in Input) map[string][]string {
	if len(in.Document.Artifacts) == 0 {
		return nil
	}
	artifactsOf := map[string][]string{}
	for _, file := range in.Document.Files {
		if refs := file.Properties["sbomb:evidence:artifacts"]; len(refs) > 0 {
			artifactsOf[file.ID.Canonical()] = refs
		}
	}
	reported := map[string]bool{}
	for _, component := range in.Document.Components {
		if component.Type == "application" {
			continue
		}
		reported[component.ID] = true
	}
	byArtifact := map[string]map[string]bool{}
	for _, relation := range in.Document.Relations {
		if !reported[relation.From] {
			continue
		}
		name := relation.From
		for _, component := range in.Document.Components {
			if component.ID == relation.From {
				name = component.Name
				break
			}
		}
		for _, file := range relation.To {
			for _, artifact := range artifactsOf[file] {
				if byArtifact[artifact] == nil {
					byArtifact[artifact] = map[string]bool{}
				}
				byArtifact[artifact][name] = true
			}
		}
	}
	if len(byArtifact) == 0 {
		return nil
	}
	out := make(map[string][]string, len(byArtifact))
	for artifact, names := range byArtifact {
		out[artifact] = sortedKeys(names)
	}
	return out
}

// shortDigest is the first bytes of a SHA-256, for a reference from one entry
// to a text printed under another. The full digest is in the review record and
// in the document; here it only has to be unambiguous to a reader.
func shortDigest(digest string) string {
	if len(digest) <= 16 {
		return digest
	}
	if _, err := hex.DecodeString(digest[:16]); err != nil {
		return digest
	}
	return digest[:16]
}
