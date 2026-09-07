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

```bash
curl -fsSL https://andste82.github.io/sbomb/install.sh | sh
```

Windows, in PowerShell:

```powershell
irm https://andste82.github.io/sbomb/install.ps1 | iex
```

That takes the latest release for your platform and verifies it against the
release's `SHA256SUMS` before installing — there is no way to skip that check.
`--version v0.13.0` pins a release, `--bin-dir` chooses where it lands, and
`--with-sbom` puts the release's own CycloneDX document beside the binary.

Or do it by hand, which is the same four steps:

```bash
VERSION=v0.13.0
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

## The CMake integration, without installing anything

If the project would rather fetch sbomb than have everyone install it, the
release ships a CMake bundle. One `FetchContent_Declare` gives you the module
*and* a binary for whoever is building:

```cmake
include(FetchContent)
FetchContent_Declare(sbomb
  URL https://github.com/Andste82/sbomb/releases/download/v0.13.0/sbomb-cmake.tar.gz)
FetchContent_MakeAvailable(sbomb)

add_executable(app src/main.c)
sbomb_enable(TARGET app POLICY lenient)
```

```bash
cmake -S . -B build
cmake --build build
cmake --build build --target sbomb
```

The binary is downloaded at configure time into the build tree and **verified
against the release's `SHA256SUMS`**, which is not optional. It is fetched for
the machine running the build, not for the target: a firmware project
cross-compiling to bare-metal ARM still gets the binary for the developer's
laptop.

The URL pins the version, so everyone on the project runs the same tool and a
build from two years from now runs the same one again. `-DSBOMB_EXECUTABLE=…`
overrides the whole thing when somebody already has sbomb, and nothing is
downloaded then.

### Two downloads, not one

It matters where the knobs are, because the two halves are fetched by different
code:

| | Fetched by | Verified by |
|---|---|---|
| `sbomb-cmake.tar.gz` (the modules, 7 KB) | CMake's `FetchContent` | whatever you tell `FetchContent_Declare` |
| the sbomb binary (6.6 MB) | sbomb's own `SbombFetch.cmake` | always, against `SHA256SUMS` |

CMake does **not** verify a `FetchContent` download by default — its
`TLS_VERIFY` is off unless asked, and it checks no digest unless given one. So
the bundle deserves both, and `FetchContent_Declare` passes them straight
through to `ExternalProject_Add`:

```cmake
FetchContent_Declare(sbomb
  URL      https://github.com/Andste82/sbomb/releases/download/v0.13.0/sbomb-cmake.tar.gz
  URL_HASH SHA256=<the sbomb-cmake.tar.gz line from the release's SHA256SUMS>
  TLS_VERIFY ON)
```

### Behind a TLS-intercepting proxy

A proxy that re-signs TLS presents its own certificate, and the binary download
then fails with `SSL peer certificate or SSH remote key was not OK` — the
verification working, not breaking.

Both downloads read `CMAKE_TLS_CAINFO`, so naming the CA once covers the pair.
On the command line, leaving the project's `CMakeLists.txt` exactly as above:

```bash
cmake -S . -B build \
  -DCMAKE_TLS_VERIFY=ON \
  -DCMAKE_TLS_CAINFO=/etc/ssl/certs/corporate-root.pem
```

Or in the project itself, so nobody has to remember the flags:

```cmake
cmake_minimum_required(VERSION 3.24)
project(device C)

# Both downloads: the bundle, which CMake fetches, and the binary, which sbomb
# fetches. Named before FetchContent_Declare, because that is when the bundle's
# download is set up.
set(CMAKE_TLS_VERIFY ON)
set(CMAKE_TLS_CAINFO "/etc/ssl/certs/corporate-root.pem")

include(FetchContent)
FetchContent_Declare(sbomb
  URL      https://github.com/Andste82/sbomb/releases/download/v0.13.0/sbomb-cmake.tar.gz
  URL_HASH SHA256=<the sbomb-cmake.tar.gz line from the release's SHA256SUMS>)
FetchContent_MakeAvailable(sbomb)
```

**`CMAKE_TLS_VERIFY=ON` is the half that is easy to leave out**, and leaving it
out is silent: `FetchContent` checks no certificate unless asked, so the CA is
then simply unused and an intercepted bundle download succeeds unverified. It
also turns verification on for every other `FetchContent` download in the
project, which is the point — but it can surface a dependency that has been
arriving unchecked until now.

To keep the setting to sbomb alone, name the CA on each side instead: pass
`TLS_VERIFY ON` and `TLS_CAINFO` to `FetchContent_Declare` for the bundle, and
`-DSBOMB_FETCH_TLS_CAINFO=…` for the binary. That variable is consulted first,
before `CMAKE_TLS_CAINFO`, `SSL_CERT_FILE` and `CURL_CA_BUNDLE` — so a machine
already set up for curl or OpenSSL usually needs nothing at all.

Note that naming a CA *adds* trust rather than replacing it: the system store is
still consulted, so this makes the intercepted connection work — it does not
narrow the download to that one authority.

There is no switch to turn the verification off, and none is planned. The
checksum is what makes the download defensible, and `SHA256SUMS` arrives over
the same connection as the binary it vouches for.

### Pinning the digest yourself

That last sentence is the limit of the default: unpinned, the download is trust
on first use, because whoever could substitute the binary could substitute the
list beside it. A digest from somewhere the network cannot reach — the release
notes, an audit, a colleague — closes that:

```bash
# One value that covers every platform: the digest of SHA256SUMS itself.
cmake -S . -B build -DSBOMB_FETCH_SHA256SUMS=<digest of SHA256SUMS>

# Or pin this host's binary directly. SHA256SUMS is then not fetched at all.
cmake -S . -B build -DSBOMB_FETCH_SHA256=<digest of sbomb-linux-amd64>
```

Both values are the ones published in the release's own `SHA256SUMS`. A
mismatch names which of the two it was checked against, and a mistyped digest
is refused as a mistyped digest rather than reported as a mismatch.

## The CMake integration, with sbomb already installed

The bundled module sets the flags, files the File API query and adds a target
that produces the SBOM on demand:

```cmake
list(APPEND CMAKE_MODULE_PATH "${CMAKE_CURRENT_LIST_DIR}/path/to/sbomb/cmake")
include(Sbomb)

add_executable(app src/main.c)
sbomb_enable(TARGET app POLICY lenient CONFIG "${CMAKE_SOURCE_DIR}/sbomb.json")
```

```bash
cmake -S . -B build -DSBOMB_EXECUTABLE="$PWD/sbomb"
cmake --build build
cmake --build build --target sbomb     # produces the SBOMs
```

**Start with `lenient`.** The target fails the build when the policy fails,
because a policy that cannot stop anything is decoration — and a first run on
an unprepared project has findings, which is the tool working rather than the
tool breaking. `lenient` lets you see the answer first. Work upwards from
there, as [Choosing a policy](#choosing-a-policy) describes; `POLICY cra` on a
project whose components have no supplier, licence or version will fail, and is
meant to.

`sbomb_enable` may be called for several targets. Each gets its own
`sbomb-<target>` target, and the aggregate `sbomb` target builds all of them.

That gives **one document per target**. A product made of several deliverables
— a bootloader and a firmware image that ship together — is a different thing,
and belongs in one assembly document: set `mode` to `assembly` and list them
under `artifacts[]`. See
[docs/configuration.md](configuration.md#artifacts).

Two things the module does that are worth knowing:

* It sets `CMAKE_EXPORT_COMPILE_COMMANDS=ON` in the cache, because sbomb needs
  the compile database and a build configured without it has no evidence to
  read.
* **`include(Sbomb)` belongs above the targets.** It switches
  `CMAKE_EXPORT_COMPILE_COMMANDS` on, and the generator decides whether to
  record a compile command when it processes a target — so below them the
  setting arrives too late, `compile_commands.json` appears only on the next
  configure, and sbomb loses the object-to-source evidence. The module warns
  when it is included after a target rather than letting the SBOM come out
  quietly worse.

**The normal build never runs sbomb.** The targets are excluded from `all`, so
`cmake --build build` stays exactly as fast as it was; the SBOM is produced
when you ask for it.

| Cache variable | Default | Purpose |
|---|---|---|
| `SBOMB_EXECUTABLE` | `sbomb` | Path to the binary |
| `SBOMB_OUTPUT_DIR` | `${CMAKE_BINARY_DIR}/sbom` | Where the documents go |
| `SBOMB_LINK_EVIDENCE` | `ON` | Add the linker map and dependency-file flags |

Linker flags are probed before use, so a toolchain that does not support them
produces a status message rather than a broken link. They are added only to
targets that are linked: a static or object library is archived, and an
imported target belongs to somebody else's build, so both are reported and
skipped rather than silently carrying a flag that does nothing.

`CONFIG` is used when you name it. When you do not, `SBOMB_DEFAULT_CONFIG`
applies — `${CMAKE_SOURCE_DIR}/sbomb.json` — but **only if that file exists**,
because passing a configuration nobody wrote turns a run that would have worked
on defaults into a failure.

On CMake 3.27 and later the File API query is filed for the run that is
happening, so one configure leaves a reply and the SBOM target does nothing but
run sbomb. Below 3.27 the query is only read at the start of a run, so it takes
effect on the next one; there the target re-configures once, because a reply
that never arrives is not a worse answer but no answer at all.

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
- uses: Andste82/sbomb/.github/actions/sbomb@v0.13.0
  with:
    version: v0.13.0
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
