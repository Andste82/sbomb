### Milestone 19 — Packaging, Images, and Assets

**Goal:** cover firmware inputs outside the compiler/linker graph.

**Deliverables:** `internal/adapters/packaging` supporting the native manifest (Appendix E), CMake custom-command chains, `install_manifest.txt`, and generic image manifests; asset classification; assembly-mode product roots.

**Tests:** a firmware manifest fixture producing bootloader + application + filesystem; a transformed asset chain (`asset.json → generated.asset.bin → filesystem.img → package`); an asset adjacent to a generated output that MUST NOT be included; assembly mode with a shared file used by two artifacts appearing once with both artifact refs.

**Acceptance**

```
go test ./internal/adapters/packaging/... -race                                     # 0
sbomb generate --build-dir testdata/fixtures/firmware/build --mode assembly \
  --config testdata/config/firmware.json --output /tmp/fw.cdx.json --reproducible   # 0
cmp /tmp/fw.cdx.json testdata/golden/firmware-assembly.cdx.json                     # 0
```

**Definition of Done:** a product assembly SBOM with multiple artifacts, packaged assets, and correct shared-file handling.

---
