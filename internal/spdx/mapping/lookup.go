package mapping

import "strings"

// Candidate is one element a document reader offers to explain --component:
// its local identity and the name a person would type for it.
type Candidate struct {
	LocalID string
	Name    string
}

// FindComponent is the lookup every SPDX reader answers explain --component
// --sbom with, so that the same question about the same run gets the same
// answer in every version and in CycloneDX: by local identity first, as the
// review report prints it -- exactly, then as a component, then as a
// toolchain component -- and then by name, as a person types it. It returns
// the index of the match among the candidates.
func FindComponent(candidates []Candidate, name string) (int, bool) {
	for _, local := range []string{name, "component:" + name, "toolchain:" + name} {
		for index, candidate := range candidates {
			if candidate.LocalID == local {
				return index, true
			}
		}
	}
	for index, candidate := range candidates {
		if candidate.Name == name {
			return index, true
		}
	}
	return -1, false
}

// BaseName is the CycloneDX component name of a used file: the canonical path
// after its last "/", or after the anchor when it has none. A reader offers a
// used file under it, because that is the name the CycloneDX document of the
// same run gives the file.
func BaseName(canonical string) string {
	if index := strings.LastIndexByte(canonical, '/'); index >= 0 {
		return canonical[index+1:]
	}
	if index := strings.LastIndexByte(canonical, ':'); index >= 0 {
		return canonical[index+1:]
	}
	return canonical
}

// ProductName is the name of the product package, which every SPDX version
// names its document after, or "" when the model has none.
func (m *Model) ProductName() string {
	for _, pkg := range m.Packages {
		if pkg.Role == RoleProduct {
			return pkg.Name
		}
	}
	return ""
}
