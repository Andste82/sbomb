### Milestone 1 — CLI, Configuration, Path Model, Empty SBOM

**Goal:** the skeleton that everything else plugs into, plus a valid, deterministic, empty CycloneDX document.

**Deliverables**

* `cmd/sbomb` with subcommands `generate`, `version`, `schema` (others stubbed returning exit 1 with "not implemented in milestone N").
* `internal/config`: loading, JSON Schema (Appendix G) embedded and used for structural validation of the config, defaults, policy profile resolution, precedence (§32.2), waiver file loading.
* `internal/pathmodel`: anchor registry, §7.3 resolution algorithm, canonical strings, `slug()`, redaction.
* `internal/domain`: all types of §38 (no logic beyond `Canonical()`, `Confidence.Float()`, `Downgrade()`).
* `internal/cyclonedx`: model structs with explicit field ordering, writer, structural validator, `serialNumber` per §28.9, embedded official schemas.
* `internal/findings`: finding type, catalogue constants (Appendix A), JSON writer, waiver matching.

**Tests**

* Config: valid file; unknown key rejected; invalid enum rejected; missing required field rejected; CLI-over-config precedence; profile `strict` overrides defaults; waiver expiry parsing.
* Pathmodel: longest-anchor-wins; segment-boundary matching (`/a/bc` must not match anchor `/a/b`); Windows drive normalization; backslash conversion; `..` lexical resolution; unanchored fallback; redaction determinism; `slug()` truncation with hash suffix.
* CycloneDX: empty document validates structurally; `--reproducible` twice yields identical bytes including `serialNumber`; non-reproducible runs differ only in `serialNumber` and `timestamp`; key order matches the golden file.
* Exit codes: unknown flag → 1; missing `--build-dir` → 1; `--output` with two artifacts → 1.

**Acceptance**

```
sbomb version                                                        # 0
sbomb generate --build-dir testdata/empty --output /tmp/e.cdx.json \
                  --reproducible                                        # 0
sbomb generate --build-dir testdata/empty --output /tmp/e2.cdx.json \
                  --reproducible && cmp /tmp/e.cdx.json /tmp/e2.cdx.json  # 0
sbomb generate --config testdata/config/invalid-enum.json ...        # 1
```

`/tmp/e.cdx.json` MUST equal `testdata/golden/empty-1.6.cdx.json` byte for byte.

**Definition of Done:** a valid, empty, byte-reproducible CycloneDX 1.6 document; every configuration option of §33.1 parses and resolves; anchor resolution is correct under both the POSIX and the Windows path flavor.

---
