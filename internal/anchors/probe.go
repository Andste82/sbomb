package anchors

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/exec"
	"github.com/example/sbomb/internal/pathmodel"
)

// toolchainProbe is what the three permitted compiler probes of section 9.2
// together say about one compiler.
type toolchainProbe struct {
	// Root is the directory the compiler reports as its own installation, from
	// the "install:" line of -print-search-dirs. It is narrower than the
	// installation prefix the File API reports, deliberately: a system
	// compiler's prefix is /usr, and an anchor over /usr would rename every
	// distribution file in the document.
	Root string
	// LinkDirs are the directories the compiler passes to the linker, from the
	// "libraries:" line. Section 24.1 treats what lies under them as
	// distribution libraries.
	LinkDirs []string
	// Triple and Version name the toolchain, for the anchor key.
	Triple  string
	Version string
}

// versionNumber matches a bare version such as "13.3.0". Only a token of this
// exact shape is taken out of --version output: the rest of that line is prose
// that differs between distributions, and guessing at it would put the
// packaging of one machine into a portable identity.
var versionNumber = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

// probeCompiler asks one compiler the three questions section 9.2 permits. It
// returns false when the compiler did not answer the one question that matters
// -- where it is installed -- because a toolchain anchor without a root is not
// an anchor.
func probeCompiler(ctx context.Context, runner *exec.Runner, compiler string) (toolchainProbe, bool) {
	searchDirs, err := runner.RunCompilerProbe(ctx, compiler, "-print-search-dirs")
	if err != nil {
		return toolchainProbe{}, false
	}
	probe := parseSearchDirs(string(searchDirs))
	if probe.Root == "" || probe.Root == "." {
		return toolchainProbe{}, false
	}
	if triple, tripleErr := runner.RunCompilerProbe(ctx, compiler, "-dumpmachine"); tripleErr == nil {
		probe.Triple = strings.TrimSpace(string(triple))
	}
	if version, versionErr := runner.RunCompilerProbe(ctx, compiler, "--version"); versionErr == nil {
		probe.Version = versionFromBanner(string(version))
	}
	return probe, true
}

// parseSearchDirs reads the -print-search-dirs answer. It reports the
// installation directory and the library directories, and nothing else:
// section 24.4 names an include-directory probe, but -print-search-dirs is not
// it -- the output has no include line at all, which is why the include
// directories stay a matter for the File API.
func parseSearchDirs(output string) toolchainProbe {
	probe := toolchainProbe{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		switch {
		case strings.HasPrefix(line, "install:"):
			probe.Root = filepath.Clean(strings.TrimSpace(strings.TrimPrefix(line, "install:")))
		case strings.HasPrefix(line, "libraries:"):
			probe.LinkDirs = searchPathEntries(strings.TrimPrefix(line, "libraries:"))
		}
	}
	return probe
}

// anchorKey names the toolchain from what it said about itself. The target
// triple and the version number are the compiler's own answers; when it gave
// neither, the key falls back to the directory it lives in, which is still a
// statement rather than an invention.
func (p toolchainProbe) anchorKey() string {
	name := strings.TrimSpace(strings.Trim(p.Triple+"-"+p.Version, "-"))
	if name == "" {
		name = filepath.Base(p.Root)
	}
	return "toolchain:" + pathmodel.Slug(name, 64)
}

// searchPathEntries splits one -print-search-dirs value. GCC writes a leading
// "=" for the sysroot prefix and separates the directories with the platform's
// path separator; the entries carry ".." segments that Clean removes, so that
// two spellings of one directory do not become two anchors.
func searchPathEntries(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "=")
	out := make([]string, 0, 8)
	for _, entry := range strings.Split(value, string(filepath.ListSeparator)) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		out = append(out, filepath.Clean(entry))
	}
	return out
}

// versionFromBanner takes the version number out of the first line of
// --version output, or "" when that line holds nothing of that shape.
func versionFromBanner(banner string) string {
	line, _, _ := strings.Cut(banner, "\n")
	fields := strings.Fields(strings.TrimSpace(line))
	for index := len(fields) - 1; index >= 0; index-- {
		if versionNumber.MatchString(fields[index]) {
			return fields[index]
		}
	}
	return ""
}

// uniqueCompilers fixes the order the compilers are probed in, so that two
// runs over the same build directory register the same anchors.
func uniqueCompilers(compilers []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(compilers))
	for _, compiler := range compilers {
		compiler = strings.TrimSpace(compiler)
		if compiler == "" || seen[compiler] {
			continue
		}
		seen[compiler] = true
		out = append(out, compiler)
	}
	sort.Strings(out)
	return out
}
