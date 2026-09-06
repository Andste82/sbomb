# Phase 8 — Hardening

Working plan and durable state of the phase. Each step ends with a green gate
and its own commit.

Specification: §30 (security and trust boundaries), §31 (performance
requirements), §14 and §27 (determinism), milestone 15.

This is the phase that decides whether sbomb can be pointed at a real firmware
build without falling over or becoming an attack surface. It adds no capability.

## Order and why

Performance comes first because it can force design changes: if memory grows
with the source tree rather than with the used-file count, that is not a tuning
problem. Everything after it is independent.

### 8a — Measure the performance budget (§31) — done

| Scenario | Requirement |
|---|---|
| 50 000 used files, 200 MB map, 50 000 depfiles | ≤ 90 s, ≤ 1.5 GiB RSS |
| 10 000 used files | ≤ 15 s, ≤ 512 MiB |
| Hashing throughput | ≥ 400 MB/s aggregate on 4 cores |
| Memory growth | linear in used-file count, not in source-tree size |

`tools/generate-large` exists and **no test invokes it**. Benchmarks exist for
hashing, BOM serialization, graph construction and depfile parsing; map
parsing, which §31 names, has none -- and it grew in phase 7 from reading a few
blocks to reading every line of the memory map.

### 8b — Parser limits as one policy (§30)

Limits exist in eighteen places, each parser inventing its own. §30 wants one:
max line 1 MiB, max 100 000 tokens per line, `--max-input-size` (default
2 GiB), recursion depth 64. Two flags do not exist at all: `--max-input-size`
and `--strict-symlinks` (§30.5, `O_NOFOLLOW`). §30.7 -- redaction applying
equally to the SBOM, the findings JSON and the review report -- is to be
verified rather than assumed.

### 8c — Fuzzing what phases 6 and 7 added

Ten fuzz targets exist, all older than phase 6. Nothing fuzzes the DWARF file
table reader, the two response-file tokenizers, the unity and PCH include
parsers, the vcpkg SPDX reader or the package manifest. All of it is untrusted
input from somebody else's build tree.

### 8d — Windows determinism executed, not simulated

`WindowsFlavor` is unit-tested on Linux, which proves the type and not the
tool. A `windows-latest` job running the same corpus and comparing bytes is
what makes the cross-platform claim true.

### 8e — An honest self-SBOM

`scripts/release.sh` writes a fabricated `compile_commands.json` naming one
file and an empty map. The tool that refuses to guess guesses about itself.
`go list -deps -json` is real evidence; the alternative is to drop the
self-SBOM rather than fake it.

### 8f — Corpus reproducibility

Two `regen.sh` runs with no source change produce ~150 changed files: CMake
names its File API index by wall clock, the codemodel hash moves with it, and
`.ninja_deps` is a binary log of modification times. Normalizing the index name
and the deps log at harvest time removes the churn.

## Acceptance

* The performance budget is measured by a test, not asserted in prose.
* `--max-input-size` and `--strict-symlinks` exist and are honoured.
* Every parser added in phases 6 and 7 has a fuzz target.
* A Windows runner produces byte-identical output to Linux for the portable
  fixture.
* The self-SBOM is derived from evidence or does not exist.


## 8a result

Measured on this runner, `test/perf` with `-tags perf`:

| Scenario | Budget | Before | After |
|---|---|---|---|
| 50 000 units, 200 MB map | ≤ 90 s, ≤ 1.5 GiB | 152 s, 1204 MiB | **65.6 s, 1370 MiB** |
| 10 000 units, 40 MB map | ≤ 15 s, ≤ 512 MiB | — | **7.7 s, 273 MiB** |

Memory grows linearly in the used-file count as section 31 requires: five times
the units, five times the resident set.

The first run missed the budget by 69 %. Profiling put 48 % of CPU in
filesystem syscalls, and 77 % of all file reading in
`make.parseDependencyFiles` -- on a build tree that has no Makefile. Two causes,
both fixed:

* `makeadapter.Parse` was called twice per run, once from `collectCompileEvidence`
  and once from `buildEvidenceGraph`, so the whole scan and every dependency
  file were read twice.
* The adapter keyed on the presence of `CMakeFiles/`, which every CMake build
  has whatever generator wrote it. It therefore walked Ninja build trees too,
  duplicating evidence `.ninja_deps` already provides. It now requires a
  `Makefile`, which is what section 9.1 selects an adapter by.

Map parsing, which section 31 names and which had no benchmark, turned out not
to be the bottleneck at all: 94.6 MB/s, so about two seconds for a 200 MB map.
It has a benchmark now regardless, because the section requires one.
