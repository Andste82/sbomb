### Milestone 12 — CycloneDX Writer

**Goal:** the real SBOM.

**Deliverables**

* `internal/sbomwriter`: the format-neutral `Document` and `Writer` interface of §36.1, plus the registry. Discovery hands over a `Document`; nothing above this layer knows CycloneDX exists.
* `internal/cyclonedx`: the CycloneDX 1.6 writer built on `cyclonedx-go`, implementing §28 in full — flat components, dependency graph, bom-refs, native `evidence.identity`/`occurrences`, licenses and NOASSERTION encoding, namespaced properties (Appendix B), hashes, external references, metadata, the build-environment component, and the §1.5 CRA properties.
* The §29 ordering pass, applied to the model **before** serialization, because library serialization order is not a guarantee.
* In-process validation per §32.5: embedded CycloneDX 1.6 JSON Schema plus the semantic checks, run on the exact output bytes, with atomic write-then-rename.

**Tests**

* Golden SBOM for the Ninja/GCC fixture, byte-identical under `--reproducible`.
* Every `bom-ref` appears exactly once in `dependencies[]`.
* No nested `components[].components`.
* All property names start with `sbomb:` and appear in Appendix B.
* NOASSERTION encoding present with review properties.
* Ordering rules of §29 verified by shuffling internal maps before serialization.
* Structural validator rejects: duplicate bom-ref, dangling dependency ref, bad hash length, bad purl, bad timestamp.
* Output validates against the embedded CycloneDX 1.6 schema in-process, in a normal (untagged) test.
* A deliberately corrupted document fails validation and no file is left at the output path (atomic-write test).
* The `cra` profile's field-completeness check fails on a document missing a component supplier, and passes on the complete fixture.

**Acceptance**

```
go test ./internal/cyclonedx/... -race                                              # 0
sbomb generate --build-dir testdata/fixtures/gcc-13/p02-static/build \
  --config testdata/config/p02-full.json --output /tmp/p02.cdx.json --reproducible  # 0
cmp /tmp/p02.cdx.json testdata/golden/gcc-13-p02.cdx.json                           # 0
sbomb validate --input /tmp/p02.cdx.json                                         # 0
```

**Definition of Done:** a usable, deterministic CycloneDX 1.6 SBOM is generated for a CMake/Ninja/GCC fixture and passes structural validation.

---
