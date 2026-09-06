# Phase 1 — The anchor model

**Status: complete.** Commit `d8c71a5`.

## Why here

Canonical paths are the node identities, the `bom-ref`s and the basis of header
classification. The specification requires `bom-ref`s to stay stable across
runs; changing them after the output binding is what breaks that promise, so
the anchor model comes before the output.

## Work

* **An anchor registry** with the registration order of §7.4 and resolution by
  longest prefix at a segment boundary, case-sensitive per path flavor.
* **The five missing anchor kinds**: `toolchain:`, `sysroot:`, `extern:`,
  `sdk:`, `pkg:` -- fed from `anchors[]` in the configuration and from
  `toolchains-v1` in the File API.
* **`UNANCHORED_FILE`** as a finding, plus `--redact-unanchored-paths`.
* **The §24 classification** that follows from it: system and compiler headers
  out, toolchain runtime as a separate component, `DYNAMIC_DEPENDENCIES_IGNORED`
  instead of silent omission.

## Result

Of the seven inputs in the real `app.d`, exactly two remain -- the object and
the archive; the five toolchain and system files are correctly sorted out.

## Defects this uncovered

**The CMake File API adapter could not read a real reply.** It expected fixed
filenames and read `targets[]` from the codemodel, where real CMake writes one
file per target. Every target came out empty. Rewritten around `index-*.json`
and the per-target files, and tested against all 25 fixtures.

`relativeTo` computed a byte prefix rather than whole segments, which broke
under case-insensitive Windows matching.
