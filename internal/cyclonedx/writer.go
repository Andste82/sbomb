package cyclonedx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/pathmodel"
	"github.com/example/sbomb/internal/sbomwriter"
)

// Writer serializes a document as CycloneDX JSON, in either of the
// specification versions this build knows.
type Writer struct{}

func init() { sbomwriter.Register(Writer{}) }

func (Writer) ID() string { return "cyclonedx-json" }

// The specification versions this build can write and validate.
const (
	Version16 = "1.6"
	Version17 = "1.7"
)

// supportedVersions is the one list every other check derives from: the
// writer's Versions, the schema table, the semantic validator, and the
// configuration enum are all this, so none of them can drift.
var supportedVersions = []string{Version16, Version17}

// Versions lists what this writer can emit. 1.7 is additive over 1.6 -- 108
// definitions against 91, nothing removed, the same required top-level fields
// -- so a document written at 1.6 stays structurally valid at 1.7.
func (Writer) Versions() []string { return append([]string(nil), supportedVersions...) }

// DefaultVersion stays 1.6 while the compliance target does. Section 28.1
// pins it because BSI TR-03183-2 names it as the minimum, and nothing in 1.7
// changes that; 1.7 is written when a consumer asks for it (section 32.2).
func (Writer) DefaultVersion() string { return Version16 }

// Detect recognises a CycloneDX JSON document by its own bomFormat field, and
// reports the specification version it declares -- including one this build
// cannot write, so that `validate` can say what it is looking at rather than
// only that it does not know.
func (Writer) Detect(data []byte) (string, bool) {
	var header struct {
		BomFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return "", false
	}
	if header.BomFormat != "CycloneDX" {
		return "", false
	}
	return header.SpecVersion, true
}

// resolveSpecVersion turns the caller's choice into the version to write. An
// empty value is the default rather than an error; anything else must be a
// version this writer emits, because silently downgrading a document a
// consumer asked for is worse than refusing.
func resolveSpecVersion(requested string) (string, error) {
	if requested == "" {
		return Writer{}.DefaultVersion(), nil
	}
	for _, supported := range supportedVersions {
		if supported == requested {
			return requested, nil
		}
	}
	return "", fmt.Errorf("unsupported CycloneDX specVersion %q; supported: %s", requested, strings.Join(supportedVersions, ", "))
}

