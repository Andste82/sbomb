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

// Section 7.9. The corpus is the only place where this can be observed end to
// end: its evidence names /__fixture_src__ and the sources are committed
// somewhere else entirely, which is the CI split -- build in one job, generate
// in another -- reproduced exactly.

// fileRefs is the document's file components, sorted. They are the identities
// that must not move when the tree does.
func fileRefs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
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
	refs := make([]string, 0, len(document.Components))
	for _, component := range document.Components {
		if component.Type == "file" {
			refs = append(refs, component.BomRef)
		}
	}
	sort.Strings(refs)
	return refs
}

// componentLicenses maps every non-file component to what the document says its
// licence is.
func componentLicenses(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []struct {
			BomRef   string `json:"bom-ref"`
			Type     string `json:"type"`
			Licenses []struct {
				Expression string `json:"expression"`
				License    struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"license"`
			} `json:"licenses"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	licenses := map[string]string{}
	for _, component := range document.Components {
		if component.Type == "file" || len(component.Licenses) == 0 {
			continue
		}
		first := component.Licenses[0]
		switch {
		case first.Expression != "":
			licenses[component.BomRef] = first.Expression
		case first.License.ID != "":
			licenses[component.BomRef] = first.License.ID
		default:
			licenses[component.BomRef] = first.License.Name
		}
	}
	return licenses
}

// A build directory whose evidence records root A, and the tree at B: the
// licences resolve, which they cannot do without the relocation.
func TestRelocatedSourceTreeResolvesLicences(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	output := filepath.Join(t.TempDir(), "relocated.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
		"--source-dir", testutil.CorpusSourceTree(t),
		"--policy", "lenient", "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	want := map[string]string{
		"component:mit-lib":       "MIT",
		"component:apache-lib":    "Apache-2.0",
		"component:bsd-hdr":       "BSD-3-Clause",
		"component:lgpl-lib":      "LGPL-2.1-only",
		"component:multi-license": "MIT OR Apache-2.0",
		"component:nocopyright":   "0BSD",
	}
	licenses := componentLicenses(t, output)
	for ref, expression := range want {
		if licenses[ref] != expression {
			t.Errorf("licence of %s = %q, want %q", ref, licenses[ref], expression)
		}
	}
	// The generator's GPL text belongs to the generator and to nothing else.
	// It is a component here because the build graph names it as the input of
	// the file it wrote (section 16), and it is excluded from distribution --
	// but the licence of a neighbouring directory must never land on a
	// component that does not carry it, which is what this asserts.
	if licenses["component:gpl-gen"] != "GPL-2.0-only" {
		t.Errorf("licence of component:gpl-gen = %q, want GPL-2.0-only",
			licenses["component:gpl-gen"])
	}
	for ref, expression := range licenses {
		if ref == "component:gpl-gen" {
			continue
		}
		if strings.HasPrefix(expression, "GPL-2.0") {
			t.Errorf("%s carries %q; only the code generator is under it", ref, expression)
		}
	}
}

// The whole point of the anchor model: where the bytes are read from is not
// part of what the document says. Two trees in two different directories
// produce the same document byte for byte, serial number included.
func TestRelocationChangesNoIdentity(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	run := func(sourceDir string) string {
		t.Helper()
		output := filepath.Join(t.TempDir(), "out.cdx.json")
		args := []string{"generate", "--build-dir", buildDir, "--policy", "lenient",
			"--output", output, "--reproducible"}
		if sourceDir != "" {
			args = append(args, "--source-dir", sourceDir)
		}
		code, _, stderr := execute(args)
		if code != 0 || stderr != "" {
			t.Fatalf("generate = code %d, stderr %q", code, stderr)
		}
		return output
	}

	here := run(testutil.CorpusSourceTree(t))
	there := run(testutil.CorpusSourceTreeCopy(t))
	first, err := os.ReadFile(here)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(there)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("the same tree in two directories produced two different documents; a physical path reached the output")
	}
	if strings.Contains(string(first), testutil.RepoRoot(t)) {
		t.Error("the document names the directory the sources were read from")
	}

	// And against the run that reads no tree at all: the file identities come
	// from the build evidence, so they are the same whether the sources are
	// there or not.
	if got, want := fileRefs(t, here), fileRefs(t, run("")); !equalStrings(got, want) {
		t.Errorf("file identities with a source tree = %v\nwithout one = %v", got, want)
	}
}

// Section 7.9 rule 5: a source tree that is not there is a finding, once, and
// nothing else about the run changes. Before this milestone the licences were
// NOASSERTION and no finding said why.
func TestSourceTreeUnavailableIsReportedOncePerRun(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	directory := t.TempDir()
	findingsPath := filepath.Join(directory, "findings.json")
	output := filepath.Join(directory, "out.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--output", output, "--findings-json", findingsPath, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q; the finding must not change the exit code", code, stderr)
	}
	data, err := os.ReadFile(findingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Findings []struct {
			ID       string `json:"id"`
			Severity string `json:"severity"`
			Subject  struct {
				Kind string `json:"kind"`
				Ref  string `json:"ref"`
			} `json:"subject"`
			Message string `json:"message"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, finding := range report.Findings {
		if finding.ID != "SOURCE_TREE_UNAVAILABLE" {
			continue
		}
		count++
		if finding.Severity != "warning" {
			t.Errorf("severity = %q, want warning", finding.Severity)
		}
		// Section 30.7: a finding is one of the surfaces redaction covers, so
		// it names the anchor and not the directory.
		if strings.Contains(finding.Subject.Ref, "/") || strings.Contains(finding.Message, "/") {
			t.Errorf("the finding names a path: %s %s", finding.Subject.Ref, finding.Message)
		}
	}
	if count != 1 {
		t.Fatalf("SOURCE_TREE_UNAVAILABLE appears %d time(s); section 7.9 requires once per run", count)
	}

	// What the finding explains, stated in the document as section 22.7
	// requires rather than left blank.
	for ref, expression := range componentLicenses(t, output) {
		if expression != "NOASSERTION" {
			t.Errorf("licence of %s = %q with no source tree, want NOASSERTION", ref, expression)
		}
	}
	if !strings.Contains(string(mustRead(t, output)), `"value": "no-evidence"`) {
		t.Error("no component states the NOASSERTION reason no-evidence")
	}

	// And the counterpart: with the tree it does not fire.
	second := filepath.Join(t.TempDir(), "findings.json")
	code, _, stderr = execute([]string{"generate", "--build-dir", testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss"),
		"--source-dir", testutil.CorpusSourceTree(t), "--policy", "lenient",
		"--output", filepath.Join(t.TempDir(), "out.cdx.json"), "--findings-json", second, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	if strings.Contains(string(mustRead(t, second)), "SOURCE_TREE_UNAVAILABLE") {
		t.Error("the finding fires for a source tree that is there")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// `--source-dir .` is a path like any other. Section 3 defaults project.root
// to "." when nothing configured it, and the relocation of section 7.9 told
// the two apart by that string -- so a user standing in the restored source
// tree and typing the shortest thing that names it got no relocation at all:
// every licence NOASSERTION, and SOURCE_TREE_UNAVAILABLE advising them to
// point --source-dir at the tree they had just pointed it at.
func TestADotSourceDirRelocatesLikeAnyOtherPath(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	tree := testutil.CorpusSourceTreeCopy(t)

	licencesOf := func(args ...string) map[string]string {
		t.Helper()
		output := filepath.Join(t.TempDir(), "out.cdx.json")
		code, _, stderr := execute(append([]string{"generate", "--build-dir", buildDir,
			"--policy", "lenient", "--output", output, "--reproducible"}, args...))
		if code != 0 || stderr != "" {
			t.Fatalf("generate %v = code %d, stderr %q", args, code, stderr)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Components []struct {
				Name     string `json:"name"`
				Licenses []struct {
					License *struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"license"`
					Expression string `json:"expression"`
				} `json:"licenses"`
			} `json:"components"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, component := range document.Components {
			for _, entry := range component.Licenses {
				switch {
				case entry.Expression != "":
					out[component.Name] = entry.Expression
				case entry.License != nil && entry.License.ID != "":
					out[component.Name] = entry.License.ID
				case entry.License != nil:
					out[component.Name] = entry.License.Name
				}
			}
		}
		return out
	}

	absolute := licencesOf("--source-dir", tree)
	if absolute["mit-lib"] != "MIT" {
		t.Fatalf("the absolute path resolved mit-lib to %q, want MIT", absolute["mit-lib"])
	}

	// The same tree, named as the working directory.
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tree); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	dotted := licencesOf("--source-dir", ".")
	if dotted["mit-lib"] != absolute["mit-lib"] {
		t.Errorf("`--source-dir .` resolved mit-lib to %q, want %q as the absolute path did",
			dotted["mit-lib"], absolute["mit-lib"])
	}
}
