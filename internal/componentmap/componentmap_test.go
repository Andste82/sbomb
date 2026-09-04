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

func TestMapFileUnknownComponentStillMarked(t *testing.T) {
	m := NewMapper(nil)
	got, ok := m.MapFile(domain.FileID{Anchor: "project", RelPath: "dep/third_party/noise.c"})
	if !ok {
		t.Fatal("expected unknown mapping to be returned")
	}
	if got.Name == "" || got.Type != "library" {
		t.Fatalf("unexpected unknown component: %#v", got)
	}
	if got.Properties["sbomb:component:detectedBy"][0] != "unresolved" {
		t.Fatalf("unknown component missing unresolved marker: %#v", got.Properties)
	}
}
