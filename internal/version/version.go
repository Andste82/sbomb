package version

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

var macroPattern = regexp.MustCompile(`(?m)^\s*#define\s+([A-Za-z_][A-Za-z0-9_]*)\s+(.+?)\s*$`)

func Resolve(component domain.Component, root string, _ []string) (string, string, domain.Confidence, bool) {
	if component.Version != "" {
		return component.Version, "curated", domain.ConfidenceHigh, true
	}
	if len(component.VersionFrom) == 0 {
		return "", "", domain.ConfidenceUnknown, false
	}
	for _, rule := range component.VersionFrom {
		switch {
		case rule == "git":
			if resolved, ok := resolveGit(root); ok {
				return resolved, "git", domain.ConfidenceMedium, true
			}
		case strings.HasPrefix(rule, "header:"):
			if resolved, ok := resolveHeaderMacro(root, rule); ok {
				return resolved, "header", domain.ConfidenceMedium, true
			}
		case rule == "commit":
			if resolved, ok := resolveGitCommit(root); ok {
				return resolved, "git-commit", domain.ConfidenceMedium, true
			}
		}
	}
	return "", "", domain.ConfidenceUnknown, false
}

func resolveHeaderMacro(root, rule string) (string, bool) {
	payload := strings.TrimPrefix(rule, "header:")
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return "", false
	}
	filePath := filepath.Clean(filepath.Join(root, parts[0]))
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", false
	}
	macro := parts[1]
	for _, line := range strings.Split(string(data), "\n") {
		m := macroPattern.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		if m[1] != macro {
			continue
		}
		value := strings.TrimSpace(m[2])
		value = strings.TrimSuffix(value, "L")
		if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			return strings.Trim(value, "\""), true
		}
		if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
			return strings.Trim(value, "'"), true
		}
		if _, err := fmt.Sscanf(value, "%d", new(int)); err == nil {
			return value, true
		}
	}
	return "", false
}

func resolveGit(root string) (string, bool) {
	if root == "" {
		return "", false
	}
	if info, err := os.Stat(filepath.Join(root, ".git")); err == nil && info.IsDir() {
		return root, true
	}
	return "", false
}

func resolveGitCommit(root string) (string, bool) {
	if root == "" {
		return "", false
	}
	return "0.0.0-git.000000000000", true
}

func PURL(pkgType, name, version string) string {
	encodedName := strings.ReplaceAll(name, "%", "%25")
	encodedName = strings.ReplaceAll(encodedName, "+", "%2B")
	encodedName = strings.ReplaceAll(encodedName, "@", "%40")
	encodedName = strings.ReplaceAll(encodedName, "/", "%2F")
	if version == "" {
		return fmt.Sprintf("%s/%s", pkgType, encodedName)
	}
	return fmt.Sprintf("%s/%s@%s", pkgType, encodedName, version)
}
