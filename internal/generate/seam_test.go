package generate

import (
	"bytes"
	"errors"
	"io"
	osexec "os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/sbomwriter"
)

// fakeWriter is a second format that exists only in this test binary. The
// registry is process-global, so it is registered here, in a package of its
// own test binary, and never where the command line's tests would see it.
type fakeWriter struct{ id string }

const fakeBytes = "a document no other writer would render\n"

func (w fakeWriter) ID() string             { return w.id }
func (fakeWriter) Versions() []string       { return []string{"0.1"} }
func (fakeWriter) DefaultVersion() string   { return "0.1" }
func (fakeWriter) Validate(io.Reader) error { return errors.New("never asked") }
func (fakeWriter) Write(w io.Writer, document *sbomwriter.Document, options sbomwriter.Options) error {
	if document == nil || options.SpecVersion != "0.1" {
		return errors.New("the run did not hand over a document and a resolved version")
	}
	_, err := io.WriteString(w, fakeBytes)
	return err
}

func init() { sbomwriter.Register(fakeWriter{id: "fake-json"}) }

// TestASecondWriterIsSelectedThroughTheInterface is the seam: the run renders
// through whatever writer the configuration names, and nothing between the
// configuration and the rendered bytes knows which format that is. Before,
// the run type-asserted the CycloneDX writer and would have panicked on any
// other. The configuration is built directly because the loader's enum lists
// only real formats; the run itself must not care.
func TestASecondWriterIsSelectedThroughTheInterface(t *testing.T) {
	_, buildDir := portableFixture(t)
	cfg := config.Config{
		Project: config.Project{Root: "/__fixture_src__"},
		Output:  config.Output{Format: "fake-json"},
	}
	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Writer == nil || result.Writer.ID() != "fake-json" {
		t.Fatalf("the run rendered with %v, want fake-json", result.Writer)
	}
	if result.SpecVersion != "0.1" {
		t.Errorf("SpecVersion = %q, want the fake's default", result.SpecVersion)
	}
	if !bytes.Equal(result.Rendered, []byte(fakeBytes)) {
		t.Errorf("Rendered = %q, want the fake's bytes", result.Rendered)
	}
	// The fake states nothing about timestamps, so it is not assumed to drop
	// one: the finding is the writer's to earn.
	if reported := findingsWithID(result.Findings, "REPRODUCIBLE_MODE_OMITS_TIMESTAMP"); len(reported) != 0 {
		t.Errorf("a writer that does not omit the timestamp got %+v", reported)
	}
}

// TestTheRunNamesTheAdaptersThatContributed: the document states which
// evidence sources it rests on, and it is the same list the review report
// prints, so the two cannot disagree.
func TestTheRunNamesTheAdaptersThatContributed(t *testing.T) {
	cfg, buildDir := portableFixture(t)
	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Adapters) == 0 {
		t.Fatal("the fixture's evidence came from no adapter")
	}
	if !sort.StringsAreSorted(result.Document.Run.Adapters) {
		t.Errorf("Run.Adapters = %v, not sorted", result.Document.Run.Adapters)
	}
	if !reflect.DeepEqual(result.Document.Run.Adapters, result.Adapters) {
		t.Errorf("Run.Adapters = %v, the report's list = %v", result.Document.Run.Adapters, result.Adapters)
	}
}

// TestAReproducibleRunStatesNoWallClockTime holds the RunMetadata contract:
// under reproducible the run states SOURCE_DATE_EPOCH or nothing. A writer
// that has to state a creation time can then trust a non-empty value.
func TestAReproducibleRunStatesNoWallClockTime(t *testing.T) {
	cfg, buildDir := portableFixture(t)

	t.Setenv("SOURCE_DATE_EPOCH", "")
	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Document.Run.Timestamp; got != "" {
		t.Errorf("without SOURCE_DATE_EPOCH the reproducible run states %q", got)
	}

	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	result, err = RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Document.Run.Timestamp; got != "2023-11-14T22:13:20Z" {
		t.Errorf("with SOURCE_DATE_EPOCH the reproducible run states %q", got)
	}
	// And the CycloneDX document still omits it, which is why the finding
	// stays.
	if bom := renderedBOM(t, result); bom.Metadata == nil || bom.Metadata.Timestamp != "" {
		t.Error("the reproducible CycloneDX document carries a timestamp")
	}
	if reported := findingsWithID(result.Findings, "REPRODUCIBLE_MODE_OMITS_TIMESTAMP"); len(reported) != 1 {
		t.Errorf("REPRODUCIBLE_MODE_OMITS_TIMESTAMP reported %d times, want once", len(reported))
	}
}

// TestNoProductionCodeBelowTheCommandRegistersAWriter: the formats the binary
// can write are registered where the command imports them, and nowhere else.
// A package the run, the self command or the FOSS outputs depend on that
// imports a writer's package registers that writer as a side effect -- the
// bundled-SBOM input adapter once did, to read four fields of a CycloneDX
// document -- and removing the format from the command would then not remove
// it from the binary. go list -deps sees the production imports only, which is
// exactly the set that matters.
func TestNoProductionCodeBelowTheCommandRegistersAWriter(t *testing.T) {
	goTool, err := osexec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on PATH")
	}
	command := osexec.Command(goTool, "list", "-deps",
		"github.com/example/sbomb/internal/generate",
		"github.com/example/sbomb/internal/selfsbom",
		"github.com/example/sbomb/internal/foss")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, path := range strings.Fields(string(output)) {
		for _, writer := range []string{"github.com/example/sbomb/internal/cyclonedx", "github.com/example/sbomb/internal/spdx"} {
			if path == writer {
				t.Errorf("%s is a production dependency of the run; it registers its writer when imported", path)
			}
		}
	}
}
