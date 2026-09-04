### Milestone 21 — IAR and Vendor Linkers — **PARKED**

**Status: parked by decision. Do not implement until fixtures are supplied.** An implementing agent MUST skip this milestone and MUST NOT attempt to obtain a vendor toolchain.

**Goal (when unparked):** support toolchains that cannot be obtained or executed in CI.

**Constraint (important for an implementing agent):** IAR, ARMCC, and most vendor toolchains require commercial licenses and cannot be installed by an agent. This milestone is therefore **fixture-driven only**. The agent MUST NOT attempt to download or install these toolchains. If the required fixtures are absent, the milestone is blocked and the agent MUST record that in `docs/status.md` rather than fabricating fixtures.

**Deliverables:** `internal/adapters/linkers/iar` (ILINK map + `.d` output); an `experimental` marker on the adapter; documentation of how a user contributes a fixture (`docs/contributing-fixtures.md`) including a redaction checklist.

**Tests:** golden parses of contributed map fixtures; a synthetic-but-labelled map only for negative/robustness tests, never for correctness claims.

**Acceptance**

```
go test ./internal/adapters/linkers/iar/... -race                                   # 0
```

Skipped with a clear message when fixtures are absent, and the skip MUST be visible in CI output.

**Definition of Done:** the core inventory and evidence graph are unchanged by adding the adapter — asserted by running the full core test suite with the adapter registered.

---
