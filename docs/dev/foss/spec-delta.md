# What `docs/dev/spec.md` has to say first

None of the FOSS milestones can be merged without amending the specification,
and this is not bureaucracy: two user-facing catalogues are **generated** from
the appendices and CI fails when they disagree with the code.

```
go run ./tools/findingsdoc --check    # docs/findings.md   <- Appendix A
go run ./tools/propertydoc --check    # docs/properties.md <- Appendix B
```

`docs/findings.md` and `docs/properties.md` are outputs. Editing them by hand
is the one thing that cannot work.

Numbering below is a proposal. What matters is that each item lands somewhere
and that the appendices are complete.

---

## 1. New section — §7.9 Source Tree Relocation

The counterpart of the build-root mapping that §7.6 already implies and
`physicalFor` already implements.

* The **logical source root** is the source root the build evidence records
  (CMake File API `paths.source`). It is the identity root and never changes.
* The **physical source root** is where those bytes are now. It defaults to the
  logical one and is set by `--source-dir` / `project.root`.
* Reads of source-tree files resolve logical → physical. Identity does not.
* A read that would leave every registered anchor is refused (§30.4).
* If the physical source root does not exist, `SOURCE_TREE_UNAVAILABLE` is
  emitted once, and everything that depends on reading sources degrades
  explicitly rather than silently.

This changes the documented meaning of `--source-dir` (§32.1), which today sets
the identity root. Record the change in `deviations.md` with the observed
behaviour that motivated it: a build directory restored in a second CI job
produces NOASSERTION for every licence and no finding says why.

## 2. §19.2 — the component root becomes a resolved fact

Add: every mapped component has a **root**, resolved in the order of §19.2 —
curated `components[].path`, package-manager root, git submodule boundary, SDK
root, anchor root. The deepest common directory of the used files is the
**last** fallback and must be recorded as such, because it varies with what the
linker kept.

Consequence: `sbomb:component:root`, already reserved in Appendix B, becomes
emitted. New finding `COMPONENT_ROOT_UNRESOLVED` (info) when only the fallback
was available.

Strategy 6 already walks upward for package metadata and already stops at the
anchor root. Add a recognized licence file to the marker files that strategy
looks for (decision [Q7](decisions.md)). State why this is not the layout
heuristic the section forbids: that rule is about directory *names* —
`vendor/`, `dep/`, `third_party/` — and a licence file is a file with content,
like `vcpkg.json`. Without it a library copied into the tree with nothing but a
`LICENSE` and an `include/` directory is never recognized, which is the most
common shape in embedded work.

## 3. §22.3 — recognized licence files

Add the `LICENSE-<id>` form the section already names to the implemented list,
and state the matching rule once (case-insensitive, optional `.txt`/`.md`).

## 4. New section — §22.9 Licence Artifact Retention

* What is retained: the recognized licence files and the NOTICE/COPYRIGHT files
  **of the component root**, verbatim, with SHA-256 and canonical path.
* NOTICE and COPYRIGHT are retained for reproduction and are **not** inputs to
  the identification priority of §22.2. (They are today; that is a defect.)
* Limits, consistent with §30: at most 8 artifacts per component, at most 1 MiB
  each. Exceeding either bounds the *list*, never truncates a retained file.
* Retention adds no read that §22.1 does not already permit.
* No canonical SPDX text is ever substituted for a text the component does not
  carry.

## 5. New section — §22.10 Copyright Statements

* Sources: used files and retained artifacts. Nothing else.
* Window: the first 64 KiB of a file, matching §22.2 rule 2.
* Recognized forms, exhaustively: `SPDX-FileCopyrightText:` and a line
  containing `Copyright` with an optional `(c)`/`(C)`/`©`, an optional year or
  range, and a holder.
* Expressions are RE2 (§30.6).
* The **original line** is stored, with the `FileID` it came from.
  Deduplication uses a normalized key that is never stored or displayed.
* Limit: 200 statements per component.

## 6. New section — §24.5 Distribution Role, or an addition to §19

Per node and per component:

* `build-time-only` — reachable from an artifact **exclusively** through
  `generator-input`, `generator-output` or `toolchain` edges.
* `distributed` — everything else that is reachable.

Stated in this direction on purpose: an evidence type added later must not be
able to remove a component from an attribution document by omission.

Linkage form, aggregated per component from its files, values fixed:
`static-archive-member`, `static-object`, `dynamic`, `header-only`,
`embedded-asset`, `generated-source`, `build-tool`. A component may carry
several.

## 7. §19.4 — modification status becomes tri-state

`true` from a dirty tree or from package metadata recording applied patches;
`false` only when a positive check ran and came back clean; `unknown`
otherwise, including when `--allow-introspection` is off. Absence of
information is never `false`.

If comparing against the recorded upstream is ever wanted, §9.2 needs
`git rev-list --count <upstream>..HEAD` and the security argument for it.
Recorded in `open-questions.md`, not assumed.

