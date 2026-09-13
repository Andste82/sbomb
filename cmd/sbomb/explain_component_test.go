package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/testutil"
)

// Section 32.3 specifies three subjects for `explain`, and the component one
// answered nothing on every project: the evidence dump carries source, header,
// object, archive and artifact nodes, and component mapping happens above the
// graph without being written back into it (deviation D46, open question Q19).
//
// The mapping is in the document instead -- section 28's dependency cascade
// gives every grouping component a dependsOn list of its `file:` refs -- so
// the subject is resolved there and each file is explained from the dump.
func explainFixture(t *testing.T) (buildDir, document string) {
	t.Helper()
	buildDir = testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	document = filepath.Join(t.TempDir(), "app.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir,
		"--source-dir", testutil.CorpusSourceTree(t), "--policy", "lenient",
		"--output", document, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	return buildDir, document
}

func TestExplainAnswersForAComponent(t *testing.T) {
	buildDir, document := explainFixture(t)

	code, out, stderr := execute([]string{"explain", "--build-dir", buildDir,
		"--sbom", document, "--component", "mit-lib"})
	if code != 0 || stderr != "" {
		t.Fatalf("explain --component = code %d, stderr %q", code, stderr)
	}
	// Both files the document groups under the component, each with the chain
	// that reaches the deliverable. The archive member is the interesting one:
	// it says the linker extracted it, which is what "is this library really
	// in our product" asks.
	for _, want := range []string{
		"project:dep/mit-lib/src/mit_a.c",
		"project:dep/mit-lib/include/mit_lib.h",
		"archive-member",
		"artifact:build:fossapp",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the answer does not mention %q:\n%s", want, out)
		}
	}
	// And not the two archive members the linker never extracted, which are
	// not in the component's file set either.
	for _, unwanted := range []string{"mit_b.c", "mit_c.c"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("%s is not a used file of the component and must not appear:\n%s", unwanted, out)
		}
	}
}

func TestExplainForAComponentInJSON(t *testing.T) {
	buildDir, document := explainFixture(t)

	code, out, stderr := execute([]string{"explain", "--build-dir", buildDir,
		"--sbom", document, "--component", "bsd-hdr", "--format", "json"})
	if code != 0 || stderr != "" {
		t.Fatalf("explain --component --format json = code %d, stderr %q", code, stderr)
	}
	var answer struct {
		Component string `json:"component"`
		Files     []struct {
			Subject string          `json:"subject"`
			Chains  [][]interface{} `json:"chains"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &answer); err != nil {
		t.Fatalf("the answer is not JSON: %v\n%s", err, out)
	}
	if answer.Component != "bsd-hdr" {
		t.Errorf("component = %q, want bsd-hdr", answer.Component)
	}
	if len(answer.Files) != 1 || len(answer.Files[0].Chains) == 0 {
		t.Errorf("files = %+v, want the one header with its chains", answer.Files)
	}
}

// Three refusals, each naming which of three different things went wrong. The
// last two matter most: "unknown name" and "this document is about another
// build" are not the same answer, and neither is "no evidence chain", which
// reads as "that component is not in the product".
func TestExplainForAComponentRefusesWithTheReason(t *testing.T) {
	buildDir, document := explainFixture(t)

	t.Run("no document named", func(t *testing.T) {
		code, _, stderr := execute([]string{"explain", "--build-dir", buildDir, "--component", "mit-lib"})
		if code != 1 || !strings.Contains(stderr, "--sbom") {
			t.Fatalf("code = %d, stderr = %q; want a usage error naming the flag", code, stderr)
		}
	})

	t.Run("a document with no such component", func(t *testing.T) {
		code, _, stderr := execute([]string{"explain", "--build-dir", buildDir,
			"--sbom", document, "--component", "not-in-there"})
		if code != 1 || !strings.Contains(stderr, "names no component") {
			t.Fatalf("code = %d, stderr = %q; want the unknown name as the reason", code, stderr)
		}
	})

	t.Run("a document about another build", func(t *testing.T) {
		// The dump of one project and the document of another. Every file the
		// document groups is unknown here, which is a mismatch and not an
		// absent chain.
		other := testutil.CorpusBuildDir(t, "gcc-ninja", "p02-static")
		code, _, stderr := execute([]string{"generate", "--build-dir", other, "--policy", "lenient",
			"--output", filepath.Join(t.TempDir(), "other.cdx.json"), "--reproducible"})
		if code != 0 {
			t.Fatalf("generate on the second fixture = code %d, stderr %q", code, stderr)
		}
		code, _, stderr = execute([]string{"explain", "--build-dir", other,
			"--sbom", document, "--component", "mit-lib"})
		if code != 1 || !strings.Contains(stderr, "describes a different build") {
			t.Fatalf("code = %d, stderr = %q; want the mismatch as the reason", code, stderr)
		}
	})

	t.Run("a document that is not one", func(t *testing.T) {
		broken := filepath.Join(t.TempDir(), "broken.cdx.json")
		if err := os.WriteFile(broken, []byte("not json at all"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := execute([]string{"explain", "--build-dir", buildDir,
			"--sbom", broken, "--component", "mit-lib"})
		if code != 1 || !strings.Contains(stderr, "is not a document this can read") {
			t.Fatalf("code = %d, stderr = %q; want the unreadable document as the reason", code, stderr)
		}
	})

	t.Run("a document without a component subject", func(t *testing.T) {
		code, _, stderr := execute([]string{"explain", "--build-dir", buildDir,
			"--sbom", document, "--file", "project:src/main.c"})
		if code != 1 || !strings.Contains(stderr, "--sbom is read for --component") {
			t.Fatalf("code = %d, stderr = %q; want --sbom refused without --component", code, stderr)
		}
	})
}
