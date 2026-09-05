package make_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	makeadapter "github.com/example/sbomb/internal/adapters/make"
	"github.com/example/sbomb/internal/adapters/ninja"
)

// TestMakeAndNinjaSourceInventoryEquivalent checks the property that matters
// for the SBOM: the same project, built with two different CMake generators,
// must yield the same set of source files. Both sides read real generator
// output for the same fixture project, so a divergence is a real adapter
// disagreement rather than a mismatch between two hand-written samples.
func TestMakeAndNinjaSourceInventoryEquivalent(t *testing.T) {
	const project = "p03-dupnames"
	corpus := filepath.Join("..", "..", "..", "testdata", "fixtures")

	makeBuild, err := makeadapter.Parse(filepath.Join(corpus, "gcc-make", project, "build"))
	if err != nil {
		t.Fatalf("parsing Make evidence: %v", err)
	}
	makeSources := map[string]bool{}
	for _, target := range makeBuild.Targets {
		for _, source := range target.ObjectSources {
			makeSources[source] = true
		}
	}
	if len(makeSources) == 0 {
		t.Fatal("Make adapter resolved no sources at all")
	}

	buildNinja, err := os.Open(filepath.Join(corpus, "gcc-ninja", project, "build", "build.ninja"))
	if err != nil {
		t.Fatalf("opening Ninja evidence: %v", err)
	}
	defer buildNinja.Close()
	ninjaBuild, err := ninja.ParseFile(buildNinja)
	if err != nil {
		t.Fatalf("parsing Ninja evidence: %v", err)
	}
	ninjaSources := map[string]bool{}
	for _, rule := range ninjaBuild.Rules {
		if len(rule.Outputs) == 0 || !strings.HasSuffix(rule.Outputs[0], ".o") {
			continue
		}
		for _, input := range rule.Inputs {
			if isCSource(input) {
				ninjaSources[input] = true
			}
		}
	}
	if len(ninjaSources) == 0 {
		t.Fatal("Ninja adapter resolved no sources at all")
	}

	if diff := symmetricDifference(makeSources, ninjaSources); len(diff) > 0 {
		t.Fatalf("Make and Ninja source inventories differ: %v\n  Make:  %v\n  Ninja: %v",
			diff, sortedKeys(makeSources), sortedKeys(ninjaSources))
	}

	// p03-dupnames exists to prove that identical basenames stay distinct.
	var utilSources int
	for source := range makeSources {
		if filepath.Base(source) == "util.c" {
			utilSources++
		}
	}
	if utilSources != 2 {
		t.Fatalf("got %d distinct util.c sources, want 2 (duplicate basenames must not collapse)", utilSources)
	}
}

func isCSource(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".cc", ".cpp", ".cxx", ".s":
		return true
	default:
		return false
	}
}

func symmetricDifference(a, b map[string]bool) []string {
	var diff []string
	for key := range a {
		if !b[key] {
			diff = append(diff, "only in Make: "+key)
		}
	}
	for key := range b {
		if !a[key] {
			diff = append(diff, "only in Ninja: "+key)
		}
	}
	sort.Strings(diff)
	return diff
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
