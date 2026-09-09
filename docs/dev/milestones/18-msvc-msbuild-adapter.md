### Milestone 18 — MSVC / MSBuild Adapter

**Status: unparked.** Both conditions the parking named are met, and a survey of
the code found that the milestone as written overstated what is missing.

**[Amended.]** This read "parked by decision. Do not implement until explicitly
unparked", on three grounds: there is no Windows CI runner, PDB parsing is out
of scope, and the embedded targets that motivate this tool do not use MSVC. The
first is no longer true -- `.github/workflows/determinism.yaml` already runs on
`windows-latest`, which is the same amendment milestone 14 took in phase 8d. The
second is true and stays true, and the survey below shows it does not have to
block MSVC: on a single-config generator, headers come from `/showIncludes`
through Ninja's deps log and link evidence comes from the map, and neither is
debug information. The third is a statement about priority, not feasibility.
What is left is that MSVC is two milestones wearing one name, and they are
separated here.

**Goal:** a build produced by the Microsoft toolchain yields the same SBOM the
same project yields when built with GCC.

---

## What already exists

The tool was written with the Windows toolchain in view, so this does not start
at zero. Read out of the code rather than out of the plan:

| Piece | Where | State |
|---|---|---|
| PE/COFF inspection, PDB path and signature from the CodeView debug directory | `internal/adapters/binfmt` | Works. PDB *parsing* is out of scope, and a PE without DWARF reports `DEBUG_INFO_UNAVAILABLE` rather than failing |
| Windows path flavor: drive letters, UNC, backslashes, case-insensitive prefix matching, `--path-flavor windows` | `internal/pathmodel` | Complete, and exercised on Linux by the `win-synthetic` fixture |
| MSVC response-file quoting, selected from the compiler name (`cl`, `link`, `lib`, `rc`, `clang-cl`, `lld-link`) | `internal/respfile` | Complete, fuzzed |
| `.obj` recognised wherever `.o` is | `internal/generate` (`isObjectPath`, `linkgraph.go`) | Complete |
| Multi-config selection and `--config-name`, with `AMBIGUOUS_BUILD_CONFIG` when neither the flag nor `build.config` decides | `internal/adapters/cmakeapi` | Complete, and generator-independent: it reads the File API reply, which a Visual Studio generator writes too |
| `.ninja_deps` reader | `internal/adapters/ninja/deps.go` | Complete and deps-mode agnostic. Ninja resolves `deps = msvc` and `msvc_deps_prefix` into the same binary log, so `/showIncludes` output reaches the tool with no log parser at all |
| Header evidence policy `dwarf-preferred` | `internal/generate/headers.go` | Falls back per translation unit, so an object with no DWARF keeps its depfile headers instead of losing them |
| MSVC map format detection | `internal/adapters/linkers/mapparser` (`Sniff`, `FormatMSVC`) | Detection only. Parsing falls through to `parseGenericLine`, which records tokens ending in a known extension and nothing else |
| Archive member reading (`!<arch>`) | `internal/adapters/archive` | Present, and a `.lib` is the same container -- but it has never been run against one |

The corollary matters more than the table: **the Microsoft compiler driven by a
single-config CMake generator is largely supported already, and nobody has ever
run it.** Ninja and NMake Makefiles both export `compile_commands.json`, both
write the build graph the existing adapters read, and `cl.exe` under Ninja
writes its header dependencies into `.ninja_deps`. What is unproven is not the
design but the claim.

---

## 18A — MSVC on a single-config generator

**Goal:** `cmake -G Ninja` with `cl.exe`, and the same with `NMake Makefiles`,
produce a correct SBOM.

**Deliverables**

* An `msvc-ninja` toolchain in the fixture corpus (`tools/fixtures/regen.sh`),
  generated on `windows-latest` rather than in Docker, over the same projects
  the Linux toolchains use. This is what makes everything below testable.
* Sentinel roots of a Windows shape. `regen.sh` builds under `/__fixture_src__`
  and `/__fixture_build__` so captured evidence is portable natively; the
  Windows equivalent is a fixed drive-qualified root (`C:/__fixture_src__`),
  which the corpus tests -- `TestFixturesContainNoHostPaths` above all -- must
  accept as portable rather than reject as a host path.
* An MSVC map parser worth the name: the `Publics by Value` and static-symbol
  sections, the `lib:obj` column that carries member-level attribution, and the
  section table. Section-driven like the GNU and lld parsers, not
  `parseGenericLine`.
* `Sbomb.cmake` must ask for the map in the linker's own spelling. It probes
  `-Wl,-Map=` with `check_linker_flag` today, which fails under `link.exe` and
  silently drops map evidence; `link.exe` spells it `/MAP:<file>`.
