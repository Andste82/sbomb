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
reference. CycloneDX 1.7 is draft-07 as well, so adding it does not change
this.

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
named from that manager's own metadata. **Closed.**

CPM is covered without an adapter of its own. It is a CMake-level wrapper around
FetchContent, so what reaches the build tree is FetchContent's own evidence and
the FetchContent adapter finds every CPM package already; a second adapter would
claim the same roots and turn one of the two away whole. Instead the lock CPM
writes, `<build>/cpm-package-lock.cmake`, is read as a second origin for the
package that adapter found, and the version it names is ranked against the
populate script's tag and the checkout's own answer per field (§21.1). Two
limits come with that, both deliberate:

- With `CPM_SOURCE_CACHE` set, CPM points FetchContent at a checkout under
  `${CPM_SOURCE_CACHE}/<name>/<hash>` and `_deps/<name>-src` is never created.
  The identity root then names a directory that does not exist, no file is
  attributed to the package and `PACKAGE_NOT_LINKED` says so. This is how the
  FetchContent adapter has always behaved; computing that path would mean
  reproducing CPM's own hash, which is a guess (§20.1).
- The copy of the lock a project commits to its source tree is not read. Its
  path is whatever `CPMUsePackageLock` was given, and its content is allowed to
  be older than the build tree being analysed, while the file in the build
  directory is rewritten on every configure and is therefore install state.

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

**Four things the implementation had to get right.** RE2 caps a bounded repeat
at 1000 and SPDX writes `.{0,5000}` throughout; 334 of the 739 templates fail
to compile without rewriting it. The bound has to be *clamped* rather than
dropped: with an unbounded variable, a two-clause template swallows a third
clause and a second licence after it, which is how a BSD-2-Clause template came
to match a BSD-3-Clause file. Optional blocks nest, three deep at the most in
46 templates, so the closing marker is found by counting; taking the first one
ends an outer block on an inner block's marker and truncates it silently, which
is why GPL-2.0 and LGPL-3.0 matched nothing at all until this counted. A
deprecated identifier is used only when nothing current matched, because
GPL-2.0 is the superseded spelling of GPL-2.0-only and matches every text it
does; reporting both would report SPDX's own renaming as a disagreement, and
the digest table already resolves collisions this way. And translating all 739
templates takes 3.5 seconds, far too long for a run, so each is compiled only
if a prefilter on its longest invariant literal survives a substring search.

**What is left, measured on the same corpus.** Of the 55 files that still yield
NOASSERTION: 24 contain no known licence text at all, being bespoke agreements
or pointers to a licence elsewhere; 18 contain several licence texts in one
file, which nothing in the permitted techniques resolves into one expression; 4
are `-only`/`-or-later` pairs, reported as an ambiguity with both candidates
named because the licence text genuinely does not distinguish them; and the
rest deviate from the SPDX text in ways SPDX does not declare, such as the
older Apache LICENSE saying `brackets "{}"` where the current text says
`brackets "[]"`.

## D19 — Licenses found in a file are evidence, not a conclusion

Section 22.7 lists five reason codes for NOASSERTION, and none of them fits the
commonest reason a licence file resolves to nothing: it holds *two* licences.
"Dual licensed under MIT or Apache-2.0" is two complete texts one after the
other with a sentence in between. Compared as a whole the file is neither, so
every technique of section 22.3 returns nothing — the worst possible answer,
because both licences are plainly there. Measured over 142 real licence files,
18 were of this shape and a further 15 held one licence with other material
around it.

Two claims have to be kept apart:

* **Which licence texts are present.** Establishable, and each one found is a
  complete text matched end to end — not a weaker match, a weaker *claim*.
* **How they relate.** Whether both apply or the recipient chooses is written
  in the prose between them. Reading that is the keyword heuristic section 22.3
  forbids, and getting it wrong is a compliance error rather than a cosmetic
  one.

Section 22.4 already requires evidence and assumption to be distinguishable,
and CycloneDX 1.6 models exactly this: `component.evidence.licenses` for what
was observed, `component.licenses` for what applies. So the licences found go
to the first, the second stays NOASSERTION with the new reason code
`license-composition-unresolved`, and the finding names them so a reviewer
knows what the question is. Curating `components[].license` fills in the
conclusion, and because the observation sits beside it the assertion can be
checked rather than believed.

The two forms are not interchangeable, which is the point: CycloneDX 1.6's
`licenseChoice` is a choice between a *list of licences* and a *tuple of
exactly one expression*. A list says which are present; an expression says how
they combine. An observation can only make the first statement.

**What CycloneDX 1.7 changes, and what it does not.** 1.7 relaxes
`licenseChoice`: one array may now mix licence objects and SPDX expressions.
That does not make the deviation obsolete, because the deviation is about what
an observation may *claim*, not about what the schema will *hold*. Two complete
licence texts in one file still say which licences are present and nothing
about how they relate, and at 1.7 they are still rendered as two identifiers.

What it does lift is a ceiling. Where an observation does carry a relation —
an `SPDX-License-Identifier` line reading `MIT OR Apache-2.0`, which states the
relation in the text rather than leaving a reader to infer it — 1.6 forced it
to be flattened to one identifier as soon as a second observation stood beside
it, and 1.7 does not. sbomb emits an expression there when written at 1.7, and
identifiers at both versions otherwise. Observation yields bare identifiers
today, so this changes no current output; it means the writer no longer has to
throw away a relation it was given.

**One thing the implementation had to get right.** Locating a licence inside a
larger file uses the template between its first and last fixed words, not the
whole template. BSD-3-Clause opens with the copyright variable, and an
unanchored search takes the earliest start that can work: the match began up to
a thousand characters before the licence did, swallowed the end of the licence
before it, and the two then overlapped so only one survived. A licence is
located by its fixed words; the variable material at its edges is not part of
the search.

**Measured, on the same 142 files.** Of the 51 that no whole-file technique
resolved, 8 now yield several licences as evidence and 15 yield one; 28 still
yield nothing, being bespoke agreements or pointers to a licence elsewhere.

## D20 — Four configuration keys are removed rather than implemented

A configuration could set `output.hashAlgorithms`, declare `generators[]`,
curate `components[].cdxType` or `components[].upstream` — and nothing read any
of them. They were not stray: all four are in the specification. They were
simply never built.

That is a worse state than not having them. Unknown keys are a configuration
error, which is a good property and one the documentation advertises; a reader
therefore concludes that an accepted key has an effect. `generators[]` was even
*validated* — a missing `output` field failed the load — so the file was
checked and then discarded.

Each is removed for its own reason:

* **`output.hashAlgorithms`** had exactly one admissible value. BSI TR-03183-2
  wants SHA-256 and sbomb computes SHA-256. A key with one legal value is not
  a setting.
* **`components[].cdxType`** was meant to override the CycloneDX type
  separately from `components[].type`. But `type` already passes through to
  CycloneDX unchanged, so it solved a problem that does not exist.
* **`components[].upstream`** was step 8 of the licence priority order of
  section 22.2. A repository URL settles a licence only if something fetches
  it, and this tool does not access the network. The step is not implementable
  as specified.
* **`generators[]`** was the fourth of four generator-input sources in section
  14. The first three — CMake custom-command dependencies, build-graph edges,
  the generator's own depfile — cover every well-behaved case. The fourth is an
  unverifiable assertion from a configuration file, in a tool that otherwise
  records only what it can prove. It comes back if a real build needs it, with
  a fixture showing the case and an evidence strength that says what it is.

`output.format` and `output.specVersion` stay and are now *checked* against the
one value each admits, so a file asking for a format that does not exist says
so. Selecting a format from them is the work of issues #1 and #3.

**`output.reproducible` is wired rather than removed.** Reproducibility is a
property of a project, not of an invocation: a project whose SBOMs have to be
comparable wants that of every run, not only of the ones where somebody
remembered the flag. `--reproducible` still forces it on for a single run;
neither can turn the other off.

## D21 — Unknown configuration keys are refused at every level, not only the top

The specification promises that a typo cannot silently disable a policy gate,
and the documentation repeats it. It was true only of top-level keys.
`{"policy": {"failOnMisingHash": true}}` and `{"policy": {"profil": "strict"}}`
both loaded without complaint, and the gate somebody believed was on stayed
off — which is the one failure a configuration file must not have.

The cause was the shape of the check: a hand-written allowlist of top-level
names, with `encoding/json` silently dropping everything unknown below it.
Loading now decodes with `DisallowUnknownFields`, so every object in the
document is checked, and the allowlist is gone.

This is stricter than before and can reject a file that used to load. That is
the point: a file it now rejects was a file whose author believed something
that was not happening.

## D22 — Nine specified command-line flags are removed rather than built

The specification's CLI table listed 43 flags; 20 existed. Unlike the
configuration keys of D20 nothing here was silently ignored — an absent flag is
refused with `unknown flag` — but a normative table describing a tool twice the
size of the real one is not a reference anybody can use.

Seven were built, because each mirrors a setting the configuration file already
had and a one-off run against somebody else's build tree should not require
writing a file first: `--source-dir`, `--mode`, `--config-name`, `--map`,
`--link-depfile`, `--image-manifest`, `--evidence-dump`.

Seven stay specified and absent, each waiting on the feature it belongs to
rather than on effort. They are named in section 32 beneath the table.

Nine are removed:

