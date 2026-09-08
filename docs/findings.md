# Findings

Findings are structured diagnostics emitted by discovery and policy
evaluation. Use `--findings-json <path>` for machine-readable output and
`--review-report <path>` for the human-readable review document, which follows
the nine sections of the specification.

The JSON field names are normative: `id`, `severity`, `subject`, `message`,
`detail`, `evidence`, `remediation`, `waived`, `waiverReason`. Findings are
sorted by `(id, subject.kind, subject.ref, message)` so that two runs of the
same build produce identical output.

## Catalogue

<!-- BEGIN GENERATED CATALOGUE -->

This build emits 41 of the 53 identifiers below. The rest are specified and
reserved: they describe evidence this version does not yet read, and a run will
never report them. They are listed and marked so that the table is the whole
catalogue rather than a snapshot of one version.

| ID | Severity | Gate | Meaning | Status |
|---|---|---|---|---|
| `AMBIGUOUS_ADAPTER_SELECTION` | error | always (exit 1) | Two incompatible adapters both detected | reserved |
| `AMBIGUOUS_BUILD_CONFIG` | error | always (exit 1) | Multi-config build without `--config-name` | reserved |
| `AMBIGUOUS_FINAL_DELIVERABLE` | error | always (exit 1) | Discovery found several candidates | emitted |
| `ARCHIVE_MEMBERS_UNRESOLVED` | warning | — | Archive used, member-level detail unavailable | emitted |
| `CMAKE_FILE_API_UNAVAILABLE` | warning | — | No reply directory and regeneration not permitted | emitted |
| `COMPONENT_ROOT_UNRESOLVED` | info | — | Component root fell back to the common directory of the used files | emitted |
| `CONFIG_DEPRECATED_OPTION` | warning | — | Deprecated configuration key used | reserved |
| `CRA_FIELD_INCOMPLETE` | error | `cra` profile | Aggregate: one or more §1.5(1) fields missing on some component | reserved |
| `DEBUG_INFO_UNAVAILABLE` | info | — | Artifact stripped or no DWARF | emitted |
| `DYNAMIC_DEPENDENCIES_IGNORED` | info | — | Artifact has `DT_NEEDED`/imports while `systemLibraries=exclude` | emitted |
| `HEADER_EVIDENCE_FALLBACK` | info | — | A CU had no DWARF coverage; depfile used instead | emitted |
| `INPUT_LIMIT_EXCEEDED` | warning | — | A parser limit of §30 was reached | reserved |
| `INTERNAL_INVARIANT_VIOLATION` | error | always (exit 70) | A graph invariant of §8.8 or a `bom-ref` uniqueness assertion of §28.4 failed | emitted |
| `LICENSE_CONFLICT` | warning | `failOnReviewRequired` | Conflicting license evidence | emitted |
| `LINKED_OBJECT_SOURCE_UNRESOLVED` | warning | `failOnMissingSourceForLinkedObject` | Object could not be mapped to a source | emitted |
| `LINK_EVIDENCE_ARTIFACT_MISMATCH` | error | `failOnStaleBuildArtifacts` | Evidence does not correspond to the artifact | reserved |
| `LINK_EVIDENCE_UNCORRELATED` | info | — | Correlation not possible for this format | reserved |
| `LTO_ATTRIBUTION_DEGRADED` | warning | `failOnWeakEvidence` | LTO present and no DWARF fallback available | emitted |
| `MALFORMED_BINARY` | info | — | An artifact could not be parsed as ELF or PE, so no debug-info evidence was read (§11.4) | emitted |
| `MALFORMED_LINK_EVIDENCE` | warning | — | Link evidence truncated or unparsable past a point | emitted |
| `MISSING_ARTIFACT` | error | always (exit 2) | Configured artifact does not exist | emitted |
| `MISSING_COMPILE_EVIDENCE` | warning | — | No compile database was found, so object-to-source mapping loses a strategy (§14.1) | emitted |
| `MISSING_COMPONENT_HASH` | warning | `failOnMissingComponentHash` | Component has no hash for its deployable form (CRA/BSI field) | emitted |
| `MISSING_FILE_HASH` | warning | `failOnMissingHash` | File unavailable or unreadable | emitted |
| `MISSING_FINAL_DELIVERABLE` | error | always (exit 1) | No artifact configured or discovered | emitted |
| `MISSING_GENERATOR_INPUT_EVIDENCE` | warning | — | Generated file used, generator inputs unknown | emitted |
| `MISSING_HEADER_DEPENDENCY_EVIDENCE` | warning | `failOnMissingHeaderEvidence` | Used TU has no dependency evidence | emitted |
| `MISSING_LINK_EVIDENCE` | error | `allowMissingLinkEvidence` | No link evidence source succeeded | emitted |
| `MISSING_PACKAGE_EVIDENCE` | warning | — | Package/image artifact without a manifest | emitted |
| `MISSING_SUPPLIER` | warning | `failOnMissingSupplier` | Component has no supplier/creator (CRA/BSI field) | emitted |
| `NINJA_DEPS_UNAVAILABLE` | info | — | `ninja -t deps` not permitted or failed | reserved |
| `OBJECT_SOURCE_MAPPING_CONFLICT` | info | — | Two strategies disagreed; higher priority used | reserved |
| `PCH_HEADERS_EXCLUDED` | info | — | Headers removed by `pchHeaders=exclude` | emitted |
| `PREBUILT_LIBRARY_UNMAPPED` | warning | `prebuiltLibrariesRequireMapping` | Prebuilt library has no component mapping | emitted |
| `REPRODUCIBLE_MODE_OMITS_TIMESTAMP` | info | — | `--reproducible` output lacks `metadata.timestamp`; not a CRA deliverable | emitted |
| `RSP_DEPTH_EXCEEDED` | warning | — | Response file recursion limit hit | reserved |
| `SECTION_GC_EXCLUDED` | info | — | Objects removed by `sectionGarbageCollection=exclude` | emitted |
| `SECTION_GC_INFO_UNAVAILABLE` | info | — | `sectionGarbageCollection` requested but the evidence source does not report discarded sections | emitted |
| `STALE_BUILD_EVIDENCE` | error | `failOnStaleBuildArtifacts` | Timestamps or hashes indicate a stale build | emitted |
| `STALE_CMAKE_CONFIGURATION` | warning | `failOnStaleBuildArtifacts` | CMake inputs newer than the File API reply | reserved |
| `TOOLCHAIN_LAYOUT_UNKNOWN` | warning | — | Implicit include dirs unknown; heuristic classification | emitted |
| `UNANCHORED_FILE` | warning | `failOnUnanchoredFile` | File matched no anchor | emitted |
| `UNITY_SOURCE_UNRESOLVED` | warning | `failOnMissingSourceForLinkedObject` | Unity TU constituents not recoverable | emitted |
| `UNKNOWN_COMPONENT` | warning | `failOnUnknownComponent` | File could not be mapped to a component | emitted |
| `UNKNOWN_HEADER_CLASS` | info | `failOnReviewRequired` | Header could not be classified | emitted |
| `UNKNOWN_LICENSE` | warning | `failOnUnknownLicense` | Component license is NOASSERTION | emitted |
| `UNKNOWN_PURL` | info | — | No package type assertable | emitted |
| `UNKNOWN_VERSION` | warning | `failOnUnknownVersion` | Component version could not be resolved | emitted |
| `VCS_DIRTY` | info | `failOnReviewRequired` | Component working tree is dirty | emitted |
| `WAIVER_EXPIRED` | warning | — | A waiver's `expires` date has passed | emitted |
| `WAIVER_UNUSED` | info | — | A waiver matched no finding | emitted |
| `WEAK_EVIDENCE` | warning | `failOnWeakEvidence` | The only evidence for a file is a textual fallback source (§8.4) | emitted |
| `WHOLE_ARCHIVE_MEMBERS_ENUMERATED` | info | — | Members read from the archive, not reported by the linker | reserved |

<!-- END GENERATED CATALOGUE -->

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

## Introspection

sbomb never runs a build command. It may run a fixed allowlist of introspection
commands, and only when `--allow-introspection` is passed or a
`build.introspection.*` flag is set; the default is off. `--allow-introspection`
enables every group, `--allow-introspection=git,ninja` only the named ones.

Every permitted invocation has an exact argument shape, runs without a shell,
inherits nothing but `PATH`, times out after 30 seconds and has its output
bounded at 64 MiB. A path passed to one of them must exist and lie inside a
registered anchor. `sbomb generate -v --allow-introspection` prints the
allowlist, and every executed command is logged.

When introspection is off and an adapter needs a command, the adapter degrades
and emits an informational finding naming the evidence it could not obtain. It
never guesses in place of the answer.

## Waivers

A waiver file suppresses a finding deterministically. A waived finding still
appears with `waived: true`; an expired waiver does not suppress and produces
`WAIVER_EXPIRED`; a waiver that matches nothing produces `WAIVER_UNUSED`, so
that stale waivers surface in review rather than accumulating.

Missing compile or link evidence is reported rather than inferred from source
tree presence. The generated document stays byte-reproducible when
`--reproducible` is selected.
