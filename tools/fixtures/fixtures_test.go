package fixtures_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/tools/fixtures"
	"io/fs"
)

func TestFixtureCorpusPresent(t *testing.T) {
	for _, pair := range fixtures.AllPairs() {
		toolchain := pair[0]
		project := pair[1]
		dir := filepath.Join("..", "..", "testdata", "fixtures", toolchain, project)
		if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
			t.Fatalf("fixture missing manifest.json for %s/%s: %v", toolchain, project, err)
		}
	}
}

func TestFixturesContainNoHostPaths(t *testing.T) {
	if err := fixtures.CheckNoHostPaths(filepath.Join("..", "..", "testdata", "fixtures")); err != nil {
		t.Fatalf("fixture corpus contains host-specific paths: %v", err)
	}
}

func TestFixtureProvenancePresent(t *testing.T) {
	for _, pair := range fixtures.AllPairs() {
		dir := filepath.Join("..", "..", "testdata", "fixtures", pair[0], pair[1])
		if err := fixtures.CheckFixtureProvenance(dir); err != nil {
			t.Fatalf("fixture provenance missing or invalid for %s/%s: %v", pair[0], pair[1], err)
		}
	}
}

func TestNoThirdPartySourceCommitted(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "fixtures")
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Nothing is decided by a directory name: CMake writes a "src"
			// directory of stamp files for every populated dependency, and
			// FetchContent writes a "<name>-src" whose licence file is
			// required evidence for section 22.2. What the rule forbids is
			// source code, and that is checked file by file below.
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 256*1024 {
			t.Fatalf("fixture file too large: %s (%d bytes)", path, info.Size())
		}
		if isSourceExtension(path) && !generatedIntoTheBuildTree(path) && !harvestedFossSource(root, path) {
			t.Errorf("fixture tree contains source code: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walking fixture tree: %v", err)
	}
}

func TestFixgenIdempotent(t *testing.T) {
	tempDir := t.TempDir()
	fixtureFile := filepath.Join(tempDir, "sample.txt")
	content := "workspace=/workspaces/sbomb/build/debug\nartifact=/workspaces/sbomb/build/hello\n"
	if err := os.WriteFile(fixtureFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fixtures.RewriteFixtureTree(tempDir); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixtures.RewriteFixtureTree(tempDir); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("fixture rewrite drifted across runs")
	}
	if strings.Contains(string(second), "/workspaces/sbomb") {
		t.Fatal("sentinel rewriting failed to remove host path")
	}
}

// TestHarvestedFossSourcesArePresent asserts the other half of the rule above:
// the one source tree the policy allows has to actually be there. Every
// attribution behaviour is read out of these files, and a corpus that lost
// them looks complete while testing nothing.
func TestHarvestedFossSourcesArePresent(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "fixtures", fixtures.FossSourceTree)
	for _, required := range []string{
		filepath.Join("dep", "mit-lib", "LICENSE"),
		filepath.Join("dep", "apache-lib", "LICENSE"),
		filepath.Join("dep", "apache-lib", "NOTICE"),
		filepath.Join("dep", "bsd-hdr", "LICENSE"),
		filepath.Join("dep", "lgpl-lib", "LICENSE"),
		filepath.Join("dep", "gpl-gen", "LICENSE"),
		filepath.Join("dep", "multi-license", "LICENSE-MIT"),
		filepath.Join("dep", "multi-license", "LICENSE-APACHE"),
		filepath.Join("dep", "nocopyright", "LICENSE"),
		filepath.Join("src", "main.c"),
		"PROVENANCE.md",
	} {
		if _, err := os.Stat(filepath.Join(root, required)); err != nil {
			t.Errorf("harvested FOSS source tree is missing %s: %v", required, err)
		}
	}
	// The two components that exist to be incomplete. A licence file appearing
	// here would silently remove the cases they stand for.
	for _, forbidden := range []string{
		filepath.Join("dep", "nolicense", "LICENSE"),
		filepath.Join("dep", "nolicense", "COPYING"),
	} {
		if _, err := os.Stat(filepath.Join(root, forbidden)); err == nil {
			t.Errorf("harvested FOSS source tree carries %s, which is the case it exists to not have", forbidden)
		}
	}
}

