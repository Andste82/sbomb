//go:build perf

// Package perf measures the budget of specification section 31 instead of
// asserting it in prose. The `large` fixture is generated rather than
// committed: it is two hundred megabytes of linker map and fifty thousand of
// everything else.
package perf

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

var (
	fixtureDir = flag.String("fixture", "", "reuse a generated large fixture instead of building one")
	units      = flag.Int("units", 50000, "translation units in the generated fixture")
	mapBytes   = flag.Int64("map-bytes", 200<<20, "linker map size")
)

// budget is one row of the table in section 31.
type budget struct {
	units    int
	mapBytes int64
	wall     time.Duration
	rss      int64
}

func TestPerformanceBudget(t *testing.T) {
	if runtime.NumCPU() < 2 {
		t.Skip("the budget of section 31 is stated for a 4-core runner")
	}
	want := budget{units: *units, mapBytes: *mapBytes, wall: 90 * time.Second, rss: 1536 << 20}
	if *units <= 10000 {
		want.wall, want.rss = 15*time.Second, 512<<20
	}

	root := *fixtureDir
	if root == "" {
		root = filepath.Join(t.TempDir(), "large")
		generate(t, root, *units, *mapBytes)
	}
	configPath := writeConfig(t, root)

	binary := filepath.Join(t.TempDir(), "sbomb")
	build := exec.Command("go", "build", "-o", binary, "./cmd/sbomb")
	build.Dir = repoRoot(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}

	output := filepath.Join(t.TempDir(), "large.cdx.json")
	command := exec.Command(binary, "generate",
		"--build-dir", filepath.Join(root, "build"),
		"--config", configPath, "--policy", "lenient",
		"--output", output, "--reproducible")
	started := time.Now()
	combined, err := command.CombinedOutput()
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("sbomb generate: %v\n%s", err, combined)
	}

	// Maxrss of the child, which is what section 31 means by peak RSS.
	usage, ok := command.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		t.Fatal("no resource usage for the child process")
	}
	peak := usage.Maxrss * 1024 // Linux reports kilobytes

	t.Logf("section 31: %d units, %d MB map -> %s wall, %d MiB peak RSS (budget %s, %d MiB)",
		*units, *mapBytes>>20, elapsed.Round(time.Millisecond), peak>>20,
		want.wall, want.rss>>20)

	if elapsed > want.wall {
		t.Errorf("wall time %s exceeds the budget of %s", elapsed.Round(time.Millisecond), want.wall)
	}
	if peak > want.rss {
		t.Errorf("peak RSS %d MiB exceeds the budget of %d MiB", peak>>20, want.rss>>20)
	}
}

func generate(t *testing.T, root string, units int, mapBytes int64) {
	t.Helper()
	command := exec.Command("go", "run", "./tools/generate-large",
		"--output", root, "--files", itoa(units), "--map-bytes", itoa64(mapBytes))
	command.Dir = repoRoot(t)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate-large: %v\n%s", err, output)
	}
}

func writeConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "large.json")
	content := `{
  "schemaVersion": 1,
  "project": {"name": "large", "root": "` + filepath.Join(root, "src") + `"},
  "build": {"dir": "` + filepath.Join(root, "build") + `"},
  "mode": "single",
  "artifacts": [{"path": "large", "role": "application",
                 "map": "` + filepath.Join(root, "build", "large.map") + `",
                 "linkDepfile": "` + filepath.Join(root, "build", "large.d") + `"}]
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(dir, "..", ".."))
}

func itoa(value int) string { return itoa64(int64(value)) }
func itoa64(value int64) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
