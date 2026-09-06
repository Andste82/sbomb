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

### 7-0 — Close the Phase 6 verification gap

Section garbage collection and the LTO downgrade are covered by unit tests
only: no fixture project builds with `--gc-sections` or `-flto`, so
`SECTION_GC_EXCLUDED` never fires across the whole corpus. Two fixture
projects gain the flags, so both features are exercised against real linker
and compiler output. This is the same gap class that phase 0 diagnosed as the
root cause of everything it found.

### 7a — `internal/exec` (§9.2)

A single gateway for the fixed allowlist of introspection commands. Off by
default; enabled by `--allow-introspection` or `build.introspection.*`.

* exact argv shapes, never a shell, no interpolation of untrusted data;
* default timeout 30 s, output bound 64 MiB, every invocation logged;
* every path argument must exist and lie within a registered anchor;
* when introspection is off, callers degrade and emit an informational finding
  naming the evidence they could not obtain.

### 7b — Response files (§9.3)

`@file` and `.rsp` expansion in compile and link command lines: recursive with
depth limit 8 (`RSP_DEPTH_EXCEEDED`), GNU quoting on POSIX and MSVC quoting on
Windows toolchains, total expanded size bounded at 64 MiB. Without this, large
link lines and most Windows builds are simply unreadable.

### 7c — Package-manager adapters (FetchContent done) (§21, §19.2 strategies 2 and 4)

`internal/adapters/pkgmanager` with one interface and one implementation per
manager. Priority by real-world coverage: Conan and FetchContent first, then
vcpkg and CPM. Each supplies component name, version, purl, licence hint and
root path, and registers an anchor. None of them may add a file to the used
set -- discovery stays evidence-based (§21 opening paragraph).

### 7d — Git metadata and submodule boundaries (§19.4, §20.2)

Submodule boundaries as component boundaries, git-derived versions behind
`versionFrom`, normalized repository URLs with credentials stripped,
`VCS_DIRTY`. Git metadata must never expand the used-file set: a checked-out
submodule is not a used dependency.

### 7e — Packaging, images and assets (§18)

`internal/adapters/packaging` for the four manifest kinds, asset
classification, and assembly-mode product roots. A manifest that explicitly
names a file as an input is sufficient evidence; adjacency never is.

### 7f — ESP-IDF (milestone 20)

Blocked on a fixture: the IDF toolchain is not installed in this container and
is too large to add. The adapter is written against a captured fixture, so this
step needs that capture first. Assessed at the end rather than faked.

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

**Conan, vcpkg and CPM are not implemented.** Neither tool is installed in the
container, and writing a parser against a remembered file format is the exact
failure this project spent phase 0 diagnosing. Conan can be installed from
PyPI and a local recipe needs no network, so a real fixture is reachable; it
costs a Dockerfile change, a CI change and a regen.sh change, which is its own
piece of work. FetchContent needed none of that, which is why it went first.
