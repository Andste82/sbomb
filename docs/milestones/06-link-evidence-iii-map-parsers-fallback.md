### Milestone 6 — Link Evidence III: Map Parsers (Fallback)

**Goal:** support toolchains without `--dependency-file`, using format-sniffed parsers.

**Deliverables**

* `internal/adapters/linkers/{gnuld,gold,lld,msvc}`: streaming parsers per §11.5, format sniffing, extraction of objects, archives, extracted members, shared libraries, discarded input sections, and (when enabled) linker scripts.
* Section-GC handling per §4.5.
* Whole-archive handling per §4.6, including reading `ar` archives directly (`internal/adapters/archive`, implementing the common `ar` format for member enumeration).

**Tests**

* Golden parses for every map in the Milestone 0 corpus; `p02-static` yields exactly one extracted member.
* Format sniffing selects the correct parser for each map; an unknown format yields `AMBIGUOUS_ADAPTER_SELECTION` or `MALFORMED_LINK_EVIDENCE` as appropriate.
* Truncated map → partial result + `MALFORMED_LINK_EVIDENCE`.
* 200 MB synthetic map parses within the §31 budget (benchmark, not asserted in unit tests).
* Discarded-sections block is parsed; `sectionGarbageCollection=exclude` removes the object and emits `SECTION_GC_EXCLUDED`.
* Thin archive members resolve relative to the archive directory.
* Fuzz target per parser.

**Acceptance**

```
go test ./internal/adapters/linkers/... ./internal/adapters/archive/... -race     # 0
go test -bench BenchmarkMapParse -benchtime 1x ./internal/adapters/linkers/...    # 0
sbomb evidence --build-dir testdata/fixtures/gcc-12/p02-static/build \
  --adapters cmakeapi,map --format json > /tmp/m.json && \
  cmp /tmp/m.json testdata/golden/gcc-12-p02-map-evidence.json                    # 0
```

**Definition of Done:** the tool produces the same linked-unit set from the map as from the dependency file on every fixture where both exist; a golden test asserts that equivalence.

---
