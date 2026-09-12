package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// Section 24.5, against the corpus. The unit tests in internal/generate state
// the derivation; what only the corpus can state is that a real build's
// generator input comes out build-time-only, and that the archive ratio of a
// real static library is the one the linker actually produced.

// attributed is one component's three attributes as the document publishes
// them.
type attributed struct {
	scope       string
	role        string
	forms       []string
	members     string
	headerOnly  string
	modified    string
	hasPedigree bool
}

// componentAttributes reads the attributes of every non-file component.
func componentAttributes(t *testing.T, path string) map[string]attributed {
	t.Helper()
	var document struct {
		Components []struct {
			BomRef     string          `json:"bom-ref"`
			Type       string          `json:"type"`
			Scope      string          `json:"scope"`
			Pedigree   json.RawMessage `json:"pedigree"`
			Properties []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"properties"`
		} `json:"components"`
	}
	if err := json.Unmarshal(mustRead(t, path), &document); err != nil {
		t.Fatal(err)
	}
	out := map[string]attributed{}
	for _, component := range document.Components {
		if component.Type == "file" {
			continue
		}
		entry := attributed{scope: component.Scope, hasPedigree: len(component.Pedigree) > 0}
		for _, property := range component.Properties {
			switch property.Name {
			case "sbomb:component:distributionRole":
				entry.role = property.Value
			case "sbomb:component:linkageForm":
				entry.forms = append(entry.forms, property.Value)
			case "sbomb:component:archiveMembersUsed":
				entry.members = property.Value
			case "sbomb:component:headerOnly":
				entry.headerOnly = property.Value
			case "sbomb:component:modified":
				entry.modified = property.Value
			}
		}
		sort.Strings(entry.forms)
		out[component.BomRef] = entry
	}
	return out
}

// fileAttributes reads the two file-level properties of every file component.
func fileAttributes(t *testing.T, path string) map[string][2]string {
	t.Helper()
	var document struct {
		Components []struct {
			BomRef     string `json:"bom-ref"`
			Type       string `json:"type"`
			Properties []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"properties"`
		} `json:"components"`
	}
	if err := json.Unmarshal(mustRead(t, path), &document); err != nil {
		t.Fatal(err)
	}
	out := map[string][2]string{}
	for _, component := range document.Components {
		if component.Type != "file" {
			continue
		}
		var entry [2]string
		for _, property := range component.Properties {
			switch property.Name {
			case "sbomb:file:distributionRole":
				entry[0] = property.Value
			case "sbomb:file:linkageForm":
				entry[1] = property.Value
			}
		}
		out[component.BomRef] = entry
	}
	return out
}

// Every component of p14-foss, on both toolchains. mit-lib is the case the
// milestone names: three sources compiled, one member extracted, so the ratio
// is 1/3 and the form is static-archive-member. bsd-hdr contributed nothing
// but a header. gpl-gen is the case the whole export turns on: a GPL-2.0 code
// generator that the build compiled and ran, whose output is in the
// deliverable and whose own code is not, so it is build-time-only, its form is
// build-tool and its scope is excluded -- and it is the only such component of
// the fixture.
//
// It is asserted on gcc-ninja alone. Section 16 names three sources of
// generator-input evidence and the Ninja build graph is the one that exists;
// the Makefiles generator records the same dependency in build.make and
// section 16 does not list it, so there the generator is absent from the
// document and MISSING_GENERATOR_INPUT_EVIDENCE says so
// (TestAGeneratedFileWithoutGeneratorEvidenceIsReported, open question Q14).
func TestFOSSFixtureComponentAttributes(t *testing.T) {
	want := map[string]attributed{
		"component:project": {scope: "required", role: "distributed", modified: "unknown",
			forms: []string{"static-archive-member", "static-object"}, members: "1/1"},
		"component:apache-lib": {scope: "required", role: "distributed", modified: "unknown",
			forms: []string{"static-archive-member"}, members: "2/2"},
		"component:bsd-hdr": {scope: "required", role: "distributed", modified: "unknown",
			forms: []string{"header-only"}, headerOnly: "true"},
		"component:lgpl-lib": {scope: "required", role: "distributed", modified: "unknown",
			forms: []string{"static-archive-member"}, members: "2/2"},
		"component:mit-lib": {scope: "required", role: "distributed", modified: "unknown",
			forms: []string{"static-archive-member"}, members: "1/3"},
		"component:multi-license": {scope: "required", role: "distributed", modified: "unknown",
			forms: []string{"static-archive-member"}, members: "1/1"},
		"component:nocopyright": {scope: "required", role: "distributed", modified: "unknown",
			forms: []string{"static-archive-member"}, members: "1/1"},
	}
	generator := map[string]attributed{
		"component:gpl-gen": {scope: "excluded", role: "build-time-only", modified: "unknown",
			forms: []string{"build-tool"}},
	}
	// Both specification versions, because scope and pedigree exist at both
	// and the document is validated in process against the embedded schema of
	// whichever it declares (section 32.5) -- so a run that exits 0 is a run
	// whose document validated.
	for _, toolchain := range []string{"gcc-ninja", "gcc-make"} {
		for _, specVersion := range []string{"1.6", "1.7"} {
			t.Run(toolchain+"/"+specVersion, func(t *testing.T) {
				expected := map[string]attributed{}
				for ref, entry := range want {
					expected[ref] = entry
				}
				buildTimeOnly := 0
				if toolchain == "gcc-ninja" {
					for ref, entry := range generator {
						expected[ref] = entry
						buildTimeOnly++
					}
				}
				assertFOSSAttributes(t, toolchain, specVersion, expected, buildTimeOnly)
			})
		}
	}
}

