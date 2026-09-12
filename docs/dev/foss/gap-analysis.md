# The requirements against the code as it stands

Each requirement of [requirements.md](requirements.md) against what
`internal/` actually does today. Line references are pointers, not contracts.

Last checked against `main` at `9bea8e1` (2026-09-12). Four defects this
analysis named have been fixed since it was written — see R2 and the
[README](README.md) — and one it did not foresee was found and fixed with
them.

Verdicts: **exists** — nothing to build. **partial** — the mechanism is there
and something specific is missing. **missing** — no code covers it. **wrong** —
code covers it and produces the wrong answer for this purpose.

---

## R1 — Describe exactly what is distributed

**partial.** The reachability filter is the whole point of the tool and it
already works: `usedFiles(graph, artifactIDs, excludedByGC)` in
`internal/generate/generate.go` keeps only what a deliverable can reach, and
the archive-member and DWARF narrowing paths make it tighter than any scanner.

What is missing is the distinction *within* the reachable set between what is
shipped and what only helped build it. A code generator's inputs are reachable
through `generator-input` edges and land in the same used-file set as linked
objects.

**To build:** a role derivation over the graph (milestone
[F6](milestones/F6-component-attributes.md)).

**Trap to avoid.** Specifying the role as an allowlist of "distributing" edge
types makes `build-time-only` the default for anything unlisted, and a
component then disappears from the notices document silently. §8.3 of the
specification defines fourteen evidence types; the code currently emits seven
(`link`, `archive-member`, `compile`, `source-mapping`, `header-dependency`,
`generator-input`, `package`). The derivation must be the other way round:
`distributed` unless a node is reachable *exclusively* through
`generator-input`, `generator-output` or `toolchain`.

## R2 — The component's own licence text, verbatim

**partial.** The three defects this section named when the plan was written
have since been fixed on `main`; what is left is the retention itself.

The bytes are read and dropped every time:

* `license.ResolveFile` (`internal/license/license.go:119`) reads the file and
  returns only a `LicenseFinding`.
* `licenseFromComponentRoot` (`internal/generate/components.go:1115`) consults
  the recognized files of the component root and returns the **first** that
  resolves; the rest are never opened.
* `licenseEvidenceFromComponentRoot` (`:1132`) reads them again for the
  observation path.

**What has been fixed since, and is therefore no longer F3 or F4 work:**

1. **The component root is a resolved fact.** `resolveRoot`
   (`components.go:1198`) settles it in the mapping priority order — curated
   path, package-manager root, the marker directory strategy 6 stopped at, the
   anchor root — with the common directory of used files as the last resort and
   `COMPONENT_ROOT_UNRESOLVED` (info) when only that applied. §19.2 is amended
   accordingly, `domain.Component.Root` is populated (`components.go:778`) and
   `sbomb:component:root` is emitted. A recognized licence file marks a
   boundary (`licenseBoundaryFiles`, `components.go:53`), which is decision
   [Q7](decisions.md). Landed in `634ba4a`.
2. **The second reader is gone.** `resolveLicense` ascended from a file's
   directory to the filesystem root looking for `LICENSE`, `COPYING` or
   `NOTICE` — §22.1 forbids exactly that. It had been unreachable from any run
   since component mapping replaced it, and a test asserted the forbidden
   behaviour. Both were removed in `bee9a03`.
3. **The recognized-file list matches §22.3.** The hand-written list of eleven
   exact names is replaced by `recognizedLicenseFile` / `licenseFilesIn`
   (`components.go:1157`): case-insensitive, optional `.txt`/`.md`, and
   `LICENSE-<id>` as a family, consulted in a fixed order. Landed in `2717c1b`.

**A fourth defect was found while fixing those and is also gone.** The upward
marker walk and the fallback component root both derived a *directory* from a
file's path without asking whether the path resolves. Evidence from a build
made elsewhere names files this machine does not have, their parents exist here
by coincidence, and a manifest in one of them named a component — this
repository's own test run produced a component called `tmp` from a stray
`west.yml` in `/tmp`. Both steps skip an unreadable file now (`efbcd8a`). It
matters here because every artifact this track retains is read from the
component root, and a root derived from a dead path would have been retained
from whatever sits there.

**To build:** retention of the bytes with hash and canonical path, all matching
files rather than the first, and the CycloneDX attachment to carry them
(milestone [F4](milestones/F4-license-artifacts.md)).