* **`--hash-alg`** — SHA-256 is the only value, and its configuration
  counterpart went with D20. A flag with one legal value is not a setting.
* **`--jobs`** — the performance budget of section 31 is met single-threaded
  and measured by a test. A worker-pool knob that changes nothing invites
  tuning that cannot help.
* **`--log-level`, `--log-format`** — `-v`, `-vv` and `-vvv` cover verbosity.
  Structured logs are for a service; this is a batch tool whose real outputs
  are already JSON.
* **`--absolute-paths`** — an absolute path in human output contradicts the
  anchor model, where the canonical identity *is* the path. It would also
  quietly undo `--redact-unanchored-paths` for anyone reading the report.
* **`--keep-raw-evidence`** — `evidence.json` already carries the source and
  adapter of every edge, which is what the flag was for.
* **`--compile-commands`, `--buildgraph`** — both are found in the build
  directory. A path override is a workaround for a discovery bug, and the fix
  for a discovery bug is to fix discovery.
* **`--depfile-mode`** — section 9.1 selects an adapter from what generated the
  tree. A switch that overrides that selection makes the answer depend on the
  operator rather than on the build.

## D23 — The evidence dump is no longer written unconditionally

`sbomb generate` wrote `<build-dir>/evidence.json` on every run, into a
directory the tool was pointed at but does not own. That default stays, because
`sbomb explain` reads the dump from exactly there and moving it would break the
documented workflow; but it is now a choice. `--evidence-dump <path>` puts it
somewhere else, and `--evidence-dump=off` leaves the build directory as it was
found.

## D24 — The configuration schema is derived from the types, not written beside them

`sbomb schema` published a hand-written JSON Schema. It named five of the
eleven sections the loader accepts — `project`, `build`, `mode`, `artifacts`,
`policy` — and set `additionalProperties: false`, so it actively rejected
`schemaVersion`, `output`, `anchors`, `discovery`, `components` and
`manifests`. Every documented example configuration failed against the schema
the tool itself hands out, and `docs/configuration.md` pointed readers at it.

A wrong schema is worse than no schema: it is machine readable, so it is
believed, and it fails a file that is correct.

It is generated from the Go types now (`internal/config/schema.go`). The
loader refuses unknown fields at every level (D21) and the schema says
`additionalProperties: false` at every level, from the same struct tags, so the
two cannot disagree about what a valid file is. What reflection cannot see —
the closed value sets `validate` enforces, and which fields are required — is
listed in two small tables beside the generator.

A test validates every committed configuration and a fully populated example
against the published schema, and checks that the schema refuses what the
loader refuses.

## D25 — Documented configurations are run through the loader

An example the tool would reject is worse than no example: somebody copies it,
gets an error, and concludes the tool is broken. `tools/docexamples` extracts
every fenced JSON block from the user documentation that looks like a
configuration and loads it exactly as a run would, filling in only the two
fields the loader insists on so that a fragment showing one section is judged
on that section.

It found one on its first run: the documentation described an artifact role
`firmware`, which the loader has never accepted. The roles are `application`,
`bootloader`, `library`, `filesystem`, `image`, `package`, `data` and `other`;
`firmware` is the CycloneDX *type* that `bootloader`, `image` and `filesystem`
produce.

## D26 — The property catalogue is checked, and two properties are renamed in it

Appendix B lists the properties an sbomb document may carry, and nothing
checked it. Both directions had drifted: eleven properties were written into
documents with no catalogue entry, so a consumer meeting `sbomb:go:moduleSum`
had nowhere to look it up; and thirty-nine were catalogued and never written,
describing a document sbomb does not produce.

`tools/propertydoc` closes the first direction the way `tools/findingsdoc`
closes it for findings: a property the code writes and the appendix does not
define fails the build. The second is handled the same way as the reserved
findings — the generated table in `docs/properties.md` marks each entry
`emitted` or `reserved`, so the catalogue stays whole rather than becoming a
snapshot of one version.

Two names in the appendix were not the ones the writer uses. The appendix said
`sbomb:file:path` and `sbomb:file:headerClass`; documents carry
`sbomb:path:canonical` and `sbomb:evidence:header:class`. The names in shipped
documents win — renaming them would break a consumer that already reads them —
so the appendix is corrected rather than the code.

## D27 — `CMakeLists.txt` is not a component-boundary marker

Section 19.2 listed "`CMakeLists.txt` with `project()`" among the files that
mark a directory as a component root. It is not implemented and should not be.

Deciding whether a `CMakeLists.txt` calls `project()` means interpreting CMake.
The call can come from a variable, from an `include()`, from a macro, from
inside an `if()`, or from a file another step generated. There is no reliable
static answer, and the failure mode is the wrong one for this tool: a false
positive **invents** a component rather than missing one, and an invented
component carries an invented boundary, an invented licence and an invented
name into a document somebody signs.

The bare existence of a `CMakeLists.txt` is no marker either, because every
subdirectory of a CMake project has one; using it would turn `src/` and
`src/drivers/` into components.

Two signals cover the same case without interpreting anything:

* **A recognized licence file** (§19.2). Existence check, no parsing. A
  directory carrying its own licence is a distinct work by convention, and a
  library copied into the source tree reliably has one even when it has no
  package manifest.
* **CMake targets**, which the File API reports directly
  (`cmakeapi.Target.Sources`). That is CMake's own statement about which source
  belongs to which target, not an inference about a text file.

## D28 — Component-mapping strategies 4 and 5 are still open

D7 records strategies 2 through 5 as closed and names them "Conan, vcpkg,
FetchContent and git submodules". Those are strategies 2 and 3 of §19.2. The
numbering in D7 is wrong: strategy 4 is the known SDK layout, which waits on
the SDK adapter, and strategy 5 is the explicit CMake target mapping from
configuration.

Strategy 5 is worth having on its own merit. `cmakeapi.Target.Sources` is
already parsed, so mapping a component to a target name is evidence the build
system states rather than a path prefix somebody has to keep in step with the
directory layout. It is the only mapping strategy here that does not infer
anything.

## D29 — Five allowlisted commands are removed, and the compiler probe stops asking where the compiler lives

§9.2 lists fifteen command shapes. Five of them could not be called by anything,
and unlike the rest they could not be called *in principle*:

* **`ninja -C <build-dir> -t deps`** — §9.2 names it as the example fallback and
  `NINJA_DEPS_UNAVAILABLE` as its finding, so it was the one this pass most
  expected to wire. It cannot be. The command reads `<build-dir>/.ninja_deps`,
  which is the same file sbomb reads, so it can only add something when sbomb's
  own parser fails — and that is exactly when it *writes*. Measured against
  ninja 1.11: a log with a header ninja does not accept produces `bad deps log
  signature or version; starting over` and the file is **deleted**; a log with a
  good header and a damaged record produces `premature end of file; recovering`
  and the file is **truncated and rewritten** (23 bytes in, 16 bytes out). With a
  log sbomb can already read, the command adds nothing and leaves the file byte
  for byte as it was. There is no case where it helps and no case where it is
  read-only when it would. A tool pointed at a build directory it does not own
  must leave it as it found it (D23), so the shape is gone; the missing evidence
  is still named, which is more than the old code did — the read error was
  swallowed entirely. `ninja -t inputs` and `ninja -t commands` were measured the
  same way and touch nothing: they do not load the deps log at all.

* **`cmake --version`, `cmake -E capabilities`** — their only specified purpose
  is §10.1: learning which File API kinds this CMake supports, so that a missing
  reply can be regenerated. The reply is written while CMake configures, and
  configuring is a build command that §9.2 forbids. The way out is
  `--allow-cmake-regenerate`, which is one of the absent flags (status.md). Until
  it exists, the answer to "which kinds does this CMake support" is one no code
  can act on: either the reply is there and is read, or it is not and
  `CMAKE_FILE_API_UNAVAILABLE` says so. The whole `cmake` group goes with them,
  including `build.introspection.cmake`, which is now an unknown configuration
  key.
* **`ninja --version`** — its use would be a version gate before `-t inputs`
  (Ninja 1.10 and later). That gate is not built and should not be: a `-t inputs`
  that fails is simply no answer, and spending a second process on asking whether
  the first will be permitted doubles the process count to learn nothing.
* **`git status --porcelain`** — the dirty state already arrives in the `-dirty`
  suffix of `git describe --tags --always --dirty`, which runs at all three call
  sites anyway. A second route to one answer is not more evidence, it is a second
  truth that can disagree with the first; and its output grows with the working
  tree while `describe` returns one line.

They come back with the feature that needs them, not before: the `cmake` group
belongs to `--allow-cmake-regenerate`, `ninja --version` to a version gate that
someone can show is worth its process, and `ninja -t deps` to a ninja that can
print a deps log without rewriting it.

The compiler probes stay, and to make them reachable one rule changed.
`RunCompilerProbe` used to require an *absolute* compiler path to lie inside a
registered anchor. §9.2 binds path *arguments* to the anchors; the program is not
an argument, and a compiler almost never lies in the project or the build tree —
so the rule refused `/usr/bin/cc` while permitting the bare name `cc`, which PATH
resolves to the same binary. It turned down the exact statement and accepted the
vague one, and left the group with nothing it could run. The program must now
exist; it need not be anchored. Every other bound is unchanged: the argv shape is
one of three, there is no shell, the environment is `PATH` and `LC_ALL` only, and
the invocation is recorded.

