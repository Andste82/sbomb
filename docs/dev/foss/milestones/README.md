# FOSS milestones

Eight milestones. Each is independently buildable, independently testable, and
leaves the repository shippable.

The gate every change has to pass is in [../../README.md](../../README.md).
Beyond it, every milestone here must:

- add at least one golden or table-driven test for the new behaviour;
- implement nothing from a later milestone, and nothing from Stage 2;
- amend `docs/dev/spec.md` **before** emitting a new finding or property, per
  [../spec-delta.md](../spec-delta.md), because `docs/findings.md` and
  `docs/properties.md` are generated from the appendices and CI checks them;
- update `docs/CHANGELOG.md` and `docs/dev/status.md`;
- record an unsettled decision in `docs/dev/open-questions.md` rather than
  guessing at it.

## The golden rule for this track

The CycloneDX goldens **may change, in the milestone that intends it, with the
diff explained in that milestone**. F3, F4, F5 and F6 each change them once and
say what changed. In every other milestone, and in every commit within a
milestone that is not the one doing the intended change, they must be
byte-identical. That is the regression guard: not that output never moves, but
that it never moves by accident.

## Where the code goes

§35 settles it and the milestones follow it: `LicenseEngine` (`internal/license`)
resolves and retains, the component mapping layer derives the three attributes,
and `internal/foss` is a **writer** — it renders and touches no evidence. A
milestone that needs `internal/foss` to look at the graph has put something in
the wrong package (decision [Q21](../decisions.md)).

## Order

F1 and F2 are the foundation and are prerequisites for everything after them —
without them no attribution behaviour can be tested at all, and the reason is
in [../gap-analysis.md](../gap-analysis.md#the-testability-gap-which-is-the-real-blocker).
F3 is a correctness fix that the attribution data depends on. F4–F6 are the
data. F7 renders it. F8 is documentation and CI.

| ID | Title | Depends on | Size |
|---|---|---|---|
| [F1](F1-fixture.md) | Fixture project, source harvest, inventory dump | — | medium |
| [F2](F2-source-tree-relocation.md) | Source tree relocation | F1 | small |
| [F3](F3-component-root.md) | The component root as a resolved fact | F2 | medium |
| [F4](F4-license-artifacts.md) | Licence and NOTICE artifact retention | F3 | medium |
| [F5](F5-copyright.md) | Copyright statement extraction | F4 | small |
| [F6](F6-component-attributes.md) | Distribution role, linkage form, modification | F1 | medium |
| [F7](F7-foss-command.md) | The FOSS outputs: `generate --foss-out` and `sbomb foss` | F4, F5, F6 | large |
| [F8](F8-documentation-and-ci.md) | Documentation, catalogues, CI, Action | F7 | small |

F6 depends only on F1: it is graph work and can be built in parallel with
F2–F5 if two people are on this.