## R3 — Copyright statements, verbatim

**missing.** No code reads a copyright line. `internal/license` has the
normalization machinery (`looseNormalize`, the template matcher) but strips
copyright lines rather than collecting them — technique 3 of §22.3 removes them
so that a text can match a canonical one.

**To build:** an extractor over used files and retained artifacts, RE2 only
(§30.6 forbids backtracking), bounded to the first 64 KiB per file, storing the
original line and the `FileID` it came from. Milestone
[F5](milestones/F5-copyright.md).

## R4 — NOTICE, kept separate

**partial and slightly wrong.** `NOTICE` and `COPYRIGHT` are recognized by
`recognizedLicenseFile` (`internal/generate/components.go:1157`), so a NOTICE
file can currently *decide a component's licence identifier*. They rank below
the licence forms, which narrows the case without closing it. For a component whose NOTICE quotes a licence that is not
the component's own, that is a wrong answer with high confidence.

**To build:** NOTICE becomes an artifact kind of its own, retained for
reproduction (R4) and excluded from the identification chain of §22.2.
Milestone [F4](milestones/F4-license-artifacts.md).

## R5 — Modification status

**partial.** Dirty state is already detected, but only where a package manager
adapter derived it from `git describe --dirty`
(`internal/adapters/pkgmanager/fetchcontent.go:205`, `submodule.go:132`), and it
is a two-state boolean surfaced as `VCS_DIRTY` and `sbomb:component:vcsDirty`.

Three gaps:

* No tri-state. Absence of a git root today is indistinguishable from a clean
  one.
* Applied patches are not read. Conan (`conandata.yml`) and vcpkg ports record
  them, and a patched component is a modified component.
* **"Commits ahead of upstream" cannot be determined under the current
  introspection allowlist.** §9.2 permits `git rev-parse HEAD`,
  `git describe --tags --always --dirty`, `git status --porcelain` and
  `git config --get remote.origin.url`. There is no `rev-list` and no `log`.
  Either the allowlist grows — a specification change with a security argument
  attached — or the criterion is restricted to dirty-tree and patch evidence.
  The milestone restricts it and records the question.

Milestone [F6](milestones/F6-component-attributes.md).

## R6 — Source obligations named, not produced

**missing, and cheap.** Everything needed is present once R5 and the linkage
form exist: a flat list of SPDX identifiers, the linkage form, and prose. The
one piece of real engineering is the guarantee that the output cannot be
mistaken for a source offer, which is a test over the written file set.
Milestone [F7](milestones/F7-foss-command.md).

## R7 — Distribution form

**exists.** `sbomb:artifact:role` is emitted, and the deliverable model of §5
already distinguishes firmware, executable and library. Stage 1 implements no
per-licence form exception, so nothing else is needed.

## R8 — Machine-readable first

**partial.** The writer emits `licenses` and `evidence.licenses`
(`internal/cyclonedx/writer.go`), and the Go types stop exactly where the FOSS
data would begin:

| Needed | Present in schema (1.6 and 1.7) | Present in `internal/cyclonedx` |
|---|---|---|
| `license.text` (attachment) | yes | no — `LicenseIdentifier` has `ID` and `Name` only (`cyclonedx.go:97`) |
| `license.acknowledgement` | yes | no |
| `component.copyright` | yes | no — not on `Component` (`cyclonedx.go:56`) |
| `evidence.copyright[]` | yes | no — `Evidence` has `Identity`, `Occurrences`, `Licenses` (`cyclonedx.go:107`) |
| `component.pedigree` (`commits`, `patches`, `notes`) | yes | no |
| `component.scope` (`required` \| `optional` \| `excluded`) | yes | no — sbomb writes only its own `sbomb:component:scope` property (`writer.go:313`), which is a different axis |

All six are additive and schema-valid; validation is in-process against the
embedded schemas (§32.5), so a mistake here fails the build rather than a
consumer.

The last two matter more than they look. `pedigree` is the field CycloneDX
specifies for "this component was modified relative to upstream", and `scope`
is the closest standard vocabulary for "this is a build tool, not part of the
product". Binding them costs almost nothing and is the difference between data
a consumer reads and data only sbomb understands — the mapping, including where
`scope` and the distribution role are *not* the same axis, is in
[spec-delta.md §8](spec-delta.md).