What the probes then supply is less than the documentation used to claim.
Measured against gcc 13.3.0, `-print-search-dirs` reports `install:`,
`programs:` and `libraries:` and no include directories at all. The group
therefore fills `ImplicitLinkDirs` (§24.1) and registers a `toolchain:` anchor;
`ImplicitIncludeDirs` (§14.4) stays a matter for the File API, and
`TOOLCHAIN_LAYOUT_UNKNOWN` keeps firing and now says what each way can answer.
The `-E -v -x c++ /dev/null` that §24.4 names for those directories would need a
new allowlist shape *and* reading stderr, which the runner discards on purpose;
both widen the boundary and are not part of this change.

## D30 — The `osPackages` group is removed with the two shapes behind it

§9.2 permits `dpkg -S <path>` and `rpm -qf <path>` "only when systemLibraries
adapter enabled", and §24.3 describes what they would buy: a distribution
library attributed to its package, with the purl
`pkg:deb/<distro>/<name>@<version>?arch=<arch>` or `pkg:rpm/...`. There is no
such adapter, and D29 has just removed five other shapes for exactly that
reason — an allowlist entry no code reaches is a permission granted for
nothing. These two were overlooked in that pass. They are removed now, together
with `build.introspection.osPackages` and the `osPackages` group of
`--allow-introspection`, which is now an unknown group and an unknown
configuration key. The example configuration object in §9.2 still shows
`"osPackages": false` beside `"cmake": false`; both keys are refused by the
loader, so that example cannot be copied as it stands.

Unlike the cmake group, this one is a real gap rather than a redundancy: no
file-based route answers "which package owns this library" either. So the
question was not whether the feature is wanted but whether the permitted shapes
can deliver it. Three findings say they cannot, and each would have to be
settled before the group comes back.

* **The shape does not answer the question.** Measured on Ubuntu 24.04, dpkg
  1.22.6: `dpkg -S /usr/lib/x86_64-linux-gnu/libssl.so.3` prints
  `libssl3t64:amd64: /usr/lib/x86_64-linux-gnu/libssl.so.3` — a package name and
  an architecture, no version and no supplier. The version needs a second,
  different shape, `dpkg-query -W -f='${Version}' <package>` (measured:
  `3.0.13-0ubuntu3.15`), and the `<distro>` namespace of the purl needs
  `/etc/os-release`. `rpm -qf` does return an NVRA, so name and version, but no
  supplier — and §24.3 names `rpm -qf --qf ...` for it, a third shape §9.2 does
  not list. An adapter built strictly on the allowlisted shapes would produce a
  component that still carries `UNKNOWN_VERSION` and `MISSING_SUPPLIER`: more
  processes, the same findings.
* **The runner would refuse the call.** A runner's anchors are the project root,
  the build root and the build directory (`generate.go`), and §9.2 requires a
  path argument to lie inside one of them. A system library lies outside all
  three by definition, so every such call ends in `ErrPathOutsideAnchors`.
  Reaching it means putting toolchain and sysroot roots into the subprocess path
  boundary — for `/usr/bin/cc` that root is `/usr`, so the boundary would move
  from the build tree to the machine. That boundary is what makes "no shell,
  fixed shapes, paths inside an anchor" worth stating; widening it in passing,
  for a feature that only runs under a policy overlay that is not the default,
  is the wrong order. If it is widened, it is its own decision with its own
  entry here.
* **Nothing can express the mapping.** All eight §19.2 strategies assign a file
  to a component by prefix or root — `packageFor` matches whole path segments
  against a package root — while an OS package owns files scattered across the
  filesystem. It would need a new strategy and a new `sbomb:component:detectedBy`
  value, plus namespace and qualifier support in `version.PURL`, which builds
  only `pkg:<type>/<name>@<version>`. That is a new abstraction, and the data
  above does not carry it.

A file-based substitute was considered and rejected. The dpkg file lists live in
`/var/lib/dpkg/info/*.list`, so reading them means walking a directory of
thousands of files looking for a path — a scan, and architecture.md does not
accept a scan as evidence. The rpm database is not readable without a new
dependency.

The gap this leaves is not silent, and was not silent before: a system library
that reaches the document already gets `UNKNOWN_VERSION` (warning),
`MISSING_SUPPLIER` (warning) and `UNKNOWN_PURL` (info) from `enrichComponent`. A
fourth finding announcing that a disabled group did not run would be a second
voice saying the one thing.

`policy.systemLibraries` is untouched. It decides whether a distribution library
is included, excluded or reported, it says only that, and the code does it. It
never claimed to say where the library came from.

The group comes back when three things are true: one shape per package manager
that answers name, version, architecture and supplier in a single call; a
decided answer to whether system paths belong inside the subprocess anchor
boundary; and a mapping strategy that can express one package per file. One
detail for whoever does that work: on a host build with `/usr/bin/cc`, the
toolchain anchor root is `/usr`, so `/usr/lib/...` is classified `toolchain`
rather than `system` and is decided by `includeToolchainRuntime`, not by
`systemLibraries`.


## D31 — A package knows several roots, vcpkg proves its files, and one more identifier was missing from appendix A

Three gaps in §19.2 strategy 2 and §21, closed together because they are the
same gap seen from three sides: a package could say only one thing about where
it lives, so the evidence it had went unused.

**A package had exactly one root.** For FetchContent that root is
`_deps/<name>-src`, while everything CMake generated for the package — a
`configure_file` header, the libraries built from the checkout — is in
`_deps/<name>-build`. Such a file matched no package and fell through to the
project component, which reports it as the manufacturer's own code. A package
now carries a list of roots, of which the first is the identity: the anchor,
the component root, the licence file and every `git -C` refer to it. §19.2 and
§21 are amended to say so.

Only the identity root becomes an anchor. §7.2 gives a package one version-free
key; a second key such as `pkg:fetchcontent/<name>-build` would name a package
that does not exist, and registering one key twice aborts the run. The further
roots lie inside the build tree and are already identified portably through the
build anchor.

**vcpkg's own file list was never read.** vcpkg merges every port into one
triplet tree: all headers in `include/`, all libraries in `lib/`. No root can
separate them, so the best metadata in the whole project — `vcpkg.spdx.json`,
with name, version, purl, licence and supplier — never reached the header that
used it, and the file fell through to the licence heuristic or the build
anchor. vcpkg writes one list per package under
`installed/vcpkg/info/<name>_<version>_<triplet>.list`, and reading it turns a
path guess into a lookup. That list is added to the §21 evidence table.

This is not the directory scan architecture.md refuses, and not the one D30
rejected for dpkg. There the file lists had to be walked in the thousands
looking for a path; here one list is addressed by the package's own name, the
same shape as the `share/*/vcpkg.spdx.json` glob the adapter has always used to
find the packages in the first place.

The list's format is **not verifiable in this repository**: there is no vcpkg
fixture, and there is no network. The unit tests therefore write the format they
assume, which makes them a test of that assumption and not of vcpkg. The
implementation is defensive to match: a missing `info` directory, no matching
list, or two matching lists all leave the attribution exactly as it was and the
document unchanged, without a finding of their own — the list improves an
attribution that works without it, so its absence is not evidence the run
expected. The findings are not unchanged, though: without a list a port's files
are again matched against `share/<name>`, which the headers and libraries the
build used are not under, so the package reaches no used file and
`PACKAGE_NOT_LINKED` below reports it — precisely the case the list exists to
fix. A list that *is* found but breaches a bound of §30 is a different matter
and reports `INPUT_LIMIT_EXCEEDED`; it is refused
whole rather than in part, because a truncated list would attribute some of a
package's files and leave the rest to the heuristics with nothing to say which
is which. Like the Conan package root of D13, the path is covered by unit tests
rather than by the corpus, which takes in no foreign installation tree.

**`PACKAGE_NOT_LINKED` was missing from appendix A.** A package that no used
file belongs to is correctly absent from the document — it was installed but
never linked, so it is not part of the product — but it was absent in silence,
and "why is the library I installed not in my SBOM" had no answer in the
findings. None of the 54 identifiers covered it: `PREBUILT_LIBRARY_UNMAPPED`
means a library with no mapping, `COMPONENT_ROOT_UNRESOLVED` a root that had to
be guessed. As in D15, the identifier is added to appendix A rather than removed
from the implementation. Severity is info and there is no gate, because the
omission is correct behaviour; only the silence was not.

One consequence to watch: a Conan or vcpkg install tree carries transitive
dependencies of which few are linked, so a real project's findings file grows by
one info per unlinked package. If that becomes noise, the alternative is one
aggregate finding per manager naming the packages in `detail` — but no name may
disappear in the process.

## D32 — Package metadata carries its origin, and the checkout outranks every declaration

§21 asked adapters to supply a name, a version, a purl, a licence hint and root
paths, and said nothing about where any of those values came from. The
implementation matched: `version` alone recorded a source and a confidence,
while licence, supplier and purl were bare strings.

That is enough only while exactly one file describes a package. It never was.
FetchContent already has two origins for a version — the tag in the generated
populate script and `git describe` in the checkout — and the code resolved them
by assignment order: `refineFromGit` overwrote whatever the script had said.
The right answer, by accident, for a reason nothing wrote down. Adding a third
origin, such as the manifest of the manager next door, would have been a guess
about which assignment should come last.

