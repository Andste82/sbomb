### Milestone 2 — Evidence Graph and Dump Format

**Goal:** the graph, its invariants, and a stable serialization, all testable without any adapter.

**Deliverables**

* `internal/evidence`: `Graph` per §38, deduplication (§8.2), confidence derivation table (§8.6) and downgrade logic (§8.7), invariant checks (§8.8), cycle detection, `Chains()`.
* Evidence dump format (Appendix C) with writer and loader.
* `sbomb evidence --load <dump>` prints statistics and runs invariants.

**Tests**

* Building a synthetic graph: artifact → object → TU → source; TU → header; package → generated → generator-input.
* Duplicate-edge merge keeps the highest confidence and records `supersededConfidence`.
* Confidence derivation matches the §8.6 table for all 18 combinations (table-driven).
* Downgrades are cumulative, floor at `unknown`, and are recorded sorted.
* Cycle detection returns an error.
* Invariant violations are detected: orphan node; unreachable file node.
* Dump → load → dump is byte-identical (round-trip golden).
* `Chains()` returns chains in deterministic order and respects the limit.

**Acceptance**

```
go test ./internal/evidence/... -race                                   # 0
sbomb evidence --load testdata/golden/synthetic-graph.json --format json \
  > /tmp/g.json && cmp /tmp/g.json testdata/golden/synthetic-graph-stats.json   # 0
```

**Definition of Done:** a synthetic evidence graph can be built, validated, serialized, reloaded, and inspected with no compiler adapter present.

---
