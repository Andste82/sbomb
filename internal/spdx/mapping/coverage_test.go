package mapping_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
	"github.com/example/sbomb/internal/spdx"
)

// rendered is the synthetic all-fields document as the SPDX writer writes it,
// read back for the assertions of the coverage test.
type rendered struct {
	raw   string
	graph []map[string]any
	byID  map[string]map[string]any
}

func renderSynthetic(t *testing.T, document *sbomwriter.Document, options sbomwriter.Options) (*rendered, error) {
	t.Helper()
	var buffer bytes.Buffer
	if err := (spdx.Writer{}).Write(&buffer, document, options); err != nil {
		return nil, err
	}
	// The coverage is only worth something for a document sbomb would write:
	// a field that reached an invalid document has not reached a reader.
	if err := (spdx.Writer{}).Validate(bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("the rendered document is not conformant: %v", err)
	}
	if err := (spdx.Writer{}).CheckOutput(buffer.Bytes()); err != nil {
		t.Fatalf("the rendered document breaks an invariant of sbomb's own output: %v", err)
	}
	out := &rendered{raw: buffer.String(), byID: map[string]map[string]any{}}
	var root struct {
		Graph []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal(buffer.Bytes(), &root); err != nil {
		t.Fatal(err)
	}
	out.graph = root.Graph
	for _, node := range root.Graph {
		if id, ok := node["spdxId"].(string); ok {
			out.byID[local(id)] = node
		}
	}
	return out, nil
}

// local is the unescaped local identity of an IRI, or the value itself.
func local(iri string) string {
	if _, fragment, found := strings.Cut(iri, "#"); found {
		unescaped, err := url.PathUnescape(fragment)
		if err == nil {
			return unescaped
		}
		return fragment
	}
	return iri
}

func (r *rendered) node(id string) (map[string]any, error) {
	node, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("no element %s", id)
	}
	return node, nil
}

// has checks that a key of an element holds a value, directly or in an array.
func (r *rendered) has(id, key, value string) error {
	node, err := r.node(id)
	if err != nil {
		return err
	}
	switch got := node[key].(type) {
	case string:
		if got == value || local(got) == value {
			return nil
		}
	case []any:
		for _, item := range got {
			if text, ok := item.(string); ok && (text == value || local(text) == value) {
				return nil
			}
			if object, ok := item.(map[string]any); ok {
				for _, field := range object {
					if field == value {
						return nil
					}
					if list, ok := field.([]any); ok && len(list) > 0 && list[0] == value {
						return nil
					}
				}
			}
		}
	}
	return fmt.Errorf("%s.%s does not hold %q (it is %v)", id, key, value, node[key])
}

func (r *rendered) prop(id, name, value string) error {
	node, err := r.node(id)
	if err != nil {
		return err
	}
	if !nodeHasProperty(node, name, value) {
		return fmt.Errorf("%s has no property %s=%s", id, name, value)
	}
	return nil
}

func nodeHasProperty(node map[string]any, name, value string) bool {
	extensions, _ := node["extension"].([]any)
	for _, extension := range extensions {
		for _, entry := range extension.(map[string]any)["extension_cdxProperty"].([]any) {
			object := entry.(map[string]any)
			if object["extension_cdxPropName"] == name && object["extension_cdxPropValue"] == value {
				return true
			}
		}
	}
	return false
}

// rel checks a relationship of a type from one element to another, and
// returns it for further checks.
func (r *rendered) rel(from, relationshipType, to string) (map[string]any, error) {
	for _, node := range r.graph {
		source, _ := node["from"].(string)
		if local(source) != from || node["relationshipType"] != relationshipType {
			continue
		}
		for _, target := range node["to"].([]any) {
			if local(target.(string)) == to {
				return node, nil
			}
		}
	}
	return nil, fmt.Errorf("no %s relationship from %s to %s", relationshipType, from, to)
}

