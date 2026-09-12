package cyclonedx

import (
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

// Sections 19.4 and 24.5 at the document boundary. Two specified fields carry
// the FOSS attributes -- component.pedigree and component.scope -- and each
// keeps a property beside it for a reason the tests below state.

func attributeDocument() *sbomwriter.Document {
	return &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "app", Type: "application"},
		Components: []domain.Component{
			{
				ID: "component:patched", Name: "patched", Type: "library",
				DistributionRole: domain.RoleDistributed,
				LinkageForms:     []string{domain.LinkageStaticArchiveMember},
				Modification: domain.ModificationRecord{
					Status: domain.ModificationModified,
					Signal: "conandata.yml records 2 applied patch(es)",
					Commit: "0123456789abcdef0123456789abcdef01234567",
					Patches: []domain.Patch{
						{File: "0002-backport.patch", Type: domain.PatchBackport, Source: "conandata.yml",
							Description: "backport of upstream fix for the 64-bit build"},
						{File: "0001-fix-build.patch", Type: domain.PatchUnofficial, Source: "conandata.yml"},
					},
				},
				Properties: map[string][]string{
					"sbomb:component:modified":         {string(domain.ModificationModified)},
					"sbomb:component:distributionRole": {domain.RoleDistributed},
				},
			},
			{
				ID: "component:unknown-state", Name: "unknown-state", Type: "library",
				DistributionRole: domain.RoleDistributed,
				Modification:     domain.ModificationRecord{Status: domain.ModificationUnknown},
				Properties: map[string][]string{
					"sbomb:component:modified":         {string(domain.ModificationUnknown)},
					"sbomb:component:distributionRole": {domain.RoleDistributed},
				},
			},
			{
				ID: "component:clean", Name: "clean", Type: "library",
				DistributionRole: domain.RoleDistributed,
				Modification: domain.ModificationRecord{
					Status: domain.ModificationUnmodified,
					Signal: "the component root's checkout is clean and stands on its recorded tag v1.2.0",
					Commit: "89abcdef0123456789abcdef0123456789abcdef",
				},
				Properties: map[string][]string{
					"sbomb:component:modified": {string(domain.ModificationUnmodified)},
				},
			},
			{
				ID: "component:generator", Name: "generator", Type: "library",
				DistributionRole: domain.RoleBuildTimeOnly,
				LinkageForms:     []string{domain.LinkageBuildTool},
				Modification:     domain.ModificationRecord{Status: domain.ModificationUnknown},
				Properties: map[string][]string{
					"sbomb:component:distributionRole": {domain.RoleBuildTimeOnly},
					"sbomb:component:linkageForm":      {domain.LinkageBuildTool},
				},
			},
		},
		Relations: []sbomwriter.Relation{{From: "product", To: []string{
			"component:patched", "component:unknown-state", "component:clean", "component:generator"}}},
		Run: sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", ToolVersion: "0.0.0-test"},
	}
}

