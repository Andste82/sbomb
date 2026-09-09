package pkgmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// The .pc files these tests are written against are the ones on the machine
// this reader was written on: /usr/lib/x86_64-linux-gnu/pkgconfig/libxcrypt.pc
// and valgrind.pc, /usr/share/pkgconfig/systemd.pc, shared-mime-info.pc and
// xkeyboard-config.pc. Where a shape below looks arbitrary it was copied from
// one of them; where the format allows something none of them does -- a
// backslash continuation, a literal "$$" -- the test states this tool's
// assumption rather than pkg-config's behaviour, and D37 says so.

// writePkgConfigTree lays out a sysroot: the file that is used, and whatever
// .pc files the test wants beside it.
func writePkgConfigTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// describeInTree runs the reader over one file of a freshly written sysroot.
func describeInTree(t *testing.T, used string, files map[string]string) (string, SystemPackage, []domain.Finding) {
	t.Helper()
	root := t.TempDir()
	writePkgConfigTree(t, root, files)
	described, findings := NewPkgConfigReader().Describe(SystemFile{
		Path:     filepath.Join(root, filepath.FromSlash(used)),
		Boundary: root,
		Ref:      "sysroot:target/" + used,
	})
	return root, described, findings
}

func pkgConfigFindingIDs(findings []domain.Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.ID)
	}
	return ids
}

func versionOf(t *testing.T, described SystemPackage) string {
	t.Helper()
	for _, contribution := range described.Contributions {
		if contribution.Field == FieldVersion {
			if contribution.Claim.Rank != RankInstallState {
				t.Errorf("version rank = %d, want the installed state of section 21.1", contribution.Claim.Rank)
			}
			if contribution.Claim.Source != pkgConfigSource {
				t.Errorf("version source = %q, want %q", contribution.Claim.Source, pkgConfigSource)
			}
			return contribution.Claim.Value
		}
	}
	return ""
}

// The shape libxcrypt.pc has, reduced to what this reader uses.
const libfooPkgConfig = `# a comment, as libxcrypt.pc opens with one
prefix=/usr
exec_prefix=${prefix}
libdir=${prefix}/lib
includedir=${prefix}/include

Name: libfoo
Version: 1.2.3
Description: a library
Libs: -L${libdir} -lfoo
Cflags: -I${includedir}
`

func TestAPkgConfigFileNamesThePackageOfTheLibraryBesideIt(t *testing.T) {
	_, described, findings := describeInTree(t, "usr/lib/libfoo.so.3", map[string]string{
		"usr/lib/libfoo.so.3":         "ELF\n",
		"usr/lib/pkgconfig/libfoo.pc": libfooPkgConfig,
	})
	if described.Module != "libfoo" {
		t.Fatalf("module = %q, want the name of the file pkg-config keeps it under", described.Module)
	}
	if got := versionOf(t, described); got != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", got)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %v, want none for a file that answered", pkgConfigFindingIDs(findings))
	}
	// A .pc file states no licence, no supplier and no package ecosystem, so
	// exactly one contribution may come out of it.
	if len(described.Contributions) != 1 {
		t.Errorf("contributions = %d, want the version alone", len(described.Contributions))
	}
}

// systemd.pc chains root_prefix -> rootprefix -> prefix, so a value several
// levels deep is the ordinary case rather than an edge one.
func TestAChainOfVariablesResolvesTheWayASystemdPkgConfigFileWritesIt(t *testing.T) {
	_, described, _ := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
		"usr/lib/libfoo.so": "ELF\n",
		"usr/lib/pkgconfig/libfoo.pc": `root_prefix=/usr
rootprefix=${root_prefix}
exec_prefix=${rootprefix}
libdir=${exec_prefix}/lib

Name: libfoo
Version: 255
Libs: -L${libdir} -lfoo
`,
	})
	if described.Module != "libfoo" {
		t.Fatalf("module = %q, want libfoo", described.Module)
	}
	if got := versionOf(t, described); got != "255" {
		t.Errorf("version = %q, want 255", got)
	}
}

