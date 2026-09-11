# CI

The composite action at `.github/actions/sbomb/action.yaml` downloads a pinned
release, verifies its SHA-256 checksum, and invokes the same CLI used locally.
It does not implement discovery itself.

`<sbomb-version>` is a release tag, as written on the
[releases page](https://github.com/Andste82/sbomb/releases) — `v` and all.
The documentation names no particular one on purpose: a version written out
here is a version that goes stale the next time one is cut.

```yaml
- uses: Andste82/sbomb/.github/actions/sbomb@<sbomb-version>
  with:
    version: <sbomb-version>
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

| Workflow | Runs on | Job | Checks and purpose |
|---|---|---|---|
| `ci` | push, pull request | `gate` | Builds with and without network, vets, tests and formats; proves ordinary source changes remain buildable and clean. |
| | | `race` | Runs with cgo and the race detector; checks concurrent evidence collection for data races. |
| | | `corpus` | Checks complete, portable fixtures and verifies tests leave them unchanged; protects committed golden inputs. |
| | | `msvc-corpus` | Regenerates MSVC/Ninja fixtures on Windows and parses them; proves native evidence coverage still works. |
| | | `performance-budget` | Measures the 10,000-translation-unit case against its time and memory limits; catches scalability regressions. |
| | | `documentation` | Checks generated catalogues and loads documented configs; prevents docs from describing unsupported behavior. |
| | | `spdx-drift` | Runs `spdxgen --check` against current SPDX data; verifies generated hashes and templates stay current so known licenses do not become `NOASSERTION` (informational). |
| | | `end-to-end` | Runs the real CMake integration with a toolchain; catches wiring errors package tests cannot exercise. |
| `determinism` | push, pull request | `hash` | Repeats the same build on Linux and Windows targets; detects timestamps or ordering that change output. |
| | | `compare` | Compares hashes across platforms; proves equivalent evidence produces equivalent SBOM bytes. |
| `release` | `v*` tag | `publish` | Rebuilds five targets, validates versions, checksums and self-SBOMs, then publishes; protects the release artifact set. |
| `smoke-test` | called by `release` | `run` | Downloads and runs the published binaries on Linux and Windows; catches packaging or upload errors source CI cannot see. |
| | | `compare` | Compares the published platform SBOMs; confirms the release behaves consistently outside the build runner. |

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
