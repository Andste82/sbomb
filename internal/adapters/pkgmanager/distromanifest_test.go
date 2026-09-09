package pkgmanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// The tests in this file ask what an image manifest states and, just as
// importantly, what it does not: the pipeline tests in internal/generate ask
// what reading one does to a document.

// A Yocto license.manifest as bitbake writes it into the deploy directory:
// blank-line-separated blocks of "KEY: value", one per package.
const yoctoManifest = `PACKAGE NAME: busybox
PACKAGE VERSION: 1.36.1
RECIPE NAME: busybox
LICENSE: GPL-2.0-only

PACKAGE NAME: libssl3
PACKAGE VERSION: 3.0.12
RECIPE NAME: openssl
LICENSE: Apache-2.0

PACKAGE NAME: tinylog
PACKAGE VERSION: 1.9.0
RECIPE NAME: tinylog
LICENSE: MIT
`

// A Buildroot legal-info/manifest.csv: a quoted header, quoted fields, and a
// licence expression with a comma inside the quotes.
const buildrootManifest = `"PACKAGE","VERSION","LICENSE","LICENSE FILES","SOURCE","SOURCE SITE"
"busybox","1.36.1","GPL-2.0","LICENSE","busybox-1.36.1.tar.bz2","https://busybox.net/downloads"
"tinylog","1.9.0","GPL-2.0+, LGPL-2.1+ with exceptions","COPYING","tinylog-1.9.0.tar.gz","https://example.invalid"
`

func writeDistroManifest(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	writeTestFile(t, path, content)
	return path
}

// describedValue is what the index states about one field of one name, so that
// a test can say what it expects without walking a slice.
func describedValue(metadata *DistroMetadata, name string, field Field) Claim {
	for _, contribution := range metadata.Describe(name) {
		if contribution.Field == field {
			return contribution.Claim
		}
	}
	return Claim{}
}

// A package a Yocto image built is findable under the package name and under
// the recipe it came from: a component in a build tree may be named after
// either, and the two differ whenever a recipe produces several packages.
func TestAYoctoManifestDescribesAPackageUnderItsPackageAndItsRecipeName(t *testing.T) {
	path := writeDistroManifest(t, "license.manifest", yoctoManifest)
	metadata, findings := ReadDistroManifests([]string{path})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none for a manifest that reads cleanly", findings)
	}
	for _, name := range []string{"libssl3", "openssl"} {
		version := describedValue(metadata, name, FieldVersion)
		if version.Value != "3.0.12" || version.Source != "yocto" {
			t.Errorf("%s: version = %q from %q, want 3.0.12 from yocto", name, version.Value, version.Source)
		}
		if version.Rank != RankInstallState {
			t.Errorf("%s: rank = %v, want the installed state of section 21.1", name, version.Rank)
		}
		if version.Confidence != domain.ConfidenceHigh {
			t.Errorf("%s: confidence = %q, want high", name, version.Confidence)
		}
		if license := describedValue(metadata, name, FieldLicense); license.Value != "Apache-2.0" {
			t.Errorf("%s: license = %q, want Apache-2.0", name, license.Value)
		}
	}
	// Section 20.3 has a confidence table for the version and for no other
	// field, so nothing else may carry one.
	if license := describedValue(metadata, "busybox", FieldLicense); license.Confidence != "" {
		t.Errorf("license confidence = %q, want none: section 20.3 has no table for it", license.Confidence)
	}
	// Nothing else is claimed. These formats state a name, a version and a
	// licence, and a supplier or a purl would have to be invented.
	for _, field := range []Field{FieldSupplier, FieldPURL} {
		if claim := describedValue(metadata, "busybox", field); claim.Value != "" {
			t.Errorf("%s = %q, want nothing: no image manifest states it", field, claim.Value)
		}
	}
	// A name no manifest mentions is silence, not a finding and not an empty
	// claim.
	if described := metadata.Describe("something-else"); described != nil {
		t.Errorf("Describe(unknown) = %+v, want nothing", described)
	}
	if described := (*DistroMetadata)(nil).Describe("busybox"); described != nil {
		t.Errorf("a run with no manifest described %+v, want nothing", described)
	}
}

