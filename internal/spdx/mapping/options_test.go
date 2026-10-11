package mapping

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
)

func hashOf(file File, key string) string {
	for _, hash := range file.Hashes {
		if hash.Key == key {
			return hash.Value
		}
	}
	return ""
}

func sha1Hex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

// A version that needs a SHA-1 of every file gets it for a retained licence
// file from the bytes the document holds; a used file keeps the one its own
// read computed, a missing file gets none, and without the option nothing is
// added.
func TestDigestsAreComputedFromTheRetainedBytes(t *testing.T) {
	options := Options{SpecVersion: "2.3", LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true, Digests: []string{"SHA-1"}}
	model := build(t, sbomwritertest.AllFields(), options)
	notice := fileOf(t, model, "file:build:_deps/kitchen-src/NOTICE")
	if got, want := hashOf(notice, "SHA-1"), sha1Hex([]byte("sentinel notice bytes are never rendered\n")); got != want {
		t.Errorf("NOTICE SHA-1 = %q, want %q", got, want)
	}
	license := fileOf(t, model, "file:build:_deps/kitchen-src/LICENSE")
	if got, want := hashOf(license, "SHA-1"), sha1Hex([]byte("sentinel licence text of MIT\n")); got != want {
		t.Errorf("LICENSE SHA-1 = %q, want %q", got, want)
	}
	if got := hashOf(fileOf(t, model, "file:build:_deps/kitchen-src/kitchen.c"), "SHA-1"); got != sbomwritertest.HashKitchenSHA1 {
		t.Errorf("kitchen.c SHA-1 = %q, want the one its read computed", got)
	}

	// A used file that is also a retained licence file keeps its own digest.
	document := sbomwritertest.AllFields()
	for index := range document.Files {
		if document.Files[index].ID.RelPath == "_deps/kitchen-src/LICENSE" {
			document.Files[index].Hashes["SHA-1"] = "9999999999999999999999999999999999999999"
		}
	}
	if got := hashOf(fileOf(t, build(t, document, options), "file:build:_deps/kitchen-src/LICENSE"), "SHA-1"); got != "9999999999999999999999999999999999999999" {
		t.Errorf("the used file's own SHA-1 lost to the licence read: %q", got)
	}

	// A missing file gets no digest, whatever bytes a licence read retained.
	missing := sbomwritertest.AllFields()
	for index := range missing.Files {
		if missing.Files[index].ID.RelPath == "_deps/kitchen-src/LICENSE" {
			missing.Files[index].Missing = true
		}
	}
	if file := fileOf(t, build(t, missing, options), "file:build:_deps/kitchen-src/LICENSE"); len(file.Hashes) != 0 {
		t.Errorf("a missing file has digests %v", file.Hashes)
	}

	unset := allFields(t)
	for _, file := range unset.Files {
		if file.LocalID != "file:build:_deps/kitchen-src/kitchen.c" && hashOf(file, "SHA-1") != "" {
			t.Errorf("without the option %s got a SHA-1", file.LocalID)
		}
	}
}

func TestAnUnknownDigestIsRefused(t *testing.T) {
	_, err := Build(sbomwritertest.AllFields(), Options{SpecVersion: "2.3", Digests: []string{"MD5"}})
	if err == nil || !strings.Contains(err.Error(), "MD5") {
		t.Errorf("Build = %v", err)
	}
}

