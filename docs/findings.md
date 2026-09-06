# Findings

Findings are structured diagnostics emitted by discovery and policy
evaluation. Use `--findings-json <path>` for machine-readable output and
`--review-report <path>` for the human-readable review document, which follows
the nine sections of the specification.

The JSON field names are normative: `id`, `severity`, `subject`, `message`,
`detail`, `evidence`, `remediation`, `waived`, `waiverReason`. Findings are
sorted by `(id, subject.kind, subject.ref, message)` so that two runs of the
same build produce identical output.

## What turns a finding into a failure

A policy profile decides which findings fail the run. The precedence is:

1. a CLI flag, for example `--fail-on-unknown-version` or
   `--fail-on-missing-hash=false`
2. the policy profile chosen with `--policy` or `policy.profile`
3. the `policy` block of the configuration file
4. the built-in default

`--profile-overlay host-linux` adds the scope settings a hosted Linux target
needs; an overlay never changes a gate.

Scope options -- `--include-*`, `--system-libraries`,
`--include-toolchain-runtime`, `--pch-headers`,
`--section-garbage-collection` -- are discovery settings rather than verdicts.
They are applied before the document is written, and every exclusion they cause
is counted and reported. No other policy setting may remove anything from the
SBOM.

## Header evidence

`--header-evidence` chooses which source decides the header set:

| Value | Behaviour |
|---|---|
| `dwarf-preferred` (default) | The DWARF line-table file table decides for every compilation unit that names at least one header. Units it does not cover fall back to dependency files and report `HEADER_EVIDENCE_FALLBACK`. |
| `union` | Both sets are kept. The largest SBOM, and the most conservative. |
| `depfiles` | Dependency files only. For builds that ship stripped artifacts. |

Under `dwarf-preferred` a header the dependency file names but no compilation
unit does is excluded. It is never dropped silently: the review report counts
it per component under `== Header narrowing ==`, and `--report-chains all`
names every one of them.

## Waivers

A waiver file suppresses a finding deterministically. A waived finding still
appears with `waived: true`; an expired waiver does not suppress and produces
`WAIVER_EXPIRED`; a waiver that matches nothing produces `WAIVER_UNUSED`, so
that stale waivers surface in review rather than accumulating.

Missing compile or link evidence is reported rather than inferred from source
tree presence. The generated document stays byte-reproducible when
`--reproducible` is selected.
