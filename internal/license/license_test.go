package license

import (
	"os"
	"path/filepath"
	"strings"
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

// The normalizer must not depend on where the source wrapped its lines. It
// did: it dropped every line beginning with "copyright", and the Apache-2.0
// text wraps "copyright notice that is included in or attached to the work"
// onto its own line. The clause was deleted, the digest could never match, and
// 92 of 93 Apache-2.0 files in a sample of 160 real licence files came out as
// NOASSERTION.
func TestNormalizationDoesNotDependOnLineWrapping(t *testing.T) {
	wrapped := "Terms and conditions.\n" +
		"made available under the License, as indicated by a\n" +
		"copyright notice that is included in or attached to the work\n" +
		"(an example is provided in the Appendix below).\n"
	unwrapped := "Terms and conditions.\n" +
		"made available under the License, as indicated by a copyright notice\n" +
		"that is included in or attached to the work (an example is provided\n" +
		"in the Appendix below).\n"

	if NormalizeText(wrapped) != NormalizeText(unwrapped) {
		t.Errorf("the same text wrapped differently normalized differently:\n  %q\n  %q",
			NormalizeText(wrapped), NormalizeText(unwrapped))
	}
	if !strings.Contains(NormalizeText(wrapped), "copyright notice that is included") {
		t.Error("a substantive clause was removed as if it were a copyright notice")
	}
}

// A copyright statement is still ignored, as the SPDX matching guidelines
// require, in every form the licence texts actually use -- including the
// unfilled placeholders of the official texts.
func TestCopyrightStatementsAreStillIgnored(t *testing.T) {
	for _, notice := range []string{
		"Copyright (c) 2025 andste82",
		"Copyright 2009 The Go Authors. All rights reserved.",
		"Copyright [yyyy] [name of copyright owner]",
		"Copyright <year> <owner>",
		"Copyright © 2020 Example GmbH",
	} {
		if got := NormalizeText("Preamble.\n" + notice + "\nBody."); got != "preamble. body." {
			t.Errorf("NormalizeText with %q = %q, want the notice removed", notice, got)
		}
	}
	for _, prose := range []string{
		"copyright notice that is included in or attached to the work",
		"copyright license to reproduce, prepare Derivative Works of,",
	} {
		if got := NormalizeText("Preamble.\n" + prose + "\nBody."); got == "preamble. body." {
			t.Errorf("NormalizeText removed licence prose: %q", prose)
		}
	}
}