// The CSV is parsed and not split. A Buildroot licence field routinely holds a
// comma inside its quotes, and splitting on commas would cut the expression in
// half and shift every column after it.
func TestABuildrootManifestKeepsALicenceExpressionThatHoldsAComma(t *testing.T) {
	path := writeDistroManifest(t, "manifest.csv", buildrootManifest)
	metadata, findings := ReadDistroManifests([]string{path})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
	license := describedValue(metadata, "tinylog", FieldLicense)
	if license.Value != "GPL-2.0+, LGPL-2.1+ with exceptions" {
		t.Errorf("license = %q, want the whole expression the quotes hold", license.Value)
	}
	if license.Source != "buildroot" {
		t.Errorf("source = %q, want buildroot", license.Source)
	}
	version := describedValue(metadata, "busybox", FieldVersion)
	if version.Value != "1.36.1" || version.Source != "buildroot" {
		t.Errorf("version = %q from %q, want 1.36.1 from buildroot", version.Value, version.Source)
	}
	// The columns naming a path or a URL are read by nobody: this reader opens
	// only the files it was configured with.
	if sources := metadata.Sources(); len(sources) != 1 || sources[0].Entries != 2 {
		t.Errorf("sources = %+v, want one manifest with two entries", sources)
	}
}

// A configured path that is not there is a typo, and a typo has to be visible:
// a run that silently described nothing would look exactly like an image whose
// packages nothing in this build uses.
func TestAConfiguredManifestThatIsNotThereIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "license.manifest")
	metadata, findings := ReadDistroManifests([]string{path})
	if len(findings) != 1 || findings[0].ID != "MISSING_PACKAGE_EVIDENCE" {
		t.Fatalf("findings = %+v, want one MISSING_PACKAGE_EVIDENCE", findings)
	}
	if findings[0].Subject.Ref != path {
		t.Errorf("subject = %q, want the configured path", findings[0].Subject.Ref)
	}
	if len(metadata.Sources()) != 0 {
		t.Errorf("sources = %+v, want none: nothing was read", metadata.Sources())
	}
}

// A file that is neither format, and a file whose structure breaks part of the
// way through, are refused whole. Half a manifest is not a weaker answer but
// an invented one: some components would carry the version the image build
// recorded and the rest whatever their own evidence said, with nothing in the
// document saying which is which.
func TestAManifestThatCannotBeReadIsRefusedWhole(t *testing.T) {
	for _, testCase := range []struct{ name, file, content string }{
		{"an empty file", "license.manifest", ""},
		{"binary noise", "license.manifest", "\x00\x01\x02\xff\xfe"},
		{"a web page", "license.manifest", "<!doctype html>\n<html><body>404</body></html>\n"},
		{"a CSV naming no package column", "manifest.csv", "\"NAME\",\"VERSION\"\n\"busybox\",\"1.36.1\"\n"},
		{
			"a Yocto block that names no package",
			"license.manifest",
			"PACKAGE NAME: busybox\nPACKAGE VERSION: 1.36.1\n\nPACKAGE VERSION: 3.0.12\nLICENSE: MIT\n",
		},
		{
			"a Yocto line that is not a key with a value",
			"license.manifest",
			"PACKAGE NAME: busybox\nthis line belongs to no format\nPACKAGE VERSION: 1.36.1\n",
		},
		{
			"a CSV row with a field too few",
			"manifest.csv",
			"\"PACKAGE\",\"VERSION\",\"LICENSE\"\n\"busybox\",\"1.36.1\",\"GPL-2.0\"\n\"tinylog\",\"1.9.0\"\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeDistroManifest(t, testCase.file, testCase.content)
			metadata, findings := ReadDistroManifests([]string{path})
			if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
				t.Fatalf("findings = %+v, want one EVIDENCE_UNREADABLE", findings)
			}
			if findings[0].Subject.Ref != path {
				t.Errorf("subject = %q, want the file that could not be read", findings[0].Subject.Ref)
			}
			// Not one claim from the part that did parse.
			for _, name := range []string{"busybox", "tinylog"} {
				if described := metadata.Describe(name); described != nil {
					t.Errorf("%s was described from a file that was refused: %+v", name, described)
				}
			}
			if len(metadata.Sources()) != 0 {
				t.Errorf("sources = %+v, want none: nothing was read", metadata.Sources())
			}
		})
	}
}

