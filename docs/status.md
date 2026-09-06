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

- Sections 5, 11, 12 and 13: final deliverables are resolved from
  configuration or discovered from installed executable targets; link evidence
  is read from the dependency file and the map; extracted archive members are
  traced to the object they were archived from; objects are resolved to their
  sources through the strategies of section 13.2; headers come from the Ninja
  deps log and from Makefiles dependency output.
- **The output is derived from the evidence graph.** The used-file set is what
  is reachable from a deliverable, so a compiled source whose archive member
  the linker never extracted does not appear. The same project built with
  five different toolchain and generator combinations produces the same set.

## Known Gaps

- **The document shape is not yet CRA-conformant.** There is no
  `metadata.component`, the dependency array carries no `dependsOn`, and there
  is no embedded CycloneDX schema validation. That is roadmap phase 3.
- **Components, versions, licenses and suppliers are missing.** Files are
  emitted individually rather than grouped into components with a version and
  a supplier, so the CRA field set of section 1.5 is incomplete. Roadmap
  phase 4.
- **Most policy gates remain inert.** Two are wired end to end --
  `MISSING_LINK_EVIDENCE` and `MISSING_FILE_HASH` -- so `strict` and `lenient`
  now genuinely differ, but the remaining gates of section 33.1 have no
  findings to act on. Roadmap phase 5.
- **DWARF evidence is unused.** Section 11.4 makes it the primary header
  source and a strong object-to-source strategy; the adapter exists and is
  tested but is not called. Roadmap phase 6.

## Next Work

Roadmap phase 3: bind the graph to a CycloneDX document that a consumer can
use. A root component in `metadata.component`, grouping components, a real
dependency cascade with `dependsOn`, the `bom-ref` scheme of section 28.4, the
BSI properties of section 1.5(3), and the second validation layer of section
32.5 with the official schema embedded via `go:embed`.
