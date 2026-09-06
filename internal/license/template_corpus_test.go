package license

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCorpus measures the matcher against a directory of real licence files.
//
// It is the only honest way to know what detection is worth: an official SPDX
// text matches by construction, and every defect this package has had showed
// up on a file somebody actually shipped. It is skipped unless a corpus is
// named, because there is none to commit -- the files belong to other projects
// and there are thousands of them.
//
//	SBOMB_LICENSE_CORPUS=/path/to/dir go test ./internal/license/ -run TestCorpus -v
func TestCorpus(t *testing.T) {
	dir := os.Getenv("SBOMB_LICENSE_CORPUS")
	if dir == "" {
		t.Skip("set SBOMB_LICENSE_CORPUS to a directory of licence files")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var byDigest, byTemplate, ambiguous, unrecognized int
	counts := map[string]int{}
	for _, item := range entries {
		if item.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, item.Name()))
		if err != nil {
			continue
		}
		finding := ResolveFromText(string(data), item.Name())
		switch {
		case finding.Expression != "" && finding.Technique == TechniqueDigest:
			byDigest++
			counts[finding.Expression]++
		case finding.Expression != "" && finding.Technique == TechniqueTemplate:
			byTemplate++
			counts[finding.Expression]++
		case finding.Reason == ReasonConflictingEvidence:
			ambiguous++
			t.Logf("ambiguous: %s -> %v", item.Name(), finding.Conflicts)
		default:
			unrecognized++
		}
	}

	total := byDigest + byTemplate + ambiguous + unrecognized
	t.Logf("corpus of %d files: %d by digest, %d by template, %d ambiguous, %d unrecognized",
		total, byDigest, byTemplate, ambiguous, unrecognized)
	names := make([]string, 0, len(counts))
	for id := range counts {
		names = append(names, id)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	var top []string
	for _, id := range names[:min(8, len(names))] {
		top = append(top, fmt.Sprintf("%s=%d", id, counts[id]))
	}
	t.Logf("most common: %s", strings.Join(top, " "))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
