### Milestone 14 — Determinism and Cross-Platform Reproducibility

**Goal:** prove the path/identity model actually works.

**Deliverables**

* `--reproducible` completeness; `SOURCE_DATE_EPOCH` handling.
* A `testdata/fixtures/portable/` fixture whose captured evidence is host-independent (sentinel roots from Milestone 0).
* **Path flavors.** `internal/pathmodel` MUST expose an injectable flavor (`PosixFlavor`, `WindowsFlavor`) covering separator handling, case sensitivity, drive letters, and UNC prefixes, selected from `runtime.GOOS` by default and overridable in tests and via the hidden `--path-flavor` flag. This is what makes Windows behaviour testable without a Windows runner.
* A CI matrix job running the same fixture on `ubuntu-latest` (x86_64) and `ubuntu-24.04-arm` (ARM64) and comparing output hashes. **There is no Windows runner**; the Windows binary is cross-compiled, smoke-tested only by `sbomb version` under Wine if available, and otherwise declared untested in `docs/status.md`.

**Tests**

* `TestByteIdenticalAcrossRuns` — 10 runs, identical bytes.
* `TestByteIdenticalWithShuffledInputOrder` — adapters run in randomized order; output identical.
* `TestNoAbsolutePathsInOutput` — the SBOM contains no `/` -prefixed absolute path, no drive letters, no `\`.
* `TestSourceDateEpoch` — output identical with the same epoch, differs only in timestamp otherwise.
* `TestWindowsFlavorOnLinux` — the whole `win-synthetic` fixture and the mingw fixture are processed with `--path-flavor windows` on Linux and produce the golden output; this is the substitute for a Windows runner.
* CI job `determinism` asserts identical SHA-256 of the generated SBOM across both Linux runners.

**Acceptance**

```
go test ./... -run TestByteIdentical -race                                          # 0
scripts/determinism-check.sh                                                        # 0
```

CI must show the same SBOM SHA-256 on Linux x86_64 and Linux ARM64.

**Definition of Done:** byte-identical output across architectures for the portable fixture, and Windows path semantics proven by flavor tests rather than by a Windows runner. Any Windows-specific behaviour that cannot be expressed as a flavor test MUST be listed in `docs/status.md` as unverified.

---
