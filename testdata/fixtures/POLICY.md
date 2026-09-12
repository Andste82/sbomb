# Fixture Policy

- Only build evidence is committed, with one exception: for the FOSS fixture,
  the licence, notice and source files of the fixture's own dependencies are
  committed as well. They are original fixture material, not third-party
  payload, and without them no attribution behaviour can be tested. Their
  origin is recorded in `PROVENANCE.md` like everything else. No SDK content
  and no host paths, in either case.
- The exception is one directory: `p14-foss-src/`, the source tree of
  `p14-foss` harvested once rather than per toolchain, because the bytes do
  not depend on the compiler. `tools/fixtures/regen.sh --check` requires it to
  carry a licence file, so a corpus that lost it fails rather than passing
  while testing nothing.
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
- Three projects churn across regenerations for reasons outside this
  repository's control, and are named in `tools/fixtures/check-reproducible.sh`
  with the observation behind each.
- A fourth reason was removed rather than recorded. CMake serializes a target's
  transitive dependency list in the iteration order of a pointer-ordered set,
  so the array and every File API document named for its digest move between
  configure runs. `tools/fixtures/replynorm` sorts the array by target
  identifier and renames each document to the digest of what it wrote, leaving
  a reply the File API could itself have produced. `regen.sh` runs it for
  `p14-foss`; `p03-dupnames`, which has the same defect, still waits for a
  corpus-wide regeneration.
