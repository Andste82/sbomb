package license

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

func TestResolveFromTextExtractsSPDXHeader(t *testing.T) {
	text := "/* SPDX-License-Identifier: MIT */\nint main(void) { return 0; }\n"
	got := ResolveFromText(text, "src/main.c")
	if got.Expression != "MIT" {
		t.Fatalf("expected MIT expression, got %q", got.Expression)
	}
	if got.Evidence != "file-level" {
		t.Fatalf("expected file-level evidence, got %q", got.Evidence)
	}
	if got.Confidence != domain.ConfidenceHigh {
		t.Fatalf("expected high confidence, got %q", got.Confidence)
	}
}

func TestResolveFileMatchesExactLicenseTextHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "LICENSE")
	content := `MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveFile(path)
	if err != nil {
		t.Fatalf("ResolveFile returned error: %v", err)
	}
	if got.SPDXID != "MIT" {
		t.Fatalf("expected exact MIT hash match, got %#v", got)
	}
}

func TestResolveFileUnknownLicenseYieldsNOASSERTION(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "LICENSE")
	content := "This project is distributed under a custom license\nwith no SPDX match.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveFile(path)
	if err != nil {
		t.Fatalf("ResolveFile returned error: %v", err)
	}
	if got.Name != "NOASSERTION" {
		t.Fatalf("expected NOASSERTION, got %#v", got)
	}
	if got.Reason != ReasonLicenseTextUnrecognized {
		t.Fatalf("expected reason %q, got %q", ReasonLicenseTextUnrecognized, got.Reason)
	}
}

func TestCuratedOverrideWithConflict(t *testing.T) {
	got, conflicted := ResolveConflict("MIT", "Apache-2.0", "src/main.c")
	if !conflicted {
		t.Fatal("expected conflict to be detected")
	}
	if got.Expression != "MIT" {
		t.Fatalf("expected curated value to win, got %q", got.Expression)
	}
	if len(got.Conflicts) != 1 || got.Conflicts[0] != "Apache-2.0" {
		t.Fatalf("expected conflicting value recorded, got %#v", got.Conflicts)
	}
}
