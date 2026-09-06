# Deviations

Deviations from `spec.md`, recorded per specification section 0.2.
Each entry states what the specification assumes, what was observed, and what
the implementation does instead.

## D1 — Linker dependency files do not enumerate archive members

Section 11.3 treats the linker dependency file (`--dependency-file`) as the
highest-fidelity link evidence, listing "every file the link consumed,
including extracted archive members".

GNU ld 2.42 does not do this. For the `p02-static` fixture it records the
archive but not the extracted member:

```
app.d     libcrypto.a
app.map   libcrypto.a(crypto.c.o)   CMakeFiles/app.dir/main.c.o (aes_round)
```

Member-level attribution is available only from the linker map. The map
parsers are therefore a **required** evidence source for GNU ld, not the
fallback that section 11.2 places at priority 3. The dependency file remains
authoritative for the set of link inputs; the map supplies member granularity.

Consequence: `MISSING_LINK_EVIDENCE` must not be satisfied by a dependency
file alone when member-level resolution is required, and section 12's
`ARCHIVE_MEMBERS_UNRESOLVED` applies whenever only a dependency file is present.

## D2 — The race detector requires cgo

The universal gate in `docs/dev/README.md` requires `go test ./... -race`.
The race detector needs cgo, while section 37.1 requires release binaries to
build with `CGO_ENABLED=0`, which is also the devcontainer default.

The gate is therefore run with `CGO_ENABLED=1` in the dedicated `race` CI job.
Release builds continue to use `CGO_ENABLED=0`. Locally the same override
applies: `CGO_ENABLED=1 go test -race ./...`.

## D3 — The CMake File API index filename is not reproducible

CMake names the File API index `index-<configure timestamp>.json`. The
referenced reply files are content-addressed and therefore stable, but the
index filename changes on every configure.

Consumers must locate it by glob rather than by a fixed filename, which is also
how a real build directory behaves, and the adapter does.

The corpus no longer commits it under its real name. That made every fixture
churn on every regeneration, so `tools/fixtures/regen.sh` pins the name to the
fixture date -- keeping the shape the glob matches and the lexical-is-temporal
ordering the adapter uses to pick the newest of several. The deviation stands
for a real build directory; it no longer costs the corpus anything. See D17.

## D4 — ARM fixtures link freestanding

The devcontainer provides `arm-none-eabi-gcc` without newlib, so the
`arm-none-eabi` fixtures link with `-nostdlib -nostartfiles`. This is closer to
the bare-metal firmware builds the tool primarily targets than a hosted link
would be, so it is kept even where newlib is available.

## D5 — Fixture toolchain directories name a configuration, not a compiler version

Corpus directories are named `gcc-ninja`, `gcc-make`, `clang-ninja`,
`arm-none-eabi` and `mingw-w64`. They identify the build configuration the
adapters care about -- compiler family plus CMake generator -- rather than a
compiler version that silently goes stale when the container is updated. The
exact toolchain version is recorded in each fixture's `manifest.json` and
`PROVENANCE.md`.

## D6 — The CycloneDX schemas are draft-07, not draft 2020-12

Section 32.5 requires validation with "a pure-Go JSON Schema draft 2020-12
validator". The official CycloneDX 1.6 schema files declare
`http://json-schema.org/draft-07/schema#`, as do the SPDX and JSF schemas they
reference.

The embedded validator therefore has to support draft-07. The one in use
(`github.com/santhosh-tekuri/jsonschema/v6`) supports both, so the requirement
is met in substance: validation is in-process, pure Go, cgo-free and needs no
network. The draft named in the specification is simply not the one CycloneDX
publishes.

## D7 — Four of the eight component-mapping strategies are implemented

Section 19.2 lists eight component-mapping strategies. Implemented are the
curated configuration (1), the nearest directory carrying package metadata (6),
the anchor root (7) and the `unknown:` fallback with a review flag (8).

Strategies 2 through 5 -- Conan, vcpkg, FetchContent and git submodules -- were
added in roadmap phase 7, so a dependency a package manager installed is now
named from that manager's own metadata. **Closed**, except that CPM is not
covered: it is a CMake-level wrapper around FetchContent, and what reaches the
build tree is FetchContent's own evidence, which strategy 4 already reads.

## D8 — DWARF carries no inclusion depth