// The bounds of section 30, both of them refusing the file whole.
func TestAManifestThatBreachesABoundIsRefusedWhole(t *testing.T) {
	t.Run("too many bytes", func(t *testing.T) {
		padding := strings.Repeat("x", maxDistroManifestBytes)
		path := writeDistroManifest(t, "manifest.csv", buildrootManifest+"\"pad\",\"1.0\",\""+padding+"\"\n")
		metadata, findings := ReadDistroManifests([]string{path})
		if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
			t.Fatalf("findings = %+v, want one INPUT_LIMIT_EXCEEDED", findings)
		}
		if described := metadata.Describe("busybox"); described != nil {
			t.Errorf("a file over the byte bound still described %+v", described)
		}
	})
	t.Run("too many blocks", func(t *testing.T) {
		var manifest strings.Builder
		manifest.WriteString("PACKAGE NAME: busybox\nPACKAGE VERSION: 1.36.1\n\n")
		for index := 0; index <= maxDistroManifestEntries; index++ {
			fmt.Fprintf(&manifest, "PACKAGE NAME: p%d\nPACKAGE VERSION: 1.0\n\n", index)
		}
		path := writeDistroManifest(t, "license.manifest", manifest.String())
		metadata, findings := ReadDistroManifests([]string{path})
		if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
			t.Fatalf("findings = %+v, want one INPUT_LIMIT_EXCEEDED", findings)
		}
		if described := metadata.Describe("busybox"); described != nil {
			t.Errorf("a file over the entry bound still described %+v", described)
		}
	})
	t.Run("too many rows", func(t *testing.T) {
		var manifest strings.Builder
		manifest.WriteString("\"PACKAGE\",\"VERSION\",\"LICENSE\"\n")
		manifest.WriteString("\"busybox\",\"1.36.1\",\"GPL-2.0\"\n")
		for index := 0; index <= maxDistroManifestEntries; index++ {
			fmt.Fprintf(&manifest, "\"p%d\",\"1.0\",\"MIT\"\n", index)
		}
		path := writeDistroManifest(t, "manifest.csv", manifest.String())
		metadata, findings := ReadDistroManifests([]string{path})
		if len(findings) != 1 || findings[0].ID != "INPUT_LIMIT_EXCEEDED" {
			t.Fatalf("findings = %+v, want one INPUT_LIMIT_EXCEEDED", findings)
		}
		if described := metadata.Describe("busybox"); described != nil {
			t.Errorf("a file over the entry bound still described %+v", described)
		}
	})
}

// Two entries that state two versions for one name are not two statements but
// none, exactly as a file two packages claim belongs to neither (section 19.2).
// Two entries that agree are no disagreement at all and stay silent.
func TestTwoEntriesThatDisagreeDescribeNothingAndSayWhy(t *testing.T) {
	manifest := "\"PACKAGE\",\"VERSION\",\"LICENSE\"\n" +
		"\"busybox\",\"1.36.1\",\"GPL-2.0\"\n" +
		"\"busybox\",\"1.35.0\",\"GPL-2.0\"\n" +
		"\"tinylog\",\"1.9.0\",\"MIT\"\n" +
		"\"tinylog\",\"1.9.0\",\"MIT\"\n"
	path := writeDistroManifest(t, "manifest.csv", manifest)
	metadata, findings := ReadDistroManifests([]string{path})
	if len(findings) != 1 || findings[0].ID != "COMPONENT_MAPPING_CONFLICT" {
		t.Fatalf("findings = %+v, want one COMPONENT_MAPPING_CONFLICT", findings)
	}
	if findings[0].Subject.Ref != "busybox" {
		t.Errorf("subject = %q, want the contested name", findings[0].Subject.Ref)
	}
	for _, value := range []string{"1.36.1", "1.35.0"} {
		if !strings.Contains(findings[0].Message, value) {
			t.Errorf("message = %q, want both disputed values in it", findings[0].Message)
		}
	}
	if version := describedValue(metadata, "busybox", FieldVersion); version.Value != "" {
		t.Errorf("version = %q, want nothing: two statements are no statement", version.Value)
	}
	// The field they agreed on survives; the disagreement was about one field.
	if license := describedValue(metadata, "busybox", FieldLicense); license.Value != "GPL-2.0" {
		t.Errorf("license = %q, want the value both entries stated", license.Value)
	}
	if version := describedValue(metadata, "tinylog", FieldVersion); version.Value != "1.9.0" {
		t.Errorf("tinylog version = %q, want 1.9.0: repeating a value is not a dispute", version.Value)
	}
}

