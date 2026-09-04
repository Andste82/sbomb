### Milestone 8 — Compile and Header Evidence

**Goal:** the used source and header inventory.

**Deliverables**

* `internal/adapters/compiledb`: `compile_commands.json` parsing, including `arguments` vs `command` forms, response files, and `output` field.
* Header dependency collection from `.d` files, `ninja -t deps`, and DWARF (union per §4.4).
* Header classification (§14.4) driven by `toolchains-v1` implicit include directories, with the fallback path heuristic and `TOOLCHAIN_LAYOUT_UNKNOWN`.
* PCH handling (§14.5), unity-build detection and source recovery (§17.1) including the permitted `#include` parsing of the generated unity file.
* Non-C/C++ input handling (§14.6).

**Tests**

* GCC and Clang depfiles from fixtures; escaped paths; generated headers.
* A source present in `compile_commands.json` whose object is not linked MUST NOT appear.
* Header-only library (`p05-headeronly`) is included with no object.
* System headers excluded by default, included with `--include-system-headers`, and classified via toolchain data rather than hardcoded paths.
* Unity build fixture: sources recovered by `#include` parsing; a corrupted unity file yields `UNITY_SOURCE_UNRESOLVED`.
* PCH fixture: headers reachable only via PCH carry `viaPch`, and `pchHeaders=exclude` removes them and emits the finding with a count.
* A used TU with no dependency evidence → `MISSING_HEADER_DEPENDENCY_EVIDENCE`.

**Acceptance**

```
go test ./internal/adapters/compiledb/... ./internal/adapters/depfiles/... -race    # 0
sbomb generate --build-dir testdata/fixtures/gcc-13/p05-headeronly/build \
  --inventory-dump /tmp/inv.json --output /dev/null                                 # 0
cmp /tmp/inv.json testdata/golden/gcc-13-p05-inventory.json                         # 0
```

**Definition of Done:** the used source and header inventory is complete for the Ninja/GCC and Ninja/Clang fixtures, and unused files are provably absent.

---
