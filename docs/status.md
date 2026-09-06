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
- Milestone 03: the CMake File API adapter reads a real reply directory. It
  resolves the index, follows the per-target reply files, and exposes the
  source and build roots, the toolchain layout and the install rules.
- Section 7 anchors: a registry with the registration order of section 7.4,
  longest-prefix matching at segment boundaries, flavor-dependent case rules,
  the abs fallback with UNANCHORED_FILE, and --redact-unanchored-paths.
  toolchain: anchors come from toolchains-v1, sysroot: from the cache or
  --sysroot, extern:/sdk:/pkg: from the configuration.
- Section 24 scope classification: toolchain and system files are separated
  from project code using the compiler-reported implicit include and link
  directories, and are excluded from the SBOM by default.
- Adapters exist and are unit-tested for Ninja, compile databases, depfiles,
  linker maps, DWARF/ELF/PE and archives.

## Known Gaps

The single most important one:

- **The evidence graph is not the source of the output.** It is populated in
  parallel with the component list rather than being what the component list
  is derived from, so there is no reachability filter from artifact to file --
  the central premise of section 4. Consequently `unused.c` still appears in
  the `p02-static` SBOM even though its archive member is provably never
  extracted by the linker.
- **Link evidence is still unread.** The linker dependency file, the map and
  DWARF are parsed by tested packages that `internal/generate` never calls, so
  17 of 30 packages are reachable from the binary. The anchors can classify
  toolchain and system paths, but nothing feeds those paths in yet.

## Next Work

Roadmap phase 2: make the evidence graph the single source of the output.
Resolve the configured artifact, read link evidence in the preference order of
section 11.2 (with deviation D1: the map supplies archive members, the
dependency file does not), resolve objects to sources, and derive the used-file
set from reachability rather than from whatever the adapters happened to see.
