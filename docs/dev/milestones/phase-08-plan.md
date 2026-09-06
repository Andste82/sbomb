# Phase 8 — Hardening

Working plan and durable state of the phase. Each step ends with a green gate
and its own commit.

Specification: §30 (security and trust boundaries), §31 (performance
requirements), §14 and §27 (determinism), milestone 15.

This is the phase that decides whether sbomb can be pointed at a real firmware
build without falling over or becoming an attack surface. It adds no capability.

**Two of six steps remain: 8e and 8f.**

---

## 8a — The performance budget, measured — done

`tools/generate-large` existed and no test invoked it, and what it wrote was not
runnable: fifty thousand files and a 200 MB map, but no compile database, no
dependency files, and a map in a shape no parser recognizes. It writes a build
directory now, and `test/perf` measures against it with `-tags perf`.

| Scenario | Budget | Before | After |
|---|---|---|---|
| 50 000 units, 200 MB map | ≤ 90 s, ≤ 1.5 GiB | 152 s, 1204 MiB | **65.6 s, 1370 MiB** |
| 10 000 units, 40 MB map | ≤ 15 s, ≤ 512 MiB | — | **7.7 s, 273 MiB** |

Memory grows linearly in the used-file count as §31 requires: five times the
units, five times the resident set.

The first run missed by 69 %. Profiling put 48 % of CPU in filesystem syscalls
and 77 % of all file reading in `make.parseDependencyFiles`, on a build tree
with no Makefile. Two causes: `makeadapter.Parse` was called twice per run, and
the adapter keyed on `CMakeFiles/`, which every CMake build has whatever
generator wrote it. It requires a `Makefile` now, which is what §9.1 selects an
adapter by.

Map parsing, which §31 names and which had no benchmark, was not the bottleneck:
94.6 MB/s, about two seconds for a 200 MB map. It has one now regardless.

The 10 000 unit row runs in CI on every push; the 50 000 unit row is run on
demand: `go test -tags perf ./test/perf/ -timeout 15m`.

## 8b — Parser limits as one policy — done

Eighteen files each invented their own bounds. `internal/limits` holds the four
of §30 in one place -- 1 MiB per line, 100 000 tokens per line, a 2 GiB default
input ceiling, depth 64 -- and its zero value is the specified default, so a
caller with no opinion still gets them. `limits.Scanner` replaced the
hand-rolled bounded scanners the parsers of phases 6 and 7 had grown.

The two flags §30 names and nothing implemented now exist:

* `--max-input-size` (`2G`, `512M` or plain bytes) refuses a file before
  allocating for it rather than after.
* `--strict-symlinks` refuses a path whose final component is a symbolic link.
  `Stat` uses `Lstat` and `Open` passes `O_NOFOLLOW`, so the kernel decides and
  the window between checking and opening is closed.

Hashing goes through it, which is where a link pointing out of every anchor
would otherwise be followed.

**Still open in §30:** point 7 -- that `--redact-unanchored-paths` applies
equally to the SBOM, the findings JSON *and* the review report -- is assumed
rather than verified. It is one test.

## 8c — Fuzzing what phases 6 and 7 added — done

Six new targets, fourteen in total: the two response-file tokenizers,
response-file expansion, the unity and PCH include parsers, package discovery
across all four managers, and binary inspection over arbitrary bytes.
`scripts/fuzz-all.sh` runs them all.

Two defects on the first run:

* **The response-file tokenizers corrupted non-UTF-8 paths.** They iterated
  runes, so a byte that is not valid UTF-8 became U+FFFD -- one byte in, three
  out. A Latin-1 filename or a Windows path in the local code page would have
  reached the SBOM under a name matching no file. Every character these
  tokenizers act on is ASCII, so they iterate bytes now.
* **`#include ""` yielded an empty include path**, which the caller joined with
  the including file's directory and then identified as a file.

Neither was reachable from a hand-written test, because neither is something a
person thinks to write down.

## 8d — Windows determinism, executed — done

The determinism matrix gained a `windows-latest` runner. The corpus is
processed on linux/amd64, linux/arm64 and windows/amd64, and the three hashes
are compared. They matched on the first run:
`7d533b334acc36edd73385aa5945e6f75e2915cc8dda7ed60de14aadb8d769aa`.

Milestone 14 said "there is no Windows runner", which read like a design
decision and was a property of the host it was written for -- an internal
GitHub Enterprise instance with Linux runners only. Milestone 14 is amended.

Flavor tests stay where they are: string-level behaviour is better tested where
it is deterministic. The runner covers what they cannot -- that the binary runs,
and that a case-insensitive filesystem does not change the answer.

`.gitattributes` marks the corpus and the goldens as byte-exact, because git's
end-of-line conversion would otherwise rewrite them on a Windows checkout and a
hash difference would mean git edited the input rather than the tool behaving
differently.

---

## 8e — An honest self-SBOM — open

`scripts/release.sh` wrote a fabricated `compile_commands.json` naming one Go
file beside an empty linker map. It made every release build exit 3 before
writing its checksums, and it would have shipped a false bill of materials next
to the binary it claims to describe. The fabrication is removed
(deviation D16); a release currently carries the binaries and their checksums
and no SBOM.

What remains is to derive one from evidence. Go emits neither a compile
database nor a linker map, so this needs an evidence source of its own.
`go list -deps -json` is the obvious candidate: it names every package in the
binary, its module, its version and its files. That is a small adapter plus a
fixture, not a script change.

Milestone 16 requires the self-SBOM, so this closes a deviation rather than
adding a feature.

## 8f — Corpus reproducibility — open

Two `regen.sh` runs with no source change produce roughly 150 changed files:

* CMake names its File API index `index-<wall clock>.json`, and the codemodel
  reply carries a content hash that moves with it.
* `.ninja_deps` is a binary log of modification times.

`regen.sh --check` verifies completeness, not byte equality, so nothing claims
otherwise -- but it makes reviewing a real corpus change harder than it should
be. Normalizing the index filename and the deps log at harvest time removes the
churn. Verifying it costs several regenerations, each a few minutes.

---

## Acceptance

* [x] The performance budget is measured by a test, not asserted in prose.
* [x] `--max-input-size` and `--strict-symlinks` exist and are honoured.
* [x] Every parser added in phases 6 and 7 has a fuzz target.
* [x] A Windows runner produces byte-identical output to Linux.
* [ ] The self-SBOM is derived from evidence or does not exist.
* [ ] Two regenerations of an unchanged corpus produce no diff.
