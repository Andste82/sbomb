package sbomwriter

import (
	"errors"
	"io"
	"os"
	"path/filepath"
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
