# Status

## Implemented

- Milestone 00: the fixture corpus is generated from real toolchain output by
  `tools/fixtures/regen.sh`, which builds five CMake projects across five
  toolchain configurations under the sentinel roots `/__fixture_src__` and
  `/__fixture_build__`. 25 fixtures, 469 evidence files, no host paths.
- Milestone 01: CLI skeleton, configuration validation, path canonicalization
  and CycloneDX generation.
- Milestone 02: evidence graph with confidence-aware edge deduplication,
  invariant checking, cycle detection and a dump format.
- Milestone 14: deterministic serialization, `SOURCE_DATE_EPOCH` handling,
  injectable POSIX/Windows path flavors, cross-architecture hash comparison
  in CI. Native Windows execution remains unverified.
- Adapters exist and are unit-tested for the CMake File API, Ninja, Make,
  compile databases, depfiles, linker maps, DWARF/ELF/PE and archives.

## Known Gaps

The single most important one:

- **Only two adapters are reachable from the binary.** `go list -deps ./cmd/sbomb`
  resolves 14 of 33 packages. The CMake File API, Ninja, DWARF, map-parser,
  inventory, component-mapping and version packages are built and tested but
  never called by `internal/generate`, so the evidence they can produce does
  not reach the SBOM. Closing this is the subject of roadmap phases 1-3.

Concrete defects found while building the real corpus:

- `cmakeapi.ParseReplyDir` expects the reply files to be named
  `codemodel-v2.json`, `cache-v2.json` and `toolchains-v1.json`. Real CMake
  writes content-addressed names such as `codemodel-v2-07769b617595cce5ba84.json`
  and lists them in `index-*.json`. The adapter cannot read a real build
  directory and must learn index-based discovery.
- The evidence graph is populated in parallel with the component list rather
  than being the source it is derived from, so there is no reachability filter
  from artifact to file -- the central premise of section 4.

## Next Work

Roadmap phase 1: complete the anchor model (section 7). Only `project`,
`build` and `abs` exist; `toolchain:`, `sysroot:`, `extern:`, `sdk:` and
`pkg:` are missing, and without them the real link evidence -- roughly four
fifths of which is toolchain and system paths -- cannot be classified per
section 24.