// One Yocto recipe produces several packages, and LICENSE:${PN} gives them
// different licences -- openssl, gcc-runtime, elfutils and bash all do it in an
// ordinary image. The manifest contradicts itself nowhere in that: libcrypto3
// and openssl-bin are different packages. The disagreement would be
// manufactured by this reader's own recipe key, so the recipe simply describes
// nothing where its packages differ, and nothing is reported about a name the
// build need not carry at all.
func TestPackagesOfOneRecipeWithDifferentLicencesReportNothing(t *testing.T) {
	manifest := "PACKAGE NAME: libcrypto3\nPACKAGE VERSION: 3.0.12\nRECIPE NAME: openssl\nLICENSE: OpenSSL\n\n" +
		"PACKAGE NAME: openssl-bin\nPACKAGE VERSION: 3.0.12\nRECIPE NAME: openssl\nLICENSE: Apache-2.0\n\n" +
		"PACKAGE NAME: tinylog\nPACKAGE VERSION: 1.9.0\nRECIPE NAME: tinylog\nLICENSE: MIT\n"
	path := writeDistroManifest(t, "license.manifest", manifest)
	metadata, findings := ReadDistroManifests([]string{path})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: the manifest states no contradiction", findings)
	}
	// Each package keeps the licence stated for it.
	for name, want := range map[string]string{"libcrypto3": "OpenSSL", "openssl-bin": "Apache-2.0"} {
		if license := describedValue(metadata, name, FieldLicense); license.Value != want {
			t.Errorf("%s license = %q, want %q", name, license.Value, want)
		}
	}
	// The recipe key describes nothing about the licence the two disagree on,
	// and still describes the version they agree on.
	if license := describedValue(metadata, "openssl", FieldLicense); license.Value != "" {
		t.Errorf("openssl license = %q, want nothing: its packages state different ones", license.Value)
	}
	if version := describedValue(metadata, "openssl", FieldVersion); version.Value != "3.0.12" {
		t.Errorf("openssl version = %q, want 3.0.12: both packages state it", version.Value)
	}
}

// A package name a manifest states twice with two versions is a contradiction
// in the manifest itself, about a name the manifest put there, and it is
// reported -- unlike the recipe key above.
func TestOneYoctoPackageNameStatedTwiceIsReported(t *testing.T) {
	manifest := "PACKAGE NAME: busybox\nPACKAGE VERSION: 1.36.1\nRECIPE NAME: busybox\nLICENSE: GPL-2.0-only\n\n" +
		"PACKAGE NAME: busybox\nPACKAGE VERSION: 1.35.0\nRECIPE NAME: busybox\nLICENSE: GPL-2.0-only\n"
	path := writeDistroManifest(t, "license.manifest", manifest)
	metadata, findings := ReadDistroManifests([]string{path})
	if len(findings) != 1 || findings[0].ID != "COMPONENT_MAPPING_CONFLICT" {
		t.Fatalf("findings = %+v, want one COMPONENT_MAPPING_CONFLICT", findings)
	}
	if findings[0].Subject.Ref != "busybox" {
		t.Errorf("subject = %q, want the contested package name", findings[0].Subject.Ref)
	}
	if version := describedValue(metadata, "busybox", FieldVersion); version.Value != "" {
		t.Errorf("version = %q, want nothing: two statements are no statement", version.Value)
	}
	if license := describedValue(metadata, "busybox", FieldLicense); license.Value != "GPL-2.0-only" {
		t.Errorf("license = %q, want the value both blocks stated", license.Value)
	}
}

