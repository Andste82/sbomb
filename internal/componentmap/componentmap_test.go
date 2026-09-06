package componentmap

import (
	"testing"

	"github.com/example/sbomb/internal/domain"
)

func TestMapFileUsesLongestPrefixAndGlob(t *testing.T) {
	m := NewMapper([]Rule{
		{Path: "dep", Name: "dep-root", Type: "library"},
		{Path: "dep/vendor", Name: "vendor-lib", Type: "library"},
		{Match: "dep/vendor-*/**", Name: "glob-lib", Type: "library"},
	})

	if got, ok := m.MapFile(domain.FileID{Anchor: "project", RelPath: "dep/vendor/sub/thing.c"}); !ok || got.Name != "vendor-lib" {
		t.Fatalf("expected vendor-lib prefix mapping, got %#v ok=%v", got, ok)
	}
	if got, ok := m.MapFile(domain.FileID{Anchor: "project", RelPath: "dep/vendor-foo/src/lib.c"}); !ok || got.Name != "glob-lib" {
		t.Fatalf("expected glob-lib mapping, got %#v ok=%v", got, ok)
	}
}

// TestMapFileReportsNoMatch pins the contract that makes the priority chain of
// section 19.2 possible: strategy 1 must be able to say it found nothing, so
// that strategies 6 through 8 can run. Answering "unknown" here would make
// curated configuration the only strategy that ever applies.
func TestMapFileReportsNoMatch(t *testing.T) {
	m := NewMapper(nil)
	got, ok := m.MapFile(domain.FileID{Anchor: "project", RelPath: "dep/third_party/noise.c"})
	if ok {
		t.Fatalf("an empty rule set matched: %#v", got)
	}

	withRules := NewMapper([]Rule{{Path: "dep/mbedtls", Name: "mbedtls"}})
	if _, ok := withRules.MapFile(domain.FileID{Anchor: "project", RelPath: "src/main.c"}); ok {
		t.Fatal("a file outside every rule matched")
	}
	if got, ok := withRules.MapFile(domain.FileID{Anchor: "project", RelPath: "dep/mbedtls/aes.c"}); !ok || got.Name != "mbedtls" {
		t.Fatalf("MapFile() = (%#v, %v), want the mbedtls rule", got, ok)
	}
}
