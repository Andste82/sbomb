# Windows

The release includes a statically linked `windows/amd64` executable. Windows
builds can be analyzed on Windows, or from Linux with `--path-flavor windows`;
the latter preserves drive letters, separators and case-insensitive matching
in the generated evidence and SBOM. With the same path flavor, a Windows host
and a Linux host write the same document from the same build directory.

## Supported builds

The Microsoft toolchain is supported for these CMake workflows:

* **Ninja or Ninja Multi-Config with MSVC**: compile and link evidence comes
  from MSVC command lines, Ninja dependencies and the linker map.
* **NMake Makefiles with MSVC**: compile and link evidence comes from the
  compilation database, the dependency files and the linker map.
* **Visual Studio 17 2022 with MSBuild**: object/source mappings, header
  dependencies and link inputs come from the MSBuild `.tlog` files.

GCC, Clang and mingw-w64 builds remain supported as well. For a Visual Studio
multi-config build, select the configuration explicitly:

```powershell
sbomb generate --build-dir build --config-name Debug --output build/sbom.cdx.json
```

When analyzing a Windows build from Linux, add the Windows path flavor:

```bash
sbomb generate --build-dir build --config-name Debug \
  --path-flavor windows --output build/sbom.cdx.json
```

The build directory must contain the structured evidence produced by the build.
For MSBuild this means the relevant `.tlog` files; for Ninja it means the
Ninja dependency/build evidence and linker map. The committed MSVC fixtures in
`testdata/fixtures/msvc-ninja`, `testdata/fixtures/msvc-nmake` and
`testdata/fixtures/msvc-vs17` show the expected layouts.

A build directory read on another machine than the one that built it — a
Windows build analyzed from Linux, say — leaves the toolchain files the
evidence names by absolute path unread and without a hash, and reports
`HOST_FILES_NOT_READ`. Run sbomb on the build machine to have them hashed.

## Known limitation

PDB files are not parsed. MSVC builds therefore do not provide DWARF-based
header evidence and may report `DEBUG_INFO_UNAVAILABLE`; header evidence comes
from `/showIncludes`, Ninja dependencies or MSBuild TLogs instead. The PDB
path and signature may still be recorded for identity correlation.