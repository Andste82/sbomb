### Milestone F6 — Distribution role, linkage form, modification status

**Goal:** three attributes derived from a graph that already exists. This is
the best ratio of value to code in the track, and it is what no repository
scanner can do: `dep/gpl-gen` is not a shipped GPL component, and sbomb can
prove it.

**Depends on:** F1. Independent of F2–F5 and can be built in parallel.

---

**6a — Distribution role**

Per node and per component:

* `build-time-only` — reachable from an artifact **exclusively** through
  `generator-input`, `generator-output` or `toolchain` edges.
* `distributed` — everything else that is reachable.

Stated in this direction deliberately. §8.3 defines fourteen evidence types and
the code emits seven today; an allowlist of "distributing" types would make
`build-time-only` the default for anything unlisted, and a component would then
vanish from an attribution document because an edge type was added. Omission
must fail towards inclusion.

Pure graph traversal. No path heuristics, no directory names.

Properties `sbomb:file:distributionRole`, `sbomb:component:distributionRole`,
**and** the specified field `component.scope`, written per
[spec-delta.md §8](../spec-delta.md): `excluded` for `build-time-only`,
`required` otherwise. The two are not the same axis — CycloneDX `scope` is
runtime reachability, the role is presence in the artifact — and they diverge on
exactly one case, the dynamically linked system library, which is `required`
at runtime and still needs attribution. The property keeps the evidence-based
meaning; the field makes the common case readable by tools that never heard of
sbomb.

`component.scope` is not written by sbomb today. `sbomb:component:scope` is a
different property (`project | third-party | sdk | toolchain | system`) and is
untouched.

**6b — Linkage form**

Per component, aggregated from its files; a component may carry several values,
all emitted and sorted:

| Value | Meaning |
|---|---|
| `static-archive-member` | members the linker extracted from a static archive |
| `static-object` | object files linked directly |
| `dynamic` | a shared library referenced, not embedded |
| `header-only` | headers only, no object contribution |
| `embedded-asset` | embedded by packaging, not by the linker |
| `generated-source` | contributed only through generated sources |
| `build-tool` | `build-time-only` per 6a |

`domain.Component.EnvironmentProvided` already distinguishes the dynamic case
and must agree with it. For `static-archive-member`, also
`sbomb:component:archiveMembersUsed` = `<used>/<total>` when the archive index
is readable. `sbomb:component:headerOnly` changes from reserved to emitted.

**6c — Modification status**

Tri-state, evidence only:

| Signal | Result |
|---|---|
| the component root's git tree is dirty | `true` |
| package metadata records applied patches (Conan `conandata.yml`, vcpkg port patches) | `true` |
| a git root was found, was clean, and matched its recorded tag | `false` |
| anything else, including `--allow-introspection` off | `unknown` |

`false` is emitted **only** after a positive check. Absence of information is
`unknown`, never `false` — an auditor will find that one.

Written to **`component.pedigree`**, which is the field CycloneDX specifies for
exactly this: `commits[].uid` for the resolved commit, `patches[]` for what a
package manager recorded — `type: unofficial` unless the manager says otherwise,
`diff` left empty because sbomb records that a patch was applied and not its
content — and `notes` for the signal that decided the answer.

The property `sbomb:component:modified` is written **beside** it, not instead of
it, for one reason: `pedigree` has no way to say `unknown`. An absent pedigree
node does not mean "unmodified", and losing the third state at the document
boundary would undo the point of this section. Finding
`FOSS_MODIFICATION_UNKNOWN` (info).

**Comparison against the recorded upstream is not implemented.** It needs
`git rev-list --count <upstream>..HEAD`, which §9.2 does not allow, and the
allowlist is a security boundary rather than a convenience. Recorded in
`open-questions.md` with what it would cost.

**Tests**

* `gpl-gen` → `build-time-only` / `build-tool`, and it is the only such
  component in the fixture.
* `mit-lib` → `distributed` / `static-archive-member`, `archiveMembersUsed=1/3`.
* `bsd-hdr` → `header-only`; a file reachable both as a generator input and as
  a compiled source resolves to `distributed`.
* A synthetic graph with an evidence type the derivation has never seen: the
  component is `distributed`. This is the regression test for the direction of
  6a.
* Modification: clean git root → `false`; uncommitted change → `true`; no git
  → `unknown`; introspection off with a git root present → `unknown`.
* The document round-trip: `modified=true` from a recorded patch produces a
  `pedigree.patches[]` entry; `modified=unknown` produces **no** pedigree node
  and the property still says `unknown`. A consumer reading only `pedigree`
  must not be able to mistake the third state for the second.
* `component.scope` is `excluded` for `gpl-gen` and `required` for every
  distributed component, and the document validates at 1.6 and 1.7.
* Determinism: shuffling adapter order changes none of the three attributes.

**Acceptance**

```
go test ./internal/generate/... ./internal/evidence/... -race           # 0
sbomb generate --build-dir testdata/fixtures/gcc-ninja/p14-foss/build \
  --source-dir testdata/fixtures/gcc-ninja/p14-foss/src \
  --inventory-dump /tmp/f6.json --output /tmp/f6.cdx.json --reproducible  # 0
cmp /tmp/f6.json testdata/golden/gcc-ninja-p14-foss-inventory.json       # 0
go run ./tools/propertydoc --check                                       # 0
```

**Intended golden change:** components and files gain the three attributes, and
components gain `scope` and — where there is positive evidence — `pedigree`.

**Not in this milestone:** what the attributes imply. `static-archive-member`
plus LGPL is a source obligation, and that sentence belongs to F7.

**Definition of done:** every component carries all three attributes, each
derived from evidence, each with a defined value when the evidence is absent.
