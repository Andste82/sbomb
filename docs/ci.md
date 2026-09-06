# CI

The composite action at `.github/actions/sbomb/action.yaml` downloads a pinned
release, verifies its SHA-256 checksum, and invokes the same CLI used locally.
It does not implement discovery itself.

```yaml
- uses: example/sbomb/.github/actions/sbomb@v0.0.0
  with:
    version: v0.0.0
    config: sbomb.json
    output: artifacts/sbomb.cdx.json
```

Build evidence must exist before the action runs. The action should be called
after the project build or after `cmake --build build --target sbomb` setup.

## Workflows

A job never repeats the name of its workflow, and says what it checks rather
than which command it runs.

| Workflow | Runs on | Job | Checks |
|---|---|---|---|
| `ci` | push, pull request | `gate` | Builds with and without network, vets, tests, formatted |
| | | `race` | No data races, with cgo enabled |
| | | `corpus` | Fixture corpus complete, free of host paths, unchanged by the tests |
| | | `performance-budget` | 10 000 translation units within the budget of section 31 |
| | | `findings-catalogue` | `docs/findings.md` matches the code and the specification |
| | | `spdx-drift` | Embedded licence digests match the upstream SPDX list (informational) |
| | | `end-to-end` | The CMake integration against a real toolchain |
| `determinism` | push, pull request | `hash` | Two runs on one platform produce one hash — linux/amd64, linux/arm64, windows/amd64 |
| | | `compare` | All three platforms produced the same hash |
| `release` | `v*` tag | `publish` | Reproducible build of the three targets, version matches the tag, checksums cover everything, release published |
| `smoke-test` | `v*` tag | `run` | The published binary, on Linux and Windows, against the committed fixture: the right files and only those |
| | | `compare` | Both platforms produced the same SBOM |

`release` is the only workflow with write access; the others are read-only.
`spdx-drift` is the only job that may fail without blocking, because the SPDX
list changes upstream.

A release carries the three executables and their checksums. Verify a download
before running it:

```
sha256sum --check --ignore-missing SHA256SUMS
```
