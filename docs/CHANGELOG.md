# Changelog

## Unreleased

### The output is derived from the evidence graph

- Final deliverables are resolved per section 5: configured artifacts win, and
  automatic discovery is limited to installed executable targets reported by
  the CMake File API, never to the newest or only binary in the build tree.
  Debug byproducts such as `app.pdb` are not deliverables.
- Link evidence is read per section 11.2 with deviation D1: the linker
  dependency file names what the link consumed, the map names which archive
  members were extracted, and a Makefiles `link.txt` reconstructs the link
  command when neither exists.
- Extracted archive members are traced to the build-tree object they were
  archived from, so a member the linker never extracted has no path to the
  artifact and does not appear (section 12).
- Objects are resolved to sources through the strategies of section 13.2, and
  headers are attached from the Ninja deps log and Makefiles dependency output.
- The used-file set is now what is reachable from a deliverable. The same
  project built with `gcc-ninja`, `gcc-make`, `clang-ninja`, `arm-none-eabi`
  and `mingw-w64` yields the same three files, and `unused.c` -- compiled into
  the archive but never extracted -- appears in none of them.
- Graph invariants are checked before anything is written; a violation is an
  internal error with exit code 70.

### Fixed

- The linker map parsers were unusable against real map files. They scanned
  every line for anything containing a file extension, so GNU ld linker-script
  wildcards such as `*crtbegin.o(.ctors)` and lld section placements such as
  `foo.o:(.text)` were reported as archive members, and a single member was
  reported once per section it contributed. The parsers are now section-aware:
  the archive-member block, the as-needed block, the discarded-section block
  and the memory map are each read for what they actually contain, and lld's
  input column is parsed as a column. Sniffing now distinguishes GNU ld from
  gold by the memory map, which recent binutils is the only way to tell apart.
  `LOAD linker stubs`, which GNU ld writes for ARM veneers, is no longer
  recorded as a file.
- The Ninja deps log parser never stripped the NUL padding from path records:
  its trim condition was already false on entry. Every path whose length was
  not a multiple of four carried a trailing NUL, so header evidence never
  matched anything.
- Findings about the build no longer carry the directory the run happened to
  read from, which put a temporary path into every golden file.
- `LINKED_OBJECT_SOURCE_UNRESOLVED` is no longer reported for the C runtime
  startup objects, which section 24.1 excludes from the SBOM anyway.
- Paths that an adapter resolved against the directory being read are mapped
  back to the logical build root before being identified, so evidence from the
  Makefiles adapter anchors the same way as evidence from a map.
- `evidence.json`, which is sbomb's own output, was committed into the fixture
  corpus; the determinism script now works on a copy.

### Anchors and scope

- Added the section 7 anchor model: a registry with the registration order of
  section 7.4, longest-prefix matching at path-segment boundaries,
  case-sensitive matching under the POSIX flavor and case-insensitive under
  Windows, and the `abs` fallback with an `UNANCHORED_FILE` finding.
  `--redact-unanchored-paths` replaces unanchored identities with a digest.
- `toolchain:` anchors are registered from `toolchains-v1`, `sysroot:` from the
  cache or a `--sysroot` compile flag, and `extern:`/`sdk:`/`pkg:` from the
  configuration's `anchors[]`.
- Files are classified by origin scope and carry `sbomb:component:scope`.
  Toolchain and system files are excluded from the SBOM by default per section
  24.1, using the compiler-reported implicit include and link directories
  rather than a hardcoded path list. The dynamic loader in `/lib64` is covered
  by a conventional-root fallback, because no compiler reports that directory.
- Relative paths are now resolved against the directory they were recorded
  against -- a compile database entry against its `directory` field -- instead
  of being left unanchored.
- Identity uses the logical build path from the evidence (section 7.6) rather
  than the directory the evidence happens to be read from.

### Fixed

- `cmakeapi.ParseReplyDir` could not read a real reply directory. It expected
  the filenames `codemodel-v2.json`, `cache-v2.json` and `toolchains-v1.json`,
  while CMake writes content-addressed names listed in `index-*.json`, and it
  read `targets[]` from the codemodel although target detail lives in one file
  per target. It now follows the index, loads each target file, and exposes the
  source and build roots, install rules, link fragments and the implicit
  include and link directories. Reply filenames are rejected unless they are
  plain names, so a crafted index cannot read outside the reply directory.

### Fixture corpus

- The golden corpus is now generated from real toolchain output.
  `tools/fixtures/regen.sh` configures and builds five CMake projects for
  `gcc-ninja`, `gcc-make`, `clang-ninja`, `arm-none-eabi` and `mingw-w64`,
  then harvests only build evidence. Builds run under the sentinel roots
  `/__fixture_src__` and `/__fixture_build__`, so binary evidence
  (`.ninja_deps`, DWARF) carries portable paths natively.
- The previous corpus contained placeholder text such as
  `link trace for gcc-13/p02-static` in place of linker output.
- Toolchain directories were renamed to describe the build configuration
  rather than a compiler version: `gcc-13`/`gcc-12` to `gcc-ninja`,
  `gcc-12-make` to `gcc-make`, `clang-17` to `clang-ninja`. The hand-written
  `portable` fixture was replaced by real `gcc-ninja/p02-static` evidence plus
  `testdata/config/portable.json`.

### Fixed

- The Makefiles adapter resolved every object to `compiler_depend.ts` instead
  of its source. `looksLikeSource` was a blocklist that accepted any
  prerequisite that was not an object, and repeated `build.make` rules for one
  object overwrote an already resolved source. Source classification is now an
  allowlist of the compiler inputs in section 14.6, and the first resolved
  source per object wins.
- The tool version was hard-coded in three places with three different values
  while `scripts/release.sh` injected a fourth. All of them now read
  `internal/buildinfo.Version`, which release builds override via `-ldflags`.
- A global `-v` before a subcommand was appended to the subcommand's arguments
  and rejected as unknown, so `sbomb -v explain ...` failed. Verbosity is now
  parsed once and passed down.
- Tests wrote `evidence.json` into the committed corpus. They now operate on a
  copy via `testutil.CorpusBuildDir`.

### Added

- `.github/workflows/ci.yml` runs the universal gate (build, vet, test,
  gofmt), the race detector, the corpus checks and the end-to-end CMake test
  on every push and pull request. Previously only the determinism script ran.
- Golden files can be regenerated with `go test ./cmd/sbomb -update`, and test
  binaries pin the reported tool version so releases do not rewrite them.

### Removed

- `internal/findings`, an unused duplicate of `domain.Finding` whose
  `IsWaived` always returned false.
- `internal/adapters/linkers/{gnuld,gold,lld,msvc}`, four eleven-line packages
  that called `mapparser.Parse` with a fixed format string. The `msvc` one also
  contradicted milestone 18 being parked.
- Two `init()` functions that existed only to silence unused imports, and the
  unused `cyclonedx.versionString`.
