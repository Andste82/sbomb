package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSHA3Sum256MatchesTheStandardVectors(t *testing.T) {
	vectors := []struct {
		message string
		digest  string
	}{
		{"", "a7ffc6f8bf1ed76651c14756a061d662f580ff4de43b49fa82d80a4b80f8434a"},
		{"abc", "3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532"},
		{
			"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq",
			"41c0dba2a9d6240849100376a8235e2c82e1b9998a999e21db32dd97496d3376",
		},
		{
			// Longer than the 136-byte sponge rate, so the absorb loop runs.
			"abcdefghbcdefghicdefghijdefghijkefghijklfghijklmghijklmnhijklmnoijklmnopjklmnopqklmnopqrlmnopqrsmnopqrstnopqrstu",
			"916f6061fe879741ca6469b43971dfdb28b1a32dc36cb3254e812be27aad1d18",
		},
	}
	for _, vector := range vectors {
		if got := hex.EncodeToString(sha3Sum256([]byte(vector.message))); got != vector.digest {
			t.Errorf("sha3-256 of a %d-byte message = %s, want %s", len(vector.message), got, vector.digest)
		}
	}
}

// The corpus is the real regression test for the digest: every document CMake
// wrote into a reply directory is named for its own SHA3-256, so recomputing
// the name of every one of them checks the implementation against thousands of
// inputs of every length.
func TestReplyHashReproducesTheCorpusFileNames(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "fixtures")
	checked := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		if filepath.Base(filepath.Dir(path)) != "reply" {
			return nil
		}
		match := contentAddressedName.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if got := replyHash(content); got != match[2] {
			t.Errorf("%s: recomputed digest %s", path, got)
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no content-addressed reply document found in the corpus")
	}
	t.Logf("checked %d reply documents", checked)
}

func TestSortDependenciesOrdersTheArrayAndTouchesNothingElse(t *testing.T) {
	const document = "{\n\t\"artifacts\" : \n\t[\n\t\t{\n\t\t\t\"path\" : \"app\"\n\t\t}\n\t],\n" +
		"\t\"dependencies\" : \n\t[\n" +
		"\t\t{\n\t\t\t\"backtrace\" : 6,\n\t\t\t\"id\" : \"zeta::@abc\"\n\t\t},\n" +
		"\t\t{\n\t\t\t\"id\" : \"alpha::@abc\"\n\t\t},\n" +
		"\t\t{\n\t\t\t\"backtrace\" : 5,\n\t\t\t\"id\" : \"mid::@abc\"\n\t\t}\n" +
		"\t],\n\t\"name\" : \"app\"\n}\n"

	sorted, err := sortDependencies([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	got := string(sorted)
	if want := strings.Index(got, "alpha"); want < 0 || want > strings.Index(got, "mid") {
		t.Fatalf("dependencies not sorted:\n%s", got)
	}
	if strings.Index(got, "mid") > strings.Index(got, "zeta") {
		t.Fatalf("dependencies not sorted:\n%s", got)
	}
	// The backtrace travels with the element it belongs to.
	if !strings.Contains(got, "\t\t{\n\t\t\t\"backtrace\" : 6,\n\t\t\t\"id\" : \"zeta::@abc\"\n\t\t}") {
		t.Fatalf("an element was reshaped:\n%s", got)
	}
	if len(sorted) != len(document) {
		t.Fatalf("length changed: %d, want %d", len(sorted), len(document))
	}
	// Sorting an already sorted document is a no-op.
	again, err := sortDependencies(sorted)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != got {
		t.Fatal("sorting is not idempotent")
	}
}

func TestSortDependenciesLeavesADocumentWithoutOneAlone(t *testing.T) {
	const document = "{\n\t\"name\" : \"lib\"\n}\n"
	sorted, err := sortDependencies([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if string(sorted) != document {
		t.Fatalf("document changed: %q", sorted)
	}
}
