package generate

import (
	"sort"
	"strings"

	"github.com/example/sbomb/internal/adapters/cmakeapi"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

// productID is the identity of the root component inside the document. It is
// not a bom-ref; the writer derives that (section 36.1).
const productID = "product"

// buildEnvironmentID groups everything that was needed to build the product
// but is not part of it (section 24.2).
const buildEnvironmentID = "component:build-environment"

// isBuildEnvironment reports whether a component describes the build
// environment rather than the product.
func isBuildEnvironment(scope string) bool {
	return scope == "toolchain" || scope == "system"
}

// buildDocument assembles the format-neutral document: a root product, the
// grouping components the used files belong to, and the relations between
// them (sections 19 and 28.5).
func buildDocument(
	cfg config.Config,
	versionSource string,
	resolver *componentResolver,
	deliverables []Deliverable,
	files []domain.UsedFile,
	findings []domain.Finding,
	run sbomwriter.RunMetadata,
) (*sbomwriter.Document, []domain.Finding) {
	document := &sbomwriter.Document{
		Product: productComponent(cfg, versionSource, deliverables),
		Files:   files,
		Run:     run,
	}

	groups, componentFindings := groupFilesByComponent(resolver, files)
	findings = append(findings, componentFindings...)
	document.Findings = findings
	relations := make([]sbomwriter.Relation, 0, len(groups)+1)
	productTargets := make([]string, 0, len(groups))

	// Toolchain and system components are not project dependencies. Section
	// 24.2 hangs them under a synthetic build-environment component so that a
	// consumer can tell what the product needs from what building it needed.
	buildEnvironmentTargets := make([]string, 0)
	for _, group := range groups {
		document.Components = append(document.Components, group.component)
		if isBuildEnvironment(group.component.Scope) {
			buildEnvironmentTargets = append(buildEnvironmentTargets, group.component.ID)
		} else {
			productTargets = append(productTargets, group.component.ID)
		}
		fileRefs := make([]string, 0, len(group.files))
		for _, file := range group.files {
			fileRefs = append(fileRefs, file.ID.Canonical())
		}
		relations = append(relations, sbomwriter.Relation{From: group.component.ID, To: fileRefs})
	}
	if len(buildEnvironmentTargets) > 0 {
		sort.Strings(buildEnvironmentTargets)
		document.Components = append(document.Components, domain.Component{
			ID:    buildEnvironmentID,
			Name:  "build-environment",
			Type:  "framework",
			Scope: "toolchain",
			Properties: map[string][]string{
				"sbomb:component:detectedBy": {"synthetic"},
			},
		})
		productTargets = append(productTargets, buildEnvironmentID)
		relations = append(relations, sbomwriter.Relation{From: buildEnvironmentID, To: buildEnvironmentTargets})
	}
	sort.Strings(productTargets)
	relations = append(relations, sbomwriter.Relation{From: productID, To: productTargets})
	sort.Slice(relations, func(i, j int) bool { return relations[i].From < relations[j].From })
	document.Relations = relations
	return document, findings
}

// projectFromCMake fills the product's name and version from what the build
// system already recorded, when the configuration named neither.
//
// The File API cache carries CMAKE_PROJECT_NAME and CMAKE_PROJECT_VERSION
// because project() was called with them, so this is evidence rather than an
// assertion, and it saves writing the same two facts twice. Precedence is the
// one section 32.2 states everywhere else: a configured value wins, and this
// only fills a gap.
//
// It returns where the version came from, so that the document can say so
// rather than presenting a curated value and a read one as the same claim.
func projectFromCMake(cfg *config.Config, model *cmakeapi.Model) string {
	source := ""
	if cfg.Project.Version != "" {
		source = "curated"
	}
	if model == nil {
		return source
	}
	// The name only in assembly mode. Section 6.1 says the root component of a
	// single-artifact document is the deliverable, and the deliverable is not
	// the project: a build of firmware.elf describes firmware.elf, whatever
	// the enclosing project() is called. Section 6.2 is where project.name is
	// the root, and that is where reading it from the build system saves
	// writing it twice.
	//
	// CMAKE_PROJECT_NAME is the top-level project. In a super-build that is
	// the outermost one, which is the product an assembly document is about.
	if cfg.Mode == "assembly" && cfg.Project.Name == "" {
		cfg.Project.Name = model.Cache["CMAKE_PROJECT_NAME"]
	}
	if cfg.Project.Version == "" {
		if version := model.Cache["CMAKE_PROJECT_VERSION"]; version != "" {
			cfg.Project.Version = version
			source = "cmake"
		}
	}
	return source
}

// productComponent describes what the SBOM is about. In single-artifact mode
// the root component is the deliverable itself (section 6.1).
func productComponent(cfg config.Config, versionSource string, deliverables []Deliverable) domain.Component {
	name := cfg.Project.Name
	if name == "" && len(deliverables) > 0 {
		name = baseName(deliverables[0].EvidencePath)
	}
	if name == "" {
		name = "product"
	}
	componentType := cfg.Project.Type
	if componentType == "" {
		componentType = "application"
		if len(deliverables) > 0 {
			componentType = cycloneTypeForRole(deliverables[0].Role)
		}
	}
	product := domain.Component{
		ID:       productID,
		Name:     name,
		Version:  cfg.Project.Version,
		Type:     componentType,
		Supplier: cfg.Project.Supplier,
	}
	// A version is worth as much as its source, and the product's source was
	// never recorded: the document carried the string and nothing about where
	// it came from. It reaches evidence.identity from here.
	if product.Version != "" && versionSource != "" {
		product.VersionSource = versionSource
		product.VersionConf = domain.ConfidenceHigh
	}
	if cfg.Project.License != "" {
		product.Licenses = []domain.LicenseFinding{{Expression: cfg.Project.License, Evidence: "curated"}}
	}
	if len(deliverables) > 0 {
		product.Properties = map[string][]string{
			"sbomb:artifact:role": {deliverables[0].Role},
		}
	}
	return product
}

// cycloneTypeForRole maps an artifact role onto a component type (section 19.1).
func cycloneTypeForRole(role string) string {
	switch role {
	case "bootloader", "image", "filesystem":
		return "firmware"
	case "library":
		return "library"
	case "data", "package":
		return "file"
	default:
		return "application"
	}
}

type fileGroup struct {
	component domain.Component
	files     []domain.UsedFile
}

// groupFilesByComponent maps every used file onto exactly one component using
// the priority order of section 19.2, then fills in the CRA fields each
// component needs.
func groupFilesByComponent(resolver *componentResolver, files []domain.UsedFile) ([]fileGroup, []domain.Finding) {
	byComponentID := map[string]*fileGroup{}
	order := []string{}

	for _, file := range files {
		id, name, componentType, scope, detectedBy := resolver.resolve(file)
		group, known := byComponentID[id]
		if !known {
			component := domain.Component{
				ID:         id,
				Name:       name,
				Type:       componentType,
				Scope:      scope,
				DetectedBy: detectedBy,
				Properties: map[string][]string{"sbomb:component:detectedBy": {detectedBy}},
			}
			if strings.HasPrefix(name, "unknown:") {
				component.Properties["sbomb:review:required"] = []string{"true"}
			}
			group = &fileGroup{component: component}
			byComponentID[id] = group
			order = append(order, id)
		}
		group.files = append(group.files, file)
	}

	sort.Strings(order)
	groups := make([]fileGroup, 0, len(order))
	findings := []domain.Finding{}
	for _, id := range order {
		group := byComponentID[id]
		sort.Slice(group.files, func(i, j int) bool {
			return group.files[i].ID.Canonical() < group.files[j].ID.Canonical()
		})
		findings = append(findings, resolver.enrichComponent(&group.component, group.files)...)
		group.component.EnvironmentProvided = environmentProvided(group.component, group.files)
		groups = append(groups, *group)
	}
	// Which packages reached no component at all is only decidable once every
	// file has been mapped, and here the complete set is at hand. The findings
	// join the others before the sort, so their order is the same on every run.
	findings = append(findings, resolver.unusedPackageFindings(files)...)
	sortFindings(findings)
	return groups, findings
}

// environmentProvided decides whether the target expects this component to be
// present rather than carrying it.
//
// Two things have to hold, and system scope alone is only the first. The files
// have to live where the distribution keeps them, and the artifact has to link
// against them dynamically -- a system archive linked statically ends up inside
// the artifact, and calling that "provided by the environment" would be false.
// A shared library among the component's files is the evidence for the second.
func environmentProvided(component domain.Component, files []domain.UsedFile) bool {
	if component.Scope != string(anchors.ScopeSystem) {
		return false
	}
	for _, file := range files {
		if file.Class == domain.FileClassSharedLibrary {
			return true
		}
	}
	return false
}

func scopeForAnchorKind(kind string) string {
	switch kind {
	case "sdk":
		return string(anchors.ScopeSDK)
	default:
		return string(anchors.ScopeThirdParty)
	}
}

func baseName(path string) string {
	path = strings.TrimRight(path, "/\\")
	if index := strings.LastIndexAny(path, "/\\"); index >= 0 {
		return path[index+1:]
	}
	return path
}
