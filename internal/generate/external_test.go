package generate

import (
	"testing"

	"github.com/example/sbomb/internal/domain"
)

// TestEnvironmentProvidedNeedsMoreThanSystemScope pins the distinction the
// CycloneDX field actually makes. "Provided by the environment" is a claim
// about the shipped artifact, not about where a file happened to live at build
// time: a system archive linked statically ends up inside the artifact, and
// calling that externally provided would be false.
func TestEnvironmentProvidedNeedsMoreThanSystemScope(t *testing.T) {
	shared := domain.UsedFile{ID: fileID("system", "libc.so.6"), Class: domain.FileClassSharedLibrary}
	archive := domain.UsedFile{ID: fileID("system", "libm.a"), Class: domain.FileClassArchive}

	for _, testCase := range []struct {
		name  string
		scope string
		files []domain.UsedFile
		want  bool
	}{
		{"a dynamically linked system library", "system", []domain.UsedFile{shared}, true},
		{"a statically linked system archive", "system", []domain.UsedFile{archive}, false},
		{"both, because one shared library is enough", "system", []domain.UsedFile{archive, shared}, true},
		{"a project component", "project", []domain.UsedFile{shared}, false},
		{"a third-party component", "third-party", []domain.UsedFile{shared}, false},
		{"a system component with no files at all", "system", nil, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			component := domain.Component{ID: "c", Name: "c", Scope: testCase.scope}
			if got := environmentProvided(component, testCase.files); got != testCase.want {
				t.Errorf("environmentProvided = %v, want %v", got, testCase.want)
			}
		})
	}
}
