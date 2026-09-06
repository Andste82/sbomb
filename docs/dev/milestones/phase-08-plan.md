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

### 8b — Parser limits as one policy (§30) — done

Limits exist in eighteen places, each parser inventing its own. §30 wants one:
max line 1 MiB, max 100 000 tokens per line, `--max-input-size` (default
2 GiB), recursion depth 64. Two flags do not exist at all: `--max-input-size`
and `--strict-symlinks` (§30.5, `O_NOFOLLOW`). §30.7 -- redaction applying
equally to the SBOM, the findings JSON and the review report -- is to be
verified rather than assumed.

### 8c — Fuzzing what phases 6 and 7 added — done

Ten fuzz targets exist, all older than phase 6. Nothing fuzzes the DWARF file
table reader, the two response-file tokenizers, the unity and PCH include
parsers, the vcpkg SPDX reader or the package manifest. All of it is untrusted
input from somebody else's build tree.

### 8d — Windows determinism executed, not simulated — done

`WindowsFlavor` is unit-tested on Linux, which proves the type and not the
tool. The determinism matrix gained a `windows-latest` runner, so the corpus is
now processed on all three targets and the three hashes are compared.

Recorded correctly this time: milestone 14 said "there is no Windows runner",
which read like a design decision and was a property of the host it was written
for. Flavor tests stay where they are -- string-level behaviour is better
tested where it is deterministic. The runner covers what they cannot: that the
binary runs, and that a case-insensitive filesystem does not change the answer.

`.gitattributes` marks the corpus and the goldens as byte-exact, because git's
end-of-line conversion would otherwise rewrite them on a Windows checkout and a
hash difference would mean git edited the input rather than the tool behaving
differently.

### 8e — An honest self-SBOM

`scripts/release.sh` wrote a fabricated `compile_commands.json` naming one file
and an empty map. The tool that refuses to guess guessed about itself, and the
resulting findings made every release build exit 3 before it wrote its
checksums. The fabrication is removed (deviation D16); what remains is to
derive the self-SBOM from evidence. Go emits neither a compile database nor a
linker map, so it needs a source of its own -- `go list -deps -json` is the
obvious candidate.

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


## 8b result

`internal/limits` holds the bounds of section 30 in one place: 1 MiB per line,
100 000 tokens per line, a 2 GiB default input ceiling and depth 64. Its zero
value is the specified default, so a caller with no opinion still gets them.
`limits.Scanner` replaced the four hand-rolled bounded scanners the parsers of
phases 6 and 7 had grown.

The two flags section 30 names and nothing implemented now exist:

* `--max-input-size` (accepts `2G`, `512M`, or plain bytes) refuses a file
  before allocating for it rather than after.
* `--strict-symlinks` refuses a path whose final component is a symbolic link.
  `Stat` uses `Lstat` and `Open` passes `O_NOFOLLOW`, so the kernel decides and
  the window between checking and opening is closed.

Hashing goes through it, which is where a symbolic link out of every anchor
would otherwise be followed.

## 8c result

Six fuzz targets for what phases 6 and 7 added, bringing the total to fourteen:
the two response-file tokenizers, response-file expansion, the unity and PCH
include parsers, package discovery across all four managers, and binary
inspection over arbitrary bytes.

Two defects, both on the first run:

* **The response-file tokenizers corrupted non-UTF-8 paths.** They iterated
  runes, so a byte that is not valid UTF-8 became U+FFFD -- one byte in, three
  out. A Latin-1 filename or a Windows path in the local code page would have
  entered the SBOM under a name that matches no file. Every character these
  tokenizers act on is ASCII, so they iterate bytes now.
* **`#include ""` yielded an empty include path**, which the caller joined with
  the including file's directory and identified as a file. An empty target
  names nothing and is refused.

`scripts/fuzz-all.sh` runs all fourteen.
