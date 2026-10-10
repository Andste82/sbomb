package main

import (
	"github.com/example/sbomb/internal/sbomwriter"
)

// selection is the writer a run renders with and the version it writes,
// settled once before discovery.
type selection struct {
	writer  sbomwriter.Writer
	version string
}

// resolveOutput settles the format and version a run writes and asks the
// writer whether it can honour the request at all, before anything is
// discovered.
//
// It is the one place generate, foss and self turn "what the configuration
// and the flags say" into a writer. Asking only the registry would accept a
// request that fails at the very end -- a TLP the chosen version cannot carry,
// say -- after a full discovery run; asking the writer here makes that a usage
// error (exit 1), which is what it is. An empty format is the default format,
// and an empty version the writer's default.
func resolveOutput(format, specVersion string, options sbomwriter.Options) (selection, error) {
	if format == "" {
		format = sbomwriter.DefaultFormat
	}
	writer, version, err := sbomwriter.Resolve(format, specVersion)
	if err != nil {
		return selection{}, err
	}
	if preflighter, checks := writer.(sbomwriter.Preflighter); checks {
		options.SpecVersion = version
		// A refusal that carries an appendix A finding prints as
		// "ID: message", which is RefusalError's own rendering.
		if err := preflighter.Preflight(version, options); err != nil {
			return selection{}, err
		}
	}
	return selection{writer: writer, version: version}, nil
}

// formatLabel names a format the way a person writes it, rather than by the
// identifier the registry keys on. A writer that does not say is named by its
// identifier.
func formatLabel(writer sbomwriter.Writer) string {
	if describer, describes := writer.(sbomwriter.Describer); describes {
		return describer.Label()
	}
	return writer.ID()
}

// outputExtension is the file-name suffix the default output name of a format
// takes. A writer that does not say gets ".json", which is at least true of
// every format this build registers.
func outputExtension(writer sbomwriter.Writer) string {
	if describer, describes := writer.(sbomwriter.Describer); describes {
		return describer.Extension()
	}
	return ".json"
}
