# CI

The composite action at `.github/actions/sbomb/action.yaml` downloads a pinned
release, verifies its SHA-256 checksum, and invokes the same CLI used locally.
It does not implement discovery itself.

```yaml
- uses: Andste82/sbomb/.github/actions/sbomb@v0.12.0
  with:
    version: v0.12.0
    build-dir: build
    config: sbomb.json
    policy: cra
    output: artifacts/sbomb.cdx.json
```

| Input | Default | Meaning |
|---|---|---|
| `version` | — | Release tag to download |
| `build-dir` | the workspace | The CMake build directory |
| `config` | — | Configuration file |
| `policy` | `default` | Policy profile |
| `output` | `sbomb.cdx.json` | Where the document goes |
| `reproducible` | `false` | Omit the timestamp |
| `path-flavor` | the host's | Pin to `posix` when comparing across platforms |
| `token` | `${{ github.token }}` | Needed while the repository is private; pass `""` to download anonymously |
| `repository` | this repository | Where the release lives |

Build evidence must exist before the action runs, so call it after the project
build.

While this repository is private its release assets are not downloadable
without credentials, and an unauthenticated request answers 404 rather than
403 -- which reads like a missing tag. The action therefore takes a `token`,
defaulting to `${{ github.token }}`, and downloads with `gh` when it has one.
Pass `token: ""` to download anonymously, which is what a consumer of a public
release wants.

`path-flavor` is empty by default, which means the host's. Pin it to `posix`
when a document produced on a Windows runner has to be byte-identical to one
produced on Linux: the flavors differ in case sensitivity, so the same evidence
otherwise yields two different documents by design (section 7.3).

## Workflows

A job never repeats the name of its workflow, and says what it checks rather
than which command it runs.

| Workflow | Runs on | Job | Checks |
|---|---|---|---|
| `ci` | push, pull request | `gate` | Builds with and without network, vets, tests, formatted |
| | | `race` | No data races, with cgo enabled |
| | | `corpus` | Fixture corpus complete, free of host paths, unchanged by the tests |
| | | `performance-budget` | 10 000 translation units within the budget of section 31 |
| | | `documentation` | The findings and property catalogues match the code and the specification, and every documented configuration loads |
| | | `spdx-drift` | Embedded licence digests and templates match the upstream SPDX list (informational) |
| | | `end-to-end` | The CMake integration against a real toolchain |
| `determinism` | push, pull request | `hash` | Two runs on one platform produce one hash — linux/amd64, linux/arm64, windows/amd64 |
| | | `compare` | All three platforms produced the same hash |
| `release` | `v*` tag | `publish` | Reproducible build of the five targets, version matches the tag, checksums cover everything, self-SBOMs validate, release published |
| `smoke-test` | called by `release` | `run` | The published binary, on Linux and Windows, against the committed fixture: the right files and only those |
| | | `compare` | Both platforms produced the same SBOM |

`release` calls `smoke-test` once the release exists rather than letting a tag
or a `release: published` event start it. A tag push raced the workflow that
creates the release the smoke test downloads; `release: published` never fired
at all, because GitHub does not start workflows from events created with the
default `GITHUB_TOKEN`.

Cutting a release is written down in `.claude/skills/release`.

`release` is the only workflow with write access; the others are read-only.
`spdx-drift` is the only job that may fail without blocking, because the SPDX
list changes upstream.

A release carries five executables -- linux/amd64, linux/arm64, windows/amd64,
darwin/amd64 and darwin/arm64 -- a CycloneDX SBOM beside each one, and the
checksums covering all of it. The macOS builds are cross-compiled and neither
signed nor notarized, so a browser download arrives quarantined; `xattr -d
com.apple.quarantine` clears it, and a `curl` download is unaffected.

Verify a download before running it:

```
sha256sum --check --ignore-missing SHA256SUMS
```
