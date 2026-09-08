package componentmap

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// Rule describes a single component mapping rule. Either Path or Match may be
// set; if both are empty the rule is ignored.
type Rule struct {
	Path  string
	Match string
	Name  string
	Type  string
}

type Mapper struct {
	rules []Rule
}

func NewMapper(rules []Rule) *Mapper {
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.Name == "" {
			continue
		}
		out = append(out, r)
	}
	return &Mapper{rules: out}
}

// Match reports the rule that claims a file, longest match winning. The rule
// itself is returned rather than only the component it names, because the
// rule's Path is where the component begins -- and section 19.2 wants that
// root as a fact rather than as something recomputed later from whichever
// files the linker happened to keep.
func (m *Mapper) Match(fileID domain.FileID) (Rule, bool) {
	path := normalizeRelPath(fileID.RelPath)
	best := Rule{}
	bestLen := -1
	for _, rule := range m.rules {
		matched, matchLen := ruleMatches(rule, path)
		if !matched {
			continue
		}
		if matchLen > bestLen {
			best = rule
			bestLen = matchLen
		}
	}
	return best, best.Name != ""
}

func (m *Mapper) MapFile(fileID domain.FileID) (domain.Component, bool) {
	best, matched := m.Match(fileID)
	if matched {
		return domain.Component{
			ID:         best.Name,
			Name:       best.Name,
			Type:       chooseType(best.Type),
			DetectedBy: "curated",
			Properties: map[string][]string{
				"sbomb:component:detectedBy": {"curated"},
			},
		}, true
	}
	// No curated rule matched. The caller continues down the priority chain of
	// section 19.2; producing an "unknown" component here would make strategy
	// 1 answer for all eight and no later strategy could ever run.
	return domain.Component{}, false
}

func chooseType(kind string) string {
	if kind != "" {
		return kind
	}
	return "library"
}

func ruleMatches(rule Rule, rel string) (bool, int) {
	if rel == "" {
		return false, 0
	}
	if rule.Path != "" {
		p := normalizeRelPath(rule.Path)
		if p == "." || p == "" {
			return true, 1
		}
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true, len(p)
		}
	}
	if rule.Match != "" {
		if matched, matchLen := matchGlob(rule.Match, rel); matched {
			return true, matchLen
		}
	}
	return false, 0
}

func matchGlob(pattern, rel string) (bool, int) {
	pattern = filepath.ToSlash(normalizeRelPath(pattern))
	rel = filepath.ToSlash(normalizeRelPath(rel))
	if pattern == "" || rel == "" {
		return false, 0
	}
	if strings.Contains(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true, len(prefix)
		}
	}
	regex := globToRegexp(pattern)
	if ok, err := regexp.MatchString(regex, rel); err == nil && ok {
		return true, len(pattern)
	}
	return false, 0
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
				continue
			}
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '.':
			b.WriteString("\\.")
		case '/':
			b.WriteString("/")
		default:
			if strings.ContainsRune("\\+()[]{}|^$", rune(pattern[i])) {
				b.WriteString("\\")
			}
			b.WriteByte(pattern[i])
		}
	}
	b.WriteString("$")
	return b.String()
}

func normalizeRelPath(p string) string {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
	clean = strings.TrimPrefix(clean, "./")
	clean = strings.TrimPrefix(clean, "/")
	return clean
}

func (m *Mapper) Rules() []Rule {
	out := make([]Rule, len(m.rules))
	copy(out, m.rules)
	sort.Slice(out, func(i, j int) bool { return len(normalizeRelPath(out[i].Path)) > len(normalizeRelPath(out[j].Path)) })
	return out
}
