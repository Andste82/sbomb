package cyclonedx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/sbomwriter"
)

// Writer serializes a document as CycloneDX 1.6 JSON.
type Writer struct{}

func init() { sbomwriter.Register(Writer{}) }

func (Writer) ID() string { return "cyclonedx-json" }

// Versions is fixed at 1.6 by section 28.1: BSI TR-03183-2 requires it as the
// minimum, and section 1.5 makes that the field-level compliance target.
func (Writer) Versions() []string { return []string{"1.6"} }

func (w Writer) Write(out io.Writer, document *sbomwriter.Document, options sbomwriter.Options) error {
	bom, err := w.Build(document, options)
	if err != nil {
		return err
	}
	serialized, err := MarshalBOM(bom)
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, serialized)
	return err
}

func (Writer) Validate(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return Validate(data)
}

// shortDigest is the twelve-character content digest section 28.4 uses to
// disambiguate two components that share a name and have no version.
func shortDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}

// Validate runs both layers section 32.5 requires: the official JSON Schema,
// then the semantic checks a schema cannot express.
func Validate(data []byte) error {
	if err := ValidateAgainstSchema(data); err != nil {
		return err
	}
	return ValidateDocument(data)
}

// Build turns a format-neutral document into a CycloneDX BOM. All bom-ref
// derivation lives here, per section 36.1: the document itself carries none.
func (Writer) Build(document *sbomwriter.Document, options sbomwriter.Options) (BOM, error) {
	refs := newRefTable()

	product := componentToCyclone(document.Product, refs.forProduct(document.Product))
	if product.Type == "" {
		product.Type = "application"
	}

	components := make([]Component, 0, len(document.Artifacts)+len(document.Components)+len(document.Files))
	for _, artifact := range document.Artifacts {
		components = append(components, componentToCyclone(artifact, refs.forArtifact(artifact)))
	}
	for _, grouping := range document.Components {
		components = append(components, componentToCyclone(grouping, refs.forComponent(grouping)))
	}
	for _, file := range document.Files {
		components = append(components, fileToCyclone(file, refs.forFile(file)))
	}

	if duplicate := firstDuplicateRef(components, product.BomRef); duplicate != "" {
		// Section 28.4 makes collisions impossible by construction, but an
		// implementation must still assert it rather than emit a broken graph.
		return BOM{}, fmt.Errorf("bom-ref collision on %q", duplicate)
	}

	dependencies := buildDependencies(document, refs, product.BomRef, components)

	metadata := &Metadata{
		Tools: []Tool{{
			Vendor:  document.Run.ToolVendor,
			Name:    document.Run.ToolName,
			Version: document.Run.ToolVersion,
		}},
		Component:  &product,
		Properties: runProperties(document.Run),
	}
	if !options.Reproducible {
		metadata.Timestamp = document.Run.Timestamp
	}

	bom := BOM{
		BomFormat:    "CycloneDX",
		SpecVersion:  "1.6",
		Version:      1,
		Metadata:     metadata,
		Components:   components,
		Dependencies: dependencies,
	}
	return bom, nil
}

// refTable derives and remembers the bom-ref of every identity, per the scheme
// of section 28.4.
type refTable struct {
	byComponentID map[string]string
	byFileID      map[string]string
	nameCounts    map[string]int
}

func newRefTable() *refTable {
	return &refTable{
		byComponentID: map[string]string{},
		byFileID:      map[string]string{},
		nameCounts:    map[string]int{},
	}
}

func (t *refTable) forProduct(product domain.Component) string {
	ref := "product:" + pathmodel.Slug(product.Name, 64)
	t.byComponentID[product.ID] = ref
	return ref
}

func (t *refTable) forArtifact(artifact domain.Component) string {
	ref := "artifact:" + artifact.ID
	t.byComponentID[artifact.ID] = ref
	return ref
}

func (t *refTable) forComponent(component domain.Component) string {
	ref := "component:" + pathmodel.Slug(component.Name, 64)
	// Disambiguate by version, then by a digest of the component root, exactly
	// as section 28.4 prescribes.
	if t.nameCounts[ref] > 0 {
		if component.Version != "" {
			ref += "@" + pathmodel.Slug(component.Version, 32)
		} else if component.Root != nil {
			ref += "#" + shortDigest(component.Root.Canonical())
		} else {
			ref += "#" + shortDigest(component.ID)
		}
	}
	t.nameCounts["component:"+pathmodel.Slug(component.Name, 64)]++
	t.byComponentID[component.ID] = ref
	return ref
}

func (t *refTable) forFile(file domain.UsedFile) string {
	canonical := file.ID.Canonical()
	ref := "file:" + canonical
	t.byFileID[canonical] = ref
	return ref
}

// resolve maps a component or file identity onto its bom-ref.
func (t *refTable) resolve(id string) (string, bool) {
	if ref, ok := t.byComponentID[id]; ok {
		return ref, true
	}
	ref, ok := t.byFileID[id]
	return ref, ok
}

