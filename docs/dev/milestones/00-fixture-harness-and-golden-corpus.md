### Milestone 0 — Fixture Harness and Golden Corpus

**Why first:** GNU ld map format, lld map format, `.d` escaping, `build.ninja` layout, and File API replies have no usable written specification. They must be captured from real toolchains before any parser is written. Without this milestone, Milestones 4–8 are not implementable.

**Goal:** a reproducible harness that builds tiny real projects and captures their build evidence as committed golden inputs.

**Deliverables**

* `testdata/projects/` — five minimal CMake projects:
  * `p01-hello` — one executable, two sources, one header.
  * `p02-static` — executable + static library with three sources, only one of which is referenced (tests unused-member exclusion).
  * `p03-dupnames` — two targets each with `main.cpp` and `util.cpp` (duplicate basenames).
  * `p04-generated` — a `add_custom_command` generating `version.h` from `version.in`.
  * `p05-headeronly` — an INTERFACE library consumed by one TU.
* `tools/fixtures/` — a Go program `fixgen` and a `Dockerfile` per toolchain image:
  * `gcc-13 + binutils 2.42 + ninja` (primary)
  * `clang-17 + lld + ninja`
  * `gcc-12 + binutils 2.38 + make`
  * `mingw-w64 gcc + ninja` — cross-compiled on Linux; the only source of PE artifacts and Windows-shaped paths
  * `arm-none-eabi-gcc + ninja` — bare-metal, statically linked, no libc shared objects; the reference for the embedded case
* For each (project × toolchain) the harness captures into `testdata/fixtures/<toolchain>/<project>/`:
  `build.ninja`, `compile_commands.json`, `.cmake/api/v1/reply/**`, `*.map`, `link.d` (from `-Wl,--dependency-file`), `link-trace.txt` (`-Wl,-t`), all `*.d` depfiles, `.ninja_deps` (raw) and `ninja -t deps` text, `install_manifest.txt`, the stripped and unstripped artifact, and `manifest.json` describing the capture (toolchain versions, flags, host OS).
* All absolute paths in captured fixtures are rewritten to a fixed sentinel root (`/__fixture_src__`, `/__fixture_build__`) by `fixgen` so fixtures are host-independent.
* **Windows path fixtures without a Windows machine.** Windows path semantics are covered by (a) the `mingw-w64` toolchain, which produces PE artifacts and Windows-flavoured paths inside `compile_commands.json` and depfiles while running on Linux, and (b) hand-written synthetic fixtures under `testdata/fixtures/win-synthetic/` containing drive letters, backslashes, `C$:` Ninja escaping, UNC paths, spaces, and non-ASCII path segments. MSVC-native fixtures are **not** part of this milestone.

  **[Amended.]** This read "there is no Windows CI runner" and closed with "MSVC is parked, M18". Neither holds: a `windows-latest` runner is available and is already used by the determinism matrix (phase 8d), and M18 is unparked. MSVC fixtures stay out of *this* milestone regardless, because they are not producible in Docker and so cannot follow the rule below that a developer with only Docker can regenerate every fixture. They are generated on the Windows runner and are M18's deliverable, under the same policy this milestone sets: metadata text only, a Windows-shaped sentinel root (`C:/__fixture_src__`) instead of `/__fixture_src__`, and a `PROVENANCE.md` per directory.
* **Fixture licensing policy** (`testdata/fixtures/POLICY.md`, enforced by a test):
  * Fixture *projects* are original code written for this repository and carry the repository's own license.
  * Committed evidence is limited to **build metadata text**: maps, depfiles, `build.ninja`, File API replies, `compile_commands.json`, trace and log output. These are factual descriptions of a build, contain no upstream source, and are safe to commit.
  * Committed **binaries** are limited to artifacts built from the repository's own fixture projects. They MUST be linked with `-nostdlib` where the project permits, or dynamically linked, so that no third-party runtime object is embedded. The GCC Runtime Library Exception would permit distribution anyway, but avoiding the question entirely is cheaper than arguing it.
  * **No third-party source, headers, toolchain files, or SDK trees may be committed.** Where an upstream tree is needed (ESP-IDF, M20), only the metadata text is committed and the artifact is produced at capture time.
  * Every fixture directory MUST contain a `PROVENANCE.md` naming the toolchain, its version, the license of anything not authored here, and the capture date. A test asserts the file exists and parses.

**Tests**

* `TestFixtureCorpusPresent` — every declared (toolchain, project) pair exists and its `manifest.json` parses.
* `TestFixturesContainNoHostPaths` — no fixture file contains `/home/`, `/Users/`, `C:\Users`, or the CI workspace path.
* `TestFixtureProvenancePresent` — every fixture directory has a parsable `PROVENANCE.md`.
* `TestNoThirdPartySourceCommitted` — no file under `testdata/fixtures/` outside the declared metadata extensions is larger than 256 KiB, and no directory named `include/`, `src/`, or `*-src` exists there.
* `TestFixgenIdempotent` — regenerating a fixture from the same Docker image produces byte-identical output except for embedded timestamps listed in `manifest.json.volatile`.

**Acceptance**

```
go test ./tools/fixtures/... ./internal/testutil/...      # exit 0
tools/fixtures/regen.sh --check                          # exit 0 (no drift)
```

**Definition of Done:** a developer with only Docker can regenerate every Linux fixture; CI verifies no drift; no fixture contains a host-specific path. A Windows fixture is regenerated on a Windows runner and is held to the same three properties.

---
