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
