# Phase 6 — Header evidence in full

**Status: complete.** Every step below is implemented, tested against the real
fixture corpus, and covered by the acceptance criteria at the end of this file.
The plan is kept as the record of what was built and why.

Specification: §4.4 (compile/include asymmetry, header evidence modes), §4.5
(dead code elimination), §8.7 (confidence adjustments), §11.4 (DWARF adapter),
§14.4 (header classification), §14.5 (precompiled headers), §17.1 (unity
builds), §17.3 (LTO effects), §34 (review report).

## Measured starting point

`binfmt.Inspect` on `gcc-ninja/p02-static/build/app` finds both compilation
units and reports **zero headers for each**. The extractor walks DWARF *line
entries* and records `line.File`, but a header that contributes only
declarations produces no line entry, and entries inside the main source are
filtered out as equal to the CU source. §11.4 point 2 asks for the line-table
**file table**, which does carry the header:

```
CU /__fixture_src__/main.c files:
  [0] /__fixture_src__/main.c
  [1] /__fixture_src__/main.c
  [2] /__fixture_src__/crypto.h
```

So DWARF header evidence has never produced a single header, and `binfmt` is
not reachable from the binary at all. Both are fixed here.

## Order of work

Each step ends with a green gate and its own commit, so an interruption never
loses more than one step.

### 6a — DWARF header extraction (`internal/adapters/binfmt`)

* Read the line-table file table per CU instead of line entries.
* Keep the CU source out of the header set; deduplicate; sort.
* Record whether the CU carried a line program at all, so a CU without one is
  distinguishable from a CU with no headers.
* Mark `FileReference.Direct` only where the source can prove it. DWARF carries
  no inclusion depth, so it stays unset — recorded as a deviation rather than
  guessed.

### 6b — Header classification (`internal/headers`)

The seven classes of §14.4, driven by the implicit include directories that
`anchors.Result` already carries from `toolchains-v1`, never by a hardcoded
path list. Default action per class per §14.4; `unknown-header` is included and
flagged, never dropped.

### 6c — Three evidence modes (`internal/generate/headers.go`)

`dwarf-preferred` (default), `union`, `depfiles`, per §4.4:

* per translation unit, prefer the DWARF set when the CU is covered;
* `HEADER_EVIDENCE_FALLBACK` (info) per uncovered TU under `dwarf-preferred`;
* headers dropped by narrowing are counted, not silently discarded, and the
  count reaches the review report (§34) and `--report-chains all`;
* confidence: DWARF-only `high`, depfile-only its normal `medium`, both `high`
  (§11.4).

### 6d — Precompiled headers (§14.5)

Detect `cmake_pch.*` aggregation sources and their objects. A header whose only
evidence path is through the PCH gets `sbomb:evidence:header:viaPch=true` and
confidence `medium`. `policy.pchHeaders`: `include`, `annotate-only`,
`exclude` — the last removes them and emits `PCH_HEADERS_EXCLUDED` with a count.

### 6e — Unity builds (§17.1)

Detect a unity TU (File API `UNITY_BUILD`, path under `CMakeFiles/*/Unity/`, or
a source consisting only of comments and `#include`s). Recover constituents by
parsing the generated file's `#include` directives — explicitly permitted and
deterministic — then by the depfile filtered to source extensions.
`UNITY_SOURCE_UNRESOLVED` when neither works.

### 6f — LTO and confidence downgrades (§17.3, §8.7)

LTO detected from flags or `.gnu.lto_*` sections downgrades `source-mapping`
confidence by one level with reason `lto`. Every downgrade is recorded in
`attributes.confidenceDowngrades` as a sorted list of reason codes.
`LTO_ATTRIBUTION_DEGRADED` when the linker reports only LTO temporaries and no
DWARF is available.

### 6g — Section garbage collection (§4.5)

The map parsers already emit `discarded-section` records. `annotate` sets
`sbomb:evidence:link:fullyDiscarded=true` and downgrades confidence one level;
`exclude` removes the object and emits `SECTION_GC_EXCLUDED` per removal. An
object counts as fully discarded only when the evidence enumerates all of its
contributed sections and all are discarded — partial information must not
trigger exclusion.

### 6h — Fixtures and report

Two new corpus projects, `p06-unity` and `p07-pch`, built by the existing
`regen.sh` matrix, so unity and PCH handling is tested against real generator
output rather than hand-written input. Review report gains the auditable line
`headers excluded by DWARF narrowing: N`.

## Acceptance

* `gcc-ninja/p02-static` yields `crypto.h` from DWARF, and the same document
  under `--header-evidence=depfiles`.
* A build without debug info emits `HEADER_EVIDENCE_FALLBACK` per TU and still
  produces the depfile header set.
* `p06-unity` resolves every constituent source of the unity TU.
* `p07-pch` marks PCH-only headers, and `--pch-headers=exclude` removes them
  with a counted finding.
* Full gate green: build, vendored build, vet, test, `-race`, gofmt, e2e,
  fixture check, determinism.


## What the corpus uncovered

Three defects, all invisible to hand-written test data:

1. **The DWARF adapter had never produced a header.** It walked line entries; a
   declaration-only header produces none. Section 11.4 asks for the file table.
2. **`ScopeOfPath` was handed canonical identities instead of paths** where
   source nodes are materialized, which re-anchored `project:crypto.c` against
   the build root and recorded its scope as `build`. The Makefiles golden had
   been asserting that.
3. **The corpus never harvested the generated unity and PCH files**, so neither
   construct could be tested against real generator output at all.

And three places where the specification had to be interpreted rather than
followed literally, each recorded in `docs/deviations.md`: D8 (DWARF carries no
inclusion depth), D9 (what "DWARF is available for the CU" has to mean), D10
(where the precompiled header set comes from) and D11 (`annotate-only`).
