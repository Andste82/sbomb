### Milestone 3 — CMake File API Adapter

**Goal:** the highest-value structured metadata source, before any text parsing.

**Deliverables**

* `internal/adapters/cmakeapi`: reply-directory discovery, `codemodel-v2`, `cache-v2`, `cmakeFiles-v1`, `toolchains-v1` parsing; multi-config selection (§10.3); artifact discovery (§5.3, §5.4); anchor registration for toolchain and sysroot; generated-source flags; custom-command inputs/byproducts.
* `internal/buildctx`: build-directory probing, generator detection, compiler/linker identification, selected configuration.
* Query writing when `--allow-introspection` **and** `--allow-cmake-regenerate` are set; otherwise degrade with `CMAKE_FILE_API_UNAVAILABLE`.

**Tests**

* Parse the reply directories captured in Milestone 0 for all five projects and both Ninja toolchains.
* Multi-config reply with `Debug`/`Release`: `--config-name` selects; ambiguity without it → `AMBIGUOUS_BUILD_CONFIG`, exit 1.
* Automatic artifact discovery finds exactly the executable in `p01-hello`; test/example targets are skipped per §5.4.
* `isGenerated` sources in `p04-generated` are recognized.
* Missing reply directory degrades with the correct finding and does not crash.
* Malformed JSON reply produces a finding, not a panic (fuzz seed corpus).

**Acceptance**

```
go test ./internal/adapters/cmakeapi/... ./internal/buildctx/... -race    # 0
sbomb evidence --build-dir testdata/fixtures/gcc-13/p01-hello/build \
  --adapters cmakeapi --format json > /tmp/fa.json                        # 0
cmp /tmp/fa.json testdata/golden/gcc-13-p01-cmakeapi-evidence.json        # 0
```

**Definition of Done:** target, source, artifact, configuration, and toolchain metadata are available to later milestones without parsing any generator-specific text file.

---
