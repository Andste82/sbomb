package spdx3

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/example/sbomb/internal/licenselist"
	"github.com/example/sbomb/internal/sbommap"
	"github.com/example/sbomb/internal/spdx/mapping"
)

// CheckOwnOutput holds a document sbomb wrote to the invariants of how it
// writes (section 28.11.9, tier b), after Validate has found it conformant.
// None of these is a rule of the specification; each is a promise sbomb makes
// about its own documents, and a document that breaks one is a defect in the
// writer, however valid it is:
//
//   - the document is self-contained: every reference resolves inside it, to
//     one creation information, and every element is listed in the
//     SpdxDocument and reachable from its root by following what the
//     relationships say;
//   - every package and file has a name that is not white space alone;
//   - NoneElement and NoAssertionElement stand alone in a relationship's to;
//   - licences are stated the way the mapping states them, every identifier
//     is on the list or a LicenseRef, every exception on the list or an
//     AdditionRef, every reference sbomb minted is defined, operators are
//     upper case, and the list version is stated where it matters;
//   - the profiles claimed are exactly the profiles used;
//   - every property is in the sbomb namespace, and every package URL is one;
//   - relationships are numbered without gaps.
func CheckOwnOutput(data []byte) error {
	parsed, err := parseDocument(data)
	if err != nil {
		return err
	}
	found := &issues{}
	check := &ownOutput{parsed: parsed, found: found}
	check.shape()
	check.names()
	check.creationInfo()
	check.references()
	check.collections()
	check.closure()
	check.licences()
	check.profiles()
	check.properties()
	check.numbering()
	return found.err("the SPDX document breaks an invariant of sbomb's own output")
}

type ownOutput struct {
	parsed *parsedDocument
	found  *issues
}

func (c *ownOutput) elements() []map[string]any {
	var out []map[string]any
	for _, node := range c.parsed.nodes {
		if isElementClass(nodeType(node)) {
			out = append(out, node)
		}
	}
	return out
}

func (c *ownOutput) ofType(class string) []map[string]any {
	var out []map[string]any
	for _, node := range c.parsed.nodes {
		if nodeType(node) == class {
			out = append(out, node)
		}
	}
	return out
}

func (c *ownOutput) shape() {
	if _, hasGraph := c.parsed.root["@graph"]; !hasGraph || len(c.parsed.root) != 2 {
		c.found.addf("the document is not exactly @context and @graph")
	}
}

// names holds a package or a file to a name that says something. Validate
// accepts a name of white space, which the specification allows; sbomb names
// a package after its component and a file after its canonical path, and
// neither is ever blank.
func (c *ownOutput) names() {
	for _, node := range c.parsed.nodes {
		class := nodeType(node)
		for _, named := range namedClasses {
			if isA(class, named) && stringValue(node, "name") != "" && strings.TrimSpace(stringValue(node, "name")) == "" {
				c.found.addf("%s has a name of white space only; sbomb names every package and file", describe(node))
				break
			}
		}
	}
}

