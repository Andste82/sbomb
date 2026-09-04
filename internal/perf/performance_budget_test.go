//go:build perf

package perf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/sbomb/internal/adapters/linkers/mapparser"
	"github.com/example/sbomb/internal/cyclonedx"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	resolver "github.com/example/sbomb/internal/inventory"
)

func TestPerformanceBudget(t *testing.T) {
	const fileCount = 10000
	start := time.Now()
	graph := evidence.New()
	graph.AddNode(domain.Node{ID: "artifact:build/app.elf", Kind: domain.NodeArtifact})
	for index := 0; index < fileCount; index++ {
		object := domain.NodeID(fmt.Sprintf("build:obj/%05d.o", index))
		source := domain.NodeID(fmt.Sprintf("project:src/%05d.c", index))
		graph.AddNode(domain.Node{ID: object, Kind: domain.NodeObject})
		graph.AddNode(domain.Node{ID: source, Kind: domain.NodeSource})
		graph.AddEdge(domain.Edge{From: "artifact:build/app.elf", To: object, Type: "link", Strength: "linked", Confidence: domain.ConfidenceHigh, Source: "perf", Adapter: "perf"})
		graph.AddEdge(domain.Edge{From: object, To: source, Type: "source-mapping", Strength: "derived", Confidence: domain.ConfidenceHigh, Source: "perf", Adapter: "perf"})
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("10,000-file graph build took %s", elapsed)
	}

	dir := t.TempDir()
	files := make([]domain.UsedFile, fileCount)
	content := []byte("int main(void) { return 0; }\n")
	for index := range files {
		path := filepath.Join(dir, fmt.Sprintf("file-%05d.c", index))
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		files[index] = domain.UsedFile{ID: domain.FileID{Anchor: "project", RelPath: path}, Class: domain.FileClassSource}
	}
	start = time.Now()
	_ = resolver.HashUsedFiles(files)
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("10,000-file hashing took %s", elapsed)
	}

	mapInput := []byte("Memory Configuration\n" + " build/objects/main.o\n")
	if result := mapparser.Parse(strings.NewReader(string(mapInput)), "gnu-ld"); result.Err != nil {
		t.Fatal(result.Err)
	}
	if _, err := cyclonedx.MarshalBOM(cyclonedx.BOM{BomFormat: "CycloneDX", SpecVersion: "1.6", Version: 1}); err != nil {
		t.Fatal(err)
	}
}