Section 14.4 asks for `sbomb:evidence:header:directInclude` "where the source
distinguishes direct from transitive inclusion (DWARF line table, MSVC
`/showIncludes` nesting depth)". The DWARF line-table file table does not carry
inclusion depth: it is a flat list of files, and DWARF 5 adds no depth field.

The property is therefore not emitted from DWARF evidence. It will become
available with the MSVC `/showIncludes` adapter, whose nesting depth is
explicit. Asserting directness from a flat list would be a guess, and the
property would then mean nothing.

## D9 — "DWARF is available for the CU" means it names a header

Section 4.4 narrows the depfile header set against the DWARF set "when DWARF is
available for the CU". Taken as "the unit has a line program", that rule
deletes evidence rather than refining it, because toolchains disagree about
what belongs in the line-table file table:

| Build | Unit | File table |
|---|---|---|
| gcc 13.3 | `main.c` | names `crypto.h` |
| gcc 13.3 | `crypto.c` | names no header, though it includes `crypto.h` |
| clang 18.1 | either | names only the primary source |
| gcc 13.3 | unity aggregation | names the three aggregated sources, no header |

A file table that names no header carries no header evidence, so this
implementation treats such a unit as *not covered*: it falls back to the
dependency file and emits `HEADER_EVIDENCE_FALLBACK`. Without that reading,
`p02-static` built with clang loses `crypto.h` entirely -- a project-owned
header that both the dependency file and the source demonstrably use.

## D10 — The precompiled header set comes from the compile command

Section 14.5 says every translation unit of a target depends on the entire PCH
header set, but does not say how the set is discovered. With CMake and GCC:

* the dependency file of an ordinary unit does not name `cmake_pch.h` at all;
* the PCH header set appears only in the dependency information of
  `cmake_pch.h.gch`, which is not an object and never reaches the linker;
* `compile_commands.json` records `-include .../cmake_pch.h` for every unit.

The forced-include flag is therefore the per-unit evidence, and the generated
aggregation header is parsed for its `#include` directives -- the same
deterministic, execution-free parse section 17.1 permits for unity sources.

"Reached only via the PCH" is decided by asking whether any compilation unit's
debug information shows the header contributing. A header the build forces in
and nothing uses appears in every dependency file and in no line table, which is
exactly the case `pchHeaders=exclude` is meant to remove.

## D11 — `pchHeaders=annotate-only` marks the evidence weak

Section 14.5 gives `pchHeaders` the values `include` (default), `annotate-only`
and `exclude`, and defines only the last. Both of the others include the
header, so the difference has to be in the annotation. Under `annotate-only`
the header-dependency edge is additionally recorded with strength `weak`, which
makes it visible to `failOnWeakEvidence` without removing anything. Under
`include` the edge keeps strength `derived` and carries only the
`sbomb:evidence:header:viaPch` property and the lower confidence that
section 14.5 prescribes.


## D12 — "Fully discarded" is decided over image-contributing sections

Section 4.5 says an object is fully discarded "only when the evidence source
enumerates all of its contributed sections and all are listed as discarded".
Taken over *all* sections, the rule never fires on a build with debug
information. GNU ld's map for an object whose every function was garbage
collected still shows:

```
 .eh_frame      0x00000000000020e8        0x0 CMakeFiles/gcapp.dir/deadcode.c.o
 .comment       0x000000000000002d       0x2e CMakeFiles/gcapp.dir/deadcode.c.o
 .debug_info    0x00000000000000ac       0x8a CMakeFiles/gcapp.dir/deadcode.c.o
```

None of those put a byte into the deliverable: `.debug_*`, `.comment`, `.stab*`
and the attribute blobs are not allocated, and a zero-length section
contributes nothing whatever its name. Retention is therefore decided over the
sections that actually reach the image. Both criteria are read from the map --
the section name and its size -- so nothing is inferred.

A `LOAD` line is likewise not evidence of retention: GNU ld writes one for
every input it opens, including the ones it discards entirely. Only the
placement lines of the memory map say what was kept.


## D13 — A Conan package root is not in the fixture corpus

The corpus commits build evidence and no sources, and a Conan package lives in
a cache outside the build tree. The committed `p11-conan` fixture therefore
carries the CMakeDeps files -- which is what proves name, version, purl and
package root -- but not the package itself, so the licence Conan copied into
`<package>/licenses/` cannot be read back from the corpus and the component
reports NOASSERTION there.