func assertFOSSAttributes(t *testing.T, toolchain, specVersion string, want map[string]attributed, buildTimeOnly int) {
	t.Helper()
	output := filepath.Join(t.TempDir(), "out.cdx.json")
	code, _, stderr := execute([]string{"generate",
		"--build-dir", testutil.CorpusBuildDir(t, toolchain, "p14-foss"),
		"--source-dir", testutil.CorpusSourceTree(t), "--policy", "lenient",
		"--spec-version", specVersion, "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}

	got := componentAttributes(t, output)
	if len(got) != len(want) {
		t.Fatalf("components = %v, want %v", got, want)
	}
	for ref, expected := range want {
		actual := got[ref]
		if actual.scope != expected.scope || actual.role != expected.role ||
			actual.members != expected.members || actual.headerOnly != expected.headerOnly ||
			actual.modified != expected.modified ||
			strings.Join(actual.forms, ",") != strings.Join(expected.forms, ",") {
			t.Errorf("%s = %+v, want %+v", ref, actual, expected)
		}
		// The corpus carries no .git (open question Q9), so every status is
		// unknown -- and an unknown status must produce no pedigree node,
		// whatever else the component carries.
		if actual.hasPedigree {
			t.Errorf("%s carries a pedigree although its status is %q", ref, actual.modified)
		}
	}

	// "and it is the only such component in the fixture": one on a build
	// graph that names the generating rule's inputs, none on one that does
	// not. A second build-time-only component would mean a distributed
	// dependency had been dropped out of the attribution document.
	counted := 0
	for _, entry := range got {
		if entry.role == "build-time-only" {
			counted++
		}
	}
	if counted != buildTimeOnly {
		t.Errorf("build-time-only components = %d, want %d", counted, buildTimeOnly)
	}
}

// The other half of section 16 on the corpus: where the build system records
// no inputs for the generating rule, the generated file is still in the
// document and the finding says what is missing behind it. The Makefiles
// generator writes the dependency into build.make and section 16 lists no
// source that reads it (open question Q14), so gcc-make is where the absence
// is observable -- and the absence must be audible, because a generated source
// with no known input means a generator, and its licence, that this document
// does not describe.
func TestAGeneratedFileWithoutGeneratorEvidenceIsReported(t *testing.T) {
	directory := t.TempDir()
	findingsPath := filepath.Join(directory, "findings.json")
	code, _, stderr := execute([]string{"generate",
		"--build-dir", testutil.CorpusBuildDir(t, "gcc-make", "p14-foss"),
		"--source-dir", testutil.CorpusSourceTree(t), "--policy", "lenient",
		"--output", filepath.Join(directory, "out.cdx.json"),
		"--findings-json", findingsPath, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	var report struct {
		Findings []struct {
			ID      string `json:"id"`
			Subject struct {
				Ref string `json:"ref"`
			} `json:"subject"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(mustRead(t, findingsPath), &report); err != nil {
		t.Fatal(err)
	}
	var named []string
	for _, finding := range report.Findings {
		if finding.ID == "MISSING_GENERATOR_INPUT_EVIDENCE" {
			named = append(named, finding.Subject.Ref)
		}
	}
	if len(named) != 1 || named[0] != "build:generated/table.c" {
		t.Errorf("MISSING_GENERATOR_INPUT_EVIDENCE = %v, want it once for the generated source", named)
	}

	// And the Ninja build of the same project, where the inputs are known,
	// reports nothing of the kind.
	ninjaFindings := filepath.Join(directory, "ninja-findings.json")
	code, _, stderr = execute([]string{"generate",
		"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
		"--source-dir", testutil.CorpusSourceTree(t), "--policy", "lenient",
		"--output", filepath.Join(directory, "ninja.cdx.json"),
		"--findings-json", ninjaFindings, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	if err := json.Unmarshal(mustRead(t, ninjaFindings), &report); err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		if finding.ID == "MISSING_GENERATOR_INPUT_EVIDENCE" {
			t.Errorf("the Ninja build reports %s for %s although the build graph names its input",
				finding.ID, finding.Subject.Ref)
		}
	}
}

// The role derivation on a real build, from the fixture whose generator-input
// edges come from a packaging manifest rather than from the build graph
// (section 18 rather than section 16). The manifest names config.yaml as the
// input of a generated binary that is packaged into the image: the yaml is not
// in the image, the binary is, and index.html beside it was packaged as it
// stands.
func TestAGeneratorInputIsBuildTimeOnlyInTheCorpus(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "out.cdx.json")
	configPath := filepath.Join("..", "..", "testdata", "config", "p12-assets.json")
	code, _, stderr := execute([]string{"generate",
		"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p12-assets"),
		"--config", configPath, "--policy", "lenient", "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}

	want := map[string][2]string{
		"file:project:assets/config.yaml": {"build-time-only", "build-tool"},
		"file:project:assets/index.html":  {"distributed", "embedded-asset"},
		"file:build:generated/config.bin": {"distributed", "embedded-asset"},
		"file:project:main.c":             {"distributed", "static-object"},
	}
	got := fileAttributes(t, output)
	for ref, expected := range want {
		if got[ref] != expected {
			t.Errorf("%s = %v, want %v", ref, got[ref], expected)
		}
	}

	// The component the yaml belongs to is the project itself, which also
	// holds main.c: one distributed file makes the component distributed, so
	// the component is required even though one of its files is not shipped.
	components := componentAttributes(t, output)
	if entry := components["component:p12-assets"]; entry.scope != "required" || entry.role != "distributed" {
		t.Errorf("component:p12-assets = %+v, want a distributed component", entry)
	} else if strings.Join(entry.forms, ",") != "build-tool,embedded-asset,static-object" {
		t.Errorf("forms = %v, want all three the files establish", entry.forms)
	}
}