// The round trip the milestone asks for. A recorded patch produces a
// pedigree.patches[] entry; an unknown status produces no pedigree node at
// all, and the property still says unknown. A consumer reading only pedigree
// must not be able to mistake the third state for the second.
func TestPedigreeIsWrittenOnlyAfterAPositiveCheck(t *testing.T) {
	for _, specVersion := range []string{Version16, Version17} {
		t.Run(specVersion, func(t *testing.T) {
			data, err := MarshalDocument(attributeDocument(), sbomwriter.Options{
				SpecVersion: specVersion, Reproducible: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(data); err != nil {
				t.Fatalf("document is not valid at %s: %v", specVersion, err)
			}

			patched := componentNamed(t, data, "patched")
			if patched.Pedigree == nil {
				t.Fatal("a component with a recorded patch carries no pedigree")
			}
			if len(patched.Pedigree.Patches) != 2 {
				t.Fatalf("patches = %#v, want one entry per recorded patch", patched.Pedigree.Patches)
			}
			// Ordered by type (section 29), and a manager's own vocabulary is
			// carried over only where it is one of the four the enum has.
			if patched.Pedigree.Patches[0].Type != domain.PatchBackport ||
				patched.Pedigree.Patches[1].Type != domain.PatchUnofficial {
				t.Errorf("patches = %#v, want them ordered by type", patched.Pedigree.Patches)
			}
			if len(patched.Pedigree.Commits) != 1 ||
				patched.Pedigree.Commits[0].UID != "0123456789abcdef0123456789abcdef01234567" {
				t.Errorf("commits = %#v, want the resolved commit", patched.Pedigree.Commits)
			}
			// The schema gives a patch no field for its own name and none for
			// what the record says about it -- resolves is for the issues a
			// patch closes -- so both reach the document through notes and
			// neither is lost.
			for _, name := range []string{"0001-fix-build.patch", "0002-backport.patch",
				"backport of upstream fix for the 64-bit build"} {
				if !strings.Contains(patched.Pedigree.Notes, name) {
					t.Errorf("notes = %q, want it to name %s", patched.Pedigree.Notes, name)
				}
			}

			// The third state. No node, and the property still says so.
			unknown := componentNamed(t, data, "unknown-state")
			if unknown.Pedigree != nil {
				t.Errorf("pedigree = %#v for an unknown status; an absent node does not mean unmodified",
					unknown.Pedigree)
			}
			if got := propertyValues(unknown, "sbomb:component:modified"); len(got) != 1 || got[0] != "unknown" {
				t.Errorf("sbomb:component:modified = %v, want [unknown]", got)
			}

			// A positive negative: established clean, so a node exists and
			// says what established it.
			clean := componentNamed(t, data, "clean")
			if clean.Pedigree == nil || clean.Pedigree.Notes == "" {
				t.Fatalf("pedigree = %#v, want the signal that decided the answer", clean.Pedigree)
			}
			if len(clean.Pedigree.Patches) != 0 {
				t.Errorf("patches = %#v for a component nobody patched", clean.Pedigree.Patches)
			}
		})
	}
}

// component.scope is the specified field for the distribution role, and the
// property stays beside it because the two are not the same axis.
func TestScopeIsExcludedForABuildTimeOnlyComponent(t *testing.T) {
	for _, specVersion := range []string{Version16, Version17} {
		t.Run(specVersion, func(t *testing.T) {
			data, err := MarshalDocument(attributeDocument(), sbomwriter.Options{
				SpecVersion: specVersion, Reproducible: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(data); err != nil {
				t.Fatalf("document is not valid at %s: %v", specVersion, err)
			}
			if got := componentNamed(t, data, "generator").Scope; got != "excluded" {
				t.Errorf("scope = %q, want excluded", got)
			}
			for _, name := range []string{"patched", "unknown-state", "clean"} {
				if got := componentNamed(t, data, name).Scope; got != "required" {
					t.Errorf("scope of %s = %q, want required", name, got)
				}
			}
			// And the property keeps the evidence-based meaning, so a
			// consumer that knows sbomb sees which axis the answer is on.
			if got := propertyValues(componentNamed(t, data, "generator"),
				"sbomb:component:distributionRole"); len(got) != 1 || got[0] != domain.RoleBuildTimeOnly {
				t.Errorf("sbomb:component:distributionRole = %v, want [%s]", got, domain.RoleBuildTimeOnly)
			}
		})
	}
}

// A component whose role was never derived carries no scope at all. The
// schema's own default for an absent scope is "required", so silence and the
// common answer agree, and nothing is asserted that was not derived.
func TestAComponentWithNoDerivedRoleCarriesNoScope(t *testing.T) {
	document := attributeDocument()
	document.Components = append(document.Components, domain.Component{
		ID: "component:bare", Name: "bare", Type: "library",
	})
	document.Relations[0].To = append(document.Relations[0].To, "component:bare")
	data, err := MarshalDocument(document, sbomwriter.Options{SpecVersion: Version16, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := componentNamed(t, data, "bare").Scope; got != "" {
		t.Errorf("scope = %q, want none", got)
	}
}

func propertyValues(component Component, name string) []string {
	out := make([]string, 0, 1)
	for _, property := range component.Properties {
		if property.Name == name {
			out = append(out, property.Value)
		}
	}
	return out
}
