package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// fileComponents runs generate on a corpus fixture and returns the file
// components of the project itself, which is what the header rules decide.
func fileComponents(t *testing.T, toolchain, project string, extra ...string) []string {
	t.Helper()
	buildDir := testutil.CorpusBuildDir(t, toolchain, project)
	output := filepath.Join(t.TempDir(), "out.cdx.json")
	args := append([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--output", output, "--reproducible"}, extra...)
	code, _, stderr := execute(args)
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
	got := make([]string, 0, len(document.Components))
	for _, component := range document.Components {
		if strings.HasPrefix(component.BomRef, "file:project:") {
			got = append(got, component.BomRef)
		}
	}
	sort.Strings(got)
	return got
}

func assertSet(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("component set = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("component set = %v, want %v", got, want)
		}
	}
}

// A unity build compiles one generated file instead of three real sources.
// Section 17.1 requires the constituents to be recovered; without that the SBOM
// would name a build artifact and none of the code it was made from.
func TestUnityBuildRecoversItsConstituentSources(t *testing.T) {
	got := fileComponents(t, "gcc-ninja", "p06-unity")
	assertSet(t, got, []string{
		"file:project:main.c",
		"file:project:mod_a.c",
		"file:project:mod_a.h",
		"file:project:mod_b.c",
		"file:project:mod_b.h",
	})
}

// Section 14.5. pch_only.h is used by a translation unit, so debug information
// shows it contributing; pch_unused.h is forced in by the precompiled header
// and used by nothing. Only the second is reached *only* via the PCH.
func TestPrecompiledHeadersAreIncludedByDefaultAndExcludableOnDemand(t *testing.T) {
	included := fileComponents(t, "gcc-ninja", "p07-pch")
	assertSet(t, included, []string{
		"file:project:helper.c",
		"file:project:helper.h",
		"file:project:main.c",
		"file:project:pch_only.h",
		"file:project:pch_unused.h",
	})

	excluded := fileComponents(t, "gcc-ninja", "p07-pch", "--pch-headers=exclude")
	assertSet(t, excluded, []string{
		"file:project:helper.c",
		"file:project:helper.h",
		"file:project:main.c",
		"file:project:pch_only.h",
	})
}

// Section 4.4: the three modes are the whole point of the option, so they have
// to differ observably on the same evidence. Every translation unit of p07-pch
// carries usable debug information, so under dwarf-preferred the compiler's own
// stdc-predef.h -- which no unit names -- is narrowed away; under union and
// depfiles the dependency file keeps it.
func TestHeaderEvidenceModesDifferOnTheSameBuild(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p07-pch")
	counts := map[string]int{}
	for _, mode := range []string{"dwarf-preferred", "union", "depfiles"} {
		output := filepath.Join(t.TempDir(), "out.cdx.json")
		code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
			"--policy", "lenient", "--include-system-headers", "--header-evidence=" + mode,
			"--output", output, "--reproducible"})
		if code != 0 || stderr != "" {
			t.Fatalf("%s: generate = code %d, stderr %q", mode, code, stderr)
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
		for _, component := range document.Components {
			if strings.Contains(component.BomRef, "stdc-predef.h") {
				counts[mode]++
			}
		}
	}
	if counts["dwarf-preferred"] != 0 {
		t.Errorf("dwarf-preferred kept a header no compilation unit names")
	}
	if counts["union"] == 0 || counts["depfiles"] == 0 {
		t.Errorf("union/depfiles dropped a header the dependency file names: %v", counts)
	}
}

// The project's own header must survive whatever the toolchain writes into its
// line table: clang names no declaration-only header at all, and narrowing
// against an empty header set would delete evidence rather than refine it.
func TestNarrowingNeverRemovesTheOnlyEvidenceForAHeader(t *testing.T) {
	for _, toolchain := range []string{"gcc-ninja", "clang-ninja", "mingw-w64", "arm-none-eabi", "gcc-make"} {
		t.Run(toolchain, func(t *testing.T) {
			got := fileComponents(t, toolchain, "p02-static")
			var found bool
			for _, ref := range got {
				if ref == "file:project:crypto.h" {
					found = true
				}
			}
			if !found {
				t.Errorf("crypto.h is missing from %v", got)
			}
		})
	}
}

