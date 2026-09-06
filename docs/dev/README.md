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
```

## What is here

| Document | Contents |
|---|---|
| [sbomb-spec-v3.1.md](sbomb-spec-v3.1.md) | The normative specification. Section numbers referenced throughout the code and the commit messages point here. |
| [milestones/](milestones/) | The milestone plan extracted from section 41, plus the per-phase plans that record what was built and why. |
| [status.md](status.md) | What is implemented and what the known gaps are. |
| [deviations.md](deviations.md) | Where the implementation departs from the specification, and what was observed that forced it. Recorded per section 0.2. |
| [open-questions.md](open-questions.md) | What the specification does not settle and which affects output. Recorded rather than guessed at. |
| [dependencies.md](dependencies.md) | Every third-party module, what it does, and what removing it would cost. |

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
