package generate

import (
	"testing"

	"github.com/example/sbomb/internal/domain"
)

func attachment(object string, depfile, dwarf []string, covered bool) headerAttachment {
	return headerAttachment{
		object: object, source: "project:a.c",
		depfileHeaders: depfile, dwarfHeaders: dwarf, dwarfCovered: covered,
	}
}

func headerSet(resolution headerResolution) map[string]resolvedHeader {
	out := map[string]resolvedHeader{}
	for _, edge := range resolution.edges {
		out[edge.header] = edge
	}
	return out
}

func TestDwarfPreferredNarrowsAndCountsWhatItDropped(t *testing.T) {
	// Section 4.4: under dwarf-preferred a depfile-only header of a covered
	// unit is not included, but the narrowing must be auditable.
	resolution := resolveHeaderEvidence([]headerAttachment{
		attachment("build:a.o", []string{"project:used.h", "toolchain:x:stdc-predef.h"}, []string{"project:used.h"}, true),
	}, "dwarf-preferred")

	headers := headerSet(resolution)
	if _, kept := headers["toolchain:x:stdc-predef.h"]; kept {
		t.Error("a depfile-only header of a DWARF-covered unit was kept")
	}
	if len(resolution.narrowed) != 1 || resolution.narrowed[0].header != "toolchain:x:stdc-predef.h" {
		t.Errorf("narrowed = %#v, want the dropped header recorded", resolution.narrowed)
	}
	if got := headers["project:used.h"]; got.confidence != domain.ConfidenceHigh || got.source != "debug-info+depfile" {
		t.Errorf("agreed header = %+v, want high confidence from both sources", got)
	}
}

func TestUnionKeepsBothSetsAndNarrowsNothing(t *testing.T) {
	resolution := resolveHeaderEvidence([]headerAttachment{
		attachment("build:a.o", []string{"project:only-dep.h"}, []string{"project:only-dwarf.h"}, true),
	}, "union")

	headers := headerSet(resolution)
	if len(headers) != 2 {
		t.Fatalf("headers = %v, want both sets", headers)
	}
	if len(resolution.narrowed) != 0 {
		t.Errorf("union narrowed %d header(s); it must narrow nothing", len(resolution.narrowed))
	}
	if headers["project:only-dwarf.h"].confidence != domain.ConfidenceHigh {
		t.Error("a DWARF-only header must carry high confidence (section 11.4)")
	}
	if headers["project:only-dep.h"].confidence != domain.ConfidenceMedium {
		t.Error("a depfile-only header keeps its depfile confidence")
	}
}

func TestDepfilesModeIgnoresDebugInformation(t *testing.T) {
	resolution := resolveHeaderEvidence([]headerAttachment{
		attachment("build:a.o", []string{"project:only-dep.h"}, []string{"project:only-dwarf.h"}, true),
	}, "depfiles")

	headers := headerSet(resolution)
	if _, present := headers["project:only-dwarf.h"]; present {
		t.Error("depfiles mode used the DWARF set")
	}
	if _, present := headers["project:only-dep.h"]; !present {
		t.Error("depfiles mode dropped the depfile set")
	}
}

func TestUncoveredUnitFallsBackAndSaysSo(t *testing.T) {
	// A unit without usable debug information keeps its depfile headers, and
	// the substitution is reported rather than silent (section 4.4).
	resolution := resolveHeaderEvidence([]headerAttachment{
		attachment("build:a.o", []string{"project:used.h"}, nil, false),
	}, "dwarf-preferred")

	if _, present := headerSet(resolution)["project:used.h"]; !present {
		t.Fatal("an uncovered unit lost its depfile headers")
	}
	var reported bool
	for _, finding := range resolution.findings {
		if finding.ID == "HEADER_EVIDENCE_FALLBACK" && finding.Subject.Ref == "build:a.o" {
			reported = true
		}
	}
	if !reported {
		t.Error("HEADER_EVIDENCE_FALLBACK was not emitted for the uncovered unit")
	}
}

func TestPrecompiledHeadersAreEvidenceAndNarrowingCannotDropThem(t *testing.T) {
	// Section 14.5: every unit of the target depends on the whole PCH header
	// set, so DWARF narrowing must not remove it.
	unit := attachment("build:a.o", nil, []string{"project:used.h"}, true)
	unit.pchHeaders = []string{"project:forced.h"}
	resolution := resolveHeaderEvidence([]headerAttachment{unit}, "dwarf-preferred")

	forced, present := headerSet(resolution)["project:forced.h"]
	if !present {
		t.Fatal("a PCH header was narrowed away")
	}
	if !forced.viaPCH || forced.confidence != domain.ConfidenceMedium || forced.source != "pch" {
		t.Errorf("PCH header = %+v, want viaPCH with medium confidence from the pch source", forced)
	}
	if !resolution.pchOnly["project:forced.h"] {
		t.Error("a forced header that no compilation unit shows using is reached only via the PCH")
	}
	if resolution.pchOnly["project:used.h"] {
		t.Error("a header the debug information names is not reached only via the PCH")
	}
}

func TestUnityIncludesAreRecognizedOnlyInAggregationGlue(t *testing.T) {
	if !looksLikeUnityPath("build:CMakeFiles/app.dir/Unity/unity_0_c.c") {
		t.Error("a generated unity source was not recognized")
	}
	if looksLikeUnityPath("project:src/unity_helper.c") {
		t.Error("an ordinary source outside a Unity directory was taken for one")
	}
}