// The shape valgrind.pc has: several -l names, and a -L pointing into a
// subdirectory of libdir. Only the file that was really used draws the mapping;
// the other names on the line create nothing.
func TestOnlyTheLibraryThatWasUsedDrawsTheMapping(t *testing.T) {
	tree := map[string]string{
		"usr/lib/foo/libfoo.so": "ELF\n",
		"usr/lib/foo/libbar.so": "ELF\n",
		"usr/lib/pkgconfig/libfoo.pc": `prefix=/usr
libdir=${prefix}/lib

Name: foo
Version: 3.22.0
Libs: -L${libdir}/foo -lfoo -lbar -lgcc
Cflags: -I${prefix}/include/foo
`,
	}
	_, described, _ := describeInTree(t, "usr/lib/foo/libfoo.so", tree)
	if described.Module != "libfoo" {
		t.Fatalf("module = %q, want libfoo for the library that lies where the file says", described.Module)
	}
	// The second -l name is a library of the same package, and it is still not
	// found: the candidate .pc file is derived from the used file's own name,
	// and nothing in this tree is called bar.pc. Silence is the answer, not a
	// second component named after a flag.
	_, elsewhere, findings := describeInTree(t, "usr/lib/foo/libbar.so", tree)
	if elsewhere.Described() {
		t.Errorf("a library no .pc file is named after was mapped to %q", elsewhere.Module)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %v, want none", pkgConfigFindingIDs(findings))
	}
}

// A cross build's .pc file states the paths the package will have on the
// target, while every one of them really lies under the sysroot. Without the
// prefixing PKG_CONFIG_SYSROOT_DIR performs, nothing would ever match.
func TestAPathTheFileStatesIsReadThroughTheSysrootItLiesIn(t *testing.T) {
	root, described, _ := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
		"usr/lib/libfoo.so": "ELF\n",
		"usr/lib/pkgconfig/libfoo.pc": `libdir=/usr/lib

Name: libfoo
Version: 4.0
Libs: -L${libdir} -lfoo
`,
	})
	if described.Module != "libfoo" {
		t.Fatalf("module = %q, want libfoo; the sysroot %s was not prefixed onto libdir", described.Module, root)
	}
}

func TestAHeaderIsDescribedByThePackageOfTheDirectoryItSitsIn(t *testing.T) {
	_, described, _ := describeInTree(t, "usr/include/foo/foo.h", map[string]string{
		"usr/include/foo/foo.h": "#pragma once\n",
		"usr/share/pkgconfig/foo.pc": `prefix=/usr
includedir=${prefix}/include

Name: foo
Version: 2.4
Libs:
Cflags: -I${includedir}/foo
`,
	})
	if described.Module != "foo" {
		t.Fatalf("module = %q, want foo", described.Module)
	}
	if got := versionOf(t, described); got != "2.4" {
		t.Errorf("version = %q, want 2.4", got)
	}
}

// The absence of a .pc file is the ordinary case: most system files have none.
// It says nothing about the file and is not evidence anybody expected, so it is
// reported nowhere.
func TestASystemFileWithNoPkgConfigFileBesideItIsSilence(t *testing.T) {
	_, described, findings := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
		"usr/lib/libfoo.so": "ELF\n",
	})
	if described.Described() {
		t.Errorf("module = %q, want nothing", described.Module)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %v, want none for a file that is simply not there", pkgConfigFindingIDs(findings))
	}
}

func TestAPkgConfigFileWhoseStructureIsUnreadableIsReportedAndDescribesNothing(t *testing.T) {
	for name, content := range map[string]string{
		"a line that is neither a variable nor a key": "prefix=/usr\nthis line is neither\n",
		"no Name and no Version":                      "prefix=/usr\nlibdir=/usr/lib\n",
		"binary rubbish":                              "\x00\x01\x02\x03\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, described, findings := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
				"usr/lib/libfoo.so":           "ELF\n",
				"usr/lib/pkgconfig/libfoo.pc": content,
			})
			if described.Described() {
				t.Errorf("module = %q, want nothing from a file that could not be read", described.Module)
			}
			if ids := pkgConfigFindingIDs(findings); len(ids) != 1 || ids[0] != "EVIDENCE_UNREADABLE" {
				t.Fatalf("findings = %v, want one EVIDENCE_UNREADABLE", ids)
			}
			// The subject is the evidence, and it is named without the absolute
			// path of this machine.
			if ref := findings[0].Subject.Ref; ref != "usr/lib/pkgconfig/libfoo.pc" {
				t.Errorf("subject = %q, want the path relative to the anchor root", ref)
			}
		})
	}
}

