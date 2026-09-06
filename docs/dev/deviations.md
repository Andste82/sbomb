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

The fixture corpus commits the index under its real name, and consumers must
locate it by glob rather than by a fixed filename -- which is also how a real
build directory behaves.

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

Not yet implemented are the package-manager strategies (2-5): Conan, vcpkg, CPM
and FetchContent. Until they exist, a dependency that a package manager
installed is named from its anchor or its manifest directory rather than from
the manager's own metadata, and its version and supplier have to come from
curated configuration. That is roadmap phase 7.

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
