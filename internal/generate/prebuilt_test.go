package generate

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	makeadapter "github.com/example/sbomb/internal/adapters/make"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/policy"
)

// TestOnlyAnUnmappedPrebuiltLibraryIsReported links two prebuilt libraries
// from the source tree, one that a components[] entry claims and one that
// nothing claims. PREBUILT_LIBRARY_UNMAPPED is gated by
// prebuiltLibrariesRequireMapping, which is on in the default profile, so a
// finding raised for the mapped library would fail a run whose remediation has
// already been done.
func TestOnlyAnUnmappedPrebuiltLibraryIsReported(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "build")
	mapped := filepath.Join(sourceRoot, "third_party", "libfoo", "lib", "libfoo.a")
	unmapped := filepath.Join(sourceRoot, "third_party", "libbar", "libbar.a")
	for _, path := range []string{mapped, unmapped, filepath.Join(buildDir, "app")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("!<arch>\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(buildDir, "compile_commands.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	depfile := "app: " + filepath.ToSlash(mapped) + " " + filepath.ToSlash(unmapped) + "\n"
	if err := os.WriteFile(filepath.Join(buildDir, "app.d"), []byte(depfile), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Project:   config.Project{Name: "firmware", Root: sourceRoot},
		Artifacts: []config.Artifact{{Path: "app", LinkDepfile: filepath.Join(buildDir, "app.d")}},
		Components: []config.Component{{
			Path: "third_party/libfoo", Name: "libfoo", Version: "1.0.0", Supplier: "Foo Inc.",
		}},
	}
	options := Options{PathFlavor: pathmodel.PosixFlavor{}, Policy: policy.DefaultConfig()}
	result, err := RunWithOptions(cfg, buildDir, true, options)
	if err != nil {
		t.Fatal(err)
	}

	var reported []domain.Finding
	for _, finding := range result.Findings {
		if finding.ID == "PREBUILT_LIBRARY_UNMAPPED" {
			reported = append(reported, finding)
		}
	}
	if len(reported) != 1 || reported[0].Subject.Ref != "project:third_party/libbar/libbar.a" {
		t.Fatalf("PREBUILT_LIBRARY_UNMAPPED = %+v, want exactly one, for the unmapped library", reported)
	}

	// The unmapped library alone fails the default profile.
	verdict := policy.Evaluate(reported, policy.DefaultConfig(), nil, time.Time{})
	if !verdict.Fail || verdict.ExitCode != 3 {
		t.Fatalf("verdict = %+v, want the default profile to fail with exit 3", verdict)
	}

	// Once the second library is claimed too, nothing is reported.
	cfg.Components = append(cfg.Components, config.Component{
		Path: "third_party/libbar", Name: "libbar", Version: "2.0.0", Supplier: "Bar Ltd.",
	})
	result, err = RunWithOptions(cfg, buildDir, true, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range result.Findings {
		if finding.ID == "PREBUILT_LIBRARY_UNMAPPED" {
			t.Fatalf("a mapped library is still reported: %+v", finding)
		}
	}
}

// TestProducedArchivesComeFromEvidence pins what counts as an archive this
// build produced outside the build directory: one a build graph states as the
// output of an archiving step, or one with a member traced to an object of
// this build. An archive a Makefiles link line merely names, or whose member
// could not be traced, is still prebuilt -- and only an in-source build turns every archive
// under the project anchor into a produced one.
func TestProducedArchivesComeFromEvidence(t *testing.T) {
	graph := evidence.New()
	for _, id := range []string{"project:out/libgraph.a", "project:out/libmember.a", "project:vendor/libalias.a", "project:vendor/libunknown.a", "abs:opt/libext.a"} {
		graph.AddNode(domain.Node{ID: domain.NodeID(id), Kind: domain.NodeArchive, File: &domain.FileID{Anchor: anchorOf(id), RelPath: relOf(id)}})
	}
	graph.AddNode(domain.Node{ID: "build:obj/member.o", Kind: domain.NodeObject, File: &domain.FileID{Anchor: "build", RelPath: "obj/member.o"}})
	graph.AddEdge(domain.Edge{From: "project:out/libmember.a", To: "build:obj/member.o", Type: "archive-member", Strength: "linked", Confidence: domain.ConfidenceHigh})
	graph.AddNode(domain.Node{ID: "project:vendor/libunknown.a(x.o)", Kind: domain.NodeObject})
	graph.AddEdge(domain.Edge{From: "project:vendor/libunknown.a", To: "project:vendor/libunknown.a(x.o)", Type: "archive-member", Strength: "linked", Confidence: domain.ConfidenceMedium})

	b := newBuilder(graph, hostReadAnchors(t), "/bd", "/bd", NewLogger(0, nil))
	b.builtArchives["project:out/libgraph.a"] = true
	// A Makefiles link line records every archive it names, prebuilt or not.
	b.archiveInputs["project:vendor/libalias.a"] = []string{"obj/main.o"}

	got := b.producedArchives(false)
	want := map[string]bool{"project:out/libgraph.a": true, "project:out/libmember.a": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("produced = %v, want %v", got, want)
	}

	got = b.producedArchives(true)
	want = map[string]bool{"project:out/libgraph.a": true, "project:out/libmember.a": true, "project:vendor/libalias.a": true, "project:vendor/libunknown.a": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("produced in source = %v, want %v", got, want)
	}
}

// TestAnInSourceBuildsOwnArchiveIsNotPrebuilt builds in the project root
// itself. The registry then has no build anchor, so an archive the build wrote
// beside its sources resolves under the project anchor -- the same answer a
// checked-in library would get. Reporting it would fail the default profile
// for a library nobody can map, because the build made it.
func TestAnInSourceBuildsOwnArchiveIsNotPrebuilt(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "lib", "libmine.a")
	for _, path := range []string{archive, filepath.Join(root, "app")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("!<arch>\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "compile_commands.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	depfile := "app: " + filepath.ToSlash(archive) + "\n"
	if err := os.WriteFile(filepath.Join(root, "app.d"), []byte(depfile), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Project:   config.Project{Name: "firmware", Root: root},
		Artifacts: []config.Artifact{{Path: "app", LinkDepfile: filepath.Join(root, "app.d")}},
	}
	options := Options{PathFlavor: pathmodel.PosixFlavor{}, Policy: policy.DefaultConfig()}
	result, err := RunWithOptions(cfg, root, true, options)
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, file := range result.Document.Files {
		if file.ID.Canonical() == "project:lib/libmine.a" {
			linked = true
		}
	}
	if !linked {
		t.Fatalf("project:lib/libmine.a is not in the document; the test no longer exercises an in-source build")
	}
	for _, finding := range result.Findings {
		if finding.ID == "PREBUILT_LIBRARY_UNMAPPED" {
			t.Fatalf("an archive the in-source build produced is reported: %+v", finding)
		}
	}
	if verdict := policy.Evaluate(result.Findings, policy.DefaultConfig(), nil, time.Time{}); verdict.Fail {
		t.Fatalf("verdict = %+v, want the default profile to pass", verdict)
	}
}

// TestAStaticLibraryTargetsArchiveIsProduced: the CMake File API names the
// archive of every static library target, whatever the generator. An archive
// a Makefiles build writes into the source tree is this build's, though no
// build graph states it and no member was traced.
func TestAStaticLibraryTargetsArchiveIsProduced(t *testing.T) {
	b := newBuilder(evidence.New(), hostReadAnchors(t), "/bd", "/bd", NewLogger(0, nil))
	b.recordTargetArchives([]string{"/src/lib/libmine.a", "libinbuild.a"})
	got := b.producedArchives(false)
	want := map[string]bool{"project:lib/libmine.a": true, "build:libinbuild.a": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("produced = %v, want %v", got, want)
	}
}

// TestAStaticLibraryTargetsArchiveRaisesNothing: a target's archive need not
// be linked into anything, and naming it as produced must not register a
// location or a finding for a file the evidence never used -- an archive
// outside every anchor would otherwise fail a strict run as unanchored.
func TestAStaticLibraryTargetsArchiveRaisesNothing(t *testing.T) {
	b := newBuilder(evidence.New(), hostReadAnchors(t), "/bd", "/bd", NewLogger(0, nil))
	b.recordTargetArchives([]string{"/opt/out/libunlinked.a"})
	if len(b.findings) != 0 || len(b.physical) != 0 {
		t.Fatalf("findings %v, physical %v", b.findings, b.physical)
	}
}

// TestAMakefilesArchivingTargetProducesItsArchive: a Makefiles static library
// target names the archive it writes, here into the source tree; an
// executable's link line names archives as inputs and proves nothing.
func TestAMakefilesArchivingTargetProducesItsArchive(t *testing.T) {
	b := newBuilder(evidence.New(), hostReadAnchors(t), "/bd", "/bd", NewLogger(0, nil))
	b.recordMakeArchives(&makeadapter.Build{Targets: []makeadapter.Target{
		{Name: "mine", Archive: "/src/lib/libmine.a", LinkInputs: []string{"/src/lib/libmine.a", "/bd/CMakeFiles/mine.dir/a.c.o"}},
		{Name: "app", LinkInputs: []string{"/bd/CMakeFiles/app.dir/main.c.o", "/src/vendor/libfoo.a"}},
	}})
	got := b.producedArchives(false)
	want := map[string]bool{"project:lib/libmine.a": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("produced = %v, want %v", got, want)
	}
}

// TestOnlyACodemodelListsTheArchives: an in-source build counts every archive
// under the project root as produced only when nothing lists the archives it
// writes. A reply without a codemodel lists none.
func TestOnlyACodemodelListsTheArchives(t *testing.T) {
	for _, test := range []struct {
		name  string
		model *cmakeapi.Model
		want  bool
	}{
		{"no reply", nil, false},
		{"no codemodel", &cmakeapi.Model{}, false},
		{"codemodel", &cmakeapi.Model{Configurations: []cmakeapi.Configuration{{Name: "Release"}}}, true},
	} {
		if got := archivesListed(test.model); got != test.want {
			t.Errorf("%s: listed = %v, want %v", test.name, got, test.want)
		}
	}
}

// TestACodemodelEndsTheInSourceAssumption: in an in-source build, every
// archive under the project root is counted as produced only without a
// codemodel; with one, a checked-in prebuilt library is reported again.
func TestACodemodelEndsTheInSourceAssumption(t *testing.T) {
	inSource, err := anchors.Assemble(anchors.Options{Flavor: pathmodel.PosixFlavor{}, ProjectRoot: "/src", BuildRoot: "/src"})
	if err != nil {
		t.Fatal(err)
	}
	codemodel := &cmakeapi.Model{Configurations: []cmakeapi.Configuration{{Name: "Release"}}}
	for _, test := range []struct {
		name     string
		model    *cmakeapi.Model
		registry *pathmodel.Registry
		want     bool
	}{
		{"in source, no reply", nil, inSource.Registry, true},
		{"in source, codemodel", codemodel, inSource.Registry, false},
		{"out of source", nil, hostReadAnchors(t).Registry, false},
	} {
		if got := everyProjectArchiveIsBuilt(test.model, test.registry, "/src"); got != test.want {
			t.Errorf("%s: every project archive built = %v, want %v", test.name, got, test.want)
		}
	}
}
