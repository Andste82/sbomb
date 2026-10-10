package cyclonedx

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/sbomb/internal/sbomwriter"
)

// Label is how a person names the format.
func (Writer) Label() string { return "CycloneDX" }

// Extension is the conventional suffix of a CycloneDX JSON document, the one
// the CycloneDX specification recommends and the default output names use.
func (Writer) Extension() string { return ".cdx.json" }

// OmitsTimestamp is true of every reproducible CycloneDX document: section 29
// drops metadata.timestamp rather than pinning it, because the field is
// optional here and an omitted time cannot differ between two runs. That is
// what makes REPRODUCIBLE_MODE_OMITS_TIMESTAMP true of the document.
func (Writer) OmitsTimestamp(version string, options sbomwriter.Options) bool {
	return options.Reproducible
}

// EmbeddedSchema serves the schema of one version through the format-neutral
// interface, so that `sbomb schema` asks the registry and not this package.
func (Writer) EmbeddedSchema(version string) ([]byte, error) {
	schema, err := EmbeddedSchema(version)
	if err != nil {
		return nil, err
	}
	return []byte(schema), nil
}

// Preflight refuses a configured TLP the chosen version cannot carry. Build
// refuses it too, but only after a full discovery run; this is the same rule
// asked before anything is read, which is when a usage error belongs.
func (Writer) Preflight(version string, options sbomwriter.Options) error {
	if options.TLP != "" && !supportsDistributionConstraints(version) {
		return fmt.Errorf("output.tlp needs CycloneDX 1.7; this run writes %s", version)
	}
	return nil
}

// ComponentFiles expands a component to the file identities the document says
// it groups.
//
// The mapping is there and nowhere else: section 28's dependency cascade gives
// every grouping component a dependsOn list of its `file:` refs, and a file
// bom-ref is its identity behind that prefix (section 28.4) -- which is the
// key the evidence graph uses. So the expansion is a lookup, not a search.
func (Writer) ComponentFiles(data []byte, name string) ([]string, error) {
	var document struct {
		Components []struct {
			BomRef string `json:"bom-ref"`
			Name   string `json:"name"`
		} `json:"components"`
		Dependencies []struct {
			Ref       string   `json:"ref"`
			DependsOn []string `json:"dependsOn"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}

	// By bom-ref first, because that is what the document is keyed by and what
	// the review report prints; by name second, because that is what a person
	// types. A toolchain component is a grouping component too and its ref
	// carries its own prefix.
	ref := ""
	for _, candidate := range []string{name, "component:" + name, "toolchain:" + name} {
		for _, entry := range document.Components {
			if entry.BomRef == candidate {
				ref = entry.BomRef
				break
			}
		}
		if ref != "" {
			break
		}
	}
	if ref == "" {
		for _, entry := range document.Components {
			if entry.Name == name {
				ref = entry.BomRef
				break
			}
		}
	}
	if ref == "" {
		return nil, sbomwriter.ErrNoSuchComponent
	}

	files := make([]string, 0)
	for _, dependency := range document.Dependencies {
		if dependency.Ref != ref {
			continue
		}
		for _, on := range dependency.DependsOn {
			if identity, found := strings.CutPrefix(on, "file:"); found {
				files = append(files, identity)
			}
		}
	}
	return files, nil
}
