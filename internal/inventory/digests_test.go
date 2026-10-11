package inventory

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// A requested SHA-1 comes out of the read the SHA-256 needed: every readable
// file is opened exactly once, and both digests are of its bytes.
func TestARequestedDigestComesFromTheSameRead(t *testing.T) {
	files, options := observeFixture(t)
	plainReads := recordReads(t)
	plain := HashUsedFilesWithOptions(files, options)
	plainPaths := plainReads()

	withReads := recordReads(t)
	options.Digests = []string{HashAlgorithmSHA1}
	with := HashUsedFilesWithOptions(files, options)
	if got := withReads(); !reflect.DeepEqual(got, plainPaths) {
		t.Fatalf("asking for SHA-1 changed the reads:\n got %v\nwant %v", got, plainPaths)
	}
	seen := map[string]bool{}
	for _, path := range plainPaths {
		if seen[path] {
			t.Errorf("%s was read twice", path)
		}
		seen[path] = true
	}
	hashed := 0
	for index, file := range with {
		if file.Missing {
			if len(file.Hashes) != 0 {
				t.Errorf("%s is missing and has digests %v", file.ID.Canonical(), file.Hashes)
			}
			continue
		}
		hashed++
		if len(plain[index].Hashes) != 1 || plain[index].Hashes[HashAlgorithmSHA1] != "" {
			t.Errorf("%s without a request: %v, want SHA-256 alone", file.ID.Canonical(), plain[index].Hashes)
		}
		content := []byte("/* Copyright (c) 2026 " + file.ID.RelPath + " Holder */\n")
		sum1, sum256 := sha1.Sum(content), sha256.Sum256(content)
		if file.Hashes[HashAlgorithmSHA1] != hex.EncodeToString(sum1[:]) {
			t.Errorf("%s SHA-1 = %q", file.ID.Canonical(), file.Hashes[HashAlgorithmSHA1])
		}
		if file.Hashes[HashAlgorithmSHA256] != hex.EncodeToString(sum256[:]) {
			t.Errorf("%s SHA-256 = %q", file.ID.Canonical(), file.Hashes[HashAlgorithmSHA256])
		}
	}
	if hashed == 0 {
		t.Fatal("the fixture hashed nothing")
	}
}

// A known value, so that the digest is SHA-1 and not merely forty hex digits.
func TestTheSHA1OfAKnownContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "abc")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := domain.UsedFile{ID: domain.FileID{Anchor: "project", RelPath: "abc"}}
	out := HashUsedFilesWithOptions([]domain.UsedFile{file}, HashOptions{
		Anchors: []string{root},
		Resolve: func(domain.FileID) string { return path },
		Digests: []string{HashAlgorithmSHA1},
	})
	if got := out[0].Hashes[HashAlgorithmSHA1]; got != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Errorf("SHA-1 of \"abc\" = %q", got)
	}
}

// The dump states what the inventory knows, not what one writer asked for: a
// requested SHA-1 is left out, so that the dump of the same files is the same
// whichever format the run writes.
func TestTheDumpLeavesOutARequestedDigest(t *testing.T) {
	files, options := observeFixture(t)
	plain := BuildInventoryDump(HashUsedFilesWithOptions(files, options), nil)
	options.Digests = []string{HashAlgorithmSHA1}
	requested := HashUsedFilesWithOptions(files, options)
	if !reflect.DeepEqual(BuildInventoryDump(requested, nil), plain) {
		t.Errorf("asking for SHA-1 moved the dump:\n got %+v\nwant %+v", BuildInventoryDump(requested, nil), plain)
	}
	carried := false
	for _, file := range requested {
		carried = carried || file.Hashes[HashAlgorithmSHA1] != ""
	}
	if !carried {
		t.Fatal("no file carries the requested SHA-1, so the dump had nothing to leave out")
	}
}

// The inventory and the SPDX mapping check against one table.
func TestSupportsDigest(t *testing.T) {
	for name, want := range map[string]bool{"SHA-256": true, "SHA-1": true, "SHA1": false, "MD5": false, "": false} {
		if got := SupportsDigest(name); got != want {
			t.Errorf("SupportsDigest(%q) = %v, want %v", name, got, want)
		}
	}
}
