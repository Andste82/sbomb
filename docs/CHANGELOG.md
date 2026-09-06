# Changelog

## Unreleased

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
