package sbommap

import (
	"reflect"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

// TestIdentifiersAreTheIdentitiesOfSection28_4: every kind of element gets the
// identity the scheme names, and a Relation resolves through the table.
func TestIdentifiersAreTheIdentitiesOfSection28_4(t *testing.T) {
	root := domain.FileID{Anchor: "build", RelPath: "_deps/zlib-src"}
	document := &sbomwriter.Document{
		Product:   domain.Component{ID: "product", Name: "My App"},
		Artifacts: []domain.Component{{ID: "build:bin/app"}},
		Components: []domain.Component{
			{ID: "c1", Name: "zlib"},
			{ID: "c2", Name: "zlib", Version: "1.3.1"},
			{ID: "c3", Name: "zlib", Root: &root},
			{ID: "c4", Name: "zlib"},
		},
		Files: []domain.UsedFile{{ID: domain.FileID{Anchor: "source", RelPath: "src/main.c"}}},
	}
	table, err := Identifiers(document)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		table.Product():                            "product:my-app",
		table.Component("build:bin/app"):           "artifact:build:bin/app",
		table.Component("c1"):                      "component:zlib",
		table.Component("c2"):                      "component:zlib@1.3.1",
		table.Component("c3"):                      "component:zlib#" + ShortDigest(root.Canonical()),
		table.Component("c4"):                      "component:zlib#" + ShortDigest("c4"),
		table.File("source:src/main.c"):            "file:source:src/main.c",
		FileIdentity("source:src/main.c"):          "file:source:src/main.c",
		mustResolve(t, table, "product"):           "product:my-app",
		mustResolve(t, table, "source:src/main.c"): "file:source:src/main.c",
	}
	for got, want := range checks {
		if got != want {
			t.Errorf("identity %q, want %q", got, want)
		}
	}
	if _, ok := table.Resolve("nothing"); ok {
		t.Error("an identity the document does not carry resolved")
	}
}

func mustResolve(t *testing.T, table *Table, id string) string {
	t.Helper()
	ref, ok := table.Resolve(id)
	if !ok {
		t.Fatalf("%q did not resolve", id)
	}
	return ref
}

// TestTwoComponentsOfTheSameNameAndVersionGetDistinctIdentities: the version
// decoration is taken, so the second escalates -- to the root digest when it
// has a root, to the identity digest otherwise.
func TestTwoComponentsOfTheSameNameAndVersionGetDistinctIdentities(t *testing.T) {
	root := domain.FileID{Anchor: "build", RelPath: "a"}
	document := &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "app"},
		Components: []domain.Component{
			{ID: "c1", Name: "zlib", Version: "1.3"},
			{ID: "c2", Name: "zlib", Version: "1.3"},
			{ID: "c3", Name: "zlib", Version: "1.3"},
			{ID: "c4", Name: "zlib", Version: "1.3", Root: &root},
		},
	}
	table, err := Identifiers(document)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{table.Component("c1"), table.Component("c2"), table.Component("c3"), table.Component("c4")}
	want := []string{
		"component:zlib",
		"component:zlib@1.3",
		"component:zlib#" + ShortDigest("c3"),
		"component:zlib#" + ShortDigest(root.Canonical()),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("identities = %v, want %v", got, want)
	}
}

// TestTheCounterIsTheLastResort: when the root and identity digests are taken
// too, a counter in walk order separates what is left.
func TestTheCounterIsTheLastResort(t *testing.T) {
	table := newTable()
	same := domain.Component{ID: "same", Name: "zlib", Version: "1.3"}
	got := []string{table.forComponent(same), table.forComponent(same), table.forComponent(same), table.forComponent(same)}
	want := []string{
		"component:zlib",
		"component:zlib@1.3",
		"component:zlib#" + ShortDigest("same"),
		"component:zlib#" + ShortDigest("same") + "~2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("identities = %v, want %v", got, want)
	}
}

