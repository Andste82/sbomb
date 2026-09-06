# Phase 7 — Adapter breadth

Working plan and durable state of the phase. Each step ends with a green gate
and its own commit, so an interruption never loses more than one step.

Specification: §9.2 (introspection allowlist), §9.3 (response files), §18
(assets, packaging, images), §19.2 strategies 2-5, §19.4 (git metadata), §20
(versions, purls, suppliers), §21 (package-manager adapters), §22.2 (licence
priority), milestone 20 (ESP-IDF).

## Why this order

`internal/exec` comes first because three of the remaining work packages need
it. Everything after it is largely independent and can be done in any order.

## Steps

### 7-0 — Close the Phase 6 verification gap — done

Section garbage collection and the LTO downgrade are covered by unit tests
only: no fixture project builds with `--gc-sections` or `-flto`, so
`SECTION_GC_EXCLUDED` never fires across the whole corpus. Two fixture
projects gain the flags, so both features are exercised against real linker
and compiler output. This is the same gap class that phase 0 diagnosed as the
root cause of everything it found.

### 7a — `internal/exec` — done (§9.2)

A single gateway for the fixed allowlist of introspection commands. Off by
default; enabled by `--allow-introspection` or `build.introspection.*`.

* exact argv shapes, never a shell, no interpolation of untrusted data;
* default timeout 30 s, output bound 64 MiB, every invocation logged;
* every path argument must exist and lie within a registered anchor;
* when introspection is off, callers degrade and emit an informational finding
  naming the evidence they could not obtain.

### 7b — Response files — done (§9.3)

`@file` and `.rsp` expansion in compile and link command lines: recursive with
depth limit 8 (`RSP_DEPTH_EXCEEDED`), GNU quoting on POSIX and MSVC quoting on
Windows toolchains, total expanded size bounded at 64 MiB. Without this, large
link lines and most Windows builds are simply unreadable.

### 7c — Package-manager adapters — done (§21, §19.2 strategy 2)

`internal/adapters/pkgmanager` with one interface and one implementation per
manager. Priority by real-world coverage: Conan and FetchContent first, then
vcpkg and CPM. Each supplies component name, version, purl, licence hint and
root path, and registers an anchor. None of them may add a file to the used
set -- discovery stays evidence-based (§21 opening paragraph).

### 7d — Git metadata and submodule boundaries — done (§19.4, §20.2)

Submodule boundaries as component boundaries, git-derived versions behind
`versionFrom`, normalized repository URLs with credentials stripped,
`VCS_DIRTY`. Git metadata must never expand the used-file set: a checked-out
submodule is not a used dependency.

### 7e — Packaging, images and assets — done (§18)

`internal/adapters/packaging` for the four manifest kinds, asset
classification, and assembly-mode product roots. A manifest that explicitly
names a file as an input is sufficient evidence; adjacency never is.

### 7f — ESP-IDF (milestone 20) — parked

Parked by decision. ESP-IDF is available behind `--build-arg WITH_ESP_IDF=1`
but adds 2.1 GB, and the adapter needs a captured fixture that the image has to
produce first. Nothing else in phase 7 depends on it.

## Acceptance

* A Conan-consuming build with no curated configuration yields component name,
  version, supplier and purl from the manager's own metadata.
* `--allow-introspection` off is the default and no command runs without it.
* A link line behind a response file is parsed identically to the expanded one.
* Section garbage collection and LTO are exercised by corpus fixtures rather
  than by unit tests alone.
* Full gate green throughout.


## Noted while working

**The corpus does not regenerate byte-for-byte.** Two runs of `regen.sh` with no
source change produce a diff of roughly 150 files: CMake names its File API
index `index-<wall-clock timestamp>.json`, the codemodel reply carries a
content hash that moves with it, and `.ninja_deps` is a binary log of
modification times. The committed corpus is therefore a captured artifact
rather than a reproducible one, which makes reviewing a real corpus change
harder than it should be. `regen.sh --check` verifies completeness, not byte
equality, so nothing is claiming otherwise -- but normalizing the index
filename and the deps log belongs in phase 8 alongside the other determinism
work.

**Conan, vcpkg and CPM are not implemented yet, but they are no longer
blocked.** The tools are in the image now, and each was verified to produce
real evidence offline from a local package. What the adapters will read, taken
from actual probe builds rather than from memory:

*Conan 2* -- `conan create` of a local recipe and `conan install` of a
consumer, no network:

| File | Carries |
|---|---|
| `<build>/<name>-config-version.cmake` | `set(PACKAGE_VERSION "0.6.1")` |
| `<build>/<name>-release-<arch>-data.cmake` | `set(<name>_PACKAGE_FOLDER_RELEASE "...")` -- the package root |
| `<package root>/licenses/LICENSE` | the licence file, where section 22.2 point 5 expects it |
| `<package root>/conaninfo.txt` | settings and options |

purl per section 20.4: `pkg:conan/<name>@<version>`.

*vcpkg* -- an overlay port whose source is a local directory, installed with
`VCPKG_FORCE_SYSTEM_BINARIES=1`, no network. It writes a complete SPDX
document per package:

```
installed/<triplet>/share/<name>/vcpkg.spdx.json
  packages[0].name / .versionInfo / .licenseConcluded
  packages[0].externalRefs[0].referenceLocator
      = "pkg:vcpkg/tinyfmt@2.1.0?triplet=x64-linux"
installed/<triplet>/share/<name>/vcpkg_abi_info.txt
installed/vcpkg/status            (Package/Version/Depends per entry)
```

The purl is stated outright, so nothing has to be constructed.

*CPM* -- builds on FetchContent and writes the same
`_deps/<name>-subbuild/.../<name>-populate-gitclone.cmake`, so the existing
FetchContent adapter already recognizes a CPM dependency. `cpm-package-lock.cmake`
adds declared versions when the project uses `CPMDeclarePackage`.


## Tooling in the image

`.devcontainer/Dockerfile` installs Conan 2.32.0, west 1.5.0, vcpkg (pinned to
a commit) and CPM.cmake 0.43.1 with a checksum -- 131 MB in total. Every
version is pinned because the fixture corpus is generated from these tools, and
an unpinned one would make the corpus unreproducible.

ESP-IDF is behind `--build-arg WITH_ESP_IDF=1`, off by default, because it adds
2.1 GB: 659 MB of sources and 1.4 GB of toolchains. It also needs
`libusb-1.0-0`, which is now in the apt list: without it the openocd
post-install check fails and `install.sh` aborts before it finishes, leaving
the environment unusable.
