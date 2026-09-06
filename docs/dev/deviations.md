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

The two forms are not interchangeable, which is the point: CycloneDX's
`licenseChoice` is a choice between a *list of licences* and a *tuple of
exactly one expression*. A list says which are present; an expression says how
they combine. An observation can only make the first statement.

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
