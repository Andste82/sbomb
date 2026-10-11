package sbomwriter

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
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
	if err := validateTightly(writer, data); err != nil {
		return err
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

// validateTightly validates with the garbage collector set to collect at half
// its usual heap growth. Validation is where a run peaks: the run's own data
// and the rendered document are still alive, the decoded document joins them,
// and checking it allocates heavily and briefly. At the default growth the
// heap may reach twice what is alive before it is collected, and that headroom
// is most of what separates a large run from the memory budget of section 31.
// The phase is short, so collecting more often costs little time. A growth
// the user set through GOGC is halved as well, and GOGC=off is left off.
func validateTightly(writer Writer, data []byte) error {
	defer tightenCollector()()
	return validateOutput(writer, data)
}

// collector is the setting tightenCollector changed, shared by the calls
// that overlap: the setting is the process's, so the first to start changes
// it and the last to finish restores it.
var collector struct {
	sync.Mutex
	holders  int
	previous int
}

func tightenCollector() (release func()) {
	collector.Lock()
	defer collector.Unlock()
	if collector.holders == 0 {
		collector.previous = debug.SetGCPercent(-1)
		tightened := collector.previous
		if tightened > 0 {
			tightened = max(tightened/2, 1)
		}
		debug.SetGCPercent(tightened)
	}
	collector.holders++
	return func() {
		collector.Lock()
		defer collector.Unlock()
		collector.holders--
		if collector.holders == 0 {
			debug.SetGCPercent(collector.previous)
		}
	}
}

// validateOutput holds a document to both layers and, where the writer has
// them, to the invariants of its own output: in one pass when the writer
// offers one (OutputValidator), else call by call.
func validateOutput(writer Writer, data []byte) error {
	if validator, validates := writer.(OutputValidator); validates {
		return validator.ValidateOutput(data)
	}
	if err := writer.Validate(bytes.NewReader(data)); err != nil {
		return err
	}
	if checker, checks := writer.(OutputChecker); checks {
		return checker.CheckOutput(data)
	}
	return nil
}
