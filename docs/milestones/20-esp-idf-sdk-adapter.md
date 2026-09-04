### Milestone 20 — ESP-IDF SDK Adapter

**Goal:** the first concrete embedded SDK adapter.

**Deliverables:** `internal/adapters/sdk/espidf` reading `project_description.json`, component directories, `idf_component.yml` / `dependencies.lock`, `sdkconfig`, `partitions.csv`, and the generated bootloader/application/partition binaries; anchor registration `sdk:esp-idf`; component boundaries at IDF component granularity; versions and purls per §20.4.

**Tests:** a committed ESP-IDF build fixture (captured once, checked in, since the IDF toolchain is large); component boundary correctness; managed components get `pkg:idf/...` purls; IDF headers classify as `sdk-header`; the partition table drives assembly-mode artifacts.

**Acceptance**

```
go test ./internal/adapters/sdk/espidf/... -race                                    # 0
sbomb generate --build-dir testdata/fixtures/espidf/blink/build --mode assembly \
  --output /tmp/idf.cdx.json --reproducible                                         # 0
cmp /tmp/idf.cdx.json testdata/golden/espidf-blink.cdx.json                         # 0
```

**Definition of Done:** a realistic embedded CMake project produces a complete evidence-based SBOM with correct component boundaries and versions.

---
