package license

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/example/sbomb/internal/domain"
)

// Copyright statement extraction, section 22.10.
//
// For MIT and the BSD family the copyright line is not decoration: the licence
// says the notice shall be reproduced, and the holder is named in the notice
// rather than in the identifier. A reformatted year range and a normalized
// holder are both alterations of a notice somebody is obliged to reproduce, so
// what is stored here is the statement as the file states it and nothing else.
//
// Two forms are recognized and no more -- the machine-readable REUSE tag and
// the conventional line -- because every further form is a guess about what a
// line means. Both expressions are RE2 (section 30.6 forbids backtracking),
// which Go's regexp package is by construction.

const (
	// CopyrightWindow is how far into a file a statement is looked for: the
	// first 64 KiB, which is the window section 22.2 rule 2 already uses for
	// SPDX identifiers. A notice belongs at the top of a file; scanning a
	// whole translation unit for one would buy nothing and cost section 31.
	CopyrightWindow = 64 << 10

	// MaxCopyrightStatements is the per-component limit of section 22.10.
	MaxCopyrightStatements = 200

	// maxCopyrightStatementBytes bounds one recognized statement. It is the
	// section 30 point 9 bound on untrusted input applied to a line: a
	// notice is a line of prose, and 1 KiB of it is already generous. A
	// longer candidate is not recognized rather than shortened, because a
	// truncated notice is not the notice the licence said to reproduce.
	maxCopyrightStatementBytes = 1 << 10
)

var (
	// commentLeader is the scaffolding a notice sits behind in a source file.
	// It is removed so that the statement can be required to *begin* the line
	// (see classicNotice); it is never part of the stored value, which starts
	// where the notice starts.
	commentLeader = regexp.MustCompile(`^[ \t]*(?:/\*+|\*+/?|//+|#+|;+|--+|<!--|%+|!+)?[ \t]*`)

	// spdxCopyrightTag is form 1: the REUSE 3.3 and SPDX tag. What follows the
	// colon is the statement.
	spdxCopyrightTag = regexp.MustCompile(`(?i)SPDX-FileCopyrightText:[ \t]*`)

	// classicNotice is form 2: an optional (c)/(C)/© marker, the word
	// Copyright, an optional marker, an optional year or year range, and a
	// holder -- which is the submatch, and which has to exist.
	//
	// It is anchored at the start of the line, after the comment leader, and
	// that is the one place this deviates from "a line containing Copyright"
	// (deviation D43). A licence text says the word a dozen times in its own
	// prose -- "reproduce the above copyright notice", "the name of the
	// copyright holder" -- and none of those are notices. Requiring the
	// statement to begin the line is a structural rule about where a notice
	// sits, not a keyword heuristic about what the rest of the line says.
	classicNotice = regexp.MustCompile(`(?i)^(?:\(c\)[ \t]*|©[ \t]*)?copyrights?\b[ \t]*(?:\(c\)|©)?[ \t]*(?:[0-9]{4}(?:[ \t]*[-–—,][ \t]*[0-9]{2,4})*[ \t]*,?[ \t]*)*(.*)$`)

	// holderText is what makes a candidate a notice rather than the word on
	// its own: something is named.
	holderText = regexp.MustCompile(`[\pL\pN]`)

	// copyrightWord locates the word itself, so that its case can be read. A
	// notice is written with a capital; a wrapped line of licence prose is not.
	copyrightWord = regexp.MustCompile(`(?i)copyrights?`)

	// placeholderGroup is the bracketed form a licence's own "how to apply
	// this licence" appendix uses where the holder belongs:
	// `Copyright [yyyy] [name of copyright owner]` in Apache-2.0,
	// `Copyright (C) <year> <name of author>` in the GNU licences. A holder
	// that is nothing but one of these is not a holder -- the line is the
	// licence telling somebody to write a notice, not a notice -- and every
	// Apache-2.0 file in the world carries it, so recognizing it would put a
	// placeholder into the attribution of every Apache-2.0 component. This is
	// not a keyword heuristic: it is the same declared-variable form section
	// 22.3 technique 4 already reads out of the SPDX templates.
	placeholderGroup = regexp.MustCompile(`\[[^\]]*\]|<[^>]*>|\{[^}]*\}`)

	// commentTerminator closes the scaffolding the leader opened. Removing it
	// is not a rewrite of the statement: it is not part of the statement.
	commentTerminator = regexp.MustCompile(`[ \t]*(?:\*+/|-->|\*+)[ \t]*$`)

	// leadingCopyrightWord and yearBlock strip from the *dedup key* what two
	// spellings of one notice may differ in. Neither ever touches a stored
	// value.
	leadingCopyrightWord = regexp.MustCompile(`(?i)^[\s]*(?:\(c\)|©)?[\s]*copyrights?\b[\s]*:?[\s]*(?:\(c\)|©)?`)
	yearBlock            = regexp.MustCompile(`[0-9]{4}(?:[\s]*[-–—,][\s]*[0-9]{2,4})*`)
	nonKeyRune           = regexp.MustCompile(`[^\pL\pN]+`)
)