// SupportsVersion reports whether this build can write and validate a version.
func SupportsVersion(version string) bool {
	_, err := resolveSpecVersion(version)
	return err == nil && version != ""
}

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
	specVersion, err := resolveSpecVersion(options.SpecVersion)
	if err != nil {
		return BOM{}, err
	}
	refs := newRefTable()

	product := componentToCyclone(document.Product, refs.forProduct(document.Product), specVersion)
	if product.Type == "" {
		product.Type = "application"
	}

	components := make([]Component, 0, len(document.Artifacts)+len(document.Components)+len(document.Files))
	for _, artifact := range document.Artifacts {
		components = append(components, componentToCyclone(artifact, refs.forArtifact(artifact), specVersion))
	}
	for _, grouping := range document.Components {
		components = append(components, componentToCyclone(grouping, refs.forComponent(grouping), specVersion))
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
		Properties: runProperties(document.Run, specVersion),
	}
	if !options.Reproducible {
		metadata.Timestamp = document.Run.Timestamp
	}
	if options.TLP != "" {
		// 1.6 has nowhere to put this. Dropping it silently would make a
		// configured distribution constraint disappear from the document it
		// was meant to constrain, so the caller is told instead.
		if !supportsDistributionConstraints(specVersion) {
			return BOM{}, fmt.Errorf("a TLP classification needs CycloneDX 1.7; this document is %s", specVersion)
		}
		metadata.DistributionConstraints = &DistributionConstraints{TLP: options.TLP}
	}

	bom := BOM{
		BomFormat:    "CycloneDX",
		SpecVersion:  specVersion,
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

func componentToCyclone(component domain.Component, ref, specVersion string) Component {
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
	// Observed, not concluded. A licence file holding two complete texts says
	// which licences are present and nothing about how they relate, so the
	// finding goes here and out.Licenses stays NOASSERTION until curated.
	observed := observedLicensesToCyclone(component.LicenseEvidence, specVersion)
	identity := versionIdentityEvidence(component)
	if len(observed) > 0 || len(identity) > 0 {
		out.Evidence = &Evidence{Licenses: observed, Identity: identity}
	}
	// 1.7 can say that the environment provides a component; 1.6 cannot, and
	// leaves the fact to sbomb:component:scope and the build-environment
	// grouping of section 24.2, which both versions carry.
	out.IsExternal = component.EnvironmentProvided && externalComponentsAllowed(specVersion)
	out.ExternalReferences, out.Properties = vcsToCyclone(component.VCS, specVersion)
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

func runProperties(run sbomwriter.RunMetadata, specVersion string) []Property {
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
	if !run.Reproducible {
		return properties
	}
	// Volatile run properties are omitted in reproducible mode (section 29).
	return properties
}

// observedLicensesToCyclone renders licence evidence.
//
// At 1.6 it is a list of licence identifiers and nothing else, because
// licenseChoice there is a choice: a list of licence objects, or a tuple of
// exactly one expression. That restriction and deviation D19 agree for the
// commonest observation -- a file holding two complete texts says which
// licences are present and nothing about how they relate, so a list is the
// only honest form.
//
// 1.7 relaxes licenseChoice: one array may mix licence objects and SPDX
// expressions. That does not change what an observation may claim. It only
// lifts the ceiling for the case where the observation itself carries a
// compound expression -- an SPDX-License-Identifier line reading
// "MIT OR Apache-2.0" states the relation, in the text, rather than leaving a
// reader to infer it. Where it does, 1.7 can now say so beside observations
// that did not. A bare identifier stays an identifier at both versions, so a
// 1.7 document differs from its 1.6 twin only where the extra expressiveness
// is actually used.
func observedLicensesToCyclone(findings []domain.LicenseFinding, specVersion string) []License {
	licenses := make([]License, 0, len(findings))
	for _, finding := range findings {
		switch {
		case mixedLicenseChoiceAllowed(specVersion) && isCompoundExpression(finding.Expression):
			licenses = append(licenses, License{Expression: finding.Expression})
		case finding.SPDXID != "":
			licenses = append(licenses, License{License: &LicenseIdentifier{ID: finding.SPDXID}})
		case finding.Name != "":
			licenses = append(licenses, License{License: &LicenseIdentifier{Name: finding.Name}})
		}
	}
	return licenses
}

// versionIdentityEvidence says where the component's version came from.
//
// sbomb has always worked this out and never published it: the source and its
// confidence went to the review report and nowhere else, while the document
// carried a bare version string a consumer could not weigh. CycloneDX has a
// field for exactly this claim, and it has had it since 1.5 -- so this is
// written at both specification versions, per section 28.1.
//
// A component with a version but no recorded source produces nothing. That is
// not a gap to fill with a guess: a claim about how a value was established is
// worth less than nothing when it is invented.
func versionIdentityEvidence(component domain.Component) []IdentityEvidence {
	if component.Version == "" || component.VersionSource == "" {
		return nil
	}
	confidence := component.VersionConf.Float()
	return []IdentityEvidence{{
		Field:          "version",
		ConcludedValue: component.Version,
		Confidence:     confidence,
		Methods: []Method{{
			Technique: techniqueForVersionSource(component.VersionSource),
			// The exact source stays here. The technique vocabulary is closed
			// and coarse -- three of sbomb's sources share manifest-analysis --
			// so the field that survives the mapping is the one that says
			// which manifest.
			Value:      component.VersionSource,
			Confidence: confidence,
		}},
	}}
}

// techniqueForVersionSource maps a version source onto the closed technique
// vocabulary of CycloneDX. Anything unrecognised is "other", which is a
// vocabulary entry rather than a failure: claiming the nearest-looking
// technique for a source nobody has mapped would be a guess presented as a
// measurement.
func techniqueForVersionSource(source string) string {
	switch source {
	case "curated":
		// Declared by whoever wrote the configuration, not derived.
		return "attestation"
	case "conan", "vcpkg", "fetchcontent", "cmake", "bundled-sbom", "idf", "cpm":
		// cmake is CMAKE_PROJECT_VERSION, read from the File API cache: the
		// build system's own manifest, in the same sense as a package
		// manager's. bundled-sbom is a document the upstream shipped inside
		// the package: a stronger origin than any manifest, but still a
		// declaration read out of a file, which is what this technique names.
		// idf is the ESP-IDF component manager's dependencies.lock and the
		// idf_component.yml beside the component it unpacked: a lock file and a
		// manifest, both declarations read out of a file. cpm is the lock
		// CPM.cmake writes into the build directory while configuring, which is
		// a declaration read out of a file in the same sense.
		return "manifest-analysis"
	case "header":
		return "source-code-analysis"
	case "go-build-info":
		return "binary-analysis"
	default:
		// git, git-describe, git-commit and anything added later. Repository
		// metadata is none of the listed techniques.
		return "other"
	}
}

// vcsToCyclone renders a component's repository record, and returns the
// external references and the component properties that carry it.
//
// The URL goes to externalReferences at both versions: CycloneDX has had the
// `vcs` reference type since well before 1.6, and a standard field beats a
// property in the sbomb namespace wherever the standard has one. That is why
// this is not gated on the version, and why the 1.6 output moved when it
// landed.
//
// The commit and the dirty flag have no standard field of their own. 1.7 gives
// external references a property bag, which is where they belong -- beside the
// URL they qualify rather than loose on the component. At 1.6 there is no such
// bag, so they stay component properties. That difference is the one thing
// here the version decides.
func vcsToCyclone(record *domain.VCSRecord, specVersion string) ([]ExternalReference, []Property) {
	if record == nil {
		return nil, nil
	}
	qualifiers := make([]Property, 0, 2)
	if record.Commit != "" {
		qualifiers = append(qualifiers, Property{Name: "sbomb:component:vcsCommit", Value: record.Commit})
	}
	if record.Dirty {
		qualifiers = append(qualifiers, Property{Name: "sbomb:component:vcsDirty", Value: "true"})
	}
	if record.URL == "" {
		// Nothing to hang a reference on; the qualifiers are all there is.
		return nil, qualifiers
	}
	reference := ExternalReference{URL: record.URL, Type: "vcs"}
	if referencePropertiesAllowed(specVersion) {
		reference.Properties = qualifiers
		qualifiers = nil
	}
	return []ExternalReference{reference}, qualifiers
}

// referencePropertiesAllowed reports whether an external reference may carry a
// property bag. 1.7 introduced it.
func referencePropertiesAllowed(specVersion string) bool { return specVersion != Version16 }

// externalComponentsAllowed reports whether a component may be marked as
// provided by the environment. 1.7 introduced isExternal.
//
// No versionRange goes with it. The schema permits one only alongside
// isExternal, and it has to be a vers range; DT_NEEDED records a soname and
// nothing more, and deriving a range from whatever the build host happens to
// have installed would describe that host rather than the product.
func externalComponentsAllowed(specVersion string) bool { return specVersion != Version16 }

// mixedLicenseChoiceAllowed reports whether one licenses array may hold both
// licence objects and SPDX expressions. Only 1.6 forbids it.
func mixedLicenseChoiceAllowed(specVersion string) bool { return specVersion != Version16 }

// supportsDistributionConstraints reports whether metadata may carry a TLP
// classification. 1.7 introduced it.
func supportsDistributionConstraints(specVersion string) bool { return specVersion != Version16 }

// SupportsDistributionConstraints is the exported form, for the command line
// to refuse a configured TLP before any work is done rather than at the write.
func SupportsDistributionConstraints(specVersion string) bool {
	return supportsDistributionConstraints(specVersion)
}

// isCompoundExpression reports whether an SPDX expression states a relation
// between licences rather than naming one. "MIT" is not compound and is better
// rendered as the identifier it is; "MIT OR Apache-2.0" is, and rendering it
// as an identifier would invent a licence by that name.
func isCompoundExpression(expression string) bool {
	fields := strings.Fields(expression)
	if len(fields) < 2 {
		return strings.ContainsAny(expression, "()")
	}
	for _, field := range fields {
		switch strings.ToUpper(strings.Trim(field, "()")) {
		case "AND", "OR", "WITH":
			return true
		}
	}
	return strings.ContainsAny(expression, "()")
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
