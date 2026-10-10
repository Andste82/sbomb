package mapping

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbommap"
	"github.com/example/sbomb/internal/sbomwriter"
)

// ErrNoCreationTime is the refusal of a document whose run states no creation
// time. SPDX requires one in every version, and under reproducible the only
// time a second run would state again is SOURCE_DATE_EPOCH -- so an empty
// timestamp is a refusal rather than a time made up here. The writer's
// Preflight refuses the same run before discovery; this is the backstop.
var ErrNoCreationTime = errors.New("an SPDX document must state when it was created, and the run states no time; " +
	"under --reproducible that time is SOURCE_DATE_EPOCH, which is unset or unreadable")

// Build derives the model of one document.
func Build(document *sbomwriter.Document, options Options) (*Model, error) {
	if document == nil {
		return nil, errors.New("no document to map")
	}
	if options.SpecVersion == "" {
		return nil, errors.New("no SPDX version to map the document for")
	}
	created, err := creationTime(document.Run.Timestamp)
	if err != nil {
		return nil, err
	}
	creator := document.Run.ToolVendor
	if creator == "" {
		creator = document.Run.ToolName
	}
	if creator == "" {
		return nil, errors.New("an SPDX document must name who created it, and the run names neither a tool vendor nor a tool")
	}
	table, err := sbommap.Identifiers(document)
	if err != nil {
		return nil, err
	}
	b := &builder{
		options:  options,
		table:    table,
		edges:    newEdgeBuilder(document, table),
		files:    map[string]*File{},
		licenses: map[string]License{},
		custom:   map[string]CustomLicense{},
		agents:   map[string]bool{creator: true},
	}
	model := &Model{
		SpecVersion: options.SpecVersion,
		Created:     created,
		Creator:     creator,
		Run:         runOf(document.Run, options.SpecVersion),
	}

	for _, file := range document.Files {
		b.usedFile(file)
	}
	model.Edges = b.edges.build()

	model.Packages = append(model.Packages, b.pkg(document.Product, RoleProduct, table.Product()))
	for _, artifact := range document.Artifacts {
		model.Packages = append(model.Packages, b.pkg(artifact, RoleArtifact, table.Component(artifact.ID)))
	}
	for _, component := range document.Components {
		model.Packages = append(model.Packages, b.pkg(component, RoleComponent, table.Component(component.ID)))
	}
	sort.SliceStable(model.Packages, func(i, j int) bool {
		if model.Packages[i].Role != model.Packages[j].Role {
			return model.Packages[i].Role < model.Packages[j].Role
		}
		return model.Packages[i].LocalID < model.Packages[j].LocalID
	})

	for _, file := range b.files {
		file.finish()
		if file.Origin&OriginUsed != 0 {
			file.KnownLeaf = b.edges.knownLeaf(file.LocalID)
		}
		model.Files = append(model.Files, *file)
	}
	sort.Slice(model.Files, func(i, j int) bool { return model.Files[i].LocalID < model.Files[j].LocalID })

	b.defineStrayRefs()
	for _, license := range b.licenses {
		model.Licenses = append(model.Licenses, license)
	}
	sort.Slice(model.Licenses, func(i, j int) bool { return model.Licenses[i].Expression < model.Licenses[j].Expression })
	for _, custom := range b.custom {
		model.Custom = append(model.Custom, custom)
	}
	sort.Slice(model.Custom, func(i, j int) bool { return model.Custom[i].Ref < model.Custom[j].Ref })
	for agent := range b.agents {
		model.Agents = append(model.Agents, agent)
	}
	sort.Strings(model.Agents)
	return model, nil
}

// Digest is the canonical encoding of the model, the input of the
// reproducible document identity (section 28.11.2). It contains the spec
// version, the creation time and the tool version, so a document in another
// version, pinned to another time or written by another build gets another
// identity.
//
// The encoding must be injective: two models that render differently must
// never share a digest, or two different documents would share every element
// IRI. encoding/json is not -- it replaces each byte of a string that is not
// valid UTF-8 with U+FFFD, while the renderer keeps that byte, percent-encoded,
// in the IRI of a file whose path carries it. So the model is walked here
// instead: the model has no maps and every slice is sorted, each string and
// byte slice is written with its length and every one of its bytes, each
// slice with its length, and each struct field after its name.
func Digest(model *Model) []byte {
	var buffer bytes.Buffer
	digestValue(&buffer, reflect.ValueOf(*model))
	return buffer.Bytes()
}