// ExtractCopyright returns the copyright statements of one file, in the order
// they appear in it. The value returned for each is a verbatim substring of
// the file: nothing is reformatted, no year range is merged, no holder is
// normalized.
//
// The input is the bytes a reader already has -- the hashing pass of section
// 23 for a used file, the retained bytes of section 22.9 for an artifact -- so
// extraction opens nothing and adds no I/O pass (decision Q9).
func ExtractCopyright(data []byte) []string {
	window := copyrightSearchWindow(data)
	if len(window) == 0 {
		return nil
	}
	// Nothing below can match a window that does not carry the word at all,
	// and most files do not. Three regular expressions per line over 64 KiB is
	// the cost this avoids: section 31 budgets 15 seconds for a whole run, and
	// the scan below is a byte comparison that stops at the first hit.
	if !containsFold(window, "copyright") {
		return nil
	}
	var found []string
	previous := ""
	for _, line := range strings.Split(window, "\n") {
		// The line above is carried rather than the answer about it: whether
		// it finished its sentence is asked only where a lowercase word made
		// the question matter, which is a handful of lines in a corpus rather
		// than every line of every file.
		if statement, ok := copyrightInLine(line, previous); ok {
			found = append(found, statement)
		}
		previous = line
	}
	return found
}

// continuesASentence reports whether the next line carries on the sentence this
// one started. A licence text wraps, and a wrap can put the word at the start
// of a line: the ISC text ends a line with "provided that the above" and
// begins the next with "copyright notice and this permission notice appear in
// all copies." Section 22.10 asks for a notice, and that is prose.
//
// The question is asked structurally rather than by looking for words: a line
// that ends a sentence, or ends nothing at all, cannot be continued. Only a
// *lowercase* word is judged by it, so a notice written at the start of a line
// in the ordinary way is never affected by what stands above it.
func continuesASentence(line string) bool {
	body := strings.TrimRight(line[len(commentLeader.FindString(line)):], " \t\r")
	body = commentTerminator.ReplaceAllString(body, "")
	body = strings.TrimRight(body, " \t")
	if body == "" {
		return false
	}
	switch body[len(body)-1] {
	case '.', ':', ';', '!', '?':
		return false
	}
	// A comma ends nothing, so the line below it is still the same sentence.
	return true
}

