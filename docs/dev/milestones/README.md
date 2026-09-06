# Milestones

These files are extracted from [the specification](../sbomb-spec-v3.1.md#41-milestones) to support focused implementation work.

## Rules

The gate every change has to pass is in [../README.md](../README.md); it is
stated once so the two cannot drift apart. Beyond it, a milestone must:

- Keep the repository shippable; `cmd/sbomb` must build and `sbomb version` must work.
- Add at least one golden or table-driven test per new behaviour.
- Do not implement behaviour belonging to a later milestone.
- Update `docs/CHANGELOG.md` and `docs/dev/status.md`.
- Record unspecified decisions in `docs/dev/open-questions.md` rather than guessing.

Milestones 0-16 are the core track and must be implemented in order. Milestones 17, 19, and 20 are adapter tracks and may be implemented after Milestone 16. Milestones 18 and 21 are parked and must not be started until explicitly unparked.

## Files

- [00-fixture-harness-and-golden-corpus.md](00-fixture-harness-and-golden-corpus.md) - Milestone 0 — Fixture Harness and Golden Corpus
- [01-cli-configuration-path-model-empty-sbom.md](01-cli-configuration-path-model-empty-sbom.md) - Milestone 1 — CLI, Configuration, Path Model, Empty SBOM
- [02-evidence-graph-and-dump-format.md](02-evidence-graph-and-dump-format.md) - Milestone 2 — Evidence Graph and Dump Format
- [03-cmake-file-api-adapter.md](03-cmake-file-api-adapter.md) - Milestone 3 — CMake File API Adapter
- [04-link-evidence-i-dependency-file-and-trace.md](04-link-evidence-i-dependency-file-and-trace.md) - Milestone 4 — Link Evidence I: Dependency File and Trace
- [05-link-evidence-ii-dwarf-binary-inspection.md](05-link-evidence-ii-dwarf-binary-inspection.md) - Milestone 5 — Link Evidence II: DWARF / Binary Inspection
- [06-link-evidence-iii-map-parsers-fallback.md](06-link-evidence-iii-map-parsers-fallback.md) - Milestone 6 — Link Evidence III: Map Parsers (Fallback)
- [07-ninja-buildgraph-and-object-source-resolution.md](07-ninja-buildgraph-and-object-source-resolution.md) - Milestone 7 — Ninja Buildgraph and Object→Source Resolution
- [08-compile-and-header-evidence.md](08-compile-and-header-evidence.md) - Milestone 8 — Compile and Header Evidence
- [09-inventory-classification-hashing-staleness.md](09-inventory-classification-hashing-staleness.md) - Milestone 9 — Inventory, Classification, Hashing, Staleness
- [10-component-mapping-versions-and-purls.md](10-component-mapping-versions-and-purls.md) - Milestone 10 — Component Mapping, Versions, and PURLs
- [11-license-resolution.md](11-license-resolution.md) - Milestone 11 — License Resolution
- [12-cyclonedx-writer.md](12-cyclonedx-writer.md) - Milestone 12 — CycloneDX Writer
- [13-policy-findings-waivers-report-explain.md](13-policy-findings-waivers-report-explain.md) - Milestone 13 — Policy, Findings, Waivers, Report, Explain
- [14-determinism-and-cross-platform-reproducibility.md](14-determinism-and-cross-platform-reproducibility.md) - Milestone 14 — Determinism and Cross-Platform Reproducibility
- [15-robustness-fuzzing-performance.md](15-robustness-fuzzing-performance.md) - Milestone 15 — Robustness, Fuzzing, Performance
- [16-cmake-integration-github-action-release.md](16-cmake-integration-github-action-release.md) - Milestone 16 — CMake Integration, GitHub Action, Release
- [17-makefiles-generator-adapter.md](17-makefiles-generator-adapter.md) - Milestone 17 — Makefiles Generator Adapter
- [18-msvc-msbuild-adapter-parked.md](18-msvc-msbuild-adapter-parked.md) - Milestone 18 — MSVC / MSBuild Adapter — **PARKED**
- [19-packaging-images-and-assets.md](19-packaging-images-and-assets.md) - Milestone 19 — Packaging, Images, and Assets
- [20-esp-idf-sdk-adapter.md](20-esp-idf-sdk-adapter.md) - Milestone 20 — ESP-IDF SDK Adapter
- [21-iar-and-vendor-linkers-parked.md](21-iar-and-vendor-linkers-parked.md) - Milestone 21 — IAR and Vendor Linkers — **PARKED**

The original specification remains authoritative if this extracted directory ever drifts.

## Phase plans

The milestones above describe what to build. The phase plans below record what
was actually built, in the order the work was done, together with the defects
each phase uncovered. They are the working state of the roadmap.

- [phase-06-plan.md](phase-06-plan.md) - Header evidence in full — complete
- [phase-07-plan.md](phase-07-plan.md) - Adapter breadth — complete, ESP-IDF parked
