package cyclonedx

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbommap"
	"github.com/example/sbomb/internal/sbomwriter"
)

// TestWriteCarriesTheDocumentsOwnIdentity: the serial number is part of the
// rendering. Under reproducible it is the digest of the canonical BOM, exactly
// what callers used to compute themselves, so moving it into Write moved no
// byte; otherwise it is random; and the FOSS review rendering asks for none.
func TestWriteCarriesTheDocumentsOwnIdentity(t *testing.T) {
	serialOf := func(options sbomwriter.Options) string {
		t.Helper()
		data, err := MarshalDocument(sampleDocument(), options)
		if err != nil {
			t.Fatal(err)
		}
		var bom BOM
		if err := json.Unmarshal(data, &bom); err != nil {
			t.Fatal(err)
		}
		return bom.SerialNumber
	}

	built, err := (Writer{}).Build(sampleDocument(), sbomwriter.Options{SpecVersion: Version16, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := serialOf(sbomwriter.Options{SpecVersion: Version16, Reproducible: true}), ReproducibleSerialNumber(built); got != want {
		t.Errorf("reproducible serial = %q, want the digest of the canonical BOM %q", got, want)
	}

	first := serialOf(sbomwriter.Options{SpecVersion: Version16})
	second := serialOf(sbomwriter.Options{SpecVersion: Version16})
	if !strings.HasPrefix(first, "urn:uuid:") || first == second {
		t.Errorf("two ordinary renders got %q and %q; want two distinct random urn:uuid serials", first, second)
	}

	if got := serialOf(sbomwriter.Options{SpecVersion: Version16, Reproducible: true, OmitIdentity: true}); got != "" {
		t.Errorf("OmitIdentity still wrote the serial %q", got)
	}
}

// TestTwoComponentsOfTheSameNameAndVersionGetDistinctIdentities: section 28.4
// decorates a second component of a name with its version, and two components
// of one name and one version would decorate to the same ref. The decoration
// escalates instead -- root digest, identity digest, counter -- so the
// document is written rather than refused, and the first component keeps the
// undecorated ref.
func TestTwoComponentsOfTheSameNameAndVersionGetDistinctIdentities(t *testing.T) {
	rootA := domain.FileID{Anchor: "project", RelPath: "third_party/a/zlib"}
	document := &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "app", Type: "application"},
		Components: []domain.Component{
			{ID: "c1", Name: "zlib", Version: "1.3", Type: "library"},
			{ID: "c2", Name: "zlib", Version: "1.3", Type: "library", Root: &rootA},
			{ID: "c3", Name: "zlib", Version: "1.3", Type: "library"},
			{ID: "c4", Name: "zlib", Version: "1.3", Type: "library", Root: &rootA},
		},
		Relations: []sbomwriter.Relation{{From: "product", To: []string{"c1", "c2", "c3", "c4"}}},
		Run:       sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", ToolVersion: "0.0.0-test"},
	}
	bom, err := (Writer{}).Build(document, sbomwriter.Options{SpecVersion: Version16, Reproducible: true})
	if err != nil {
		t.Fatalf("two components of one name and version were refused: %v", err)
	}
	refs := map[string]bool{}
	for _, component := range bom.Components {
		refs[component.BomRef] = true
	}
	// c1 bare, c2 by version, c3 escalates past the taken version form to its
	// identity digest (it has no root), c4 escalates past the version form to
	// its root digest.
	expected := []string{
		"component:zlib",
		"component:zlib@1.3",
		"component:zlib#" + sbommap.ShortDigest("c3"),
		"component:zlib#" + sbommap.ShortDigest(rootA.Canonical()),
	}
	got := make([]string, 0, len(refs))
	for ref := range refs {
		got = append(got, ref)
	}
	if len(got) != 4 {
		t.Fatalf("refs = %v, want four distinct ones", got)
	}
	for _, ref := range expected {
		if !refs[ref] {
			t.Errorf("missing ref %q in %v", ref, got)
		}
	}
	serialized, err := MarshalBOM(bom)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate([]byte(serialized)); err != nil {
		t.Fatalf("the escalated document is not valid: %v", err)
	}
}

