### Milestone 4 — Link Evidence I: Dependency File and Trace

**Goal:** the most reliable link evidence first, so that the map parser is a fallback rather than a foundation.

**Deliverables**

* `internal/adapters/depfiles`: the Make-format depfile parser of Appendix D (shared by link depfiles and compile depfiles).
* `internal/adapters/linkers/depfile`: consumes `-Wl,--dependency-file` output, produces `link` and `archive-member` edges.
* Linker trace parser (`-Wl,-t`), producing `weak`-strength edges.
* Artifact correlation (§11.7) for ELF via `debug/elf` build-id, size, and mtime.
* Response file expansion (§9.3).

**Tests**

* Appendix D grammar: escaped spaces, `$$`, line continuations, multiple targets, `#` comments, Windows drive colons, `C$:` Ninja form, CRLF, missing trailing newline, empty prerequisite list, 100 000-line depfile.
* Link depfile from `p02-static` yields exactly the linked archive and the extracted member, not the unused members.
* Trace parser recognizes `libfoo.a(bar.o)`.
* Correlation mismatch (artifact rebuilt after map) → `LINK_EVIDENCE_ARTIFACT_MISMATCH`.
* Response file: nested, depth limit exceeded → `RSP_DEPTH_EXCEEDED`.
* Fuzz targets for the depfile parser; corpus seeded from all Milestone 0 depfiles.

**Acceptance**

```
go test ./internal/adapters/depfiles/... ./internal/adapters/linkers/depfile/... -race  # 0
go test -run Fuzz -fuzz FuzzDepfile -fuzztime 60s ./internal/adapters/depfiles/         # 0
sbomb evidence --build-dir testdata/fixtures/clang-17/p02-static/build \
  --adapters cmakeapi,linkdepfile --format json > /tmp/l.json && \
  cmp /tmp/l.json testdata/golden/clang-17-p02-linkdep-evidence.json                    # 0
```

**Definition of Done:** for a toolchain that supports `--dependency-file`, the tool produces a complete, deterministic list of linked units with `linked` strength and `high` confidence, and unused archive members are provably absent.

---
