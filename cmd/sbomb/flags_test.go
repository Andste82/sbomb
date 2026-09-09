package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// Seven settings existed only in the configuration file. A one-off run against
// a build tree somebody handed you should not need a file written first.
func TestCommandLineOverridesTheConfiguration(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	config := filepath.Join("..", "..", "testdata", "config", "portable.json")
	output := filepath.Join(t.TempDir(), "out.cdx.json")

	code, _, stderr := execute([]string{"generate",
		"--build-dir", buildDir, "--config", config, "--output", output,
		"--policy", "lenient", "--reproducible",
		"--mode", "single", "--source-dir", ".", "--config-name", "Debug",
		"--image-manifest", filepath.Join(buildDir, "does-not-exist.json"),
		"--evidence-dump=off"})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
}

// The image manifest of an embedded-Linux distribution build is named with a
// flag of its own, in both spellings, because --image-manifest already names
// the native manifest of appendix E. A path that is not there is a warning and
// not the end of the run: the manifest improves a document that is correct
// without it.
func TestTheDistroManifestFlagIsAcceptedInBothSpellings(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
	config := filepath.Join("..", "..", "testdata", "config", "portable.json")
	manifest := filepath.Join(t.TempDir(), "manifest.csv")
	if err := os.WriteFile(manifest, []byte("\"PACKAGE\",\"VERSION\",\"LICENSE\"\n\"zlib\",\"1.3.1\",\"Zlib\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := execute([]string{"generate",
		"--build-dir", buildDir, "--config", config,
		"--output", filepath.Join(t.TempDir(), "out.cdx.json"),
		"--policy", "lenient", "--reproducible", "--evidence-dump=off",
		"--distro-manifest", manifest,
		"--distro-manifest=" + filepath.Join(buildDir, "does-not-exist.csv")})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}

	if code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--distro-manifest"}); code != 1 || stderr == "" {
		t.Fatalf("exit code = %d, stderr = %q, want a usage error for a flag without a value", code, stderr)
	}
}

func TestModeRejectsAnUnknownValue(t *testing.T) {
	code, _, stderr := execute([]string{"generate", "--build-dir", t.TempDir(), "--mode", "nonsense"})
	if code != 1 || stderr == "" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
}

// The evidence dump used to be written into the build directory unconditionally.
// It still is by default, because that is where explain looks for it, but a run
// can now choose the path or leave the directory untouched.
func TestEvidenceDumpIsSelectable(t *testing.T) {
	config := filepath.Join("..", "..", "testdata", "config", "portable.json")

	t.Run("default writes beside the build", func(t *testing.T) {
		buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
		output := filepath.Join(t.TempDir(), "out.cdx.json")
		if code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
			"--config", config, "--output", output, "--policy", "lenient", "--reproducible"}); code != 0 {
			t.Fatalf("exit code = %d, stderr = %s", code, stderr)
		}
		if _, err := os.Stat(filepath.Join(buildDir, "evidence.json")); err != nil {
			t.Errorf("no evidence dump in the build directory: %v", err)
		}
	})

	t.Run("a chosen path", func(t *testing.T) {
		buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
		dir := t.TempDir()
		chosen := filepath.Join(dir, "graph.json")
		if code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
			"--config", config, "--output", filepath.Join(dir, "out.cdx.json"),
			"--policy", "lenient", "--reproducible", "--evidence-dump", chosen}); code != 0 {
			t.Fatalf("exit code = %d, stderr = %s", code, stderr)
		}
		if _, err := os.Stat(chosen); err != nil {
			t.Errorf("nothing at the chosen path: %v", err)
		}
		if _, err := os.Stat(filepath.Join(buildDir, "evidence.json")); err == nil {
			t.Error("the build directory got a dump as well")
		}
	})

	t.Run("off leaves the build directory untouched", func(t *testing.T) {
		buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
		dir := t.TempDir()
		if code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
			"--config", config, "--output", filepath.Join(dir, "out.cdx.json"),
			"--policy", "lenient", "--reproducible", "--evidence-dump=off"}); code != 0 {
			t.Fatalf("exit code = %d, stderr = %s", code, stderr)
		}
		if _, err := os.Stat(filepath.Join(buildDir, "evidence.json")); err == nil {
			t.Error("the build directory was written to anyway")
		}
	})
}

// The struck flags must fail loudly. A tool that accepts a flag it does not
// implement is the failure this whole pass removed from the configuration.
func TestRemovedFlagsAreRefused(t *testing.T) {
	for _, flag := range []string{
		"--hash-alg=sha512", "--jobs=4", "--log-level=debug", "--log-format=json",
		"--absolute-paths", "--keep-raw-evidence", "--compile-commands=x.json",
		"--buildgraph=build.ninja", "--depfile-mode=ninja",
	} {
		code, _, stderr := execute([]string{"generate", "--build-dir", t.TempDir(), flag})
		if code != 1 || stderr == "" {
			t.Errorf("%s: exit code = %d, stderr = %q; want a refusal", flag, code, stderr)
		}
	}
}
