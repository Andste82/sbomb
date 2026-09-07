### Milestone F5 — Copyright statement extraction

**Goal:** the other half of attribution. For MIT and BSD the copyright line is
not decoration: the licence says the notice shall be reproduced.

**Depends on:** F4.

---

**Deliverables**

* §22.10 of the specification, per [spec-delta.md §5](../spec-delta.md).

* Extraction from **used files** and the artifacts retained in F4. Nothing
  else, and no directory walk.

* **It rides on the hashing pass, it does not add one** (decision
  [Q9](../decisions.md)). `hashOne` (`internal/inventory/inventory.go:166`)
  already does `os.ReadFile`, so the bytes are in memory when the SHA-256 is
  computed. `HashOptions` gains `Observe func(domain.FileID, []byte)`, supplied
  by the `generate` layer. Zero extra I/O against the §31 budget, and the §35
  layering holds: `internal/inventory` learns nothing about licences.

* The first 64 KiB of a file, matching the window §22.2 rule 2 already uses for
  SPDX identifiers.

* Exactly two recognized forms, both RE2 (§30.6 forbids backtracking):
  * `SPDX-FileCopyrightText: <text>`
  * a line containing `Copyright`, an optional `(c)` / `(C)` / `©`, an optional
    year or year range, and a holder.

* The **original line** is stored verbatim with the `FileID` it came from.
  Deduplication uses a normalized key — collapsed whitespace, unified `(c)`,
  merged year ranges for an otherwise identical holder — and the key is never
  stored and never displayed. Among duplicates the entry kept is the first in
  canonical file order, so the result does not depend on map iteration.

* Limit: 200 statements per component; excess emits `FOSS_COPYRIGHT_LIMIT`
  (info) and truncates deterministically.

* CycloneDX binding: `evidence.copyright[].text`, sorted by text.
  `component.copyright` is written **only** from a curated value, because
  `component.copyright` is a conclusion and `evidence.copyright` is an
  observation — the same separation §22.4 already draws for licences.

* Finding `FOSS_COPYRIGHT_MISSING` (info) for a distributed component with no
  statement.

* **No inference.** A holder is never derived from a repository URL, a
  directory name or a package owner, and a year range is never reformatted in
  the stored value.

**Tests**

* Both forms; a file with two holders; a file with none.
* A year-range merge affects only the dedup key — the output shows the original
  strings, including the differing years.
* `SPDX-FileCopyrightText` and a classic line naming the same holder collapse
  to one entry, and the kept entry is deterministic across shuffled input
  order.
* `nocopyright`: finding emitted, component otherwise unchanged.
* Non-UTF-8 bytes inside the 64 KiB window do not panic and do not corrupt the
  stored value.
* A 300-statement component: limit finding, deterministic truncation.
* A fuzz target for the extractor, joining `scripts/fuzz-all.sh`.
* The run opens no file it did not open before: the observed path set with
  extraction on equals the set with it off.

**Acceptance**

```
go test ./internal/license/... -race                                    # 0
go test -run Fuzz -fuzz FuzzCopyright -fuzztime 30s ./internal/license/ # 0
sbomb generate --build-dir testdata/fixtures/gcc-ninja/p14-foss/build \
  --source-dir testdata/fixtures/gcc-ninja/p14-foss/src \
  --inventory-dump /tmp/f5.json --output /tmp/f5.cdx.json --reproducible  # 0
cmp /tmp/f5.json testdata/golden/gcc-ninja-p14-foss-inventory.json       # 0
go run ./tools/findingsdoc --check                                       # 0
```

**Intended golden change:** components gain `evidence.copyright`.

**Not in this milestone:** deciding what the copyright statements mean, or
rendering them.

**Definition of done:** every distributed component carries its verbatim
copyright statements or an explicit finding that it has none, and nothing in
the stored value has been rewritten.
