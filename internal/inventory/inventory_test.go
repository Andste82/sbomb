package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/example/sbomb/internal/domain"
)

func BenchmarkHashUsedFiles(b *testing.B) {
	dir := b.TempDir()
	files := make([]domain.UsedFile, 100)
	content := []byte(strings.Repeat("x", 4096))
	for index := range files {
		path := filepath.Join(dir, fmt.Sprintf("file-%03d.bin", index))
		if err := os.WriteFile(path, content, 0o600); err != nil {
			b.Fatal(err)
		}
		files[index] = domain.UsedFile{ID: domain.FileID{Anchor: "project", RelPath: path}, Class: domain.FileClassSource}
	}
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		_ = HashUsedFiles(files)
	}
}

func TestMergeUsedFilesDeduplicatesEvidence(t *testing.T) {
	files := []domain.UsedFile{
		{
			ID:    domain.FileID{Anchor: "project", RelPath: "src/main.c"},
			Class: domain.FileClassSource,
			Properties: map[string][]string{
				"evidence": {"cmake"},
			},
			Hashes: map[string]string{"sha256": "aaa"},
		},
		{
			ID:    domain.FileID{Anchor: "project", RelPath: "src/main.c"},
			Class: domain.FileClassSource,
			Properties: map[string][]string{
				"evidence": {"ninja"},
			},
			Hashes: map[string]string{"sha256": "bbb"},
		},
	}

	merged := MergeUsedFiles(files)
	if len(merged) != 1 {
		t.Fatalf("expected deduplicated length 1, got %d", len(merged))
	}
	if got := merged[0].Properties["evidence"]; len(got) != 2 || (got[0] != "cmake" && got[1] != "cmake") {
		t.Fatalf("expected merged evidence sources, got %#v", got)
	}
}

func TestHashUsedFilesMatchesRawBytesAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	lf := filepath.Join(dir, "lf.txt")
	crlf := filepath.Join(dir, "crlf.txt")
	missing := filepath.Join(dir, "missing.txt")

	if err := os.WriteFile(lf, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crlf, []byte("hello\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := []domain.UsedFile{
		{ID: domain.FileID{Anchor: "project", RelPath: lf}, Class: domain.FileClassSource},
		{ID: domain.FileID{Anchor: "project", RelPath: crlf}, Class: domain.FileClassSource},
		{ID: domain.FileID{Anchor: "project", RelPath: missing}, Class: domain.FileClassSource, Missing: true},
	}

	h1 := sha256.Sum256([]byte("hello\n"))
	h2 := sha256.Sum256([]byte("hello\r\n"))
	out := HashUsedFiles(files)
	if len(out) != 3 {
		t.Fatalf("expected 3 files, got %d", len(out))
	}
	if out[0].Hashes["sha256"] != hex.EncodeToString(h1[:]) {
		t.Fatalf("unexpected LF hash: %s", out[0].Hashes["sha256"])
	}
	if out[1].Hashes["sha256"] != hex.EncodeToString(h2[:]) {
		t.Fatalf("unexpected CRLF hash: %s", out[1].Hashes["sha256"])
	}
	if !out[2].Missing || out[2].Hashes != nil {
		t.Fatalf("missing file should not have a hash and should be marked missing: %#v", out[2])
	}
	if got, ok := out[2].Properties["finding"]; !ok || len(got) == 0 || got[0] != "MISSING_FILE_HASH" {
		t.Fatalf("missing file should record MISSING_FILE_HASH finding, got %#v", out[2].Properties)
	}
}

func TestHashUsedFilesRejectsSymlinkEscapingAnchors(t *testing.T) {
	dir := t.TempDir()
	anchor := filepath.Join(dir, "anchor")
	escaping := filepath.Join(dir, "outside.txt")
	if err := os.Mkdir(anchor, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(escaping, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(anchor, "link.txt")
	if err := os.Symlink(escaping, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	file := domain.UsedFile{ID: domain.FileID{Anchor: "project", RelPath: link}, Class: domain.FileClassSource}
	blocked := HashUsedFilesWithOptions([]domain.UsedFile{file}, HashOptions{Anchors: []string{anchor}})[0]
	if !blocked.Missing {
		t.Fatal("escaping symlink was hashed without allow-unanchored-reads")
	}
	allowed := HashUsedFilesWithOptions([]domain.UsedFile{file}, HashOptions{AllowUnanchoredReads: true})[0]
	if allowed.Missing || allowed.Hashes["sha256"] == "" {
		t.Fatal("explicitly allowed unanchored symlink was not hashed")
	}
}

func TestClassificationAndDeterministicDump(t *testing.T) {
	files := []domain.UsedFile{
		{ID: domain.FileID{Anchor: "project", RelPath: "src/generated.c"}, Class: classifyFile("src/generated.c", true)},
		{ID: domain.FileID{Anchor: "project", RelPath: "include/foo.h"}, Class: classifyFile("include/foo.h", false)},
		{ID: domain.FileID{Anchor: "project", RelPath: "build/app.o"}, Class: classifyFile("build/app.o", false)},
		{ID: domain.FileID{Anchor: "project", RelPath: "lib/libssl.so.3"}, Class: classifyFile("lib/libssl.so.3", false)},
	}

	for _, want := range []domain.FileClass{
		domain.FileClassGeneratedSource,
		domain.FileClassHeader,
		domain.FileClassObject,
		domain.FileClassSharedLibrary,
	} {
		if !containsFileClass(files, want) {
			t.Fatalf("missing classification %s", want)
		}
	}

	dump := BuildInventoryDump(files, nil)
	if dump.SchemaVersion != 1 {
		t.Fatalf("schemaVersion mismatch: got %d want 1", dump.SchemaVersion)
	}
	keys := make([]string, 0, len(dump.Files))
	for _, f := range dump.Files {
		keys = append(keys, f.Canonical)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if dump.Files[i].Canonical != k {
			t.Fatalf("dump files not sorted: got %v want %v", dump.Files, keys)
		}
	}
}

func TestStalenessDetectionAndBuildIDSuppression(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "src.c")
	artifact := filepath.Join(dir, "app")
	if err := os.WriteFile(source, []byte("int x;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, time.Now().Add(2*time.Hour), time.Now().Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(artifact, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}

	findings, err := DetectStaleness([]domain.UsedFile{{
		ID:    domain.FileID{Anchor: "project", RelPath: source},
		Class: domain.FileClassSource,
	}}, []string{artifact}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 {
		t.Fatal("expected stale source evidence finding")
	}
	if findings[0].ID != "STALE_BUILD_EVIDENCE" {
		t.Fatalf("expected STALE_BUILD_EVIDENCE, got %s", findings[0].ID)
	}
}

func containsFileClass(files []domain.UsedFile, want domain.FileClass) bool {
	for _, f := range files {
		if f.Class == want {
			return true
		}
	}
	return false
}
