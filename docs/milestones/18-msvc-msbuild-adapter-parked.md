### Milestone 18 — MSVC / MSBuild Adapter — **PARKED**

**Status: parked by decision. Do not implement until explicitly unparked.** An implementing agent MUST skip this milestone. The section is retained so the design is not lost and so nothing in Milestones 1–16 quietly assumes MSVC will never exist.

Parking rationale: there is no Windows CI runner, PDB parsing is out of scope, and the embedded targets that motivate this tool do not use MSVC. Unparking requires a Windows runner or committed MSVC fixtures plus a decision on PDB support.

**Goal (when unparked):** Windows/MSVC support. This is a full milestone on its own because the Visual Studio generator produces no `compile_commands.json`.

**Deliverables**

* `internal/adapters/msbuild`: `.vcxproj` reading (sources, configuration/platform), `.tlog` parsing (`CL.read.1.tlog`, `CL.write.1.tlog`, `link.read.1.tlog`, `link.write.1.tlog`, `RC.*.tlog`), which supplies both object→source mapping and header dependencies.
* `/showIncludes` build-log parsing as a fallback, including localized "Note: including file:" prefixes (prefix taken from the log's own first occurrence pattern, not hardcoded English).
* MSVC map parser completion, `/OPT:REF` discarded-symbol handling, `link /VERBOSE:LIB` trace.
* Multi-config selection for the Visual Studio generator (§10.3).
* UTF-16LE-with-BOM decoding for `.tlog` files.

**Tests:** committed Windows fixtures from Milestone 0; UTF-16 tlog decoding; localized `/showIncludes`; `x64` vs `Win32` platform selection; duplicate basenames across projects; a `.rc` resource input.

**Acceptance**

```
go test ./internal/adapters/msbuild/... ./internal/adapters/linkers/msvc/... -race  # 0
sbomb generate --build-dir testdata/fixtures/msvc-vs17/p02-static/build \
  --config-name Release --output /tmp/ms.cdx.json --reproducible                    # 0
cmp /tmp/ms.cdx.json testdata/golden/msvc-vs17-p02.cdx.json                         # 0
```

When unparked, these tests MUST run from committed fixtures on Linux CI, since no Windows runner exists.

**Definition of Done:** an MSVC-built project produces an SBOM equivalent in content to the GCC-built version of the same project.

---
