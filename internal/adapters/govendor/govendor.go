// Package govendor reads vendor/modules.txt, the record the go command writes
// when it copies a module's sources into the repository.
//
// It is a secondary source and is treated as one. The binary says which
// modules were linked and at which versions; this says which sources are on
// disk and where. The caller uses the second only where it agrees with the
// first, because a vendor directory left over from another commit would
// otherwise donate the wrong licence to the right component.
package govendor

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/example/sbomb/internal/limits"
)

// Module is one entry of modules.txt.
type Module struct {
	Path    string
	Version string
	// ReplacementPath and ReplacementVersion carry a replace directive. The
	// path may be a filesystem path rather than a module path, which is what
	// a local replacement looks like.
	ReplacementPath    string
	ReplacementVersion string
	// Explicit marks a module required directly by the main module rather than
	// pulled in transitively.
	Explicit bool
	// Packages are the import paths whose sources were actually vendored. The
	// go command copies only the packages that are reached, not whole modules.
	Packages []string
}

// Dir is where this module's sources sit below the vendor directory.
func (m Module) Dir(vendorDir string) string {
	return filepath.Join(vendorDir, filepath.FromSlash(m.Path))
}

// Parse reads modules.txt from a module root. A root with no vendor directory
// is not an error: it returns nil, nil, and the caller carries on without the
// licence evidence a vendor tree would have supplied.
func Parse(moduleDir string, bounds limits.Config) ([]Module, error) {
	path := filepath.Join(moduleDir, "vendor", "modules.txt")
	if _, err := bounds.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	file, err := bounds.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var (
		modules []Module
		current *Module
	)
	scanner := limits.Scanner(file)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "## "):
			// Annotations belong to the module heading above them. "explicit"
			// is the only one that says anything an SBOM cares about; the
			// others record the language version and are ignored on purpose.
			if current != nil {
				for _, annotation := range strings.Split(strings.TrimPrefix(line, "## "), ";") {
					if strings.TrimSpace(annotation) == "explicit" {
						current.Explicit = true
					}
				}
			}
		case strings.HasPrefix(line, "# "):
			module, ok := parseHeading(strings.TrimPrefix(line, "# "))
			if !ok {
				continue
			}
			modules = append(modules, module)
			current = &modules[len(modules)-1]
		default:
			if current != nil {
				current.Packages = append(current.Packages, strings.TrimSpace(line))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return modules, nil
}

// parseHeading reads "path version" and the optional "=> path version" that a
// replace directive adds.
func parseHeading(text string) (Module, bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return Module{}, false
	}
	module := Module{Path: fields[0]}
	rest := fields[1:]
	if len(rest) > 0 && rest[0] != "=>" {
		module.Version = rest[0]
		rest = rest[1:]
	}
	if len(rest) > 0 && rest[0] == "=>" {
		rest = rest[1:]
		if len(rest) > 0 {
			module.ReplacementPath = rest[0]
			rest = rest[1:]
		}
		if len(rest) > 0 {
			// A replacement by filesystem path carries no version, which is
			// why this is conditional rather than positional.
			module.ReplacementVersion = rest[0]
		}
	}
	return module, module.Path != ""
}

// Index maps module path to entry, for the agreement check the caller makes
// against the binary.
func Index(modules []Module) map[string]Module {
	index := make(map[string]Module, len(modules))
	for _, module := range modules {
		index[module.Path] = module
	}
	return index
}
