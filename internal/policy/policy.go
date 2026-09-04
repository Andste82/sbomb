package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/example/sbomb/internal/domain"
)

// Config is the minimal policy configuration required by milestone 13.
type Config struct {
	Profile string

	FailOnUnknownComponent            bool
	FailOnUnknownLicense              bool
	FailOnMissingHash                 bool
	FailOnMissingSourceForLinkedObject bool
	FailOnStaleBuildArtifacts         bool
	FailOnReviewRequired              bool
	FailOnWeakEvidence                bool
	FailOnMissingHeaderEvidence       bool
	FailOnUnanchoredFile             bool
	FailOnUnknownVersion             bool
	FailOnMissingSupplier            bool
	FailOnMissingComponentHash       bool
	AllowMissingLinkEvidence         bool

	HeaderEvidence     string
	SeverityOverrides map[string]domain.Severity
	WaiversFile       string
}

type Waiver struct {
	ID        string `json:"id"`
	Subject   string `json:"subject"`
	Reason    string `json:"reason"`
	ApprovedBy string `json:"approvedBy,omitempty"`
	Expires   string `json:"expires,omitempty"`
}

type Result struct {
	Findings []domain.Finding
	Fail     bool
	ExitCode int
}

func DefaultConfig() Config {
	return Config{
		Profile: "default",

		FailOnUnknownComponent:            false,
		FailOnUnknownLicense:              false,
		FailOnMissingHash:                 true,
		FailOnMissingSourceForLinkedObject: true,
		FailOnStaleBuildArtifacts:         true,
		FailOnReviewRequired:              false,
		FailOnWeakEvidence:                false,
		FailOnMissingHeaderEvidence:       false,
		FailOnUnanchoredFile:             false,
		FailOnUnknownVersion:             false,
		FailOnMissingSupplier:            false,
		FailOnMissingComponentHash:       false,
		AllowMissingLinkEvidence:         false,
		HeaderEvidence:                  "dwarf-preferred",
		SeverityOverrides:               map[string]domain.Severity{},
	}
}

func ResolveProfile(name string) Config {
	switch strings.ToLower(name) {
	case "", "default":
		return DefaultConfig()
	case "lenient":
		cfg := DefaultConfig()
		cfg.Profile = "lenient"
		cfg.FailOnMissingHash = false
		cfg.FailOnMissingSourceForLinkedObject = false
		cfg.FailOnStaleBuildArtifacts = false
		cfg.AllowMissingLinkEvidence = true
		cfg.HeaderEvidence = "dwarf-preferred"
		return cfg
	case "strict":
		cfg := DefaultConfig()
		cfg.Profile = "strict"
		cfg.FailOnUnknownComponent = true
		cfg.FailOnUnknownLicense = true
		cfg.FailOnMissingHash = true
		cfg.FailOnMissingSourceForLinkedObject = true
		cfg.FailOnStaleBuildArtifacts = true
		cfg.FailOnReviewRequired = true
		cfg.FailOnWeakEvidence = true
		cfg.FailOnMissingHeaderEvidence = true
		cfg.FailOnUnanchoredFile = true
		cfg.FailOnUnknownVersion = true
		cfg.FailOnMissingSupplier = true
		cfg.FailOnMissingComponentHash = true
		cfg.AllowMissingLinkEvidence = false
		cfg.HeaderEvidence = "union"
		return cfg
	case "cra":
		cfg := DefaultConfig()
		cfg.Profile = "cra"
		cfg.FailOnUnknownComponent = true
		cfg.FailOnUnknownLicense = true
		cfg.FailOnMissingHash = true
		cfg.FailOnMissingSourceForLinkedObject = true
		cfg.FailOnStaleBuildArtifacts = true
		cfg.FailOnMissingSupplier = true
		cfg.FailOnMissingComponentHash = true
		cfg.FailOnUnknownVersion = true
		cfg.HeaderEvidence = "dwarf-preferred"
		return cfg
	default:
		cfg := DefaultConfig()
		cfg.Profile = name
		return cfg
	}
}