## 8. §28 — CycloneDX binding for the FOSS data

**The rule, stated once.** Where the format specifies a field, the field wins —
§28.1 already says this for the `vcs` external reference. A property is added
*beside* a field only where the field cannot carry the precision the evidence
has, and never *instead* of one.

This is not a matter of taste. The text documents of §9 are read by people; the
document is read by tools. Dependency-Track, `sbomqs`, TR-03183 conformance
checkers, ORT and ScanCode all consume the document and none of them consume a
notices file. Data in a specified field is interoperable; data in an `sbomb:`
property is private to sbomb.

**Binding table**

| Datum | Specified field | Property beside it | Why the property, if any |
|---|---|---|---|
| Retained licence text | `evidence.licenses[].license.text` (`contentType: text/plain`, `encoding: base64`) | `sbomb:component:licenseFile`, `sbomb:component:noticeFile` | the field carries the bytes, the property carries provenance: canonical path and SHA-256 |
| Evidence class | `license.acknowledgement` = `declared` (from the component's own files) / `concluded` (curated) | `sbomb:license:evidenceClass` (existing) | the enum has two values; §22.4 has six |
| Copyright, observed | `evidence.copyright[].text` | — | — |
| Copyright, concluded | `component.copyright` — curated only | — | — |
| **Modification** | **`component.pedigree`** — `commits[].uid` for the resolved commit, `patches[]` for patches a package manager recorded, `notes` for the signal that decided it | `sbomb:component:modified` | `pedigree` cannot express `unknown`: an absent node does not mean "unmodified", and the tri-state of §7 must survive into the document |
| **Distribution role** | **`component.scope`** — `excluded` for `build-time-only`, `required` otherwise | `sbomb:component:distributionRole` | the two are not the same axis; see below |
| Linkage form | — | `sbomb:component:linkageForm` | CycloneDX has no field for it |

**`pedigree` in detail.** `patches[].type` is an enum
(`unofficial`, `monkey`, `backport`, `cherry-pick`); a patch a package manager
applied is `unofficial` unless the manager says otherwise, and `patches[].diff`
stays empty — sbomb records that a patch was applied, not its content. This is
the standard place for what §7 currently plans to say in a property, and using
it means an SBOM consumer sees the modification without knowing sbomb exists.

**`component.scope` is not the same axis, and the mapping must be defined
rather than assumed.** CycloneDX defines `excluded` as "component usage for test
and other non-runtime purposes […] not reachable within a call graph at
runtime" — that is runtime reachability. sbomb's distribution role is "is it
inside the artifact". They agree on the two common cases and diverge on one:

| Case | sbomb role | `scope` |
|---|---|---|
| statically linked archive member | `distributed` | `required` |
| build-time code generator | `build-time-only` | `excluded` |
| dynamically linked system library | not in the artifact, but `distributed` for licence purposes | `required` — it is reachable at runtime |

So `scope` is written from the role with that one exception spelled out, and
`sbomb:component:distributionRole` keeps the evidence-based meaning unchanged.
Note that sbomb does not write `scope` today at all: `sbomb:component:scope` is
a different property with the values `project | third-party | sdk | toolchain |
system` and stays as it is.

**Ordering** for the new arrays goes into the §29 table: `evidence.copyright[]`
by `text`, retained texts by `(kind, canonical path)`, `pedigree.patches[]` by
`(type, notes)`, `pedigree.commits[]` by `uid`.

**Size.** New policy setting `licenseTextInSBOM` ∈ `off` (default), `evidence`,
so that `generate` output keeps its current size.

State normatively that **the FOSS outputs never change the document** (decision
[B1](decisions.md)): the retained text always reaches the notices files, and
reaches the document only when this setting says so. `--foss-out` is an output
selector, never a content switch — otherwise an SBOM would depend on which
side-outputs somebody asked for.

**Why CycloneDX carries this and SPDX would not, yet.** Worth recording,
because §36.1 promises an SPDX writer and somebody will ask why the FOSS data
is bound to CycloneDX first. SPDX has no clean slot for *the verbatim text of a
listed licence as this component carries it* — which is the entire point for
MIT and BSD, where the holder is inside the text:

* `PackageAttributionText` (2.3) states explicitly that it "is not meant to
  include the package's actual complete license text";
* `ExtractedLicensingInfo.extractedText` carries real text but exists only for
  `LicenseRef-` licences, not for listed ones;
* SPDX 3.0.1 puts `licenseText` on the `License` class, where a `ListedLicense`
  carries the *canonical* text; expressing the component's own wording means
  modelling MIT as a `CustomLicense` and losing the identifier.

CycloneDX puts `license.text` beside `license.id`, so both are stated at once.
When the SPDX writer arrives it maps what it can and the deviation is recorded
rather than discovered.

## 9. §32 — the FOSS outputs: one flag and one subcommand

The FOSS documents are an **additional rendering of a `generate` run**, in the
row that §32.1 already contains: `--review-report`, `--findings-json` and
`--evidence-dump` are all further outputs of one discovery. Add:

```
--foss-out <dir>     write the FOSS documents alongside the SBOM
```

and a subcommand that is a thin front end over the same code path, for the case
where no SBOM is wanted on disk:

```
sbomb foss --build-dir <dir> [--source-dir <dir>] --out <dir>
           [--format text|markdown] [--reproducible]
```

State the invariant normatively: **the two entry points share one discovery and
one renderer, and neither workflow runs discovery twice.** This is not a
performance note. Two invocations mean two graphs built from a tree that may
have changed in between, and the SBOM and the notices document could then
disagree while each is internally correct.

`--out` overwrites its four files the way `--output` overwrites an SBOM. It
deletes nothing and refuses nothing (decision [Q14](decisions.md)).

§29 gains the ordering of the notices document: components by name then
`bom-ref`, and a licence text shared by several components printed once, keyed
by the SHA-256 of the retained bytes, with those components listed under it
(decision [Q13](decisions.md)).

§30.7 is extended by name to `foss-review.txt` and `foss-review.json`.
`THIRD-PARTY-NOTICES.txt` carries no filesystem paths and needs no rule
(decision [Q8](decisions.md)).

Exit codes are the existing ones of §32.4 in full — `0`, `1`, `2`, `4`, `70`
with the documented precedence. **No exit code depends on licence content**;
everything the FOSS view finds is informational. `policy` gates (`3`) are
evaluated by `generate` as they are today and not by `foss`.

## 10. Appendix A — new findings, and waivers

The new findings are waivable like every other (decision
[Q11](decisions.md)) — sbomb's waivers annotate rather than delete, so a waived
finding keeps its reason, approver and expiry and only stops failing the build.
§33 gains one sentence: **a waiver never changes `THIRD-PARTY-NOTICES.txt`.**
That document reports what the tool saw, not what somebody decided about it.

| ID | Severity | Gate | Meaning |
|---|---|---|---|
| `SOURCE_TREE_UNAVAILABLE` | warning | — | The source root the evidence names cannot be read |
| `COMPONENT_ROOT_UNRESOLVED` | info | — | Component root fell back to the common directory of used files |
| `FOSS_LICENSE_TEXT_MISSING` | info | — | Distributed component has a licence identifier but no retained text |
| `FOSS_LICENSE_ARTIFACT_LIMIT` | info | — | More recognized licence files than the retention limit |
| `FOSS_COPYRIGHT_MISSING` | info | — | Distributed component carries no copyright statement |
| `FOSS_COPYRIGHT_LIMIT` | info | — | More copyright statements than the limit |
| `FOSS_MODIFICATION_UNKNOWN` | info | — | Modification status could not be established |
| `FOSS_LICENSE_UNCLASSIFIED` | info | — | Licence identifier is on no obligation list; not assumed permissive |
| `FOSS_SOURCE_OBLIGATION` | info | — | A distributed component's licence triggers a source obligation |
| `FOSS_PER_FILE_LICENSE_DIVERGENCE` | info | — | Files of one component carry different SPDX identifiers |

## 11. Appendix B — new properties

Two of these accompany a specified field rather than replacing it —
`sbomb:component:modified` beside `component.pedigree`, and
`sbomb:component:distributionRole` beside `component.scope`. §8 says why each
one is still needed; the appendix entry should say it too, in a word, so that
nobody later removes the property as redundant.

Grouping components:

```
sbomb:component:licenseFile      (repeated, <canonicalPath>@sha256:<hex>)
sbomb:component:noticeFile       (repeated, <canonicalPath>@sha256:<hex>)
sbomb:component:distributionRole (distributed | build-time-only)
sbomb:component:linkageForm      (repeated, sorted)
sbomb:component:archiveMembersUsed
sbomb:component:modified         (true | false | unknown)
sbomb:component:sourceObligation (repeated, sorted)
```

File components:

```
sbomb:file:distributionRole      (distributed | build-time-only)
sbomb:file:linkageForm
```

`sbomb:component:root` and `sbomb:component:headerOnly` already exist as
reserved entries and change status to emitted.

The name grammar in `tools/propertydoc` is
`sbomb:<area>:<key>` with alphanumeric segments; all of the above satisfy it.

## 12. `testdata/fixtures/POLICY.md`

Amend the first rule. Proposed wording:

> Only build evidence is committed, with one exception: for the FOSS fixture,
> the licence, notice and source files of the fixture's own dependencies are
> committed as well. They are original fixture material, not third-party
> payload, and without them no attribution behaviour can be tested. Their
> origin is recorded in `PROVENANCE.md` like everything else.
