### Milestone F4 — Licence and NOTICE artifact retention

**Goal:** keep the bytes. This is requirement R2 and R4, and it is the milestone
the attribution document is actually made of.

**Depends on:** F3.

---

**Why the bytes and not the identifier**

For MIT and BSD the rights holder is inside the licence text. The canonical
SPDX text for `MIT` carries a placeholder where the holder belongs, so shipping
it does not discharge the obligation — it ships a template. The component's own
file is the deliverable; the identifier is an index into a catalogue.

**Deliverables**

* §22.9 of the specification, per [spec-delta.md §4](../spec-delta.md).

* A retained artifact per recognized file in the component root:

  ```go
  type LicenseArtifact struct {
      Kind       string        // "license" | "notice" | "copyright"
      File       domain.FileID // canonical, component-root relative
      SHA256     string
      Bytes      []byte        // verbatim, unmodified, including line endings
      DetectedID string        // SPDX id when detection succeeded, else ""
      Technique  string        // the §22.3 technique, or "" for none
  }
  ```

  on `domain.Component`, sorted by `(Kind, File)`. **All** matching files are
  retained, not the first that resolves — `licenseFromComponentRoot`
  (`internal/generate/components.go:1115`) returns early today and stops. The
  candidates it walks are no longer a fixed list of names: `licenseFilesIn`
  (`:1157`) lists the settled root once and matches its entries against §22.3,
  case-insensitively and including `LICENSE-<id>`, in a fixed order. Retention
  consumes that list rather than replacing it.

* **NOTICE and COPYRIGHT leave the identification chain.** They are retained
  for reproduction and are no longer inputs to §22.2. A NOTICE that quotes a
  licence is not evidence of the component's licence, and treating it as such
  is a wrong answer with high confidence.

* Limits per §30: 8 artifacts per component, 1 MiB each. Exceeding either
  bounds the **list** and emits `FOSS_LICENSE_ARTIFACT_LIMIT` (info); a
  retained file is never truncated, because a truncated licence is not a
  licence.

* CycloneDX binding, per [spec-delta.md §8](../spec-delta.md):
  `LicenseIdentifier` gains `Text *Attachment` and `Acknowledgement string`;
  the retained text is written to `evidence.licenses[].license.text` as
  `text/plain` + `base64`. `acknowledgement` is `declared` when the text came
  from the component's own files and `concluded` when a curated value decided
  the identifier.

* Policy setting `licenseTextInSBOM` ∈ `off` (default) | `evidence`, so
  `generate` keeps its current output size and the FOSS view asks for the text.

* Properties `sbomb:component:licenseFile` and `sbomb:component:noticeFile`,
  repeated and sorted, each `<canonicalPath>@sha256:<hex>`.

* Finding `FOSS_LICENSE_TEXT_MISSING` (info): a component with a resolved
  licence identifier and no retained text. That is precisely the case where
  attribution cannot be satisfied from what sbomb saw.

* **No fallback to a canonical SPDX text, ever.** If the component did not
  carry the text, the tool says so.

**Tests**

* `mit-lib`: text retained byte-for-byte, SHA-256 matches a hash computed
  independently in the test, the holder line is present in the retained bytes.
* `apache-lib`: `LICENSE` and `NOTICE` retained as distinct kinds; the NOTICE
  content does not appear as a licence finding.
* `multi-license`: both files retained; neither is dropped by the early return.
* `nolicense`: no artifact and no `FOSS_LICENSE_TEXT_MISSING` — there is no
  identifier either, which is the existing `UNKNOWN_LICENSE`.
* A component with a 2 MiB licence file: limit finding, list bounded, the
  retained file that *is* kept is complete.
* Retention adds no new file reads: the set of paths opened during a run is
  recorded in the test and compared against the run before retention. (No such
  recorder exists yet; the milestone adds a small one for the test, or asserts
  over the existing physical-path map. Do not claim the property without a
  mechanism that checks it.)
* Round-trip: the base64 in the document decodes to the retained bytes exactly.
* Schema validation passes at 1.6 and 1.7 (`internal/cyclonedx` validates
  in-process, so this is automatic once the field is written).

**Acceptance**

```
go test ./internal/license/... ./internal/cyclonedx/... -race           # 0
sbomb generate --build-dir testdata/fixtures/gcc-ninja/p14-foss/build \
  --source-dir testdata/fixtures/gcc-ninja/p14-foss/src \
  --inventory-dump /tmp/f4.json --output /tmp/f4.cdx.json --reproducible  # 0
cmp /tmp/f4.json testdata/golden/gcc-ninja-p14-foss-inventory.json      # 0
go run ./tools/findingsdoc --check                                      # 0
go run ./tools/propertydoc --check                                      # 0
```

**Intended golden change:** the inventory gains retained artifacts; the
CycloneDX document gains the two properties. With `licenseTextInSBOM=off` by
default the document does not gain the texts, so the size change is small — a
second golden with `evidence` covers the text path.

**Not in this milestone:** copyright lines, roles, the `foss` command.

**Definition of done:** every mapped component's licence and notice files are
available verbatim with their hashes, in the inventory and — on request — in
the document, and a component that carries no text says so.
