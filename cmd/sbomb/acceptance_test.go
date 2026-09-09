package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

func TestMilestone13Acceptance(t *testing.T) {
	// The whole build directory, so the run sees the CMake File API reply and
	// can register the toolchain anchor, not just the compile database.
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	configPath := filepath.Join("..", "..", "testdata", "config", "p02-full.json")
	strictOutput := filepath.Join(t.TempDir(), "strict.cdx.json")
	lenientOutput := filepath.Join(t.TempDir(), "lenient.cdx.json")
	strictReport := filepath.Join(t.TempDir(), "strict.txt")
	// The corpus commits build evidence, not sources, so the files the chains
	// reach cannot be hashed. Strict gates on that; lenient does not.
	strictCode, _, strictErr := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--policy", "strict", "--output", strictOutput, "--review-report", strictReport, "--reproducible"})
	if strictCode != 3 || strictErr != "" {
		t.Fatalf("strict result = code %d, stderr %q; want code 3 on unhashable files", strictCode, strictErr)
	}
	lenientCode, _, lenientErr := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--policy", "lenient", "--output", lenientOutput, "--reproducible"})
	if lenientCode != 0 || lenientErr != "" {
		t.Fatalf("lenient result = code %d, stderr %q; want code 0", lenientCode, lenientErr)
	}
	// default and cra differ only in their gates, not in scope, so section
	// 33.3 requires them to produce the same document. strict is deliberately
	// not compared here: it changes includeLinkerScripts and
	// sectionGarbageCollection, which are the scope settings 33.3 exempts.
	craOutput := filepath.Join(t.TempDir(), "cra.cdx.json")
	defaultOutput := filepath.Join(t.TempDir(), "default.cdx.json")
	execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--policy", "cra", "--output", craOutput, "--reproducible"})
	execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--output", defaultOutput, "--reproducible"})
	craSBOM, err := os.ReadFile(craOutput)
	if err != nil {
		t.Fatal(err)
	}
	defaultSBOM, err := os.ReadFile(defaultOutput)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(craSBOM, defaultSBOM) {
		t.Fatal("two profiles with identical scope produced different documents; policy may only change the verdict")
	}
	strictSBOM, err := os.ReadFile(strictOutput)
	if err != nil {
		t.Fatal(err)
	}
	lenientSBOM, err := os.ReadFile(lenientOutput)
	if err != nil {
		t.Fatal(err)
	}
	if len(strictSBOM) == 0 || len(lenientSBOM) == 0 {
		t.Fatal("a profile produced no document at all")
	}
	actualReport, err := os.ReadFile(strictReport)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "gcc-13-p02-report.txt", actualReport)
	_, _, explainErr := execute([]string{"explain", "--build-dir", buildDir, "--file", "project:crypto.c"})
	if explainErr != "" {
		t.Fatalf("explain returned stderr: %s", explainErr)
	}
	_, explainOutput, _ := execute([]string{"explain", "--build-dir", buildDir, "--file", "project:crypto.c"})
	assertGolden(t, "explain-crypto.txt", []byte(explainOutput))

	// A second, independent copy of the same evidence must produce the same
	// bytes; this is the reproducibility half of the acceptance criterion.
	secondBuild := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	secondOutput := filepath.Join(t.TempDir(), "second.cdx.json")
	secondReport := filepath.Join(t.TempDir(), "second.txt")
	secondCode, _, secondErr := execute([]string{"generate", "--build-dir", secondBuild, "--config", configPath, "--policy", "strict", "--output", secondOutput, "--review-report", secondReport, "--reproducible"})
	if secondCode != 3 || secondErr != "" {
		t.Fatalf("second strict result = code %d, stderr %q; want code 3", secondCode, secondErr)
	}
	secondSBOM, err := os.ReadFile(secondOutput)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(strictSBOM, secondSBOM) {
		t.Fatal("reproducible SBOM changed between runs")
	}
	secondReportBytes, err := os.ReadFile(secondReport)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actualReport, secondReportBytes) {
		t.Fatal("reproducible report changed between runs")
	}

	brokenBuild := t.TempDir()
	if err := os.WriteFile(filepath.Join(brokenBuild, "compile_commands.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	discoveryCode, _, discoveryErr := execute([]string{"generate", "--build-dir", brokenBuild, "--config", configPath, "--policy", "strict", "--output", filepath.Join(t.TempDir(), "broken.cdx.json"), "--reproducible"})
	if discoveryCode != 2 || discoveryErr == "" {
		t.Fatalf("discovery precedence = code %d, stderr %q; want code 2 with an error", discoveryCode, discoveryErr)
	}
}

// TestLinkEvidenceGateSeparatesProfiles exercises the one policy gate that is
// wired end to end today: a build directory with compile evidence but no link
// evidence fails under default and passes under lenient. The remaining gates of
// section 33.1 cannot be exercised until their findings are produced.
func TestLinkEvidenceGateSeparatesProfiles(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	for _, name := range []string{"app.map", "app.d", "libcrypto.a"} {
		if err := os.Remove(filepath.Join(buildDir, name)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join("..", "..", "testdata", "config", "p02-full.json")

	defaultCode, _, _ := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--output", filepath.Join(t.TempDir(), "d.cdx.json"), "--reproducible"})
	if defaultCode != 3 {
		t.Errorf("default profile without link evidence = %d, want 3", defaultCode)
	}
	lenientCode, _, _ := execute([]string{"generate", "--build-dir", buildDir, "--config", configPath, "--policy", "lenient", "--output", filepath.Join(t.TempDir(), "l.cdx.json"), "--reproducible"})
	if lenientCode != 0 {
		t.Errorf("lenient profile without link evidence = %d, want 0", lenientCode)
	}
}

// TestToolchainAndSystemFilesAreNotComponents is the phase 1 acceptance
// criterion at the CLI level: real evidence names many toolchain and system
// files, and none of them may appear as a project dependency.
func TestToolchainAndSystemFilesAreNotComponents(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	output := filepath.Join(t.TempDir(), "scoped.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient", "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []struct {
			BomRef     string `json:"bom-ref"`
			Properties []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"properties"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Components) == 0 {
		t.Fatal("the SBOM has no components at all")
	}
	for _, component := range document.Components {
		for _, property := range component.Properties {
			if property.Name != "sbomb:component:scope" {
				continue
			}
			switch property.Value {
			case "toolchain", "system":
				t.Errorf("%s is %s scope and must not be a component", component.BomRef, property.Value)
			case "":
				t.Errorf("%s carries no scope", component.BomRef)
			}
		}
		// Every identity must be anchored, never an absolute host path.
		if strings.HasPrefix(component.BomRef, "file:abs:") {
			t.Errorf("%s was not anchored", component.BomRef)
		}
	}
}

func TestMilestone17MakefilesAcceptance(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-make", "p02-static")
	output := filepath.Join(t.TempDir(), "mk.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient", "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("Make acceptance result = code %d, stderr %q", code, stderr)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "gcc-make-p02.cdx.json", actual)
}

func TestMilestone18AMSVCAcceptance(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "msvc-ninja", "p02-static")
	output := filepath.Join(t.TempDir(), "ms.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--path-flavor", "windows", "--policy", "lenient", "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("MSVC acceptance result = code %d, stderr %q", code, stderr)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "msvc-ninja-p02.cdx.json", actual)
}

func TestMilestone18ANMakeAcceptance(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "msvc-nmake", "p02-static")
	output := filepath.Join(t.TempDir(), "nmake.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--path-flavor", "windows", "--policy", "lenient", "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("MSVC NMake acceptance result = code %d, stderr %q", code, stderr)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "msvc-nmake-p02.cdx.json", actual)
}

func TestMilestone18BMSBuildAcceptance(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "msvc-vs17", "p02-static")
	output := filepath.Join(t.TempDir(), "vs.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--config-name", "Debug", "--path-flavor", "windows", "--policy", "lenient", "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("MSBuild acceptance result = code %d, stderr %q", code, stderr)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "msvc-vs17-p02.cdx.json", actual)
}

// TestEvidenceChainYieldsTheSameFilesAcrossToolchains is the phase 2
// acceptance criterion. The same project built with five different toolchain
// and generator combinations must yield the same used-file set, because the
// set is derived from what the linker demonstrably consumed rather than from
// what each build system happens to record.
//
// It also pins the property the whole tool exists for: p02-static compiles
// unused.c into libcrypto.a, the linker never extracts that member, and so it
// must not appear -- while crypto.h, which no link evidence names directly,
// must appear because a used translation unit included it.
func TestEvidenceChainYieldsTheSameFilesAcrossToolchains(t *testing.T) {
	want := []string{
		"file:project:crypto.c",
		"file:project:crypto.h",
		"file:project:main.c",
	}
	for _, toolchain := range []string{"gcc-ninja", "gcc-make", "clang-ninja", "arm-none-eabi", "mingw-w64"} {
		t.Run(toolchain, func(t *testing.T) {
			buildDir := testutil.CorpusBuildDir(t, toolchain, "p02-static")
			output := filepath.Join(t.TempDir(), "out.cdx.json")
			code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient", "--output", output, "--reproducible"})
			if code != 0 || stderr != "" {
				t.Fatalf("generate = code %d, stderr %q", code, stderr)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Components []struct {
					BomRef string `json:"bom-ref"`
				} `json:"components"`
			}
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			// Only the file components: the grouping component that holds
			// them is asserted separately.
			got := make([]string, 0, len(document.Components))
			var grouping int
			for _, component := range document.Components {
				if strings.HasPrefix(component.BomRef, "file:") {
					got = append(got, component.BomRef)
					continue
				}
				grouping++
			}
			sort.Strings(got)
			if grouping != 1 {
				t.Errorf("got %d grouping components, want exactly one for the project", grouping)
			}
			if len(got) != len(want) {
				t.Fatalf("got %d components %v, want %v", len(got), got, want)
			}
			for index := range want {
				if got[index] != want[index] {
					t.Fatalf("component set = %v, want %v", got, want)
				}
			}
		})
	}
}

// TestUnextractedArchiveMembersAreAbsent states the same property directly, so
// that a regression names its cause.
func TestUnextractedArchiveMembersAreAbsent(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	output := filepath.Join(t.TempDir(), "out.cdx.json")
	if code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient", "--output", output, "--reproducible"}); code != 0 {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "unused.c") {
		t.Error("unused.c is in the SBOM although the linker never extracted its archive member")
	}
	if !strings.Contains(string(data), "crypto.h") {
		t.Error("crypto.h is missing although a used translation unit included it")
	}
}

// TestValidateSubcommandRunsBothLayers covers section 32.5: a generated
// document validates, and either layer failing is exit code 4.
func TestValidateSubcommandRunsBothLayers(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	output := filepath.Join(t.TempDir(), "out.cdx.json")
	if code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient", "--output", output, "--reproducible"}); code != 0 {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	if code, _, stderr := execute([]string{"validate", "--input", output}); code != 0 {
		t.Fatalf("validate = code %d, stderr %q; the writer must produce documents its validator accepts", code, stderr)
	}

	// Schema layer: an invalid component type.
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["components"].([]any)[0].(map[string]any)["type"] = "not-a-type"
	broken := filepath.Join(t.TempDir(), "broken.cdx.json")
	brokenData, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, brokenData, 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execute([]string{"validate", "--input", broken})
	if code != 4 {
		t.Errorf("validate on a schema-invalid document = code %d, want 4", code)
	}
	if !strings.Contains(stderr, "components/0/type") {
		t.Errorf("the error does not name the offending location: %q", stderr)
	}
}

// TestEvidenceSubcommandDumpsTheGraph covers section 32.1: discovery can be
// inspected on its own, without producing an SBOM.
func TestEvidenceSubcommandDumpsTheGraph(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	output := filepath.Join(t.TempDir(), "evidence.json")
	code, _, stderr := execute([]string{"evidence", "--build-dir", buildDir, "--output", output})
	if code != 0 || stderr != "" {
		t.Fatalf("evidence = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var dump struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatal(err)
	}
	if len(dump.Nodes) == 0 || len(dump.Edges) == 0 {
		t.Fatalf("the evidence dump is empty: %d nodes, %d edges", len(dump.Nodes), len(dump.Edges))
	}
}

// TestMissingDeliverableIsAUsageError covers the exit-code precedence of
// section 32.4: the tool refuses to guess what the SBOM is about.
func TestMissingDeliverableIsAUsageError(t *testing.T) {
	buildDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(buildDir, "compile_commands.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--output", filepath.Join(t.TempDir(), "o.json")})
	if code != 1 {
		t.Errorf("generate without a deliverable = code %d, want 1", code)
	}
	if !strings.Contains(stderr, "MISSING_FINAL_DELIVERABLE") {
		t.Errorf("stderr = %q, want MISSING_FINAL_DELIVERABLE", stderr)
	}
}
