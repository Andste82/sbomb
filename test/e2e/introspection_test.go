//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNinjaIntrospectionFillsWhatTheBuildGraphHides covers the two ninja
// fallbacks of section 9.2 against a real ninja, because that is the only way
// to show they add something.
//
// The project puts the archive rule behind a subninja. sbomb's own build.ninja
// parser accepts subninja but does not follow it, so without introspection the
// objects behind the archive are unknown and the member stays unresolved --
// exactly the gap `ninja -t inputs` closes. There is no compile database
// either, so `ninja -t commands` is the only source for the compile lines.
func TestNinjaIntrospectionFillsWhatTheBuildGraphHides(t *testing.T) {
	for _, tool := range []string{"ninja", "cc", "ar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(mustThisFile(t)), "..", ".."))
	build := t.TempDir()

	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(build, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("main.c", "int helper(void);\nint main(void){return helper();}\n")
	write("a.c", "int helper(void){return 0;}\n")
	write("build.ninja", "rule cc\n  command = cc -c $in -o $out\n"+
		"rule link\n  command = cc $in -o $out -Wl,-Map=$out.map\n\n"+
		"subninja sub.ninja\n\n"+
		"build main.o: cc main.c\n"+
		"build app: link main.o libx.a\n")
	write("sub.ninja", "rule ar\n  command = ar rcs $out $in\n\n"+
		"build a.o: cc a.c\n"+
		"build libx.a: ar a.o\n")
	write("sbomb.json", `{"project": {"name": "na", "root": "`+build+`"}, "artifacts": [{"path": "app"}]}`)

	if output, err := exec.Command("ninja", "-C", build).CombinedOutput(); err != nil {
		t.Fatalf("ninja: %v\n%s", err, output)
	}

	sbomb := filepath.Join(t.TempDir(), "sbomb")
	buildTool := exec.Command("go", "build", "-o", sbomb, "./cmd/sbomb")
	buildTool.Dir = root
	if output, err := buildTool.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}

	generate := func(extra ...string) (files []string, findings []string) {
		t.Helper()
		dir := t.TempDir()
		output := filepath.Join(dir, "out.cdx.json")
		findingsPath := filepath.Join(dir, "findings.json")
		args := append([]string{"generate", "--build-dir", build,
			"--config", filepath.Join(build, "sbomb.json"), "--policy", "lenient",
			"--output", output, "--findings-json", findingsPath, "--evidence-dump=off"}, extra...)
		if combined, err := exec.Command(sbomb, args...).CombinedOutput(); err != nil {
			t.Fatalf("sbomb generate: %v\n%s", err, combined)
		}
		return readFileNames(t, output), readFindingIDs(t, findingsPath)
	}

	// Without the group the archive member cannot be traced to an object, so
	// the source behind it is not in the document and the run says why.
	plainFiles, plainFindings := generate()
	if contains(plainFiles, "a.c") {
		t.Error("a source reached the document that no evidence names")
	}
	if !contains(plainFindings, "ARCHIVE_MEMBERS_UNRESOLVED") {
		t.Errorf("the unresolved member was not reported: %v", plainFindings)
	}

	// With the group ninja answers both questions, and the member resolves.
	introspectedFiles, introspectedFindings := generate("--allow-introspection=ninja")
	if !contains(introspectedFiles, "a.c") {
		t.Errorf("`ninja -t inputs` did not close the gap; files = %v", introspectedFiles)
	}
	if contains(introspectedFindings, "ARCHIVE_MEMBERS_UNRESOLVED") {
		t.Errorf("the member is resolved and still reported: %v", introspectedFindings)
	}
}

func readFileNames(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(document.Components))
	for _, component := range document.Components {
		names = append(names, component.Name)
	}
	return names
}

func readFindingIDs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Findings []struct {
			ID string `json:"id"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(document.Findings))
	for _, finding := range document.Findings {
		ids = append(ids, finding.ID)
	}
	return ids
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle || strings.HasSuffix(value, "/"+needle) {
			return true
		}
	}
	return false
}