func (r *rendered) relProp(from, relationshipType, to, name, value string) error {
	for _, node := range r.graph {
		source, _ := node["from"].(string)
		if local(source) != from || node["relationshipType"] != relationshipType {
			continue
		}
		for _, target := range node["to"].([]any) {
			if local(target.(string)) == to && nodeHasProperty(node, name, value) {
				return nil
			}
		}
	}
	return fmt.Errorf("no %s relationship from %s to %s qualified by %s=%s", relationshipType, from, to, name, value)
}

func (r *rendered) absent(sentinel string) error {
	if strings.Contains(r.raw, sentinel) {
		return fmt.Errorf("%q reached the document", sentinel)
	}
	return nil
}

func (r *rendered) present(sentinel string) error {
	if !strings.Contains(r.raw, sentinel) {
		return fmt.Errorf("%q is not in the document", sentinel)
	}
	return nil
}

func all(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func ignore[T any](_ T, err error) error { return err }

const (
	kitchen      = "component:kitchen"
	kitchenC     = "file:build:_deps/kitchen-src/kitchen.c"
	kitchenH     = "file:build:_deps/kitchen-src/kitchen.h"
	kitchenLic   = "file:build:_deps/kitchen-src/LICENSE"
	kitchenNote  = "file:build:_deps/kitchen-src/NOTICE"
	product      = "product:sentinel-product"
	app          = "artifact:build:bin/app"
	firstPatch   = "patch:component:kitchen/1"
	missingFile  = "file:project:hdr/hdr.h"
	concludedMIT = "license:MIT OR Apache-2.0"
	// licenceTextLocator is the retained MIT text of kitchen's LICENSE,
	// attached to that file byte for byte.
	licenceTextLocator = "data:text/plain;charset=utf-8;base64,c2VudGluZWwgbGljZW5jZSB0ZXh0IG9mIE1JVAo="
	// kitchenCopyright is the curated notice and, after it, the statement
	// that names no file.
	kitchenCopyright = "Copyright sentinel curated notice\nCopyright sentinel holder C, of no file"
)

// TestEveryDocumentFieldReachesTheDocument is the completeness guarantee of
// the SPDX writer: every field of every type a Document reaches has a row
// here, and each row follows the field's value from the synthetic document
// into the rendered SPDX document -- to the element, key, property or
// relationship section 28.11.3 says it goes to -- or, for a field that is not
// rendered, asserts that its value left no trace and says why. A field added
// to any of these types without a row fails the test, so nothing sbomb knows
// can silently stop reaching the document.
func TestEveryDocumentFieldReachesTheDocument(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	doc, err := renderSynthetic(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions())
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]func(r *rendered) error{
		"sbomwriter.Document.Product": func(r *rendered) error {
			return all(r.has(product, "type", "software_Package"), r.has("sbom", "rootElement", product))
		},
		"sbomwriter.Document.Artifacts":  func(r *rendered) error { return ignore(r.rel(product, "contains", app)) },
		"sbomwriter.Document.Components": func(r *rendered) error { return r.has(kitchen, "type", "software_Package") },
		"sbomwriter.Document.Files":      func(r *rendered) error { return r.has(kitchenC, "type", "software_File") },
		"sbomwriter.Document.Relations":  func(r *rendered) error { return ignore(r.rel(kitchen, "contains", kitchenC)) },
		// Not rendered, as in CycloneDX: findings are written by
		// --findings-json and the review report, never into the SBOM.
		"sbomwriter.Document.Findings": func(r *rendered) error {
			return all(r.absent("SENTINEL_FINDING_NEVER_RENDERED"), r.absent("sentinel finding message"))
		},
		"sbomwriter.Document.Run": func(r *rendered) error { return r.prop("document", "sbomb:run:toolVersion", "0.0.0-sentinel") },

		"sbomwriter.RunMetadata.ToolVersion": func(r *rendered) error {
			return all(r.prop("document", "sbomb:run:toolVersion", "0.0.0-sentinel"), r.prop("tool:sbomb", "sbomb:run:toolVersion", "0.0.0-sentinel"))
		},
		"sbomwriter.RunMetadata.Timestamp":    func(r *rendered) error { return r.present(`"created": "2023-11-14T22:13:20Z"`) },
		"sbomwriter.RunMetadata.Reproducible": func(r *rendered) error { return r.prop("document", "sbomb:run:reproducible", "true") },
		"sbomwriter.RunMetadata.PolicyProfile": func(r *rendered) error {
			return r.prop("document", "sbomb:run:policyProfile", "sentinel-policy-profile")
		},
		"sbomwriter.RunMetadata.BuildConfig": func(r *rendered) error { return r.prop("document", "sbomb:build:config", "SentinelConfig") },
		"sbomwriter.RunMetadata.Generator":   func(r *rendered) error { return r.prop("document", "sbomb:build:generator", "Sentinel Generator") },
		"sbomwriter.RunMetadata.Adapters": func(r *rendered) error {
			return all(r.prop("document", "sbomb:run:adapters", "sentinel-adapter-a"), r.prop("document", "sbomb:run:adapters", "sentinel-adapter-b"))
		},

		"sbomwriter.Options.SpecVersion": func(r *rendered) error {
			return all(r.present(`"specVersion": "3.0.1"`), r.prop("document", "sbomb:run:specVersion", "3.0.1"))
		},
		// Refused, never dropped: SPDX has no field for a distribution
		// constraint on the document.
		"sbomwriter.Options.TLP": func(r *rendered) error {
			options := sbomwritertest.AllFieldsOptions()
			options.TLP = "AMBER"
			if _, err := renderSynthetic(t, sbomwritertest.AllFields(), options); err == nil || !strings.Contains(err.Error(), "output.tlp") {
				return fmt.Errorf("a TLP was not refused: %v", err)
			}
			return nil
		},
		"sbomwriter.Options.LicenseText": func(r *rendered) error {
			return all(r.has(kitchenLic, "externalRef", licenceTextLocator), r.absent(`"software_attributionText"`))
		},
		"sbomwriter.Options.Reproducible": func(r *rendered) error {
			again, err := renderSynthetic(t, sbomwritertest.AllFields(), sbomwritertest.AllFieldsOptions())
			if err != nil || again.raw != r.raw {
				return fmt.Errorf("a reproducible document came out twice differently (%v)", err)
			}
			options := sbomwritertest.AllFieldsOptions()
			options.Reproducible = false
			random, err := renderSynthetic(t, sbomwritertest.AllFields(), options)
			if err != nil || random.raw == r.raw {
				return fmt.Errorf("a document that is not reproducible has the derived identity (%v)", err)
			}
			return nil
		},
		// Ignored: an SPDX document's identity is the namespace of every
		// element IRI, and a document without one is not a document.
		"sbomwriter.Options.OmitIdentity": func(r *rendered) error {
			options := sbomwritertest.AllFieldsOptions()
			options.OmitIdentity = true
			again, err := renderSynthetic(t, sbomwritertest.AllFields(), options)
			if err != nil || again.raw != r.raw {
				return fmt.Errorf("OmitIdentity changed the document (%v)", err)
			}
			return nil
		},

		"sbomwriter.Relation.From": func(r *rendered) error { return ignore(r.rel(app, "hasStaticLink", kitchen)) },
		"sbomwriter.Relation.To":   func(r *rendered) error { return ignore(r.rel(kitchen, "contains", kitchenH)) },

		"domain.Component.ID": func(r *rendered) error {
			// The identity is what relations name the component by; the
			// local identity is derived from the name (section 28.4).
			return ignore(r.rel(kitchen, "contains", kitchenC))
		},
		// Never set in a Document; the writer derives identities itself.
		"domain.Component.BomRef":        func(r *rendered) error { return r.absent("sentinel-bomref-never-read") },
		"domain.Component.Name":          func(r *rendered) error { return r.has(kitchen, "name", "kitchen") },
		"domain.Component.Version":       func(r *rendered) error { return r.has(kitchen, "software_packageVersion", "1.2.3-sentinel") },
		"domain.Component.VersionSource": func(r *rendered) error { return r.prop(kitchen, "sbomb:version:source", "conan") },
		"domain.Component.VersionConf":   func(r *rendered) error { return r.prop(kitchen, "sbomb:version:confidence", "medium") },
		"domain.Component.Type": func(r *rendered) error {
			// A type the purpose vocabulary has no word for keeps its exact
			// spelling in a property.
			return all(r.has(kitchen, "software_primaryPurpose", "library"), r.has("component:libc", "software_primaryPurpose", "operatingSystem"),
				r.has("component:type-crypto", "software_primaryPurpose", "other"),
				r.prop("component:type-crypto", "sbomb:component:type", "cryptographic-asset"))
		},
		"domain.Component.PURL": func(r *rendered) error {
			return all(r.has(kitchen, "software_packageUrl", "pkg:generic/kitchen@1.2.3-sentinel"),
				r.has(kitchen, "externalIdentifier", "pkg:generic/kitchen@1.2.3-sentinel"))
		},
		"domain.Component.CPE": func(r *rendered) error {
			return r.has(kitchen, "externalIdentifier", "cpe:2.3:a:sentinel:kitchen:1.2.3:*:*:*:*:*:*:*")
		},
		"domain.Component.Supplier": func(r *rendered) error {
			return all(r.has(kitchen, "suppliedBy", "agent:Sentinel Supplier Org"), r.has("agent:Sentinel Supplier Org", "type", "Organization"))
		},
		"domain.Component.Originator": func(r *rendered) error {
			return all(r.has(kitchen, "originatedBy", "agent:Sentinel Originator Org"),
				r.prop(kitchen, "sbomb:component:originator", "Sentinel Originator Org"))
		},
		"domain.Component.Description": func(r *rendered) error { return r.has(kitchen, "description", "sentinel description of kitchen") },
		"domain.Component.CVEExclusions": func(r *rendered) error {
			return all(ignore(r.rel("vulnerability:CVE-2024-0001", "doesNotAffect", kitchen)),
				r.prop(kitchen, "sbomb:component:cveExclusion", "CVE-2024-0001: sentinel reason one"))
		},
		"domain.Component.Root":  func(r *rendered) error { return r.prop(kitchen, "sbomb:component:root", "build:_deps/kitchen-src") },
		"domain.Component.Scope": func(r *rendered) error { return r.prop(kitchen, "sbomb:component:scope", "third-party") },
		"domain.Component.Licenses": func(r *rendered) error {
			return ignore(r.rel(kitchen, "hasConcludedLicense", concludedMIT))
		},
		"domain.Component.LicenseEvidence": func(r *rendered) error {
			return ignore(r.rel(kitchen, "hasDeclaredLicense", "license:MIT"))
		},
		"domain.Component.DetectedBy": func(r *rendered) error { return r.prop(kitchen, "sbomb:component:detectedBy", "sentinel-detector") },
		"domain.Component.VCS":        func(r *rendered) error { return r.has(kitchen, "externalRef", "vcs") },
		"domain.Component.EnvironmentProvided": func(r *rendered) error {
			return ignore(r.rel(app, "hasProvidedDependency", "component:libc"))
		},
		"domain.Component.DistributionRole": func(r *rendered) error {
			tool, err := r.rel(app, "usesTool", "component:gen")
			if err != nil {
				return err
			}
			if tool["scope"] != "build" {
				return fmt.Errorf("a build-time-only component is not a build-scoped tool: %v", tool)
			}
			return r.prop(kitchen, "sbomb:component:distributionRole", "distributed")
		},
		"domain.Component.LinkageForms": func(r *rendered) error {
			return all(ignore(r.rel(app, "hasStaticLink", kitchen)), r.prop(kitchen, "sbomb:component:linkageForm", "static-archive-member"))
		},
		"domain.Component.ArchiveMembersUsed": func(r *rendered) error { return r.prop(kitchen, "sbomb:component:archiveMembersUsed", "3/7") },
		"domain.Component.HeaderOnly": func(r *rendered) error {
			return all(r.has("component:hdr", "software_additionalPurpose", "source"), r.prop("component:hdr", "sbomb:component:headerOnly", "true"))
		},
		"domain.Component.Modification": func(r *rendered) error { return r.has(kitchen, "software_sourceInfo", "sentinel signal prose") },
		"domain.Component.LicenseArtifacts": func(r *rendered) error {
			return ignore(r.rel(kitchen, "hasEvidence", kitchenLic))
		},
		"domain.Component.Copyrights": func(r *rendered) error {
			// A statement of no file is a copyright notice of the package,
			// after the curated one.
			return all(r.has(kitchenH, "software_copyrightText", "Copyright sentinel holder A"),
				r.has(kitchen, "software_copyrightText", kitchenCopyright))
		},
		"domain.Component.SourceObligations": func(r *rendered) error {
			return r.prop(kitchen, "sbomb:component:sourceObligation", "sentinel-obligation")
		},
		"domain.Component.Copyright": func(r *rendered) error {
			return r.has(kitchen, "software_copyrightText", kitchenCopyright)
		},
		"domain.Component.Properties": func(r *rendered) error {
			return r.prop(kitchen, "sbomb:component:declaredRevision", "sentinel-declared-revision")
		},
		// Never set in a Document; the files of a component are its relations.
		"domain.Component.Files": func(r *rendered) error { return r.absent("sentinel-files-never-read") },

		"domain.UsedFile.ID": func(r *rendered) error { return r.has(kitchenC, "name", "build:_deps/kitchen-src/kitchen.c") },
		"domain.UsedFile.Class": func(r *rendered) error {
			return all(r.has(kitchenC, "software_primaryPurpose", "source"), r.prop(kitchenC, "sbomb:file:class", "source"))
		},
		// Never set; the header class travels as sbomb:evidence:header:class.
		"domain.UsedFile.HeaderClass": func(r *rendered) error {
			return all(r.absent("sentinel-header-class-never-read"), r.prop(kitchenH, "sbomb:evidence:header:class", "third-party"))
		},
		"domain.UsedFile.Hashes": func(r *rendered) error {
			return all(r.has(kitchenC, "verifiedUsing", sbomwritertest.HashKitchenSource), r.has(kitchenC, "verifiedUsing", sbomwritertest.HashKitchenSHA1))
		},
		"domain.UsedFile.SizeBytes": func(r *rendered) error { return r.prop(kitchenC, "sbomb:file:size", "4242") },
		"domain.UsedFile.Missing":   func(r *rendered) error { return r.prop(missingFile, "sbomb:file:missing", "true") },
		// Not rendered: which component owns a file is the contains
		// relationship, derived from the relations.
		"domain.UsedFile.ComponentID": func(r *rendered) error { return r.absent("sentinel-component-id-never-rendered") },
		"domain.UsedFile.Properties": func(r *rendered) error {
			return all(r.prop(kitchenC, "sbomb:file:linkageForm", "static-archive-member"), r.absent(`"finding"`))
		},

		"domain.FileID.Anchor": func(r *rendered) error {
			return r.prop(kitchenC, "sbomb:path:canonical", "build:_deps/kitchen-src/kitchen.c")
		},
		"domain.FileID.RelPath": func(r *rendered) error { return r.has(kitchenC, "name", "build:_deps/kitchen-src/kitchen.c") },

		"domain.LicenseFinding.Expression": func(r *rendered) error {
			return r.has(concludedMIT, "simplelicensing_licenseExpression", "MIT OR Apache-2.0")
		},
		"domain.LicenseFinding.SPDXID": func(r *rendered) error {
			return ignore(r.rel("component:type-file", "hasConcludedLicense", "license:Zlib"))
		},
		"domain.LicenseFinding.Name": func(r *rendered) error {
			return r.has("license-text:LicenseRef-sbomb-Sentinel-Custom-Licence-2ce69ab6-d9c0bbf6", "name", "Sentinel Custom Licence")
		},
		"domain.LicenseFinding.Evidence": func(r *rendered) error {
			return r.relProp(kitchen, "hasConcludedLicense", concludedMIT, "sbomb:license:evidenceClass", "file-level")
		},
		"domain.LicenseFinding.Confidence": func(r *rendered) error {
			return r.relProp(kitchen, "hasConcludedLicense", concludedMIT, "sbomb:license:confidence", "high")
		},
		"domain.LicenseFinding.Source": func(r *rendered) error {
			return r.relProp(kitchen, "hasConcludedLicense", concludedMIT, "sbomb:license:source", "build:_deps/kitchen-src/kitchen.h")
		},
		"domain.LicenseFinding.Reason": func(r *rendered) error {
			return r.relProp(kitchen, "hasConcludedLicense", concludedMIT, "sbomb:license:reason", "sentinel-reason-code")
		},
		"domain.LicenseFinding.Conflicts": func(r *rendered) error {
			return r.relProp(kitchen, "hasConcludedLicense", concludedMIT, "sbomb:license:conflictingValue", "sentinel-conflicting-value")
		},
		"domain.LicenseFinding.Technique": func(r *rendered) error {
			return r.relProp(kitchen, "hasConcludedLicense", concludedMIT, "sbomb:license:technique", "spdx-identifier")
		},

		"domain.LicenseArtifact.Kind": func(r *rendered) error {
			// A grant's text is written; a notice's never is.
			return all(r.has(kitchenLic, "externalRef", licenceTextLocator),
				r.absent("sentinel notice bytes are never rendered"))
		},
		"domain.LicenseArtifact.File":   func(r *rendered) error { return r.has(kitchenNote, "software_primaryPurpose", "documentation") },
		"domain.LicenseArtifact.SHA256": func(r *rendered) error { return r.has(kitchenNote, "verifiedUsing", sbomwritertest.HashKitchenNotice) },
		"domain.LicenseArtifact.Bytes": func(r *rendered) error {
			return all(r.has(kitchenLic, "externalRef", licenceTextLocator),
				r.present("data:text/plain;base64,//5zZW50aW5lbA=="))
		},
		"domain.LicenseArtifact.DetectedID": func(r *rendered) error { return ignore(r.rel(kitchenLic, "hasDeclaredLicense", "license:MIT")) },
		"domain.LicenseArtifact.Technique": func(r *rendered) error {
			return r.relProp(kitchenLic, "hasDeclaredLicense", "license:MIT", "sbomb:license:technique", "spdx-identifier")
		},

		"domain.ModificationRecord.Status": func(r *rendered) error { return r.prop(kitchen, "sbomb:component:modified", "true") },
		"domain.ModificationRecord.Signal": func(r *rendered) error { return r.has(kitchen, "software_sourceInfo", "sentinel signal prose") },
		// Not rendered, as in CycloneDX: it is the remediation of the finding
		// an unknown status raises, not a fact about the component.
		"domain.ModificationRecord.Remediation": func(r *rendered) error { return r.absent("sentinel remediation never rendered") },
		"domain.ModificationRecord.Commit":      func(r *rendered) error { return r.prop(kitchen, "sbomb:component:vcsCommit", "sentinelcommit222") },
		"domain.ModificationRecord.Patches":     func(r *rendered) error { return ignore(r.rel(kitchen, "patchedBy", firstPatch)) },

		"domain.Patch.File":        func(r *rendered) error { return r.has(firstPatch, "name", "0002-sentinel.patch") },
		"domain.Patch.Type":        func(r *rendered) error { return r.prop(firstPatch, "sbomb:patch:type", "backport") },
		"domain.Patch.Description": func(r *rendered) error { return r.has(firstPatch, "description", "sentinel patch description B") },
		"domain.Patch.Source":      func(r *rendered) error { return r.prop(firstPatch, "sbomb:patch:source", "conandata.yml") },

		"domain.VCSRecord.URL":    func(r *rendered) error { return r.has(kitchen, "externalRef", "https://sentinel.example/kitchen.git") },
		"domain.VCSRecord.Commit": func(r *rendered) error { return r.prop(kitchen, "sbomb:component:vcsCommit", "sentinelcommit111") },
		"domain.VCSRecord.Dirty":  func(r *rendered) error { return r.prop(kitchen, "sbomb:component:vcsDirty", "true") },

		"domain.CVEExclusion.CVE": func(r *rendered) error {
			return all(r.has("vulnerability:CVE-2024-0001", "name", "CVE-2024-0001"), r.has("vulnerability:CVE-2024-0001", "externalIdentifier", "cve"))
		},
		"domain.CVEExclusion.Reason": func(r *rendered) error {
			statement, err := r.rel("vulnerability:CVE-2024-0001", "doesNotAffect", kitchen)
			if err != nil {
				return err
			}
			for _, node := range r.graph {
				if node["security_impactStatement"] == "sentinel reason two" {
					return nil
				}
			}
			return fmt.Errorf("the second reason is not a statement of its own: %v", statement)
		},

		"domain.CopyrightStatement.Text": func(r *rendered) error { return r.present("Copyright sentinel holder B") },
		"domain.CopyrightStatement.File": func(r *rendered) error {
			// Two components read a statement from NOTICE: both are on the
			// file, one per line, in canonical order.
			return r.has(kitchenNote, "software_copyrightText", "Copyright sentinel holder B\nCopyright sentinel holder D")
		},
	}
	// The creation information has no spdxId, so the two rows that follow it
	// read it directly: it is the first node of the graph.
	rows["sbomwriter.RunMetadata.ToolName"] = func(r *rendered) error {
		info := r.graph[0]
		using, _ := info["createdUsing"].([]any)
		if len(using) != 1 || local(using[0].(string)) != "tool:sbomb" {
			return fmt.Errorf("createdUsing = %v", info["createdUsing"])
		}
		return r.has("tool:sbomb", "name", "sbomb")
	}
	rows["sbomwriter.RunMetadata.ToolVendor"] = func(r *rendered) error {
		by, _ := r.graph[0]["createdBy"].([]any)
		if len(by) != 1 || local(by[0].(string)) != "agent:Sentinel Vendor Org" {
			return fmt.Errorf("createdBy = %v", r.graph[0]["createdBy"])
		}
		return r.has("agent:Sentinel Vendor Org", "type", "Organization")
	}

	types := []reflect.Type{
		reflect.TypeOf(sbomwriter.Document{}), reflect.TypeOf(sbomwriter.RunMetadata{}), reflect.TypeOf(sbomwriter.Options{}),
		reflect.TypeOf(sbomwriter.Relation{}), reflect.TypeOf(domain.Component{}), reflect.TypeOf(domain.UsedFile{}),
		reflect.TypeOf(domain.FileID{}), reflect.TypeOf(domain.LicenseFinding{}), reflect.TypeOf(domain.LicenseArtifact{}),
		reflect.TypeOf(domain.ModificationRecord{}), reflect.TypeOf(domain.Patch{}), reflect.TypeOf(domain.VCSRecord{}),
		reflect.TypeOf(domain.CVEExclusion{}), reflect.TypeOf(domain.CopyrightStatement{}),
	}
	covered := map[string]bool{}
	for _, typ := range types {
		for index := 0; index < typ.NumField(); index++ {
			key := typ.String() + "." + typ.Field(index).Name
			covered[key] = true
			row, ok := rows[key]
			if !ok {
				t.Errorf("%s has no row: say where the SPDX document carries it, or why it does not", key)
				continue
			}
			if err := row(doc); err != nil {
				t.Errorf("%s: %v", key, err)
			}
		}
	}
	for key := range rows {
		if !covered[key] {
			t.Errorf("row %s names no field", key)
		}
	}
}
