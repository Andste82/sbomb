package pathmodel

import "testing"

func TestResolve(t *testing.T) {
	if got := Resolve("/tmp/project/src", "/tmp/project", "/tmp/project"); got != "project:src" {
		t.Fatalf("Resolve() = %q, want %q", got, "project:src")
	}
	if got := Resolve("/tmp/project/../project/src", "/tmp/project", "/tmp/project"); got != "project:src" {
		t.Fatalf("Resolve() = %q, want %q", got, "project:src")
	}
	if got := Slug("/tmp/project/src/very-long-name/alpha-beta-gamma", 16); got == "" || len(got) > 32 {
		t.Fatalf("Slug() = %q, length too long", got)
	}
}

func TestWindowsFlavor(t *testing.T) {
	flavor := WindowsFlavor{}
	if got := ResolveWithFlavor(`C:\work\project\src\main.c`, `c:/work/project`, `C:/work/build`, flavor); got != `project:src/main.c` {
		t.Fatalf("ResolveWithFlavor() = %q, want %q", got, `project:src/main.c`)
	}
	if got := ResolveWithFlavor(`\\server\share\project\src\main.c`, `\\SERVER\SHARE\PROJECT`, ``, flavor); got != `project:src/main.c` {
		t.Fatalf("UNC ResolveWithFlavor() = %q, want %q", got, `project:src/main.c`)
	}
}

func TestWindowsFlavorOnLinux(t *testing.T) {
	flavor := WindowsFlavor{}
	paths := map[string]string{
		`C:\fixture\project\src\main.c`:              `project:src/main.c`,
		`c:/FIXTURE/PROJECT/src/../include/config.h`: `project:include/config.h`,
		`C:\fixture\build\generated\version.h`:       `build:generated/version.h`,
	}
	if got := ResolveWithFlavor(`\\server\share\project\src\main.c`, `\\SERVER\SHARE\PROJECT`, ``, flavor); got != `project:src/main.c` {
		t.Errorf("UNC ResolveWithFlavor() = %q, want %q", got, `project:src/main.c`)
	}
	for input, want := range paths {
		if got := ResolveWithFlavor(input, `C:\fixture\project`, `C:\fixture\build`, flavor); got != want {
			t.Errorf("ResolveWithFlavor(%q) = %q, want %q", input, got, want)
		}
	}
}
