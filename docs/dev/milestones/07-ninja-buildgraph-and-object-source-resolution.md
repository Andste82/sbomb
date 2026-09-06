### Milestone 7 — Ninja Buildgraph and Object→Source Resolution

**Goal:** map linked objects to sources deterministically, including duplicate basenames.

**Deliverables**

* `internal/adapters/ninja`: `build.ninja` parser (rules, variables including `$in`/`$out`, `include`/`subninja`, `$:` and `$ ` escaping, response-file rules), plus optional `ninja -t deps` / `-t commands` invocation via `internal/exec`.
* `.ninja_deps` binary reader (best-effort, version-guarded; on unsupported version, degrade to `ninja -t deps` or `.d` files).
* `internal/inventory/resolver`: the §13.2 object→source algorithm with strategy ordering, conflict detection, and per-strategy provenance recorded in edge attributes.

**Tests**

* Object→source mapping for all fixtures; `p03-dupnames` produces two distinct `main.cpp` sources under different targets.
* Windows path escaping `C$:/src/main.cpp` round-trips.
* Archive context is preserved (member → object → source).
* Missing source in the graph → `LINKED_OBJECT_SOURCE_UNRESOLVED`, object promoted to a file component.
* Strategy conflict between File API and Ninja → `OBJECT_SOURCE_MAPPING_CONFLICT`, File API wins.
* Basename-only matching is never used: a test with two identical basenames and deliberately missing structured metadata MUST yield `unresolved`, not a guess.

**Acceptance**

```
go test ./internal/adapters/ninja/... ./internal/inventory/... -race              # 0
sbomb evidence --build-dir testdata/fixtures/gcc-13/p03-dupnames/build \
  --format json > /tmp/n.json && cmp /tmp/n.json testdata/golden/gcc-13-p03-evidence.json  # 0
```

**Definition of Done:** map/dependency-file evidence plus the buildgraph produces source-level evidence for every linked object, or an explicit unresolved finding.

---