// A licence's "how to apply this licence" appendix writes the holder as a
// bracketed placeholder -- `Copyright (C) <year> <name of author>` in the GNU
// texts, `Copyright [yyyy] [name of copyright owner]` in Apache-2.0 -- and the
// copyright extractor rejects a holder that is nothing but one of those
// (internal/license/copyright.go, placeholderGroup). A licence text whose
// opening angle brackets went missing defeats that rule silently: the line
// stops looking like a placeholder and is attributed to the component as a
// real notice. The corpus shipped exactly that damage in the LGPL and GPL
// texts, and it reached the goldens, so the shape is asserted here rather than
// left to be noticed again.
func TestFixtureLicenceTextsCarryBalancedPlaceholders(t *testing.T) {
	for _, dir := range []string{
		filepath.Join("projects", "p14-foss", "dep"),
		filepath.Join("..", "..", "testdata", "fixtures", fixtures.FossSourceTree, "dep"),
	} {
		matches, err := filepath.Glob(filepath.Join(dir, "*", "LICENSE*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
			t.Fatalf("no licence text found under %s", dir)
		}
		for _, path := range matches {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for number, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, ">") && !strings.Contains(line, "<") {
					t.Errorf("%s:%d closes a placeholder that was never opened: %q",
						path, number+1, line)
				}
			}
		}
	}
}

// TestRegenCheckRequiresHarvestedFossSources runs the corpus completeness check
// against a stand-in repository, because the real one is complete and the
// interesting case is the incomplete one. The check reports every fixture as
// missing there; what is asserted is only the line about the source tree.
func TestRegenCheckRequiresHarvestedFossSources(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required to run the corpus check")
	}
	stand := t.TempDir()
	scriptDir := filepath.Join(stand, "tools", "fixtures")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("regen.sh")
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(scriptDir, "regen.sh")
	if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
		t.Fatal(err)
	}
	licensePath := filepath.Join(stand, "testdata", "fixtures", fixtures.FossSourceTree, "dep", "mit-lib", "LICENSE")
	if err := os.MkdirAll(filepath.Dir(licensePath), 0o755); err != nil {
		t.Fatal(err)
	}

	const wanted = "missing " + fixtures.FossSourceTree + "/dep/mit-lib/LICENSE"
	run := func() string {
		var stderr strings.Builder
		command := exec.Command(bash, scriptPath, "--check")
		command.Stderr = &stderr
		_ = command.Run()
		return stderr.String()
	}
	if output := run(); !strings.Contains(output, wanted) {
		t.Errorf("check did not report the absent source tree:\n%s", output)
	}
	if err := os.WriteFile(licensePath, []byte("MIT License\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output := run(); strings.Contains(output, wanted) {
		t.Errorf("check reported an absent source tree although it is there:\n%s", output)
	}
}

// harvestedFossSource recognizes the one source tree testdata/fixtures/POLICY.md
// admits: the FOSS fixture's own dependencies, whose licence texts, notices and
// copyright headers are the material the attribution export reads. They are
// original fixture material rather than third-party payload, and without them
// no attribution behaviour can be observed at all.
func harvestedFossSource(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(filepath.ToSlash(relative), fixtures.FossSourceTree+"/")
}

func isSourceExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp", ".hxx", ".s", ".asm":
		return true
	}
	return false
}

// generatedIntoTheBuildTree recognizes the source-shaped files a generator
// wrote into the build tree. Those are evidence -- the unity aggregation file
// and the precompiled-header glue are what sections 17.1 and 14.5 are read
// from, and a file a custom command wrote into build/generated/ is what the
// build actually compiled -- and none of them is part of anyone's source tree.
func generatedIntoTheBuildTree(path string) bool {
	slashed := filepath.ToSlash(path)
	base := filepath.Base(slashed)
	return strings.Contains(slashed, "/CMakeFiles/") ||
		strings.Contains(slashed, "/build/generated/") ||
		strings.HasPrefix(base, "cmake_pch.") ||
		strings.HasPrefix(base, "unity_")
}

// sbomb writes its evidence dump into the build directory it was pointed at,
// so a manual run against a committed fixture leaves one behind -- and the
// next `git add -A` commits the tool's own output as the tool's own input.
// regen.sh removes it before harvesting for that reason
// ("sbomb's own output must never become fixture input"), and this is the same
// rule stated where a stray copy would be noticed.
//
// msvc-vs17/p02-static is the one exception: `sbomb explain` reads the dump
// out of the build directory, and that fixture carries one on purpose so the
// command can be exercised against the corpus.
func TestNoFixtureCarriesAStrayEvidenceDump(t *testing.T) {
	const deliberate = "msvc-vs17/p02-static"
	root := filepath.Join("..", "..", "testdata", "fixtures")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "evidence.json" {
			return err
		}
		relative := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		if strings.HasPrefix(relative, deliberate+"/") {
			return nil
		}
		t.Errorf("%s is sbomb's own output committed as its own input; regen.sh removes it before harvesting", relative)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