* A recorded decision on what replaces `--dependency-file` and `-Wl,-t`, neither
  of which `link.exe` has. `/VERBOSE:LIB` names every library searched and
  extracted from; `/VERBOSE:REF` names what `/OPT:REF` removed, which the map
  does not list. Both are linker output, not a file the linker writes, so
  capturing them is a build-side change like the map is.
* `--path-flavor` chosen from the evidence, not from `runtime.GOOS` alone, when
  a Windows-shaped build is analysed from Linux.

**Tests**

* The corpus tests, extended to the new toolchain: every declared pair present,
  `PROVENANCE.md` parsable, no host paths, no third-party source.
* MSVC map parsing against a captured map: archive members attributed to the
  `.lib` they came from, `/OPT:REF` removals classed as discarded, and a
  truncated map yielding what was read plus an error (section 11.5).
* `.lib` member reading against a real MSVC static library.
* The header set for a `cl.exe` object comes out of `.ninja_deps` and classifies
  into the seven classes of section 14.4 from the implicit include directories
  `toolchains-v1` reports for MSVC.
* A golden SBOM for `msvc-ninja/p02-static`, generated on Linux from the
  committed fixture with `--path-flavor windows`.

**Acceptance**

```
go test ./internal/adapters/linkers/... ./tools/fixtures/... -race    # 0
sbomb generate --build-dir testdata/fixtures/msvc-ninja/p02-static/build \
  --path-flavor windows --output /tmp/ms.cdx.json --reproducible      # 0
cmp /tmp/ms.cdx.json testdata/golden/msvc-ninja-p02.cdx.json          # 0
```

**Definition of Done:** an MSVC-built project produces an SBOM whose used-file
set equals the GCC-built one for the same project, and the two documents differ
only where they genuinely differ -- artifact name, toolchain component, and the
debug-information finding.

---

## 18B — The Visual Studio generator and MSBuild

**Goal:** the generator most Windows projects actually use. It is a milestone of
its own because it writes no `compile_commands.json` and no `build.ninja`: there
is no build graph to read, only what MSBuild logged while it ran.

**Deliverables**

* `internal/adapters/msbuild`: `.vcxproj` reading (sources, configuration and
  platform), and `.tlog` parsing -- `CL.read.1.tlog`, `CL.write.1.tlog`,
  `link.read.1.tlog`, `link.write.1.tlog`, `RC.*.tlog` -- which supplies both
  the object→source mapping and the header dependencies.
* UTF-16LE-with-BOM decoding for `.tlog` files.
* `/showIncludes` build-log parsing as a fallback for a build whose `.tlog`
  files were cleaned, including localized "Note: including file:" prefixes; the
  prefix is taken from the log's own first occurrence pattern, never hardcoded
  English.
* Platform selection (`x64` against `Win32`) alongside the configuration
  selection the File API already provides.
* The adapter plugs in where the others do: `collectCompileEvidence` in
  `internal/generate/pipeline.go`, selected by which file the build directory
  has, as section 9.1 requires. It must name the strategy each mapping came from
  -- `msbuild-tlog` -- because section 13.2 attaches confidence to the strategy.

**Tests:** UTF-16 tlog decoding, including a file with no BOM and one truncated
mid-code-unit; localized `/showIncludes`; `x64` against `Win32` selection;
duplicate basenames across projects; a `.rc` resource input; a `.tlog` naming an
object no `.vcxproj` claims.

**Acceptance**

```
go test ./internal/adapters/msbuild/... -race                                 # 0
sbomb generate --build-dir testdata/fixtures/msvc-vs17/p02-static/build \
  --config-name Release --output /tmp/vs.cdx.json --reproducible              # 0
cmp /tmp/vs.cdx.json testdata/golden/msvc-vs17-p02.cdx.json                   # 0
```

**Definition of Done:** a project built with the Visual Studio generator
produces the same SBOM as the same project built with Ninja and the same
compiler.

---

## What stays out

* **PDB parsing.** Out of scope, and the consequence is stated rather than
  hidden: an MSVC artifact carries no DWARF, so section 13.2 strategy 6 -- ask
  the object -- never fires, and `--header-evidence dwarf-preferred` resolves to
  the depfile side for every unit. `DEBUG_INFO_UNAVAILABLE` is the correct
  answer, and the PDB path and signature are still recorded for identity
  correlation.
* **`clang-cl` and `lld-link`** are Microsoft-shaped command lines in front of a
  toolchain that does emit DWARF. They are not a separate milestone: whatever
  18A needs for `cl.exe` argument shapes serves them, and `lld-link /lldmap`
  writes the lld map the existing parser already reads.

**Ordering:** 18A first and alone. It is the smaller half, it is the half that
proves the Windows fixture pipeline works, and 18B cannot be tested at all until
a Windows fixture generator exists.

---
