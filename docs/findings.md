# Findings

Findings are structured diagnostics emitted by discovery and policy
evaluation. Use `--findings-json path` for machine-readable output and
`--review-report path --report-format markdown` for a review document.

Missing compile or link evidence is reported rather than inferred from source
tree presence. A policy profile can turn findings into a failing exit status;
the generated CycloneDX document remains deterministic when
`--reproducible` is selected.