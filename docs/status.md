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
- Sections 28 and 32.5: the document has a root component in
  `metadata.component`, grouping components, a closed dependency cascade, the
  `bom-ref` scheme of section 28.4 and the three BSI properties of section
  1.5(3). Both validation layers run on the exact bytes before they are
  renamed into place: the official CycloneDX 1.6 JSON Schema, embedded with
  `go:embed`, and the semantic checks a schema cannot express.
- Section 36.1: discovery hands a format-neutral `sbomwriter.Document` to a
  registered writer. Nothing below that package knows about `bom-ref` strings
  or CycloneDX property names.
- `sbomb validate` and `sbomb evidence` exist, and `sbomb schema --cyclonedx`
  prints the embedded schema.

## Known Gaps

- **Component metadata is missing.** Grouping components come from the anchor
  root alone -- strategy 7 of section 19.2 -- so they carry no version,
  supplier or purl, and the CRA field set of section 1.5(1) stays incomplete.
  The license table has two entries where it needs roughly seven hundred.
  Roadmap phase 4.
- **Most policy gates remain inert.** Two are wired end to end --
  `MISSING_LINK_EVIDENCE` and `MISSING_FILE_HASH` -- so `strict` and `lenient`
  now genuinely differ, but the remaining gates of section 33.1 have no
  findings to act on. Roadmap phase 5.
- **DWARF evidence is unused.** Section 11.4 makes it the primary header
  source and a strong object-to-source strategy; the adapter exists and is
  tested but is not called. Roadmap phase 6.

## Next Work

Roadmap phase 4: fill in what CRA compliance actually needs. The component
mapping strategies of section 19.2 beyond the anchor root, versions and purls
per section 20, suppliers from curated configuration or package metadata, and
the SPDX license hash table that `tools/spdxgen` is meant to generate.
