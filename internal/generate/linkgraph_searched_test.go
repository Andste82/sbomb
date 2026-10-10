package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/sbomb/internal/anchors"
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
