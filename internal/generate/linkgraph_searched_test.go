package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/adapters/linkers/mapparser"
	"github.com/example/sbomb/internal/anchors"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/evidence"
	"github.com/example/sbomb/internal/pathmodel"
)

// TestALinkCommandNamesASearchedLibraryAsTheMapWould: with no map, the inputs
// of a link come from the command line the build system recorded (section
// 11.2, priority 5). link.exe takes the C runtime and the import libraries of
// the system DLLs there by bare name, as its map names them, and finds them on
// the LIB search path. Under the Windows flavor such a name that is not a file
// in the build directory must stay unanchored, exactly as it does when a map
// names it: the command-line fallback used to join every bare name onto the
// build root, which reported kernel32.lib as the project's own build output.
// A project library that does lie in the build directory is still build output,
// and under the POSIX flavor a bare name on a link command is a file the build
// directory holds -- a library searched for is spelled -l there.
func TestALinkCommandNamesASearchedLibraryAsTheMapWould(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flavor pathmodel.Flavor
		want   map[string]string
	}{
		{"windows", pathmodel.WindowsFlavor{}, map[string]string{
			"main.obj":     "build:main.obj",
			"project.lib":  "build:project.lib",
			"kernel32.lib": "abs:kernel32.lib",
			"MSVCRTD.lib":  "abs:MSVCRTD.lib",
		}},
		{"posix", pathmodel.PosixFlavor{}, map[string]string{
			"main.o":       "build:main.o",
			"project.lib":  "build:project.lib",
			"kernel32.lib": "build:kernel32.lib",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"main.obj", "main.o", "project.lib"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			registry, err := anchors.Assemble(anchors.Options{Flavor: tc.flavor, ProjectRoot: filepath.Join(dir, "src"), BuildRoot: dir})
			if err != nil {
				t.Fatal(err)
			}
			b := newBuilder(evidence.New(), registry, dir, dir, NewLogger(0, nil))
			command := make([]string, 0, len(tc.want))
			for input := range tc.want {
				command = append(command, input)
			}
			b.recordReconstructedLink("app.exe", command)

			inputs := b.collectLinkEvidence(Deliverable{Path: filepath.Join(dir, "app.exe"), EvidencePath: "app.exe"}, dir, "", "")
			if len(inputs) != len(tc.want) {
				t.Fatalf("inputs = %+v, want one per command-line input", inputs)
			}
			for _, input := range inputs {
				got, _ := b.identifyLinkInput(input.Path, input.Searched)
				if want := tc.want[input.Path]; got != want {
					t.Errorf("%s is %q, want %q", input.Path, got, want)
				}
			}
		})
	}
}

// TestABareLibraryNameIsTheArchiveTheBuildProduced: link.exe's map names a
// library by bare name whether it came from the build or from the LIB search
// path, and the map does not record which directory it was found in. The
// build graph does: an archive the build produced under that name is that
// archive, in whatever subdirectory, and a project library handed over with
// /LIBPATH must not be taken for a system library. Only the top of the build
// directory used to be looked at, and case-sensitively, so a library built in
// a subdirectory, or spelled FOO.lib by the map while the build wrote foo.lib,
// became an unanchored system library and its members lost their objects.
// The answers must not depend on the host: a case-insensitive file system
// would otherwise keep the map's spelling where Linux keeps the disk's.
func TestABareLibraryNameIsTheArchiveTheBuildProduced(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Top.lib"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := anchors.Assemble(anchors.Options{Flavor: pathmodel.WindowsFlavor{}, ProjectRoot: filepath.Join(dir, "src"), BuildRoot: dir})
	if err != nil {
		t.Fatal(err)
	}
	b := newBuilder(evidence.New(), registry, dir, dir, NewLogger(0, nil))
	b.archiveInputs["build:sub/foo.lib"] = []string{"sub/foo.obj"}
	b.archiveInputs["build:one/dup.lib"] = []string{"one/dup.obj"}
	b.archiveInputs["build:two/dup.lib"] = []string{"two/dup.obj"}

	for _, tc := range []struct {
		name, path string
		searched   bool
	}{
		{"foo.lib", "sub/foo.lib", false},
		{"FOO.LIB", "sub/foo.lib", false},
		{"top.lib", "Top.lib", false},
		{"kernel32.lib", "kernel32.lib", true},
		// Two produced archives of one name cannot be told apart from it, so
		// neither is claimed; with no file of that name at the top of the
		// build directory, the name is left to the search path.
		{"dup.lib", "dup.lib", true},
		// A name with a directory in it is a path the evidence gives.
		{`sub\foo.lib`, `sub\foo.lib`, false},
	} {
		path, searched := b.libraryLocation(mapparser.FormatMSVC, tc.name)
		if path != tc.path || searched != tc.searched {
			t.Errorf("%s: path %q searched %v, want %q %v", tc.name, path, searched, tc.path, tc.searched)
		}
	}
}

// TestAWindowsBuildDirectoryIsMatchedWithoutRegardToCase: on the machine that
// built, MSBuild's tracking logs name the build directory in capitals while
// filepath.Abs spells it as it is on disk. Under the Windows flavor the two are
// one directory, and a file below it is build output, not an unanchored path.
// Under the POSIX flavor case still tells two directories apart.
func TestAWindowsBuildDirectoryIsMatchedWithoutRegardToCase(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name   string
		flavor pathmodel.Flavor
		want   string
	}{
		{"windows", pathmodel.WindowsFlavor{}, "/logical/DEBUG/CRYPTO.LIB"},
		{"posix", pathmodel.PosixFlavor{}, strings.ToUpper(pathmodel.NormalizeSeparators(dir)) + "/DEBUG/CRYPTO.LIB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry, err := anchors.Assemble(anchors.Options{Flavor: tc.flavor, ProjectRoot: "/src", BuildRoot: "/logical"})
			if err != nil {
				t.Fatal(err)
			}
			b := newBuilder(evidence.New(), registry, "/logical", dir, NewLogger(0, nil))
			shouted := strings.ToUpper(pathmodel.NormalizeSeparators(dir)) + "/DEBUG/CRYPTO.LIB"
			if got := pathmodel.NormalizeSeparators(b.logicalFor(shouted)); got != tc.want {
				t.Errorf("logicalFor(%q) = %q, want %q", shouted, got, tc.want)
			}
		})
	}
}

// TestAnArchiveOrObjectIsKnownByItsExtensionInAnyCase: MSBuild and NMake spell
// file names in capitals, and FOO.LIB is an archive as much as foo.lib is. A
// case-sensitive check took it for a plain link input, so its members were
// never traced back to the objects they came from.
func TestAnArchiveOrObjectIsKnownByItsExtensionInAnyCase(t *testing.T) {
	for _, path := range []string{"foo.lib", "FOO.LIB", "libfoo.a", "LIBFOO.A"} {
		if !isArchivePath(path) || kindForPath(path) != domain.NodeArchive {
			t.Errorf("%s is not taken for an archive", path)
		}
	}
	for _, path := range []string{"main.obj", "MAIN.OBJ", "main.o", "MAIN.O"} {
		if !isObjectPath(path) {
			t.Errorf("%s is not taken for an object", path)
		}
	}
	for _, path := range []string{"main.c", "a.library", "o"} {
		if isArchivePath(path) || isObjectPath(path) {
			t.Errorf("%s is taken for an archive or an object", path)
		}
	}
}
