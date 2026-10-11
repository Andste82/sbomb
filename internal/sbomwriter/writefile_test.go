package sbomwriter

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

// checkingWriter is a writer whose validation and output check answer what
// the test tells them to. It is never registered.
type checkingWriter struct {
	invalid   error
	unchecked error
	checked   *int
}

func (checkingWriter) ID() string                                { return "checking-json" }
func (checkingWriter) Versions() []string                        { return []string{"1"} }
func (checkingWriter) DefaultVersion() string                    { return "1" }
func (checkingWriter) Write(io.Writer, *Document, Options) error { return nil }
func (w checkingWriter) Validate(r io.Reader) error {
	if _, err := io.ReadAll(r); err != nil {
		return err
	}
	return w.invalid
}
func (w checkingWriter) CheckOutput([]byte) error {
	if w.checked != nil {
		*w.checked++
	}
	return w.unchecked
}

// TestWriteFileWritesOnlyWhatPassedEveryCheck: section 32.5 validates the
// exact bytes before they are renamed into place, so a document that fails
// either the format's validation or the writer's own output check never
// appears at the path, and a previous document there survives.
func TestWriteFileWritesOnlyWhatPassedEveryCheck(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "nested", "out.json")

	checks := 0
	if err := WriteFile(path, checkingWriter{checked: &checks}, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if checks != 1 {
		t.Errorf("the output check ran %d times, want once", checks)
	}

	for name, writer := range map[string]checkingWriter{
		"invalid":   {invalid: errors.New("schema says no")},
		"unchecked": {unchecked: errors.New("not how sbomb writes")},
	} {
		if err := WriteFile(path, writer, []byte("second")); err == nil {
			t.Errorf("%s: a document that failed was written", name)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "first" {
			t.Errorf("%s: the previous document became %q (%v)", name, data, err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the directory holds %d entries; a refused write left a temporary file behind", len(entries))
	}
}

// TestSourceDateEpochTellsUnsetFromUnreadable: a format that has to state a
// pinned time must be able to tell "nobody pinned one" from "somebody tried".
func TestSourceDateEpochTellsUnsetFromUnreadable(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "")
	if got, err := SourceDateEpoch(); got != "" || err != nil {
		t.Errorf("unset = %q, %v", got, err)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	if got, err := SourceDateEpoch(); got != "2023-11-14T22:13:20Z" || err != nil {
		t.Errorf("1700000000 = %q, %v", got, err)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "yesterday")
	if _, err := SourceDateEpoch(); err == nil {
		t.Error("an unreadable SOURCE_DATE_EPOCH was accepted")
	}
	// The ends of what RFC 3339 can state are readable; one second beyond
	// either is not, because it has no four-digit year.
	for value, want := range map[string]string{
		"253402300799": "9999-12-31T23:59:59Z",
		"-62167219200": "0000-01-01T00:00:00Z",
		"253402300800": "",
		"-62167219201": "",
	} {
		t.Setenv("SOURCE_DATE_EPOCH", value)
		got, err := SourceDateEpoch()
		if got != want || (want == "") != (err != nil) {
			t.Errorf("%s = %q, %v; want %q", value, got, err, want)
		}
	}
}

// TestARefusalNamesItsFinding: the command line prints a refusal as the
// identifier a user looks up in appendix A, then the sentence.
func TestARefusalNamesItsFinding(t *testing.T) {
	var err error = &RefusalError{ID: "SOME_FINDING", Message: "cannot be honoured"}
	if err.Error() != "SOME_FINDING: cannot be honoured" {
		t.Errorf("Error() = %q", err.Error())
	}
}

// validatingWriter answers Validate and CheckOutput in one call as well.
type validatingWriter struct {
	checkingWriter
	verdict error
	calls   *int
}

func (w validatingWriter) ValidateOutput([]byte) error {
	*w.calls++
	return w.verdict
}

// A writer that validates its output in one pass is asked once, and its
// verdict decides as the two separate calls would.
func TestWriteFilePrefersTheOnePassValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	calls, checks := 0, 0
	writer := validatingWriter{
		checkingWriter: checkingWriter{invalid: errors.New("not asked"), checked: &checks},
		calls:          &calls,
	}
	if err := WriteFile(path, writer, []byte("document")); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || checks != 0 {
		t.Errorf("ValidateOutput ran %d times and CheckOutput %d; want the one pass alone", calls, checks)
	}
	writer.verdict = errors.New("refused")
	if err := WriteFile(path, writer, []byte("second")); err == nil {
		t.Error("a document the one pass refused was written")
	}
	if data, _ := os.ReadFile(path); string(data) != "document" {
		t.Errorf("the previous document was replaced by %q", data)
	}
}

// Validation runs with the collector tightened, and the setting the run had
// is restored afterwards, whatever the verdict.
func TestWriteFileRestoresTheCollectorSetting(t *testing.T) {
	previous := debug.SetGCPercent(80)
	defer debug.SetGCPercent(previous)
	seen := 0
	writer := validatingWriter{calls: new(int)}
	observe := func() {
		seen = debug.SetGCPercent(-1)
		debug.SetGCPercent(seen)
	}
	checking := observingWriter{validatingWriter: writer, observe: observe}
	if err := WriteFile(filepath.Join(t.TempDir(), "out.json"), checking, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if seen != 40 {
		t.Errorf("validation ran at GOGC %d, want half of 80", seen)
	}
	if now := debug.SetGCPercent(80); now != 80 {
		t.Errorf("GOGC is %d after writing, want the 80 it was", now)
	}
}

type observingWriter struct {
	validatingWriter
	observe func()
}

func (w observingWriter) ValidateOutput(data []byte) error {
	w.observe()
	return w.validatingWriter.ValidateOutput(data)
}

func currentGCPercent() int {
	current := debug.SetGCPercent(-1)
	debug.SetGCPercent(current)
	return current
}

// GOGC=0, which collects all the time, is not halved into "off"; and calls
// that overlap restore the setting the first of them found, once the last of
// them is done.
func TestTheCollectorSettingSurvivesOverlapAndZero(t *testing.T) {
	previous := debug.SetGCPercent(0)
	defer debug.SetGCPercent(previous)
	release := tightenCollector()
	if got := currentGCPercent(); got != 0 {
		t.Errorf("GOGC 0 became %d", got)
	}
	release()

	debug.SetGCPercent(80)
	first := tightenCollector()
	second := tightenCollector()
	first()
	if got := currentGCPercent(); got != 40 {
		t.Errorf("with a call still validating, GOGC is %d, want 40", got)
	}
	second()
	if got := currentGCPercent(); got != 80 {
		t.Errorf("after both, GOGC is %d, want the 80 it was", got)
	}
}