func TestAPkgConfigFileOverTheByteLimitIsRefusedWhole(t *testing.T) {
	_, described, findings := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
		"usr/lib/libfoo.so":           "ELF\n",
		"usr/lib/pkgconfig/libfoo.pc": libfooPkgConfig + strings.Repeat("# padding\n", maxPkgConfigBytes/10+1),
	})
	if described.Described() {
		t.Errorf("module = %q, want nothing; a file over the bound is refused whole", described.Module)
	}
	if ids := pkgConfigFindingIDs(findings); len(ids) != 1 || ids[0] != "INPUT_LIMIT_EXCEEDED" {
		t.Fatalf("findings = %v, want one INPUT_LIMIT_EXCEEDED", ids)
	}
}

func TestAnExpansionThatCannotTerminateRefusesTheWholeFile(t *testing.T) {
	for name, definitions := range map[string]string{
		"a cycle": "a=${b}\nb=${a}\nlibdir=${a}\n",
		"a chain deeper than the bound": func() string {
			var out strings.Builder
			out.WriteString("v0=/usr/lib\n")
			last := maxPkgConfigExpansionDepth + 2
			for level := 1; level <= last; level++ {
				fmt.Fprintf(&out, "v%d=${v%d}\n", level, level-1)
			}
			fmt.Fprintf(&out, "libdir=${v%d}\n", last)
			return out.String()
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			_, described, findings := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
				"usr/lib/libfoo.so": "ELF\n",
				"usr/lib/pkgconfig/libfoo.pc": definitions +
					"\nName: libfoo\nVersion: 1.0\nLibs: -L${libdir} -lfoo\n",
			})
			if described.Described() {
				t.Errorf("module = %q, want nothing", described.Module)
			}
			if ids := pkgConfigFindingIDs(findings); len(ids) != 1 || ids[0] != "INPUT_LIMIT_EXCEEDED" {
				t.Fatalf("findings = %v, want one INPUT_LIMIT_EXCEEDED", ids)
			}
		})
	}
}

// A .pc file may name a variable the caller is expected to define on the
// command line. Half a directory is a directory that does not exist, so the
// value is unusable -- and a file waiting for an override is not a broken file,
// so nothing is reported.
func TestAValueNamingAnUndefinedVariableIsUnusableAndSilent(t *testing.T) {
	_, described, findings := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
		"usr/lib/libfoo.so": "ELF\n",
		"usr/lib/pkgconfig/libfoo.pc": `libdir=${nowhere}/lib

Name: libfoo
Version: 1.0
Libs: -L${libdir} -lfoo
`,
	})
	if described.Described() {
		t.Errorf("module = %q, want nothing from a directory that could not be resolved", described.Module)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %v, want none", pkgConfigFindingIDs(findings))
	}
}

func TestADirectoryThatClimbsOutOfTheAnchorIsRefused(t *testing.T) {
	_, described, findings := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
		"usr/lib/libfoo.so": "ELF\n",
		"usr/lib/pkgconfig/libfoo.pc": `libdir=/usr/lib/../../etc

Name: libfoo
Version: 1.0
Libs: -L/usr/lib/../../etc -lfoo
`,
	})
	if described.Described() {
		t.Errorf("module = %q, want nothing; section 30.3 refuses a path with a %q segment", described.Module, "..")
	}
	if len(findings) != 0 {
		t.Errorf("findings = %v, want none", pkgConfigFindingIDs(findings))
	}
}

