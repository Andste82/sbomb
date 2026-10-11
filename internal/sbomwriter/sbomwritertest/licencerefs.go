package sbomwritertest

import (
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

// Licence expressions of LicenceReferences: references the build stated
// itself, which a version may be unable to name as they stand.
const (
	// LicenceStatedRefs is a choice between a LicenseRef the build stated and
	// one qualified by another document.
	LicenceStatedRefs = "LicenseRef-acme OR DocumentRef-other:LicenseRef-beta"
	// LicenceUnlistedException is a listed licence with an exception the
	// SPDX list does not carry.
	LicenceUnlistedException = "GPL-2.0-only WITH Acme-exception"
)

// LicenceReferences is a build of two third-party components whose licences
// name references the build stated: a LicenseRef, a LicenseRef of another
// document, and an exception off the SPDX list. A version that cannot name
// one of them as it stands (SPDX 2.3 has no AdditionRef and resolves a
// LicenseRef only inside the document) must still write a valid document.
//
// Every call returns a fresh document.
func LicenceReferences() *sbomwriter.Document {
	return &sbomwriter.Document{
		Product: domain.Component{
			ID: "product", Name: "licence-refs", Version: "1.0.0", VersionSource: "curated", Type: "application",
			Licenses: []domain.LicenseFinding{{Expression: "MIT", Evidence: "curated"}},
		},
		Artifacts: []domain.Component{{ID: "build:bin/app", Name: "app", Type: "application"}},
		Components: []domain.Component{
			{
				ID: "component:acme", Name: "acme", Version: "1.0.0", Type: "library", Scope: "third-party",
				LinkageForms: []string{domain.LinkageStaticArchiveMember}, DistributionRole: domain.RoleDistributed,
				Licenses: []domain.LicenseFinding{{Expression: LicenceStatedRefs, Evidence: "curated", Source: "curated"}},
			},
			{
				ID: "component:excepted", Name: "excepted", Version: "2.0.0", Type: "library", Scope: "third-party",
				LinkageForms: []string{domain.LinkageStaticArchiveMember}, DistributionRole: domain.RoleDistributed,
				Licenses: []domain.LicenseFinding{{Expression: LicenceUnlistedException, Evidence: "curated", Source: "curated"}},
			},
		},
		Relations: []sbomwriter.Relation{
			{From: "build:bin/app", To: []string{"component:acme", "component:excepted"}},
			{From: "product", To: []string{"build:bin/app"}},
		},
		Run: sbomwriter.RunMetadata{
			ToolName: "sbomb", ToolVendor: "Sentinel Vendor Org", ToolVersion: "0.0.0-sentinel",
			Timestamp: "2023-11-14T22:13:20Z", Reproducible: true,
		},
	}
}