§21.1 is therefore added: each metadata value carries its origin and that
origin's rank, the highest rank wins, and equal ranks keep the value found
first. The specification had no such ordering — §19.2 orders mapping
strategies, §20.2 orders version sources, §22.2 orders licence evidence, and
none of them says which of two files describing one package to believe.

**Rank is not confidence, and this is the whole point of a separate ordering.**
§20.3 already grades how sure the tool is of a value. A tag in a generated
script is an exact declaration and rates high; a `git describe` with distance
rates medium. Ordering by confidence would publish the tag and contradict the
tree it claims to describe. Rank asks the other question — how authoritative is
the place this came from — and answers it the other way round.

**The working copy is ranked above every declared origin**, which is one rank
more than the four the origins themselves suggest. Without it the FetchContent
tag would beat `git describe` and the versions in the corpus would change: this
change is a change of data structure and nothing else, and every fixture
document and findings file is byte-identical to the release before it. The rank
is not a device to preserve output, though. A tag says which revision was asked
for; the checkout says which one is there, and whether someone has since edited
it. §19.4 already treats git metadata as the authority on a checkout.

**vcpkg's `vcpkg.spdx.json` is ranked as installed state, not as a bundled
SBOM.** vcpkg writes that document itself while installing; rank 4 is reserved
for an SBOM the upstream shipped inside its own package, which nothing reads
today. The distinction changes no output now and decides a winner as soon as
something does read one.

Contributions that lose are kept rather than dropped, and nothing reads them in
this release. Reporting that two origins disagreed is the next step, and a
report needs the loser: a value that was overwritten and forgotten cannot be
named later. Discovery is the only time they are collected, because packages
are copied by value once discovery returns.

One thing this deliberately does not do: when two *managers* claim the same
directory, `Discover` still drops the second package whole instead of merging
its claims into the first. Two managers claiming one tree disagree about who
installed the package, not about what its version is, and folding the loser's
metadata in would describe a package the winning manager never installed.

## D33 — Package-manager readers split in two: adapters that search, enrichers that describe

§21 knows one kind of reader. An adapter is asked for the packages it can prove,
so it enumerates and it searches: it looks in `_deps/`, in a triplet tree, in the
build directory, wherever its manager is known to keep things. Everything §21
says about a reader is said about that one.

That leaves a gap the mapping rules make visible. §19.2 strategy 6 finds a
component by walking up from a used file until it meets a marker file, and a
library copied into the tree meets nothing but its `LICENSE`. The component is
correctly found and correctly bounded, and then it is named after its directory
and nothing else: no version, no supplier, no purl. Whatever states them — a
`vcpkg.json`, a `Cargo.toml`, an SBOM the upstream shipped — is usually lying in
the very directory that was just resolved, and no reader in §21 is allowed to be
handed a directory rather than a manager's install layout.

An **enricher** is therefore added beside the adapter. It is given a directory
that has already been settled as a component root and returns what the files
directly in it state. It does not enumerate, does not search, does not descend
and does not walk up. Its return type is contributions and findings, with no
path and no root in it, so it cannot add a file to the used set and cannot move
a component boundary — the rule of §21 holds structurally for it, not by
discipline. The registry has a fixed order, because equally ranked claims keep
the first one found and the order therefore decides what a document publishes.

It is called at exactly two places: on the identity root of every package
discovery kept, and on the root of a component that only §19.2 strategy 6
found. Not on a further root of a package — those are build trees, and a
manifest read there would describe generated output — and not on a root taken
from the deepest common directory of the used files, because that root is a
guess and reading a file at a guessed location would promote the guess to a
source.

**A described value obeys the rank table of §21.1.** Enrichment of a package
runs inside `Discover`, after the owning manager has stated its own claims, so
an equally ranked enricher loses to the manager that installed the package and a
stronger origin — a bundled SBOM — wins and is named as the origin.

**Two orderings had to be decided now**, because the call sites fix them even
though no reader exists yet:

* **§22.2 point 3 above point 4.** The specification puts explicit component
  metadata above package-manager metadata, while §21.1 ranks a foreign manifest
  below installed state. They do not in fact collide: inside a package a manager
  owns, the manifest beside it is one more origin and §21.1 decides, which is
  the later and more specific rule; for a component no manager owns there is no
  manager metadata to lose to, and §22.2 applies as written — a declared licence
  beats the `LICENSE` file found in the same root.
* **Enrichment stands behind `versionFrom`.** §20.2 makes `components[].version`
  and `components[].versionFrom` curated configuration, which outranks every
  package-manager source. A `versionFrom` rule that finds nothing therefore
  leaves the version empty and `UNKNOWN_VERSION` is reported: the user said
  where the version is to be read from, and a manifest answering in its place
  would replace an instruction with a guess.

**A reader does not change a component's name.** Name and identity are settled
in `resolve` before the used files are grouped, and a manifest that names the
component differently from its directory would move a boundary rather than
describe one. The name stays the directory name even where a manifest states
another; changing that is a mapping question, not an enrichment one.

The registry was empty when this was written: both call sites, the interface and
the ranking around it came first, so that every reader added later is added in
one place and under one set of rules. The first reader is the bundled-SBOM
reader of D34.

---

## D34 — A bundled SBOM is read, and six things the specification leaves open had to be settled

§21.1 gives rank 4 to "an SBOM the upstream shipped inside the package, at the
package root" and says nothing further: not which file that is, not which part
of it may be read, and not what to do with a document that is none of those
things. The reader in `internal/adapters/pkgmanager/bundledsbom.go` had to
settle six questions, and each answer changes what a document says.

**vcpkg's own document is excluded by name.** `vcpkg.spdx.json` is an SPDX
document lying at a package root, and it is *not* an upstream SBOM: vcpkg wrote
it while installing the port, so it is that manager's install state at rank 3.
A generic reader that picked it up would read the same file a second time at
rank 4, beat the adapter that installed the package, and silently relabel the
origin of every vcpkg component in the document. The exclusion is by filename
rather than by asking who owns the root, because `ComponentRoot` carries a path
and a name by design and nothing else — and because the rule is right in the
other case too: a vcpkg tree reached through §19.2 strategy 6, with no adapter
involved, must not have that file read as an upstream statement either.

**The marker names are fixed and the reader's globs are not.** §19.2 strategy 6
recognizes four names by `stat`; the reader globs `*.cdx.json` and `*.spdx.json`
once in a root that is already settled. A directory listing in the marker walk
would be paid once per used file per ancestor directory, tens of thousands of
times over against the budget of §31, and the marker walk exists to bound
components rather than to find documents. The cost is real and is accepted: a
dependency shipping `mbedtls-3.4.cdx.json` and nothing else is described by that
document if something else already bounded its directory, and is not bounded by
it otherwise.

**SPDX 2.x only.** SPDX 3.0 is JSON-LD with a different shape; reading it by
pattern matching on an `@graph` would be exactly the guess this tool refuses. A
document declaring any other version is refused whole and reported, so that the
values it would have supplied are traceable to the version rather than absent
without explanation.

**The described package comes from `documentDescribes` or a `DESCRIBES`
relationship, never from `packages[0]`.** The vcpkg adapter may take the first
package because vcpkg's own layout guarantees the port comes first; a foreign
document guarantees nothing, and the file lying in a dependency's directory may
well be a product SBOM listing a whole delivery. A document naming several
described packages is refused rather than reduced to one of them. The single
exception is a document with exactly one package and no relationship at all,
which cannot mean anything else.

**A name in a document is never published.** It is read far enough to establish
that the document describes *a* package at all — a CycloneDX `metadata.component`
without a name describes nothing — and then dropped. D33 settles that a reader
does not change a component's name, and `Contribution` has no field for one. It
is deliberately not a gate either: directory names routinely differ from package
names (`dep/mbedtls-3.4` against `mbedtls`), so refusing a document over a
mismatch would throw away good evidence to enforce a convention nobody agreed
to.

**No JSON-Schema validation, and a licence array is read only where it is
unambiguous.** `internal/cyclonedx` compiles the 1.6 and 1.7 schemas, and
validating against them would refuse a perfectly readable 1.4 document, or one
that is invalid in a field this reader never looks at. "Schema-violating" is
therefore defined operationally: the document must parse, must declare its
format, and must name exactly one component it is about. A CycloneDX licence
array yields an expression, or a single licence object's SPDX identifier; a bare
`name` states nothing this tool will publish, because CycloneDX defines that
field as the licence that has no SPDX identifier and the claim would end up in
`licenses[].expression`, which the schemas describe as a valid SPDX expression.
Several licence objects without an expression state nothing either, because the
format does not say whether they apply together or the recipient chooses, and
§22.3 forbids deciding that by inspection.

**What this does not do.** A product-level SBOM that happens to sit in a
dependency's directory is read as if it described that dependency. There is no
reliable way to tell the two apart without gating on the name, which costs more
than it saves; the exposure is bounded to four fields, adds no file and moves no
boundary. `EVIDENCE_UNREADABLE` was added to appendix A for the refusals, rather
than stretching `INPUT_LIMIT_EXCEEDED` over a document that is simply not
parseable. And `pkgmanager.Options` carries no `limits.Config`, so
`--max-input-size` does not reach this reader any more than it reaches the
existing adapters: the bound is the package-level `maxSPDXBytes` the vcpkg
adapter already uses, which is what §30 requires by default but not what a user
who lowered the ceiling would expect. Both are recorded rather than fixed here.

