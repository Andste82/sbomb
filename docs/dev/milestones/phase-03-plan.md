# Phase 3 — CycloneDX binding and validation

**Status: complete.** Commit `c7c76ad`.

## Why here

The document had no `metadata.component` and a `dependencies` array without a
single `dependsOn`. For a consumer that is not a dependency graph, it is a list.

## Work

* **Document shape §28**: root product in `metadata.component` and only there,
  artifact and grouping components, flat `components[]`.
* **The `bom-ref` scheme of §28.4** with the normative `slug()` function and an
  enforced uniqueness check.
* **The dependency cascade** product → artifact → component → file, with a
  closure check: every ref present exactly once, including with an empty
  `dependsOn`.
* **The BSI mandatory properties** `sbomb:cdx:executableProperty` /
  `archiveProperty` / `structuredProperty`, derived from the file class.
* **A second validation layer**: the official CycloneDX 1.6 schema embedded via
  `go:embed` plus a pure-Go validator, and atomic writing -- temp file,
  validate, rename.
* **The missing subcommands** `validate` and `evidence`, both already advertised
  in the README.

## Result

The schema layer proved itself on its first run: it caught hashes recorded under
the algorithm name `sha256`, which CycloneDX does not know. The semantic layer
got the test it was missing -- its closure check compared the component
references with themselves and therefore proved nothing.

`vendor/` is committed, the build needs no network (§37.3), and
`docs/dev/dependencies.md` justifies every dependency.
