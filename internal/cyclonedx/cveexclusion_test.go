package cyclonedx

import (
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

// The manifest may state a cve with no reason, and the property then has to
// stop at the cve: a value ending on ": " states a reason that was never given.
func TestCVEExclusionPropertyOmitsAnAbsentReason(t *testing.T) {
	component := domain.Component{
		Name:    "lwip",
		Version: "2.1.3",
		CVEExclusions: []domain.CVEExclusion{
			{CVE: "CVE-2020-22283", Reason: "the affected code path is not built"},
			{CVE: "CVE-2020-22284"},
		},
	}

	out := componentToCyclone(component, "ref-1", "1.6", sbomwriter.Options{})

	got := map[string]bool{}
	for _, p := range out.Properties {
		if p.Name == "sbomb:component:cveExclusion" {
			got[p.Value] = true
		}
	}
	for _, want := range []string{
		"CVE-2020-22283: the affected code path is not built",
		"CVE-2020-22284",
	} {
		if !got[want] {
			t.Errorf("missing property value %q; got %v", want, got)
		}
	}
	if got["CVE-2020-22284: "] {
		t.Error("a cve with no reason was published with a dangling separator")
	}
}