---

## D35 — The ESP-IDF component manager is read without a YAML library, and four things the specification leaves open had to be settled

§19.2 strategy 2 names the ESP-IDF component manager as a source of exact
package-manager metadata, §20.4 prescribes `pkg:idf/<namespace>/<name>@<version>`
and §21 names `idf_component.yml` and `dependencies.lock` as its evidence. The
adapter in `internal/adapters/pkgmanager/espidf.go` had to settle four questions
the specification leaves open, and each answer changes what the document says.

**No YAML library, and a deliberately small reader instead.** Nothing in this
repository parses YAML and `vendor/` carries no parser, so reading these two
files meant either taking a dependency or writing one.
`internal/adapters/pkgmanager/idfyaml.go` is that reader: block mappings of
scalars, and a written list of the constructs it refuses — anchors, aliases and
merge keys, tags and the reserved indicators, a tab in the indentation, a flow
collection that does not close on its own line, a second document, a duplicate
key, a dedent to a column no enclosing mapping starts at, and a nesting deeper
than eight. Refusal aborts the whole file and is reported as
`EVIDENCE_UNREADABLE` naming it, because half a lock file is not a weaker answer
but an invented one. Two constructs are skipped rather than refused — a block
sequence and a block scalar, which real manifests carry in `targets:` and
`description:` — by indentation and without interpreting a line of them; a flow
collection that opens and closes on one line is skipped the same way, since its
extent is then unambiguous. Refusing those three would throw away files that
state a licence perfectly plainly two lines further down. **It is not a YAML
implementation and must not be reused as one**; if a second format arrives, the
question of a vendored parser is open again.

**The lock file ranks 3 and the component's own manifest ranks 2.** §21.1 makes
rank 3 "the installed state the owning manager recorded: file list, lock file,
resolved dependency" and rank 2 "the manifest the owning manager declares".
`dependencies.lock` is the first and `managed_components/<ns>__<name>/idf_component.yml`
is the second, so the resolved version wins and the manifest's own is retained
as the losing claim. Ranking both alike would have made the published version
depend on the order of the `Take` calls, which is exactly the coin toss §21.1
exists to prevent.

**The purl is built in the adapter.** `version.PURL` escapes `/` to `%2F`, which
is right for a purl type without a namespace and wrong for this one:
`pkg:idf/espressif%2Fled_strip@2.5.3` is not the form §20.4 prescribes. Widening
that shared helper for one adapter would touch every purl the tool writes, so
`idfPURL` escapes each segment on its own, in the shape `GenericPURL` already
uses. A component whose lock entry names no namespace becomes
`pkg:idf/<name>@<version>`; a namespace is never invented. The fallback in
`internal/generate/components.go` that derives a purl from an anchor key would
still produce the `%2F` form for a component that reached it, which no component
of this adapter does, since it always states a purl of its own.

**The component's published name carries its namespace.** `espressif/led_strip`
rather than `led_strip`: two namespaces may hold a component of the same name,
and `PACKAGE_NOT_LINKED` and the component identity both key on the name.

**What could not be verified here.** No fixture in this repository holds an
ESP-IDF project — milestone 20 is parked and the toolchain image layer is behind
`--build-arg WITH_ESP_IDF=1` — so three shapes are assumed rather than proven:
that `dependencies.lock` and `managed_components/` lie in the project source
directory, that a lock entry's key is `<namespace>/<name>` with its version under
`version:` and its origin under `source: type:`, and that the manager unpacks a
component into `<namespace>__<name>`. Where an assumption does not hold the
adapter finds nothing and says so — a component whose directory is missing is
`MISSING_PACKAGE_EVIDENCE` naming the directory that was looked for — rather than
attaching a component to a tree that might belong to something else. A captured
real project should be run against it before this is called finished.

**What this is not.** Strategy 2 only. `project_description.json`, `sdkconfig`,
`partitions.csv` and the IDF's own `components/` tree are the SDK adapter of
strategy 4, which is milestone 20 and still parked. The IDF's own components are
not managed packages and this adapter never looks at them.

---

## D36 — An image manifest is a third kind of reader, and it may describe but never enumerate

§21 knew two kinds of reader after D33: an adapter, which searches where a
manager keeps its packages and enumerates them, and an enricher, which is handed
a directory that already stands as a component root. A Yocto `license.manifest`
and a Buildroot `legal-info/manifest.csv` fit neither, and forcing one of them
would have cost the rule this package exists for.

**Not an adapter.** `Discover` drops a package whose identity root is empty, so
a metadata-only entry cannot enter that way at all. Giving each entry a root
would be worse than useless: the entries would claim files by path prefix, and
every one of them that matched nothing would be reported as
`PACKAGE_NOT_LINKED` — 797 info findings for the 800-package manifest of an
image in which this build links three libraries. An image manifest describes an
image; the packages it names that this build never touched are the normal case
and not a fault.

**Not an enricher either.** The return type is exactly right — `Enrich` hands
back contributions and findings, with no path and no root anywhere in the
signature — but the contract is not. An enricher is defined as reading the files
lying directly in a settled component root, and `applyEnrichment` refuses a root
with an empty path. This reader opens a file the user named, usually outside
every build tree, and matches it against a component by **name**.

So `internal/adapters/pkgmanager/distromanifest.go` is a third kind: a second
origin for components that already exist, keyed by name. It reuses
`Contribution`, `Claim`, `Take` and the ranking of §21.1 and adds no vocabulary
beyond the type itself. Because `Contribution` is a field plus a claim and
carries no path, no root and no anchor, the guarantee that an image manifest
cannot add a file, create a component or move a boundary is readable from the
signature rather than promised in a comment.

**Rank 3, and that is a decision.** §21.1 does not name this origin. The
manifest is what the image build recorded after building and installing, which
is the same category as a lock file or an installed-file list, so it ranks as
installed state: it loses to an SBOM the upstream shipped (rank 4) and to the
checkout (rank 5), and at equal rank it loses to the manager that installed the
package, because the reader is asked after the adapter. The consequence worth
stating out loud is at the other end: rank 3 **outranks** a declared manifest at
rank 2, so a project that configures both an image manifest and, say, an
`idf_component.yml` will publish the image build's answer. That is defensible —
the image manifest says what was built, a declaration says what was asked for —
but it is a change in what such a project publishes, so it is in the changelog
rather than left to be discovered.

**Matching is by exact name, and by nothing else.** The package name, and for
Yocto the recipe name as well, because a component in a build tree may be named
after either and the two differ whenever a recipe produces several packages.
Nothing is matched by path, by prefix, by resemblance or by stripping a `-dev`
suffix: a wrong match publishes a wrong version with high confidence, which is
the failure this tool exists to avoid. The honest cost is that files pulled out
of a Yocto recipe-sysroot are named by §19.2 strategy 7 or 8 — `unknown:sysroot:…`
— and no distribution package name will ever equal that. Such a component is not
described, and the way to connect one is `components[].name`, said so in the user
documentation. Guessing a package name out of a sysroot path is not an option.

**An unmatched entry is silence.** Not `PACKAGE_NOT_LINKED`, which is for a
manager that installed a dependency *for this build*, and not a finding of any
other identifier: the counts go to the log (§39.3). The alternative — a finding
per entry — would make the report unreadable for exactly the projects this
feature is for.

**A disagreement inside the manifests reuses `COMPONENT_MAPPING_CONFLICT`
rather than adding an identifier.** Two entries stating two versions for one
name are two statements and therefore none, as §19.2 already decides for a file
two packages claim; the field is dropped and the disagreement reported. Appendix
A's wording is widened from "the same file or root" to "the same file, root or
name" for it. A new identifier would have said nothing the existing one does not.

**The find location is a configuration key of its own, `distroManifests`, and
not `cfg.Manifests`.** That list is the native manifest of appendix E and
nothing else: it is parsed by `internal/adapters/manifest.ParseFile`, so a Yocto
file placed there is read as JSON and reported as `MISSING_PACKAGE_EVIDENCE`, by
two separate call sites. The shape is copied — a `[]string` resolved against
`cfg.Project.Root` when relative, plus a repeatable flag appended after the
configuration is loaded — and the mechanism is separate. `--distro-manifest` is
likewise a flag of its own, because `--image-manifest` is taken and one word for
two formats would make both unreadable.

**Buildroot's `host-manifest.csv` is not refused by name.** The bundled-SBOM
reader skips `vcpkg.spdx.json` by name because it *searches* a root and would
otherwise pick that file up on its own. Nothing is searched for here: every file
opened was named by the user, and silently refusing a file somebody explicitly
configured would be worse than reading it. The warning that the host manifest
describes the build machine rather than the image is in the user documentation
instead.

**What could not be verified here.** No fixture in this repository holds a Yocto
or a Buildroot build, no section of the specification defined either format
before this change, and `encoding/csv` was used nowhere in the repository. Two
shapes are therefore assumed rather than proven: that a `license.manifest` is
blank-line-separated blocks of `PACKAGE NAME` / `PACKAGE VERSION` /
`RECIPE NAME` / `LICENSE`, and that `manifest.csv` is a CSV whose header names
`PACKAGE`, `VERSION` and `LICENSE` among its columns. The format is recognized
from the first non-empty line rather than from the file name, so a manifest
copied out of a deploy directory under another name still works; where an
assumption does not hold, the file is refused whole and reported as
`EVIDENCE_UNREADABLE` naming it, rather than half-read into claims nobody can
trace. A captured real image build should be run against this before it is
called finished.