The path is exercised by unit tests instead, and on a real build tree the
licence resolves normally. Harvesting a package cache into the corpus would
mean committing third-party sources, which the corpus policy forbids.


## D14 — The native manifest accepted no absolute path

Appendix E states that manifest paths "are resolved relative to the project
root unless absolute", and what it rejects is a path that escapes *every
anchor*. The parser rejected every absolute path outright, which made the
format unusable for the common case: a build that writes its own manifest names
what it produced by full path, because a build tree separate from the source
tree cannot be addressed relative to the project root.

Absolute paths are accepted now. A relative path that climbs above its own root
is still refused, with `INPUT_LIMIT_EXCEEDED` as the specification requires,
and whether a path escapes every anchor is decided where the anchors are known
-- one that matches none is identified as unanchored and reported.

The defect was invisible because the manifest adapter was parsed for
validation only and never wired into the graph.


## D15 — Four finding identifiers were missing from appendix A

The catalogue in appendix A listed 48 identifiers. The implementation emits
four the specification never defined, although its normative text requires the
behaviour behind three of them:

| Identifier | Required by |
|---|---|
| `WEAK_EVIDENCE` | §8.4, and `policy.failOnWeakEvidence` appears in three profile tables -- a gate with no finding to gate on |
| `INTERNAL_INVARIANT_VIOLATION` | §8.8 and §28.4, both of which mandate exit 70 |
| `MISSING_COMPILE_EVIDENCE` | §14.1: the absence of a compile database has to be reportable |
| `MALFORMED_BINARY` | §11.4 covers a stripped artifact but not an unparsable one |

They were added to appendix A rather than removed from the implementation. The
gap was invisible until `tools/findingsdoc` began cross-checking the three sets
that have to agree: what the specification defines, what the code emits, and
what the user documentation lists. `go run ./tools/findingsdoc --check` runs in
CI, so it cannot reopen.


## D16 — A release ships no self-SBOM until it can be derived from evidence

Milestone 16 requires each release to carry "a self-SBOM generated by the tool
from its own build". What `scripts/release.sh` produced was generated from a
compile database the script wrote on the spot, naming a single Go file, beside
an empty linker map. That is not the tool's own build; it is the evidence-free
guessing sbomb exists to refuse, and publishing it would put a false bill of
materials next to the binary it claims to describe.

It also made every release build fail. The generated findings tripped the
default policy, `sbomb generate` returned exit 3, and `set -e` aborted the
script before it wrote `SHA256SUMS` -- which nobody noticed, because nothing
ever ran `scripts/release.sh build` in CI.

The fabricated generation is removed, and the self-SBOM is back, derived from
evidence: `sbomb self` reads the module record the Go linker embeds in the
binary. **Closed.**

The evidence source is not the one this entry first proposed. `go list -deps
-json` describes the working tree and would have to run as a subprocess outside
the allowlist of section 9.2; the linker's record describes the artifact, is
read from the artifact, and needs no subprocess at all. What it costs is
file-level detail: nothing in a Go binary names the source files that went into
it, so the module is the component. A Go module is versioned, licensed and
published as a unit, so that is the right granularity, and the document says
what it has rather than implying evidence it does not.

Two things a release SBOM would like are still absent, both deliberately. The
build carries `-buildvcs=false`, so no commit is recorded: enabling it would
make a build from a source tarball differ from a build from a git clone, and
reproducibility across environments is worth more than the field -- the tag
pins the commit anyway. And no component carries a CycloneDX hash, because a Go
module has no artifact to hash on its own; the `go.sum` entry covers a file
tree, not a file, so it is recorded as `sbomb:go:moduleSum` and
`MISSING_COMPONENT_HASH` is reported rather than suppressed.

## D17 — Three fixture projects are not byte-reproducible

Section 27 asks for determinism from sbomb. Nothing asks it of the fixture
corpus, but a corpus that changes on every regeneration makes a real corpus
change impossible to review: two runs of `tools/fixtures/regen.sh` over
unchanged sources rewrote around 150 files.

