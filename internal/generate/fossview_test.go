package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/foss"
	"github.com/example/sbomb/internal/limits"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/testutil"
)

// fossFixture is the p14-foss corpus with its sources where they actually
// are: the evidence names /__fixture_src__ and the harvested tree lives in
// testdata/fixtures, so every run here is a relocated one (section 7.9).
func fossFixture(t *testing.T) (config.Config, string) {
	t.Helper()
	return config.Config{Project: config.Project{Root: testutil.CorpusSourceTree(t)}},
		filepath.Join(testutil.RepoRoot(t), "testdata", "fixtures", "gcc-ninja", "p14-foss", "build")
}

// TestOneDiscoveryWithAndWithoutTheFOSSView is the assertion section 32.6
// makes about cost and about correctness at once: asking for the FOSS view
// does not repeat discovery.
//
// It is asserted over the run's own counters -- how often the evidence graph
// was assembled, and how many files were opened for a hash -- rather than by
// timing, which is not a test.
func TestOneDiscoveryWithAndWithoutTheFOSSView(t *testing.T) {
	cfg, buildDir := fossFixture(t)

	plain, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.DefaultFlavor()})
	if err != nil {
		t.Fatal(err)
	}
	withView, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.DefaultFlavor(), FOSSView: true})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Counters.GraphBuilds != 1 || withView.Counters.GraphBuilds != 1 {
		t.Errorf("graph builds = %d without the view and %d with it; one run is one graph",
			plain.Counters.GraphBuilds, withView.Counters.GraphBuilds)
	}
	if plain.Counters.HashedFiles == 0 {
		t.Fatal("no file was hashed, so the comparison below proves nothing")
	}
	if plain.Counters.HashedFiles != withView.Counters.HashedFiles {
		t.Errorf("hashed files = %d with the FOSS view and %d without it; nothing may be re-hashed for it",
			withView.Counters.HashedFiles, plain.Counters.HashedFiles)
	}
	if plain.FOSSView != nil {
		t.Error("the licence view was computed although nobody asked for it")
	}
	if withView.FOSSView == nil {
		t.Fatal("the licence view was not computed")
	}
	if withView.FOSSView.NarrowedTotal == 0 {
		t.Error("the narrowing delta is zero on a fixture whose dependency files name toolchain headers")
	}
}

