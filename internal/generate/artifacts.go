package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
)

// Deliverable is one resolved final deliverable: the artifact the evidence
// chains must reach, per specification section 5.
type Deliverable struct {
	// Path is where the artifact can be read right now.
	Path string
	// EvidencePath is how the artifact is named in build evidence: relative to
	// the build directory when it lies below it, absolute otherwise. Identity
	// is computed from this, not from Path (section 7.6).
	EvidencePath string
	// Role is the configured role, defaulting to application.
	Role string
	// DiscoveredBy records how the artifact was selected, for the review report.
	DiscoveredBy string
}

// defaultExcludeTargetPatterns is the test and example heuristic of section
// 5.4. It applies only to automatic discovery, never to a configured artifact.
var defaultExcludeTargetPatterns = []string{"*test*", "*example*", "*sample*", "*benchmark*"}

// resolveDeliverables determines what the SBOM is about. Configured artifacts
// win; otherwise the CMake File API is asked, and only in the two structured
// ways section 5.3 permits. Selecting the newest, largest or only binary in
// the build directory is explicitly forbidden (section 5.2).
func resolveDeliverables(cfg config.Config, buildDir string, model *cmakeapi.Model, logger *Logger) ([]Deliverable, []domain.Finding, error) {
	findings := []domain.Finding{}

	if len(cfg.Artifacts) > 0 {
		deliverables := make([]Deliverable, 0, len(cfg.Artifacts))
		for _, artifact := range cfg.Artifacts {
			path, found := locateArtifact(artifact.Path, cfg.Project.Root, buildDir)
			if !found {
				return nil, findings, &ExitError{
					Code: 2,
					Finding: domain.Finding{
						ID:       "MISSING_ARTIFACT",
						Severity: domain.SeverityError,
						Subject:  domain.Subject{Kind: "artifact", Ref: artifact.Path},
						Message:  fmt.Sprintf("the configured artifact %q does not exist or is not readable", artifact.Path),
					},
				}
			}
			role := artifact.Role
			if role == "" {
				role = "application"
			}
			deliverables = append(deliverables, Deliverable{Path: path, EvidencePath: evidencePath(path, buildDir), Role: role, DiscoveredBy: "configuration"})
		}
		return deliverables, findings, nil
	}

	// Automatic discovery is permitted only from structured build metadata.
	if model == nil {
		return nil, findings, &ExitError{
			Code: 1,
			Finding: domain.Finding{
				ID:       "MISSING_FINAL_DELIVERABLE",
				Severity: domain.SeverityError,
				Subject:  domain.Subject{Kind: "build", Ref: buildDir},
				Message:  "no artifact is configured and no CMake File API reply is available to discover one",
				Remediation: "Configure artifacts[] with the file that is delivered, or enable the CMake File API " +
					"so that installed executable targets can be discovered.",
			},
		}
	}

	candidates := discoverCandidates(model, cfg.Discovery, logger)
	if cfg.Build.Config != "" {
		prefix := filepath.ToSlash(cfg.Build.Config) + "/"
		filtered := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			if strings.HasPrefix(filepath.ToSlash(candidate), prefix) {
				filtered = append(filtered, candidate)
			}
		}
		// Single-config generators (Ninja, Make) never prefix artifact paths
		// with the configuration name, so a --config-name passed alongside
		// them would otherwise discard every real candidate. Only narrow the
		// set when the prefix actually matched something.
		if len(filtered) > 0 {
			candidates = filtered
		}
	}
	if len(candidates) == 0 {
		return nil, findings, &ExitError{
			Code: 1,
			Finding: domain.Finding{
				ID:          "MISSING_FINAL_DELIVERABLE",
				Severity:    domain.SeverityError,
				Subject:     domain.Subject{Kind: "build", Ref: buildDir},
				Message:     "no executable target with an install rule was found",
				Remediation: "Configure artifacts[] with the file that is delivered.",
			},
		}
	}
	if len(candidates) > 1 && cfg.Mode != "assembly" {
		return nil, findings, &ExitError{
			Code: 1,
			Finding: domain.Finding{
				ID:       "AMBIGUOUS_FINAL_DELIVERABLE",
				Severity: domain.SeverityError,
				Subject:  domain.Subject{Kind: "build", Ref: buildDir},
				Message:  fmt.Sprintf("discovery found %d candidates: %s", len(candidates), strings.Join(candidates, ", ")),
				Remediation: "Configure artifacts[] to name the deliverable, or set mode to assembly to describe " +
					"all of them as one product.",
			},
		}
	}

	deliverables := make([]Deliverable, 0, len(candidates))
	for _, candidate := range candidates {
		path, found := locateArtifact(candidate, cfg.Project.Root, buildDir)
		if !found {
			// The target exists in the reply but was never built.
			findings = append(findings, domain.Finding{
				ID:       "MISSING_ARTIFACT",
				Severity: domain.SeverityWarning,
				Subject:  domain.Subject{Kind: "artifact", Ref: candidate},
				Message:  "a discovered target has not been built yet",
			})
			continue
		}
		deliverables = append(deliverables, Deliverable{Path: path, EvidencePath: evidencePath(path, buildDir), Role: "application", DiscoveredBy: "cmake-file-api"})
	}
	if len(deliverables) == 0 {
		return nil, findings, &ExitError{
			Code: 1,
			Finding: domain.Finding{
				ID:       "MISSING_FINAL_DELIVERABLE",
				Severity: domain.SeverityError,
				Subject:  domain.Subject{Kind: "build", Ref: buildDir},
				Message:  "every discovered target is missing from the build directory",
			},
		}
	}
	return deliverables, findings, nil
}

