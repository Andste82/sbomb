package generate

import (
	"os"
	"testing"

	"github.com/example/sbomb/internal/buildinfo"
)

// TestMain pins the reported tool version for every test in this package.
// Golden SBOMs record the tool that wrote them, so without this pin every
// release would rewrite every golden file for no substantive change.
func TestMain(m *testing.M) {
	buildinfo.Version = "0.0.0-test"
	os.Exit(m.Run())
}