func Evaluate(findings []domain.Finding, cfg Config, waivers []Waiver, now time.Time) Result {
	if cfg.Profile == "" {
		cfg = ResolveProfile(cfg.Profile)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	out := make([]domain.Finding, 0, len(findings))
	used := make(map[string]bool)
	for _, f := range findings {
		matched, waiverUsed, waiverReason, expired := matchWaiver(f, waivers, now)
		if matched {
			used[waiverKey(waiverUsed)] = true
		}
		if matched && !expired {
			f.Waived = true
			f.WaiverReason = waiverReason
			out = append(out, f)
			continue
		}
		if matched && expired {
			out = append(out, f)
			out = append(out, newFinding("WAIVER_EXPIRED", domain.SeverityWarning, f.Subject, fmt.Sprintf("waiver for %s expired", f.ID), map[string]any{"waiver": waiverReason}))
			continue
		}
		out = append(out, f)
	}

	for _, w := range waivers {
		if !used[waiverKey(w)] {
			out = append(out, domain.Finding{
				ID:       "WAIVER_UNUSED",
				Severity: domain.SeverityInfo,
				Subject:  domain.Subject{Kind: "configuration", Ref: w.Subject},
				Message:  fmt.Sprintf("waiver for %s matched no findings", w.ID),
				Detail: map[string]any{"waiver": w},
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Subject.Kind != out[j].Subject.Kind {
			return out[i].Subject.Kind < out[j].Subject.Kind
		}
		if out[i].Subject.Ref != out[j].Subject.Ref {
			return out[i].Subject.Ref < out[j].Subject.Ref
		}
		return out[i].Message < out[j].Message
	})

	fail := false
	for _, f := range out {
		if f.Waived {
			continue
		}
		if shouldFail(f, cfg) {
			fail = true
			break
		}
	}

	res := Result{Findings: out, Fail: fail, ExitCode: 0}
	if fail {
		res.ExitCode = 3
	}
	return res
}

func shouldFail(f domain.Finding, cfg Config) bool {
	if f.Severity == domain.SeverityInfo {
		return false
	}
	switch f.ID {
	case "UNKNOWN_COMPONENT":
		return cfg.FailOnUnknownComponent
	case "UNKNOWN_LICENSE":
		return cfg.FailOnUnknownLicense
	case "MISSING_FILE_HASH":
		return cfg.FailOnMissingHash
	case "LINKED_OBJECT_SOURCE_UNRESOLVED", "UNITY_SOURCE_UNRESOLVED":
		return cfg.FailOnMissingSourceForLinkedObject
	case "STALE_BUILD_EVIDENCE", "STALE_CMAKE_CONFIGURATION":
		return cfg.FailOnStaleBuildArtifacts
	case "LICENSE_CONFLICT", "VCS_DIRTY", "UNKNOWN_HEADER_CLASS":
		return cfg.FailOnReviewRequired
	case "LTO_ATTRIBUTION_DEGRADED", "WEAK_EVIDENCE":
		return cfg.FailOnWeakEvidence
	case "MISSING_HEADER_DEPENDENCY_EVIDENCE":
		return cfg.FailOnMissingHeaderEvidence
	case "UNANCHORED_FILE":
		return cfg.FailOnUnanchoredFile
	case "UNKNOWN_VERSION":
		return cfg.FailOnUnknownVersion
	case "MISSING_SUPPLIER":
		return cfg.FailOnMissingSupplier
	case "MISSING_COMPONENT_HASH":
		return cfg.FailOnMissingComponentHash
	case "MISSING_LINK_EVIDENCE":
		return !cfg.AllowMissingLinkEvidence
	case "WAIVER_EXPIRED", "WAIVER_UNUSED":
		return false
	default:
		return false
	}
}

func matchWaiver(f domain.Finding, waivers []Waiver, now time.Time) (matched bool, used Waiver, reason string, expired bool) {
	for _, w := range waivers {
		if w.ID != "*" && w.ID != f.ID {
			continue
		}
		if matchesSubject(w.Subject, f.Subject.Ref) {
			matched = true
			used = w
			reason = w.Reason
			expired = isExpired(w, now)
			return matched, used, reason, expired
		}
	}
	return false, Waiver{}, "", false
}

func matchesSubject(pattern, ref string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.TrimSpace(pattern) == "" {
		return true
	}
	if pattern == ref {
		return true
	}
	regex, err := wildcardToRegexp(pattern)
	if err != nil {
		return false
	}
	return regex.MatchString(ref)
}

func wildcardToRegexp(pattern string) (*regexp.Regexp, error) {
	escaped := regexp.QuoteMeta(pattern)
	escaped = strings.ReplaceAll(escaped, "\\*\\*", ".*")
	escaped = strings.ReplaceAll(escaped, "\\*", ".*")
	escaped = strings.ReplaceAll(escaped, "\\?", ".")
	return regexp.Compile("^" + escaped + "$")
}

func isExpired(w Waiver, now time.Time) bool {
	if w.Expires == "" {
		return false
	}
	if ts, err := time.Parse("2006-01-02", w.Expires); err == nil {
		return !now.Before(ts)
	}
	if ts, err := time.Parse(time.RFC3339, w.Expires); err == nil {
		return !now.Before(ts)
	}
	return false
}

func newFinding(id string, sev domain.Severity, subj domain.Subject, msg string, detail map[string]any) domain.Finding {
	return domain.Finding{ID: id, Severity: sev, Subject: subj, Message: msg, Detail: detail}
}

func waiverKey(w Waiver) string {
	return fmt.Sprintf("%s|%s|%s", w.ID, w.Subject, w.Expires)
}

func LoadWaivers(path string) ([]Waiver, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Waivers []Waiver `json:"waivers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return doc.Waivers, nil
}

func WriteFindingsJSON(path string, findings []domain.Finding) error {
	if path == "" {
		return nil
	}
	payload := map[string]any{
		"schemaVersion": 1,
		"toolVersion":   "sbomb",
		"findings":      findings,
		"summary": map[string]int{
			"error":   0,
			"warning": 0,
			"info":    0,
			"waived":  0,
		},
	}
	for _, f := range findings {
		switch f.Severity {
		case domain.SeverityError:
			payload["summary"].(map[string]int)["error"]++
		case domain.SeverityWarning:
			payload["summary"].(map[string]int)["warning"]++
		case domain.SeverityInfo:
			payload["summary"].(map[string]int)["info"]++
		}
		if f.Waived {
			payload["summary"].(map[string]int)["waived"]++
		}
	}
	return os.WriteFile(path, []byte(mustJSON(payload)), 0o600)
}

func MustPolicyJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b) + "\n"
}

func mustJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b) + "\n"
}
