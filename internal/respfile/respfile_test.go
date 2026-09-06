package respfile

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func memory(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		content, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("no such file: %s", path)
		}
		return []byte(content), nil
	}
}

// The property that matters: a command line behind a response file must parse
// exactly like the expanded one.
func TestExpansionIsIndistinguishableFromTheExpandedLine(t *testing.T) {
	got, err := Expand([]string{"ld", "-o", "app", "@objects.rsp", "-lm"}, Options{
		ReadFile: memory(map[string]string{"objects.rsp": "a.o b.o\nsub/c.o\n"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ld", "-o", "app", "a.o", "b.o", "sub/c.o", "-lm"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Expand() = %v, want %v", got, want)
	}
}

func TestNestedResponseFilesAreFollowed(t *testing.T) {
	got, err := Expand([]string{"@outer.rsp"}, Options{
		ReadFile: memory(map[string]string{
			"outer.rsp": "-o app @inner.rsp",
			"inner.rsp": "a.o b.o",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "-o app a.o b.o" {
		t.Errorf("Expand() = %v", got)
	}
}

func TestADepthLimitStopsACycle(t *testing.T) {
	_, err := Expand([]string{"@loop.rsp"}, Options{
		ReadFile: memory(map[string]string{"loop.rsp": "@loop.rsp"}),
	})
	if !errors.Is(err, ErrDepthExceeded) {
		t.Errorf("err = %v, want ErrDepthExceeded", err)
	}
}

func TestTheSizeLimitIsEnforced(t *testing.T) {
	_, err := Expand([]string{"@big.rsp"}, Options{
		MaxBytes: 8,
		ReadFile: memory(map[string]string{"big.rsp": strings.Repeat("a.o ", 100)}),
	})
	if !errors.Is(err, ErrSizeExceeded) {
		t.Errorf("err = %v, want ErrSizeExceeded", err)
	}
}

func TestAnUnreadableResponseFileKeepsTheReference(t *testing.T) {
	// Dropping it would silently shorten the link line; keeping it lets the
	// caller report evidence it could not read.
	got, err := Expand([]string{"@missing.rsp", "a.o"}, Options{ReadFile: memory(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "@missing.rsp a.o" {
		t.Errorf("Expand() = %v", got)
	}
}

func TestGNUQuotingRules(t *testing.T) {
	cases := map[string][]string{
		`a.o "with space.o" b.o`: {"a.o", "with space.o", "b.o"},
		`with\ space.o`:          {"with space.o"},
		`'single quoted.o'`:      {"single quoted.o"},
		"a.o\n\tb.o\r\nc.o":      {"a.o", "b.o", "c.o"},
		`-DNAME=\"value\"`:       {`-DNAME="value"`},
		`""`:                     {""},
	}
	for input, want := range cases {
		got := Tokenize(input, GNU)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("Tokenize(%q, GNU) = %v, want %v", input, got, want)
		}
	}
}

// The reason the two tokenizers exist: under GNU rules a Windows path loses
// its separators, because every backslash is read as an escape.
func TestMSVCQuotingKeepsWindowsPaths(t *testing.T) {
	input := `"C:\build\obj\main.obj" C:\build\obj\util.obj`
	got := Tokenize(input, MSVC)
	want := []string{`C:\build\obj\main.obj`, `C:\build\obj\util.obj`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Tokenize(MSVC) = %v, want %v", got, want)
	}
	if under := Tokenize(input, GNU); under[0] == want[0] {
		t.Error("the GNU tokenizer preserved backslashes; then the two rule sets would be the same")
	}
}

func TestMSVCEscapedQuoteAndBackslashRuns(t *testing.T) {
	cases := map[string][]string{
		`a\\"b c"`:      {`a\b c`},
		`"say \"hi\""`:  {`say "hi"`},
		`C:\dir\\ next`: {`C:\dir\\`, "next"},
	}
	for input, want := range cases {
		got := Tokenize(input, MSVC)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("Tokenize(%q, MSVC) = %v, want %v", input, got, want)
		}
	}
}

func TestQuotingIsChosenByTheToolchainNotTheHost(t *testing.T) {
	for _, compiler := range []string{"cl", "cl.exe", "C:\\VS\\link.exe", "lld-link", "clang-cl"} {
		if QuotingForCompiler(compiler) != MSVC {
			t.Errorf("QuotingForCompiler(%q) = GNU, want MSVC", compiler)
		}
	}
	for _, compiler := range []string{"gcc", "/usr/bin/cc", "clang++", "arm-none-eabi-gcc", "x86_64-w64-mingw32-gcc"} {
		if QuotingForCompiler(compiler) != GNU {
			t.Errorf("QuotingForCompiler(%q) = MSVC, want GNU", compiler)
		}
	}
}