var createdPattern = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$`)

func (c *ownOutput) creationInfo() {
	infos := c.ofType("CreationInfo")
	if len(infos) != 1 || nodeID(infos[0]) != creationInfoID {
		c.found.addf("the document must have exactly one creation information, %s", creationInfoID)
		return
	}
	info := infos[0]
	if !createdPattern.MatchString(stringValue(info, "created")) {
		c.found.addf("created %q is not YYYY-MM-DDThh:mm:ssZ", stringValue(info, "created"))
	}
	if createdBy, _ := stringList(info, "createdBy"); len(createdBy) == 0 {
		c.found.addf("the creation information names nobody in createdBy")
	}
	for _, element := range c.elements() {
		if value, isString := element["creationInfo"].(string); !isString || value != creationInfoID {
			c.found.addf("%s does not refer to %s", describe(element), creationInfoID)
		}
	}
}

// referenceKeys are the keys whose values name another node.
var referenceKeys = []string{"createdBy", "createdUsing", "element", "from", "originatedBy", "rootElement", "suppliedBy", "to"}

func (c *ownOutput) references() {
	for _, node := range c.parsed.nodes {
		for _, key := range referenceKeys {
			values, _ := stringList(node, key)
			for _, value := range values {
				if _, resolves := c.parsed.byID[value]; !resolves && !predefinedIndividuals[value] {
					c.found.addf("%s names %s in %s, which the document does not define", describe(node), value, key)
				}
			}
		}
		// sbomb writes NoneElement and NoAssertionElement only as the one
		// target of a relationship: "there are none" and "nobody asserts
		// which" are statements about the whole set. Validate refuses
		// NoneElement beside other elements, which the Relationship class
		// calls not valid; NoAssertionElement beside them is sbomb's own
		// rule, and explain reads a known leaf as exactly one NoneElement.
		if targets, _ := stringList(node, "to"); len(targets) > 1 {
			for _, individual := range []string{noneElement, noAssertionElem} {
				if slices.Contains(targets, individual) {
					c.found.addf("%s names %s beside other elements in to; sbomb writes it only alone", describe(node), individual)
				}
			}
		}
		for _, entry := range dictionaryEntries(node) {
			if _, resolves := c.parsed.byID[entry.Value]; !resolves {
				c.found.addf("%s maps %s to %s, which the document does not define", describe(node), entry.Key, entry.Value)
			}
		}
	}
}

func dictionaryEntries(node map[string]any) []dictionaryEntry {
	items, _ := node["simplelicensing_customIdToUri"].([]any)
	var out []dictionaryEntry
	for _, item := range items {
		object, _ := item.(map[string]any)
		out = append(out, dictionaryEntry{Key: stringValue(object, "key"), Value: stringValue(object, "value")})
	}
	return out
}

func (c *ownOutput) collections() {
	documents := c.ofType("SpdxDocument")
	sboms := c.ofType("software_Sbom")
	if len(documents) != 1 || len(sboms) != 1 {
		c.found.addf("the document must have exactly one SpdxDocument and one software_Sbom, not %d and %d", len(documents), len(sboms))
		return
	}
	documentID := nodeID(documents[0])
	sbomID := nodeID(sboms[0])
	var want []string
	for _, element := range c.elements() {
		if id := nodeID(element); id != documentID {
			want = append(want, id)
		}
	}
	listed, _ := stringList(documents[0], "element")
	if strings.Join(listed, "\n") != strings.Join(want, "\n") {
		c.found.addf("the SpdxDocument must list every other element exactly once, in graph order")
	}
	var wantSbom []string
	for _, id := range want {
		if id != sbomID {
			wantSbom = append(wantSbom, id)
		}
	}
	listedSbom, _ := stringList(sboms[0], "element")
	if strings.Join(listedSbom, "\n") != strings.Join(wantSbom, "\n") {
		c.found.addf("the software_Sbom must list every element but itself and the SpdxDocument, in graph order")
	}
	if roots, _ := stringList(documents[0], "rootElement"); len(roots) != 1 || roots[0] != sbomID {
		c.found.addf("the SpdxDocument's root must be the software_Sbom")
	}
	if roots, _ := stringList(sboms[0], "rootElement"); len(roots) != 1 {
		c.found.addf("the software_Sbom must have exactly one root, the product")
	}
}

// closure walks the document from its root, along what each statement says,
// in the direction it says it: the SpdxDocument to the Sbom to the product,
// every relationship from its source to its targets, an element to the agents
// that supplied or originated it, the creation information to its creator and
// tool, and a licence expression to the definitions of the references it
// uses. A VEX statement is about the element it names, so it -- and the
// vulnerability it is from -- is reached from that element: the one reverse
// step, because the statement's subject is its target. The Sbom's element
// list is not followed; it lists everything, and following it would make the
// walk prove nothing. Every element must be reached.
func (c *ownOutput) closure() {
	documents := c.ofType("SpdxDocument")
	if len(documents) != 1 {
		return
	}
	outgoing := map[string][]map[string]any{}
	about := map[string][]map[string]any{}
	for _, node := range c.parsed.nodes {
		class := nodeType(node)
		if !isA(class, "Relationship") {
			continue
		}
		if class == classVexNotAffected {
			targets, _ := stringList(node, "to")
			for _, target := range targets {
				about[target] = append(about[target], node)
			}
			continue
		}
		from := stringValue(node, "from")
		outgoing[from] = append(outgoing[from], node)
	}
	reached := map[string]bool{}
	queue := []string{nodeID(documents[0])}
	visit := func(id string) {
		if id != "" && !reached[id] {
			reached[id] = true
			queue = append(queue, id)
		}
	}
	reached[queue[0]] = true
	for _, info := range c.ofType("CreationInfo") {
		for _, key := range []string{"createdBy", "createdUsing"} {
			values, _ := stringList(info, key)
			for _, value := range values {
				visit(value)
			}
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		node := c.parsed.byID[id]
		if node == nil {
			continue
		}
		switch nodeType(node) {
		case "SpdxDocument", "software_Sbom":
			roots, _ := stringList(node, "rootElement")
			for _, root := range roots {
				visit(root)
			}
		}
		for _, key := range []string{"suppliedBy", "originatedBy"} {
			values, _ := stringList(node, key)
			for _, value := range values {
				visit(value)
			}
		}
		for _, entry := range dictionaryEntries(node) {
			visit(entry.Value)
		}
		if isA(nodeType(node), "Relationship") {
			targets, _ := stringList(node, "to")
			for _, target := range targets {
				visit(target)
			}
			visit(stringValue(node, "from"))
		}
		for _, relationship := range outgoing[id] {
			visit(nodeID(relationship))
		}
		for _, statement := range about[id] {
			visit(nodeID(statement))
		}
	}
	for _, element := range c.elements() {
		if !reached[nodeID(element)] {
			c.found.addf("%s cannot be reached from the document's root", describe(element))
		}
	}
}

func (c *ownOutput) licences() {
	defined := map[string]bool{}
	for _, text := range c.ofType("simplelicensing_SimpleLicensingText") {
		defined[nodeID(text)] = true
	}
	for _, node := range c.parsed.nodes {
		class := nodeType(node)
		relationshipType := stringValue(node, "relationshipType")
		switch relationshipType {
		case "hasConcludedLicense", "hasDeclaredLicense":
			if from := c.parsed.byID[stringValue(node, "from")]; from == nil ||
				(nodeType(from) != "software_Package" && nodeType(from) != "software_File") {
				c.found.addf("%s states a licence of something that is not a package or a file", describe(node))
			}
			targets, _ := stringList(node, "to")
			for _, target := range targets {
				if target == noAssertionLic || target == noneLic {
					continue
				}
				if to := c.parsed.byID[target]; to == nil || nodeType(to) != "simplelicensing_LicenseExpression" {
					c.found.addf("%s points a licence relationship at %s, which is not a licence expression", describe(node), target)
				}
			}
		case "doesNotAffect":
			if class != classVexNotAffected {
				c.found.addf("%s uses doesNotAffect outside a VEX not-affected assessment", describe(node))
			}
			if from := c.parsed.byID[stringValue(node, "from")]; from == nil || nodeType(from) != "security_Vulnerability" {
				c.found.addf("%s is a VEX statement that is not from a vulnerability", describe(node))
			}
		}
		if class != "simplelicensing_LicenseExpression" {
			continue
		}
		expression := stringValue(node, "simplelicensing_licenseExpression")
		parsed, err := mapping.ParseExpression(expression)
		if err != nil {
			continue // tier a reports it
		}
		if parsed.LowerCaseOperators {
			// The grammar allows it; sbomb writes one spelling, so that one
			// licence is one expression element.
			c.found.addf("%s writes the operators of %q in lower case; sbomb writes them upper case", describe(node), expression)
		}
		mapped := map[string]string{}
		for _, entry := range dictionaryEntries(node) {
			mapped[entry.Key] = entry.Value
		}
		listed := false
		for _, id := range parsed.Licenses {
			switch {
			case licenselist.Known(id):
				listed = true
			case mapping.IsLicenseRef(id):
				if mapping.IsSbombLicenseRef(id) && !defined[mapped[id]] {
					c.found.addf("%s uses %s, which no licence text of the document defines", describe(node), id)
				}
			case canonicalOf(licenselist.Canonical, id) != "":
				// A reader matches it without case (annex B), but sbomb
				// writes the list's spelling (deviation D55).
				c.found.addf("%s writes %q, which the SPDX licence list spells %q", describe(node), id, canonicalOf(licenselist.Canonical, id))
			default:
				c.found.addf("%s names %s, which is neither on the SPDX licence list %s nor a LicenseRef", describe(node), id, licenselist.Version)
			}
		}
		for _, id := range parsed.Additions {
			switch {
			case licenselist.KnownException(id):
				listed = true
			case mapping.IsAdditionRef(id):
				if mapping.IsSbombAdditionRef(id) && !defined[mapped[id]] {
					c.found.addf("%s uses %s, which no licence text of the document defines", describe(node), id)
				}
			case canonicalOf(licenselist.CanonicalException, id) != "":
				c.found.addf("%s writes %q, which the SPDX licence list spells %q", describe(node), id, canonicalOf(licenselist.CanonicalException, id))
			default:
				c.found.addf("%s names the exception %s, which is not on the SPDX licence list %s", describe(node), id, licenselist.Version)
			}
		}
		if version := stringValue(node, "simplelicensing_licenseListVersion"); listed && version != licenselist.Version {
			c.found.addf("%s names listed licences but states licence list version %q, not %s", describe(node), version, licenselist.Version)
		}
	}
}

// canonicalOf is the list's spelling of an identifier the list carries in
// another case, or "" when it does not carry it at all.
func canonicalOf(lookup func(string) (string, bool), id string) string {
	canonical, ok := lookup(id)
	if !ok {
		return ""
	}
	return canonical
}

// profiles holds profileConformance to exactly what the document uses (the
// extension profile excepted, deviation D49b): a claim beyond what is used is
// as wrong as one short of it.
func (c *ownOutput) profiles() {
	documents := c.ofType("SpdxDocument")
	if len(documents) != 1 {
		return
	}
	want := map[string]bool{"core": true, "software": true}
	for _, node := range c.parsed.nodes {
		switch profile := profileOfName(nodeType(node)); profile {
		case "simpleLicensing", "security":
			want[profile] = true
		}
		for _, key := range []string{"from", "to"} {
			values, _ := stringList(node, key)
			for _, value := range values {
				if value == noAssertionLic || value == noneLic {
					want["expandedLicensing"] = true
				}
			}
		}
	}
	claimed, _ := stringList(documents[0], "profileConformance")
	if strings.Join(claimed, ",") != strings.Join(sortedKeys(want), ",") {
		c.found.addf("profileConformance is %v; the document uses exactly %v", claimed, sortedKeys(want))
	}
	// The computed set above is what the renderer decides to claim; this is
	// what the bytes actually use, key by key, so that a class or property
	// the renderer starts writing without updating the computation is caught
	// as well. The specification does not require it (see conformance);
	// sbomb promises it for its own documents.
	isClaimed := map[string]bool{}
	for _, profile := range claimed {
		isClaimed[profile] = true
	}
	for _, profile := range sortedKeys(usedProfiles(c.parsed)) {
		// The extension profile cannot be claimed in a form both the schema
		// and the model accept (deviation D49b).
		if profile == "extension" || isClaimed[profile] {
			continue
		}
		c.found.addf("the document uses the %s profile but %s does not claim it in profileConformance", profile, describe(documents[0]))
	}
}

func (c *ownOutput) properties() {
	for _, node := range c.parsed.nodes {
		if purl, ok := node["software_packageUrl"].(string); ok && !strings.HasPrefix(purl, "pkg:") {
			c.found.addf("%s has a package URL %q that does not start with pkg:", describe(node), purl)
		}
		walkObjects(node, func(object map[string]any) {
			if nodeType(object) != "extension_CdxPropertyEntry" {
				return
			}
			if err := sbommap.ValidatePropertyName(stringValue(object, "extension_cdxPropName")); err != nil {
				c.found.addf("%s: %v", describe(node), err)
			}
		})
	}
}

func (c *ownOutput) numbering() {
	next := 1
	for _, node := range c.parsed.nodes {
		if !isA(nodeType(node), "Relationship") {
			continue
		}
		want := "relationship:" + strconv.Itoa(next)
		if local := localOf(nodeID(node)); local != want {
			c.found.addf("%s is numbered out of order; the next relationship is %s", describe(node), want)
		}
		next++
	}
}
