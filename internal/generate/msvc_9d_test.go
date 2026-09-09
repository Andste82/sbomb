//go:build !windows

package generate

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/pathmodel"
)

func TestMSVCAndGCCP02UsedFileSetsAgree(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	msvcBuild := filepath.Join(repoRoot, "testdata", "fixtures", "msvc-ninja", "p02-static", "build")
	nmakeBuild := filepath.Join(repoRoot, "testdata", "fixtures", "msvc-nmake", "p02-static", "build")
	gccBuild := filepath.Join(repoRoot, "testdata", "fixtures", "gcc-ninja", "p02-static", "build")
	msvc := run9dFixture(t, msvcBuild, "C:/__fixture_src__", "C:/__fixture_build__")
	nmake := run9dFixture(t, nmakeBuild, "C:/__fixture_src__", "C:/__fixture_build__")
	gcc := run9dFixture(t, gccBuild, "/__fixture_src__", "/__fixture_build__")

	gccProject := projectFileSet(gcc)
	msvcProject := projectFileSet(msvc)
	nmakeProject := projectFileSet(nmake)
	if diff := symmetricFileSetDifference(gccProject, msvcProject); len(diff) > 0 {
		t.Fatalf("MSVC and GCC p02-static used-file sets differ:\n%s", strings.Join(diff, "\n"))
	}
	if diff := symmetricFileSetDifference(gccProject, nmakeProject); len(diff) > 0 {
		t.Fatalf("MSVC NMake and GCC p02-static used-file sets differ:\n%s", strings.Join(diff, "\n"))
	}
	if len(msvcProject) == 0 {
		t.Fatal("MSVC produced no project files in the used-file set")
	}
}

func run9dFixture(t *testing.T, buildDir, sourceRoot, buildRoot string) map[string]bool {
	t.Helper()
	absoluteBuild, err := filepath.Abs(buildDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Project: config.Project{Name: "p02-static", Root: sourceRoot},
		Build:   config.Build{Dir: buildRoot},
	}
	result, err := RunWithOptions(cfg, absoluteBuild, true, Options{PathFlavor: pathmodel.WindowsFlavor{}})
	if err != nil {
		t.Fatalf("generate %s: %v", buildDir, err)
	}
	files := map[string]bool{}
	for _, node := range result.Graph.Nodes() {
		switch node.Kind {
		case domain.NodeArchive, domain.NodeArchiveMember, domain.NodeObject, domain.NodeSource, domain.NodeHeader, domain.NodeAsset, domain.NodeToolchainFile:
		default:
			continue
		}
		files[string(node.ID)] = true
	}
	return files
}

func symmetricFileSetDifference(want, got map[string]bool) []string {
	var diff []string
	for path := range want {
		if !got[path] {
			diff = append(diff, fmt.Sprintf("only in GCC: %s", path))
		}
	}
	for path := range got {
		if !want[path] {
			diff = append(diff, fmt.Sprintf("only in MSVC: %s", path))
		}
	}
	sort.Strings(diff)
	return diff
}

func projectFileSet(files map[string]bool) map[string]bool {
	project := map[string]bool{}
	for path := range files {
		if strings.HasPrefix(path, "project:") {
			project[path] = true
			continue
		}
		for _, marker := range []string{"__fixture_src__/", "__fixture_src__\\"} {
			if index := strings.Index(path, marker); index >= 0 {
				relative := strings.ReplaceAll(path[index+len(marker):], "\\", "/")
				project["project:"+relative] = true
				break
			}
		}
	}
	return project
}
