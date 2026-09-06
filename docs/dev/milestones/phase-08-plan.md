# Phase 8 — Hardening

Working plan and durable state of the phase. Each step ends with a green gate
and its own commit.

Specification: §30 (security and trust boundaries), §31 (performance
requirements), §14 and §27 (determinism), milestone 15.

This is the phase that decides whether sbomb can be pointed at a real firmware
build without falling over or becoming an attack surface. It adds no capability.

**All six steps are done.**

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

**§30 point 7** -- that `--redact-unanchored-paths` applies equally to the
SBOM, the findings JSON *and* the review report -- was assumed rather than
verified here. It is verified in 8f.

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

## 8e — An honest self-SBOM — done

`scripts/release.sh` wrote a fabricated `compile_commands.json` naming one Go
file beside an empty linker map. It made every release build exit 3 before
writing its checksums, and it would have shipped a false bill of materials next
to the binary it claims to describe. That fabrication was removed in 8a
(deviation D16); this puts the document back, derived from evidence.

The evidence is the module record the Go linker writes into the binary: every
module linked in, at the version and with the `go.sum` hash that reached the
artifact, plus the toolchain, the target platform and the commit. It is not a
description of what the build was asked to do, it is part of the deliverable,
so it cannot disagree with the deliverable. `sbomb self <binary>` reads it and
nothing else -- no subprocess, no network, no source tree -- which is why a
linux host describes a cross-compiled windows/amd64 binary correctly.

`go list -deps -json`, which this plan named first, was dropped. It describes
the working tree rather than the artifact and would have to run outside the
allowlist of section 9.2. What it would have added is file-level detail, and
nothing in a Go binary names its source files; a module is versioned, licensed
and published as a unit, so the module is the component.

| Piece | Where |
|---|---|
| Reads the linker's record | `internal/adapters/gobin` |
| Reads `vendor/modules.txt` | `internal/adapters/govendor` |
| Assembles the document | `internal/selfsbom` |
| Command | `cmd/sbomb/self.go` |

Licence evidence comes from the vendor tree and only where it agrees with the
binary: a vendor directory from another commit holds the right module at the
wrong version, and its licence would be attributed to what was linked while
looking exactly as confident as a correct answer. That case is
`STALE_BUILD_EVIDENCE` now.

Exact licence-text matching turned out to recognize none of the three vendored
licences. All three fill in the copyright holder or renumber the clause list,
which is what a real licence file looks like; section 22.3 technique 2 hashes
the normalized text and only matches a verbatim one. `--license
<module>=<SPDX>` curates them, they are marked `curated` rather than detected,
and a curated value the licence text contradicts is a `LICENSE_CONFLICT`.

Running `scripts/release.sh build` end to end for the first time found that
**the tool did not build for Windows at all**: `internal/limits` used
`syscall.O_NOFOLLOW`, which does not exist there, so every cross-compile had
failed since 8b landed. The no-follow open is platform-split now.

## 8f — Corpus reproducibility — done

Two `regen.sh` runs over unchanged sources rewrote around 150 files. Five
causes were ours:

| Cause | Fix |
|---|---|
| File API index filename carries the configure wall clock | pinned to the fixture date |
| `.ninja_deps` records each output's modification time | `tools/fixtures/depsnorm` zeroes the field |
| Parallel builds reorder that log | builds run serially |
| The GNU PE linker stamps a link time into every `.exe` | `--no-insert-timestamp` |
| GCC draws a random seed for the LTO sections | `-frandom-seed` pinned |

Three projects still move, and in each the toolchain is what is not
reproducible: CMake orders a target's dependency list unstably, the LTO map
names GCC's temporary objects -- which is what that fixture exists to show --
and Conan gives a locally built package a random cache folder. Deviation D17
records each with what was measured.

`tools/fixtures/check-reproducible.sh` regenerates twice and fails on any
difference outside those three, which it names rather than pattern-matches, so
a fourth cannot join them quietly. `regen.sh --only <toolchain>[/<project>]`
makes the loop tolerable: one pair takes seconds where the corpus takes
minutes.

Section 30 point 7 is closed with it. `--redact-unanchored-paths` had to apply
to the SBOM, the findings JSON and the review report equally, which was assumed
rather than checked; it does, and a test says so -- including the half that
proves the unredacted outputs do contain the paths.

---

## Acceptance

* [x] The performance budget is measured by a test, not asserted in prose.
* [x] `--max-input-size` and `--strict-symlinks` exist and are honoured.
* [x] Every parser added in phases 6 and 7 has a fuzz target.
* [x] A Windows runner produces byte-identical output to Linux.
* [x] The self-SBOM is derived from evidence or does not exist.
* [x] Two regenerations of an unchanged corpus produce no diff, outside three
  projects where the toolchain is what is not reproducible (D17).
