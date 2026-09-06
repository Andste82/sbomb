# Phase 0 — Gate and fixtures

**Status: complete.** Commit `8fe4435`.

## Why it came first

Without verifiable inputs and a gate that actually runs, every later step
regresses unnoticed. This was the cheapest step with the largest leverage, and
the measurement that motivated it was the root cause of everything else:

```
$ cat testdata/fixtures/gcc-13/p02-static/link-trace.txt
link trace for gcc-13/p02-static
```

`tools/fixtures/regen.sh` built nothing. It wrote `manifest.json` and
`PROVENANCE.md` through a Python stub, and the supposed linker output was
placeholder prose. Milestones 04 through 07 -- depfile, DWARF, map parsers,
ninja -- could therefore never be integration-tested against real toolchain
output, which is why they were delivered as libraries and never wired in.
**That is the cause, not a symptom.**

## Work

* **Real fixture generation.** `regen.sh` configures and builds five projects
  per toolchain and harvests only evidence. Built under the sentinel roots
  `/__fixture_src__` and `/__fixture_build__`, so `.ninja_deps` and the DWARF in
  the artifacts carry portable paths natively -- rewriting them afterwards would
  have been impossible for binary formats.
* **Toolchain matrix matched to reality**: gcc 13.3, clang 18.1,
  arm-none-eabi 13.2, mingw-w64, plus the Makefiles generator.
* **A CI gate** on push and pull request: build, vet, test, gofmt.
* **The universal gate restored.** `-race` is impossible with the devcontainer's
  `CGO_ENABLED=0`, so it became a dedicated job and deviation D2.
* **One source of truth for the tool version**, `internal/buildinfo`.
* **Cleanup**: `internal/findings` (a duplicate of `domain`), four eleven-line
  linker wrapper packages, two `init()` hacks against unused imports.

## Result

25 fixtures across five toolchains, 471 evidence files, 5.4 MB, no host paths.
The full gate runs green including `-race` and end-to-end.

## Defects this uncovered

The new corpus broke the Make/Ninja equivalence test immediately: the Makefiles
adapter resolved **every** object to `compiler_depend.ts`, a CMake timestamp
file, instead of to a source. Two causes -- `looksLikeSource` was a blocklist
that accepted any prerequisite that was not an object, and CMake writes one
prerequisite per line for the same object, so the last one won. Fixed with an
allowlist of the compiler inputs of §14.6 and first-match-wins per object.

Second find, deferred to phase 2: `cmakeapi.ParseReplyDir` expected the
filenames `codemodel-v2.json` and `cache-v2.json`. Real CMake writes
content-addressed names and points at them from `index-*.json`, so the adapter
could not read a real build directory at all.
