package generate

import (
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/config"
)

func TestRunBuildsGraphAndReportsMissingEvidence(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "fixtures", "gcc-13", "p02-static")
	result, err := Run(config.Config{}, fixture, true)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Graph == nil || len(result.Graph.Nodes()) == 0 {
		t.Fatal("Run did not create an evidence graph")
	}
	if result.BOM.Metadata == nil || result.BOM.Metadata.Timestamp != "" {
		t.Fatal("reproducible BOM should omit metadata timestamp")
	}
	found := false
	for _, finding := range result.Findings {
		if finding.ID == "MISSING_LINK_EVIDENCE" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected missing link evidence finding")
	}
}
