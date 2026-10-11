package spdx

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/digest"
	"github.com/example/sbomb/internal/sbommap"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
	"github.com/example/sbomb/internal/spdx/mapping"
	"github.com/example/sbomb/internal/spdx/spdx23"
)

// options23 are the all-fields options at SPDX 2.3.
func options23() sbomwriter.Options {
	options := sbomwritertest.AllFieldsOptions()
	options.SpecVersion = Version23
	return options
}

// The writer writes SPDX 2.3 through the same Write, and the document it
// writes is detected, validated and checked as SPDX 2.3.
func TestTheWriterWritesSpdx23(t *testing.T) {
	data := write(t, sbomwritertest.AllFields(), options23())
	if !bytes.HasPrefix(data, []byte("{\n  \"spdxVersion\": \"SPDX-2.3\",")) {
		t.Fatalf("the document does not start with its version:\n%.200s", data)
	}
	writer, version, err := sbomwriter.DetectFormat(data)
	if err != nil || writer.ID() != ID || version != Version23 {
		t.Errorf("DetectFormat = %v %q %v", writer, version, err)
	}
	if err := (Writer{}).ValidateOutput(data); err != nil {
		t.Errorf("ValidateOutput = %v", err)
	}
	model, err := mapping.Build(sbomwritertest.AllFields(), mapping.Options{
		SpecVersion: Version23, LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true,
		Digests: spdx23.FileDigests(), SelfContainedLicenseRefs: spdx23.SelfContainedLicenseRefs,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "urn:uuid:" + sbommap.DocumentUUID(mapping.Digest(model))
	if !bytes.Contains(data, []byte(`"documentNamespace": "`+want+`"`)) {
		t.Errorf("the reproducible namespace is not %s", want)
	}
	if bytes.Equal(data, write(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions())) {
		t.Error("2.3 and 3.0.1 wrote the same bytes")
	}
}

// Only SPDX 2.3 needs a digest beyond SHA-256, and the run is told so before
// it reads anything.
func TestFileDigestsPerVersion(t *testing.T) {
	for version, want := range map[string][]string{Version23: {"SHA-1"}, Version301: nil, "": nil, "2.2": nil} {
		if got := (Writer{}).FileDigests(version); !reflect.DeepEqual(got, want) {
			t.Errorf("FileDigests(%q) = %v, want %v", version, got, want)
		}
	}
	// The answer is a copy: a caller cannot change the table.
	(Writer{}).FileDigests(Version23)[0] = "MD5"
	if got := (Writer{}).FileDigests(Version23); got[0] != "SHA-1" {
		t.Error("FileDigests handed out the table")
	}
	// Every digest a version asks for is one the shared table computes, so
	// the hashing of used files and the mapping of retained licence files
	// both can (they answer from that table alone).
	for version := range renderers {
		for _, name := range (Writer{}).FileDigests(version) {
			if !digest.Supported(name) || !digest.OnRequest(name) {
				t.Errorf("SPDX %s asks for a %s digest the shared table does not compute on request", version, name)
			}
		}
	}
}

// Preflight refuses at 2.3 what it refuses at 3.0.1, naming the version.
func TestPreflightAt23(t *testing.T) {
	if err := (Writer{}).Preflight(Version23, sbomwriter.Options{TLP: "AMBER"}); err == nil || !strings.Contains(err.Error(), "SPDX 2.3") {
		t.Errorf("TLP = %v", err)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "")
	var refusal *sbomwriter.RefusalError
	err := (Writer{}).Preflight(Version23, sbomwriter.Options{Reproducible: true})
	if !errors.As(err, &refusal) || refusal.ID != "REPRODUCIBLE_CREATION_TIME_MISSING" || !strings.Contains(err.Error(), "SPDX 2.3") {
		t.Errorf("reproducible without SOURCE_DATE_EPOCH = %v", err)
	}
	if err := (Writer{}).Write(&bytes.Buffer{}, sbomwritertest.AllFields(), sbomwriter.Options{SpecVersion: Version23, TLP: "CLEAR"}); err == nil || !strings.Contains(err.Error(), "SPDX 2.3") {
		t.Errorf("Write with a TLP = %v", err)
	}
}

// LocalIdentity reads both versions' element identifiers back.
func TestLocalIdentityReadsBothVersions(t *testing.T) {
	for reference, want := range map[string]string{
		"SPDXRef-file-3Aproject-3Asrc-2Fmain.c":                        "file:project:src/main.c",
		"urn:uuid:6f1d3a52-6c49-4f0e-9a52-2b1f4d6c0e11#component:zlib": "component:zlib",
		"component:zlib": "component:zlib",
		"zlib":           "zlib",
	} {
		if got := LocalIdentity(reference); got != want {
			t.Errorf("LocalIdentity(%q) = %q, want %q", reference, got, want)
		}
	}
}

// explain --component --sbom answers the same about the same run in both
// versions: the lookup is shared, and so are the candidates.
func TestComponentFilesAgreeAcrossVersions(t *testing.T) {
	doc23 := write(t, sbomwritertest.AllFields(), options23())
	doc301 := write(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions())
	for _, name := range []string{"kitchen", "component:kitchen", "libc", "hdr", "app", "fs.img", "kitchen.c", "hdr.h", "libc.so.6",
		"build-environment", "toolchain headers", "sentinel-product", "nowhere", "NOTICE", "0002-sentinel.patch"} {
		files23, err23 := (Writer{}).ComponentFiles(doc23, name)
		files301, err301 := (Writer{}).ComponentFiles(doc301, name)
		sort.Strings(files23)
		sort.Strings(files301)
		if (err23 == nil) != (err301 == nil) || err23 != nil && !errors.Is(err23, sbomwriter.ErrNoSuchComponent) {
			t.Errorf("%s: 2.3 says %v, 3.0.1 says %v", name, err23, err301)
			continue
		}
		if len(files23) != len(files301) || len(files23) > 0 && !reflect.DeepEqual(files23, files301) {
			t.Errorf("%s: 2.3 lists %v, 3.0.1 lists %v", name, files23, files301)
		}
	}
}

// The renderers are siblings: neither imports the other, and the shared
// mapping imports neither (production code; a test may compare them). A renderer reaching into its sibling would make
// the two versions one package in all but name.
func TestTheRenderersAreSiblings(t *testing.T) {
	forbidden := map[string][]string{
		"spdx23":  {"github.com/example/sbomb/internal/spdx/spdx3", "github.com/example/sbomb/internal/spdx"},
		"spdx3":   {"github.com/example/sbomb/internal/spdx/spdx23", "github.com/example/sbomb/internal/spdx"},
		"mapping": {"github.com/example/sbomb/internal/spdx/spdx3", "github.com/example/sbomb/internal/spdx/spdx23", "github.com/example/sbomb/internal/spdx"},
	}
	for directory, imports := range forbidden {
		paths, err := filepath.Glob(filepath.Join(directory, "*.go"))
		if err != nil || len(paths) == 0 {
			t.Fatalf("%s: no files (%v)", directory, err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range file.Imports {
				imported, _ := strconv.Unquote(spec.Path.Value)
				for _, bad := range imports {
					if imported == bad {
						t.Errorf("%s imports %s", path, imported)
					}
				}
			}
		}
	}
}

// The 2.3 entry of the writer's version table, not a copy of it: a licence
// reference the build stated is defined in the document, one of another
// document and an unlisted exception become references sbomb defines, and no
// AdditionRef appears. With customAdditions or selfContainedRefs flipped in
// the table, this document is refused by the 2.3 checks.
func TestTheSpdx23WriterDefinesTheReferencesTheBuildStated(t *testing.T) {
	writer, err := sbomwriter.Get(ID, Version23)
	if err != nil {
		t.Fatal(err)
	}
	options := sbomwritertest.AllFieldsOptions()
	options.SpecVersion = Version23
	var buffer bytes.Buffer
	if err := writer.Write(&buffer, sbomwritertest.LicenceReferences(), options); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	if err := spdx23.Validate(data); err != nil {
		t.Errorf("Validate: %v", err)
	}
	if err := spdx23.ValidateOwnOutput(data); err != nil {
		t.Errorf("ValidateOwnOutput: %v", err)
	}
	var doc struct {
		Packages []struct {
			Name             string `json:"name"`
			LicenseConcluded string `json:"licenseConcluded"`
		} `json:"packages"`
		Extracted []struct {
			LicenseID string `json:"licenseId"`
			Text      string `json:"extractedText"`
		} `json:"hasExtractedLicensingInfos"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	defined := map[string]string{}
	for _, info := range doc.Extracted {
		defined[info.LicenseID] = info.Text
	}
	if _, found := defined["LicenseRef-acme"]; !found {
		t.Errorf("LicenseRef-acme is not defined in hasExtractedLicensingInfos (defined: %v)", defined)
	}
	if bytes.Contains(data, []byte("AdditionRef")) {
		t.Error("the 2.3 document names an AdditionRef")
	}
	concluded := map[string]string{}
	for _, p := range doc.Packages {
		concluded[p.Name] = p.LicenseConcluded
	}
	// The reference of another document is replaced by one sbomb defines,
	// whose text is the name the build stated.
	acme := concluded["acme"]
	if !strings.HasPrefix(acme, "LicenseRef-acme OR LicenseRef-sbomb-") || strings.Contains(acme, ":") {
		t.Errorf("acme concludes %q, want LicenseRef-acme OR a reference sbomb defines", acme)
	}
	if other := strings.TrimPrefix(acme, "LicenseRef-acme OR "); defined[other] != "DocumentRef-other:LicenseRef-beta" {
		t.Errorf("%s is defined as %q, want the stated reference", other, defined[other])
	}
	// The expression with an unlisted exception is one reference sbomb
	// defines, whose text is the whole expression.
	excepted := concluded["excepted"]
	if !strings.HasPrefix(excepted, "LicenseRef-sbomb-") || defined[excepted] != sbomwritertest.LicenceUnlistedException {
		t.Errorf("excepted concludes %q (defined as %q), want one reference for %q", excepted, defined[excepted], sbomwritertest.LicenceUnlistedException)
	}
}
