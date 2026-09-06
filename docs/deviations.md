# Deviations

Deviations from `sbomb-spec-v3.1.md`, recorded per specification section 0.2.
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

The universal gate in `docs/milestones/README.md` requires `go test ./... -race`.
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
