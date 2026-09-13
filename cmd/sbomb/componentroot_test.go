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

// Section 19.2, against the corpus. The unit tests in internal/generate state
// the priority order and the nesting rules; what only the corpus can state is
// that the roots a real build resolves to are the directories the licence
// texts are actually in, over evidence that was written on another machine.

// componentRoots maps every non-file component to the root the document
// publishes for it, with the empty string for a component that carries none.
func componentRoots(t *testing.T, path string) map[string]string {
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
	roots := map[string]string{}
	for _, component := range document.Components {
		if component.Type == "file" {
			continue
		}
		roots[component.BomRef] = ""
		for _, property := range component.Properties {
			if property.Name == "sbomb:component:root" {
				roots[component.BomRef] = property.Value
			}
		}
	}
	return roots
}

// findingIDs is every finding the run reported, as a set.
func findingIDs(t *testing.T, path string) map[string]bool {
	t.Helper()
	var report struct {
		Findings []struct {
			ID      string `json:"id"`
			Subject struct {
				Ref string `json:"ref"`
			} `json:"subject"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(mustRead(t, path), &report); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, finding := range report.Findings {
		ids[finding.ID] = true
		ids[finding.ID+" "+finding.Subject.Ref] = true
	}
	return ids
}

// Every component of p14-foss begins where its licence text is, on both
// toolchains, from evidence that names a source root this machine does not
// have. Each entry here is a case the plan lists:
//
//   - mit-lib compiles three sources and the linker extracted one member, so
//     the deepest common directory of the used files is dep/mit-lib/src and
//     the LICENSE one level up was the one that used to be missed;
//   - bsd-hdr has a LICENSE and an include/ directory and no build file at
//     all, which is the shape a licence-file boundary exists for;
//   - multi-license carries LICENSE-MIT and LICENSE-APACHE and no file called
//     LICENSE, so a boundary list of exact names never saw it.
//
// gpl-gen is in the document on gcc-ninja only, where the build graph names
// the inputs of the generating rule (section 16); its root and its licence are
// resolved exactly like a linked component's, which is the point -- a
// build-time-only component is described, not omitted.
func TestFOSSFixtureComponentRoots(t *testing.T) {
	want := map[string]string{
		"component:project":       "project:",
		"component:apache-lib":    "project:dep/apache-lib",
		"component:bsd-hdr":       "project:dep/bsd-hdr",
		"component:lgpl-lib":      "project:dep/lgpl-lib",
		"component:mit-lib":       "project:dep/mit-lib",
		"component:multi-license": "project:dep/multi-license",
		"component:nocopyright":   "project:dep/nocopyright",
		"component:vendored-mix":  "project:dep/vendored-mix",
	}
	generatorRoot := map[string]string{"component:gpl-gen": "project:dep/gpl-gen"}
	wantLicenses := map[string]string{
		"component:project":       "MIT",
		"component:apache-lib":    "Apache-2.0",
		"component:bsd-hdr":       "BSD-3-Clause",
		"component:lgpl-lib":      "LGPL-2.1-only",
		"component:mit-lib":       "MIT",
		"component:multi-license": "MIT OR Apache-2.0",
		"component:nocopyright":   "0BSD",
		// Section 22.2 stops at the first identifier it reads, so the
		// GPL-2.0-only file copied into this directory does not change what
		// the component resolves to. Section 22.5 reports it instead
		// (FOSS_PER_FILE_LICENSE_DIVERGENCE).
		"component:vendored-mix": "MIT",
	}
	generatorLicense := map[string]string{"component:gpl-gen": "GPL-2.0-only"}
	for _, toolchain := range []string{"gcc-ninja", "gcc-make"} {
		t.Run(toolchain, func(t *testing.T) {
			want, wantLicenses := want, wantLicenses
			if toolchain == "gcc-ninja" {
				want, wantLicenses = merged(want, generatorRoot), merged(wantLicenses, generatorLicense)
			}
			directory := t.TempDir()
			output := filepath.Join(directory, "out.cdx.json")
			findingsPath := filepath.Join(directory, "findings.json")
			code, _, stderr := execute([]string{"generate",
				"--build-dir", testutil.CorpusBuildDir(t, toolchain, "p14-foss"),
				"--source-dir", testutil.CorpusSourceTree(t), "--policy", "lenient",
				"--output", output, "--findings-json", findingsPath, "--reproducible"})
			if code != 0 || stderr != "" {
				t.Fatalf("generate = code %d, stderr %q", code, stderr)
			}

			roots := componentRoots(t, output)
			if len(roots) != len(want) {
				t.Fatalf("components = %v, want %v", roots, want)
			}
			for ref, root := range want {
				if roots[ref] != root {
					t.Errorf("root of %s = %q, want %q", ref, roots[ref], root)
				}
			}
			licenses := componentLicenses(t, output)
			for ref, expression := range wantLicenses {
				if licenses[ref] != expression {
					t.Errorf("licence of %s = %q, want %q", ref, licenses[ref], expression)
				}
			}

			// The root of mit-lib is resolved although only one of its three
			// members was extracted: the other two are not in the document,
			// and the root is still their parent's parent.
			refs := fileRefs(t, output)
			for _, absent := range []string{"file:project:dep/mit-lib/src/mit_b.c", "file:project:dep/mit-lib/src/mit_c.c"} {
				for _, ref := range refs {
					if ref == absent {
						t.Fatalf("%s is in the document; the fixture no longer states the case", absent)
					}
				}
			}

			// Every root here was named by a marker, a package manager or the
			// anchor. None was guessed, so the finding that says a root was
			// guessed must not appear for any of them.
			if findingIDs(t, findingsPath)["COMPONENT_ROOT_UNRESOLVED"] {
				t.Error("COMPONENT_ROOT_UNRESOLVED fired for a component whose root a marker named")
			}
		})
	}
}

// merged is one expectation map plus the entries a toolchain adds to it.
func merged(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

// The licence texts that reach F4 are the ones the root carries, and a
// component carrying two carries both. LICENSE-MIT is the marker's sibling and
// must not be hidden by whichever of the two decided the boundary.
func TestFOSSFixtureMultiLicenceRootCarriesBothTexts(t *testing.T) {
	root := filepath.Join(testutil.CorpusSourceTree(t), "dep", "multi-license")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, 2)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "LICENSE") {
			got = append(got, entry.Name())
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "LICENSE-APACHE,LICENSE-MIT" {
		t.Fatalf("the component root carries %v, want both licence files", got)
	}
}

// The plan's second root test: the same build, read once with the objects the
// linker discarded and once without them. p08-gcsections is the only fixture
// built with --gc-sections, and sectionGarbageCollection decides whether sbomb
// acts on what the linker recorded -- so the two runs see two different
// used-file sets over one piece of evidence, which is exactly the variation
// that used to move a component root. Neither a root nor a licence may move
// with it.
func TestTheDiscardedObjectsDoNotMoveAnyComponentRoot(t *testing.T) {
	run := func(mode string) (map[string]string, map[string]string) {
		t.Helper()
		output := filepath.Join(t.TempDir(), "out.cdx.json")
		code, _, stderr := execute([]string{"generate",
			"--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p08-gcsections"),
			"--policy", "lenient", "--section-garbage-collection=" + mode,
			"--output", output, "--reproducible"})
		if code != 0 || stderr != "" {
			t.Fatalf("generate with %s = code %d, stderr %q", mode, code, stderr)
		}
		return componentRoots(t, output), componentLicenses(t, output)
	}

	kept, keptLicenses := run("ignore")
	dropped, droppedLicenses := run("exclude")
	if len(kept) == 0 {
		t.Fatal("the run produced no component to compare")
	}
	if len(kept) != len(dropped) {
		t.Fatalf("components with the discarded objects = %v, without them = %v", kept, dropped)
	}
	for ref, root := range kept {
		if dropped[ref] != root {
			t.Errorf("root of %s = %q with the discarded objects and %q without them", ref, root, dropped[ref])
		}
		if droppedLicenses[ref] != keptLicenses[ref] {
			t.Errorf("licence of %s = %q with the discarded objects and %q without them",
				ref, keptLicenses[ref], droppedLicenses[ref])
		}
	}
}
