package domain

import (
	"strings"
	"testing"
)

// A report that says only that two sources disagreed is not a report. Every
// message has to carry each value and the source it came from, because that is
// what a reviewer needs and because no renderer shows Detail.
func TestAConflictMessageNamesEverySideAndItsSource(t *testing.T) {
	tests := []struct {
		name     string
		conflict Conflict
		contains []string
	}{
		{
			name: "two sides with a winner",
			conflict: Conflict{
				Field:   "source of this object",
				Subject: Subject{Kind: "file", Ref: "build:app.o"},
				Sides: []ConflictSide{
					{Source: "cmake-file-api", Value: "project:main.c"},
					{Source: "ninja-buildgraph", Value: "project:other.c"},
				},
				Winner: "cmake-file-api",
				Reason: "the strategy order of section 13.2 puts it first",
			},
			contains: []string{
				"cmake-file-api", "project:main.c", "ninja-buildgraph", "project:other.c",
				"cmake-file-api wins", "section 13.2",
			},
		},
		{
			name: "three sides with a winner",
			conflict: Conflict{
				Field:   "source of this object",
				Subject: Subject{Kind: "file", Ref: "build:app.o"},
				Sides: []ConflictSide{
					{Source: "cmake-file-api", Value: "project:main.c"},
					{Source: "ninja-buildgraph", Value: "project:other.c"},
					{Source: "dwarf", Value: "project:third.c"},
				},
				Winner: "cmake-file-api",
			},
			contains: []string{"dwarf", "project:third.c", "cmake-file-api wins"},
		},
		{
			name: "two sides and nothing wins",
			conflict: Conflict{
				Field:   "package that owns this file",
				Subject: Subject{Kind: "file", Ref: "build:shared/util.h"},
				Sides: []ConflictSide{
					{Source: "vcpkg", Value: "left"},
					{Source: "conan", Value: "right"},
				},
				Reason: "two statements are no statement",
			},
			contains: []string{"vcpkg", "left", "conan", "right", "none of them wins"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			finding, ok := test.conflict.Finding("COMPONENT_MAPPING_CONFLICT", SeverityInfo)
			if !ok {
				t.Fatal("a disagreement between two sides has to be reportable")
			}
			if finding.ID != "COMPONENT_MAPPING_CONFLICT" || finding.Severity != SeverityInfo {
				t.Errorf("finding = %s/%s, want the identifier and severity asked for", finding.ID, finding.Severity)
			}
			if finding.Subject != test.conflict.Subject {
				t.Errorf("subject = %+v, want %+v", finding.Subject, test.conflict.Subject)
			}
			for _, want := range test.contains {
				if !strings.Contains(finding.Message, want) {
					t.Errorf("message %q does not name %q", finding.Message, want)
				}
			}
			if len(finding.Detail) == 0 {
				t.Error("detail is empty; --findings-json would carry nothing")
			}
		})
	}
}

// A conflict needs an opponent. One answer, or none, is a decision nobody
// contradicted, and a sentence about it would report something that never
// happened.
func TestOneSideIsNoConflict(t *testing.T) {
	for _, sides := range [][]ConflictSide{nil, {{Source: "vcpkg", Value: "left"}}} {
		conflict := Conflict{
			Field:   "package that owns this file",
			Subject: Subject{Kind: "file", Ref: "build:shared/util.h"},
			Sides:   sides,
		}
		if finding, ok := conflict.Finding("COMPONENT_MAPPING_CONFLICT", SeverityInfo); ok {
			t.Errorf("sides %+v produced %q, want no finding", sides, finding.Message)
		}
	}
}
