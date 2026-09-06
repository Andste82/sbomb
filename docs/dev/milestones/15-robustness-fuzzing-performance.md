### Milestone 15 — Robustness, Fuzzing, Performance

**Goal:** meet §30 and §31.

**Deliverables**

* Fuzz targets for: depfile, GNU/gold/lld/MSVC map, `build.ninja`, `.ninja_deps`, `compile_commands.json`, File API reply, ELF/PE reader, manifest, configuration, waivers.
* Enforced parser limits (line, token, recursion, total size) with dedicated findings.
* Benchmarks for map parsing, depfile parsing, graph build, hashing, serialization.
* A generated `large` fixture (50 000 files, 200 MB map) produced by a generator script rather than committed.

**Tests**

* Each fuzz target runs 60 s in CI with a seed corpus from Milestone 0 and reports zero crashes.
* Limits: a 2 MiB single line is rejected with `INPUT_LIMIT_EXCEEDED`, not an OOM.
* Zip-slip and `..` entries in a manifest are rejected.
* A symlink escaping all anchors is refused unless `--allow-unanchored-reads`.
* Benchmarks assert the §31 budgets in a `-tags perf` test on the CI runner class.

**Acceptance**

```
scripts/fuzz-all.sh 60s                                                             # 0
go test -tags perf -run TestPerformanceBudget ./...                                 # 0
```

**Definition of Done:** no parser can be crashed or made to allocate unboundedly by fixture-derived mutations, and the performance budgets hold.

---