// discoverCandidates applies section 5.3: installed executables first, then
// executables that are not tests or examples.
func discoverCandidates(model *cmakeapi.Model, discovery config.Discovery, logger *Logger) []string {
	patterns := discovery.ExcludeTargetPatterns
	if len(patterns) == 0 {
		patterns = defaultExcludeTargetPatterns
	}

	installed := []string{}
	other := []string{}
	for _, configuration := range model.Configurations {
		for _, target := range configuration.Targets {
			if target.Type != "EXECUTABLE" {
				continue
			}
			if matchesAnyPattern(target.Name, patterns) {
				logger.Debug("Discovery skips target %q: it matches a test or example pattern", target.Name)
				continue
			}
			for _, artifact := range target.Artifacts {
				if artifact.Path == "" || isDebugSidecar(artifact.Path) {
					continue
				}
				if len(target.Install) > 0 {
					installed = append(installed, artifact.Path)
				} else {
					other = append(other, artifact.Path)
				}
			}
		}
	}
	if len(installed) > 0 {
		sort.Strings(installed)
		return dedupe(installed)
	}
	sort.Strings(other)
	return dedupe(other)
}

// isDebugSidecar reports whether a target artifact is a debug or link
// byproduct rather than something that is delivered. CMake lists app.pdb
// alongside app.exe, and treating both as candidates makes every MinGW or MSVC
// build look ambiguous.
func isDebugSidecar(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdb", ".map", ".d", ".ilk", ".exp", ".lib":
		return true
	default:
		return false
	}
}

// matchesAnyPattern reports whether name matches any glob, case-insensitively.
func matchesAnyPattern(name string, patterns []string) bool {
	lowered := strings.ToLower(name)
	for _, pattern := range patterns {
		if matched, err := filepath.Match(strings.ToLower(pattern), lowered); err == nil && matched {
			return true
		}
	}
	return false
}

// locateArtifact finds where a configured or discovered artifact path can be
// read. Paths are resolved against the project root and the build directory,
// because the two conventions are both in use.
func locateArtifact(path, projectRoot, buildDir string) (string, bool) {
	if path == "" {
		return "", false
	}
	candidates := []string{path}
	if !filepath.IsAbs(path) {
		if projectRoot != "" {
			candidates = append(candidates, filepath.Join(projectRoot, path))
		}
		candidates = append(candidates, filepath.Join(buildDir, path))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// evidencePath expresses an artifact the way build evidence names it.
func evidencePath(path, buildDir string) string {
	if relative, err := filepath.Rel(buildDir, path); err == nil && !strings.HasPrefix(relative, "..") {
		return relative
	}
	return path
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := values[:0]
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// ExitError carries a finding that determines the process exit code, so that
// the CLI can honour the precedence of section 32.4 without inspecting text.
type ExitError struct {
	Code    int
	Finding domain.Finding
}

func (e *ExitError) Error() string {
	return e.Finding.ID + ": " + e.Finding.Message
}
