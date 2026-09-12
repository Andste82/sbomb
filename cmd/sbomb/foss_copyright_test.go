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

// copyrightDocument is the shape section 22.10 writes: the observation as an
// array of statements, the conclusion as a single string.
type copyrightDocument struct {
	Components []struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		BomRef    string `json:"bom-ref"`
		Copyright string `json:"copyright"`
		Evidence  struct {
			Copyright []struct {
				Text string `json:"text"`
			} `json:"copyright"`
		} `json:"evidence"`
	} `json:"components"`
}

func copyrightByComponent(t *testing.T, data []byte) (map[string][]string, map[string]string) {
	t.Helper()
	var document copyrightDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	observed := map[string][]string{}
	concluded := map[string]string{}
	for _, component := range document.Components {
		if component.Type == "file" {
			continue
		}
		concluded[component.BomRef] = component.Copyright
		for _, statement := range component.Evidence.Copyright {
			observed[component.BomRef] = append(observed[component.BomRef], statement.Text)
		}
	}
	return observed, concluded
}

// The fixture's attribution, component by component. Every case the corpus was
// built for is here: the notice in a source header, the notice that exists only
// inside the licence text, the dependency that has none at all, and the two
// files of one component that state the same holder twice.
func TestFOSSFixtureCopyrightStatements(t *testing.T) {
	want := map[string][]string{
		// src/main.c states the manufacturer's own notice; nolicense/ has no
		// licence file, so its files fold into the project component and its
		// holder is stated here too.
		"component:project": {
			"Copyright (c) 2026 Fixture Unlicensed Authors",
			"Copyright (c) 2026 sbomb fixture authors",
		},
		// Two sources and a header state one holder, and the NOTICE states it
		// again: one entry. The "how to apply this license" appendix of the
		// Apache text states a placeholder, which is not a holder.
		"component:apache-lib": {"Copyright 2026 Fixture Apache Library Authors"},
		// The header carries an identifier and no notice. The holder is inside
		// the licence text, which is exactly why BSD requires the text to be
		// reproduced.
		"component:bsd-hdr": {"Copyright (c) 2026, Fixture BSD Header Authors"},
		// The LGPL text carries the Free Software Foundation's notice on the
		// licence itself, and the fixture's own sources carry theirs. Both are
		// in the component's files; what they mean is not decided here. The
		// "how to apply" appendix of the same text states a placeholder, which
		// is not a holder -- as for apache-lib above.
		"component:lgpl-lib": {
			"Copyright (C) 1991, 1999 Free Software Foundation, Inc.",
			"Copyright (C) 2026 Fixture LGPL Library Authors",
		},
		"component:mit-lib":       {"Copyright (c) 2026 Fixture MIT Library Authors"},
		"component:multi-license": {"Copyright (c) 2026 Fixture Dual Licensed Authors"},
		// 0BSD with no notice in the sources and none in the licence file.
		"component:nocopyright": nil,
	}
	// The GPL-2.0 code generator is a component on gcc-ninja only (section
	// 16, open question Q14). Its notices are read exactly like a linked
	// component's: its own source header and the Free Software Foundation's
	// notice on the licence text, with the placeholder of the "how to apply"
	// appendix rejected -- the same shapes lgpl-lib shows, and the reason a
	// build-time-only component is described rather than dropped.
	generator := map[string][]string{
		"component:gpl-gen": {
			"Copyright (C) 1989, 1991 Free Software Foundation, Inc.",
			"Copyright (C) 2026 Fixture GPL Generator Authors",
		},
	}
	for _, toolchain := range []string{"gcc-ninja", "gcc-make"} {
		t.Run(toolchain, func(t *testing.T) {
			want := want
			if toolchain == "gcc-ninja" {
				merged := make(map[string][]string, len(want)+len(generator))
				for ref, statements := range want {
					merged[ref] = statements
				}
				for ref, statements := range generator {
					merged[ref] = statements
				}
				want = merged
			}
			buildDir := testutil.CorpusBuildDir(t, toolchain, "p14-foss")
			output := filepath.Join(t.TempDir(), "foss.cdx.json")
			code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
				"--source-dir", testutil.CorpusSourceTree(t), "--output", output, "--reproducible"})
			if code != 0 || stderr != "" {
				t.Fatalf("generate = code %d, stderr %q", code, stderr)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			observed, concluded := copyrightByComponent(t, data)

			for ref, statements := range want {
				if strings.Join(observed[ref], "\n") != strings.Join(statements, "\n") {
					t.Errorf("%s: statements = %q, want %q", ref, observed[ref], statements)
				}
				delete(observed, ref)
			}
			for ref, statements := range observed {
				if len(statements) > 0 {
					t.Errorf("%s carries unexpected statements %q", ref, statements)
				}
			}
			// Nothing is curated in this run, so no conclusion may be
			// published: component.copyright is somebody's assertion and
			// section 22.10 writes it from a curated value alone.
			for ref, value := range concluded {
				if value != "" {
					t.Errorf("%s: component.copyright = %q, and nothing was curated", ref, value)
				}
			}
		})
	}
}

// The prose of a licence text is where the word "copyright" appears most, and
// none of it is a notice (deviation D43). The fixture ships the MIT, BSD,
// Apache and LGPL texts, so this is the corpus that would show the failure.
func TestNoLicenceProseIsStoredAsANotice(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	output := filepath.Join(t.TempDir(), "foss.cdx.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--source-dir", testutil.CorpusSourceTree(t), "--output", output, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	observed, _ := copyrightByComponent(t, data)

	forbidden := []string{
		"Redistributions", "permission notice", "HOLDERS AND CONTRIBUTORS",
		"Grant of Copyright License", "name of copyright owner", "shall mean",
	}
	for ref, statements := range observed {
		for _, statement := range statements {
			for _, fragment := range forbidden {
				if strings.Contains(statement, fragment) {
					t.Errorf("%s stores licence prose as a notice: %q", ref, statement)
				}
			}
			if len(statement) > 200 {
				t.Errorf("%s stores a %d-byte statement, which is prose and not a notice: %q",
					ref, len(statement), statement)
			}
		}
	}
}

// Section 22.10 states the absence where the entry would have been. The
// fixture has one such component, and it is a real shape: 0BSD requires no
// notice at all, so a dependency under it may legitimately carry none.
func TestTheComponentWithoutANoticeIsReported(t *testing.T) {
	buildDir := testutil.CorpusBuildDir(t, "gcc-ninja", "p14-foss")
	directory := t.TempDir()
	findingsPath := filepath.Join(directory, "findings.json")
	code, _, stderr := execute([]string{"generate", "--build-dir", buildDir, "--policy", "lenient",
		"--source-dir", testutil.CorpusSourceTree(t), "--output", filepath.Join(directory, "out.cdx.json"),
		"--findings-json", findingsPath, "--reproducible"})
	if code != 0 || stderr != "" {
		t.Fatalf("generate = code %d, stderr %q", code, stderr)
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
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, finding := range report.Findings {
		if finding.ID != "FOSS_COPYRIGHT_MISSING" {
			continue
		}
		if finding.Severity != "info" {
			t.Errorf("severity = %q, want info", finding.Severity)
		}
		subjects = append(subjects, finding.Subject.Kind+":"+finding.Subject.Ref)
	}
	sort.Strings(subjects)
	if strings.Join(subjects, ",") != "component:component:nocopyright" {
		t.Errorf("FOSS_COPYRIGHT_MISSING on %v, want the one component that states no notice", subjects)
	}
}