// containsFold reports whether text holds needle, compared without case and
// without allocating: lowering a 64 KiB window to ask one question would cost
// a copy of every file the tool hashes. needle must already be lowercase.
func containsFold(text, needle string) bool {
	for i := 0; i+len(needle) <= len(text); i++ {
		if lowerASCII(text[i]) != needle[0] {
			continue
		}
		matched := true
		for j := 1; j < len(needle); j++ {
			if lowerASCII(text[i+j]) != needle[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// copyrightSearchWindow is the first CopyrightWindow bytes, cut back to the
// last line break inside it. A window that ends mid-line would offer a
// truncated statement, and a truncated notice is not the notice.
func copyrightSearchWindow(data []byte) string {
	if len(data) <= CopyrightWindow {
		return string(data)
	}
	window := data[:CopyrightWindow]
	// bytes rather than strings: converting the window to ask where its last
	// line break is would copy 64 KiB of every large file for one index.
	if cut := bytes.LastIndexByte(window, '\n'); cut >= 0 {
		return string(window[:cut])
	}
	return ""
}

// copyrightInLine recognizes the two forms of section 22.10 in one line and
// returns the statement, which begins where the notice begins.
func copyrightInLine(line, previous string) (string, bool) {
	// Both forms carry the word -- `SPDX-FileCopyrightText:` holds it too --
	// so a line without it cannot match either, and asking three regular
	// expressions is the cost this avoids on nearly every line of nearly every
	// file.
	if !containsFold(line, "copyright") {
		return "", false
	}
	line = strings.TrimRight(line, " \t\r")
	if tag := spdxCopyrightTag.FindStringIndex(line); tag != nil {
		// Form 1. The statement is what the tag introduces; the tag itself is
		// the marker that a statement follows, not part of it. Everything
		// after it is the holder, so the statement and the holder are checked
		// as one.
		statement, ok := storableStatement(line[tag[1]:])
		if !ok || !namesAHolder(statement) {
			return "", false
		}
		// SPDX and REUSE reserve two values for "there is no copyright to
		// state" and "it was not established". Storing either as a statement
		// publishes the absence of a notice as a notice, and it suppresses the
		// FOSS_COPYRIGHT_MISSING that says the attribution is incomplete.
		switch strings.TrimSpace(statement) {
		case "NONE", "NOASSERTION":
			return "", false
		}
		return statement, true
	}
	body := line[len(commentLeader.FindString(line)):]
	match := classicNotice.FindStringSubmatchIndex(body)
	if match == nil {
		return "", false
	}
	// A lowercase word carrying on the sentence above it is wrapped prose, not
	// a notice. Every convention writes a notice with a capital -- `Copyright`,
	// `COPYRIGHT` -- and a licence's own conditions are the text that wraps.
	if word := copyrightWord.FindStringIndex(body); word != nil && body[word[0]] == 'c' &&
		continuesASentence(previous) {
		return "", false
	}
	// Submatch 1 is the holder. A line that is the word alone, or the word
	// with a placeholder where the holder belongs, names nobody.
	if !namesAHolder(body[match[2]:match[3]]) {
		return "", false
	}
	return storableStatement(body)
}

// namesAHolder says whether a holder was actually stated. REUSE 3.3 requires
// the holder and only recommends the year, so this is the one part of a
// notice that has to be there.
//
// It asks the same question deduplication asks, and deliberately the same way:
// what is left of the holder once the placeholders and the years are gone has
// to name something. A candidate that survived here and normalized to nothing
// would be stored and then silently dropped as a duplicate of every other
// empty key.
func namesAHolder(holder string) bool {
	return copyrightKey(placeholderGroup.ReplaceAllString(holder, " ")) != ""
}

// storableStatement is the last gate before a statement is kept: the comment
// scaffolding at the end of the line is dropped, and what remains has to be
// storable without being changed.
func storableStatement(statement string) (string, bool) {
	statement = commentTerminator.ReplaceAllString(strings.TrimRight(statement, " \t\r"), "")
	statement = strings.TrimRight(statement, " \t")
	if statement == "" || !holderText.MatchString(statement) {
		return "", false
	}
	if len(statement) > maxCopyrightStatementBytes {
		return "", false
	}
	// A file of a build tree is untrusted input (section 30) and need not be
	// UTF-8. A statement that is not cannot be carried into a JSON document
	// unchanged -- the encoder would substitute U+FFFD for every invalid byte,
	// and a notice with substituted bytes is not verbatim. So it is not
	// recognized, rather than being stored as something the file does not say.
	if !utf8.ValidString(statement) {
		return "", false
	}
	return statement, true
}

// copyrightKey is what deduplication compares. It is never stored and never
// displayed: it exists so that two spellings of one notice -- the REUSE tag
// and the conventional line, "(c)" and "©", one year and a range of years --
// collapse to a single entry while the entry that survives is still the
// original text of a file.
func copyrightKey(statement string) string {
	key := strings.ToLower(statement)
	key = leadingCopyrightWord.ReplaceAllString(key, " ")
	key = yearBlock.ReplaceAllString(key, " ")
	key = nonKeyRune.ReplaceAllString(key, " ")
	return strings.TrimSpace(key)
}

// DedupeCopyright collapses the statements of one component that say the same
// thing and returns what is left, ordered by text.
//
// Among duplicates the entry kept is the first in canonical file order, so the
// answer does not depend on the order the caller happened to collect them in
// -- which for a set of files gathered from a map is no order at all. Within
// one file the order is the order of appearance, which is the order
// ExtractCopyright returns.
func DedupeCopyright(statements []domain.CopyrightStatement) []domain.CopyrightStatement {
	if len(statements) == 0 {
		return nil
	}
	ordered := make([]domain.CopyrightStatement, len(statements))
	copy(ordered, statements)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].File.Canonical() < ordered[j].File.Canonical()
	})

	seen := make(map[string]bool, len(ordered))
	kept := make([]domain.CopyrightStatement, 0, len(ordered))
	for _, statement := range ordered {
		key := copyrightKey(statement.Text)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, statement)
	}
	// Section 29: evidence.copyright[] is ordered by text, and the stored
	// order is the same one, so that a truncation at the limit takes the same
	// entries whatever order the files were read in.
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].Text != kept[j].Text {
			return kept[i].Text < kept[j].Text
		}
		return kept[i].File.Canonical() < kept[j].File.Canonical()
	})
	return kept
}
