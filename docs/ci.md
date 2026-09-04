# CI

The composite action at `.github/actions/sbomb/action.yml` downloads a pinned
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