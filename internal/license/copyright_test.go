package license

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/example/sbomb/internal/domain"
)

func statementsOf(t *testing.T, text string) []string {
	t.Helper()
	return ExtractCopyright([]byte(text))
}

func joined(statements []string) string { return strings.Join(statements, "|") }

// The two forms section 22.10 recognizes, and nothing between them. Both are
// stored as the file states them: the tag is the marker that a statement
// follows, the conventional line is the statement itself.
func TestBothRecognizedFormsAreExtracted(t *testing.T) {
	for _, testCase := range []struct {
		name string
		file string
		want string
	}{
		{"reuse tag", "/* SPDX-FileCopyrightText: 2026 Acme Inc. */\nint main(void){return 0;}\n", "2026 Acme Inc."},
		{"classic", " * Copyright (c) 2026 Acme Inc.\n", "Copyright (c) 2026 Acme Inc."},
		{"classic upper marker", "// Copyright (C) 2026 Acme Inc.\n", "Copyright (C) 2026 Acme Inc."},
		{"classic sign", "# Copyright © 2020-2026 Acme Inc.\n", "Copyright © 2020-2026 Acme Inc."},
		{"marker before the word", "(C) Copyright 2026 Acme Inc.\n", "(C) Copyright 2026 Acme Inc."},
		{"no year at all", "Copyright Acme Inc.\n", "Copyright Acme Inc."},
		{"several years", "Copyright (c) 1991, 1999, 2026 Acme Inc.\n", "Copyright (c) 1991, 1999, 2026 Acme Inc."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := statementsOf(t, testCase.file)
			if len(got) != 1 || got[0] != testCase.want {
				t.Fatalf("statements = %q, want [%q]", got, testCase.want)
			}
		})
	}
}

// A file may name several holders, and each line is a notice of its own. The
// order is the order of appearance, so that "the first in canonical file
// order" means something.
func TestAFileWithTwoHoldersYieldsBoth(t *testing.T) {
	file := "/*\n * Copyright (c) 2019 First Holder\n * Copyright (c) 2026 Second Holder\n */\n#include <stdio.h>\n"
	got := statementsOf(t, file)
	want := "Copyright (c) 2019 First Holder|Copyright (c) 2026 Second Holder"
	if joined(got) != want {
		t.Fatalf("statements = %q, want %q", got, want)
	}
}

// The word in prose is not a notice, and a licence text is where the word
// appears most (deviation D43). None of these lines may be stored.
func TestProseAboutCopyrightIsNotANotice(t *testing.T) {
	file := strings.Join([]string{
		"The above copyright notice and this permission notice shall be included",
		"2. Redistributions in binary form must reproduce the above copyright notice,",
		"3. Neither the name of the copyright holder nor the names of its contributors",
		`THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"`,
		"   2. Grant of Copyright License. Subject to the terms and conditions of",
		"we copyright the library, and (2) we offer you this license",
		"Copyright",
		"Copyright (c)",
	}, "\n") + "\n"
	if got := statementsOf(t, file); len(got) != 0 {
		t.Fatalf("statements = %q, want none", got)
	}
}

// The "how to apply this license" appendix of Apache-2.0 and of the GNU
// licences tells the reader to write a notice. It is not one, and every
// licence text of those families carries it.
func TestAPlaceholderWhereTheHolderBelongsIsNotANotice(t *testing.T) {
	file := "Copyright [yyyy] [name of copyright owner]\nCopyright (C) <year>  <name of author>\nCopyright {year} {holder}\n"
	if got := statementsOf(t, file); len(got) != 0 {
		t.Fatalf("statements = %q, want none", got)
	}
	// The same line with a real holder beside the placeholder year is a
	// notice: what has to be stated is the holder.
	got := statementsOf(t, "Copyright [yyyy] Acme Inc.\n")
	if len(got) != 1 || got[0] != "Copyright [yyyy] Acme Inc." {
		t.Fatalf("statements = %q, want the notice with its real holder", got)
	}
}

// Nothing is rewritten. The year range is the file's, the punctuation is the
// file's, the case is the file's, and the comment scaffolding is not part of
// the notice.
func TestTheStatementIsStoredVerbatim(t *testing.T) {
	file := "/* Copyright (c) 2019 - 2021,2026  ACME   inc., All Rights Reserved  */\n"
	want := "Copyright (c) 2019 - 2021,2026  ACME   inc., All Rights Reserved"
	got := statementsOf(t, file)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("statement = %q, want %q", got, want)
	}
	if !strings.Contains(file, got[0]) {
		t.Errorf("the stored statement %q is not a substring of the file", got[0])
	}
}

