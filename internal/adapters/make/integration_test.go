package make_test

import (
	"path/filepath"
	"strings"
	"testing"

	makeadapter "github.com/example/sbomb/internal/adapters/make"
	"github.com/example/sbomb/internal/adapters/ninja"
)

func TestMakeAndNinjaSourceInventoryEquivalent(t *testing.T) {
	fixture := filepath.Join("..", "..", "..", "testdata", "fixtures", "gcc-12-make", "p03-dupnames", "build")
	makeBuild, err := makeadapter.Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(makeBuild.Targets) != 1 {
		t.Fatalf("got %d Make targets, want 1", len(makeBuild.Targets))
	}

	ninjaBuild, err := ninja.ParseFile(strings.NewReader(`
build CMakeFiles/app.dir/one/main.cpp.o: CXX_COMPILER /__fixture_src__/p03/one/main.cpp
build CMakeFiles/app.dir/two/main.cpp.o: CXX_COMPILER /__fixture_src__/p03/two/main.cpp
build app: LINK CMakeFiles/app.dir/one/main.cpp.o CMakeFiles/app.dir/two/main.cpp.o
`))
	if err != nil {
		t.Fatal(err)
	}
	makeSources := make(map[string]bool)
	for _, source := range makeBuild.Targets[0].ObjectSources {
		makeSources[source] = true
	}
	ninjaSources := make(map[string]bool)
	for _, rule := range ninjaBuild.Rules {
		if len(rule.Inputs) == 1 && strings.HasSuffix(rule.Outputs[0], ".o") {
			ninjaSources[rule.Inputs[0]] = true
		}
	}
	if len(makeSources) != len(ninjaSources) {
		t.Fatalf("source counts differ: Make=%d Ninja=%d", len(makeSources), len(ninjaSources))
	}
	for source := range makeSources {
		if !ninjaSources[source] {
			t.Fatalf("source %q is present in Make inventory but not Ninja inventory", source)
		}
	}
}
