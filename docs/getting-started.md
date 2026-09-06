# Getting started

This walks through a first run: what your build has to produce, how to get
sbomb, and how to read what comes out.

## What your build has to produce

sbomb reads the artefacts a normal CMake build already leaves behind, plus two
that need a linker flag. In order of how much they are worth:

| Evidence | How to get it | Without it |
|---|---|---|
| CMake File API reply | Written automatically when a client queries it; the bundled module does | Targets, anchors and the toolchain are unknown |
| `compile_commands.json` | `-DCMAKE_EXPORT_COMPILE_COMMANDS=ON` | Objects cannot be traced to sources |
| Linker map | `-Wl,-Map=<artifact>.map` | Archive members cannot be told apart from whole archives |
| Link dependency file | `-Wl,--dependency-file=<artifact>.d` | One of several link-evidence sources is missing |
| Debug information | Build with `-g`, do not strip | Header evidence falls back to dependency files |

The linker map is the one worth going out of your way for. It is what turns
"this program links libcrypto" into "this program uses these eleven object
files out of libcrypto".

## Getting sbomb

Download a release and check it before running it:

```bash
VERSION=v0.10.0
BASE=https://github.com/Andste82/sbomb/releases/download/$VERSION
curl -fLO "$BASE/sbomb-linux-amd64"
curl -fLO "$BASE/SHA256SUMS"
sha256sum --check --ignore-missing SHA256SUMS
chmod +x sbomb-linux-amd64
```

Binaries are published for linux/amd64, linux/arm64, windows/amd64,
darwin/amd64 and darwin/arm64. Each one ships a CycloneDX SBOM of itself
beside it. The macOS builds are cross-compiled and not notarized, so a browser
download arrives quarantined — `xattr -d com.apple.quarantine ./sbomb-darwin-*`
clears it, and a `curl` download is unaffected.

Or build it yourself; there is nothing to install:

```bash
go build -o sbomb ./cmd/sbomb
```

## The manual route

If you would rather not touch your `CMakeLists.txt`:

```bash
cmake -S . -B build \
      -DCMAKE_EXPORT_COMPILE_COMMANDS=ON \
      -DCMAKE_BUILD_TYPE=Debug \
      -DCMAKE_EXE_LINKER_FLAGS="-Wl,-Map=build/app.map -Wl,--dependency-file=build/app.d"
cmake --build build

sbomb generate --build-dir build --output build/app.cdx.json
```

sbomb finds the map and the dependency file next to the artifact. If they sit
somewhere else, name them in the configuration under `artifacts[]`.

## The CMake integration

The bundled module sets the flags, files the File API query and adds a target
that produces the SBOM on demand:

```cmake
list(APPEND CMAKE_MODULE_PATH "${CMAKE_CURRENT_LIST_DIR}/path/to/sbomb/cmake")
include(Sbomb)

add_executable(app src/main.c)
sbomb_enable(TARGET app POLICY cra CONFIG "${CMAKE_SOURCE_DIR}/sbomb.json")
```

```bash
cmake -S . -B build -DSBOMB_EXECUTABLE="$PWD/sbomb"
cmake --build build
cmake --build build --target sbomb     # produces the SBOMs
```

`sbomb_enable` may be called for several targets. Each gets its own
`sbomb-<target>` target, and the aggregate `sbomb` target builds all of them.

**The normal build never runs sbomb.** The targets are excluded from `all`, so
`cmake --build build` stays exactly as fast as it was; the SBOM is produced
when you ask for it.

| Cache variable | Default | Purpose |
|---|---|---|
| `SBOMB_EXECUTABLE` | `sbomb` | Path to the binary |
| `SBOMB_OUTPUT_DIR` | `${CMAKE_BINARY_DIR}/sbom` | Where the documents go |
| `SBOMB_LINK_EVIDENCE` | `ON` | Add the linker map and dependency-file flags |

Linker flags are probed before use, so a toolchain that does not support them
produces a status message rather than a broken link.

## Reading the result

A run writes the SBOM where `--output` says, and `evidence.json` into the build
directory. Ask for the review report as well the first few times:

```bash
sbomb generate --build-dir build --output build/app.cdx.json \
  --review-report build/review.txt --findings-json build/findings.json
```

The report names the adapters that contributed, the counts, and every finding
with its subject. Start there. When something in it surprises you, ask:

```bash
sbomb explain --build-dir build --file project:src/main.c
```

which prints the chain of evidence back to the artifact — and prints nothing at
all when there is no chain, which is the answer to "why is my file missing?".

## Findings are the point, not the noise

A first run on a real project produces findings. That is the tool working: it
reports what it could not establish instead of inventing it. The common ones:

| Finding | Usually means |
|---|---|
| `UNKNOWN_COMPONENT` | Files that belong to no configured or detected component — add a `components[]` entry |
| `UNKNOWN_VERSION`, `MISSING_SUPPLIER` | The CRA fields nobody has curated yet |
| `UNKNOWN_LICENSE` | No licence file found, or one holding something other than a single known licence |
| `MISSING_LINK_EVIDENCE` | No linker map and no debug information — add the flags above |
| `UNANCHORED_FILE` | A file outside every anchor — add an `anchors[]` entry so its identity stays portable |
| `STALE_BUILD_EVIDENCE` | The artifact is older than its inputs; rebuild before believing the SBOM |

[docs/findings.md](findings.md) lists all of them.

## Choosing a policy

The profile does not change what is discovered — only whether the run passes
and which optional material is in scope.

| Profile | Use it for |
|---|---|
| `lenient` | The first run on an unprepared project |
| `default` | Day-to-day CI |
| `cra` | Release gating under the Cyber Resilience Act |
| `strict` | Everything on, including the gates most projects need waivers for |

Work upwards. Start with `lenient` to see the shape of the answer, curate
components until `default` passes, then turn on `cra` for releases.

A finding you have judged and accepted belongs in a waiver file with a reason
and an expiry, not in a permanently loosened gate.

## In CI

Use the composite action, or call the binary. Either way, build first:

```yaml
- uses: Andste82/sbomb/.github/actions/sbomb@v0.10.0
  with:
    version: v0.10.0
    build-dir: build
    config: sbomb.json
    policy: cra
    output: build/app.cdx.json
```

[docs/ci.md](ci.md) has the details, including what to do about a private
repository.

## Working on sbomb itself

Development setup, the pinned toolchains in the container, the fixture corpus
and the specification live under [docs/dev](dev/).
