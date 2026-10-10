package mapping

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/sbomwriter"
	"github.com/example/sbomb/internal/sbomwriter/sbomwritertest"
)

func build(t *testing.T, document *sbomwriter.Document, options Options) *Model {
	t.Helper()
	if options.SpecVersion == "" {
		options.SpecVersion = "3.0.1"
	}
	model, err := Build(document, options)
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func allFields(t *testing.T) *Model {
	t.Helper()
	return build(t, sbomwritertest.AllFields(), Options{LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true, CustomAdditions: true})
}

func packageOf(t *testing.T, model *Model, local string) Package {
	t.Helper()
	for _, pkg := range model.Packages {
		if pkg.LocalID == local {
			return pkg
		}
	}
	t.Fatalf("no package %s", local)
	return Package{}
}

func fileOf(t *testing.T, model *Model, local string) File {
	t.Helper()
	for _, file := range model.Files {
		if file.LocalID == local {
			return file
		}
	}
	t.Fatalf("no file %s", local)
	return File{}
}

func edgesFrom(model *Model, from string) map[string][]EdgeKind {
	out := map[string][]EdgeKind{}
	for _, edge := range model.Edges {
		if edge.From == from {
			out[edge.To] = append(out[edge.To], edge.Kind)
		}
	}
	return out
}

func hasProperty(properties []Property, name, value string) bool {
	for _, property := range properties {
		if property.Name == name && property.Value == value {
			return true
		}
	}
	return false
}

// linkDocument is a single-artifact product linking one component of the
// given forms, each of whose files carries those forms too.
func linkDocument(forms []string, environmentProvided bool) *sbomwriter.Document {
	file := domain.UsedFile{ID: domain.FileID{Anchor: "project", RelPath: "c.c"}, Class: domain.FileClassSource}
	return &sbomwriter.Document{
		Product: domain.Component{ID: "product", Name: "app", Type: "application"},
		Components: []domain.Component{{
			ID: "component:c", Name: "c", LinkageForms: forms, EnvironmentProvided: environmentProvided,
		}},
		Files: []domain.UsedFile{file},
		Relations: []sbomwriter.Relation{
			{From: "component:c", To: []string{file.ID.Canonical()}},
			{From: "product", To: []string{"component:c"}},
		},
		Run: sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", Timestamp: "2023-11-14T22:13:20Z"},
	}
}

func TestEachLinkageFormBecomesItsEdge(t *testing.T) {
	cases := map[string][]EdgeKind{
		domain.LinkageStaticArchiveMember: {EdgeStaticLink},
		domain.LinkageStaticObject:        {EdgeStaticLink},
		domain.LinkageDynamic:             {EdgeDynamicLink},
		domain.LinkageBuildTool:           {EdgeTool},
		domain.LinkageHeaderOnly:          {EdgeDependsOn},
		domain.LinkageEmbeddedAsset:       {EdgeEmbeds},
	}
	for form, want := range cases {
		model := build(t, linkDocument([]string{form}, false), Options{})
		if got := edgesFrom(model, "product:app")["component:c"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s -> %v, want %v", form, got, want)
		}
	}
}

func TestAComponentWithTwoFormsGetsTwoEdges(t *testing.T) {
	model := build(t, linkDocument([]string{domain.LinkageDynamic, domain.LinkageStaticObject, domain.LinkageStaticArchiveMember}, false), Options{})
	got := edgesFrom(model, "product:app")["component:c"]
	if !reflect.DeepEqual(got, []EdgeKind{EdgeDynamicLink, EdgeStaticLink}) {
		t.Errorf("edges = %v; two static forms are one static link, and the dynamic link is a second edge", got)
	}
}

func TestAHeaderOnlyComponentIsASourceDependency(t *testing.T) {
	document := linkDocument([]string{domain.LinkageHeaderOnly}, false)
	document.Components[0].HeaderOnly = true
	model := build(t, document, Options{})
	if got := edgesFrom(model, "product:app")["component:c"]; !reflect.DeepEqual(got, []EdgeKind{EdgeDependsOn}) {
		t.Errorf("edges = %v", got)
	}
	if !packageOf(t, model, "component:c").HeaderOnly {
		t.Error("the package does not say it is header-only")
	}
}

func TestAnEnvironmentProvidedLibraryIsAProvidedDynamicDependency(t *testing.T) {
	model := build(t, linkDocument([]string{domain.LinkageDynamic}, true), Options{})
	if got := edgesFrom(model, "product:app")["component:c"]; !reflect.DeepEqual(got, []EdgeKind{EdgeDynamicLink, EdgeProvidedDependency}) {
		t.Errorf("edges = %v", got)
	}
}

func TestABuildTimeOnlyComponentIsATool(t *testing.T) {
	document := linkDocument([]string{domain.LinkageBuildTool}, false)
	document.Components[0].DistributionRole = domain.RoleBuildTimeOnly
	model := build(t, document, Options{})
	if got := edgesFrom(model, "product:app")["component:c"]; !reflect.DeepEqual(got, []EdgeKind{EdgeTool}) {
		t.Errorf("edges = %v", got)
	}
}

// TestGeneratedSourceAddsNoEdge: section 24.5 adds generated-source beside the
// linker form, so it adds nothing to the edges; alone it leaves only the
// statement that the product depends on the component.
func TestGeneratedSourceAddsNoEdge(t *testing.T) {
	model := build(t, linkDocument([]string{domain.LinkageGeneratedSource, domain.LinkageStaticArchiveMember}, false), Options{})
	if got := edgesFrom(model, "product:app")["component:c"]; !reflect.DeepEqual(got, []EdgeKind{EdgeStaticLink}) {
		t.Errorf("edges = %v", got)
	}
	alone := build(t, linkDocument([]string{domain.LinkageGeneratedSource}, false), Options{})
	if got := edgesFrom(alone, "product:app")["component:c"]; !reflect.DeepEqual(got, []EdgeKind{EdgeDependsOn}) {
		t.Errorf("edges = %v", got)
	}
	if !hasProperty(packageOf(t, model, "component:c").Properties, "sbomb:component:linkageForm", domain.LinkageGeneratedSource) {
		t.Error("the generated-ness must stay a property")
	}
}

func TestAssemblyModeTypesEachArtifactEdgeByTheFilesItReached(t *testing.T) {
	model := allFields(t)
	app := edgesFrom(model, "artifact:build:bin/app")
	image := edgesFrom(model, "artifact:build:fs.img")
	if !reflect.DeepEqual(app["component:kitchen"], []EdgeKind{EdgeStaticLink}) {
		t.Errorf("app -> kitchen = %v; its files reached the app as archive members", app["component:kitchen"])
	}
	if !reflect.DeepEqual(image["component:kitchen"], []EdgeKind{EdgeEmbeds}) {
		t.Errorf("image -> kitchen = %v; only its licence file reached the image, as an embedded asset", image["component:kitchen"])
	}
	if !reflect.DeepEqual(app["component:gen"], []EdgeKind{EdgeTool}) || !reflect.DeepEqual(image["component:gen"], []EdgeKind{EdgeTool}) {
		t.Errorf("a build tool is a tool of every artifact that named it: %v %v", app["component:gen"], image["component:gen"])
	}
	if _, linked := app["component:assets"]; linked {
		t.Error("the app reached no file of the assets component")
	}
}

// assemblyDocument is a product of two artifacts, "build:a1" and "build:a2".
// Each file is given the artifacts it names and the form it reached them
// through; relations are given as they are.
func assemblyDocument(components []domain.Component, files []domain.UsedFile, relations []sbomwriter.Relation) *sbomwriter.Document {
	return &sbomwriter.Document{
		Product:    domain.Component{ID: "product", Name: "app", Type: "application"},
		Artifacts:  []domain.Component{{ID: "build:a1", Name: "a1", Type: "application"}, {ID: "build:a2", Name: "a2", Type: "application"}},
		Components: components,
		Files:      files,
		Relations: append([]sbomwriter.Relation{
			{From: "product", To: []string{"build:a1", "build:a2"}},
		}, relations...),
		Run: sbomwriter.RunMetadata{ToolName: "sbomb", ToolVendor: "sbomb", Timestamp: "2023-11-14T22:13:20Z"},
	}
}

func assemblyFile(path, form string, artifacts ...string) domain.UsedFile {
	properties := map[string][]string{"sbomb:file:linkageForm": {form}}
	for _, artifact := range artifacts {
		properties["sbomb:evidence:artifacts"] = append(properties["sbomb:evidence:artifacts"], "artifact:"+artifact)
	}
	if len(artifacts) == 0 {
		delete(properties, "sbomb:evidence:artifacts")
	}
	return domain.UsedFile{ID: domain.FileID{Anchor: "project", RelPath: path}, Class: domain.FileClassAsset, Properties: properties}
}

// TestABuildToolIsAToolOfEveryArtifactThatNamesItsComponent: a tool reaches a
// deliverable through the build, not through a file the deliverable contains.
// Here the component's build-tool file names only a2, while a1 names the
// component through an embedded asset; a1 was built with the tool all the
// same, so both artifacts use it. This is the shape of gcc-ninja/p12-assets,
// where only the image's configuration names the image but every artifact
// that names the asset component was built by it.
func TestABuildToolIsAToolOfEveryArtifactThatNamesItsComponent(t *testing.T) {
	config := assemblyFile("assets/config.yaml", domain.LinkageBuildTool, "build:a2")
	data := assemblyFile("assets/data.bin", domain.LinkageEmbeddedAsset, "build:a1")
	document := assemblyDocument(
		[]domain.Component{{ID: "component:t", Name: "t", LinkageForms: []string{domain.LinkageBuildTool, domain.LinkageEmbeddedAsset}}},
		[]domain.UsedFile{config, data},
		[]sbomwriter.Relation{
			{From: "component:t", To: []string{config.ID.Canonical(), data.ID.Canonical()}},
			{From: "build:a1", To: []string{"component:t"}},
			{From: "build:a2", To: []string{"component:t"}},
		},
	)
	model := build(t, document, Options{})
	if got := edgesFrom(model, "artifact:build:a1")["component:t"]; !reflect.DeepEqual(got, []EdgeKind{EdgeEmbeds, EdgeTool}) {
		t.Errorf("a1 -> t = %v; the asset reached a1, and a1 was built with the tool", got)
	}
	if got := edgesFrom(model, "artifact:build:a2")["component:t"]; !reflect.DeepEqual(got, []EdgeKind{EdgeTool}) {
		t.Errorf("a2 -> t = %v; only the tool's file reached a2", got)
	}
}

// TestAGroupingMemberWhoseFilesNameNoArtifactIsLinkedFromTheProduct: in an
// assembly the product stands for "no artifact claimed this". A grouped member
// that reached the build through a linkage form, but none of whose files names
// an artifact, is linked from the product -- dropping it would lose the only
// statement of how it reached the build.
func TestAGroupingMemberWhoseFilesNameNoArtifactIsLinkedFromTheProduct(t *testing.T) {
	library := assemblyFile("sysroot/libm.so", domain.LinkageDynamic)
	document := assemblyDocument(
		[]domain.Component{
			{ID: "component:environment", Name: "environment", Type: "platform"},
			{ID: "component:m", Name: "m", LinkageForms: []string{domain.LinkageDynamic}},
		},
		[]domain.UsedFile{library},
		[]sbomwriter.Relation{
			{From: "component:environment", To: []string{"component:m"}},
			{From: "component:m", To: []string{library.ID.Canonical()}},
			{From: "product", To: []string{"component:environment"}},
		},
	)
	model := build(t, document, Options{})
	if got := edgesFrom(model, "product:app")["component:m"]; !reflect.DeepEqual(got, []EdgeKind{EdgeDynamicLink}) {
		t.Errorf("product -> m = %v", got)
	}
	for _, artifact := range []string{"artifact:build:a1", "artifact:build:a2"} {
		if got := edgesFrom(model, artifact)["component:m"]; got != nil {
			t.Errorf("%s -> m = %v; no file of m names it", artifact, got)
		}
	}
}

func TestAComponentNoArtifactClaimedStaysUnderTheProduct(t *testing.T) {
	model := allFields(t)
	product := edgesFrom(model, "product:sentinel-product")
	if !reflect.DeepEqual(product["component:type-file"], []EdgeKind{EdgeStaticLink}) {
		t.Errorf("product -> type-file = %v", product["component:type-file"])
	}
	if !reflect.DeepEqual(product["artifact:build:bin/app"], []EdgeKind{EdgeContains}) {
		t.Errorf("the product contains its artifacts: %v", product["artifact:build:bin/app"])
	}
}

func TestTheBuildEnvironmentContainsTheToolchain(t *testing.T) {
	model := allFields(t)
	environment := edgesFrom(model, "component:build-environment")
	for _, member := range []string{"component:libc", "component:toolchain-headers"} {
		if !reflect.DeepEqual(environment[member], []EdgeKind{EdgeContains}) {
			t.Errorf("build-environment -> %s = %v", member, environment[member])
		}
	}
	if got := edgesFrom(model, "product:sentinel-product")["component:build-environment"]; !reflect.DeepEqual(got, []EdgeKind{EdgeDependsOn}) {
		t.Errorf("product -> build-environment = %v", got)
	}
	// A member the evidence linked is linked from the deliverable as well; one
	// that reached nothing through a form stays under its grouping only.
	if got := edgesFrom(model, "artifact:build:bin/app")["component:libc"]; !reflect.DeepEqual(got, []EdgeKind{EdgeDynamicLink, EdgeProvidedDependency}) {
		t.Errorf("app -> libc = %v", got)
	}
	for _, edge := range model.Edges {
		if edge.To == "component:toolchain-headers" && edge.From != "component:build-environment" {
			t.Errorf("toolchain headers reached the product's dependency path: %v", edge)
		}
	}
}

func TestAnEmptyRelationIsAKnownLeaf(t *testing.T) {
	model := allFields(t)
	if !packageOf(t, model, "component:kitchen@1.2.3-sentinel").KnownLeaf {
		t.Error("a component whose relation names nothing is a known leaf")
	}
	if !fileOf(t, model, "file:build:_deps/kitchen-src/kitchen.c").KnownLeaf {
		t.Error("a used file depends on nothing")
	}
	if packageOf(t, model, "component:kitchen").KnownLeaf {
		t.Error("a component that contains files is no leaf")
	}
	if fileOf(t, model, "file:build:_deps/kitchen-src/NOTICE").KnownLeaf {
		t.Error("an evidence-only file has no counterpart in the dependency graph to be a leaf of")
	}
}

func TestNoAssertionIsItsOwnKind(t *testing.T) {
	for _, finding := range []domain.LicenseFinding{{Name: "NOASSERTION"}, {Expression: "NOASSERTION"}, {SPDXID: "NOASSERTION"}} {
		target, ok := routeLicense(finding, true)
		if !ok || target.license != (License{Kind: LicenseNoAssertion}) || target.custom != nil {
			t.Errorf("%+v -> %+v", finding, target)
		}
	}
	if target, _ := routeLicense(domain.LicenseFinding{Expression: "NONE"}, true); target.license.Kind != LicenseNone {
		t.Errorf("NONE -> %+v", target)
	}
	if _, ok := routeLicense(domain.LicenseFinding{}, true); ok {
		t.Error("an empty finding states something")
	}
}

func TestANameThatIsNoIdentifierBecomesAnInjectiveDefinedLicenseRef(t *testing.T) {
	a, _ := routeLicense(domain.LicenseFinding{Name: "GPL v2"}, true)
	b, _ := routeLicense(domain.LicenseFinding{Name: "GPL-v2"}, true)
	c, _ := routeLicense(domain.LicenseFinding{Name: "GPL/v2"}, true)
	if a.license.Expression == b.license.Expression || a.license.Expression == c.license.Expression {
		t.Errorf("distinct names share a reference: %q %q %q", a.license.Expression, b.license.Expression, c.license.Expression)
	}
	if b.license.Expression != "LicenseRef-sbomb-GPL-v2" {
		t.Errorf("a name that is already reference-safe is kept as it is: %q", b.license.Expression)
	}
	if !strings.HasPrefix(a.license.Expression, "LicenseRef-sbomb-GPL-v2-") || len(a.custom) != 1 || a.custom[0].Name != "GPL v2" || !a.whole {
		t.Errorf("reference %q, definition %+v", a.license.Expression, a.custom)
	}
	if _, err := ParseExpression(a.license.Expression); err != nil {
		t.Errorf("the minted reference is no expression: %v", err)
	}
	model := allFields(t)
	var found bool
	for _, custom := range model.Custom {
		if custom.Name == "Sentinel Custom Licence" {
			found = true
			if custom.TextRetained || custom.Text != "Sentinel Custom Licence" || custom.TextFile != "build:_deps/kitchen-src/COPYING.custom" {
				t.Errorf("a text that is not UTF-8 is not a licence text; the definition is the name and names the file the bytes travel on: %+v", custom)
			}
		}
		if custom.Name == "Sentinel Retained Licence" && (!custom.TextRetained || custom.Text != "sentinel retained custom licence text\n") {
			t.Errorf("a retained text defines its reference: %+v", custom)
		}
	}
	if !found {
		t.Error("the custom licence is not defined")
	}
}

func TestACompoundExpressionIsOneLicense(t *testing.T) {
	target, _ := routeLicense(domain.LicenseFinding{Expression: "(MIT OR Apache-2.0) AND BSD-3-Clause"}, true)
	if target.license.Expression != "(MIT OR Apache-2.0) AND BSD-3-Clause" || !target.license.ListedIDs || target.custom != nil {
		t.Errorf("%+v", target)
	}
	with, _ := routeLicense(domain.LicenseFinding{Expression: "GPL-2.0-only WITH Classpath-exception-2.0"}, true)
	if with.license.Expression != "GPL-2.0-only WITH Classpath-exception-2.0" {
		t.Errorf("%+v", with)
	}
}

// TestAnExpressionWithAnUnknownIdentifierKeepsItsStructure: only the operand
// the list does not carry is replaced by a reference sbomb defines. Falling
// back to the SPDXID the pipeline fills with the first identifier would state
// "MIT" for "MIT AND Acme-Proprietary-1.0" -- a different licence, and one
// obligation fewer, than the one observed.
func TestAnExpressionWithAnUnknownIdentifierKeepsItsStructure(t *testing.T) {
	target, _ := routeLicense(domain.LicenseFinding{Expression: "MIT AND Acme-Proprietary-1.0", SPDXID: "MIT", Name: "MIT AND Acme-Proprietary-1.0"}, true)
	if target.license.Expression != "MIT AND LicenseRef-sbomb-Acme-Proprietary-1.0" || !target.license.ListedIDs || target.whole {
		t.Errorf("%+v", target)
	}
	if len(target.custom) != 1 || target.custom[0] != (CustomLicense{Ref: "LicenseRef-sbomb-Acme-Proprietary-1.0", Name: "Acme-Proprietary-1.0", Text: "Acme-Proprietary-1.0"}) {
		t.Errorf("the unlisted operand is defined by what was observed: %+v", target.custom)
	}
	// Every unlisted operand gets its own reference, a "+" stays with the
	// operand it qualifies, and listed operands and exceptions stay as they are.
	target, _ = routeLicense(domain.LicenseFinding{Expression: "(Acme-1.0+ OR GPL-2.0-only WITH Classpath-exception-2.0) AND Beta-2"}, true)
	if want := "(LicenseRef-sbomb-Acme-1.0--"; !strings.HasPrefix(target.license.Expression, want) ||
		!strings.HasSuffix(target.license.Expression, " OR GPL-2.0-only WITH Classpath-exception-2.0) AND LicenseRef-sbomb-Beta-2") || len(target.custom) != 2 {
		t.Errorf("%+v", target)
	}
	if _, err := ParseExpression(target.license.Expression); err != nil {
		t.Errorf("the rewritten expression is no expression: %v", err)
	}
	// A lone unlisted identifier is the whole finding.
	if target, _ := routeLicense(domain.LicenseFinding{Expression: "Acme-Proprietary-1.0"}, true); target.license.Expression != "LicenseRef-sbomb-Acme-Proprietary-1.0" || !target.whole {
		t.Errorf("%+v", target)
	}
	// A licence from the list where an exception belongs is a misreading no
	// minted exception would state truthfully; the whole text is one
	// reference rather than an expression that claims Apache-2.0 is an
	// exception.
	if target, _ := routeLicense(domain.LicenseFinding{Expression: "MIT WITH Apache-2.0", SPDXID: "MIT"}, true); !IsSbombLicenseRef(target.license.Expression) || !target.whole || target.custom[0].Name != "MIT WITH Apache-2.0" {
		t.Errorf("a licence after WITH: %+v", target)
	}
	// A text that is no expression is one reference for the whole text, never
	// its first identifier.
	if target, _ := routeLicense(domain.LicenseFinding{Expression: "MIT, see COPYING", SPDXID: "MIT"}, true); !IsSbombLicenseRef(target.license.Expression) || target.custom[0].Name != "MIT, see COPYING" {
		t.Errorf("%+v", target)
	}
	// Without an expression a listed SPDXID is the licence.
	if target, _ := routeLicense(domain.LicenseFinding{SPDXID: "MIT", Name: "The MIT License"}, true); target.license.Expression != "MIT" || target.custom != nil {
		t.Errorf("%+v", target)
	}
	// A LicenseRef the build stated is kept, and sbomb defines nothing for it.
	if target, _ := routeLicense(domain.LicenseFinding{Expression: "LicenseRef-acme"}, true); target.license.Expression != "LicenseRef-acme" || target.custom != nil || target.license.ListedIDs {
		t.Errorf("%+v", target)
	}
	// An exception on the list names something from the list as much as a
	// licence does, so the list version is owed even when the licence it
	// modifies is a LicenseRef -- with the case of the list, as for a licence.
	for _, expression := range []string{"LicenseRef-acme WITH Classpath-exception-2.0", "LicenseRef-acme with classpath-exception-2.0"} {
		target, _ := routeLicense(domain.LicenseFinding{Expression: expression}, true)
		if target.license.Expression != "LicenseRef-acme WITH Classpath-exception-2.0" || target.custom != nil || !target.license.ListedIDs {
			t.Errorf("%q: %+v, want it as it is, spelled as the list spells it, with ListedIDs", expression, target)
		}
	}
}

// Annex B: identifiers "should be matched in a case-insensitive manner", so
// "mit" is the listed MIT licence. It is written as the list spells it, stays
// listed (the expression carries the licence-list version) and mints no
// custom licence -- inside a compound expression, after WITH, in the SPDXID,
// and in a version without AdditionRef, where a lower-case listed exception
// must not collapse the expression into one opaque reference.
func TestAListedIdentifierInAnotherCaseIsTheListedLicence(t *testing.T) {
	for _, test := range []struct {
		finding domain.LicenseFinding
		want    string
	}{
		{domain.LicenseFinding{Expression: "mit"}, "MIT"},
		{domain.LicenseFinding{SPDXID: "mit"}, "MIT"},
		{domain.LicenseFinding{Expression: "apache-2.0 OR mit"}, "Apache-2.0 OR MIT"},
		{domain.LicenseFinding{Expression: "BSD-3-Clause AND bsd-2-clause"}, "BSD-3-Clause AND BSD-2-Clause"},
		{domain.LicenseFinding{Expression: "gpl-2.0-only with classpath-exception-2.0"}, "GPL-2.0-only WITH Classpath-exception-2.0"},
	} {
		for _, customAdditions := range []bool{true, false} {
			target, _ := routeLicense(test.finding, customAdditions)
			if target.license.Expression != test.want || !target.license.ListedIDs || target.custom != nil || target.whole {
				t.Errorf("%+v (customAdditions %v): %+v, want %q, listed, nothing minted", test.finding, customAdditions, target, test.want)
			}
		}
	}
	// An identifier off the list keeps the case it was observed in.
	if target, _ := routeLicense(domain.LicenseFinding{Expression: "mit AND acme-1.0"}, true); target.license.Expression != "MIT AND LicenseRef-sbomb-acme-1.0" {
		t.Errorf("%+v", target)
	}
}

// Annex B: "There MUST NOT be white space between a license-id and any
// following +". An observed "GPL-2.0 +" meant "GPL-2.0+", and is written so;
// carried byte for byte it would be no expression at all.
func TestAPlusSetApartFromItsIdentifierIsJoinedToIt(t *testing.T) {
	for expression, want := range map[string]string{
		"GPL-2.0 +":        "GPL-2.0+",
		"GPL-2.0 + or MIT": "GPL-2.0+ OR MIT",
		"(LGPL-2.1 +)":     "(LGPL-2.1+)",
	} {
		target, _ := routeLicense(domain.LicenseFinding{Expression: expression}, true)
		if target.license.Expression != want || !target.license.ListedIDs || target.custom != nil {
			t.Errorf("%q: %+v, want %q", expression, target, want)
		}
		if _, err := ParseExpression(target.license.Expression); err != nil {
			t.Errorf("%q: the written expression is no expression: %v", expression, err)
		}
	}
	// An unlisted identifier set apart from its "+" is minted for "Acme-1.0+",
	// the same observation as the joined spelling.
	spaced, _ := routeLicense(domain.LicenseFinding{Expression: "Acme-1.0 +"}, true)
	joined, _ := routeLicense(domain.LicenseFinding{Expression: "Acme-1.0+"}, true)
	if spaced.license.Expression != joined.license.Expression {
		t.Errorf("%q and %q", spaced.license.Expression, joined.license.Expression)
	}
}

// TestAnUnlistedExceptionIsAnAdditionRefWhereTheVersionHasOne: SPDX 3.0.1
// defines SimpleLicensingText as "a license or addition that is not listed",
// so an exception the list does not carry is minted as an AdditionRef and the
// listed licence before WITH stays machine-readable. A version without
// AdditionRef (SPDX 2.3) gets one reference for the whole text instead, and so
// does an AdditionRef the build stated, which that version cannot write.
func TestAnUnlistedExceptionIsAnAdditionRefWhereTheVersionHasOne(t *testing.T) {
	finding := domain.LicenseFinding{Expression: "GPL-2.0-or-later WITH Acme-linking-exception", SPDXID: "GPL-2.0-or-later"}
	target, _ := routeLicense(finding, true)
	if target.license.Expression != "GPL-2.0-or-later WITH AdditionRef-sbomb-Acme-linking-exception" || target.whole || !target.license.ListedIDs {
		t.Errorf("with AdditionRef: %+v", target)
	}
	if len(target.custom) != 1 || !target.custom[0].Addition || target.custom[0].Ref != "AdditionRef-sbomb-Acme-linking-exception" ||
		target.custom[0].Name != "Acme-linking-exception" {
		t.Errorf("the exception is defined as an addition: %+v", target.custom)
	}
	whole, _ := routeLicense(finding, false)
	if !IsSbombLicenseRef(whole.license.Expression) || !whole.whole || whole.custom[0].Name != finding.Expression || whole.custom[0].Addition {
		t.Errorf("without AdditionRef: %+v", whole)
	}

	stated := domain.LicenseFinding{Expression: "MIT WITH AdditionRef-acme"}
	if kept, _ := routeLicense(stated, true); kept.license.Expression != "MIT WITH AdditionRef-acme" || kept.custom != nil {
		t.Errorf("a stated AdditionRef is kept where the version has the form: %+v", kept)
	}
	if replaced, _ := routeLicense(stated, false); !IsSbombLicenseRef(replaced.license.Expression) || !replaced.whole {
		t.Errorf("a stated AdditionRef is one whole reference where the version lacks the form: %+v", replaced)
	}
	// Both: an unlisted licence and an unlisted exception keep the structure.
	both, _ := routeLicense(domain.LicenseFinding{Expression: "Acme-1.0 WITH Acme-exception OR MIT"}, true)
	if both.license.Expression != "LicenseRef-sbomb-Acme-1.0 WITH AdditionRef-sbomb-Acme-exception OR MIT" || len(both.custom) != 2 {
		t.Errorf("both: %+v", both)
	}
}

// TestAnAdditionRefIsDefinedInTheModel builds a component whose
// licence has an unlisted exception and checks the model defines it once.
func TestAnAdditionRefIsDefinedInTheModel(t *testing.T) {
	document := sbomwritertest.AllFields()
	document.Components[0].Licenses = []domain.LicenseFinding{{Expression: "GPL-2.0-or-later WITH Acme-linking-exception"}}
	model := build(t, document, Options{CustomAdditions: true})
	defined := false
	for _, custom := range model.Custom {
		defined = defined || custom.Ref == "AdditionRef-sbomb-Acme-linking-exception" && custom.Addition
	}
	if !defined {
		t.Errorf("the minted exception is not defined: %+v", model.Custom)
	}
}

// TestAnIdentifierLineIsCarriedAsItsExpression routes what the licence
// resolver actually produces for an SPDX-License-Identifier line -- an
// expression, and an SPDXID that is its first identifier -- rather than a
// finding written by hand, so that a change on either side of the seam that
// brings the first-identifier fallback back is caught.
func TestAnIdentifierLineIsCarriedAsItsExpression(t *testing.T) {
	for line, want := range map[string]string{
		"// SPDX-License-Identifier: MIT or Acme-Proprietary-1.0\n":  "MIT OR LicenseRef-sbomb-Acme-Proprietary-1.0",
		"// SPDX-License-Identifier: MIT AND Acme-Proprietary-1.0\n": "MIT AND LicenseRef-sbomb-Acme-Proprietary-1.0",
		"// SPDX-License-Identifier: Apache-2.0 or MIT\n":            "Apache-2.0 OR MIT",
	} {
		finding := license.ResolveFromText(line, "project:src/a.c")
		if finding.SPDXID == "" || finding.Expression == finding.SPDXID {
			t.Fatalf("%q resolves to %+v; the test no longer exercises the first-identifier SPDXID", line, finding)
		}
		target, _ := routeLicense(finding, true)
		if target.license.Expression != want {
			t.Errorf("%q -> %q, want %q", line, target.license.Expression, want)
		}
	}
}

// TestLowerCaseOperatorsAreOperators: SPDX 2.x readers accept "MIT or
// Apache-2.0", and such lines occur in real headers. Read as written they are
// three identifiers; the operators are upper-cased and the licence choice is
// kept.
func TestLowerCaseOperatorsAreOperators(t *testing.T) {
	for expression, want := range map[string]string{
		"MIT or Apache-2.0":                         "MIT OR Apache-2.0",
		"(MIT and BSD-3-Clause) Or Zlib":            "(MIT AND BSD-3-Clause) OR Zlib",
		"GPL-2.0-only with Classpath-exception-2.0": "GPL-2.0-only WITH Classpath-exception-2.0",
		"MIT or Acme-Proprietary-1.0":               "MIT OR LicenseRef-sbomb-Acme-Proprietary-1.0",
		"MIT OR Apache-2.0":                         "MIT OR Apache-2.0",
		"(MIT  OR Apache-2.0)":                      "(MIT  OR Apache-2.0)",
	} {
		target, _ := routeLicense(domain.LicenseFinding{Expression: expression, SPDXID: "MIT", Name: expression}, true)
		if target.license.Expression != want || !target.license.ListedIDs {
			t.Errorf("%q -> %+v, want %q", expression, target, want)
		}
	}
}

func TestObservedLicencesAreDeclaredAndTheConclusionIsConcluded(t *testing.T) {
	kitchen := packageOf(t, allFields(t), "component:kitchen")
	if len(kitchen.Concluded) != 1 || kitchen.Concluded[0].License.Expression != "MIT OR Apache-2.0" {
		t.Errorf("concluded = %+v", kitchen.Concluded)
	}
	if len(kitchen.Declared) != 2 {
		t.Fatalf("declared = %+v", kitchen.Declared)
	}
	for _, use := range kitchen.Declared {
		if use.License.Expression == "MIT" && !hasProperty(use.Properties, "sbomb:license:technique", "spdx-digest") {
			t.Errorf("the observation keeps how it was made: %+v", use.Properties)
		}
	}
	concluded := kitchen.Concluded[0].Properties
	for _, want := range []Property{
		{Name: "sbomb:license:confidence", Value: "high"}, {Name: "sbomb:license:conflictingValue", Value: "sentinel-conflicting-value"},
		{Name: "sbomb:license:evidenceClass", Value: "file-level"}, {Name: "sbomb:license:reason", Value: "sentinel-reason-code"},
	} {
		if !hasProperty(concluded, want.Name, want.Value) {
			t.Errorf("the conclusion lacks %v: %v", want, concluded)
		}
	}
}

// TestALicenceFoundInAFileIsDeclaredUnlessTheLicenceIsCurated: in the 3.0.1
// vocabulary hasDeclaredLicense is that an artifact was found to contain a
// licence, "for example as detected by use of automated tooling". Every
// detection technique is that, and the component states the same finding read
// from the same file as declared, so the file must too -- one observation is
// never published with both verbs.
func TestALicenceFoundInAFileIsDeclaredUnlessTheLicenceIsCurated(t *testing.T) {
	model := allFields(t)
	license := fileOf(t, model, "file:build:_deps/kitchen-src/LICENSE")
	if len(license.Detected) != 1 || license.Detected[0].Acknowledgement != "declared" {
		t.Errorf("an identifier the file states about itself is declared: %+v", license.Detected)
	}
	custom := fileOf(t, model, "file:build:_deps/kitchen-src/COPYING.custom")
	if len(custom.Detected) != 1 || custom.Detected[0].Acknowledgement != "declared" {
		t.Errorf("a template match is declared: %+v", custom.Detected)
	}
	kitchen := packageOf(t, model, "component:kitchen")
	var declaredByPackage bool
	for _, use := range kitchen.Declared {
		declaredByPackage = declaredByPackage || use.License == custom.Detected[0].License
	}
	if !declaredByPackage {
		t.Errorf("the component declares %v read from the same file, so the file must declare it too: %+v", custom.Detected[0].License, kitchen.Declared)
	}
	if notice := fileOf(t, model, "file:build:_deps/kitchen-src/NOTICE"); len(notice.Detected) != 0 {
		t.Errorf("a notice states no licence: %+v", notice.Detected)
	}

	curated := sbomwritertest.AllFields()
	curated.Components[0].Licenses[0].Source = "curated"
	if detected := fileOf(t, build(t, curated, Options{}), "file:build:_deps/kitchen-src/LICENSE").Detected; detected[0].Acknowledgement != "concluded" {
		t.Errorf("beside a curated licence every file is a conclusion: %+v", detected)
	}
}

func TestASharedFileUnitesTheStatementsOfEveryComponent(t *testing.T) {
	document := sbomwritertest.AllFields()
	shared := domain.FileID{Anchor: "build", RelPath: "_deps/kitchen-src/NOTICE"}
	document.Components[1].Copyrights = []domain.CopyrightStatement{
		{Text: "Copyright sentinel holder B", File: shared},
		{Text: "Copyright sentinel holder D", File: shared},
	}
	model := build(t, document, Options{})
	notice := fileOf(t, model, "file:build:_deps/kitchen-src/NOTICE")
	if !reflect.DeepEqual(notice.Copyright, []string{"Copyright sentinel holder B", "Copyright sentinel holder D"}) {
		t.Errorf("copyright = %v", notice.Copyright)
	}
	for _, local := range []string{"component:kitchen", "component:kitchen@1.2.3-sentinel"} {
		if evidence := packageOf(t, model, local).EvidenceFiles; !contains(evidence, "file:build:_deps/kitchen-src/NOTICE") {
			t.Errorf("%s does not name the shared file as evidence: %v", local, evidence)
		}
	}
}

// TestTwoLicencesThatShareANameButNotATextAreTwoReferences: a licence called
// "Proprietary" in two components, each read from a file with its own text,
// is two licences. One shared definition would state that the second component
// is under the first one's text. Observed by name alone, the two are one
// reference: the name is all either observation says.
func TestTwoLicencesThatShareANameButNotATextAreTwoReferences(t *testing.T) {
	document := sbomwritertest.AllFields()
	// Components 0 and 2 (kitchen and hdr); 1 is kitchen again, under the
	// same name and version.
	texts := map[int]string{0: "Acme proprietary terms: no redistribution\n", 2: "Beta proprietary terms: redistribution allowed\n"}
	for index, text := range texts {
		file := domain.FileID{Anchor: "build", RelPath: fmt.Sprintf("proprietary-%d/LICENSE", index)}
		component := &document.Components[index]
		component.LicenseEvidence = append(component.LicenseEvidence, domain.LicenseFinding{Name: "Proprietary", Source: file.Canonical()})
		component.LicenseArtifacts = append(component.LicenseArtifacts, domain.LicenseArtifact{
			Kind: domain.LicenseArtifactLicense, File: file, Bytes: []byte(text), DetectedID: "Proprietary", Technique: "spdx-template",
		})
	}
	model := build(t, document, Options{LicenseText: sbomwriter.LicenseTextEvidence})
	byText := map[string]string{}
	for _, custom := range model.Custom {
		if custom.Name == "Proprietary" {
			byText[custom.Text] = custom.Ref
		}
	}
	if len(byText) != 2 || byText[texts[0]] == byText[texts[2]] || byText[texts[0]] == "" {
		t.Fatalf("definitions by text = %v", byText)
	}
	for index, text := range texts {
		pkg := packageOf(t, model, string(document.Components[index].ID))
		var found bool
		for _, use := range pkg.Declared {
			found = found || use.License.Expression == byText[text]
		}
		if !found {
			t.Errorf("%s does not declare the licence of its own text %s: %+v", pkg.LocalID, byText[text], pkg.Declared)
		}
		file := fileOf(t, model, "file:"+fmt.Sprintf("build:proprietary-%d/LICENSE", index))
		if len(file.Detected) != 1 || file.Detected[0].License.Expression != byText[text] {
			t.Errorf("the file of %q states %+v", text, file.Detected)
		}
	}

	bare := sbomwritertest.AllFields()
	for index := range texts {
		bare.Components[index].LicenseEvidence = append(bare.Components[index].LicenseEvidence, domain.LicenseFinding{Name: "Proprietary"})
	}
	var refs []string
	for _, custom := range build(t, bare, Options{LicenseText: sbomwriter.LicenseTextEvidence}).Custom {
		if custom.Name == "Proprietary" {
			refs = append(refs, custom.Ref)
		}
	}
	if !reflect.DeepEqual(refs, []string{"LicenseRef-sbomb-Proprietary"}) {
		t.Errorf("a name observed without a text is one shared reference: %v", refs)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestSuppliersAndOriginatorsAreOneOrganizationPerName(t *testing.T) {
	model := allFields(t)
	want := []string{"Sentinel Originator Org", "Sentinel Supplier Org", "Sentinel Vendor Org"}
	if !reflect.DeepEqual(model.Agents, want) {
		t.Errorf("agents = %v, want %v", model.Agents, want)
	}
	if model.Creator != "Sentinel Vendor Org" {
		t.Errorf("creator = %q", model.Creator)
	}
}

func TestAMissingFileHasNoHashAndSaysItIsMissing(t *testing.T) {
	document := sbomwritertest.AllFields()
	for index := range document.Files {
		if document.Files[index].Missing {
			document.Files[index].Hashes = map[string]string{"SHA-256": sbomwritertest.HashKitchenSource}
		}
	}
	// A retained licence artifact naming the same file does not give it its
	// digest back: the run marked the file missing, and the document must not
	// state a digest of bytes this run did not read as the used file.
	document.Components[0].LicenseArtifacts = append(document.Components[0].LicenseArtifacts, domain.LicenseArtifact{
		Kind: domain.LicenseArtifactLicense, File: domain.FileID{Anchor: "project", RelPath: "hdr/hdr.h"}, SHA256: sbomwritertest.HashKitchenSource,
	})
	missing := fileOf(t, build(t, document, Options{}), "file:project:hdr/hdr.h")
	if missing.Hashable() || len(missing.Hashes) != 0 {
		t.Errorf("a file nobody read has a digest: %+v", missing.Hashes)
	}
	if missing.Origin&OriginEvidence == 0 {
		t.Error("the licence artifact did not reach the missing file, so the guard was not exercised")
	}
	if !hasProperty(missing.Properties, "sbomb:file:missing", "true") || hasProperty(missing.Properties, "sbomb:file:size", "0") {
		t.Errorf("properties = %v", missing.Properties)
	}
}

func TestAModifiedComponentIsPatchedByItsPatches(t *testing.T) {
	model := allFields(t)
	kitchen := packageOf(t, model, "component:kitchen")
	if len(kitchen.Patches) != 4 {
		t.Fatalf("patches = %+v; the exact duplicate is one patch", kitchen.Patches)
	}
	first := fileOf(t, model, kitchen.Patches[0].LocalID)
	if first.Origin != OriginPatch || first.PatchFor != "component:kitchen" || first.Name != "0002-sentinel.patch" ||
		first.Description != "sentinel patch description B" || !hasProperty(first.Properties, "sbomb:patch:type", "backport") ||
		!hasProperty(first.Properties, "sbomb:patch:source", "conandata.yml") || first.Hashable() {
		t.Errorf("first patch = %+v", first)
	}
	if kitchen.SourceInfo != "sentinel signal prose" {
		t.Errorf("source info = %q", kitchen.SourceInfo)
	}
}

// TestAPatchOfNoFileIsStillNamed: Conan's base64 payload patches have no
// patch file, and SPDX 3.0.1 requires a name of every File. The file is named
// after its place among the component's patches, and the patch itself keeps
// the empty file name the metadata gave.
func TestAPatchOfNoFileIsStillNamed(t *testing.T) {
	model := allFields(t)
	kitchen := packageOf(t, model, "component:kitchen")
	for _, patch := range kitchen.Patches {
		file := fileOf(t, model, patch.LocalID)
		if file.Name == "" {
			t.Errorf("%s has no name", patch.LocalID)
		}
		if patch.File == "" && file.Name != "patch 2 of kitchen (the metadata names no patch file)" {
			t.Errorf("the patch of no file is named %q", file.Name)
		}
	}
}

func TestTwoPatchesDifferingOnlyInDescriptionStayTwo(t *testing.T) {
	kitchen := packageOf(t, allFields(t), "component:kitchen")
	var descriptions []string
	for _, patch := range kitchen.Patches {
		if patch.File == "0001-sentinel.patch" {
			descriptions = append(descriptions, patch.Description)
		}
	}
	if !reflect.DeepEqual(descriptions, []string{"sentinel patch description A", "sentinel patch description A2"}) {
		t.Errorf("descriptions = %v", descriptions)
	}
}

func TestTheSameCVEWithTwoReasonsIsTwoStatements(t *testing.T) {
	kitchen := packageOf(t, allFields(t), "component:kitchen")
	want := []CVEExclusion{
		{ID: "CVE-2024-0001", Reason: "sentinel reason one"},
		{ID: "CVE-2024-0001", Reason: "sentinel reason two"},
		{ID: "CVE-2024-0002"},
		{ID: "GHSA-sent-inel-0001", Reason: "sentinel reason of an advisory"},
	}
	if !reflect.DeepEqual(kitchen.CVEExclusions, want) {
		t.Errorf("exclusions = %+v", kitchen.CVEExclusions)
	}
}

func TestRetainedLicenceTextTravelsWithItsFileOnlyWhenAskedFor(t *testing.T) {
	withText := fileOf(t, allFields(t), "file:build:_deps/kitchen-src/LICENSE")
	if len(withText.LicenseText) != 1 || string(withText.LicenseText[0]) != "sentinel licence text of MIT\n" {
		t.Errorf("licence text = %q", withText.LicenseText)
	}
	without := fileOf(t, build(t, sbomwritertest.AllFields(), Options{}), "file:build:_deps/kitchen-src/LICENSE")
	if len(without.LicenseText) != 0 {
		t.Error("licence bytes reached a document that did not ask for them")
	}
	if notice := fileOf(t, allFields(t), "file:build:_deps/kitchen-src/NOTICE"); len(notice.LicenseText) != 0 {
		t.Error("notice bytes are never written")
	}
}

func TestCopyrightStatementsStayOnTheFileTheyWereReadFrom(t *testing.T) {
	model := allFields(t)
	if header := fileOf(t, model, "file:build:_deps/kitchen-src/kitchen.h"); !reflect.DeepEqual(header.Copyright, []string{"Copyright sentinel holder A"}) {
		t.Errorf("header copyright = %v", header.Copyright)
	}
	kitchen := packageOf(t, model, "component:kitchen")
	if !reflect.DeepEqual(kitchen.UnfiledCopyright, []string{"Copyright sentinel holder C, of no file"}) {
		t.Errorf("a statement of no file is a copyright notice of the package: %v", kitchen.UnfiledCopyright)
	}
}

func TestTheCuratedCopyrightIsThePackageCopyright(t *testing.T) {
	if copyright := packageOf(t, allFields(t), "component:kitchen").Copyright; copyright != "Copyright sentinel curated notice" {
		t.Errorf("copyright = %q", copyright)
	}
}

func TestTheDigestCoversCreatedAndSpecVersion(t *testing.T) {
	base := Digest(allFields(t))
	later := sbomwritertest.AllFields()
	later.Run.Timestamp = "2024-01-01T00:00:00Z"
	if string(Digest(build(t, later, Options{LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true}))) == string(base) {
		t.Error("another creation time gives the same digest")
	}
	other := build(t, sbomwritertest.AllFields(), Options{SpecVersion: "2.3", LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true})
	if string(Digest(other)) == string(base) {
		t.Error("another SPDX version gives the same digest")
	}
	if string(Digest(allFields(t))) != string(base) {
		t.Error("the digest of one document moved")
	}
}

// TestTheDigestKeepsEveryByteOfAPath: nothing upstream holds a file path to
// valid UTF-8, and the renderer keeps every byte of one in the element IRI,
// percent-encoded. Two runs that differ only in such a byte state different
// files, so they must not get one digest -- and with it one namespace for two
// documents that differ (section 28.11.2). encoding/json would hand both the
// same U+FFFD.
func TestTheDigestKeepsEveryByteOfAPath(t *testing.T) {
	withFile := func(relPath string) *Model {
		document := sbomwritertest.AllFields()
		document.Files = append(document.Files, domain.UsedFile{ID: domain.FileID{Anchor: "build", RelPath: relPath}, Class: domain.FileClassSource})
		return build(t, document, Options{LicenseText: sbomwriter.LicenseTextEvidence, Reproducible: true, CustomAdditions: true})
	}
	latin1E, latin1Grave := withFile("lat\xe9.c"), withFile("lat\xe8.c")
	if string(Digest(latin1E)) == string(Digest(latin1Grave)) {
		t.Error("two paths that differ in one byte that is not valid UTF-8 give the same digest")
	}
	if string(Digest(latin1E)) != string(Digest(withFile("lat\xe9.c"))) {
		t.Error("the digest of one document moved")
	}
}

// TestTheCreationTimeIsTheRunsTimeInUTCWithAZ: the mapping states the run's
// timestamp, normalised to the one form 3.0.1 accepts, in either mode. That a
// reproducible run's timestamp is SOURCE_DATE_EPOCH is decided where the run
// is set up, and tested there
// (TestAReproducibleSpdxDocumentIsCreatedAtSourceDateEpoch).
func TestTheCreationTimeIsTheRunsTimeInUTCWithAZ(t *testing.T) {
	document := sbomwritertest.AllFields()
	document.Run.Timestamp = "2023-11-15T00:13:20+02:00"
	for _, reproducible := range []bool{false, true} {
		if created := build(t, document, Options{Reproducible: reproducible}).Created; created != "2023-11-14T22:13:20Z" {
			t.Errorf("reproducible=%v: created = %q; SPDX states UTC with a Z", reproducible, created)
		}
	}
}

func TestReproducibleWithoutSourceDateEpochIsAnError(t *testing.T) {
	document := sbomwritertest.AllFields()
	document.Run.Timestamp = ""
	if _, err := Build(document, Options{SpecVersion: "3.0.1", Reproducible: true}); !errors.Is(err, ErrNoCreationTime) {
		t.Errorf("err = %v", err)
	}
}

func TestTheRunCarriesItsPropertiesAndAdapters(t *testing.T) {
	run := allFields(t).Run
	for _, want := range []Property{
		{Name: "sbomb:run:adapters", Value: "sentinel-adapter-a"}, {Name: "sbomb:run:adapters", Value: "sentinel-adapter-b"},
		{Name: "sbomb:run:reproducible", Value: "true"}, {Name: "sbomb:run:specVersion", Value: "3.0.1"}, {Name: "sbomb:run:toolVersion", Value: "0.0.0-sentinel"},
		{Name: "sbomb:run:policyProfile", Value: "sentinel-policy-profile"}, {Name: "sbomb:build:config", Value: "SentinelConfig"},
		{Name: "sbomb:build:generator", Value: "Sentinel Generator"},
	} {
		if !hasProperty(run.Properties, want.Name, want.Value) {
			t.Errorf("run lacks %v", want)
		}
	}
}

func TestParseExpressionFollowsTheGrammar(t *testing.T) {
	valid := map[string][]string{
		"MIT":                                  {"MIT"},
		"GPL-2.0+":                             {"GPL-2.0"},
		"(MIT OR Apache-2.0) AND BSD-3-Clause": {"MIT", "Apache-2.0", "BSD-3-Clause"},
		"Apache-2.0 WITH LLVM-exception":       {"Apache-2.0"},
		"DocumentRef-x:LicenseRef-y OR MIT":    {"DocumentRef-x:LicenseRef-y", "MIT"},
		"LicenseRef-a WITH AdditionRef-b":      {"LicenseRef-a"},
		"((MIT))":                              {"MIT"},
		// SPDX 3.0.1 annex B: operators are all upper or all lower case.
		"MIT or Apache-2.0": {"MIT", "Apache-2.0"},
		"(MIT and Zlib) or GPL-2.0-only with Classpath-exception-2.0": {"MIT", "Zlib", "GPL-2.0-only"},
	}
	for expression, licenses := range valid {
		parsed, err := ParseExpression(expression)
		if err != nil {
			t.Errorf("%q: %v", expression, err)
			continue
		}
		if !reflect.DeepEqual(parsed.Licenses, licenses) {
			t.Errorf("%q names %v, want %v", expression, parsed.Licenses, licenses)
		}
		if lower := strings.Contains(expression, " or ") || strings.Contains(expression, " and "); parsed.LowerCaseOperators != lower {
			t.Errorf("%q: LowerCaseOperators = %v", expression, parsed.LowerCaseOperators)
		}
	}
	for _, expression := range []string{"", "MIT And Zlib", "MIT oR Zlib", "MIT With LLVM-exception", "MIT OR", "(MIT", "MIT)", "AND MIT", "MIT WITH", "LicenseRef-a+", "MIT WITH LicenseRef-x", "a b", "MIT/Zlib", "x:y",
		// Annex B: "There MUST NOT be white space between a license-id and
		// any following +".
		"GPL-2.0 +", "GPL-2.0\t+", "(GPL-2.0 + OR MIT)", "+", "MIT OR +"} {
		if _, err := ParseExpression(expression); err == nil {
			t.Errorf("%q was accepted", expression)
		}
	}
}

// TestAUsedFileKeepsItsOwnDigestWhenItsLicenceReadDisagrees: a licence file
// that is also a used file is hashed twice, once as the used file and once as
// the retained licence artifact, and one digest per algorithm is kept. It must
// be the used file's, the one the evidence chain hashed. The digests were
// ordered by value before the duplicate was dropped, so whichever was smaller
// won: a file that changed between the two reads stated the licence read's
// digest, and the used file's own was dropped without a word.
func TestAUsedFileKeepsItsOwnDigestWhenItsLicenceReadDisagrees(t *testing.T) {
	document := sbomwritertest.AllFields()
	used := -1
	for index, file := range document.Files {
		if !file.Missing {
			used = index
			break
		}
	}
	if used < 0 {
		t.Fatal("the synthetic document has no file that was read")
	}
	usedDigest, licenceDigest := strings.Repeat("f", 64), strings.Repeat("0", 64)
	document.Files[used].Hashes = map[string]string{"SHA-256": usedDigest}
	document.Components[0].LicenseArtifacts = append(document.Components[0].LicenseArtifacts, domain.LicenseArtifact{
		Kind: domain.LicenseArtifactLicense, File: document.Files[used].ID, SHA256: licenceDigest,
	})
	file := fileOf(t, build(t, document, Options{}), "file:"+document.Files[used].ID.Canonical())
	if file.Origin&OriginEvidence == 0 {
		t.Fatal("the licence artifact did not reach the used file, so two digests were never offered")
	}
	if len(file.Hashes) != 1 || file.Hashes[0].Value != usedDigest {
		t.Errorf("hashes = %+v, want the used file's own SHA-256 alone", file.Hashes)
	}
}
