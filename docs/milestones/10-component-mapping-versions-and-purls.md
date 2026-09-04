### Milestone 10 — Component Mapping, Versions, and PURLs

**Goal:** every used file gets a component; every component gets a version or an explicit finding.

**Deliverables**

* `internal/componentmap`: the §19.2 strategy chain, longest-prefix matching, glob support, Git submodule boundaries, unknown components (§19.3).
* `internal/version`: the §20 resolution chain, confidence, header-macro reading, purl construction (§20.4), supplier/externalReferences.
* Git metadata via `internal/exec` (introspection-gated), with URL normalization and dirty detection.
* Package-manager adapters: FetchContent and CPM first (they are pure filesystem+Git); Conan and vcpkg in this milestone if their fixtures exist, otherwise deferred with a stub and a recorded finding.

**Tests**

* Nested curated mappings: longest prefix wins.
* A file under `dep/` with no metadata → `UNKNOWN_COMPONENT`, still present in the inventory.
* Git submodule boundary detection from a fixture repository created in the test.
* Version from curated config, Git tag, Git describe with distance, header macro, and none.
* purl percent-encoding for names containing `+` and `@`.
* `versionFrom` restricting strategies is honoured.
* Component bom-ref collision path: two components with the same name and version get distinct refs via the root-hash suffix.

**Acceptance**

```
go test ./internal/componentmap/... ./internal/version/... -race                    # 0
sbomb generate --build-dir testdata/fixtures/gcc-13/p02-static/build \
  --config testdata/config/p02-components.json --inventory-dump /tmp/c.json \
  --output /dev/null --reproducible                                                 # 0
cmp /tmp/c.json testdata/golden/gcc-13-p02-components.json                          # 0
```

**Definition of Done:** every used file receives exactly one primary component or an `UNKNOWN_COMPONENT` finding, and every component has a version or `UNKNOWN_VERSION`.

---