// Section 22.10: the window is the first 64 KiB, and it is cut back to a line
// break so that no statement is ever stored truncated.
func TestOnlyTheFirst64KiBIsSearched(t *testing.T) {
	inside := "Copyright (c) 2026 Inside Holder\n"
	filler := strings.Repeat("/* padding */\n", (CopyrightWindow/14)+1)
	got := statementsOf(t, inside+filler+"Copyright (c) 2026 Outside Holder\n")
	if len(got) != 1 || got[0] != "Copyright (c) 2026 Inside Holder" {
		t.Fatalf("statements = %q, want only the one inside the window", got)
	}

	// A statement that straddles the end of the window is not half-stored: the
	// window ends at the last line break before it.
	padding := strings.Repeat("x", CopyrightWindow-10)
	straddling := padding + "\nCopyright (c) 2026 Straddling Holder\n"
	for _, statement := range statementsOf(t, straddling) {
		if !strings.Contains(straddling, statement) {
			t.Errorf("stored %q, which is not in the file", statement)
		}
		if strings.HasPrefix("Copyright (c) 2026 Straddling Holder", statement) &&
			statement != "Copyright (c) 2026 Straddling Holder" {
			t.Errorf("stored the truncated statement %q", statement)
		}
	}
}

// A build tree is untrusted input and need not be UTF-8. Invalid bytes must
// not crash the extractor, and they must not reach a stored value: a JSON
// encoder substitutes U+FFFD for each one, and a notice with substituted bytes
// is not the notice.
func TestNonUTF8BytesAreNeitherFatalNorStored(t *testing.T) {
	data := []byte("Copyright (c) 2026 Valid Holder\nCopyright (c) 2026 \xff\xfe Broken Holder\n\xff\xfe\xfd\n")
	got := ExtractCopyright(data)
	if len(got) != 1 || got[0] != "Copyright (c) 2026 Valid Holder" {
		t.Fatalf("statements = %q, want the one valid notice", got)
	}
	for _, statement := range got {
		if !utf8.ValidString(statement) {
			t.Errorf("stored %q, which is not valid UTF-8", statement)
		}
	}
}

// A notice is a line of prose. A kilobyte of one is already generous, and the
// candidate is refused rather than shortened: a truncated notice is not the
// notice the licence said to reproduce.
func TestAnOversizedCandidateIsNotRecognized(t *testing.T) {
	long := "Copyright (c) 2026 " + strings.Repeat("A", maxCopyrightStatementBytes)
	if got := statementsOf(t, long+"\n"); len(got) != 0 {
		t.Fatalf("statements = %q, want none: the candidate is over the limit", got)
	}
}

func fileID(anchor domain.AnchorKey, rel string) domain.FileID {
	return domain.FileID{Anchor: anchor, RelPath: rel}
}

// The dedup key merges year ranges; the stored value keeps the years the file
// states. Both halves are asserted here, because a key that leaked into the
// output would satisfy neither MIT nor BSD.
func TestAYearRangeMergeAffectsOnlyTheDedupKey(t *testing.T) {
	statements := []domain.CopyrightStatement{
		{Text: "Copyright (c) 2019-2021 Acme Inc.", File: fileID("project", "a.c")},
		{Text: "Copyright (c) 2019, 2020, 2021 Acme Inc.", File: fileID("project", "b.c")},
		{Text: "Copyright (c) 2026 Acme Inc.", File: fileID("project", "c.c")},
		{Text: "Copyright (c) 2026 Other Holder", File: fileID("project", "d.c")},
	}
	kept := DedupeCopyright(statements)
	if len(kept) != 2 {
		t.Fatalf("kept %d statements, want 2 holders: %#v", len(kept), kept)
	}
	// The first in canonical file order wins, with its own years, unmerged and
	// unreformatted.
	if kept[0].Text != "Copyright (c) 2019-2021 Acme Inc." || kept[0].File.Canonical() != "project:a.c" {
		t.Errorf("kept[0] = %#v, want the statement of a.c as a.c states it", kept[0])
	}
	if kept[1].Text != "Copyright (c) 2026 Other Holder" {
		t.Errorf("kept[1] = %#v, want the second holder", kept[1])
	}
	for _, statement := range kept {
		// No merged range was invented, and no key was published.
		if strings.Contains(statement.Text, "2019-2026") || statement.Text != strings.TrimSpace(statement.Text) {
			t.Errorf("stored %q, which no file says", statement.Text)
		}
	}
}

