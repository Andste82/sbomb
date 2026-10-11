package spdx23

import (
	"math/rand"
	"testing"
)

func TestElementIDsRoundTrip(t *testing.T) {
	for local, want := range map[string]string{
		"file:project:src/main.c":   "SPDXRef-file-3Aproject-3Asrc-2Fmain.c",
		"component:zlib-ng":         "SPDXRef-component-3Azlib-2Dng",
		"component:kitchen@1.2.3":   "SPDXRef-component-3Akitchen-401.2.3",
		"file:project:assets/ü.png": "SPDXRef-file-3Aproject-3Aassets-2F-C3-BC.png",
	} {
		id := elementID(local)
		if id != want {
			t.Errorf("elementID(%q) = %q, want %q", local, id, want)
		}
		if !elementIDPattern.MatchString(id) {
			t.Errorf("%q does not match the SPDXID pattern", id)
		}
		if back, ok := LocalIdentity(id); !ok || back != local {
			t.Errorf("LocalIdentity(%q) = %q, %v", id, back, ok)
		}
	}
}

// The encoding is injective and reversible over arbitrary bytes, the escape
// character and the bytes of local identities included.
func TestElementIDsAreInjective(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	alphabet := []byte("aZ09.-:/_ @%\xc3\xbc\x00\xff")
	seen := map[string]string{}
	for round := 0; round < 20000; round++ {
		length := random.Intn(8)
		local := make([]byte, length)
		for index := range local {
			local[index] = alphabet[random.Intn(len(alphabet))]
		}
		id := elementID(string(local))
		if previous, known := seen[id]; known && previous != string(local) {
			t.Fatalf("%q and %q share %q", previous, local, id)
		}
		seen[id] = string(local)
		if length > 0 && !elementIDPattern.MatchString(id) {
			t.Fatalf("%q does not match the SPDXID pattern", id)
		}
		if back, ok := LocalIdentity(id); length > 0 && (!ok || back != string(local)) {
			t.Fatalf("LocalIdentity(%q) = %q, %v; want %q", id, back, ok, local)
		}
	}
}

// No local identity encodes to the document's own identifier, and nothing
// elementID cannot have written decodes.
func TestNoElementIsTheDocumentAndForeignIDsDoNotDecode(t *testing.T) {
	for _, local := range []string{"DOCUMENT", "product:DOCUMENT", "component:DOCUMENT"} {
		if local != "DOCUMENT" && elementID(local) == documentID {
			t.Errorf("%q encodes to the document", local)
		}
	}
	for _, id := range []string{documentID, "SPDXRef-", "SPDXRef-Package", "SPDXRef-a-3", "SPDXRef-a-3a", "SPDXRef-a-zz", "DocumentRef-x:SPDXRef-a", "urn:uuid:1#file:x"} {
		if local, ok := LocalIdentity(id); ok && elementID(local) != id {
			t.Errorf("LocalIdentity(%q) = %q, which does not encode back", id, local)
		}
	}
	for _, id := range []string{documentID, "SPDXRef-a-3", "SPDXRef-a-3a", "SPDXRef-a-zz", "DocumentRef-x:SPDXRef-a"} {
		if _, ok := LocalIdentity(id); ok {
			t.Errorf("%q decoded", id)
		}
	}
}
