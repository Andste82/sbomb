package sbomwriter

import (
	"bytes"
	"os"
	"path/filepath"
)

// WriteFile validates a rendered document and puts it at path atomically.
//
// Validation runs on the exact bytes that will be written, before the
// temporary file is renamed into place (section 32.5): a document that fails
// either layer never appears at the output path, not even briefly, and a
// previous document there is left as it was. A writer that also holds its own
// output to the invariants of how it writes (OutputChecker) is held to them
// here, because this is the one place sbomb's own documents pass through on
// their way out.
func WriteFile(path string, writer Writer, data []byte) error {
	if err := writer.Validate(bytes.NewReader(data)); err != nil {
		return err
	}
	if checker, checks := writer.(OutputChecker); checks {
		if err := checker.CheckOutput(data); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sbomb-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
