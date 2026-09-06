### Milestone 17 — Makefiles Generator Adapter

**Goal:** support `Unix Makefiles` / `NMake Makefiles` builds.

**Deliverables:** `internal/adapters/make` reading CMake's generated `CMakeFiles/<target>.dir/{build.make,link.txt,depend.make,compiler_depend.make}` and `DependInfo.cmake`; object→source mapping from `build.make`; link line from `link.txt`; depfiles from `compiler_depend.make` or `.d`.

**Tests:** the `gcc-12 + make` fixtures for all five projects; `link.txt` with response files; duplicate basenames; a target with no `compiler_depend.make` degrades to `.d` files.

**Acceptance**

```
go test ./internal/adapters/make/... -race                                          # 0
sbomb generate --build-dir testdata/fixtures/gcc-12-make/p02-static/build \
  --output /tmp/mk.cdx.json --reproducible                                          # 0
cmp /tmp/mk.cdx.json testdata/golden/gcc-12-make-p02.cdx.json                       # 0
```

**Definition of Done:** the inventory for the Make fixture is identical to the Ninja fixture of the same project except for build-anchored object paths — asserted by a dedicated cross-generator equivalence test.

---