func digestValue(buffer *bytes.Buffer, value reflect.Value) {
	length := func(n int) { buffer.Write(binary.AppendUvarint(nil, uint64(n))) }
	switch value.Kind() {
	case reflect.String:
		length(value.Len())
		buffer.WriteString(value.String())
	case reflect.Bool:
		if value.Bool() {
			buffer.WriteByte(1)
		} else {
			buffer.WriteByte(0)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		buffer.Write(binary.AppendVarint(nil, value.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		buffer.Write(binary.AppendUvarint(nil, value.Uint()))
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8 {
			length(value.Len())
			buffer.Write(value.Bytes())
			return
		}
		length(value.Len())
		for i := 0; i < value.Len(); i++ {
			digestValue(buffer, value.Index(i))
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			name := value.Type().Field(i).Name
			length(len(name))
			buffer.WriteString(name)
			digestValue(buffer, value.Field(i))
		}
	case reflect.Pointer:
		if value.IsNil() {
			buffer.WriteByte(0)
			return
		}
		buffer.WriteByte(1)
		digestValue(buffer, value.Elem())
	default:
		// A map has no canonical order, and an interface or a float no
		// encoding chosen here; the model has none of them, and one added
		// later must be given an encoding before it reaches the identity.
		panic(fmt.Sprintf("mapping.Digest: no canonical encoding for %s", value.Type()))
	}
}

// creationTime normalises the run's RFC 3339 timestamp to the one form SPDX
// 3.0.1 accepts: UTC, whole seconds, with a Z.
func creationTime(timestamp string) (string, error) {
	if timestamp == "" {
		return "", ErrNoCreationTime
	}
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return "", fmt.Errorf("the run's timestamp %q is not an RFC 3339 time", timestamp)
	}
	return parsed.UTC().Format("2006-01-02T15:04:05Z"), nil
}

// The properties every format carries are derived once, in sbommap. The ones
// below are the SPDX-only groups of appendix B: facts CycloneDX states in a
// field of its own or not at all, which an SPDX document of any version
// carries as properties. They are derived here, in the version-neutral
// description, and in these helpers alone (with fieldProperties and
// findingProperties), so that a second SPDX version inherits them and no
// CycloneDX document is promised them.

// spdxRunProperties are the SPDX-only properties of the run.
func spdxRunProperties(run sbomwriter.RunMetadata) []Property {
	properties := []Property{{Name: "sbomb:run:reproducible", Value: strconv.FormatBool(run.Reproducible)}}
	for _, adapter := range run.Adapters {
		properties = append(properties, Property{Name: "sbomb:run:adapters", Value: adapter})
	}
	return properties
}

// spdxFileProperties are the SPDX-only properties of a used file: what
// CycloneDX states as the component's type, size and missing evidence.
func spdxFileProperties(used domain.UsedFile) []Property {
	var properties []Property
	if used.Class != "" {
		properties = append(properties, Property{Name: "sbomb:file:class", Value: string(used.Class)})
	}
	if used.Missing {
		properties = append(properties, Property{Name: "sbomb:file:missing", Value: "true"})
	} else if used.SizeBytes > 0 {
		properties = append(properties, Property{Name: "sbomb:file:size", Value: strconv.FormatInt(used.SizeBytes, 10)})
	}
	return properties
}

func runOf(run sbomwriter.RunMetadata, specVersion string) Run {
	properties := append(sbommap.RunProperties(run, specVersion), spdxRunProperties(run)...)
	return Run{
		ToolName:    run.ToolName,
		ToolVendor:  run.ToolVendor,
		ToolVersion: run.ToolVersion,
		Properties:  canonicalProperties(properties),
	}
}

type builder struct {
	options  Options
	table    *sbommap.Table
	edges    *edgeBuilder
	files    map[string]*File
	licenses map[string]License
	custom   map[string]CustomLicense
	agents   map[string]bool
}

func (b *builder) usedFile(used domain.UsedFile) {
	canonical := used.ID.Canonical()
	file := b.file(b.table.File(canonical), canonical)
	file.Origin |= OriginUsed
	file.Class = string(used.Class)
	file.Missing = used.Missing
	file.SizeBytes = used.SizeBytes
	if !used.Missing {
		for key, value := range used.Hashes {
			file.Hashes = append(file.Hashes, Hash{Key: key, Value: value})
		}
	}
	file.Properties = append(file.Properties, sbommap.FileProperties(used)...)
	file.Properties = append(file.Properties, spdxFileProperties(used)...)
}

// file returns the file of a local identity, creating it on first use. A file
// several components name, or that is both used and evidence, is one entry.
func (b *builder) file(local, canonical string) *File {
	if file, known := b.files[local]; known {
		return file
	}
	file := &File{LocalID: local, Canonical: canonical, Name: canonical}
	if canonical != "" {
		file.Properties = []Property{{Name: "sbomb:path:canonical", Value: canonical}}
	}
	b.files[local] = file
	return file
}

func (b *builder) pkg(component domain.Component, role PackageRole, local string) Package {
	out := Package{
		LocalID:     local,
		Role:        role,
		Name:        component.Name,
		Version:     component.Version,
		PURL:        component.PURL,
		Supplier:    component.Supplier,
		Originator:  component.Originator,
		Description: component.Description,
		Type:        component.Type,
		StatedType:  sbommap.EffectiveType(component),
		HeaderOnly:  component.HeaderOnly,
		Copyright:   component.Copyright,
		SourceInfo:  component.Modification.Signal,
		KnownLeaf:   b.edges.knownLeaf(local),
	}
	if component.CPE != "" {
		out.CPEs = []string{component.CPE}
	}
	if component.VCS != nil {
		out.VCSURL = component.VCS.URL
	}
	for _, agent := range []string{component.Supplier, component.Originator} {
		if agent != "" {
			b.agents[agent] = true
		}
	}

	properties := sbommap.VCSProperties(component.VCS)
	properties = append(properties, sbommap.ComponentProperties(component)...)
	properties = append(properties, fieldProperties(component)...)
	out.Properties = canonicalProperties(properties)

	for _, finding := range component.Licenses {
		if use, ok := b.licenseUse(finding, component, "", findingProperties(finding)); ok {
			out.Concluded = append(out.Concluded, use)
		}
	}
	for _, finding := range component.LicenseEvidence {
		if use, ok := b.licenseUse(finding, component, "", findingProperties(finding)); ok {
			out.Declared = append(out.Declared, use)
		}
	}
	out.Concluded = canonicalUses(out.Concluded)
	out.Declared = canonicalUses(out.Declared)

	for _, exclusion := range component.CVEExclusions {
		out.CVEExclusions = append(out.CVEExclusions, CVEExclusion{ID: exclusion.CVE, Reason: exclusion.Reason})
	}
	sort.Slice(out.CVEExclusions, func(i, j int) bool {
		if out.CVEExclusions[i].ID != out.CVEExclusions[j].ID {
			return out.CVEExclusions[i].ID < out.CVEExclusions[j].ID
		}
		return out.CVEExclusions[i].Reason < out.CVEExclusions[j].Reason
	})
	out.CVEExclusions = dedupe(out.CVEExclusions)

	evidence := b.licenseArtifacts(component)
	for _, statement := range component.Copyrights {
		if statement.Text == "" {
			continue
		}
		canonical := statement.File.Canonical()
		if canonical == "" {
			// A statement that names no file still has to be carried, and
			// it is a copyright notice like any other: the package's
			// copyright text, beside the curated notice. (Attribution text
			// is for acknowledgements a distributor adds, not for notices.)
			out.UnfiledCopyright = append(out.UnfiledCopyright, statement.Text)
			continue
		}
		local := sbommap.FileIdentity(canonical)
		file := b.file(local, canonical)
		file.Origin |= OriginEvidence
		file.Copyright = append(file.Copyright, statement.Text)
		evidence = append(evidence, local)
	}
	sort.Strings(out.UnfiledCopyright)
	out.UnfiledCopyright = dedupe(out.UnfiledCopyright)
	sort.Strings(evidence)
	out.EvidenceFiles = dedupe(evidence)
	out.Patches = b.patches(component.Modification.Patches, local, component.Name)
	return out
}

// licenseArtifacts turns the retained licence files of section 22.9 into
// evidence files, and returns their local identities.
func (b *builder) licenseArtifacts(component domain.Component) []string {
	curated := sbommap.CuratedLicense(component)
	var locals []string
	for _, artifact := range component.LicenseArtifacts {
		canonical := artifact.File.Canonical()
		local := sbommap.FileIdentity(canonical)
		file := b.file(local, canonical)
		file.Origin |= OriginEvidence | OriginLicenseFile
		if artifact.SHA256 != "" && !file.Missing {
			file.Hashes = append(file.Hashes, Hash{Key: "SHA-256", Value: artifact.SHA256})
		}
		if b.options.LicenseText == sbomwriter.LicenseTextEvidence &&
			artifact.Kind == domain.LicenseArtifactLicense && len(artifact.Bytes) > 0 {
			file.LicenseText = append(file.LicenseText, artifact.Bytes)
		}
		if artifact.DetectedID != "" {
			var properties []Property
			if artifact.Technique != "" {
				properties = append(properties, Property{Name: "sbomb:license:technique", Value: artifact.Technique})
			}
			finding := domain.LicenseFinding{Expression: artifact.DetectedID}
			if use, ok := b.licenseUse(finding, component, canonical, properties); ok {
				use.Acknowledgement = fileAcknowledgement(curated)
				file.Detected = append(file.Detected, use)
			}
		}
		locals = append(locals, local)
	}
	return locals
}

// fileAcknowledgement says how SPDX states the licence detected in a retained
// licence file. The 3.0.1 vocabulary draws the line differently than
// CycloneDX's acknowledgement does: hasDeclaredLicense is that the artifact
// "was discovered to actually contain" the licence, "for example as detected
// by use of automated tooling", and hasConcludedLicense is what "the SPDX data
// creator" concluded governs it. Every detection technique of section 22.3 --
// an SPDX-License-Identifier line, a digest, a template -- is that discovery,
// and it is the same discovery the component's own hasDeclaredLicense states
// for the same file; the technique travels in sbomb:license:technique. Only a
// curated licence is sbomb's conclusion, and then the file is stated under it
// as concluded. (CycloneDX keeps its own split, in its writer: there
// "declared" means the authors said it, and a digest match is concluded.)
func fileAcknowledgement(curated bool) string {
	if curated {
		return "concluded"
	}
	return "declared"
}

// licenseUse routes one finding and records its target. readFrom is the file
// the finding was read from, when the caller knows it; otherwise the finding's
// own Source says.
//
// A reference that stands for the whole finding is defined by the licence
// text section 22.9 retained for that file, when there is one, and the text
// is then part of the reference's identity: two components that each observed
// a licence called "Proprietary" in files with different texts are under two
// different licences, and one shared definition would state that the second
// is under the first one's text. A reference defined by the observed name
// alone stays shared -- the name is all either observation says.
func (b *builder) licenseUse(finding domain.LicenseFinding, component domain.Component, readFrom string, properties []Property) (LicenseUse, bool) {
	target, ok := routeLicense(finding, b.options.CustomAdditions)
	if !ok {
		return LicenseUse{}, false
	}
	if target.whole {
		if readFrom == "" {
			readFrom = finding.Source
		}
		if retained, found := b.retainedText(component, readFrom); found {
			custom := &target.custom[0]
			sum := sha256.Sum256(retained)
			custom.Ref += "-" + hex.EncodeToString(sum[:])[:8]
			target.license.Expression = custom.Ref
			if utf8.Valid(retained) {
				custom.Text, custom.TextRetained = string(retained), true
			} else {
				custom.TextFile = readFrom
			}
		}
	}
	if target.license.Kind == LicenseExpression {
		b.licenses[target.license.Expression] = target.license
	}
	for _, custom := range target.custom {
		b.defineCustom(custom)
	}
	return LicenseUse{License: target.license, Properties: canonicalProperties(properties)}, true
}

// retainedText is the licence text section 22.9 retained for a file, when the
// run asked for licence texts. Bytes that are not UTF-8 are returned as well:
// they cannot be a licence text field, but they are retained, and the caller
// says where they travel instead.
func (b *builder) retainedText(component domain.Component, canonical string) ([]byte, bool) {
	if b.options.LicenseText != sbomwriter.LicenseTextEvidence || canonical == "" {
		return nil, false
	}
	for _, artifact := range component.LicenseArtifacts {
		if artifact.Kind == domain.LicenseArtifactLicense && artifact.File.Canonical() == canonical && len(artifact.Bytes) > 0 {
			return artifact.Bytes, true
		}
	}
	return nil, false
}

// defineCustom keeps one definition per reference: a retained text over
// retained bytes that are not text, those over the bare name, and between two
// of a kind the first in byte order, so that the choice does not depend on
// which component was mapped first.
func (b *builder) defineCustom(custom CustomLicense) {
	rank := func(c CustomLicense) int {
		switch {
		case c.TextRetained:
			return 2
		case c.TextFile != "":
			return 1
		}
		return 0
	}
	existing, known := b.custom[custom.Ref]
	if known {
		if rank(existing) != rank(custom) {
			if rank(existing) > rank(custom) {
				return
			}
		} else if existing.Text < custom.Text || existing.Text == custom.Text && existing.TextFile <= custom.TextFile {
			return
		}
	}
	b.custom[custom.Ref] = custom
}

// defineStrayRefs defines a LicenseRef-sbomb-* or AdditionRef-sbomb-* that
// reached the document inside an expression sbomb did not mint -- a build that
// wrote one into its own file. Every reference in sbomb's namespace is
// defined, so a reader never meets one it cannot look up.
func (b *builder) defineStrayRefs() {
	for _, license := range b.licenses {
		parsed, err := ParseExpression(license.Expression)
		if err != nil {
			continue
		}
		for _, id := range parsed.Licenses {
			if _, known := b.custom[id]; IsSbombLicenseRef(id) && !known {
				b.custom[id] = CustomLicense{Ref: id, Name: id, Text: id}
			}
		}
		for _, id := range parsed.Additions {
			if _, known := b.custom[id]; IsSbombAdditionRef(id) && !known {
				b.custom[id] = CustomLicense{Ref: id, Name: id, Text: id, Addition: true}
			}
		}
	}
}

// patches turns the recorded patches of a component into patch files. A
// patch the metadata recorded without a file name -- Conan's base64 payload
// patches have none -- still needs one: SPDX 3.0.1 raises name to "minCount
// 1" for every File. It is named after its place among the component's
// patches, which is all the metadata says about it, and Patch.File stays
// empty so that no renderer mistakes the stated name for a file the metadata
// named.
func (b *builder) patches(patches []domain.Patch, component, componentName string) []Patch {
	sorted := append([]domain.Patch(nil), patches...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, c := sorted[i], sorted[j]
		if a.Type != c.Type {
			return a.Type < c.Type
		}
		if a.File != c.File {
			return a.File < c.File
		}
		if a.Source != c.Source {
			return a.Source < c.Source
		}
		return a.Description < c.Description
	})
	sorted = dedupe(sorted)
	out := make([]Patch, 0, len(sorted))
	for index, patch := range sorted {
		local := "patch:" + component + "/" + strconv.Itoa(index+1)
		file := b.file(local, "")
		file.Name = patch.File
		if file.Name == "" {
			owner := componentName
			if owner == "" {
				owner = component
			}
			file.Name = fmt.Sprintf("patch %d of %s (the metadata names no patch file)", index+1, owner)
		}
		file.Description = patch.Description
		file.Origin |= OriginPatch
		file.PatchFor = component
		if patch.Type != "" {
			file.Properties = append(file.Properties, Property{Name: "sbomb:patch:type", Value: patch.Type})
		}
		if patch.Source != "" {
			file.Properties = append(file.Properties, Property{Name: "sbomb:patch:source", Value: patch.Source})
		}
		out = append(out, Patch{LocalID: local, File: patch.File, Type: patch.Type, Source: patch.Source, Description: patch.Description})
	}
	return out
}

// fieldProperties are the properties a component's own fields imply. The
// generate pipeline writes most of them into the property map as well, with
// the same values, so on its documents these add nothing; they are derived
// here from the fields so that a document built by any other producer says the
// same, and so that the field and the property cannot silently disagree. The
// two version properties exist only here: CycloneDX states version provenance
// in evidence.identity, which SPDX has no field for.
//
// sbomb:component:type exists only here as well. CycloneDX states the type in
// its own field; an SPDX version states a purpose, from a vocabulary that has
// no word for some types (cryptographic-asset in 3.0.1; platform, data and
// several more in 2.3) and spells others differently. The property keeps the
// exact type whatever the version's purpose says, so that no version has to
// decide on its own whether its purpose lost it.
func fieldProperties(component domain.Component) []Property {
	var properties []Property
	add := func(name, value string) {
		if value != "" {
			properties = append(properties, Property{Name: name, Value: value})
		}
	}
	add("sbomb:component:type", component.Type)
	if component.Version != "" && component.VersionSource != "" {
		add("sbomb:version:source", component.VersionSource)
		add("sbomb:version:confidence", string(component.VersionConf))
	}
	if component.Root != nil {
		add("sbomb:component:root", component.Root.Canonical())
	}
	add("sbomb:component:detectedBy", component.DetectedBy)
	add("sbomb:component:distributionRole", component.DistributionRole)
	for _, form := range component.LinkageForms {
		add("sbomb:component:linkageForm", form)
	}
	add("sbomb:component:archiveMembersUsed", component.ArchiveMembersUsed)
	if component.HeaderOnly {
		add("sbomb:component:headerOnly", "true")
	}
	add("sbomb:component:modified", string(component.Modification.Status))
	add("sbomb:component:vcsCommit", component.Modification.Commit)
	for _, obligation := range component.SourceObligations {
		add("sbomb:component:sourceObligation", obligation)
	}
	return properties
}

// findingProperties qualify one licence statement: how the licence was
// established, from where, how confidently, and what disagreed with it.
// CycloneDX can only say this once per component; SPDX states each licence as
// its own relationship, and the qualification belongs on the statement it
// qualifies.
func findingProperties(finding domain.LicenseFinding) []Property {
	var properties []Property
	add := func(name, value string) {
		if value != "" {
			properties = append(properties, Property{Name: name, Value: value})
		}
	}
	add("sbomb:license:evidenceClass", finding.Evidence)
	add("sbomb:license:technique", finding.Technique)
	add("sbomb:license:source", finding.Source)
	add("sbomb:license:confidence", string(finding.Confidence))
	add("sbomb:license:reason", finding.Reason)
	for _, conflict := range finding.Conflicts {
		add("sbomb:license:conflictingValue", conflict)
	}
	return properties
}

// finish puts a file's collected statements into canonical order.
func (f *File) finish() {
	// One digest per algorithm: a retained licence file that is also a used
	// file was hashed twice, and the used-file hash came first -- Build reads
	// every used file before any component's licence artifacts. The sort is
	// stable and by algorithm alone, so that hash is the one kept: ordering by
	// value as well made the smaller digest win, and a used file whose two
	// reads disagreed would have stated the licence read's digest instead of
	// the one the evidence chain hashed.
	sort.SliceStable(f.Hashes, func(i, j int) bool { return f.Hashes[i].Key < f.Hashes[j].Key })
	hashes := f.Hashes[:0]
	for _, hash := range f.Hashes {
		if len(hashes) > 0 && hashes[len(hashes)-1].Key == hash.Key {
			continue
		}
		hashes = append(hashes, hash)
	}
	f.Hashes = hashes
	if len(f.Hashes) == 0 {
		f.Hashes = nil
	}
	sort.Strings(f.Copyright)
	f.Copyright = dedupe(f.Copyright)
	sort.Slice(f.LicenseText, func(i, j int) bool { return bytes.Compare(f.LicenseText[i], f.LicenseText[j]) < 0 })
	texts := f.LicenseText[:0]
	for _, text := range f.LicenseText {
		if len(texts) > 0 && bytes.Equal(texts[len(texts)-1], text) {
			continue
		}
		texts = append(texts, text)
	}
	f.LicenseText = texts
	if len(f.LicenseText) == 0 {
		f.LicenseText = nil
	}
	f.Detected = canonicalUses(f.Detected)
	f.Properties = canonicalProperties(f.Properties)
}

// canonicalProperties sorts a property set by (name, value) and drops exact
// repeats: a property derived from a field and written by the builder too is
// one statement.
func canonicalProperties(properties []Property) []Property {
	if len(properties) == 0 {
		return nil
	}
	out := append([]Property(nil), properties...)
	sbommap.SortProperties(out)
	return dedupe(out)
}

func canonicalUses(uses []LicenseUse) []LicenseUse {
	if len(uses) == 0 {
		return nil
	}
	sort.SliceStable(uses, func(i, j int) bool { return useKey(uses[i]) < useKey(uses[j]) })
	out := uses[:0]
	for _, use := range uses {
		if len(out) > 0 && useKey(out[len(out)-1]) == useKey(use) {
			continue
		}
		out = append(out, use)
	}
	return out
}

func useKey(use LicenseUse) string {
	var key strings.Builder
	fmt.Fprintf(&key, "%d\x00%s\x00%s", use.License.Kind, use.License.Expression, use.Acknowledgement)
	for _, property := range use.Properties {
		key.WriteString("\x00" + property.Name + "\x01" + property.Value)
	}
	return key.String()
}

// dedupe drops adjacent repeats from a sorted slice.
func dedupe[T comparable](values []T) []T {
	if len(values) == 0 {
		return nil
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