// TestTheIdentifierTableIsBuiltInOneFixedOrder: the components are walked in
// the order the document lists them, so swapping two of one name swaps which
// one keeps the undecorated identity -- and nothing else does.
func TestTheIdentifierTableIsBuiltInOneFixedOrder(t *testing.T) {
	a := domain.Component{ID: "a", Name: "zlib", Version: "1"}
	b := domain.Component{ID: "b", Name: "zlib", Version: "2"}
	first, err := Identifiers(&sbomwriter.Document{Components: []domain.Component{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Identifiers(&sbomwriter.Document{Components: []domain.Component{b, a}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Component("a") != "component:zlib" || second.Component("b") != "component:zlib" {
		t.Errorf("the first component of a name must keep the bare identity: %q, %q", first.Component("a"), second.Component("b"))
	}
	again, _ := Identifiers(&sbomwriter.Document{Components: []domain.Component{a, b}})
	if again.Component("b") != first.Component("b") {
		t.Error("the same document gave two different tables")
	}
}

func TestTwoFilesOfOneIdentityAreACollision(t *testing.T) {
	file := domain.UsedFile{ID: domain.FileID{Anchor: "source", RelPath: "a.c"}}
	_, err := Identifiers(&sbomwriter.Document{Files: []domain.UsedFile{file, file}})
	if err == nil || !strings.Contains(err.Error(), "file:source:a.c") {
		t.Errorf("err = %v, want a collision on the file", err)
	}
}

// TestComponentPropertiesAreWhatTheCycloneDXWriterEmits: the set is built
// from the fields the CycloneDX writer always rendered as properties and the
// builder's own map, and only the sbomb namespace of that map.
func TestComponentPropertiesAreWhatTheCycloneDXWriterEmits(t *testing.T) {
	component := domain.Component{
		Originator:    "Upstream Ltd",
		CVEExclusions: []domain.CVEExclusion{{CVE: "CVE-2024-1"}, {CVE: "CVE-2024-2", Reason: "not built"}},
		Scope:         "third-party",
		Properties: map[string][]string{
			"sbomb:component:linkageForm": {"static-object", "dynamic"},
			"finding":                     {"MISSING_FILE_HASH"},
		},
	}
	got := ComponentProperties(component)
	SortProperties(got)
	want := []Property{
		{"sbomb:cdx:archiveProperty", "no-archive"},
		{"sbomb:cdx:executableProperty", "non-executable"},
		{"sbomb:cdx:structuredProperty", "structured"},
		{"sbomb:component:cveExclusion", "CVE-2024-1"},
		{"sbomb:component:cveExclusion", "CVE-2024-2: not built"},
		{"sbomb:component:linkageForm", "dynamic"},
		{"sbomb:component:linkageForm", "static-object"},
		{"sbomb:component:originator", "Upstream Ltd"},
		{"sbomb:component:scope", "third-party"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("properties =\n%v\nwant\n%v", got, want)
	}
	vcs := VCSProperties(&domain.VCSRecord{URL: "https://x", Commit: "abc", Dirty: true})
	if !reflect.DeepEqual(vcs, []Property{{"sbomb:component:vcsCommit", "abc"}, {"sbomb:component:vcsDirty", "true"}}) {
		t.Errorf("vcs properties = %v", vcs)
	}
}

func TestOnlyTheSbombNamespaceReachesADocument(t *testing.T) {
	file := domain.UsedFile{
		ID:         domain.FileID{Anchor: "source", RelPath: "a.c"},
		Class:      domain.FileClassSource,
		Properties: map[string][]string{"finding": {"MISSING_FILE_HASH"}, "sbomb:file:linkageForm": {"static-object"}},
	}
	for _, property := range FileProperties(file) {
		if !strings.HasPrefix(property.Name, "sbomb:") {
			t.Errorf("internal key %q reached the property set", property.Name)
		}
	}
}

func TestAPropertyNameOutsideTheNamespaceIsRefused(t *testing.T) {
	for _, name := range []string{"", "finding", "cdx:foo"} {
		if ValidatePropertyName(name) == nil {
			t.Errorf("%q was accepted", name)
		}
	}
	if err := ValidatePropertyName("sbomb:component:scope"); err != nil {
		t.Error(err)
	}
}

// TestTheDocumentUUIDIsStableAndCoversItsInput: one input, one identity; any
// change of the input, another.
func TestTheDocumentUUIDIsStableAndCoversItsInput(t *testing.T) {
	a := DocumentUUID([]byte("one"))
	if a != DocumentUUID([]byte("one")) {
		t.Error("the identity of one input moved")
	}
	if a == DocumentUUID([]byte("two")) {
		t.Error("two inputs share an identity")
	}
	if len(a) != 36 || a[14] != '5' {
		t.Errorf("%q is not a version 5 UUID", a)
	}
}

func TestACuratedOrOverriddenLicenceIsCurated(t *testing.T) {
	if !CuratedLicense(domain.Component{Licenses: []domain.LicenseFinding{{Source: "curated"}}}) {
		t.Error("a curated value is curated")
	}
	if !CuratedLicense(domain.Component{Licenses: []domain.LicenseFinding{{Reason: "conflicting-evidence", Source: "LICENSE"}}}) {
		t.Error("a resolved conflict is curated")
	}
	if CuratedLicense(domain.Component{Licenses: []domain.LicenseFinding{{Source: "LICENSE"}}}) {
		t.Error("a licence read from a file is not curated")
	}
	if CuratedLicense(domain.Component{}) {
		t.Error("no licence is not a curated one")
	}
}
