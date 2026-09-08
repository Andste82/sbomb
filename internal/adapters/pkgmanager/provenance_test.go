package pkgmanager

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/exec"
)

// standInGit puts a program called git on PATH that answers the three
// questions section 9.2 allows, so the introspection path is driven by the
// answers git gives rather than by git itself. A unit test that built a real
// repository would depend on whichever git the machine happens to carry; the
// e2e suite is where a real one belongs.
func standInGit(t *testing.T, described, commit string) *exec.Runner {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"# $1 $2 are -C <dir>; $3 is the subcommand.\n" +
		"case \"$3\" in\n" +
		"  describe) printf '%s\\n' '" + described + "' ;;\n" +
		"  rev-parse) printf '%s\\n' '" + commit + "' ;;\n" +
		"  *) exit 1 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &exec.Runner{Features: exec.Features{Git: true}}
}

// claimsOf lists the four metadata claims of a package with the field each one
// belongs to, so an invariant can be stated about all of them at once.
func claimsOf(found Package) map[Field]Claim {
	return map[Field]Claim{
		FieldVersion:  found.Version,
		FieldLicense:  found.License,
		FieldSupplier: found.Supplier,
		FieldPURL:     found.PURL,
	}
}

// assertNoOriginWithoutAValue is the invariant that makes an origin worth
// publishing: a claim that names where it came from must have something to
// say. The opposite shape would put identity evidence in the document for a
// version nobody stated.
func assertNoOriginWithoutAValue(t *testing.T, packages []Package) {
	t.Helper()
	for _, found := range packages {
		for field, claim := range claimsOf(found) {
			if claim.Value == "" && (claim.Source != "" || claim.Rank != RankNone) {
				t.Errorf("%s: the %s claim names origin %q at rank %d but no value",
					found.Name, field, claim.Source, claim.Rank)
			}
		}
	}
}

// findingsWithID is the local reading of a findings slice, so a test can name
// the report it means instead of walking the slice itself.
func findingsWithID(findings []domain.Finding, id string) []domain.Finding {
	out := make([]domain.Finding, 0, 1)
	for _, finding := range findings {
		if finding.ID == id {
			out = append(out, finding)
		}
	}
	return out
}

// writeTestFile writes one file and the directories above it, so a case can
// lay out the evidence it needs in a line.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The populate script says which revision was asked for and the checkout says
// which one is there, so the checkout wins -- and the tag it displaced is kept,
// because reporting the disagreement is the next thing to be done with it.
// Where the checkout answers nothing usable it is no origin at all, and the
// declaration it could not improve on has to survive intact.
func TestTheCheckoutOutranksTheDeclaredTag(t *testing.T) {
	cases := []struct {
		name           string
		described      string
		wantVersion    string
		wantSource     string
		wantConfidence domain.Confidence
		wantDisplaced  string
		wantDirty      bool
	}{{
		name:      "an exact tag replaces the one that was asked for",
		described: "v1.3.0", wantVersion: "1.3.0", wantSource: "git-describe",
		wantConfidence: domain.ConfidenceHigh, wantDisplaced: "1.2.0",
	}, {
		name:      "a describe with distance outranks the declaration and is rated lower",
		described: "v1.3.0-4-gdeadbee", wantVersion: "1.3.0-4-gdeadbee", wantSource: "git-describe",
		wantConfidence: domain.ConfidenceMedium, wantDisplaced: "1.2.0",
	}, {
		name:      "a modified checkout outranks it too",
		described: "v1.3.0-dirty", wantVersion: "1.3.0", wantSource: "git-describe",
		wantConfidence: domain.ConfidenceMedium, wantDisplaced: "1.2.0", wantDirty: true,
	}, {
		// This is the case the ranking was needed for. A tag literally named
		// "v" trims away to nothing, and overwriting the declaration with it
		// destroyed the only version that was known while still publishing an
		// origin for it.
		name:      "an answer that trims away to nothing leaves the declaration standing",
		described: "v", wantVersion: "1.2.0", wantSource: "fetchcontent",
		wantConfidence: domain.ConfidenceHigh,
	}, {
		name:      "a bare dirty marker names no revision either",
		described: "-dirty", wantVersion: "1.2.0", wantSource: "fetchcontent",
		wantConfidence: domain.ConfidenceHigh, wantDirty: true,
	}, {
		name:      "an empty answer is no answer",
		described: "", wantVersion: "1.2.0", wantSource: "fetchcontent",
		wantConfidence: domain.ConfidenceHigh,
	}}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			build := t.TempDir()
			writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.2.0")
			runner := standInGit(t, testCase.described, "037797e856cb1ec53c1545741d08fa758b6f0edb")
			runner.Anchors = []string{build}

			packages, findings := Discover(Options{
				BuildDir: build, Runner: runner, Context: context.Background(),
			})
			if len(packages) != 1 {
				t.Fatalf("packages = %#v", packages)
			}
			found := packages[0]
			if found.Version.Value != testCase.wantVersion || found.Version.Source != testCase.wantSource {
				t.Errorf("version = %q from %q, want %q from %q", found.Version.Value,
					found.Version.Source, testCase.wantVersion, testCase.wantSource)
			}
			if found.Version.Confidence != testCase.wantConfidence {
				t.Errorf("confidence = %q, want %q", found.Version.Confidence, testCase.wantConfidence)
			}
			if found.Dirty != testCase.wantDirty {
				t.Errorf("dirty = %v, want %v", found.Dirty, testCase.wantDirty)
			}
			// The purl restates the version that won, so no losing value can
			// reach the document through it either.
			if !strings.Contains(found.PURL.Value, "@"+testCase.wantVersion) {
				t.Errorf("purl = %q, want the version that won in it", found.PURL.Value)
			}
			displaced := make([]string, 0, 1)
			for _, contribution := range found.Superseded {
				if contribution.Field == FieldVersion {
					displaced = append(displaced, contribution.Claim.Value)
				}
			}
			if testCase.wantDisplaced == "" && len(displaced) != 0 {
				t.Errorf("superseded = %q, want none: only one origin stated a version", displaced)
			}
			if testCase.wantDisplaced != "" &&
				(len(displaced) != 1 || displaced[0] != testCase.wantDisplaced) {
				t.Errorf("superseded = %q, want the displaced tag %q kept", displaced, testCase.wantDisplaced)
			}
			if reported := findingsWithID(findings, "UNKNOWN_VERSION"); len(reported) != 0 {
				t.Errorf("findings = %+v, want none: a version was known throughout", reported)
			}
			assertNoOriginWithoutAValue(t, packages)
		})
	}
}

