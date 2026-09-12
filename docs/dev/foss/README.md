# The FOSS attribution export

sbomb already collects the facts that FOSS compliance is made of. It then
throws most of them away. This directory describes what it would take to keep
them, and to render them as the two artifacts a manufacturer actually needs.

## The idea, once

The evidence chain answers one question well: **what is in this artifact.**
That is also the first question of every attribution obligation. The Apache
Software Foundation states the rule that every other licence family implies:

> The LICENSE and NOTICE files must exactly represent the contents of the
> distribution they reside in. […] only components and resources actually
> included in a distribution have any bearing on that distribution's NOTICE
> and LICENSE content.

A repository scanner cannot say that. sbomb can, because the used-file set is
derived from the link, archive and compile evidence rather than from a
directory walk. The build-time-only GPL code generator is not a shipped GPL
component, and sbomb is the only place in the toolchain that knows it.

So the extension is not a second product. It is:

1. stop discarding the licence and NOTICE bytes that detection already reads,
2. extract the copyright lines that MIT and BSD require verbatim,
3. derive from the graph what is distributed, how it is linked, and whether it
   was modified,
4. write all four into the SBOM using the fields CycloneDX specifies for them,
5. render a shippable notices document and an internal review record from that
   same data, as further outputs of the same run rather than a second analysis.

What it is **not**: a licence-compatibility judgement, an obligation verdict,
or anything resembling a source offer. The reasoning for each refusal is in
[requirements.md](requirements.md) and the machine-checked part of it in
milestone [F7](milestones/F7-foss-command.md).

## What is here

| Document | Contents |
|---|---|
| [decisions.md](decisions.md) | Every question the plan raised and how it was settled, with the reasoning. Read this before changing any milestone. |
| [requirements.md](requirements.md) | What an attribution export must satisfy, per licence family, with the sources. Written first, because the rest is derived from it. |
| [gap-analysis.md](gap-analysis.md) | Each requirement against the code as it stands, with file and line. Says what exists, what is wrong today, and what has to be built. |
| [spec-delta.md](spec-delta.md) | The amendments `docs/dev/spec.md` needs. Nothing here can be built without them: the findings and property catalogues are generated from the appendices and CI refuses a mismatch. |
| [milestones/](milestones/) | F1–F8, each independently buildable and testable. |

## Findings that came out of the analysis and are not FOSS work

Defects in the current tool that the FOSS work would otherwise inherit, each
worth fixing on its own merit. Three of the four have been fixed since this
plan was written.

* **Licence detection silently stops working when the source tree moves.**
  *Open.* The build root has a logical/physical mapping
  (`internal/generate/linkgraph.go:130` and `:145`); the source root does not.
  Build on one machine, run sbomb on another, and every licence becomes
  NOASSERTION with no finding. Milestone
  [F2](milestones/F2-source-tree-relocation.md).
* **The component root was guessed from the used files.** *Fixed in `634ba4a`.*
  The root is resolved by the strategy that named the component, and
  `COMPONENT_ROOT_UNRESOLVED` says when only the fallback applied.
* **A second reader walked above the component.** *Fixed in `bee9a03`.*
  `resolveLicense` ascended to the filesystem root for a `LICENSE`, which §22.1
  forbids. It had been unreachable from any run since component mapping
  replaced it, and a test asserted the forbidden behaviour.
* **A path that resolves to nothing was treated as a directory.** *Fixed in
  `efbcd8a`, and not foreseen by this plan.* The upward marker walk and the
  fallback component root both derived a directory from a file's path without
  asking whether the file is there. Evidence from a build made elsewhere names
  files this machine does not have, and a manifest in one of their coincidental
  parents named a component — the repository's own test run produced a
  component called `tmp` from a stray `west.yml` in `/tmp`. It belongs on this
  list because every artifact this track retains is read from the component
  root.

## Stage 2 — outlook, not part of this track

Stage 1 answers *what is in the artifact and under which text*. Stage 2 would
answer *what must therefore be done*, and the second question is only worth
asking once the first is answered reliably on real projects. Do not start any
of this until Stage 1 has been used and has demonstrably fallen short:

1. **An obligation matrix as customer-owned data.** A JSON file mapping SPDX
   identifiers to a fixed obligation vocabulary — `attribution`,
   `license-text`, `copyright-notice`, `notice-file`, `modification-notice`,
   `source-offer`, `relinking-capability`, `same-license-derivative`,
   `patent-grant-terms`, `no-endorsement` — with conditions permitted only on
   `linkageForm` and `modified`.
2. **An adoption gate.** sbomb refuses to apply a matrix whose `adopted` field
   is null. That is the line between "our tool told us" and "we assessed this",
   and it is the only reason a matrix is safe to ship at all.
3. **A `foss` policy profile with gates**, for teams that want CI to fail on an
   unresolved obligation or a missing copyright statement.
4. **A licence gate in `sbomb diff`** — fail when a new copyleft component
   appears or a component's licence changes between releases. Probably the most
   used feature of the lot once it exists, and cheap because `bom-ref`s are
   anchored version-free (§28.4). It belongs to the `diff` work, not here.

## Status

Planned. Nothing in this directory is implemented. `docs/dev/status.md` is the
authority on what is.
