package licenselist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The CycloneDX writer decides what goes into license.id from the enum of the
// SPDX schema CycloneDX ships, and the SPDX writer decides what needs a
// LicenseRef from this list. Both are generated from a licence-list release;
// if they ever name different releases, one build would be described as
// carrying a listed licence in one format and an unlisted one in the other.
func TestTheListIsTheOneTheCycloneDXSchemaCarries(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "cyclonedx", "schema", "spdx.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Comment string   `json:"$comment"`
		Enum    []string `json:"enum"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(schema.Comment, "-"+Version) {
		t.Errorf("the CycloneDX SPDX schema is %q, this list is %s", schema.Comment, Version)
	}
	ours := append(Licenses(), Exceptions()...)
	sort.Strings(ours)
	theirs := append([]string(nil), schema.Enum...)
	sort.Strings(theirs)
	if strings.Join(ours, "\n") != strings.Join(theirs, "\n") {
		t.Errorf("the licence list (%d entries) and the CycloneDX enum (%d entries) differ", len(ours), len(theirs))
	}
}

func TestAnExceptionIsNotALicence(t *testing.T) {
	if !KnownException("LLVM-exception") || Known("LLVM-exception") {
		t.Error("LLVM-exception must be an exception and not a licence")
	}
	if !Known("Apache-2.0") || KnownException("Apache-2.0") {
		t.Error("Apache-2.0 must be a licence and not an exception")
	}
	if Known("mit") {
		t.Error("Known is exact; \"mit\" is not the list's spelling of \"MIT\"")
	}
	if Known("LicenseRef-acme") {
		t.Error("a LicenseRef is never on the list")
	}
}

// Annex B: identifiers "should be matched in a case-insensitive manner". The
// canonical lookups find an identifier in any case and return the list's
// spelling, and keep licences and exceptions apart as Known does.
func TestAnIdentifierIsFoundWithoutCaseAndSpelledAsTheListSpellsIt(t *testing.T) {
	for _, test := range []struct{ id, want string }{
		{"mit", "MIT"},
		{"MIT", "MIT"},
		{"apache-2.0", "Apache-2.0"},
		{"GPL-2.0-ONLY", "GPL-2.0-only"},
	} {
		if got, ok := Canonical(test.id); !ok || got != test.want {
			t.Errorf("Canonical(%q) = %q, %v; want %q", test.id, got, ok, test.want)
		}
	}
	if got, ok := CanonicalException("classpath-exception-2.0"); !ok || got != "Classpath-exception-2.0" {
		t.Errorf("CanonicalException(classpath-exception-2.0) = %q, %v", got, ok)
	}
	if _, ok := Canonical("llvm-exception"); ok {
		t.Error("an exception is not a licence in any case")
	}
	if _, ok := CanonicalException("mit"); ok {
		t.Error("a licence is not an exception in any case")
	}
	if _, ok := Canonical("acme-1.0"); ok {
		t.Error("an identifier off the list is off it in every case")
	}
}

// The case-insensitive lookup folds each table into a map; two entries that
// differed only in case would make one of them unreachable.
func TestNoTwoIdentifiersDifferOnlyInCase(t *testing.T) {
	for _, table := range [][]string{licenses, exceptions} {
		seen := map[string]string{}
		for _, id := range table {
			if other, dup := seen[strings.ToLower(id)]; dup {
				t.Errorf("%q and %q differ only in case", other, id)
			}
			seen[strings.ToLower(id)] = id
		}
	}
}

func TestTheTablesAreSortedForTheBinarySearch(t *testing.T) {
	if !sort.StringsAreSorted(licenses) || !sort.StringsAreSorted(exceptions) {
		t.Fatal("the generated tables must be sorted bytewise, or Known misses entries")
	}
}
