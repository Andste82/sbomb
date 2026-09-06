### Milestone 13 — Policy, Findings, Waivers, Report, Explain

**Goal:** predictable CI behaviour and explainability.

**Deliverables**

* `internal/policy`: evaluation of all §33.1 options, profiles, severity overrides, exit-code precedence (§32.4).
* Waiver matching, expiry, `WAIVER_EXPIRED`, `WAIVER_UNUSED`.
* `--findings-json` writer.
* `internal/report`: the §34 review report, deterministic, text and markdown.
* `sbomb explain` (§32.3) in text and JSON.

**Tests**

* Strict vs lenient on the same fixture yields exit 3 vs 0 with identical SBOMs (policy never changes the SBOM, §33.3).
* Each `failOn*` option independently flips the exit code when its finding is present.
* A waiver suppresses exactly its finding and nothing else; expired waiver does not suppress and adds `WAIVER_EXPIRED`; unused waiver adds `WAIVER_UNUSED`.
* Exit-code precedence: a run with both a discovery error and a policy failure exits 2.
* `explain` output for a header in `p02-static` matches the golden chain rendering exactly.
* Report is byte-identical across runs under `--reproducible`.

**Acceptance**

```
go test ./internal/policy/... ./internal/report/... -race                            # 0
sbomb generate --build-dir testdata/fixtures/gcc-13/p02-static/build \
  --config testdata/config/p02-full.json --policy strict --output /tmp/s.cdx.json \
  --findings-json /tmp/f.json --review-report /tmp/r.txt --reproducible             # 3
sbomb generate ... --policy lenient ... --output /tmp/l.cdx.json --reproducible  # 0
cmp /tmp/s.cdx.json /tmp/l.cdx.json                                                 # 0  (SBOM identical)
cmp /tmp/r.txt testdata/golden/gcc-13-p02-report.txt                                # 0
sbomb explain --build-dir testdata/fixtures/gcc-13/p02-static/build \
  --file project:src/crypto.c > /tmp/x.txt && cmp /tmp/x.txt testdata/golden/explain-crypto.txt  # 0
```

**Definition of Done:** CI receives predictable pass/fail behaviour, every finding is machine-readable and waivable, and every included file can be explained.

---
