# Fixture Policy

- Only build evidence is committed: no source tree, no SDK content, no host paths.
- Every fixture is produced by a real toolchain via `tools/fixtures/regen.sh`,
  which builds under the sentinel roots `/__fixture_src__` and
  `/__fixture_build__`. Binary evidence (`.ninja_deps`, DWARF inside the
  artifacts) therefore contains portable paths natively.
- The sources the corpus is built from live in `tools/fixtures/projects/`.
- Toolchain directory names describe the build configuration, not a compiler
  version; the exact version is recorded in each `manifest.json` and
  `PROVENANCE.md`.
- One file is not byte-reproducible across regenerations: the CMake File API
  index is named `index-<configure timestamp>.json` by CMake itself. Consumers
  must locate it by glob, which is also how a real build directory works.