// Every adapter has to name the origin it read and how far that origin counts,
// because the document publishes the origin of the version it prints and the
// ranking decides which of two origins that is. The strings are output rather
// than an internal label: section 20.3 maps them onto a closed CycloneDX
// vocabulary, and a renamed origin silently becomes "other".
func TestEachAdapterNamesTheOriginItRead(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, build, source string) Options
		want    map[Field]Claim
	}{{
		name: "conan states the version in the file CMakeDeps installed",
		prepare: func(t *testing.T, build, _ string) Options {
			packageRoot := filepath.Join(build, "pkg", "tinycbor")
			writeTestFile(t, filepath.Join(build, "tinycbor-config-version.cmake"),
				"set(PACKAGE_VERSION \"0.6.1\")\n")
			writeTestFile(t, filepath.Join(build, "tinycbor-release-x86_64-data.cmake"),
				"set(tinycbor_PACKAGE_FOLDER_RELEASE \""+packageRoot+"\")\n")
			writeTestFile(t, filepath.Join(packageRoot, "include", "cbor.h"), "#define CBOR 1\n")
			return Options{BuildDir: build, Context: context.Background()}
		},
		want: map[Field]Claim{
			FieldVersion: {Value: "0.6.1", Source: "conan", Rank: RankInstallState, Confidence: domain.ConfidenceHigh},
			FieldPURL:    {Value: "pkg:conan/tinycbor@0.6.1", Source: "conan", Rank: RankInstallState},
		},
	}, {
		name: "vcpkg states all four in the document it wrote while installing",
		prepare: func(t *testing.T, build, _ string) Options {
			share := filepath.Join(build, "vcpkg_installed", "x64-linux", "share", "tinyfmt")
			writeTestFile(t, filepath.Join(share, "vcpkg.spdx.json"),
				`{"packages":[{"name":"tinyfmt","versionInfo":"2.1.0","licenseDeclared":"MIT",`+
					`"supplier":"Organization: Example Org","externalRefs":[{"referenceCategory":"PACKAGE-MANAGER",`+
					`"referenceType":"purl","referenceLocator":"pkg:vcpkg/tinyfmt@2.1.0?triplet=x64-linux"}]}]}`)
			return Options{BuildDir: build, Context: context.Background()}
		},
		want: map[Field]Claim{
			FieldVersion:  {Value: "2.1.0", Source: "vcpkg", Rank: RankInstallState, Confidence: domain.ConfidenceHigh},
			FieldLicense:  {Value: "MIT", Source: "vcpkg", Rank: RankInstallState},
			FieldSupplier: {Value: "Example Org", Source: "vcpkg", Rank: RankInstallState},
			FieldPURL:     {Value: "pkg:vcpkg/tinyfmt@2.1.0?triplet=x64-linux", Source: "vcpkg", Rank: RankInstallState},
		},
	}, {
		name: "fetchcontent states the revision in the script CMake ran",
		prepare: func(t *testing.T, build, _ string) Options {
			writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.2.0")
			return Options{BuildDir: build, Context: context.Background()}
		},
		want: map[Field]Claim{
			FieldVersion: {Value: "1.2.0", Source: "fetchcontent", Rank: RankInstallState, Confidence: domain.ConfidenceHigh},
			FieldPURL: {
				Value:  "pkg:generic/tinylog@1.2.0?vcs_url=git%2Bhttps%3A%2F%2Fexample.invalid%2Forg%2Ftinylog",
				Source: "fetchcontent", Rank: RankInstallState,
			},
		},
	}, {
		name: "a submodule declares no revision, so only the checkout can state one",
		prepare: func(t *testing.T, _, source string) Options {
			writeTestFile(t, filepath.Join(source, ".gitmodules"),
				"[submodule \"dep/tinyhash\"]\n\tpath = dep/tinyhash\n"+
					"\turl = https://example.invalid/org/tinyhash.git\n")
			writeTestFile(t, filepath.Join(source, "dep", "tinyhash", "hash.c"), "int h(void){return 0;}\n")
			runner := standInGit(t, "v0.9.0", "")
			runner.Anchors = []string{source}
			return Options{SourceDir: source, Runner: runner, Context: context.Background()}
		},
		want: map[Field]Claim{
			FieldVersion: {Value: "0.9.0", Source: "git-describe", Rank: RankObservedCheckout, Confidence: domain.ConfidenceHigh},
			FieldPURL: {
				Value:  "pkg:generic/tinyhash@0.9.0?vcs_url=git%2Bhttps%3A%2F%2Fexample.invalid%2Forg%2Ftinyhash",
				Source: "git-submodule", Rank: RankObservedCheckout,
			},
		},
	}}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			build, source := t.TempDir(), t.TempDir()
			options := testCase.prepare(t, build, source)

			packages, _ := Discover(options)
			if len(packages) != 1 {
				t.Fatalf("packages = %#v", packages)
			}
			got := claimsOf(packages[0])
			for field, want := range testCase.want {
				if got[field] != want {
					t.Errorf("%s claim = %#v, want %#v", field, got[field], want)
				}
			}
			assertNoOriginWithoutAValue(t, packages)
		})
	}
}