// buildDependencies renders the cascade of section 28.5. Every bom-ref in the
// document appears exactly once as a dependency entry, even when it depends on
// nothing, so that a consumer can close the graph.
func buildDependencies(document *sbomwriter.Document, refs *refTable, productRef string, components []Component) []Dependency {
	dependsOn := map[string][]string{productRef: nil}
	for _, component := range components {
		dependsOn[component.BomRef] = nil
	}
	for _, relation := range document.Relations {
		from, known := refs.resolve(relation.From)
		if !known {
			continue
		}
		for _, target := range relation.To {
			to, resolved := refs.resolve(target)
			if !resolved {
				continue
			}
			dependsOn[from] = append(dependsOn[from], to)
		}
	}

	dependencies := make([]Dependency, 0, len(dependsOn))
	for ref, targets := range dependsOn {
		sort.Strings(targets)
		dependencies = append(dependencies, Dependency{Ref: ref, DependsOn: dedupeStrings(targets)})
	}
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].Ref < dependencies[j].Ref })
	return dependencies
}

func componentToCyclone(component domain.Component, ref string) Component {
	out := Component{
		Type:    component.Type,
		Name:    component.Name,
		Version: component.Version,
		BomRef:  ref,
		PURL:    component.PURL,
	}
	if out.Type == "" {
		out.Type = "library"
	}
	if component.Supplier != "" {
		out.Supplier = &OrganizationalEntity{Name: component.Supplier}
	}
	out.Licenses = licensesToCyclone(component.Licenses)
	out.Properties = append(out.Properties, propertiesFromMap(component.Properties)...)
	if component.Scope != "" {
		out.Properties = append(out.Properties, Property{Name: "sbomb:component:scope", Value: component.Scope})
	}
	out.Properties = append(out.Properties, bsiProperties(domain.FileClassUnknown, out.Type)...)
	return out
}

func fileToCyclone(file domain.UsedFile, ref string) Component {
	canonical := file.ID.Canonical()
	out := Component{
		Type:   "file",
		Name:   baseName(canonical),
		BomRef: ref,
		Properties: []Property{
			{Name: "sbomb:path:canonical", Value: canonical},
		},
	}
	for _, algorithm := range sortedMapKeys(file.Hashes) {
		out.Hashes = append(out.Hashes, Hash{Alg: algorithm, Value: file.Hashes[algorithm]})
	}
	out.Properties = append(out.Properties, propertiesFromMap(file.Properties)...)
	out.Properties = append(out.Properties, bsiProperties(file.Class, "file")...)
	return out
}

// bsiProperties emits the three properties BSI TR-03183-2 requires per
// component, derived from the file class (section 1.5(3)).
func bsiProperties(class domain.FileClass, componentType string) []Property {
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

func runProperties(run sbomwriter.RunMetadata) []Property {
	properties := []Property{
		{Name: "sbomb:run:specVersion", Value: "1.6"},
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
	if !run.Reproducible {
		return properties
	}
	// Volatile run properties are omitted in reproducible mode (section 29).
	return properties
}

func licensesToCyclone(findings []domain.LicenseFinding) []License {
	licenses := make([]License, 0, len(findings))
	for _, finding := range findings {
		switch {
		case finding.Expression != "":
			licenses = append(licenses, License{Expression: finding.Expression})
		case finding.SPDXID != "":
			licenses = append(licenses, License{License: &LicenseIdentifier{ID: finding.SPDXID}})
		case finding.Name != "":
			licenses = append(licenses, License{License: &LicenseIdentifier{Name: finding.Name}})
		}
	}
	return licenses
}

// propertiesFromMap emits only catalogued properties. Discovery annotates
// files with internal markers in the same map; the property catalogue of
// appendix B governs what reaches a consumer, so anything outside the sbomb
// namespace stays inside the tool.
func propertiesFromMap(values map[string][]string) []Property {
	properties := make([]Property, 0, len(values))
	for _, name := range sortedSliceMapKeys(values) {
		if !strings.HasPrefix(name, "sbomb:") {
			continue
		}
		for _, value := range values[name] {
			properties = append(properties, Property{Name: name, Value: value})
		}
	}
	return properties
}

func firstDuplicateRef(components []Component, extra string) string {
	seen := map[string]bool{extra: true}
	for _, component := range components {
		if seen[component.BomRef] {
			return component.BomRef
		}
		seen[component.BomRef] = true
	}
	return ""
}

func baseName(canonical string) string {
	if index := strings.LastIndexByte(canonical, '/'); index >= 0 {
		return canonical[index+1:]
	}
	if index := strings.LastIndexByte(canonical, ':'); index >= 0 {
		return canonical[index+1:]
	}
	return canonical
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	var previous string
	for index, value := range values {
		if index > 0 && value == previous {
			continue
		}
		previous = value
		out = append(out, value)
	}
	return out
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedSliceMapKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// MarshalDocument is a convenience for callers that only need the bytes.
func MarshalDocument(document *sbomwriter.Document, options sbomwriter.Options) ([]byte, error) {
	var buffer bytes.Buffer
	if err := (Writer{}).Write(&buffer, document, options); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
