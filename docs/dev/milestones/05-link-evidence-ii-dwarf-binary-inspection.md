### Milestone 5 — Link Evidence II: DWARF / Binary Inspection

**Goal:** an artifact-intrinsic evidence source that survives archives and LTO and needs no build-tree cooperation.

**Deliverables**

* `internal/adapters/binfmt`: ELF and PE opening; GNU build-id and PDB signature extraction; `DT_NEEDED` / import table enumeration (behind `--include-runtime-libraries`). Mach-O and PDB parsing are out of scope.
* DWARF CU enumeration (`DW_AT_name`, `DW_AT_comp_dir`) → translation-unit nodes with `debug-info` edges.
* DWARF line-table file-table extraction → header evidence with `direct` vs transitive flag where the line program permits.
* Graceful degradation on stripped binaries (`DEBUG_INFO_UNAVAILABLE`).
* LTO detection from ELF sections and flags (§17.2).

**Tests**

* Unstripped `p01-hello` artifact yields exactly the expected CU set and header set.
* Stripped artifact yields the informational finding and no crash.
* PE artifact from the mingw fixture yields CUs from its DWARF sections; a synthetic MSVC-style PE with only a PDB reference yields `DEBUG_INFO_UNAVAILABLE` rather than an error.
* Truncated / corrupted ELF returns a finding, not a panic (fuzz corpus).
* §4.4 semantics: under the default `headerEvidence=dwarf-preferred`, the DWARF header set is authoritative for CUs it covers; headers seen only in the depfile for such a CU are excluded but counted, and the count appears in the report. Under `union` both sets are merged. Under `depfiles` DWARF header evidence is ignored. All three modes are covered by table-driven tests on the same fixture.
* A CU without DWARF coverage falls back to its depfile and emits `HEADER_EVIDENCE_FALLBACK`.

**Acceptance**

```
go test ./internal/adapters/binfmt/... -race                                       # 0
sbomb evidence --build-dir testdata/fixtures/gcc-13/p01-hello/build \
  --adapters cmakeapi,dwarf --format json > /tmp/d.json && \
  cmp /tmp/d.json testdata/golden/gcc-13-p01-dwarf-evidence.json                   # 0
```

**Definition of Done:** translation units and headers can be recovered from the artifact alone, and the result agrees with the depfile-derived set on the fixtures.

---