// Evidence that cannot be read is not evidence. A file that is truncated,
// malformed or says nothing has to leave the field empty rather than produce a
// value with an origin behind it, because the claim is what the document
// publishes and what the ranking compares.
func TestUnreadableEvidenceClaimsNothing(t *testing.T) {
	cases := []struct {
		name         string
		prepare      func(t *testing.T, build, source string) Options
		wantPackages int
		wantFinding  string
	}{{
		name: "an SPDX document that is not JSON describes no package at all",
		prepare: func(t *testing.T, build, _ string) Options {
			writeTestFile(t, filepath.Join(build, "vcpkg_installed", "x64-linux", "share",
				"tinyfmt", "vcpkg.spdx.json"), "{\"packages\":[{\"name\"")
			return Options{BuildDir: build, Context: context.Background()}
		},
	}, {
		name: "an SPDX document that asserts nothing leaves every field empty",
		prepare: func(t *testing.T, build, _ string) Options {
			writeTestFile(t, filepath.Join(build, "vcpkg_installed", "x64-linux", "share",
				"tinyfmt", "vcpkg.spdx.json"),
				`{"packages":[{"name":"tinyfmt","versionInfo":"","licenseDeclared":"NOASSERTION",`+
					`"licenseConcluded":"NOASSERTION","supplier":"NOASSERTION"}]}`)
			return Options{BuildDir: build, Context: context.Background()}
		},
		wantPackages: 1, wantFinding: "UNKNOWN_VERSION",
	}, {
		name: "a truncated Conan config file states no version",
		prepare: func(t *testing.T, build, _ string) Options {
			writeTestFile(t, filepath.Join(build, "tinycbor-config-version.cmake"),
				"set(PACKAGE_VERSION \"0.6.1")
			writeTestFile(t, filepath.Join(build, "tinycbor-release-x86_64-data.cmake"),
				"set(tinycbor_PACKAGE_FOLDER_RELEASE \""+filepath.Join(build, "pkg")+"\")\n")
			return Options{BuildDir: build, Context: context.Background()}
		},
		wantPackages: 1, wantFinding: "UNKNOWN_VERSION",
	}, {
		name: "a Conan data file with no package folder maps no files, so the entry is dropped",
		prepare: func(t *testing.T, build, _ string) Options {
			writeTestFile(t, filepath.Join(build, "tinycbor-config-version.cmake"),
				"set(PACKAGE_VERSION \"0.6.1\")\n")
			writeTestFile(t, filepath.Join(build, "tinycbor-release-x86_64-data.cmake"), "# nothing\n")
			return Options{BuildDir: build, Context: context.Background()}
		},
		wantFinding: "MISSING_PACKAGE_EVIDENCE",
	}, {
		name: "a populate script without a checkout names no revision",
		prepare: func(t *testing.T, build, _ string) Options {
			writeTestFile(t, filepath.Join(build, "_deps", "tinylog-subbuild",
				"tinylog-populate-prefix", "tmp", "tinylog-populate-gitclone.cmake"),
				"execute_process(COMMAND \"/usr/bin/git\" status)\n")
			if err := os.MkdirAll(filepath.Join(build, "_deps", "tinylog-src"), 0o755); err != nil {
				t.Fatal(err)
			}
			return Options{BuildDir: build, Context: context.Background()}
		},
		wantPackages: 1, wantFinding: "UNKNOWN_VERSION",
	}, {
		name: "a .gitmodules entry without a path declares no boundary",
		prepare: func(t *testing.T, _, source string) Options {
			writeTestFile(t, filepath.Join(source, ".gitmodules"),
				"[submodule \"dep/tinyhash\"]\n\turl = https://example.invalid/org/tinyhash.git\n")
			return Options{SourceDir: source, Context: context.Background()}
		},
	}}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			build, source := t.TempDir(), t.TempDir()
			options := testCase.prepare(t, build, source)

			packages, findings := Discover(options)
			if len(packages) != testCase.wantPackages {
				t.Fatalf("packages = %#v, want %d", packages, testCase.wantPackages)
			}
			assertNoOriginWithoutAValue(t, packages)
			for _, found := range packages {
				if found.Version.Value != "" {
					t.Errorf("version = %q, want none: nothing readable stated one", found.Version.Value)
				}
				if strings.Contains(found.PURL.Value, "@") {
					t.Errorf("purl = %q, want no version in it", found.PURL.Value)
				}
				if len(found.Superseded) != 0 {
					t.Errorf("superseded = %#v, want none: there was no second origin", found.Superseded)
				}
			}
			if testCase.wantFinding == "" {
				return
			}
			if reported := findingsWithID(findings, testCase.wantFinding); len(reported) != 1 {
				t.Errorf("findings = %+v, want one %s", findings, testCase.wantFinding)
			}
		})
	}
}

