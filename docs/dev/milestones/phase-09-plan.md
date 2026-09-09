# Phase 9 — The Microsoft toolchain

Working plan and durable state of the phase. Each step ends with a green gate
and its own commit.

Specification: §1.3 (supported environments), §9.1 (adapter selection), §11
(link evidence), §13.2 (object→source strategies), §14.4 (header
classification), milestone 18.

This phase adds the only mainstream toolchain the tool does not read. It starts
from an unusual position: most of the machinery is already there and has never
been pointed at a real Microsoft build. The first two steps therefore find out
what is true before anything is written.

**No step is done.**

---

## 9a — A fixture the corpus cannot produce today

`tools/fixtures/regen.sh` builds under the sentinel roots `/__fixture_src__`
and `/__fixture_build__` so the captured evidence records portable paths
natively, with no post-hoc rewriting. That trick is what makes the corpus
host-independent, and it is the first thing Windows breaks: a path there is
drive-qualified, so the Windows sentinel has to be `C:/__fixture_src__` and the
corpus tests have to tell a sentinel drive letter from a host one.

The step is the harness, not the parsing: an `msvc-ninja` toolchain built by
`cmake -G Ninja` with `cl.exe` on `windows-latest`, over the same projects the
Linux toolchains use, harvested by the same rules — metadata text, artifacts
built only from our own projects, a `PROVENANCE.md` per directory.

Two things are worth measuring before writing a line of adapter code, because
the whole shape of 9b–9d depends on the answers:

* whether `.ninja_deps` from a `cl.exe` build carries the headers, which it
  should: ninja resolves `deps = msvc` and `msvc_deps_prefix` into the same
  binary log the existing reader already parses;
* whether CMake's `compile_commands.json` under MSVC names outputs the existing
  strategy 4 can key on.

Gate: `tools/fixtures/regen.sh --check` passes with the new toolchain declared,
`TestFixturesContainNoHostPaths` still passes, and CI regenerates the Windows
fixture on `windows-latest` and fails on drift.

Where it starts, because a corpus toolchain is declared in more than one place:

| File | What it holds |
|---|---|
| `tools/fixtures/regen.sh` | the `TOOLCHAINS` array, `PROJECT_TOOLCHAINS` for pairs that make no sense, and `--check` |
| `tools/fixtures/fixgen.go` | `Toolchains()`, `SentinelSourceRoot`, `SentinelBuildRoot`, `NormalizeText`, `hostPathFragments` (which already rejects `C:\Users`) |
| `tools/fixtures/toolchains/*.cmake` | one toolchain file per cross build; a native `cl.exe` build needs none |
| `.github/workflows/ci.yaml`, job `corpus` | runs `--check` and the corpus tests on every push |

The two lists disagree today and it is not the Windows work that has to fix it:
`regen.sh` names thirteen projects, `fixgen.go` names five, so `AllPairs()`
checks a third of the corpus. Whichever list the new toolchain joins, it joins
both.

## 9b — The MSVC map, actually parsed

`Sniff` recognises the format and then hands it to `parseGenericLine`, which
records any token ending in a known extension. That is the same "scan every line
for anything path-shaped" approach the package comment says produces wrong
answers, kept only because nothing ever fed it a real map.

Replaced by a section-driven parser like the GNU and lld ones: the public and
static symbol tables, the `lib:obj` column that carries member-level
attribution, and the section table. Member-level attribution is the point —
section 11.2 wants to know which archive members were extracted, and on Windows
the map is where that is written down.

Gate: the parser reads the captured map from 9a; a truncated map yields what was
read plus an error (§11.5); the fuzz target covers the new sections; `.lib`
member reading is proven against a real static library rather than assumed from
the `!<arch>` magic.

## 9c — Link evidence without a dependency file

`link.exe` has neither `--dependency-file` nor `-Wl,-t`. `Sbomb.cmake` probes
for the GNU spellings with `check_linker_flag`, and when the probe fails it logs
that it is skipping map evidence and carries on — which under MSVC means every
build silently loses its strongest link evidence.

The linker's own spellings go in: `/MAP:<file>` for the map, `/VERBOSE:LIB` for
what was searched and extracted, `/VERBOSE:REF` for what `/OPT:REF` removed. The
last two are console output rather than files the linker writes, so capturing
them is a build-side change of the same kind the map is, and the CMake module is
where that belongs.

Gate: a fixture built through `Sbomb.cmake` on Windows has a map; the discarded
sections of §4.5 are populated from `/VERBOSE:REF` rather than left empty; a
project whose build refuses the flags still produces a document and says what
is missing.

## 9d — The first MSVC SBOM

Everything above is evidence collection. This step is the claim: the same
project, built with GCC on Linux and with MSVC on Windows, yields the same
used-file set. Anything that differs is either a real difference — artifact
name, toolchain component, `DEBUG_INFO_UNAVAILABLE` where GCC has DWARF — or a
defect, and the golden is what forces that distinction to be made explicitly.

Gate: `testdata/golden/msvc-ninja-p02.cdx.json`, generated on Linux from the
committed fixture with `--path-flavor windows`, plus a test that compares the
used-file set against `gcc-ninja/p02-static` and names every difference.

## 9e — The Visual Studio generator

Milestone 18B. The generator most Windows projects use writes no
`compile_commands.json` and no `build.ninja`; the object→source mapping and the
header dependencies come from what MSBuild logged, in UTF-16LE `.tlog` files,
with `/showIncludes` build-log parsing as the fallback for a build that was
cleaned.

Deliberately last. It is the largest piece, it is untestable before 9a exists,
and it is the piece most likely to be skipped entirely if 18A turns out to serve
the projects that actually need this.

Gate: `internal/adapters/msbuild` under `-race`; a Visual Studio build and a
Ninja build of the same project with the same compiler produce the same
document.

## 9f — Saying so

The support claims change only here, after the tests say they are true: §1.3 of
the specification, `docs/windows.md`, the README table, `docs/CHANGELOG.md` and
`docs/dev/status.md`. Until then the tool documents MSVC as planned, not
supported, because a document that overstates what was verified is the failure
mode this project exists to avoid.

---

## Acceptance

* [ ] A Windows fixture is regenerated on a Windows runner and CI fails on drift.
* [ ] The MSVC map is parsed by section, and archive members are attributed.
* [ ] `Sbomb.cmake` asks `link.exe` for evidence in `link.exe`'s spelling.
* [ ] An MSVC build and a GCC build of one project agree on the used-file set.
* [ ] A Visual Studio build and a Ninja build of one project agree, or 18B is
  parked again with what was learned written down.
* [ ] No support claim is made that a test does not back.