// TestPreflightRefusesATLPOnlyWhereTheVersionHasNoPlaceForIt: the same rule
// Build applies, asked before discovery and worded as the command line always
// worded it.
func TestPreflightRefusesATLPOnlyWhereTheVersionHasNoPlaceForIt(t *testing.T) {
	err := (Writer{}).Preflight(Version16, sbomwriter.Options{TLP: "AMBER"})
	if err == nil || err.Error() != "output.tlp needs CycloneDX 1.7; this run writes 1.6" {
		t.Errorf("Preflight at 1.6 = %v", err)
	}
	if err := (Writer{}).Preflight(Version17, sbomwriter.Options{TLP: "AMBER"}); err != nil {
		t.Errorf("Preflight at 1.7 = %v", err)
	}
	if err := (Writer{}).Preflight(Version16, sbomwriter.Options{}); err != nil {
		t.Errorf("Preflight without a TLP = %v", err)
	}
}

// TestComponentFilesReadsTheDependencyCascade: the format-specific half of
// explain --component --sbom, by bom-ref and by name, and "not in there" as a
// distinct answer from "cannot be read".
func TestComponentFilesReadsTheDependencyCascade(t *testing.T) {
	data, err := MarshalDocument(sampleDocument(), sbomwriter.Options{SpecVersion: Version16, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mbedtls", "component:mbedtls"} {
		files, err := (Writer{}).ComponentFiles(data, name)
		if err != nil {
			t.Fatalf("ComponentFiles(%q) = %v", name, err)
		}
		if !reflect.DeepEqual(files, []string{"pkg:conan/mbedtls:aes.h"}) {
			t.Errorf("ComponentFiles(%q) = %v", name, files)
		}
	}
	if _, err := (Writer{}).ComponentFiles(data, "absent"); !errors.Is(err, sbomwriter.ErrNoSuchComponent) {
		t.Errorf("an absent component = %v, want ErrNoSuchComponent", err)
	}
	if _, err := (Writer{}).ComponentFiles([]byte("not json"), "mbedtls"); err == nil || errors.Is(err, sbomwriter.ErrNoSuchComponent) {
		t.Errorf("an unreadable document = %v, want a read error", err)
	}
}

// TestTheWriterDescribesItself pins the answers the command line builds its
// messages and default names from, and that the schema it serves through the
// interface is the one it validates against.
func TestTheWriterDescribesItself(t *testing.T) {
	var writer sbomwriter.Writer = Writer{}
	describer, ok := writer.(sbomwriter.Describer)
	if !ok {
		t.Fatal("the CycloneDX writer does not describe itself")
	}
	if describer.Label() != "CycloneDX" || describer.Extension() != ".cdx.json" {
		t.Errorf("Label, Extension = %q, %q", describer.Label(), describer.Extension())
	}
	if !describer.OmitsTimestamp(Version16, sbomwriter.Options{Reproducible: true}) ||
		describer.OmitsTimestamp(Version16, sbomwriter.Options{}) {
		t.Error("OmitsTimestamp does not follow reproducible")
	}
	if _, ok := writer.(sbomwriter.OutputChecker); ok {
		t.Error("the CycloneDX writer checks its output in Validate already; a second checker would run it twice")
	}
	provider := writer.(sbomwriter.SchemaProvider)
	for _, version := range []string{"", Version16, Version17} {
		served, err := provider.EmbeddedSchema(version)
		if err != nil {
			t.Fatal(err)
		}
		direct, _ := EmbeddedSchema(version)
		if !bytes.Equal(served, []byte(direct)) {
			t.Errorf("the schema served for %q differs from the embedded one", version)
		}
	}
}