// A file past a bound of section 30 is not read at all, so it states nothing
// -- and stating nothing must not turn into an empty value with an origin
// attached to it. The content is beside the point here: every adapter refuses
// on the size before it opens the file, which is why the padding is a
// truncation rather than megabytes of generated text.
func TestEvidencePastTheBoundsOfSectionThirtyClaimsNothing(t *testing.T) {
	cases := []struct {
		name         string
		prepare      func(t *testing.T, build, source string) Options
		wantPackages int
	}{{
		name: "an SPDX document over the byte bound describes no package",
		prepare: func(t *testing.T, build, _ string) Options {
			path := filepath.Join(build, "vcpkg_installed", "x64-linux", "share",
				"tinyfmt", "vcpkg.spdx.json")
			writeTestFile(t, path, `{"packages":[{"name":"tinyfmt","versionInfo":"2.1.0"}]}`)
			padTo(t, path, maxSPDXBytes+1)
			return Options{BuildDir: build, Context: context.Background()}
		},
	}, {
		name: "a Conan config file over the byte bound states no version",
		prepare: func(t *testing.T, build, _ string) Options {
			path := filepath.Join(build, "tinycbor-config-version.cmake")
			writeTestFile(t, path, "set(PACKAGE_VERSION \"0.6.1\")\n")
			padTo(t, path, maxConanFileBytes+1)
			writeTestFile(t, filepath.Join(build, "tinycbor-release-x86_64-data.cmake"),
				"set(tinycbor_PACKAGE_FOLDER_RELEASE \""+filepath.Join(build, "pkg")+"\")\n")
			return Options{BuildDir: build, Context: context.Background()}
		},
		wantPackages: 1,
	}, {
		name: "a populate script over the byte bound names no revision",
		prepare: func(t *testing.T, build, _ string) Options {
			writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.2.0")
			padTo(t, filepath.Join(build, "_deps", "tinylog-subbuild", "tinylog-populate-prefix",
				"tmp", "tinylog-populate-gitclone.cmake"), maxPopulateScriptBytes+1)
			return Options{BuildDir: build, Context: context.Background()}
		},
		wantPackages: 1,
	}, {
		name: "a .gitmodules over the byte bound declares no boundary",
		prepare: func(t *testing.T, _, source string) Options {
			path := filepath.Join(source, ".gitmodules")
			writeTestFile(t, path, "[submodule \"dep/tinyhash\"]\n\tpath = dep/tinyhash\n"+
				"\turl = https://example.invalid/org/tinyhash.git\n")
			padTo(t, path, maxGitmodulesBytes+1)
			return Options{SourceDir: source, Context: context.Background()}
		},
	}}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			build, source := t.TempDir(), t.TempDir()
			options := testCase.prepare(t, build, source)

			packages, _ := Discover(options)
			if len(packages) != testCase.wantPackages {
				t.Fatalf("packages = %#v, want %d", packages, testCase.wantPackages)
			}
			assertNoOriginWithoutAValue(t, packages)
			for _, found := range packages {
				if found.Version.Value != "" || found.Version.Source != "" {
					t.Errorf("version = %#v, want none: the file was never read", found.Version)
				}
			}
		})
	}
}

