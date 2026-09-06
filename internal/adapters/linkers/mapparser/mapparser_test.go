package mapparser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func corpusMap(t *testing.T, toolchain, project, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "testdata", "fixtures", toolchain, project, "build", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func parseCorpus(t *testing.T, toolchain, project, name string) Result {
	t.Helper()
	text := corpusMap(t, toolchain, project, name)
	format := Sniff(text)
	if format == "" {
		t.Fatalf("%s/%s: map format not recognized", toolchain, project)
	}
	result := Parse(strings.NewReader(text), format)
	if result.Err != nil {
		t.Fatalf("%s/%s: %v", toolchain, project, result.Err)
	}
	return result
}

func members(result Result) []string {
	var out []string
	for _, record := range result.Records {
		if record.Kind == ArchiveMember {
			out = append(out, record.Path)
		}
	}
	return out
}

// TestOnlyExtractedMembersAreReported is the property the SBOM depends on:
// p02-static compiles crypto.c and unused.c into one archive, and the linker
// extracts only crypto.c.o. Reporting unused.c.o would put a file in the SBOM
// that demonstrably never reached the binary (specification section 12).
func TestOnlyExtractedMembersAreReported(t *testing.T) {
	for _, toolchain := range []string{"gcc-ninja", "gcc-make", "clang-ninja", "arm-none-eabi"} {
		result := parseCorpus(t, toolchain, "p02-static", mapName(t, toolchain))
		got := members(result)
		if len(got) != 1 {
			t.Errorf("%s: got %d archive members, want exactly one: %v", toolchain, len(got), got)
			continue
		}
		if !strings.HasPrefix(got[0], "libcrypto.a(crypto.c.o") {
			t.Errorf("%s: extracted member = %q, want libcrypto.a(crypto.c.o...)", toolchain, got[0])
		}
		for _, member := range got {
			if strings.Contains(member, "unused") {
				t.Errorf("%s: unused.c.o was reported although the linker never extracted it", toolchain)
			}
		}
	}
}

func mapName(t *testing.T, toolchain string) string {
	t.Helper()
	if toolchain == "mingw-w64" {
		return "app.exe.map"
	}
	return "app.map"
}

// TestLinkerScriptWildcardsAreNotArchiveMembers guards the defect the real
// corpus exposed: a context-free scan reads *crtbegin.o(.ctors) as the member
// ".ctors" of the archive "*crtbegin.o".
func TestLinkerScriptWildcardsAreNotArchiveMembers(t *testing.T) {
	result := parseCorpus(t, "gcc-ninja", "p02-static", "app.map")
	for _, record := range result.Records {
		if strings.ContainsAny(record.Path, "*?") {
			t.Errorf("a linker script wildcard was recorded as evidence: %+v", record)
		}
		if record.Kind == ArchiveMember && strings.HasPrefix(record.Member, ".") {
			t.Errorf("a section name was recorded as an archive member: %+v", record)
		}
	}
}

// TestLLDSectionPlacementsAreNotArchiveMembers guards the same defect in the
// other direction: lld writes "foo.o:(.text)" in its input column.
func TestLLDSectionPlacementsAreNotArchiveMembers(t *testing.T) {
	result := parseCorpus(t, "clang-ninja", "p02-static", "app.map")
	for _, record := range result.Records {
		if strings.Contains(record.Path, ":(") {
			t.Errorf("an lld section placement leaked into the records: %+v", record)
		}
	}
	if got := len(members(result)); got != 1 {
		t.Errorf("lld map yielded %d archive members, want 1", got)
	}
}

func TestRecordsAreDeduplicated(t *testing.T) {
	// A member is named once per section it contributes; the records must not
	// repeat it.
	result := parseCorpus(t, "gcc-ninja", "p02-static", "app.map")
	seen := map[string]int{}
	for _, record := range result.Records {
		seen[string(record.Kind)+" "+record.Path]++
	}
	for key, count := range seen {
		if count > 1 {
			t.Errorf("record %q appears %d times", key, count)
		}
	}
}

func TestDirectLinkInputsComeFromLoadLines(t *testing.T) {
	result := parseCorpus(t, "gcc-ninja", "p02-static", "app.map")
	var sawObject, sawArchive bool
	for _, record := range result.Records {
		if record.Kind == LinkedObject && strings.HasSuffix(record.Path, "main.c.o") {
			sawObject = true
		}
		if record.Kind == StaticArchive && record.Path == "libcrypto.a" {
			sawArchive = true
		}
	}
	if !sawObject {
		t.Error("the project's own object is not among the direct link inputs")
	}
	if !sawArchive {
		t.Error("libcrypto.a is not among the direct link inputs")
	}
}

func TestStaticallyLinkedRuntimeYieldsManyMembers(t *testing.T) {
	// mingw links the C runtime statically, so many members really are
	// extracted. A parser that under-reports would be as wrong as one that
	// over-reports.
	result := parseCorpus(t, "mingw-w64", "p02-static", "app.exe.map")
	got := members(result)
	if len(got) < 10 {
		t.Errorf("got %d archive members from the mingw map, want the statically linked runtime", len(got))
	}
	var sawProject bool
	for _, member := range got {
		if strings.HasPrefix(member, "libcrypto.a(") {
			sawProject = true
		}
	}
	if !sawProject {
		t.Error("the project's own archive member is missing among the runtime members")
	}
}

func TestSniffDistinguishesFormats(t *testing.T) {
	cases := map[string]string{
		"gcc-ninja":     FormatGNULD,
		"clang-ninja":   FormatLLD,
		"arm-none-eabi": FormatGNULD,
	}
	for toolchain, want := range cases {
		if got := Sniff(corpusMap(t, toolchain, "p02-static", "app.map")); got != want {
			t.Errorf("Sniff(%s) = %q, want %q", toolchain, got, want)
		}
	}
	if got := Sniff("nothing recognizable here"); got != "" {
		t.Errorf("Sniff(garbage) = %q, want the empty string", got)
	}
}

func TestDiscardedSectionsAreRecorded(t *testing.T) {
	result := parseCorpus(t, "gcc-ninja", "p02-static", "app.map")
	var discarded int
	for _, record := range result.Records {
		if record.Kind == DiscardedSection {
			discarded++
		}
	}
	if discarded == 0 {
		t.Error("no discarded input sections were recorded although the map lists them")
	}
}

func TestEmptyInputIsMalformed(t *testing.T) {
	result := Parse(strings.NewReader(""), FormatGNULD)
	if result.Err == nil {
		t.Fatal("an empty map should be reported as malformed")
	}
}

func TestTruncatedMapReturnsWhatWasParsed(t *testing.T) {
	text := corpusMap(t, "gcc-ninja", "p02-static", "app.map")
	// Cut mid-file, after the archive member block.
	truncated := text[:len(text)/2]
	result := Parse(strings.NewReader(truncated), FormatGNULD)
	if len(result.Records) == 0 {
		t.Fatal("a truncated map should still yield the records parsed before the cut")
	}
	if got := members(result); len(got) != 1 {
		t.Errorf("truncated map yielded %d members, want 1", len(got))
	}
}

func TestOverlongLineIsRejected(t *testing.T) {
	long := "LOAD " + strings.Repeat("a", MaxLineLength+1) + ".o"
	result := Parse(strings.NewReader("Linker script and memory map\n"+long), FormatGNULD)
	if result.Err == nil {
		t.Fatal("an overlong line should be rejected")
	}
}