// The REUSE tag and the conventional line are two spellings of one notice.
// They collapse, and which of them survives does not depend on the order a
// caller collected the files in -- which, for files gathered from a map, is no
// order at all.
func TestTheTagAndTheClassicLineCollapseDeterministically(t *testing.T) {
	statements := []domain.CopyrightStatement{
		{Text: "Copyright (c) 2026 Acme Inc.", File: fileID("project", "b.c")},
		{Text: "2019-2026 Acme Inc.", File: fileID("project", "a.c")},
		{Text: "Copyright © 2026 ACME INC.", File: fileID("project", "c.c")},
	}
	want := DedupeCopyright(statements)
	if len(want) != 1 || want[0].File.Canonical() != "project:a.c" {
		t.Fatalf("kept %#v, want the single entry of a.c", want)
	}

	source := rand.New(rand.NewSource(1))
	for run := 0; run < 50; run++ {
		shuffled := make([]domain.CopyrightStatement, len(statements))
		copy(shuffled, statements)
		source.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got := DedupeCopyright(shuffled)
		if len(got) != 1 || got[0] != want[0] {
			t.Fatalf("shuffled input kept %#v, want %#v", got, want[0])
		}
	}
}

// The key exists so that two spellings collapse, and it may never be seen.
func TestTheDedupKeyIsHolderOnly(t *testing.T) {
	for _, pair := range [][2]string{
		{"Copyright (c) 2026 Acme Inc.", "2026 Acme Inc."},
		{"Copyright (C) 1991, 1999 Acme Inc.", "copyright © 1991-1999 acme, inc"},
		{"(C) Copyright 2026 Acme Inc.", "Copyright Acme Inc."},
	} {
		if copyrightKey(pair[0]) != copyrightKey(pair[1]) {
			t.Errorf("keys differ: %q -> %q, %q -> %q", pair[0], copyrightKey(pair[0]), pair[1], copyrightKey(pair[1]))
		}
	}
	if copyrightKey("Copyright 2026 Acme Inc.") == copyrightKey("Copyright 2026 Other Inc.") {
		t.Error("two holders share a key")
	}
	if key := copyrightKey("Copyright (c) 2026 Acme Inc."); key != "acme inc" {
		t.Errorf("key = %q, want the holder alone", key)
	}
}

// The extractor reads a file out of somebody else's build tree, which section
// 30 calls untrusted input. Two properties hold for every input: it returns,
// and every statement it returns is a substring of the input -- which is what
// "verbatim" means operationally.
func FuzzCopyright(f *testing.F) {
	f.Add([]byte("/* Copyright (c) 2026 Acme Inc. */\n"))
	f.Add([]byte("SPDX-FileCopyrightText: 2026 Acme Inc.\n"))
	f.Add([]byte("Copyright [yyyy] [name of copyright owner]\n"))
	f.Add([]byte("copyright\ncopyright (c)\nCOPYRIGHT 1999-2026 \xff\xfe\n"))
	f.Add([]byte(strings.Repeat("Copyright (c) 2026 A\n", 100)))
	f.Add([]byte("* Copyright (C) 1991, 1999 Free Software Foundation, Inc."))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		statements := ExtractCopyright(data)
		for _, statement := range statements {
			if statement == "" {
				t.Fatalf("an empty statement was extracted from %q", data)
			}
			if !strings.Contains(string(data), statement) {
				t.Fatalf("statement %q is not a substring of %q", statement, data)
			}
			if len(statement) > maxCopyrightStatementBytes {
				t.Fatalf("statement of %d bytes is over the limit", len(statement))
			}
			if !utf8.ValidString(statement) {
				t.Fatalf("statement %q is not valid UTF-8", statement)
			}
			if copyrightKey(statement) == "" {
				t.Fatalf("statement %q normalizes to an empty key, so it names nobody", statement)
			}
		}
		// The key is a pure function of the text: two runs agree, and so do
		// two orders of the same statements.
		for _, statement := range statements {
			if copyrightKey(statement) != copyrightKey(statement) {
				t.Fatalf("the key of %q is not stable", statement)
			}
		}
	})
}
