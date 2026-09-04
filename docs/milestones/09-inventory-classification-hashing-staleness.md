### Milestone 9 — Inventory, Classification, Hashing, Staleness

**Goal:** a deterministic internal inventory independent of CycloneDX.

**Deliverables**

* `internal/inventory`: evidence merge, file classification, deduplication, bounded parallel hashing, missing-file recording, size capture.
* Staleness detection (§27) with the full precedence chain.
* Inventory dump format (§40).

**Tests**

* Duplicate evidence from two adapters collapses to one `UsedFile` with merged properties.
* Hash correctness against known vectors; raw bytes, no normalization; CRLF file hashes differ from LF.
* Missing file → no hash + `MISSING_FILE_HASH`.
* Generated and prebuilt files classify correctly.
* Staleness: touching a source after the artifact yields `STALE_BUILD_EVIDENCE`; a successful build-id correlation suppresses a timestamp-only violation.
* Hashing is order-independent: shuffling input order yields an identical dump.
* The tool reads no file outside the evidence set (asserted with an `fs.FS` wrapper that records opens).

**Acceptance**

```
go test ./internal/inventory/... -race                                              # 0
sbomb generate --build-dir testdata/fixtures/gcc-13/p02-static/build \
  --inventory-dump /tmp/i.json --output /dev/null --reproducible                    # 0
cmp /tmp/i.json testdata/golden/gcc-13-p02-inventory.json                           # 0
```

**Definition of Done:** the inventory is deterministic, hashed, classified, staleness-checked, and produced without any CycloneDX code path.

---
