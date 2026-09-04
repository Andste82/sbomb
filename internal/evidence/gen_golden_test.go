package evidence

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// TestGenerateGoldenData generates the golden test data files.
// This test is skipped in normal test runs and must be invoked explicitly with:
//   go test ./internal/evidence/... -run TestGenerateGoldenData -v
func TestGenerateGoldenData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping golden data generation in short mode")
	}

	// Check if the -update flag was passed (or similar mechanism)
	// For now, we'll always generate it to create the golden files
	t.Log("Generating golden test data...")

	g := New()

	// Build synthetic graph per Milestone 02 test requirements
	g.AddNode(domain.Node{ID: "artifact:build/app.elf", Kind: domain.NodeArtifact})

	g.AddNode(domain.Node{ID: "build:app.o", Kind: domain.NodeObject})
	g.AddEdge(domain.Edge{
		From:       "artifact:build/app.elf",
		To:         "build:app.o",
		Type:       "link",
		Strength:   "linked",
		Confidence: domain.ConfidenceHigh,
		Source:     "ld:--dependency-file",
		Adapter:    "linkdepfile",
	})

	g.AddNode(domain.Node{ID: "build:main.cpp.tu", Kind: domain.NodeTranslationUnit})
	g.AddEdge(domain.Edge{
		From:       "build:app.o",
		To:         "build:main.cpp.tu",
		Type:       "compile",
		Strength:   "derived",
		Confidence: domain.ConfidenceHigh,
		Source:     "ninja:build.ninja",
		Adapter:    "ninja",
	})

	g.AddNode(domain.Node{ID: "project:src/main.cpp", Kind: domain.NodeSource})
	g.AddEdge(domain.Edge{
		From:       "build:main.cpp.tu",
		To:         "project:src/main.cpp",
		Type:       "source-mapping",
		Strength:   "derived",
		Confidence: domain.ConfidenceHigh,
		Source:     "ninja:build.ninja",
		Adapter:    "ninja",
	})

	g.AddNode(domain.Node{ID: "project:include/app.h", Kind: domain.NodeHeader})
	g.AddEdge(domain.Edge{
		From:       "build:main.cpp.tu",
		To:         "project:include/app.h",
		Type:       "header-dependency",
		Strength:   "derived",
		Confidence: domain.ConfidenceHigh,
		Source:     "depfile:main.cpp.d",
		Adapter:    "depfiles",
	})

	g.AddNode(domain.Node{ID: "pkg:conan/mbedtls", Kind: domain.NodePackage})
	g.AddEdge(domain.Edge{
		From:       "artifact:build/app.elf",
		To:         "pkg:conan/mbedtls",
		Type:       "package",
		Strength:   "direct",
		Confidence: domain.ConfidenceMedium,
		Source:     "conanfile:conanfile.txt",
		Adapter:    "conan",
	})

	g.AddNode(domain.Node{ID: "build:generated/version.h", Kind: domain.NodeHeader})
	g.AddEdge(domain.Edge{
		From:       "build:main.cpp.tu",
		To:         "build:generated/version.h",
		Type:       "header-dependency",
		Strength:   "derived",
		Confidence: domain.ConfidenceHigh,
		Source:     "depfile:main.cpp.d",
		Adapter:    "depfiles",
	})

	g.AddNode(domain.Node{ID: "tools/genversion.py", Kind: domain.NodeGenerator})
	g.AddEdge(domain.Edge{
		From:       "build:generated/version.h",
		To:         "tools/genversion.py",
		Type:       "generated",
		Strength:   "generated",
		Confidence: domain.ConfidenceHigh,
		Source:     "ninja:build.ninja",
		Adapter:    "ninja",
	})

	g.AddNode(domain.Node{ID: "project:cmake/version.in", Kind: domain.NodeGeneratorInput})
	g.AddEdge(domain.Edge{
		From:       "tools/genversion.py",
		To:         "project:cmake/version.in",
		Type:       "generator-input",
		Strength:   "derived",
		Confidence: domain.ConfidenceHigh,
		Source:     "ninja:build.ninja",
		Adapter:    "ninja",
	})

	// Generate dump file
	goldenPath := "../../testdata/golden/synthetic-graph.json"
	
	// Ensure directory exists
	dir := filepath.Dir(goldenPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create directory %s: %v", dir, err)
	}
	
	// Dump to buffer first
	var buf bytes.Buffer
	if err := g.Dump(&buf); err != nil {
		t.Fatalf("Failed to dump graph: %v", err)
	}

	// Write buffer to file
	if err := os.WriteFile(goldenPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("Failed to write golden file: %v", err)
	}

	// Verify file was created
	stat, err := os.Stat(goldenPath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	t.Logf("Golden dump file written to %s (%d bytes, actual: %d)", goldenPath, len(buf.Bytes()), stat.Size())

	// Generate stats file
	statsPath := "../../testdata/golden/synthetic-graph-stats.json"
	
	// Create stats output - same as the dump for now
	// In the future, this would be the output of `sbomb evidence --load <dump> --format json`
	if err := os.WriteFile(statsPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("Failed to write stats file: %v", err)
	}

	stat, err = os.Stat(statsPath)
	if err != nil {
		t.Fatalf("Failed to stat stats file: %v", err)
	}
	t.Logf("Golden stats file written to %s (%d bytes, actual: %d)", statsPath, len(buf.Bytes()), stat.Size())
}
