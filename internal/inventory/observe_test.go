package inventory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// recordReads replaces the one read of the hashing pass with a recording one
// for the duration of a test, and answers with the paths that were opened.
//
// Section 22.10 claims that the copyright extraction adds no read, and this is
// the mechanism that checks it rather than promising it: the same run is made
// twice, once with the observer and once without, and the two sets of paths
// are compared.
func recordReads(t *testing.T) func() []string {
	t.Helper()
	original := readFile
	var mutex sync.Mutex
	var opened []string
	readFile = func(path string) ([]byte, error) {
		mutex.Lock()
		opened = append(opened, path)
		mutex.Unlock()
		return original(path)
	}
	t.Cleanup(func() { readFile = original })
	return func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		out := make([]string, len(opened))
		copy(out, opened)
		sort.Strings(out)
		return out
	}
}

func observeFixture(t *testing.T) ([]domain.UsedFile, HashOptions) {
	t.Helper()
	root := t.TempDir()
	files := make([]domain.UsedFile, 0, 8)
	physical := map[string]string{}
	for _, name := range []string{"a.c", "b.c", "c.h", "sub/d.c", "sub/e.h", "LICENSE", "missing.c", "dir.c"} {
		id := domain.FileID{Anchor: "project", RelPath: name}
		path := filepath.Join(root, name)
		switch name {
		case "missing.c":
			// Never created: an unreadable file is hashed by nobody and
			// observed by nobody.
		case "dir.c":
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		default:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("/* Copyright (c) 2026 "+name+" Holder */\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		physical[id.Canonical()] = path
		files = append(files, domain.UsedFile{ID: id})
	}
	return files, HashOptions{
		Anchors: []string{root},
		Resolve: func(id domain.FileID) string { return physical[id.Canonical()] },
		Jobs:    4,
	}
}

// The property the milestone states: the run opens no file it did not open
// before. Extraction rides on the read the digest already needed, so the path
// set with an observer is the path set without one -- not merely a similar
// number of reads, the same paths.
func TestObservingAddsNoRead(t *testing.T) {
	files, options := observeFixture(t)

	withoutReads := recordReads(t)
	HashUsedFilesWithOptions(files, options)
	without := withoutReads()

	var seen []string
	var mutex sync.Mutex
	observed := options
	observed.Observe = func(id domain.FileID, data []byte) {
		mutex.Lock()
		defer mutex.Unlock()
		seen = append(seen, id.Canonical())
		if len(data) == 0 {
			t.Errorf("%s was observed with no bytes", id.Canonical())
		}
	}
	withReads := recordReads(t)
	HashUsedFilesWithOptions(files, observed)
	with := withReads()

	if strings.Join(with, "\n") != strings.Join(without, "\n") {
		t.Fatalf("the observed run opened\n%v\nand the plain run opened\n%v", with, without)
	}
	// One read per readable file, and the unreadable ones are neither read nor
	// observed.
	sort.Strings(seen)
	want := []string{
		"project:LICENSE", "project:a.c", "project:b.c", "project:c.h",
		"project:sub/d.c", "project:sub/e.h",
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("observed %v, want one call per readable file %v", seen, want)
	}
	if len(with) != len(want) {
		t.Errorf("opened %d path(s), want %d: %v", len(with), len(want), with)
	}
}

// The hashes are what the document is made of, and an observer may not change
// one of them.
func TestObservingChangesNoHash(t *testing.T) {
	files, options := observeFixture(t)
	plain := HashUsedFilesWithOptions(files, options)

	observed := options
	observed.Observe = func(domain.FileID, []byte) {}
	withObserver := HashUsedFilesWithOptions(files, observed)

	for i := range plain {
		if plain[i].Hashes[HashAlgorithmSHA256] != withObserver[i].Hashes[HashAlgorithmSHA256] {
			t.Errorf("%s hashed differently with an observer", plain[i].ID.Canonical())
		}
		if plain[i].Missing != withObserver[i].Missing || plain[i].SizeBytes != withObserver[i].SizeBytes {
			t.Errorf("%s was recorded differently with an observer", plain[i].ID.Canonical())
		}
	}
}
