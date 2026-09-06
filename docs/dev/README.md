# Development documentation

These documents describe how sbomb is built and why it behaves as it does.
They are living documents: they are expected to change with the code, and a
change that makes one of them wrong is not finished.

## The gate

Every change has to pass all of it. Nothing here is optional, and a step that
cannot run is a deviation to be recorded, not a step to skip.

```
go build ./...                          # compiles
go build -mod=vendor ./...              # builds with no network (section 37.3)
go vet ./...                            # vets clean
gofmt -l $(go list -f '{{.Dir}}' ./...) # prints nothing
go test ./...                           # passes
CGO_ENABLED=1 go test ./... -race       # the race detector needs cgo (deviations.md D2)
go test -tags e2e ./test/...            # end-to-end, needs a real toolchain
tools/fixtures/regen.sh --check         # the committed corpus is complete
scripts/determinism-check.sh            # two runs produce the same bytes
go run ./tools/findingsdoc --check      # the findings catalogue matches the code
go run ./tools/docexamples             # the documented configurations load
```

Two checks are not in the gate because they need every fixture toolchain
installed, which CI does not have. Run them when the corpus or the release
path changes:

```
tools/fixtures/check-reproducible.sh    # two regenerations agree
scripts/release.sh build                # the release artifacts and their SBOMs
```

## What is here

| Document | Contents |
|---|---|
| [spec.md](spec.md) | The normative specification. Section numbers referenced throughout the code and the commit messages point here. |
| [milestones/](milestones/) | The milestone plan extracted from section 41, plus the per-phase plans that record what was built and why. |
| [status.md](status.md) | What is implemented and what the known gaps are. |
| [deviations.md](deviations.md) | Where the implementation departs from the specification, and what was observed that forced it. Recorded per section 0.2. |
| [open-questions.md](open-questions.md) | What the specification does not settle and which affects output. Recorded rather than guessed at. |
| [dependencies.md](dependencies.md) | Every third-party module, what it does, and what removing it would cost. |

The findings catalogue in `docs/findings.md` is generated from appendix A of the
specification and annotated with what the code emits, by
`go run ./tools/findingsdoc`. Adding a finding therefore means adding it to
appendix A as well; CI refuses the pull request otherwise.

## The development container

`.devcontainer/Dockerfile` installs the package managers the adapters read,
every version pinned, because the fixture corpus is generated from them and an
unpinned tool would make it unreproducible.

| Tool | Version | Where |
|---|---|---|
| Conan | 2.32.0 | `/opt/pkgtools`, on `PATH` |
| west | 1.5.0 | `/opt/pkgtools`, on `PATH` |
| vcpkg | pinned commit | `/opt/vcpkg`, `$VCPKG_ROOT` |
| CPM.cmake | 0.43.1, checksum verified | `/opt/cpm/CPM.cmake`, `$CPM_PATH` |

`VCPKG_FORCE_SYSTEM_BINARIES=1` is set so vcpkg uses the cmake and ninja the
image pins rather than downloading its own. All four resolve a dependency from
a local directory without network access, which is what keeps the corpus
reproducible.

ESP-IDF is not installed by default: it adds 2.1 GB against 131 MB for
everything else. Build the image with `--build-arg WITH_ESP_IDF=1` when the
ESP-IDF fixture has to be produced or refreshed.

## Two habits worth keeping

**Record, do not guess.** When the specification is silent or reality
contradicts it, the answer goes into `deviations.md` or `open-questions.md`
with what was actually observed. Several of the deviations here exist because
a real toolchain disagreed with the specification, and the evidence is in the
entry.

**Test against real evidence.** The fixture corpus under `testdata/fixtures`
is generated from real builds by `tools/fixtures/regen.sh`. Almost every defect
this project has found was in code that passed its unit tests and failed on the
first contact with output a compiler actually produced.
