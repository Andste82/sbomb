# Getting Started

Build the CLI and run it against a configured build directory:

```sh
go build -o sbomb ./cmd/sbomb
./sbomb generate --build-dir build --config sbomb.json --output sbomb.cdx.json
```

For CMake projects, include `cmake/Sbomb.cmake`, call `sbomb_enable(TARGET app)`,
and build the report explicitly:

```sh
cmake -S . -B build -DSBOMB_EXECUTABLE="$PWD/sbomb"
cmake --build build
cmake --build build --target sbomb
```

The normal build never invokes sbomb. The explicit target produces one report
per enabled target in `SBOMB_OUTPUT_DIR`.

## Tools in the development container

`.devcontainer/Dockerfile` installs the package managers the adapters of
section 21 read, with every version pinned. The fixture corpus is generated
from these tools, so an unpinned one would make the corpus unreproducible.

| Tool | Version | Where |
|---|---|---|
| Conan | 2.32.0 | `/opt/pkgtools`, on `PATH` as `conan` |
| west | 1.5.0 | `/opt/pkgtools`, on `PATH` as `west` |
| vcpkg | pinned commit | `/opt/vcpkg`, `$VCPKG_ROOT` |
| CPM.cmake | 0.43.1, checksum verified | `/opt/cpm/CPM.cmake`, `$CPM_PATH` |

`VCPKG_FORCE_SYSTEM_BINARIES=1` is set so vcpkg uses the cmake and ninja this
image pins rather than downloading its own.

ESP-IDF is not installed by default: it adds 2.1 GB against 131 MB for
everything above. Build the image with `--build-arg WITH_ESP_IDF=1` when you
need to produce or refresh the ESP-IDF fixture, which milestone 20 captures
once and commits.

All four are usable without network access once installed: a Conan recipe, a
vcpkg overlay port and a CPM or FetchContent dependency can all be resolved
from a local directory, which is how the fixture corpus stays reproducible.
