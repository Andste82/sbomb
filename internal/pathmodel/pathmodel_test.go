package pathmodel

import "testing"

func TestResolve(t *testing.T) {
	if got := Resolve("/tmp/project/src", "/tmp/project", "/tmp/project"); got != "project:src" {
		t.Fatalf("Resolve() = %q, want %q", got, "project:src")
	}
	if got := Resolve("/tmp/project/../project/src", "/tmp/project", "/tmp/project"); got != "project:src" {
		t.Fatalf("Resolve() = %q, want %q", got, "project:src")
	}
	if got := Slug("/tmp/project/src/very-long-name/alpha-beta-gamma", 16); got == "" || len(got) > 32 {
		t.Fatalf("Slug() = %q, length too long", got)
	}
}
