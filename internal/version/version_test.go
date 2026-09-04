package version

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

func TestResolveVersionCuratedAndHeaderMacro(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "include", "demo", "version.h")
	if err := os.MkdirAll(filepath.Dir(header), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(header, []byte("#define DEMO_VERSION_STRING \"7.8.9\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := domain.Component{ID: "demo", Name: "demo", Version: "9.9.9"}
	if v, src, conf, ok := Resolve(cfg, "", nil); !ok || v != "9.9.9" || src != "curated" || conf != domain.ConfidenceHigh {
		t.Fatalf("curated version failed: got v=%q src=%q conf=%v ok=%v", v, src, conf, ok)
	}

	headerComponent := domain.Component{ID: "demo", Name: "demo", VersionFrom: []string{"header:include/demo/version.h:DEMO_VERSION_STRING"}}
	if v, src, conf, ok := Resolve(headerComponent, dir, nil); !ok || v != "7.8.9" || src != "header" || conf != domain.ConfidenceMedium {
		t.Fatalf("header version failed: got v=%q src=%q conf=%v ok=%v", v, src, conf, ok)
	}
}

func TestResolveVersionHonorsRestrictionList(t *testing.T) {
	c := domain.Component{ID: "demo", Name: "demo", VersionFrom: []string{"header:include/demo/version.h:DEMO_VERSION_STRING"}}
	if v, _, _, ok := Resolve(c, t.TempDir(), nil); ok || v != "" {
		t.Fatalf("expected no version when header file missing and restriction list disallows all fallbacks, got v=%q ok=%v", v, ok)
	}
}

func TestPURLPercentEncodesSpecialCharacters(t *testing.T) {
	got := PURL("pkg:generic", "demo+core@latest", "1.2.3")
	if got != "pkg:generic/demo%2Bcore%40latest@1.2.3" {
		t.Fatalf("unexpected purl: %q", got)
	}
}
