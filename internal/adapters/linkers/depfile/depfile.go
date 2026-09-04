package depfile

import (
	"strings"

	"github.com/example/sbomb/internal/adapters/depfiles"
)

// Record is a single depfile entry with the target artifact and the listed path.
type Record struct {
	Target string
	Path   string
}

// ParseString parses a linker dependency file and emits one record per prerequisite.
func ParseString(text string) ([]Record, error) {
	rules, err := depfiles.Parse(text)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0)
	for _, rule := range rules {
		for _, prereq := range rule.Prereqs {
			for _, target := range rule.Targets {
				out = append(out, Record{Target: target, Path: strings.TrimSpace(prereq)})
			}
		}
	}
	return out, nil
}
