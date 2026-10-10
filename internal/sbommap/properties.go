package sbommap

import (
	"fmt"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

// Property is one sbomb:* name and value of appendix B. Repeated properties
// are several entries of the same name, never one joined value.
type Property struct {
	Name  string
	Value string
}

// EffectiveType is the component type every writer states for a component,
// the product included: the type of section 19.1 as the document carries it,
// or "library" where it carries none.
//
// The neutral document may leave the type unset -- generate always sets it,
// but the document is meant to be producer-neutral -- and the answer is
// settled here, once, rather than by each writer. Three writers each defaulting
// on their own had three answers: a typeless product was a "library" in
// CycloneDX and an "application" in SPDX, and inside the SPDX document its
// purpose said one thing while the BSI triple was derived from the other.
// "library" is the answer for every element because it is the one CycloneDX
// output has always stated; a producer that knows a product is an application
// says so in the document.
func EffectiveType(component domain.Component) string {
	if component.Type == "" {
		return "library"
	}
	return component.Type
}

// ComponentProperties is the property set of a product, artifact or
// component, as every format carries it -- apart from the repository record,
// which VCSProperties derives separately because CycloneDX 1.7 moves it into
// the external reference it qualifies.
//
// The BSI triple is derived from EffectiveType, the type every writer states:
// a document that said "library" in one place and derived the triple from
// "application" in another would contradict itself.
func ComponentProperties(component domain.Component) []Property {
	var properties []Property
	if component.Originator != "" {
		properties = append(properties, Property{Name: "sbomb:component:originator", Value: component.Originator})
	}
	for _, exclusion := range component.CVEExclusions {
		properties = append(properties, Property{Name: "sbomb:component:cveExclusion", Value: CVEExclusionValue(exclusion)})
	}
	properties = append(properties, PropertiesFromMap(component.Properties)...)
	if component.Scope != "" {
		properties = append(properties, Property{Name: "sbomb:component:scope", Value: component.Scope})
	}
	properties = append(properties, BSIProperties(domain.FileClassUnknown, EffectiveType(component))...)
	return properties
}

// CVEExclusionValue is the property value of one exclusion. A reason is
// optional in the manifest, and "CVE-0000-0: " with nothing after the
// separator states a reason that was never given.
func CVEExclusionValue(exclusion domain.CVEExclusion) string {
	if exclusion.Reason == "" {
		return exclusion.CVE
	}
	return exclusion.CVE + ": " + exclusion.Reason
}

// VCSProperties is the commit and the dirty flag of a repository record. They
// have no standard field in either format; where they sit -- on the component,
// or beside the URL they qualify -- is the writer's decision.
func VCSProperties(record *domain.VCSRecord) []Property {
	if record == nil {
		return nil
	}
	var properties []Property
	if record.Commit != "" {
		properties = append(properties, Property{Name: "sbomb:component:vcsCommit", Value: record.Commit})
	}
	if record.Dirty {
		properties = append(properties, Property{Name: "sbomb:component:vcsDirty", Value: "true"})
	}
	return properties
}

// FileProperties is the property set of a used file: its canonical path, what
// discovery recorded about it, and the BSI triple of its class.
func FileProperties(file domain.UsedFile) []Property {
	properties := []Property{{Name: "sbomb:path:canonical", Value: file.ID.Canonical()}}
	properties = append(properties, PropertiesFromMap(file.Properties)...)
	properties = append(properties, BSIProperties(file.Class, "file")...)
	return properties
}

// BSIProperties emits the three properties BSI TR-03183-2 requires per
// component, derived from the file class (section 1.5(3)).
func BSIProperties(class domain.FileClass, componentType string) []Property {
	executable := "non-executable"
	archive := "no-archive"
	structured := "unstructured"

	switch class {
	case domain.FileClassSource, domain.FileClassHeader,
		domain.FileClassGeneratedSource, domain.FileClassGeneratedHeader:
		structured = "structured"
	case domain.FileClassArchive:
		archive = "archive"
	case domain.FileClassSharedLibrary:
		executable = "executable"
	case domain.FileClassObject:
		// An object is a linkable container, neither executable nor an archive.
	default:
		switch componentType {
		case "application", "firmware", "device":
			executable = "executable"
		default:
			structured = "structured"
		}
	}

	return []Property{
		{Name: "sbomb:cdx:archiveProperty", Value: archive},
		{Name: "sbomb:cdx:executableProperty", Value: executable},
		{Name: "sbomb:cdx:structuredProperty", Value: structured},
	}
}

// RunProperties is the property set of the run that wrote the document.
// specVersion is the version the document is written in, which the run alone
// does not know.
func RunProperties(run sbomwriter.RunMetadata, specVersion string) []Property {
	properties := []Property{
		{Name: "sbomb:run:specVersion", Value: specVersion},
		{Name: "sbomb:run:toolVersion", Value: run.ToolVersion},
	}
	if run.PolicyProfile != "" {
		properties = append(properties, Property{Name: "sbomb:run:policyProfile", Value: run.PolicyProfile})
	}
	if run.BuildConfig != "" {
		properties = append(properties, Property{Name: "sbomb:build:config", Value: run.BuildConfig})
	}
	if run.Generator != "" {
		properties = append(properties, Property{Name: "sbomb:build:generator", Value: run.Generator})
	}
	// Volatile run properties are omitted in reproducible mode (section 29);
	// none of the above is volatile, so reproducible changes nothing here.
	return properties
}

// PropertiesFromMap emits only catalogued properties, sorted by name and then
// in the order the values were recorded. Discovery annotates files with
// internal markers in the same map; the property catalogue of appendix B
// governs what reaches a consumer, so anything outside the sbomb namespace
// stays inside the tool.
func PropertiesFromMap(values map[string][]string) []Property {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	properties := make([]Property, 0, len(values))
	for _, name := range names {
		if !strings.HasPrefix(name, "sbomb:") {
			continue
		}
		for _, value := range values[name] {
			properties = append(properties, Property{Name: name, Value: value})
		}
	}
	return properties
}

// SortProperties orders a property set by name, then value, in place -- the
// order section 29 prescribes for every property bag.
func SortProperties(properties []Property) {
	sort.SliceStable(properties, func(a, b int) bool {
		if properties[a].Name == properties[b].Name {
			return properties[a].Value < properties[b].Value
		}
		return properties[a].Name < properties[b].Name
	})
}

// ValidatePropertyName holds a property name to the namespace of appendix B.
// A key discovery uses internally -- "finding" is one -- fails here, which is
// how a leaked marker is caught before a consumer sees it. Membership of the
// catalogue itself is what tools/propertydoc checks, against the literals the
// code writes; a runtime check would have to carry a second copy of appendix
// B that nothing keeps current.
func ValidatePropertyName(name string) error {
	if name == "" {
		return fmt.Errorf("empty property name")
	}
	if !strings.HasPrefix(name, "sbomb:") {
		return fmt.Errorf("property name %q must start with sbomb:", name)
	}
	allowed := map[string]struct{}{
		"sbomb:cdx:archiveProperty":    {},
		"sbomb:cdx:executableProperty": {},
		"sbomb:cdx:structuredProperty": {},
		"sbomb:evidence:artifacts":     {},
		"sbomb:license:reason":         {},
		"sbomb:license:review":         {},
		"sbomb:run:timestamp":          {},
		"sbomb:run:sourceDateEpoch":    {},
	}
	if _, ok := allowed[name]; ok {
		return nil
	}
	if strings.Contains(name, ":") {
		return nil
	}
	return fmt.Errorf("property name %q is not in the sbomb namespace", name)
}