// The index is built from a sorted key set rather than from map order, so the
// same evidence yields the same claims and the same findings whichever order
// the manifests were configured in.
func TestTheIndexDoesNotDependOnTheOrderTheManifestsWereRead(t *testing.T) {
	yocto := writeDistroManifest(t, "license.manifest", yoctoManifest)
	buildroot := writeDistroManifest(t, "manifest.csv",
		"\"PACKAGE\",\"VERSION\",\"LICENSE\"\n\"zlib\",\"1.3.1\",\"Zlib\"\n\"busybox\",\"9.9.9\",\"GPL-2.0\"\n")

	render := func(paths []string) string {
		metadata, findings := ReadDistroManifests(paths)
		var out strings.Builder
		for _, name := range []string{"busybox", "libssl3", "openssl", "tinylog", "zlib"} {
			fmt.Fprintf(&out, "%s %+v\n", name, metadata.Describe(name))
		}
		for _, finding := range findings {
			fmt.Fprintf(&out, "%s %s %s\n", finding.ID, finding.Subject.Ref, finding.Message)
		}
		return out.String()
	}
	forwards := render([]string{yocto, buildroot})
	backwards := render([]string{buildroot, yocto})
	if forwards != backwards {
		t.Errorf("the order of the manifests changed the answer:\n%s\n%s", forwards, backwards)
	}
	both, _ := ReadDistroManifests([]string{yocto, buildroot})
	if version := describedValue(both, "busybox", FieldVersion); version.Value != "" {
		t.Errorf("busybox version = %q, want nothing: the two manifests disagreed about it", version.Value)
	}
	// A manifest read twice is read once: the same file named twice is one
	// statement and must not become a dispute with itself.
	if once, twice := render([]string{yocto}), render([]string{yocto, yocto}); once != twice {
		t.Errorf("naming one manifest twice changed the answer:\n%s\n%s", once, twice)
	}
}

// The image manifest and the manager that installed the package rank alike, so
// the manager keeps the field and the manifest's claim is retained as the one
// that lost. Discovery is where that order is fixed: the reader is asked after
// the adapter.
func TestTheManagerThatInstalledThePackageKeepsTheFieldAtEqualRank(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")
	metadata, _ := ReadDistroManifests([]string{writeDistroManifest(t, "manifest.csv",
		"\"PACKAGE\",\"VERSION\",\"LICENSE\"\n\"tinyfmt\",\"2.0.0\",\"BSD-3-Clause\"\n")})

	packages, _ := Discover(Options{BuildDir: build, Context: context.Background(), Distro: metadata})
	if len(packages) != 1 {
		t.Fatalf("packages = %+v, want the one port vcpkg installed", packages)
	}
	entry := packages[0]
	if entry.Version.Value != "2.1.0" || entry.Version.Source != "vcpkg" {
		t.Errorf("version = %q from %q, want the manager's own answer",
			entry.Version.Value, entry.Version.Source)
	}
	var superseded []string
	for _, contribution := range entry.Superseded {
		superseded = append(superseded, string(contribution.Field)+"="+contribution.Claim.Value)
	}
	if !containsValue(superseded, "version=2.0.0") {
		t.Errorf("superseded = %v, want the manifest's claim retained rather than dropped", superseded)
	}
	// The manifest describes; it never enumerates. Nothing about the package's
	// extent may have moved.
	if len(entry.Roots) != 1 || len(entry.Files) != 0 {
		t.Errorf("roots/files = %v/%v, want exactly what the adapter found", entry.Roots, entry.Files)
	}
}

// An image manifest describing a package nothing in the build installed adds
// no package at all. The 797 entries of an image that this build has nothing
// to do with are the normal case, and neither a component nor a finding.
func TestAnImageManifestAddsNoPackageOfItsOwn(t *testing.T) {
	build := t.TempDir()
	writeVcpkg(t, build, "x64-linux", "tinyfmt", "2.1.0", "MIT", "pkg:vcpkg/tinyfmt@2.1.0")
	var manifest strings.Builder
	manifest.WriteString("\"PACKAGE\",\"VERSION\",\"LICENSE\"\n")
	for index := 0; index < 800; index++ {
		fmt.Fprintf(&manifest, "\"image-package-%d\",\"1.0\",\"MIT\"\n", index)
	}
	metadata, findings := ReadDistroManifests([]string{writeDistroManifest(t, "manifest.csv", manifest.String())})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none for 800 entries nothing matched", findings)
	}

	packages, discoveryFindings := Discover(Options{BuildDir: build, Context: context.Background(), Distro: metadata})
	if len(packages) != 1 || packages[0].Name != "tinyfmt" {
		t.Fatalf("packages = %+v, want only the port vcpkg installed", packages)
	}
	if len(discoveryFindings) != 0 {
		t.Errorf("findings = %+v, want none: an unmatched entry is silence", discoveryFindings)
	}
}

func containsValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// Both formats name paths of their own -- Buildroot's licence files and source
// archive, Yocto's file list -- and this reader opens none of them. The only
// files it ever opens are the ones the user configured, so a manifest whose
// columns point at nothing existing is read exactly as well as one whose
// columns point at real files, and says nothing about them.
func TestNoPathAnImageManifestNamesIsEverOpened(t *testing.T) {
	for _, testCase := range []struct{ name, file, content string }{
		{
			"a Buildroot licence file and source archive that are not there",
			"manifest.csv",
			"\"PACKAGE\",\"VERSION\",\"LICENSE\",\"LICENSE FILES\",\"SOURCE\",\"SOURCE SITE\"\n" +
				"\"tinylog\",\"1.9.0\",\"MIT\",\"/nonexistent/COPYING\",\"/nonexistent/tinylog.tar.gz\"," +
				"\"https://example.invalid/does-not-resolve\"\n",
		},
		{
			"a Yocto block naming files and a licence directory that are not there",
			"license.manifest",
			"PACKAGE NAME: tinylog\nPACKAGE VERSION: 1.9.0\nRECIPE NAME: tinylog\nLICENSE: MIT\n" +
				"FILES: /nonexistent/usr/lib/libtinylog.so\nLIC_FILES_CHKSUM: file:///nonexistent/COPYING\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeDistroManifest(t, testCase.file, testCase.content)
			metadata, findings := ReadDistroManifests([]string{path})
			// Not one word about a path the manifest named: this reader never
			// looked, so it has nothing to report about it.
			if len(findings) != 0 {
				t.Fatalf("findings = %+v, want none: no path a manifest names is opened", findings)
			}
			if version := describedValue(metadata, "tinylog", FieldVersion); version.Value != "1.9.0" {
				t.Errorf("version = %q, want 1.9.0: the columns this reader takes are readable either way", version.Value)
			}
			if license := describedValue(metadata, "tinylog", FieldLicense); license.Value != "MIT" {
				t.Errorf("license = %q, want MIT", license.Value)
			}
		})
	}
}

// A configured path that names a directory is reported rather than walked. A
// deploy directory holds the manifest; it is not itself one, and looking
// inside it for something manifest-shaped would be the downward search this
// tool does not do.
func TestAConfiguredManifestPathThatIsADirectoryIsReported(t *testing.T) {
	deploy := t.TempDir()
	writeTestFile(t, filepath.Join(deploy, "license.manifest"), yoctoManifest)

	metadata, findings := ReadDistroManifests([]string{deploy})
	if len(findings) != 1 || findings[0].ID != "EVIDENCE_UNREADABLE" {
		t.Fatalf("findings = %+v, want one EVIDENCE_UNREADABLE", findings)
	}
	if findings[0].Subject.Ref != deploy {
		t.Errorf("subject = %q, want the configured path", findings[0].Subject.Ref)
	}
	if described := metadata.Describe("busybox"); described != nil {
		t.Errorf("a directory was searched and described %+v", described)
	}
}

// A deploy directory is routinely reached through a symlink -- Yocto's own
// tmp/deploy/images/<machine> is full of them. A configured path is the user's
// own statement about where to look, so it is followed like every other path
// the configuration names.
func TestAManifestReachedThroughASymlinkIsRead(t *testing.T) {
	real := writeDistroManifest(t, "license.manifest", yoctoManifest)
	link := filepath.Join(t.TempDir(), "image.manifest")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("this filesystem has no symlinks: %v", err)
	}

	metadata, findings := ReadDistroManifests([]string{link})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
	if version := describedValue(metadata, "tinylog", FieldVersion); version.Value != "1.9.0" {
		t.Errorf("version = %q, want 1.9.0 through the symlink", version.Value)
	}
}
