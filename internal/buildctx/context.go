package buildctx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Context captures the detected build configuration and generator metadata for a
// CMake build directory. It intentionally stays small and dependency-free so the
// adapter layer can consume it without importing the CLI or config packages.
type Context struct {
	BuildDir  string
	SourceDir string
	Generator string
	Compiler  string
	BuildType string
	Cache     map[string]string
}

// Probe inspects a CMake build directory and extracts generator, compiler, and
// build-type metadata from CMakeCache.txt when present.
func Probe(buildDir string) (*Context, error) {
	if buildDir == "" {
		return nil, errors.New("build dir is required")
	}
	ctx := &Context{
		BuildDir: buildDir,
		Cache:    make(map[string]string),
	}

	cacheFile := filepath.Join(buildDir, "CMakeCache.txt")
	if data, err := os.ReadFile(cacheFile); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			keyParts := strings.SplitN(parts[0], ":", 2)
			if len(keyParts) < 2 {
				continue
			}
			key := keyParts[0]
			value := parts[1]
			ctx.Cache[key] = value
			switch key {
			case "CMAKE_GENERATOR":
				ctx.Generator = value
			case "CMAKE_BUILD_TYPE":
				ctx.BuildType = value
			case "CMAKE_SOURCE_DIR":
				ctx.SourceDir = value
			case "CMAKE_C_COMPILER":
				ctx.Compiler = filepath.Base(value)
			case "CMAKE_CXX_COMPILER":
				if ctx.Compiler == "" {
					ctx.Compiler = filepath.Base(value)
				}
			}
		}
	}

	if ctx.Generator == "" {
		if _, err := os.Stat(filepath.Join(buildDir, "build.ninja")); err == nil {
			ctx.Generator = "Ninja"
		}
	}
	if ctx.BuildType == "" {
		ctx.BuildType = ctx.Cache["CMAKE_BUILD_TYPE"]
	}
	if ctx.Compiler == "" {
		ctx.Compiler = ctx.Cache["CMAKE_C_COMPILER"]
		if ctx.Compiler == "" {
			ctx.Compiler = ctx.Cache["CMAKE_CXX_COMPILER"]
		}
	}
	return ctx, nil
}

// SelectConfig chooses a config name using the same precedence described in the
// CMake File API specification: explicit name, CMake build type, or the single
// discovered config.
func (c *Context) SelectConfig(name string) (string, error) {
	if name != "" {
		return name, nil
	}
	if c.BuildType != "" {
		return c.BuildType, nil
	}
	if c.Cache["CMAKE_BUILD_TYPE"] != "" {
		return c.Cache["CMAKE_BUILD_TYPE"], nil
	}
	return "", errors.New("no build configuration selected")
}
