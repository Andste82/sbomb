package msbuild

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProjectSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.vcxproj")
	data := `<Project><ItemGroup><ProjectConfiguration Include="Release|x64"/><ClCompile Include="src\main.cpp"/><ResourceCompile Include="app.rc"/></ItemGroup></Project>`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := ParseProject(path, "Release", "x64")
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Sources) != 2 || project.Sources[0] != `src\main.cpp` || project.Sources[1] != "app.rc" || len(project.Configurations) != 1 || project.Configurations[0] != "Release|x64" {
		t.Fatalf("project = %#v", project)
	}
}

func TestParseShowIncludesDetectsLocalizedPrefix(t *testing.T) {
	prefix, headers, err := ParseShowIncludes("Hinweis: Datei wird eingeschlossen: C:\\SDK\\stdio.h\nHinweis: Datei wird eingeschlossen: C:\\src\\app.h\n")
	if err != nil {
		t.Fatal(err)
	}
	if prefix != "Hinweis: Datei wird eingeschlossen:" || len(headers) != 2 {
		t.Fatalf("prefix=%q headers=%v", prefix, headers)
	}
}

func TestParseProjectPlatformSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.vcxproj")
	data := `<Project>
		<ItemGroup Condition="'$(Configuration)|$(Platform)'=='Release|x64'">
			<ClCompile Include="src\x64.cpp"/>
		</ItemGroup>
		<ItemGroup Condition="'$(Configuration)|$(Platform)'=='Debug|Win32'">
			<ClCompile Include="src\win32.cpp"/>
		</ItemGroup>
	</Project>`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	projX64, err := ParseProject(path, "Release", "x64")
	if err != nil || len(projX64.Sources) != 1 || projX64.Sources[0] != `src\x64.cpp` {
		t.Fatalf("projX64 = %#v, err = %v", projX64, err)
	}
	projWin32, err := ParseProject(path, "Debug", "Win32")
	if err != nil || len(projWin32.Sources) != 1 || projWin32.Sources[0] != `src\win32.cpp` {
		t.Fatalf("projWin32 = %#v, err = %v", projWin32, err)
	}
}