// Section 4.4 requires the narrowing to be auditable: a reviewer must be able
// to see how many headers the DWARF set removed, per component, rather than
// discover a shorter file list with no explanation.
func TestReviewReportCountsWhatNarrowingRemoved(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p07-pch")
	reportPath := filepath.Join(t.TempDir(), "review.txt")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
		"--policy", "lenient", "--header-evidence=dwarf-preferred", "--include-system-headers",
		"--output", filepath.Join(t.TempDir(), "out.cdx.json"),
		"--review-report", reportPath, "--report-chains=all", "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	text, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "== Header narrowing ==") {
		t.Fatal("the review report has no header narrowing section")
	}
	if strings.Contains(string(text), "headers excluded by DWARF narrowing: 0") {
		t.Errorf("narrowing removed headers but the report claims none:\n%s", text)
	}
	if !strings.Contains(string(text), "stdc-predef.h") {
		t.Error("--report-chains all did not name the headers that were dropped")
	}
}

// Section 14.4: the class decides, and it is recorded so a reviewer can see
// why a header is in the document. The compiler's own stdc-predef.h is a
// compiler-runtime header and stays out unless asked for.
func TestHeaderClassDecidesInclusionAndIsRecorded(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	read := func(extra ...string) map[string]string {
		output := filepath.Join(t.TempDir(), "out.cdx.json")
		args := append([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
			"--header-evidence=union", "--output", output, "--reproducible"}, extra...)
		if code, _, stderr := execute(args); code != 0 || stderr != "" {
			t.Fatalf("generate = code %d, stderr %q", code, stderr)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Components []struct {
				BomRef     string                         `json:"bom-ref"`
				Properties []struct{ Name, Value string } `json:"properties"`
			} `json:"components"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		classes := map[string]string{}
		for _, component := range document.Components {
			for _, property := range component.Properties {
				if property.Name == "sbomb:evidence:header:class" {
					classes[component.BomRef] = property.Value
				}
			}
		}
		return classes
	}

	byDefault := read()
	if byDefault["file:project:crypto.h"] != "project-header" {
		t.Errorf("crypto.h class = %q, want project-header", byDefault["file:project:crypto.h"])
	}
	for ref := range byDefault {
		if strings.Contains(ref, "stdc-predef.h") {
			t.Errorf("a compiler runtime header is in the default document: %s", ref)
		}
	}

	withSystem := read("--include-system-headers")
	var found bool
	for ref, class := range withSystem {
		if strings.Contains(ref, "stdc-predef.h") {
			found = true
			if class != "compiler-runtime-header" {
				t.Errorf("stdc-predef.h class = %q, want compiler-runtime-header", class)
			}
		}
	}
	if !found {
		t.Error("--include-system-headers did not bring the compiler runtime header back")
	}
}

// Section 4.5 against real linker output. deadcode.c is compiled into the
// executable and referenced by nothing; with per-function sections the linker
// discards every section it contributed. Unit tests cannot show this: the
// behaviour depends on what GNU ld actually writes into its map.
func TestSectionGarbageCollectionAgainstRealLinkerOutput(t *testing.T) {
	files := func(mode string) []string {
		return fileComponents(t, "gcc-ninja", "p08-gcsections", "--section-garbage-collection="+mode)
	}
	assertSet(t, files("ignore"), []string{
		"file:project:deadcode.c", "file:project:live.c",
		"file:project:live.h", "file:project:main.c",
	})
	assertSet(t, files("annotate"), []string{
		"file:project:deadcode.c", "file:project:live.c",
		"file:project:live.h", "file:project:main.c",
	})
	// exclude removes the object, and with it the source that only the object
	// reached: the evidence chain to the deliverable is broken.
	assertSet(t, files("exclude"), []string{
		"file:project:live.c", "file:project:live.h", "file:project:main.c",
	})
}

// A debug build retains .debug_* and .comment for every object, and a
// zero-length .eh_frame besides. Deciding "fully discarded" over all sections
// would therefore never fire on the builds this tool exists for.
func TestDebugSectionsDoNotCountAsAContributionToTheImage(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p08-gcsections")
	findingsPath := filepath.Join(t.TempDir(), "findings.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--section-garbage-collection=exclude", "--output", filepath.Join(t.TempDir(), "out.json"),
		"--findings-json", findingsPath, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(findingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var findings []struct{ ID string }
	if err := json.Unmarshal(data, &findings); err != nil {
		var wrapper struct {
			Findings []struct{ ID string } `json:"findings"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			t.Fatal(err)
		}
		findings = wrapper.Findings
	}
	var excluded, unavailable bool
	for _, finding := range findings {
		switch finding.ID {
		case "SECTION_GC_EXCLUDED":
			excluded = true
		case "SECTION_GC_INFO_UNAVAILABLE":
			unavailable = true
		}
	}
	if !excluded {
		t.Error("no object was reported as fully discarded")
	}
	if unavailable {
		t.Error("the map enumerates both halves, so the evidence is not unavailable")
	}
}