// padTo grows a file past a bound without writing the bytes: only its size
// decides whether an adapter reads it.
func padTo(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.Truncate(path, size); err != nil {
		t.Fatal(err)
	}
}

// The rule of this package, stated from outside it: discovery may improve what
// is known about a file, never make one part of the product. The only paths a
// package carries at all are the ones its own manager wrote down, and even
// those are a lookup key -- a header sitting in the install tree that no list
// names is not attributed, and a manager that keeps no list names no file.
func TestAPackageNamesOnlyTheFilesItsManagerWroteDown(t *testing.T) {
	build, source := t.TempDir(), t.TempDir()
	installed := filepath.Join(build, "vcpkg_installed", "x64-linux")
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")
	writeVcpkgFileList(t, build, "x64-linux", "tinyfmt", "2.1.0", []string{
		"x64-linux/include/tinyfmt.h",
	})
	// Both headers exist in the tree; only one of them is on the list vcpkg
	// wrote. The other belongs to some other port and stays unattributed.
	writeTestFile(t, filepath.Join(installed, "include", "tinyfmt.h"), "#define TINYFMT 1\n")
	writeTestFile(t, filepath.Join(installed, "include", "stranger.h"), "#define STRANGER 1\n")

	// Three more managers, each with a package and none of them keeping a file
	// list of its own.
	writeTestFile(t, filepath.Join(build, "tinycbor-config-version.cmake"),
		"set(PACKAGE_VERSION \"0.6.1\")\n")
	writeTestFile(t, filepath.Join(build, "tinycbor-release-x86_64-data.cmake"),
		"set(tinycbor_PACKAGE_FOLDER_RELEASE \""+filepath.Join(build, "pkg", "tinycbor")+"\")\n")
	writeTestFile(t, filepath.Join(build, "pkg", "tinycbor", "include", "cbor.h"), "#define CBOR 1\n")
	writePopulate(t, build, "tinylog", "https://example.invalid/org/tinylog.git", "v1.2.0")
	writeTestFile(t, filepath.Join(build, "_deps", "tinylog-src", "log.c"), "int l(void){return 0;}\n")
	writeTestFile(t, filepath.Join(source, ".gitmodules"),
		"[submodule \"dep/tinyhash\"]\n\tpath = dep/tinyhash\n"+
			"\turl = https://example.invalid/org/tinyhash.git\n")
	writeTestFile(t, filepath.Join(source, "dep", "tinyhash", "hash.c"), "int h(void){return 0;}\n")

	packages, _ := Discover(Options{BuildDir: build, SourceDir: source, Context: context.Background()})
	if len(packages) != 4 {
		t.Fatalf("packages = %#v, want one per manager", packages)
	}
	for _, found := range packages {
		if found.Manager == "vcpkg" {
			want := filepath.Join(installed, "include", "tinyfmt.h")
			if len(found.Files) != 1 || found.Files[0] != want {
				t.Errorf("vcpkg files = %q, want only %q: the list is the statement, the tree is not",
					found.Files, want)
			}
			continue
		}
		if len(found.Files) != 0 {
			t.Errorf("%s files = %q, want none: this manager wrote no list, so it names no file",
				found.Manager, found.Files)
		}
	}
}