**One known gap.** `pkgmanager.Options` carries no `limits.Config`, so
`--max-input-size` does not reach these bounds any more than it reaches the
vcpkg file list or the CPM lock. The new ceilings are file-local constants
beside the existing ones. A user who lowers the run limit does not lower this
one; that is the status quo for every package-manager parser and is left as a
known gap rather than fixed halfway here.

## D37 — pkg-config is a fourth kind of reader, and the mapping is the whole contribution

D30 removed the `osPackages` group and named three conditions for anything to
take its place: a form per manager that yields name, version, architecture and
supplier in one call; a decision about system paths inside the subprocess anchor
boundary; and a mapping strategy able to express one package per file. A `.pc`
file meets the first from a file rather than a call, and needs the third. The
second falls away entirely, because no process starts.

**Not an enricher, although the task asked for one.** An enricher is invoked at
exactly two places — on the identity root of a discovered package inside
`Discover`, and on a root whose `Source` carries the `marker:` prefix — and a
system file's component has `Source` `anchor`, so no enricher would ever run for
it. Worse, an enricher describes a root that is already settled, and for a file
under a sysroot that root is the sysroot anchor itself, under which *every*
system file of the run falls into one component. A .pc file describes exactly
one package; hanging it on that collection component would be a false statement
about all the others. And an enricher reads what lies directly in the root,
while a .pc file lies in `<libdir>/pkgconfig`. The mapping, not the description,
is the contribution here — without it there is nothing correct to describe.

**Not an adapter either, for D36's reason.** An adapter enumerates: every .pc
file in a sysroot would become a package with a root, would claim files by path
prefix, and every one that matched nothing would be reported as
`PACKAGE_NOT_LINKED`. A Debian `pkgconfig` directory holds hundreds of them.
`Discover` also runs *before* the anchors are assembled, so an adapter does not
yet know where the sysroot is or where its boundary lies.

So `internal/adapters/pkgmanager/pkgconfig.go` is a fourth kind of reader: handed
a used file and the anchor that bounds it, answering with a module name and
contributions. As with an enricher and with D36's image manifest, the guarantee
is structural — `SystemPackage` carries no path, no root, no anchor and no file
list, so it cannot widen the used set or move a boundary however the caller uses
it.

**Named addressing, not a directory index.** From `libfoo.so.3` the candidates
`foo.pc` and `libfoo.pc` are formed and `stat`ed one at a time; from
`<includedir>/foo/bar.h` the candidates `foo.pc`, `libfoo.pc`, `bar.pc` and
`libbar.pc`. Nothing enumerates a `pkgconfig` directory. That is the line D31
drew for the vcpkg file list and the one D30 refused to cross for
`/var/lib/dpkg/info`, and it is also what §31 can afford: this runs once per
used system file. The price is real and is paid deliberately — `zlib.pc` is not
found for `libz.so.1`, `openssl.pc` not for `libssl.so.3`, and a multiarch .pc
file under `/usr/lib/<triple>/pkgconfig` is not found for a header in
`/usr/include`. Those libraries stay in the sysroot component, exactly as they
are today. An index would find them and would also let the size of the sysroot,
rather than the size of the build, decide how long the run takes and what the
document says.

**The module comes from the file name, not from `Name:`.** Measured:
`xkeyboard-config.pc` states `Name: XKeyboardConfig`, and `libcrypt.pc` is a
symlink to `libxcrypt.pc` whose `Name:` says `libxcrypt`. The file name is the
identity pkg-config itself keeps the module under and the only name this reader
ever addressed; `Name:` is a display name, and publishing it would rename a
component after a second string in the same file — which D33 already refused for
a bundled SBOM.

**Verification is what makes this not a guess.** A name that matches is not
evidence; every candidate has to agree with the filesystem before anything is
mapped. For a library: a stated library directory must *be* the file's directory
and a `-l` name must name the file. For a header only an include directory
containing it, which is weaker — that is why the directory the header sits in is
tried as a module before the header's own stem, and why a header in
`/usr/include` with a same-named .pc file beside it is the shakiest case this
reader has. It is bounded by verification against a real path either way, and a
wrong answer here costs a name and a version on a `build-environment` component
rather than a product dependency.

**Sysroot prefixing is an interpretation, and it is named as one.** A .pc file
states the paths the package will have on the target (`libdir=/usr/lib`) while
in a cross build the whole tree lies under the sysroot. Prefixing the anchor root
before comparing is what `PKG_CONFIG_SYSROOT_DIR` does; the file itself says
nothing of the kind. Without it nothing would ever verify in a cross build. The
unprefixed form is accepted too, for a toolchain that rewrote the paths itself,
but only while it stays inside the anchor.

**Expansion is lazy where pkg-config's is eager.** pkg-config substitutes a
variable as it reads the file, so a forward reference stays empty there and
resolves here. The difference can only make this reader see *more*, never
something different: whatever it resolves still has to verify against a real
path. What it buys is a bound that is enforced rather than accidental — a fixed
depth and an explicit cycle check, both required by §30, where eager
substitution would have made the depth constant and unenforced.

**No new finding identifier.** `COMPONENT_MAPPING_CONFLICT` already covers a
file two sources claim and nothing gets, `INPUT_LIMIT_EXCEEDED` the bounds and
`EVIDENCE_UNREADABLE` a file that cannot be read. Appendix A is unchanged, and
`findingsdoc --check` passes without an edit.

**No purl, and no root.** §20.4 wants a package type that can be asserted; a .pc
file names neither a distribution nor a package format, so `pkg:generic` would be
invented and `UNKNOWN_PURL` (info) stays. The component gets no root either,
under a `Source` of its own so that `COMPONENT_ROOT_UNRESOLVED` does not fire for
a root nobody was looking for: `/usr/lib` is where the package installed its
libraries, and taking it for a component root would start a licence search across
the whole sysroot for every distribution library in the document.

**Two answers are no answer, and identity is part of the comparison.** Two
verified candidates are compared on module, `Name:` and `Version:` together,
because the module is what the component is named after. Two candidate
*directories* carrying the same file — `/usr/lib/pkgconfig` and
`/usr/share/pkgconfig` — therefore agree and answer once, while `crypt.pc` and
`libcrypt.pc` sitting side by side disagree about the name even when they agree
about the version, and map nothing. The real `libcrypt.pc` / `libxcrypt.pc` pair
is not a case at all: `libxcrypt.pc` is never a candidate for `libcrypt.so`,
because candidates come from the used file's name and from nothing else.

**And the same comparison a second time, over the component.** One module name
can be mapped from several used files, and a sysroot can hold two packages of
one name — a distribution `libfoo` in `/usr/lib` and a hand-built one in
`/opt/lib`. Each file is verified against the .pc file beside itself, so each
answer is right for its own file, but they name one component. Collecting them
in a map keyed by module and letting the last write win would publish a version
that is wrong for one of those files and say nothing about it — the one place
this feature could have guessed without noticing. So the answers are compared
exactly as two candidates for one file are: equal is one answer given twice,
different drops the value and is reported as `COMPONENT_MAPPING_CONFLICT` (info)
against the component. The mapping itself stands — both files do belong to a
package of that name — and the component keeps what the rest of the run knows
about it, which for a system library is a name and no version. The sides are one
per distinct answer rather than one per file, because a module can describe
hundreds of headers and a report naming every one of them would say the same two
things over and over.

**What a component was described as is collected per grouping pass, not per
run.** `resolve` is asked about files that narrowing later removed from the used
set, and a description that came from such a file would decide a component's
version — or manufacture a disagreement — out of a file the document does not
contain. The collection is therefore emptied where the findings are emptied,
when the files are grouped, and every file that is mapped records its answer
again from the memo. The memo of what was read survives, so no .pc file is
opened twice (§31).

**The reader is a field on the resolver rather than a setter.** D36's image
manifest reaches the resolver through `setDistroMetadata`, because it is read
once in `generate.go` before the anchors exist. This reader needs nothing from
the run, so it is constructed in `newComponentResolver` beside `enrich`, which
keeps it injectable for a test and keeps its per-run cache tied to the resolver's
lifetime. `generate.go` is untouched.

**What this is worth depends entirely on the policy, and that has to be said
plainly.** With the default `systemLibraries=exclude` and
`includeSystemHeaders=false`, system files are filtered out of the used set
before components are formed, so nothing is read and nothing changes. This
matters under the `host-linux` profile, or wherever those options are set
deliberately. Where it does apply it is a visible output change: what used to be
one component per sysroot becomes one per named package, with a version.

**What could not be verified here.** No fixture in this repository holds a
sysroot, and there is no network. The format is grounded in the nine .pc files
present on the machine this was written on —
`/usr/lib/x86_64-linux-gnu/pkgconfig/{libcrypt,libxcrypt,valgrind}.pc` and
`/usr/share/pkgconfig/{adwaita-icon-theme,bash-completion,shared-mime-info,systemd,udev,xkeyboard-config}.pc`.
Neither a backslash continuation nor a literal `$$` occurs in that corpus, so
what this reader does with them is an assumption the tests record rather than
pkg-config's behaviour, exactly as D31 disclosed for the vcpkg file list. A `#`
is treated as a comment only at the start of a line, which is conservative: a
`#` inside a value is kept. A captured real cross-build sysroot should be run
against this before it is called finished.