// Section 17.3: LTO degrades symbol- and section-level attribution, so the
// object-to-source edges keep their strategy and lose one confidence level,
// with the reason recorded. File-level attribution must survive.
func TestLTODowngradesObjectToSourceAttribution(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p09-lto")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--output", filepath.Join(t.TempDir(), "out.json"), "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(buildDir, "evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dump struct {
		Edges []struct {
			To         string   `json:"to"`
			Type       string   `json:"type"`
			Confidence string   `json:"confidence"`
			Downgrades []string `json:"downgrades"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatal(err)
	}
	var checked int
	for _, edge := range dump.Edges {
		if edge.Type != "source-mapping" {
			continue
		}
		checked++
		if edge.Confidence != "medium" {
			t.Errorf("%s confidence = %q, want one level below high", edge.To, edge.Confidence)
		}
		if len(edge.Downgrades) != 1 || edge.Downgrades[0] != "lto" {
			t.Errorf("%s downgrades = %v, want [lto] recorded (section 8.7)", edge.To, edge.Downgrades)
		}
	}
	if checked == 0 {
		t.Fatal("LTO destroyed file-level attribution entirely; section 17.3 says it must not")
	}
}

// Section 19.2 strategy 2 and section 21: a dependency a package manager
// populated must yield its own component with the manager's own name, version
// and licence, without a line of curated configuration. Before this, every
// FetchContent dependency landed in the project component with no version at
// all.
func TestFetchContentDependencyBecomesItsOwnComponent(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p10-fetchcontent")
	output := filepath.Join(t.TempDir(), "out.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []struct {
			BomRef   string `json:"bom-ref"`
			Type     string `json:"type"`
			Version  string `json:"version"`
			PURL     string `json:"purl"`
			Supplier *struct {
				Name string `json:"name"`
			} `json:"supplier"`
			Licenses []struct {
				Expression string `json:"expression"`
			} `json:"licenses"`
		} `json:"components"`
		Dependencies []struct {
			Ref       string   `json:"ref"`
			DependsOn []string `json:"dependsOn"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, component := range document.Components {
		if component.BomRef != "component:tinylog" {
			continue
		}
		found = true
		if component.Version != "1.2.0" {
			t.Errorf("version = %q, want the tag FetchContent checked out", component.Version)
		}
		if !strings.HasPrefix(component.PURL, "pkg:generic/tinylog@1.2.0?vcs_url=") {
			t.Errorf("purl = %q, want a generic purl carrying the checkout (section 20.4)", component.PURL)
		}
		if len(component.Licenses) != 1 || component.Licenses[0].Expression != "MIT" {
			t.Errorf("licenses = %#v, want MIT from the populated dependency's own file", component.Licenses)
		}
		// Section 20.5: a repository host is not a supplier.
		if component.Supplier != nil && component.Supplier.Name != "" {
			t.Errorf("supplier = %q was invented from the repository", component.Supplier.Name)
		}
	}
	if !found {
		t.Fatalf("no component:tinylog among %d components", len(document.Components))
	}

	// Its files belong to it, not to the project.
	for _, dependency := range document.Dependencies {
		if dependency.Ref != "component:tinylog" {
			continue
		}
		if len(dependency.DependsOn) != 2 {
			t.Errorf("tinylog owns %v, want both of its files", dependency.DependsOn)
		}
		for _, ref := range dependency.DependsOn {
			if !strings.Contains(ref, "tinylog-src") {
				t.Errorf("tinylog owns an unrelated file: %s", ref)
			}
		}
	}
}

// Section 21 with Conan, against a package created into a local cache by
// regen.sh rather than fetched from a registry. Two properties matter: the
// component carries the manager's version and purl, and its files carry
// portable identities. Conan's cache path contains a random component, so
// without the anchor the bom-refs would differ on every machine.
func TestConanDependencyIsNamedVersionedAndPortable(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p11-conan")
	output := filepath.Join(t.TempDir(), "out.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []struct {
			BomRef  string `json:"bom-ref"`
			Version string `json:"version"`
			PURL    string `json:"purl"`
		} `json:"components"`
		Dependencies []struct {
			Ref       string   `json:"ref"`
			DependsOn []string `json:"dependsOn"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, component := range document.Components {
		if component.BomRef == "component:tinycbor" {
			found = true
			if component.Version != "0.6.1" {
				t.Errorf("version = %q, want the one Conan declared", component.Version)
			}
			if component.PURL != "pkg:conan/tinycbor@0.6.1" {
				t.Errorf("purl = %q", component.PURL)
			}
		}
		// Nothing may carry the package cache path: it contains a random
		// component and would make bom-refs machine-specific.
		if strings.Contains(component.BomRef, "__fixture_pkg__") {
			t.Errorf("a bom-ref carries the package cache path: %s", component.BomRef)
		}
	}
	if !found {
		t.Fatal("no component:tinycbor; the Conan dependency was not recognized")
	}

	for _, dependency := range document.Dependencies {
		if dependency.Ref != "component:tinycbor" {
			continue
		}
		if len(dependency.DependsOn) == 0 {
			t.Error("the Conan component owns no files")
		}
		for _, ref := range dependency.DependsOn {
			if !strings.HasPrefix(ref, "file:pkg:conan/tinycbor:") {
				t.Errorf("file %s is not anchored to the package", ref)
			}
		}
	}
}

// Section 18. A firmware image contains inputs the compiler and linker never
// see, and a manifest that names a file as an input is what makes it evidence.
// The fixture puts three files next to each other in assets/: two the manifest
// names and one it does not. Only the named ones may appear -- adjacency is
// exactly what section 18 refuses to accept as proof.
func TestImageManifestBringsInAssetsAndOnlyDeclaredOnes(t *testing.T) {
	configPath := filepath.Join("..", "..", "testdata", "config", "p12-assets.json")
	read := func(extra ...string) []string {
		buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p12-assets")
		output := filepath.Join(t.TempDir(), "out.cdx.json")
		args := append([]string{"generate", "--build-dir", buildDir, "--config", configPath,
			"--policy", "lenient", "--output", output, "--reproducible"}, extra...)
		code, _, stderr := execute(args)
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
		refs := make([]string, 0, len(document.Components))
		for _, component := range document.Components {
			if strings.HasPrefix(component.BomRef, "file:") {
				refs = append(refs, component.BomRef)
			}
		}
		sort.Strings(refs)
		return refs
	}

	withAssets := read()
	assertSet(t, withAssets, []string{
		"file:build:generated/config.bin",
		"file:project:assets/config.yaml",
		"file:project:assets/index.html",
		"file:project:main.c",
	})

	// includeAssets is a scope option of section 33.1 and had never decided
	// anything before this.
	without := read("--include-assets=false")
	assertSet(t, without, []string{"file:project:main.c"})
}
