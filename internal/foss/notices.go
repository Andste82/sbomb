package foss

import (
	"fmt"
	"path"
	"strings"

	"github.com/example/sbomb/internal/domain"
)

// The two incompleteness markers of requirement R10. They are where the entry
// would have been, and they are the same string in every document sbomb
// writes, so that a reviewer can grep for them.
const (
	missingTextMarker      = "[licence text not found in component - attribution incomplete]"
	missingCopyrightMarker = "[no copyright statement found in component - attribution incomplete]"
)

// renderNotices produces THIRD-PARTY-NOTICES.txt: the one document of the four
// that ships with the product.
//
// It adds no path of its own: licence text, copyright lines, an origin URL and
// component names (decision Q8), and a test asserts that over the corpus.
// Where component mapping failed, an unmapped component's section 19.3 name
// embeds the directory the evidence named -- and --redact-unanchored-paths
// reaches it there, because it was redacted when the identity was formed
// rather than when it was printed.
//
// A waiver never changes it (section 26.3). The markers below stay where they
// are regardless of what anybody decided about the finding that reported them:
// this document says what sbomb saw.
func renderNotices(m model, s style) string {
	var b strings.Builder
	s.title(&b, "THIRD-PARTY NOTICES")
	b.WriteString("\n")
	s.prose(&b, "", fmt.Sprintf(
		"This document lists the third-party components contained in %s and reproduces "+
			"the licence and copyright notices they carry. Every component below is one "+
			"whose files reach what is distributed; a component that only helped build the "+
			"product is not in it.", productName(m)))
	b.WriteString("\n")
	s.prose(&b, "", "sbomb produces data; the manufacturer performs the assessment. Where a "+
		"licence text or a copyright line could not be established, this document says so "+
		"in place of the entry rather than substituting a canonical text.")

	if len(m.distributed) == 0 {
		s.section(&b, "COMPONENTS")
		s.prose(&b, "", "No third-party component is contained in what is distributed.")
		return b.String()
	}

	for _, candidate := range m.distributed {
		b.WriteString("\n")
		s.componentHeading(&b, heading(candidate))
		if candidate.origin != "" {
			s.plain(&b, "Origin", candidate.origin)
		}
		s.plain(&b, "Licence", licenseLine(candidate))
		b.WriteString("\n")
		renderCopyright(&b, s, candidate)
		renderTexts(&b, s, m, candidate)
	}
	return b.String()
}

func productName(m model) string {
	if m.version == "" {
		return m.product
	}
	return m.product + " " + m.version
}

func heading(candidate entry) string {
	if candidate.version == "" {
		return candidate.name
	}
	return candidate.name + " " + candidate.version
}

// licenseLine states the licence, or that none was established. "NOASSERTION"
// is what the document says (section 22.7); this file is read by people, so it
// says it in words as well.
func licenseLine(candidate entry) string {
	if candidate.license == "" {
		return "not established (NOASSERTION)"
	}
	return candidate.license
}

// renderCopyright reproduces the statements verbatim, one per line. Nothing is
// reformatted: a rewritten year range and a normalized holder are both
// alterations of a notice the licence says must be reproduced (requirement R3).
func renderCopyright(b *strings.Builder, s style, candidate entry) {
	if len(candidate.copyrights) == 0 {
		s.prose(b, "", missingCopyrightMarker)
		return
	}
	for _, statement := range candidate.copyrights {
		fmt.Fprintf(b, "%s\n", statement.Text)
	}
}

// renderTexts reproduces the component's retained files.
//
// A text several components share is printed once, under the first component
// in document order that carries it, with the others named beside it; the
// later components point at it by name and digest (decision Q13). No holder is
// lost by this: two MIT texts naming different holders have different digests,
// which is precisely why the component's own file is retained instead of a
// canonical one.
func renderTexts(b *strings.Builder, s style, m model, candidate entry) {
	if len(candidate.grants()) == 0 {
		s.marker(b, missingTextMarker)
	}
	for _, artifact := range candidate.artifacts {
		label := path.Base(artifact.File.RelPath)
		shared := m.texts[artifact.SHA256]
		if shared != nil && shared.owner != candidate.name {
			s.verbatim(b, label, fmt.Sprintf(
				"[the text is byte-identical to %s reproduced under %q above; sha256:%s]",
				shared.label, shared.owner, shortDigest(artifact.SHA256)), nil)
			continue
		}
		note := ""
		if shared != nil && len(shared.carriers) > 1 {
			note = "[also the licence text of: " + strings.Join(others(shared.carriers, candidate.name), ", ") + "]"
		}
		s.verbatim(b, label, note, artifact.Bytes)
	}
}

// others is the carriers of a shared text apart from the one printing it.
func others(carriers []string, self string) []string {
	out := make([]string, 0, len(carriers))
	for _, name := range carriers {
		if name == self {
			continue
		}
		out = append(out, name)
	}
	return out
}

// kindLabel names a retained artifact's kind in prose, for the review record.
func kindLabel(artifact domain.LicenseArtifact) string {
	switch artifact.Kind {
	case domain.LicenseArtifactLicense:
		return "licence"
	case domain.LicenseArtifactNotice:
		return "notice"
	case domain.LicenseArtifactCopyright:
		return "copyright"
	default:
		return artifact.Kind
	}
}