**One known gap, the same one D36 records.** `--max-input-size` does not reach
these ceilings; they are file-local constants beside the existing ones.

---

## D38 — A CMSIS pack descriptor is read, and the purl question is answered by not answering it

§21 gained a second enricher: the `*.pdsc` an MCU vendor ships inside a
CMSIS-Pack, read by `internal/adapters/pkgmanager/cmsispack.go`. For a vendor
pack that file is usually the only place a version and a supplier exist at all.
Five things the specification leaves open had to be settled, and each answer
changes what a document says.

**No purl, and `UNKNOWN_PURL` stays.** The purl specification registers no
`cmsis` type, so `pkg:cmsis/ARM/CMSIS@5.9.0` would be an identifier nothing can
resolve — the invented value this tool refuses. Borrowing the `pkg:generic` row
of §20.4 is worse rather than better: that row belongs to packages derived from
a repository checkout, and its `?vcs_url=` qualifier is the only part of it that
identifies anything, which a pack has nothing to put in. §20.4 now says so in a
row of its own rather than leaving the question to its `Otherwise` row, so that
the next reader does not reopen it.

**The newest release is the first `<release>` element, not the largest version
string.** The pack schema requires the release history in descending order, so
the first element is a stated fact; ordering these strings here would mean
inventing comparison semantics for a versioning scheme nothing in this
specification defines — a guess dressed as arithmetic. Where the install layout
disagrees with the history, rank settles it and the loser is kept in
`Superseded`, which is what the provenance model of §21.1 is for.

**`<license>` contributes no licence.** In this format the element holds a
*path* to a licence file inside the pack — `LICENSE.txt` — and not an SPDX
expression. Publishing it as one would put a filename into
`licenses[].expression`, which both bundled schemas describe as a valid SPDX
expression. Its only honest home would be `Package.LicenseFile`, which is a
path, and D33 keeps paths out of the `Enricher` signature structurally, so an
enricher cannot set it. Little is lost in practice: a pack's licence file lies
at the pack root under exactly the names §19.2 already looks for, so the
component keeps getting its licence from there. The newer schema's
`<licenseSets>/<license spdx="...">` attribute would be a real expression, but
it could not be verified against a `PACK.xsd` here, and a field is not added on
a recollection.

**No pack-root discovery, so this is an enricher and not an adapter.** Every
adapter addresses a path under `BuildDir` or `SourceDir`. A CMSIS pack root is
`CMSIS_PACK_ROOT` or a per-user cache; this repository reads no environment
variable for a location — only `PATH` and `SOURCE_DATE_EPOCH` — and searching
for one would be the downward scan architecture.md refuses outright. What the
pack layout does give is an unambiguous reading *upwards from a descriptor
already found*: `<Vendor>/<Name>/<version>/<Vendor>.<Name>.pdsc`. That is a
description of a root somebody else settled rather than an enumeration, so it
lives inside the enricher as a rank-3 version claim, costing no directory
listing and no second file read. It is the one place here where a directory
name rather than a statement decides a value; it is confined to a version claim
on a settled root, all three names have to agree with the descriptor before it
counts, and anything stronger supersedes it. If pack-root discovery is ever
wanted, the honest shape is a configured path in the manner of D36's
`distroManifests`, where every file read was named by the user.

**The safety of the XML reader is four fields that are never assigned.** This is
the first use of `encoding/xml` in this repository. `Decoder.Entity` stays nil
and `Decoder.Strict` stays true, which is what turns a billion-laughs bomb and
an external `SYSTEM` entity into a syntax error instead of an expansion or a
file read; `CharsetReader` stays nil, so a document declaring ISO-8859-1 is
refused rather than reinterpreted under a guess; `AutoClose` stays nil. That
safety is invisible in the code, so the tests assert the *refusals* rather than
any value, and a later edit that sets one of those fields fails there. The depth
bound of §30 is ours to impose: Go's decoder was verified here to walk a
200 000-level document to its end without complaint, bounding the nesting by
nothing but the size of the input, so `cmsispack.go` counts the depth itself.

**What this does not do, and one known gap.** The descriptor is read only for a
root something else already settled — a licence file, a manifest, a package
manager. A pack directory carrying nothing but its `.pdsc` is never offered to
this reader and goes unread: the descriptor's name is the vendor's
(`ARM.CMSIS.pdsc`), so it cannot join the fixed-name list §19.2 strategy 6 stats
per used file per ancestor directory, and a glob there is what D34 already
refused for the same budget of §31. Coverage is therefore honestly partial. And
as with D34 and D36, `--max-input-size` does not reach these ceilings; they are
file-local constants beside the existing ones. The element names read here —
`package/vendor`, `package/name` and the `version` attribute of
`package/releases/release` — and the pack layout above come from knowledge of
the format rather than from a schema in this tree: there is no CMSIS material
here and no network. A real vendor descriptor should be captured as a fixture
before this is called finished.

---

## D39 — The west manifest is read as a declaration, and where the workspace is decides everything else

§19.2 strategy 2 and §21 both name west, and until now `west.yml` was only a
strategy 6 marker file, which in a west workspace marks next to nothing: the
manifest lies at `<topdir>/zephyr/west.yml`, strategy 6 walks upward from a used
file only, and no file under `<topdir>/modules` has that directory among its
ancestors. Such a file was therefore folded into the component the anchor stands
for, with no name, version, repository or purl of its own; only a file inside
the manifest repository itself got a component, under that repository's
directory name. The adapter is
`internal/adapters/pkgmanager/west.go`. Six things the specification leaves open
had to be settled.

**The workspace is found by `.west`, upwards, and by nothing else.** A west
project's `path` is resolved against the workspace root — the directory holding
`.west` — and not against the directory the manifest lies in; in the usual
Zephyr layout those are `<topdir>` and `<topdir>/zephyr`, so resolving against
the wrong one builds roots that do not exist. The workspace root is therefore
established first, by stat-ing `<dir>/.west/config` at the source root and then
at each directory above it, bounded by `limits.MaxDepth` and by the filesystem
root. That upward walk is the one place this adapter looks above the project
anchor, and it is defensible for the reason west itself walks up: an application
in a workspace is a subdirectory of it. It lists no directory and opens one
fixed relative path per level. It does walk to the filesystem root, so an
ordinary CMake project built somewhere beneath an unrelated Zephyr workspace
will have that workspace's manifest read; nothing wrong is published — a project
nothing linked stays out of the document — but such a build can collect a
`PACKAGE_NOT_LINKED` (info) per project of a workspace it has nothing to do
with, and that is the price of finding the workspace the way west finds it. Where there is no `.west`, nothing is read at
all, even where a `west.yml` lies beside the sources — a manifest repository
nobody ran `west init` on has no projects on disk, and resolving its paths
against a guessed root is the invented value §20.1 refuses.

**Rank 2, not 3.** What `west.yml` states is what was asked for, which is
§21.1's own definition of rank 2, the manifest the owning manager declares. Rank
3 is the state the manager recorded after installing, and west records none: it
writes no lock file and no per-project file list, so `Package.Files` stays
empty and only the roots decide the mapping. Where introspection is allowed the
checkout supersedes at rank 5, as everywhere else, and the manifest's claim is
kept in `Superseded`.

**A revision that is a commit becomes a commit, never a version.** §20.2 point 5
publishes a SHA as a version only when `components[].versionFrom` asks for it,
and a Zephyr manifest pins most of its projects to a full SHA. Such a revision
goes into `Package.Commit`, where it reaches the purl's `vcs_url` qualifier and
the `vcsCommit` property, and the component honestly reports `UNKNOWN_VERSION`.
What counts as a commit is decided narrowly: a full SHA-1 or SHA-256 whatever
its digits, and otherwise only a revision of at least seven hex characters with
a letter among them. A tag of digits alone — `20240612`, the date convention —
is the only version such a manifest states, and calling it a commit would throw
it away. Anything else, including a branch name, is taken as a declared version
with a leading `v` trimmed before a digit; publishing `main` as a version is the
weakest point of this design, but it is a value the manifest states rather than
one this tool invented, and §20.1 objects to the invention more than to the
branch name.

**`import:` is not followed.** A manifest may import another repository's, and
west resolves that in memory without writing the result anywhere this tool could
read; following it would mean reading manifests out of other people's trees for
a list of projects. An application manifest that imports Zephyr's therefore
yields the projects it states itself and no others. That is partial rather than
wrong, and it is the common downstream layout, so it is said plainly here, in
`status.md` and in the changelog.

**Git introspection cannot reach a project outside the anchors.** The runner
refuses a path argument outside the anchors registered for the run, which are
the project root, the build root and the build directory, and those are fixed
before `pkgmanager.Discover` is called. In the usual Zephyr layout the workspace
root lies above the application, so every project root is outside them and every
`git` call is refused before a process exists. The refusal is silent and
harmless — the manifest's claims stand — and point 4 of this work therefore pays
off only where the workspace root is at or below the source root. Widening the
runner's anchors is not the fix and was not done here: a package anchor is
registered after discovery for good reasons, and a subprocess reaching further
than the trees being read is a decision of its own.

