// Package sbomwritertest builds the synthetic documents writers are tested
// against. It is an ordinary package rather than a _test file because the
// tests of more than one package use it -- the mapping tests of each format
// and the golden test of the command line -- and a test file cannot be
// imported.
package sbomwritertest

import (
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

// Hash values of the synthetic files: well-formed hex of the right length, so
// that a writer that checks digest lengths accepts them.
const (
	HashKitchenSource   = "1111111111111111111111111111111111111111111111111111111111111111"
	HashKitchenSHA1     = "2222222222222222222222222222222222222222"
	HashKitchenLicense  = "3333333333333333333333333333333333333333333333333333333333333333"
	HashKitchenNotice   = "4444444444444444444444444444444444444444444444444444444444444444"
	HashKitchenBinary   = "5555555555555555555555555555555555555555555555555555555555555555"
	HashKitchenRetained = "6666666666666666666666666666666666666666666666666666666666666666"
	HashUnknownKey      = "SENTINEL-UNKNOWN-KEY"
	HashUnknownValue    = "abcdef0123"
)

// sentinelFindingID is the identifier of the one finding of the document. It is
// a constant rather than a literal in the composite below, because
// tools/findingsdoc counts a literal ID as a finding the code emits.
const sentinelFindingID = "SENTINEL_FINDING_NEVER_RENDERED"

// NonUTF8LicenseText is a retained licence text that is not UTF-8, which no
// JSON string can carry unaltered.
var NonUTF8LicenseText = []byte{0xff, 0xfe, 's', 'e', 'n', 't', 'i', 'n', 'e', 'l'}

// AllFields is one document in which every field of every type a Document
// reaches is set, each to a value found nowhere else in it, so that a test can
// follow each value into the rendered document -- or assert that a field which
// is not rendered left no trace. It is an assembly of two artifacts, so that
// the per-artifact linkage forms are exercised, and it carries a
// build-environment grouping with a provided system library, a build tool, a
// header-only component, an embedded asset, a generated source, a missing
// file, a digest of an algorithm no format names, a retained licence text that
// is not UTF-8, patches (one the metadata names no file for), upstream CVE
// exclusions and an advisory that is no CVE, a licence only known by name,
// and every component type and file class.
//
// Every call returns a fresh document; a test may shuffle or modify it.
func AllFields() *sbomwriter.Document {
	kitchenRoot := domain.FileID{Anchor: "build", RelPath: "_deps/kitchen-src"}
	kitchenFile := func(name string) domain.FileID {
		return domain.FileID{Anchor: "build", RelPath: "_deps/kitchen-src/" + name}
	}
	appArtifact := "build:bin/app"
	imageArtifact := "build:fs.img"
	app := []string{"artifact:" + appArtifact}
	image := []string{"artifact:" + imageArtifact}
	both := []string{"artifact:" + appArtifact, "artifact:" + imageArtifact}

	usedFile := func(id domain.FileID, class domain.FileClass, scope string, artifacts []string, form string) domain.UsedFile {
		properties := map[string][]string{
			"sbomb:component:scope":       {scope},
			"sbomb:file:distributionRole": {domain.RoleDistributed},
			// An internal marker of discovery, which no writer may emit.
			"finding": {"MISSING_FILE_HASH"},
		}
		if len(artifacts) > 0 {
			properties["sbomb:evidence:artifacts"] = append([]string(nil), artifacts...)
		}
		if form != "" {
			properties["sbomb:file:linkageForm"] = []string{form}
		}
		return domain.UsedFile{ID: id, Class: class, Properties: properties}
	}

	kitchenSource := usedFile(kitchenFile("kitchen.c"), domain.FileClassSource, "third-party", app, domain.LinkageStaticArchiveMember)
	kitchenSource.Hashes = map[string]string{"SHA-256": HashKitchenSource, "SHA-1": HashKitchenSHA1}
	kitchenSource.SizeBytes = 4242
	kitchenSource.ComponentID = "sentinel-component-id-never-rendered"
	kitchenHeader := usedFile(kitchenFile("kitchen.h"), domain.FileClassHeader, "third-party", app, domain.LinkageStaticArchiveMember)
	kitchenHeader.HeaderClass = "sentinel-header-class-never-read"
	kitchenHeader.Properties["sbomb:evidence:header:class"] = []string{"third-party"}
	kitchenConfig := usedFile(domain.FileID{Anchor: "build", RelPath: "gen/kitchen_config.h"}, domain.FileClassGeneratedHeader, "third-party", app, domain.LinkageGeneratedSource)
	kitchenTable := usedFile(domain.FileID{Anchor: "build", RelPath: "gen/table.c"}, domain.FileClassGeneratedSource, "third-party", app, domain.LinkageGeneratedSource)
	kitchenObject := usedFile(domain.FileID{Anchor: "build", RelPath: "obj/kitchen.o"}, domain.FileClassObject, "third-party", app, domain.LinkageStaticArchiveMember)
	kitchenArchive := usedFile(domain.FileID{Anchor: "build", RelPath: "lib/libkitchen.a"}, domain.FileClassArchive, "third-party", app, domain.LinkageStaticArchiveMember)
	kitchenArchive.Hashes = map[string]string{HashUnknownKey: HashUnknownValue}
	// The licence file is compiled into the image as an asset: a used file
	// and a retained licence file at once.
	kitchenLicense := usedFile(kitchenFile("LICENSE"), domain.FileClassAsset, "third-party", image, domain.LinkageEmbeddedAsset)
	kitchenLicense.Hashes = map[string]string{"SHA-256": HashKitchenLicense}

	headerOnly := usedFile(domain.FileID{Anchor: "project", RelPath: "hdr/hdr.h"}, domain.FileClassHeader, "third-party", app, domain.LinkageHeaderOnly)
	headerOnly.Missing = true
	generator := usedFile(domain.FileID{Anchor: "project", RelPath: "tools/gen.py"}, domain.FileClassUnknown, "third-party", both, domain.LinkageBuildTool)
	logo := usedFile(domain.FileID{Anchor: "project", RelPath: "assets/logo png.png"}, domain.FileClassAsset, "project", image, domain.LinkageEmbeddedAsset)
	libc := usedFile(domain.FileID{Anchor: "sysroot", RelPath: "usr/lib/libc.so.6"}, domain.FileClassSharedLibrary, "system", app, domain.LinkageDynamic)
	stdio := usedFile(domain.FileID{Anchor: "sysroot", RelPath: "usr/include/stdio.h"}, domain.FileClassHeader, "toolchain", nil, "")

	typed := func(id, name, componentType string) domain.Component {
		return domain.Component{
			ID: id, Name: name, Type: componentType, Scope: "project",
			LinkageForms: []string{domain.LinkageStaticObject},
			Licenses:     []domain.LicenseFinding{{SPDXID: "Zlib", Evidence: "component-level"}},
		}
	}

	kitchen := domain.Component{
		ID:            "component:kitchen",
		BomRef:        "sentinel-bomref-never-read",
		Name:          "kitchen",
		Version:       "1.2.3-sentinel",
		VersionSource: "conan",
		VersionConf:   domain.ConfidenceMedium,
		Type:          "library",
		PURL:          "pkg:generic/kitchen@1.2.3-sentinel",
		CPE:           "cpe:2.3:a:sentinel:kitchen:1.2.3:*:*:*:*:*:*:*",
		Supplier:      "Sentinel Supplier Org",
		Originator:    "Sentinel Originator Org",
		Description:   "sentinel description of kitchen",
		CVEExclusions: []domain.CVEExclusion{
			{CVE: "CVE-2024-0001", Reason: "sentinel reason one"},
			{CVE: "CVE-2024-0001", Reason: "sentinel reason two"},
			{CVE: "CVE-2024-0002"},
			{CVE: "CVE-2024-0002"},
			// An advisory that is no CVE: sbom.yml accepts any identifier.
			{CVE: "GHSA-sent-inel-0001", Reason: "sentinel reason of an advisory"},
		},
		Root:  &kitchenRoot,
		Scope: "third-party",
		Licenses: []domain.LicenseFinding{{
			Expression: "MIT OR Apache-2.0",
			SPDXID:     "MIT",
			Name:       "sentinel finding name, unused beside a valid expression",
			Evidence:   "file-level",
			Confidence: domain.ConfidenceHigh,
			Source:     "build:_deps/kitchen-src/kitchen.h",
			Reason:     "sentinel-reason-code",
			Conflicts:  []string{"sentinel-conflicting-value"},
			Technique:  "spdx-identifier",
		}},
		LicenseEvidence: []domain.LicenseFinding{
			{SPDXID: "MIT", Name: "MIT", Source: "build:_deps/kitchen-src/LICENSE", Technique: "spdx-digest", Confidence: domain.ConfidenceHigh},
			{Name: "Sentinel Custom Licence", Source: "build:_deps/kitchen-src/COPYING.custom", Technique: "spdx-template", Confidence: domain.ConfidenceLow},
		},
		DetectedBy:         "sentinel-detector",
		VCS:                &domain.VCSRecord{URL: "https://sentinel.example/kitchen.git", Commit: "sentinelcommit111", Dirty: true},
		DistributionRole:   domain.RoleDistributed,
		LinkageForms:       []string{domain.LinkageEmbeddedAsset, domain.LinkageGeneratedSource, domain.LinkageStaticArchiveMember},
		ArchiveMembersUsed: "3/7",
		Modification: domain.ModificationRecord{
			Status:      domain.ModificationModified,
			Signal:      "sentinel signal prose",
			Remediation: "sentinel remediation never rendered",
			Commit:      "sentinelcommit222",
			Patches: []domain.Patch{
				{File: "0002-sentinel.patch", Type: domain.PatchBackport, Description: "sentinel patch description B", Source: "conandata.yml"},
				{File: "0001-sentinel.patch", Type: domain.PatchUnofficial, Description: "sentinel patch description A", Source: "conandata.yml"},
				{File: "0001-sentinel.patch", Type: domain.PatchUnofficial, Description: "sentinel patch description A2", Source: "conandata.yml"},
				{File: "0001-sentinel.patch", Type: domain.PatchUnofficial, Description: "sentinel patch description A", Source: "conandata.yml"},
				// A base64 payload patch: the metadata names no patch file.
				{Type: domain.PatchMonkey, Description: "sentinel patch description C, of no file", Source: "conandata.yml"},
			},
		},
		LicenseArtifacts: []domain.LicenseArtifact{
			{Kind: domain.LicenseArtifactLicense, File: kitchenFile("LICENSE"), SHA256: HashKitchenLicense,
				Bytes: []byte("sentinel licence text of MIT\n"), DetectedID: "MIT", Technique: "spdx-identifier"},
			{Kind: domain.LicenseArtifactNotice, File: kitchenFile("NOTICE"), SHA256: HashKitchenNotice,
				Bytes: []byte("sentinel notice bytes are never rendered\n")},
			{Kind: domain.LicenseArtifactLicense, File: kitchenFile("COPYING.custom"), SHA256: HashKitchenBinary,
				Bytes: NonUTF8LicenseText, DetectedID: "Sentinel Custom Licence", Technique: "spdx-template"},
			{Kind: domain.LicenseArtifactLicense, File: kitchenFile("LICENSE.retained"), SHA256: HashKitchenRetained,
				Bytes: []byte("sentinel retained custom licence text\n"), DetectedID: "Sentinel Retained Licence", Technique: "spdx-template"},
		},
		Copyrights: []domain.CopyrightStatement{
			{Text: "Copyright sentinel holder A", File: kitchenFile("kitchen.h")},
			{Text: "Copyright sentinel holder B", File: kitchenFile("NOTICE")},
			{Text: "Copyright sentinel holder C, of no file"},
		},
		SourceObligations: []string{"sentinel-obligation"},
		Copyright:         "Copyright sentinel curated notice",
		Properties: map[string][]string{
			"sbomb:component:detectedBy":       {"sentinel-detector"},
			"sbomb:component:declaredRevision": {"sentinel-declared-revision"},
		},
		Files: []domain.FileID{{Anchor: "sentinel-files-never-read", RelPath: "x"}},
	}

	components := []domain.Component{
		kitchen,
		{
			// The same name and version again: section 28.4 escalates.
			ID: "component:kitchen-again", Name: "kitchen", Version: "1.2.3-sentinel", Type: "library", Scope: "third-party",
			// A second statement read from the NOTICE kitchen's own statement
			// sits on, so that one file carries two statements and a renderer
			// that keeps only one, or joins them some other way, is caught.
			Copyrights: []domain.CopyrightStatement{{Text: "Copyright sentinel holder D", File: kitchenFile("NOTICE")}},
			// What an SPDX-License-Identifier line with lower-case operators
			// and an identifier the licence list does not carry resolves to:
			// the pipeline sets SPDXID to the first identifier, which is not
			// the licence.
			Licenses: []domain.LicenseFinding{{
				Expression: "MIT or Sentinel-Header-Licence-2.0", SPDXID: "MIT", Name: "MIT or Sentinel-Header-Licence-2.0",
				Evidence: "file-level", Confidence: domain.ConfidenceHigh, Source: "build:_deps/kitchen-src/kitchen.h", Technique: "spdx-identifier",
			}},
		},
		{
			ID: "component:hdr", Name: "hdr", Scope: "third-party", HeaderOnly: true,
			LinkageForms: []string{domain.LinkageHeaderOnly}, DistributionRole: domain.RoleDistributed,
			Licenses: []domain.LicenseFinding{{Name: "NOASSERTION", Evidence: "unknown", Reason: "no-evidence"}},
		},
		{
			ID: "component:gen", Name: "gen", Type: "application", Scope: "third-party",
			LinkageForms: []string{domain.LinkageBuildTool}, DistributionRole: domain.RoleBuildTimeOnly,
			Licenses: []domain.LicenseFinding{{Expression: "NONE", Evidence: "curated", Source: "curated"}},
		},
		{
			ID: "component:assets", Name: "assets", Type: "data", Scope: "project",
			LinkageForms: []string{domain.LinkageEmbeddedAsset}, DistributionRole: domain.RoleDistributed,
			Licenses: []domain.LicenseFinding{{Expression: "MIT AND Sentinel-Unlisted-1.0", Evidence: "file-level"}},
		},
		{
			ID: "component:libc", Name: "libc", Type: "operating-system", Scope: "system",
			EnvironmentProvided: true, LinkageForms: []string{domain.LinkageDynamic}, DistributionRole: domain.RoleDistributed,
			Licenses: []domain.LicenseFinding{{Expression: "LGPL-2.1-or-later WITH GCC-exception-3.1", Evidence: "component-level"}},
		},
		{ID: "component:toolchain-headers", Name: "toolchain headers", Type: "platform", Scope: "toolchain"},
		typed("component:type-file", "type-file", "file"),
		typed("component:type-device", "type-device", "device"),
		typed("component:type-container", "type-container", "container"),
		typed("component:type-driver", "type-driver", "device-driver"),
		typed("component:type-model", "type-model", "machine-learning-model"),
		typed("component:type-crypto", "type-crypto", "cryptographic-asset"),
		typed("component:type-framework", "type-framework", "framework"),
		typed("component:type-firmware", "type-firmware", "firmware"),
		typed("component:type-untyped", "type-untyped", ""),
		{
			ID: "component:build-environment", Name: "build-environment", Type: "framework", Scope: "toolchain",
			Properties: map[string][]string{"sbomb:component:detectedBy": {"synthetic"}},
		},
	}

	files := []domain.UsedFile{
		kitchenSource, kitchenHeader, kitchenConfig, kitchenTable, kitchenObject, kitchenArchive, kitchenLicense,
		headerOnly, generator, logo, libc, stdio,
	}

	canonical := func(file domain.UsedFile) string { return file.ID.Canonical() }
	relations := []sbomwriter.Relation{
		{From: "component:kitchen", To: []string{canonical(kitchenSource), canonical(kitchenHeader), canonical(kitchenConfig),
			canonical(kitchenTable), canonical(kitchenObject), canonical(kitchenArchive), canonical(kitchenLicense)}},
		{From: "component:kitchen-again", To: nil},
		{From: "component:hdr", To: []string{canonical(headerOnly)}},
		{From: "component:gen", To: []string{canonical(generator)}},
		{From: "component:assets", To: []string{canonical(logo)}},
		{From: "component:libc", To: []string{canonical(libc)}},
		{From: "component:toolchain-headers", To: []string{canonical(stdio)}},
		{From: "component:build-environment", To: []string{"component:libc", "component:toolchain-headers"}},
		{From: appArtifact, To: []string{"component:gen", "component:hdr", "component:kitchen"}},
		{From: imageArtifact, To: []string{"component:assets", "component:gen", "component:kitchen"}},
		{From: "product", To: []string{appArtifact, imageArtifact}},
		{From: "product", To: []string{
			"component:build-environment", "component:kitchen-again",
			"component:type-container", "component:type-crypto", "component:type-device", "component:type-driver",
			"component:type-file", "component:type-firmware", "component:type-framework", "component:type-model",
			"component:type-untyped",
			// A target nothing in the document is: skipped, as every writer
			// skips it.
			"component:nowhere",
		}},
	}

	return &sbomwriter.Document{
		Product: domain.Component{
			ID: "product", Name: "sentinel-product", Version: "9.9.9-sentinel", VersionSource: "curated",
			VersionConf: domain.ConfidenceHigh, Type: "application", Supplier: "Sentinel Vendor Org",
			Licenses:   []domain.LicenseFinding{{Expression: "Apache-2.0", Evidence: "curated"}},
			Properties: map[string][]string{"sbomb:artifact:role": {"application"}},
		},
		Artifacts: []domain.Component{
			{ID: appArtifact, Name: "app", Type: "application"},
			{ID: imageArtifact, Name: "fs.img", Type: "firmware"},
		},
		Components: components,
		Files:      files,
		Relations:  relations,
		Findings: []domain.Finding{{
			ID: sentinelFindingID, Severity: domain.SeverityInfo,
			Subject: domain.Subject{Kind: "component", Ref: "component:kitchen"},
			Message: "sentinel finding message never rendered",
		}},
		Run: sbomwriter.RunMetadata{
			ToolName:      "sbomb",
			ToolVendor:    "Sentinel Vendor Org",
			ToolVersion:   "0.0.0-sentinel",
			Timestamp:     "2023-11-14T22:13:20Z",
			Reproducible:  true,
			PolicyProfile: "sentinel-policy-profile",
			BuildConfig:   "SentinelConfig",
			Generator:     "Sentinel Generator",
			Adapters:      []string{"sentinel-adapter-b", "sentinel-adapter-a"},
		},
	}
}

// AllFieldsOptions are the serialization choices AllFields is rendered with:
// reproducible, with licence texts, at the writer's default version.
func AllFieldsOptions() sbomwriter.Options {
	return sbomwriter.Options{
		LicenseText:  sbomwriter.LicenseTextEvidence,
		Reproducible: true,
	}
}
