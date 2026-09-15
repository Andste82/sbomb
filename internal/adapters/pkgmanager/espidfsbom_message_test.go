package pkgmanager

import (
	"path/filepath"
	"strings"
	"testing"
)

// readYAMLFile formats "the ESP-IDF %s", so the kind it is handed must not say
// so again. The message used to read "the ESP-IDF ESP-IDF sbom.yml ...".
func TestIDFSBOMNamesTheFileOnceInItsFindings(t *testing.T) {
	dir := t.TempDir()
	// Past the bound of section 30, so the read is refused and says why.
	writeFile(t, filepath.Join(dir, idfSBOMName), strings.Repeat("x", maxIDFManifestBytes+1))

	contributions, findings := espidfsbom{}.Enrich(ComponentRoot{Path: dir, Name: "lib"})
	if len(contributions) != 0 {
		t.Errorf("a manifest that was never read contributed %d claims", len(contributions))
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	message := findings[0].Message
	if strings.Contains(message, "ESP-IDF ESP-IDF") {
		t.Errorf("message names the file twice: %q", message)
	}
	if !strings.Contains(message, "the ESP-IDF "+idfSBOMName) {
		t.Errorf("message does not name the file: %q", message)
	}
}