**The format was checked against west itself, except for one file.** There is no
Zephyr material in this repository and no fixture, but the development
container pins west 1.5.0 (`docs/dev/README.md`), and the keys read —
`manifest`, `defaults`, `remotes`, `projects`, and a project's `name`, `path`,
`revision`, `url`, `remote` and `repo-path`, plus the `[manifest] path` and
`file` of `.west/config` — were verified there against west's own
`manifest-schema.yml`, `manifest.py` and `configuration.py`: `path` is
"relative to the west installation root", the manifest lives at
`topdir / manifest.path / manifest.file` with `west.yml` as the default file,
the local config is `<topdir>/.west/config`, and west's resolver produces the
same paths, URLs and revisions for the manifest the unit tests use as this
adapter does. Two keys are deliberately not read: `self:`, because west gives
the manifest repository no name but the word "manifest", and west's fallback
revision `master`, because that is west's default rather than something the
manifest declared. What could **not** be checked is `zephyr/module.yml`: it is
Zephyr's file rather than west's, and nothing here documents it — that one key
is knowledge of the format, exactly as D38's CMSIS element names were, and it is
used only where it is a usable single segment. A real workspace should still be
captured as a fixture before this is called finished. Two smaller consequences
worth recording: a project
directory that does not exist is silence rather than `MISSING_PACKAGE_EVIDENCE`,
because west deliberately does not clone the projects whose groups are filtered
out and a warning each would report the size of the manifest instead of the
state of the build; and a real workspace declares dozens of projects, so a first
run over one produces one `PACKAGE_NOT_LINKED` (info) per project nothing
linked, which is correct and is a visible change in the length of the report.

**The reader that made it possible.** `idfyaml.go` is now `yamlsubset.go`: it
could not read a block sequence at all — it recorded the key as present and
skipped the block — and `manifest.projects` is exactly that shape. It now reads
a sequence whose items open a mapping, keeps every refusal it had, counts each
sequence and each item toward the nesting bound, and bounds a sequence's length
as well. A sequence of plain scalars is still not read, because no caller asks
for one and a bare word with no key above it says nothing. The rename is
mechanical, and it was made because leaving `west.yml` to be read by a function
called `parseIDFYAML` would be worse than the diff.

One consequence of that is a widening, not only a keeping: an item is now a
mapping the reader walks, so a construct **inside** a sequence item — a
duplicate key, a reserved indicator, an unterminated quote, a sequence nested in
a sequence — now refuses the whole file where the old reader skipped the entire
block and never saw it. `targets:` followed by `- a: 1` and `a: 2` parsed before
and is refused now. No ESP-IDF file this tool has met is affected, and refusing
is the direction this reader is allowed to move in, but it is a change to a
shared reader and belongs written down rather than implied by "every refusal
stands". The other end of the same shape is answered deliberately: a `projects:`
that holds plain words rather than mappings parses into a sequence with no
items, and west refuses the manifest with `EVIDENCE_UNREADABLE` instead of
passing it over, because silence there would be indistinguishable from a
workspace that declares nothing.

## D40 — Six root readers were asked for, four were built, one became an adapter and one was dropped

Six single-file declarations were to be read from a directory that already
stands as a component root: a CMake package-version file, a Meson wrap, a
build2 `manifest`, an `xmake.lua`, a `MODULE.bazel` and an `mbed_lib.json`.
Four of them are now enrichers in
`internal/adapters/pkgmanager/rootmanifest.go`. The other two could not be,
and the reasons are the interesting part.

**The Meson wrap is an adapter, not an enricher.** A wrap lives at
`<source>/subprojects/<name>.wrap` while the subproject it describes is
unpacked into `<source>/subprojects/<directory>/` **beside** it. §21's
enricher contract says an enricher reads what lies directly in the root it was
handed and "does not descend, does not walk up, and does not look at a
sibling", so the only reader that could see a wrap from the subproject's own
root is a reader the contract forbids. Reading it as an adapter is also the
honest description of what it does: a wrap enumerates dependencies, which is
what an adapter is for. `internal/adapters/pkgmanager/meson.go` therefore reads
`<source>/subprojects/*.wrap` at one known location, and a subproject that no
used file belongs to is reported as `PACKAGE_NOT_LINKED` (info) like every
other installed-but-unlinked dependency.

**`mbed_lib.json` was not built.** The only thing that file states is a name,
and a name cannot travel through this mechanism at all: `Contribution` carries
a `Field`, `Field` names `version`, `license`, `supplier` and `purl`, and D33
already settled that a name read out of such a file is never published — the
component keeps the name that was settled before its files were grouped. An
enricher for it would be a reader that can produce nothing, which is worse than
no reader, because a dead reader reads as coverage. The honest alternative is
to add `mbed_lib.json` to the strategy 6 marker list of §19.2 so that an Mbed
library gets a component boundary of its own. That is a different change: it
moves component boundaries, and it costs an `os.Stat` per ancestor directory
per used file against the budget of §31. It belongs in a task of its own with
its own deviation, and it was not smuggled in here.

**All four enrichers contribute at rank 2, including the CMake one.** §21.1
would read a generated `<Pkg>ConfigVersion.cmake` literally as rank 3, install
state: CMake writes it while installing, and it states the version that was
configured. It is contributed at rank 2 anyway. A claim ranked too low can only
lose to a better origin, which is the correct outcome — the manager that really
installed the package should win. A claim ranked too high would overrule that
manager on the strength of a file that may have been generated by an entirely
different build of the same sources. Understating cannot publish a wrong value;
overstating can.

**Only `version` and `license` are contributed.** The package name an
`xmake.lua`, a `MODULE.bazel` or a build2 `manifest` states, and a build2
`summary`, have no destination: there is no `Field` for either, and D33 forbids
publishing the first. The `source_url` and `source_hash` of a Meson file wrap
have no destination either — a tarball URL is not a repository URL and must not
be published as one (§19.4), and there is nowhere in the model for a download
hash of a file this tool never saw.

**A build2 licence is published only if it is shaped like an SPDX
expression.** build2 permits `other: <description>` and free text, while
`licenses[].expression` is defined as an SPDX expression and every consumer
reads it as one. There is no list of valid SPDX identifiers reachable from this
package, so the check is a shape check and nothing more: identifiers
alternating with `AND`, `OR` and `WITH`, parentheses balanced. `All rights
reserved` fails it because three words in a row are a sentence and not an
expression. A value of another shape is dropped in **silence** — the file was
read and simply states nothing this document has a field for — exactly as a
CMSIS `<license>` path is.

**A build2 manifest that uses the multi-line value syntax is refused whole.**
build2's format allows a value to be continued over several lines. This reader
knows `name: value` lines and the format line `: 1`, and nothing else; a line
it cannot read that way ends the file with `EVIDENCE_UNREADABLE` and no
contributions. That is a real limitation and not an oversight: the grammar of
the multi-line form could not be verified against build2 itself from this
repository, and guessing at a delimiter would risk reading a line **inside** a
description block as a `version:`. Refusing loudly is the honest failure. A
manifest that carries a `description:` block therefore produces a warning where
a reader that knew the format would produce a version, and a fixture checked
against real build2 output is what would close it.

**`manifest` is recognised by its format line or not at all.** The name belongs
to half the world — Yocto writes a `license.manifest`, containers write
manifests, people write text files called manifest. A file of that name whose
first non-blank, non-comment line is not `: 1` is passed over in **silence**,
not reported: it is somebody else's file, and there is nothing missing about a
component that is not a build2 package.

**These names are not boundary markers.** `<Pkg>ConfigVersion.cmake`,
`manifest`, `xmake.lua` and `MODULE.bazel` were not added to
`packageMetadataFiles`, so a library copied into the tree with only an
`xmake.lua` beside it still gets no component of its own. The marker lists draw
boundaries and are paid for with a stat per ancestor directory per used file;
these readers describe a boundary somebody else already drew. A test in
`internal/generate/rootmanifest_test.go` states both halves: the same library
**with** a licence file gains a version from its `xmake.lua` and nothing else
changes, and **without** one it gains no component at all.

**The order of the enrichers is behaviour.** All four rank alike, and `Take`
keeps the first of two equally ranked claims, so a root holding both a
`MODULE.bazel` and an `xmake.lua` publishes the version of whichever reader
stands first in `var enrichers`. The registry says so in a comment, because the
alternative is somebody sorting that list alphabetically and silently changing
what documents say.

**The CMake reader will fire less often than it looks like it should.** An
installed package puts its package-version file under `lib/cmake/<Pkg>/`, which
is not the directory that stands as the component root, and an enricher may not
descend to find it. It answers where the file really lies in the root — a small
project that installs its config files flat, a checkout that generated one in
place — and that is worth having, but it is not the broad win the file name
suggests, and the changelog says so rather than overselling it.

**One thing was found and deliberately not fixed here.** `conanVersionPattern`
in `conan.go` accepts `set(PACKAGE_VERSION ${PROJECT_VERSION})` and yields the
fragment `${PROJECT_VERSION` as a version, because its value class is
`[^"\s)]+`. The new reader refuses any value containing `$`, `{` or `}` rather
than inheriting the flaw, and a test pins that. Whether the Conan adapter is
pulled along is a separate decision and belongs in a commit of its own.
