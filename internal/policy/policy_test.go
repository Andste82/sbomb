package policy

import (
	"testing"
	"time"

	"github.com/example/sbomb/internal/domain"
)

func TestEvaluateWaiverSuppressesFinding(t *testing.T) {
	findings := []domain.Finding{{
		ID:       "UNKNOWN_LICENSE",
		Severity: domain.SeverityWarning,
		Subject:  domain.Subject{Kind: "file", Ref: "project:dep/legacy_blob/foo.c"},
		Message:  "No component license could be determined.",
	}}

	cfg := DefaultConfig()
	cfg.FailOnUnknownLicense = true
	waivers := []Waiver{{
		ID:      "UNKNOWN_LICENSE",
		Subject: "project:dep/legacy_blob/*",
		Reason:  "approved",
		Expires: "2099-01-01",
	}}

	res := Evaluate(findings, cfg, waivers, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	if res.Fail {
		t.Fatal("policy should pass when the finding is waived")
	}
	if len(res.Findings) != 1 {
		t.Fatalf("expected exactly 1 finding after waiver handling, got %d", len(res.Findings))
	}
	if !res.Findings[0].Waived {
		t.Fatal("waived finding should be marked waived")
	}
}

func TestEvaluateExpiredWaiverAddsFinding(t *testing.T) {
	findings := []domain.Finding{{
		ID:       "UNKNOWN_LICENSE",
		Severity: domain.SeverityWarning,
		Subject:  domain.Subject{Kind: "file", Ref: "project:dep/legacy_blob/foo.c"},
		Message:  "No component license could be determined.",
	}}

	cfg := DefaultConfig()
	cfg.FailOnUnknownLicense = true
	waivers := []Waiver{{
		ID:      "UNKNOWN_LICENSE",
		Subject: "project:dep/legacy_blob/*",
		Reason:  "approved",
		Expires: "2020-01-01",
	}}

	res := Evaluate(findings, cfg, waivers, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	if !res.Fail {
		t.Fatal("expired waiver should not suppress the failure")
	}
	if len(res.Findings) != 2 {
		t.Fatalf("expected original finding + WAIVER_EXPIRED, got %d findings", len(res.Findings))
	}
	if res.Findings[1].ID != "WAIVER_EXPIRED" {
		t.Fatalf("expected WAIVER_EXPIRED, got %q", res.Findings[1].ID)
	}
}

func TestEvaluateUnusedWaiverAddsFinding(t *testing.T) {
	findings := []domain.Finding{{
		ID:       "MISSING_FILE_HASH",
		Severity: domain.SeverityWarning,
		Subject:  domain.Subject{Kind: "file", Ref: "project:src/main.c"},
		Message:  "File hash unavailable.",
	}}
	cfg := DefaultConfig()
	cfg.FailOnMissingHash = true
	waivers := []Waiver{{
		ID:      "UNKNOWN_LICENSE",
		Subject: "project:dep/legacy_blob/*",
		Reason:  "approved",
		Expires: "2099-01-01",
	}}

	res := Evaluate(findings, cfg, waivers, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	if !res.Fail {
		t.Fatal("missing hash should fail under the policy")
	}
	if len(res.Findings) != 2 {
		t.Fatalf("expected original finding + WAIVER_UNUSED, got %d findings", len(res.Findings))
	}
	if res.Findings[1].ID != "WAIVER_UNUSED" {
		t.Fatalf("expected WAIVER_UNUSED, got %q", res.Findings[1].ID)
	}
}

func TestEachFailureGateCanFailIndependently(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		setup func(*Config)
	}{
		{"unknown component", "UNKNOWN_COMPONENT", func(c *Config) { c.FailOnUnknownComponent = true }},
		{"unknown license", "UNKNOWN_LICENSE", func(c *Config) { c.FailOnUnknownLicense = true }},
		{"missing hash", "MISSING_FILE_HASH", func(c *Config) { c.FailOnMissingHash = true }},
		{"missing source", "LINKED_OBJECT_SOURCE_UNRESOLVED", func(c *Config) { c.FailOnMissingSourceForLinkedObject = true }},
		{"stale build", "STALE_BUILD_EVIDENCE", func(c *Config) { c.FailOnStaleBuildArtifacts = true }},
		{"review required", "LICENSE_CONFLICT", func(c *Config) { c.FailOnReviewRequired = true }},
		{"weak evidence", "WEAK_EVIDENCE", func(c *Config) { c.FailOnWeakEvidence = true }},
		{"missing header", "MISSING_HEADER_DEPENDENCY_EVIDENCE", func(c *Config) { c.FailOnMissingHeaderEvidence = true }},
		{"unanchored", "UNANCHORED_FILE", func(c *Config) { c.FailOnUnanchoredFile = true }},
		{"unknown version", "UNKNOWN_VERSION", func(c *Config) { c.FailOnUnknownVersion = true }},
		{"missing supplier", "MISSING_SUPPLIER", func(c *Config) { c.FailOnMissingSupplier = true }},
		{"missing component hash", "MISSING_COMPONENT_HASH", func(c *Config) { c.FailOnMissingComponentHash = true }},
		{"missing link", "MISSING_LINK_EVIDENCE", func(c *Config) { c.AllowMissingLinkEvidence = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{Profile: "test"}
			test.setup(&cfg)
			result := Evaluate([]domain.Finding{{ID: test.id, Severity: domain.SeverityError}}, cfg, nil, time.Time{})
			if !result.Fail || result.ExitCode != 3 {
				t.Fatalf("%s: result = %+v, want policy failure", test.id, result)
			}
		})
	}
}

func TestSeverityOverrideChangesGateBehavior(t *testing.T) {
	cfg := Config{Profile: "test", FailOnUnknownLicense: true, SeverityOverrides: map[string]domain.Severity{"UNKNOWN_LICENSE": domain.SeverityInfo}}
	result := Evaluate([]domain.Finding{{ID: "UNKNOWN_LICENSE", Severity: domain.SeverityError}}, cfg, nil, time.Time{})
	if result.Fail {
		t.Fatal("info severity override should prevent a policy failure")
	}
	if result.Findings[0].Severity != domain.SeverityInfo {
		t.Fatalf("severity = %q, want info", result.Findings[0].Severity)
	}
}
