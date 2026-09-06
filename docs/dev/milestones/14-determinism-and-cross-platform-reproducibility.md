### Milestone 14 — Determinism and Cross-Platform Reproducibility

**Goal:** prove the path/identity model actually works.

**Deliverables**

* `--reproducible` completeness; `SOURCE_DATE_EPOCH` handling.
* A `testdata/fixtures/portable/` fixture whose captured evidence is host-independent (sentinel roots from Milestone 0).
* **Path flavors.** `internal/pathmodel` MUST expose an injectable flavor (`PosixFlavor`, `WindowsFlavor`) covering separator handling, case sensitivity, drive letters, and UNC prefixes, selected from `runtime.GOOS` by default and overridable in tests and via the hidden `--path-flavor` flag. This is what makes Windows behaviour testable without a Windows runner.
* A CI matrix job running the same fixture on `ubuntu-latest` (x86_64), `ubuntu-24.04-arm` (ARM64) and `windows-latest` (x86_64), and comparing output hashes.

  **[Amended.]** This originally read "there is no Windows runner", and the Windows binary was to be declared untested. That was a property of the host the milestone was written for -- an internal GitHub Enterprise instance with Linux runners only -- and not a design decision. On github.com a Windows runner is available and is used, so the claim that a Windows build produces the same document is checked rather than declared unverified.

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

**Definition of Done:** byte-identical output across architectures *and platforms* for the portable fixture. Path semantics stay covered by flavor tests, which is where string-level behaviour belongs; the Windows runner covers what a flavor test cannot reach -- that the binary runs at all, and that a case-insensitive filesystem does not change the result.

---