// The rule of section 19.2: two statements are no statement. Both files verify
// against the library, and they say different things about it, so the file
// keeps the component its anchor gives it.
func TestTwoPkgConfigFilesThatDisagreeDescribeNothingAndAreReported(t *testing.T) {
	_, described, findings := describeInTree(t, "usr/lib/libcrypt.so", map[string]string{
		"usr/lib/libcrypt.so": "ELF\n",
		"usr/lib/pkgconfig/crypt.pc": `libdir=/usr/lib

Name: crypt
Version: 1.0
Libs: -L${libdir} -lcrypt
`,
		"usr/lib/pkgconfig/libcrypt.pc": `libdir=/usr/lib

Name: libxcrypt
Version: 4.4.36
Libs: -L${libdir} -lcrypt
`,
	})
	if described.Described() {
		t.Fatalf("module = %q, want nothing when two files disagree", described.Module)
	}
	if ids := pkgConfigFindingIDs(findings); len(ids) != 1 || ids[0] != "COMPONENT_MAPPING_CONFLICT" {
		t.Fatalf("findings = %v, want one COMPONENT_MAPPING_CONFLICT", ids)
	}
	if findings[0].Severity != domain.SeverityInfo {
		t.Errorf("severity = %q, want info", findings[0].Severity)
	}
	for _, want := range []string{"usr/lib/pkgconfig/crypt.pc", "usr/lib/pkgconfig/libcrypt.pc"} {
		if !strings.Contains(findings[0].Message, want) {
			t.Errorf("message %q does not name %q", findings[0].Message, want)
		}
	}
}

// The real libcrypt.pc is a symlink to libxcrypt.pc, and libxcrypt.pc is never
// a candidate for libcrypt.so: the candidates come from the library's own name
// and from nothing else. A sibling file describing another package is therefore
// not a disagreement and is never opened.
func TestASiblingPkgConfigFileForAnotherPackageIsNeverOpened(t *testing.T) {
	_, described, findings := describeInTree(t, "usr/lib/libcrypt.so", map[string]string{
		"usr/lib/libcrypt.so": "ELF\n",
		"usr/lib/pkgconfig/libcrypt.pc": `libdir=/usr/lib

Name: libxcrypt
Version: 4.4.36
Libs: -L${libdir} -lcrypt
`,
		"usr/lib/pkgconfig/libxcrypt.pc": `libdir=/usr/lib

Name: libxcrypt
Version: 9.9.9
Libs: -L${libdir} -lcrypt
`,
	})
	if described.Module != "libcrypt" {
		t.Fatalf("module = %q, want libcrypt", described.Module)
	}
	if got := versionOf(t, described); got != "4.4.36" {
		t.Errorf("version = %q, want the one the addressed file states", got)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %v, want none", pkgConfigFindingIDs(findings))
	}
}

// One package's .pc file can lie in either candidate directory, and a
// distribution that puts a copy in both has said one thing twice.
func TestTheSamePkgConfigFileInTwoCandidateDirectoriesIsOneAnswer(t *testing.T) {
	content := `libdir=/usr/lib

Name: libfoo
Version: 1.2.3
Libs: -L${libdir} -lfoo
`
	_, described, findings := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
		"usr/lib/libfoo.so":                 "ELF\n",
		"usr/lib/pkgconfig/libfoo.pc":       content,
		"usr/lib/share/pkgconfig/libfoo.pc": content,
	})
	if described.Module != "libfoo" {
		t.Fatalf("module = %q, want libfoo", described.Module)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %v, want none for two identical answers", pkgConfigFindingIDs(findings))
	}
}

// Verification is what separates reading a name from asserting a mapping.
func TestAPkgConfigFileThatDoesNotDescribeTheFileMapsNothing(t *testing.T) {
	for name, content := range map[string]string{
		// The shared-mime-info.pc shape: a package with no library at all.
		"an empty Libs line": "libdir=/usr/lib\n\nName: libfoo\nVersion: 2.4\nLibs:\nCflags:\n",
		"another library":    "libdir=/usr/lib\n\nName: libfoo\nVersion: 1.0\nLibs: -L${libdir} -lbar\n",
		"another directory":  "libdir=/opt/foo/lib\n\nName: libfoo\nVersion: 1.0\nLibs: -L${libdir} -lfoo\n",
		"a longer name":      "libdir=/usr/lib\n\nName: libfoo\nVersion: 1.0\nLibs: -L${libdir} -lfoobar\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, described, findings := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
				"usr/lib/libfoo.so":           "ELF\n",
				"usr/lib/pkgconfig/libfoo.pc": content,
			})
			if described.Described() {
				t.Errorf("module = %q, want nothing", described.Module)
			}
			if len(findings) != 0 {
				t.Errorf("findings = %v, want none; a name that does not verify is an answer, not a fault",
					pkgConfigFindingIDs(findings))
			}
		})
	}
}