// TestTheFOSSViewReadsOnlyTheComponentsItReports is the section 31 rule
// applied to the union view: the narrowed header of a component that appears
// in no FOSS output is counted and never opened.
func TestTheFOSSViewReadsOnlyTheComponentsItReports(t *testing.T) {
	tree := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(tree, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("reported.h", "/* Copyright (c) 2026 Reported Header Authors */\n")
	write("ignored.h", "/* Copyright (c) 2026 Ignored Header Authors */\n")
	write("synthetic.h", "/* Copyright (c) 2026 Synthetic Header Authors */\n")

	document := &sbomwriter.Document{Components: []domain.Component{
		{
			ID: "component:reported", Name: "reported", Type: "library",
			DistributionRole: domain.RoleDistributed,
			Copyrights: []domain.CopyrightStatement{
				{Text: "Copyright (c) 2026 Already Known"},
			},
		},
		{
			ID: "component:toolchain", Name: "toolchain", Type: "library",
			DistributionRole: domain.RoleBuildTimeOnly,
		},
		{
			// Section 24.2's synthetic grouping: no files of its own, so no
			// chain from an artifact decided a role for it. The renderer
			// writes no entry for it, so the read set must not open its
			// headers either -- which is the drift the shared predicate
			// prevents.
			ID: "component:build-environment", Name: "build-environment", Type: "framework",
		},
	}}
	narrowing := []NarrowingCount{
		{Component: "reported", ComponentID: "component:reported", Count: 1, Headers: []string{"project:reported.h"}},
		{Component: "toolchain", ComponentID: "component:toolchain", Count: 4, Headers: []string{"toolchain:ignored.h"}},
		{Component: "build-environment", ComponentID: "component:build-environment", Count: 2, Headers: []string{"toolchain:synthetic.h"}},
	}
	var asked []string
	resolve := func(canonical string) (string, bool) {
		asked = append(asked, canonical)
		switch canonical {
		case "project:reported.h":
			return filepath.Join(tree, "reported.h"), true
		case "toolchain:ignored.h":
			return filepath.Join(tree, "ignored.h"), true
		case "toolchain:synthetic.h":
			return filepath.Join(tree, "synthetic.h"), true
		}
		return "", false
	}

	view := fossView(narrowing, document, resolve, limits.Config{}, NewLogger(0, nil))
	if view.NarrowedTotal != 7 {
		t.Errorf("narrowed total = %d, want 7: every component's share counts", view.NarrowedTotal)
	}
	// The read set is the renderer's own predicate and not a second spelling
	// of it, so the two cannot drift apart.
	for _, candidate := range document.Components {
		if foss.Reported(candidate) != fossReportedComponents(document)[bomRefOfComponent(candidate)] {
			t.Errorf("the read set disagrees with foss.Reported about %s", candidate.Name)
		}
	}
	if len(asked) != 1 || asked[0] != "project:reported.h" {
		t.Errorf("resolved %v; want only the reported component's header", asked)
	}
	reported := view.DeltaFor("component:reported")
	if len(reported.Copyrights) != 1 || reported.Copyrights[0].Text != "Copyright (c) 2026 Reported Header Authors" {
		t.Errorf("copyright from the narrowed header = %+v", reported.Copyrights)
	}
	if reported.Copyrights[0].File.Canonical() != "project:reported.h" {
		t.Errorf("the statement records %q rather than the file it came from", reported.Copyrights[0].File.Canonical())
	}
	if ignored := view.DeltaFor("component:toolchain"); ignored.Narrowed != 4 || len(ignored.Copyrights) != 0 {
		t.Errorf("the unreported component = %+v; want it counted and not read", ignored)
	}
	if synthetic := view.DeltaFor("component:build-environment"); synthetic.Narrowed != 2 || len(synthetic.Copyrights) != 0 {
		t.Errorf("the component with no role = %+v; want it counted and not read", synthetic)
	}
}

// TestTheUnionViewDropsWhatTheComponentAlreadyStates: the delta is what the
// licence view adds, not what it repeats. Otherwise the notices document would
// carry the same statement twice.
func TestTheUnionViewDropsWhatTheComponentAlreadyStates(t *testing.T) {
	tree := t.TempDir()
	const statement = "Copyright (c) 2026 The Same Authors"
	if err := os.WriteFile(filepath.Join(tree, "narrowed.h"), []byte("/* "+statement+" */\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	document := &sbomwriter.Document{Components: []domain.Component{{
		ID: "component:alpha", Name: "alpha", Type: "library",
		DistributionRole: domain.RoleDistributed,
		Copyrights:       []domain.CopyrightStatement{{Text: statement}},
	}}}
	view := fossView([]NarrowingCount{{Component: "alpha", Count: 1, Headers: []string{"project:narrowed.h"}}},
		document, func(string) (string, bool) { return filepath.Join(tree, "narrowed.h"), true },
		limits.Config{}, NewLogger(0, nil))
	if delta := view.DeltaFor("component:alpha"); len(delta.Copyrights) != 0 {
		t.Errorf("the delta repeats a statement the component already carries: %+v", delta.Copyrights)
	}
}

// TestDeltaForAnswersForAComponentWithNoDelta keeps every call site free of a
// second branch, and a nil view answers the same way.
func TestDeltaForAnswersForAComponentWithNoDelta(t *testing.T) {
	var absent *FOSSView
	if got := absent.DeltaFor("anything"); got.Narrowed != 0 || len(got.Copyrights) != 0 {
		t.Errorf("a nil view answered %+v", got)
	}
	view := &FOSSView{Components: []FOSSViewDelta{{Component: "alpha", Narrowed: 2}}}
	if got := view.DeltaFor("component:beta"); got.Narrowed != 0 {
		t.Errorf("an absent component answered %+v", got)
	}
}
