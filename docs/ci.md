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

| Workflow | Runs on | What it does |
|---|---|---|
| `ci` | every push and pull request | The gate: build, vet, test, gofmt, the race detector, the fixture corpus, the performance budget, the findings catalogue and the end-to-end tests |
| `determinism` | every push and pull request | The same build on amd64 and arm64, then a check that both produced the same SBOM hash |
| `release` | a `v*` tag | Builds `linux/amd64`, `linux/arm64` and `windows/amd64`, checks that the build is reproducible and that the binary reports the tag, then publishes the release with `SHA256SUMS` |
| `action-smoke` | a `v*` tag | Runs the composite action against the release that was just published, on Ubuntu and Windows |

`release` is the only workflow with write access; the others are read-only.

A release carries the three executables and their checksums. Verify a download
before running it:

```
sha256sum --check --ignore-missing SHA256SUMS
```
