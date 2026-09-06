### Milestone 11 — License Resolution

**Goal:** license results with explicit evidence class, confidence, and conflict handling.

**Deliverables**

* `internal/license`: the §22.2 priority chain; SPDX identifier extraction; embedded SPDX license text hash table (generated at build time from the SPDX license list into a Go file, committed); normalization per §22.3; conflict detection; NOASSERTION with reason codes; external scanner ingestion (`--license-scan`).

**Tests**

* SPDX header in a source file.
* `LICENSE` file exactly matching an SPDX text (hash match) and after normalization (copyright lines stripped).
* An unrecognized `LICENSE` file → NOASSERTION with `license-text-unrecognized`, never a guess.
* Curated override beats a file SPDX identifier and emits `LICENSE_CONFLICT` with the conflicting value recorded.
* A license found in a header does not propagate to the including source.
* Multiple licenses in a component are not merged into an expression.
* Every NOASSERTION has a reason code.

**Acceptance**

```
go test ./internal/license/... -race                                                # 0
sbomb generate --build-dir testdata/fixtures/gcc-13/p02-static/build \
  --config testdata/config/p02-licenses.json --inventory-dump /tmp/li.json \
  --output /dev/null --reproducible                                                 # 0
cmp /tmp/li.json testdata/golden/gcc-13-p02-licenses.json                           # 0
```

**Definition of Done:** every component has a license result or an explicit unresolved state with a reason, and no license is produced by similarity matching.

---