**Consequence that has to be stated once:** the existing CycloneDX goldens
**will change** in the milestones that add these fields. The regression guard
for this work is therefore not "the goldens never change" — it is "the goldens
change only in the milestone that intends it, and the diff is reviewed and
explained in that milestone". Carrying the data outside the document to keep
the goldens frozen would be the wrong trade: it would put the deliverable
where no consumer looks.

**Size.** Base64 inflates a licence text by a third. A `policy.licenseTextInSBOM`
switch with `off` (default) and `evidence` keeps `generate` output the size it
is today and lets the FOSS view ask for the text explicitly.

## R9 — Self-contained delivery

**exists as a rule, nothing to build.** No network access is possible (§30.8),
so the tool cannot substitute a URL for a text even by accident.

## R10 — Incompleteness stated

**exists.** The findings mechanism, `sbomb:license:review` and the NOASSERTION
reason codes of §22.7 are the pattern to extend. New identifiers must be added
to Appendix A first; see [spec-delta.md](spec-delta.md).

## R11 — Traceability

**exists.** Anchors, hashes, the evidence dump and `explain` cover it. One
caveat: `explain` reads the dumped graph from the build directory
(`cmd/sbomb/main.go:888`), not a fresh run, so any attribute that should be
visible there has to be written into the dump — an Appendix C change. Decision
[Q20](decisions.md) settles it: `explain` is **not** extended. It already
answers "is this component really in the product", and `foss-review.txt`
already answers everything else per component.

## R12 — Deterministic and diffable

**exists, with one new hazard.** Determinism is checked across
linux/amd64, linux/arm64 and windows/amd64 (`.github/workflows/determinism.yaml`).
Retained bytes are copied, not parsed, so the hazard is not sbomb but git:
`.gitattributes` marks `testdata/fixtures/**` and `testdata/golden/**` as
`-text`, and **`tools/fixtures/projects/**` is not marked**. A fixture licence
file checked out on Windows would arrive with CRLF and the retained bytes would
differ. Milestone [F1](milestones/F1-fixture.md) fixes this.

---

## The testability gap, which is the real blocker

The fixture corpus **contains no sources**. `testdata/fixtures/POLICY.md`:
"Only build evidence is committed: no source tree, no SDK content, no host
paths." Builds run under the sentinel roots `/__fixture_src__` and
`/__fixture_build__` so that binary evidence carries portable paths, and only
build outputs are harvested. Every golden SBOM therefore has
`licenses: [{"license": {"name": "NOASSERTION"}}]` — not because detection
fails, but because there is nothing on disk to detect.

So nothing in the FOSS work can be tested against the corpus as it stands. Two
things have to change, and they are independent:

1. **The corpus must carry the licence-bearing source tree** for one project.
   `tools/fixtures/regen.sh` already copies `tools/fixtures/projects/<p>/` into
   `/__fixture_src__` and turns each `dep/*/` into a git repository with a fixed
   identity and date, which is exactly the material R2, R3 and R5 need. Only
   the harvest step has to keep it. This is a narrow amendment to the fixture
   policy — a licence text is not third-party payload — and it is milestone
   [F1](milestones/F1-fixture.md).

2. **sbomb must be able to read a source tree that is not where it was built.**
   This is the second defect named in the README, and it is not a test crutch:

   * The build root already has this. `logicalFor` / `physicalFor`
     (`internal/generate/linkgraph.go:130` and `:145`) map the logical build
     root recorded in the evidence onto the directory being read, which is why
     fixtures work at all.
   * The source root has no such mapping. A path like
     `/__fixture_src__/dep/mit-lib/LICENSE` is opened literally and fails.
   * `--source-dir` looks like the answer and is not: it assigns
     `cfg.Project.Root` (`cmd/sbomb/main.go:650`), which is the **identity**
     root fed to `anchors.Assemble`. Pointing it at a relocated tree does not
     relocate any read; it re-anchors every file and changes the SBOM.

   In production this is the CI split that everyone has: build in one job,
   generate the SBOM in another, from a restored build directory. Licences
   silently become NOASSERTION and no finding says why. Milestone
   [F2](milestones/F2-source-tree-relocation.md).

Once F1 and F2 exist, every later milestone is testable the ordinary way: a
golden inventory and a golden document over a real, committed fixture, plus
package-level table tests for the extractors and renderers.