// A version whose documents must define every licence reference gets a
// definition of every LicenseRef the build stated, and a reference minted for
// one in another document; a version without the rule keeps both as stated.
func TestSelfContainedLicenceReferences(t *testing.T) {
	document := sbomwritertest.AllFields()
	document.Components[0].Licenses = []domain.LicenseFinding{{Expression: "LicenseRef-acme OR DocumentRef-other:LicenseRef-beta"}}
	document.Components[1].Licenses = []domain.LicenseFinding{{Expression: "DocumentRef-other:LicenseRef-beta"}}

	model := build(t, document, Options{SpecVersion: "2.3", SelfContainedLicenseRefs: true})
	custom := map[string]CustomLicense{}
	for _, c := range model.Custom {
		custom[c.Ref] = c
	}
	if c, ok := custom["LicenseRef-acme"]; !ok || c.Name != "LicenseRef-acme" || c.Text != "LicenseRef-acme" {
		t.Errorf("LicenseRef-acme = %+v, %v", c, ok)
	}
	kitchen := packageOf(t, model, "component:kitchen").Concluded[0].License.Expression
	minted := strings.TrimPrefix(kitchen, "LicenseRef-acme OR ")
	if !IsSbombLicenseRef(minted) || custom[minted].Name != "DocumentRef-other:LicenseRef-beta" {
		t.Errorf("kitchen = %q, minted = %+v", kitchen, custom[minted])
	}
	for _, license := range model.Licenses {
		if strings.Contains(license.Expression, ":") {
			t.Errorf("an expression still names another document: %q", license.Expression)
		}
	}

	plain := build(t, document, Options{SpecVersion: "3.0.1", CustomAdditions: true})
	if packageOf(t, plain, "component:kitchen").Concluded[0].License.Expression != "LicenseRef-acme OR DocumentRef-other:LicenseRef-beta" {
		t.Error("without the rule the expression changed")
	}
	for _, c := range plain.Custom {
		if c.Ref == "LicenseRef-acme" {
			t.Error("without the rule a stated reference is defined")
		}
	}
}

// The options that only 2.3 sets leave a 3.0.1 model alone: the digest every
// reproducible 3.0.1 identity is derived from does not move.
func TestTheNewOptionsLeaveA301ModelAlone(t *testing.T) {
	before := Digest(allFields(t))
	again := build(t, sbomwritertest.AllFields(), Options{
		LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true, CustomAdditions: true,
		Digests: nil, SelfContainedLicenseRefs: false,
	})
	if string(Digest(again)) != string(before) {
		t.Error("the digest of the 3.0.1 model moved")
	}
	with := build(t, sbomwritertest.AllFields(), Options{
		LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true, CustomAdditions: true,
		Digests: []string{"SHA-1"},
	})
	if string(Digest(with)) == string(before) {
		t.Error("a SHA-1 of the retained files left the digest unchanged; the test proves nothing")
	}
}

func TestCopyrightTextIsTheCuratedNoticeThenTheUnfiledOnes(t *testing.T) {
	for _, test := range []struct {
		pkg  Package
		want string
	}{
		{Package{}, ""},
		{Package{Copyright: "(c) A"}, "(c) A"},
		{Package{UnfiledCopyright: []string{"(c) B", "(c) C"}}, "(c) B\n(c) C"},
		{Package{Copyright: "(c) A", UnfiledCopyright: []string{"(c) B"}}, "(c) A\n(c) B"},
	} {
		if got := test.pkg.CopyrightText(); got != test.want {
			t.Errorf("CopyrightText(%+v) = %q, want %q", test.pkg, got, test.want)
		}
	}
}

func TestFindComponentPrefersTheIdentityOverTheName(t *testing.T) {
	candidates := []Candidate{
		{LocalID: "component:zlib", Name: "zlib"},
		{LocalID: "toolchain:gcc", Name: "gcc"},
		{LocalID: "component:other", Name: "component:zlib"},
		{LocalID: "file:project:src/main.c", Name: BaseName("project:src/main.c")},
	}
	for name, want := range map[string]int{
		"component:zlib":          0,
		"zlib":                    0,
		"gcc":                     1,
		"main.c":                  3,
		"file:project:src/main.c": 3,
	} {
		if got, found := FindComponent(candidates, name); !found || got != want {
			t.Errorf("FindComponent(%q) = %d, %v; want %d", name, got, found, want)
		}
	}
	if _, found := FindComponent(candidates, "nowhere"); found {
		t.Error("an unknown name was found")
	}
	for canonical, want := range map[string]string{"project:src/main.c": "main.c", "project:main.c": "main.c", "main.c": "main.c"} {
		if got := BaseName(canonical); got != want {
			t.Errorf("BaseName(%q) = %q", canonical, got)
		}
	}
}
