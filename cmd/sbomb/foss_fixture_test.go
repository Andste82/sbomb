package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// p14-foss is the fixture the attribution track is built on. It is the only
// project whose sources are committed, because a licence text cannot be read
// out of build evidence.

// TestFOSSFixtureLinkedObjectSet is the assertion the whole track leans on.
// The deliverable is linked against nine dependencies and the set of files
// that reaches it is decided by the linker, not by the directory listing:
//
//   - mit-lib compiles three sources and exactly one member is extracted;
//   - apache-lib and lgpl-lib have both of theirs extracted;
//   - gpl-gen is a code generator that was run during the build and linked
//     into nothing, so none of it may appear.
//
// Two toolchains, because the set is derived from evidence rather than from
// what one generator happens to record.
func TestFOSSFixtureLinkedObjectSet(t *testing.T) {
	want := []string{
		"file:build:generated/table.c",
		"file:project:dep/apache-lib/include/apache_lib.h",
		"file:project:dep/apache-lib/src/apache_one.c",
		"file:project:dep/apache-lib/src/apache_two.c",
		"file:project:dep/bsd-hdr/include/bsd_hdr.h",
		"file:project:dep/lgpl-lib/include/lgpl_lib.h",
		"file:project:dep/lgpl-lib/src/lgpl_core.c",
		"file:project:dep/lgpl-lib/src/lgpl_extra.c",
		"file:project:dep/mit-lib/include/mit_lib.h",
		"file:project:dep/mit-lib/src/mit_a.c",
		"file:project:dep/multi-license/include/multi.h",
		"file:project:dep/multi-license/src/multi.c",
		"file:project:dep/nocopyright/include/nocopyright.h",
		"file:project:dep/nocopyright/src/nocopyright.c",
		"file:project:dep/nolicense/include/nolicense.h",
		"file:project:dep/nolicense/src/nolicense.c",
		"file:project:src/main.c",
	}
	for _, toolchain := range []string{"gcc-ninja", "gcc-make"} {
		t.Run(toolchain, func(t *testing.T) {
			buildDir := testutil.CorpusBuildDir(t, toolchain, "p14-foss")
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
					Type   string `json:"type"`
				} `json:"components"`
			}
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(document.Components))
			for _, component := range document.Components {
				if component.Type != "file" {
					continue
				}
				got = append(got, component.BomRef)
			}
			sort.Strings(got)
			if len(got) != len(want) {
				t.Fatalf("file set = %v\nwant %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("file set = %v\nwant %v", got, want)
				}
			}
		})
	}
}

// TestFOSSFixtureGolden puts the fixture into the regression set before any
// attribution code exists. Every licence here is still NOASSERTION, because
// the source tree the evidence names is not where the harvested files are;
// that is what the source-tree relocation milestone changes, and this golden
// is what will show the change.
func TestFOSSFixtureGolden(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	output := filepath.Join(t.TempDir(), "foss.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient", "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "gcc-ninja-p14-foss.cdx.json", actual)
}

// TestInventoryDump exercises the flag section 32.1 lists and section 40
// specifies: the used-file set and the components in the tool's own format,
// sorted, stable, and written from the same run as the document rather than
// from a second discovery.
func TestInventoryDump(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	directory := t.TempDir()
	dumpPath := filepath.Join(directory, "inventory.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--output", filepath.Join(directory, "out.cdx.json"), "--inventory-dump", dumpPath, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	first, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("reading the inventory dump: %v", err)
	}
	assertGolden(t, "gcc-ninja-p14-foss-inventory.json", first)

	var dump struct {
		SchemaVersion int `json:"schemaVersion"`
		Files         []struct {
			Canonical string `json:"canonical"`
			Class     string `json:"class"`
		} `json:"files"`
		Components []struct {
			ID string `json:"id"`
		} `json:"components"`
	}
	if err := json.Unmarshal(first, &dump); err != nil {
		t.Fatalf("the inventory dump is not valid JSON: %v", err)
	}
	if dump.SchemaVersion != 1 {
		t.Errorf("schemaVersion = %d, want 1", dump.SchemaVersion)
	}
	if len(dump.Files) == 0 || len(dump.Components) == 0 {
		t.Fatalf("inventory dump is empty: %d file(s), %d component(s)", len(dump.Files), len(dump.Components))
	}
	// Section 40 requires the file list to be sorted by canonical path, and
	// every entry to carry the class the document says it has.
	for i := range dump.Files {
		if dump.Files[i].Class == "" {
			t.Errorf("%s carries no class", dump.Files[i].Canonical)
		}
		if i > 0 && dump.Files[i-1].Canonical >= dump.Files[i].Canonical {
			t.Fatalf("inventory dump is not sorted: %q before %q", dump.Files[i-1].Canonical, dump.Files[i].Canonical)
		}
	}

	// A second run over an independent copy of the same evidence must produce
	// the same bytes; that is what makes the dump usable as a golden at all.
	secondBuild := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	secondDirectory := t.TempDir()
	secondPath := filepath.Join(secondDirectory, "inventory.json")
	code, _, stderr = execute([]string{"generate", "--build-dir", secondBuild, "--policy", "lenient",
		"--output", filepath.Join(secondDirectory, "out.cdx.json"), "--inventory-dump", secondPath, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("second generate = code %d, stderr %q", code, stderr)
	}
	second, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("the inventory dump changed between two runs over the same evidence")
	}
}

// TestInventoryDumpNeedsAValue pins the refusal section 32.1 states for every
// flag: a value is required, and a missing one is an error rather than a
// silently ignored flag.
func TestInventoryDumpNeedsAValue(t *testing.T) {
	code, _, stderr := execute([]string{"generate", "--build-dir", t.TempDir(), "--inventory-dump"})
	if code != 1 || stderr == "" {
		t.Fatalf("result = code %d, stderr %q; want code 1 with an error", code, stderr)
	}
}