Most of that was ours and is fixed. The CMake File API index carries the
configure wall clock in its filename, so it is now pinned to the fixture date,
keeping the shape the adapter globs for. `.ninja_deps` records each output's
modification time, so `tools/fixtures/depsnorm` zeroes that field -- nothing
sbomb reads uses it; the parser decodes it and no caller looks. Ninja logs
dependencies in the order edges finish, so the builds run serially. The GNU PE
linker stamps a link time into every `.exe`, so the mingw toolchain file passes
`--no-insert-timestamp`. GCC draws a random seed per invocation and embeds it
in LTO sections, so `p09-lto` pins `-frandom-seed`.

Three projects remain, and in each the toolchain is what is not reproducible:

* **p03-dupnames.** CMake writes a target's dependency list in an unstable
  order, so `mod_a` and `mod_b` swap places in the File API reply. The reply is
  content-addressed, so its filename moves with its content, and the codemodel
  and index that name it move too. Sorting the array at harvest time would be
  post-hoc rewriting of the evidence and would break the content-addressed name.
* **p09-lto.** The linker map names GCC's temporary LTO objects,
  `/tmp/ccXXXXXX.ltrans0.ltrans.o`, drawn per invocation. That is what the
  fixture is for: section 17.3 downgrades attribution under LTO precisely
  because the map names temporaries rather than sources. The object files are
  stable now; only the map moves.
* **p11-conan.** Conan gives the cache folder of a locally built package a
  random suffix, and every generated file naming that folder moves with it.
  The cache is wiped per run so the corpus does not depend on host state;
  keeping it would trade this for a worse kind of unreproducibility. A
  deterministic layout via `conan install --deployer=full_deploy` would change
  what the fixture demonstrates and is not worth it for one project.

`tools/fixtures/check-reproducible.sh` regenerates twice and fails on any
difference outside those three, which are named in it rather than matched by
pattern, so a fourth cannot join them quietly.

## D18 — A fourth license detection technique: the SPDX template

Section 22.3 lists the permitted detection techniques "exhaustively" as three,
and the third of them recognizes only a verbatim text. Measured over 142
distinct real licence files, that recognized 32. The misses were not exotic:
`github.com/google/uuid` writes "Neither the name of **Google Inc.**" where
SPDX writes "the copyright holder" and bullets its clauses with `*` instead of
`1.`. Unmistakable to a person, unmatchable to a digest. Every dependency of
this tool failed, which is why the release script asserted their licences by
hand instead of deriving them.

The SPDX list publishes, beside each text, a `standardLicenseTemplate` that
marks the spans which may vary and gives a regular expression for each:

```
<<var;name="copyright";original="Copyright (c) <year> <owner>";match=".{0,5000}">>
<<var;name="bullet";original="1.";match=".{0,20}">> Redistributions of
```

Matching against it is added as technique 4. It is not the similarity matching
section 22.7 forbids: there is no score and no threshold, outside the marked
spans the comparison is exact, and which spans may vary is declared by the same
authority that publishes the text technique 2 hashes. It is nonetheless a
fourth technique where the specification said three, which is why this entry
exists rather than a silent change. Section 22.3 is amended.

**Measured, on the same 142 files:** 32 by digest, 55 more by template, 0
ambiguous, 55 unrecognized. The remainder are mostly not one verbatim licence
at all — dual licensing, a licence behind a preamble, bespoke agreements,
pointers to a licence elsewhere — where NOASSERTION is the right answer.

**What it costs.** The templates are 5.6 MB of text, 979 KB gzipped, embedded
with `go:embed` beside the digest table. The binary grows from 5.17 MB to
6.21 MB. Section 22.3 justified hashes-only with "under 50 KB, which keeps the
single-executable requirement unaffected"; that is a rationale, not a ceiling,
and section 37 asks for one file rather than a small one. The blob is
decompressed only after a digest lookup has already missed, so a run whose
licences are all verbatim never pays for it.

**Two things the implementation had to get right.** RE2 caps a bounded repeat
at 1000 and SPDX writes `.{0,5000}` throughout; 334 of the 739 templates fail
to compile without rewriting it. The bound has to be *clamped* rather than
dropped — with an unbounded variable, a two-clause template swallows a third
clause and a second licence after it, which is how a BSD-2-Clause template came
to match a BSD-3-Clause file. And translating all 739 templates takes 3.5
seconds, far too long for a run, so each is compiled only if a prefilter on its
longest invariant literal survives a substring search.
