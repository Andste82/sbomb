package generate

import (
	"fmt"
	"strings"
	"sync"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/license"
)

// copyrightCollector gathers the copyright statements of section 22.10 while
// the files are hashed.
//
// It exists here, in the layer above internal/inventory, for the reason
// section 35 gives: hashing owns the read, and what a copyright notice looks
// like is a licence question. The hashing pass hands over the bytes it has
// already read, so this costs one look at memory per file and no I/O at all
// (decision Q9) -- against a section 31 budget of 15 s for 10 000 files, a
// second pass over the tree would have been the whole feature's cost.
type copyrightCollector struct {
	// Hashing is a bounded worker pool (section 23), so this is written from
	// several goroutines at once.
	mu     sync.Mutex
	byFile map[string][]string
}

func newCopyrightCollector() *copyrightCollector {
	return &copyrightCollector{byFile: map[string][]string{}}
}

// observe is the inventory.HashOptions callback. It is safe for concurrent
// use, as that field requires.
func (c *copyrightCollector) observe(id domain.FileID, data []byte) {
	statements := license.ExtractCopyright(data)
	if len(statements) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byFile[id.Canonical()] = statements
}

// statements is what was found, keyed by canonical file identity. A collector
// that never ran answers with an empty map rather than nil, so a caller needs
// no nil check to ask about a file.
func (c *copyrightCollector) statements() map[string][]string {
	if c == nil {
		return map[string][]string{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byFile
}

// setCopyrightStatements hands the resolver what the hashing pass observed.
// Nothing here reads a file: every statement was taken from bytes that were
// read for their hash, or from bytes section 22.9 retained.
func (r *componentResolver) setCopyrightStatements(byFile map[string][]string) {
	r.copyrights = byFile
}

// resolveComponentCopyright is section 22.10 for one component: the statements
// of its used files and of the artifacts section 22.9 retained, deduplicated,
// bounded, and stored exactly as the files state them.
//
// The sources are those two and nothing else. A holder is never derived from a
// repository URL, a directory name or a package owner: those say who publishes
// the code, and a copyright notice says who holds the rights -- often several
// parties, and rarely the publisher alone. Inventing one would be a legal
// conclusion presented as an observation.
func (r *componentResolver) resolveComponentCopyright(component *domain.Component, files []domain.UsedFile,
	curated config.Component, hasCurated bool) []domain.Finding {
	findings := []domain.Finding{}

	var collected []domain.CopyrightStatement
	for _, file := range files {
		for _, text := range r.copyrights[file.ID.Canonical()] {
			collected = append(collected, domain.CopyrightStatement{Text: text, File: file.ID})
		}
	}
	// The retained artifacts, per section 22.10. For a BSD or MIT dependency
	// this is where the holder actually is: the notice is inside the licence
	// text, and a header file of the same component may carry none.
	for _, artifact := range component.LicenseArtifacts {
		for _, text := range license.ExtractCopyright(artifact.Bytes) {
			collected = append(collected, domain.CopyrightStatement{Text: text, File: artifact.File})
		}
	}

	kept := license.DedupeCopyright(collected)
	if len(kept) > license.MaxCopyrightStatements {
		dropped := len(kept) - license.MaxCopyrightStatements
		// Ordered by text before the cut, so the entries that survive are the
		// same ones whichever order the files were read in.
		kept = kept[:license.MaxCopyrightStatements]
		findings = append(findings, componentFinding("FOSS_COPYRIGHT_LIMIT", domain.SeverityInfo, component,
			fmt.Sprintf("the component states %d distinct copyright statements, and section 22.10 keeps %d: %d were dropped",
				len(kept)+dropped, license.MaxCopyrightStatements, dropped),
			"Review the component's notices by hand; the document carries the first "+
				fmt.Sprint(license.MaxCopyrightStatements)+" by text order."))
	}
	component.Copyrights = kept

	// The conclusion, which is a different claim from the observation and has
	// exactly one authorized source (section 22.4). CycloneDX keeps them in
	// two places for the same reason, and nothing sbomb read is ever promoted
	// into component.copyright.
	if hasCurated && curated.Copyright != "" {
		component.Copyright = strings.TrimSpace(curated.Copyright)
	}

	// A distributed component alone (section 24.5): the notice MIT and the BSD
	// family oblige a distributor to reproduce is owed for what is shipped,
	// and a build-time-only code generator ships nothing. Section 22.10 asked
	// every mapped component until the role was a resolved fact, because
	// narrowing it before then would have meant guessing which components are
	// distributed.
	if component.DistributionRole == domain.RoleDistributed && len(kept) == 0 && component.Copyright == "" {
		findings = append(findings, componentFinding("FOSS_COPYRIGHT_MISSING", domain.SeverityInfo, component,
			"no copyright statement was found in the component's files or in its retained licence artifacts",
			"If the component carries a notice sbomb did not recognize, record it in components[].copyright."))
	}
	return findings
}