// Without an anchor there is no bound on the upward walk, and section 22.1 does
// not allow an unbounded one.
func TestAFileWithNoAnchorBoundaryIsNotDescribed(t *testing.T) {
	root := t.TempDir()
	writePkgConfigTree(t, root, map[string]string{
		"usr/lib/libfoo.so":           "ELF\n",
		"usr/lib/pkgconfig/libfoo.pc": libfooPkgConfig,
	})
	described, findings := NewPkgConfigReader().Describe(SystemFile{
		Path: filepath.Join(root, "usr", "lib", "libfoo.so"),
		Ref:  "abs:whatever",
	})
	if described.Described() || len(findings) != 0 {
		t.Errorf("module = %q with findings %v, want nothing at all", described.Module, pkgConfigFindingIDs(findings))
	}
}

// An include directory holds hundreds of headers that all derive the same
// candidate. Reading it once per header would be the cost section 31 bounds,
// and reporting it once per header would be a report nobody can read.
func TestABrokenPkgConfigFileIsReadAndReportedOnceHoweverManyFilesAskForIt(t *testing.T) {
	root := t.TempDir()
	writePkgConfigTree(t, root, map[string]string{
		"usr/include/foo/a.h":        "#pragma once\n",
		"usr/include/foo/b.h":        "#pragma once\n",
		"usr/share/pkgconfig/foo.pc": "prefix=/usr\nthis line is neither\n",
	})
	reader := NewPkgConfigReader()
	reported := 0
	for _, header := range []string{"a.h", "b.h"} {
		_, findings := reader.Describe(SystemFile{
			Path:     filepath.Join(root, "usr", "include", "foo", header),
			Boundary: root,
			Ref:      "sysroot:target/usr/include/foo/" + header,
		})
		reported += len(findings)
	}
	if reported != 1 {
		t.Errorf("%d finding(s) for one broken file, want exactly one", reported)
	}
}

// The flags of a Libs or Cflags line come in two spellings, and both are read.
// Nothing in the corpus this reader was written against uses the separated one,
// so this pins the assumption rather than pkg-config's behaviour.
func TestAFlagIsReadInBothItsSpellings(t *testing.T) {
	for name, libs := range map[string]string{
		"attached":  "Libs: -L${libdir} -lfoo\n",
		"separated": "Libs: -L ${libdir} -l foo\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, described, _ := describeInTree(t, "usr/lib/libfoo.so", map[string]string{
				"usr/lib/libfoo.so":           "ELF\n",
				"usr/lib/pkgconfig/libfoo.pc": "libdir=/usr/lib\n\nName: libfoo\nVersion: 1.0\n" + libs,
			})
			if described.Module != "libfoo" {
				t.Errorf("module = %q, want libfoo", described.Module)
			}
		})
	}
}

// The candidates are derived from the used file's own name and from nothing
// else. This is the guarantee that no directory is ever listed.
func TestTheCandidateModulesComeFromTheFileName(t *testing.T) {
	for path, want := range map[string][]string{
		"/sysroot/usr/lib/libfoo.so.3":     {"foo", "libfoo"},
		"/sysroot/usr/lib/libfoo.a":        {"foo", "libfoo"},
		"/sysroot/usr/bin/foo.dll":         {"foo", "libfoo"},
		"/sysroot/usr/include/foo/bar.h":   {"foo", "libfoo", "bar", "libbar"},
		"/sysroot/usr/include/zlib.h":      {"zlib", "libzlib"},
		"/sysroot/usr/share/doc/readme.md": nil,
	} {
		got := pkgConfigModuleCandidates(filepath.FromSlash(path))
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("candidates for %s = %v, want %v", path, got, want)
		}
	}
}
