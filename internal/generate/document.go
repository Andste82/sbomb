package generate

import (
	"sort"
	"strings"

	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/sbomwriter"
)

// productID is the identity of the root component inside the document. It is
// not a bom-ref; the writer derives that (section 36.1).
const productID = "product"

// buildDocument assembles the format-neutral document: a root product, the
// grouping components the used files belong to, and the relations between
// them (sections 19 and 28.5).
func buildDocument(
	cfg config.Config,
	deliverables []Deliverable,
	files []domain.UsedFile,
	findings []domain.Finding,
	run sbomwriter.RunMetadata,
) *sbomwriter.Document {
	document := &sbomwriter.Document{
		Product:  productComponent(cfg, deliverables),
		Files:    files,
		Findings: findings,
		Run:      run,
	}

	groups := groupFilesByComponent(cfg, files)
	relations := make([]sbomwriter.Relation, 0, len(groups)+1)
	productTargets := make([]string, 0, len(groups))

	for _, group := range groups {
		document.Components = append(document.Components, group.component)
		productTargets = append(productTargets, group.component.ID)
		relations = append(relations, sbomwriter.Relation{From: group.component.ID, To: group.files})
	}
	sort.Strings(productTargets)
	relations = append(relations, sbomwriter.Relation{From: productID, To: productTargets})
	sort.Slice(relations, func(i, j int) bool { return relations[i].From < relations[j].From })
	document.Relations = relations
	return document
}

// productComponent describes what the SBOM is about. In single-artifact mode
// the root component is the deliverable itself (section 6.1).
func productComponent(cfg config.Config, deliverables []Deliverable) domain.Component {
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
	files     []string
}

// groupFilesByComponent applies strategy 7 of section 19.2: the anchor root
// itself is the component. The earlier strategies -- curated configuration,
// package-manager metadata, submodule boundaries -- refine this later; until
// then every file still belongs to exactly one named component rather than
// floating unattached.
func groupFilesByComponent(cfg config.Config, files []domain.UsedFile) []fileGroup {
	byComponentID := map[string]*fileGroup{}
	order := []string{}

	for _, file := range files {
		anchorKey := string(file.ID.Anchor)
		id, name, componentType, scope := componentForAnchor(cfg, anchorKey, file.ID.RelPath)
		group, known := byComponentID[id]
		if !known {
			component := domain.Component{
				ID:    id,
				Name:  name,
				Type:  componentType,
				Scope: scope,
			}
			if strings.HasPrefix(name, "unknown:") {
				// Section 19.3: an unmapped component is emitted and flagged,
				// never silently dropped.
				component.Properties = map[string][]string{
					"sbomb:component:detectedBy": {"unresolved"},
					"sbomb:review:required":      {"true"},
				}
				component.Licenses = []domain.LicenseFinding{{Name: "NOASSERTION", Reason: "component-unresolved"}}
			}
			group = &fileGroup{component: component}
			byComponentID[id] = group
			order = append(order, id)
		}
		group.files = append(group.files, file.ID.Canonical())
	}

	sort.Strings(order)
	groups := make([]fileGroup, 0, len(order))
	for _, id := range order {
		group := byComponentID[id]
		sort.Strings(group.files)
		groups = append(groups, *group)
	}
	return groups
}

// componentForAnchor names the component a file belongs to, based on its anchor.
func componentForAnchor(cfg config.Config, anchorKey, relPath string) (id, name, componentType, scope string) {
	kind, anchorName, _ := strings.Cut(anchorKey, ":")
	switch kind {
	case "project", "build":
		projectName := cfg.Project.Name
		if projectName == "" {
			projectName = "project"
		}
		// The project's own code and what the build generated from it are one
		// component: the application (section 19.1).
		return "component:project", projectName, "application", string(anchors.ScopeProject)
	case "pkg", "sdk", "extern":
		return anchorKey, anchorName, "library", scopeForAnchorKind(kind)
	case "toolchain":
		return anchorKey, anchorName, "library", string(anchors.ScopeToolchain)
	case "sysroot":
		return anchorKey, anchorName, "library", string(anchors.ScopeSystem)
	default:
		// Section 19.3: unknown:<anchorKey>/<first relative segment>.
		segment := relPath
		if index := strings.IndexByte(segment, '/'); index >= 0 {
			segment = segment[:index]
		}
		unknown := "unknown:" + anchorKey + "/" + segment
		return unknown, unknown, "library", string(anchors.ScopeUnknown)
	}
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
	path = strings.TrimSuffix(path, "/")
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		return path[index+1:]
	}
	return path
}
