# Open Questions

Questions the specification does not settle and which affect output. Recorded
per specification section 0.2 rather than being guessed at.

## Q1 — Should the corpus commit built artifacts?

The fixture corpus currently commits the linked executables, because DWARF
evidence (section 11.4) and artifact correlation by GNU build-id (section 11.7)
cannot be exercised without them. They are small (17-120 KiB) and contain no
host paths, since the corpus is built under the sentinel roots.

The alternative -- building artifacts on demand in tests -- would remove about
2 MB from the repository but makes the corpus non-hermetic and the tests
toolchain-dependent. Revisit if the corpus grows.

## Q2 — Staleness evidence from `.ninja_log`

Section 27.2 lists `.ninja_log` output hashes as the third-strongest staleness
signal. `.ninja_log` records wall-clock timestamps, so committing it would make
the corpus non-reproducible. It is currently not harvested.

Artifact identity correlation (section 27.2 signal 1) is stronger and is
available from the committed artifacts, so this may not need resolving.

## Q3 — Which compilation database entry wins for a unity or PCH object?

**Answered by construction.** The compile database is indexed by the `output`
it names, not by its `file`, and a compilation has exactly one output. So there
is no contest: the entry for a unity object is the one whose output is that
object, and the sources it stands for are recovered separately from the
generated unity file itself (section 17.1). Forced includes, which is where a
precompiled header shows up, are keyed the same way.

Strategies are tried in a fixed order and the first mapping for an object wins;
a later strategy disagreeing is reported as `OBJECT_SOURCE_MAPPING_CONFLICT`
rather than silently overwritten.

## Q4 — Should `--source-dir` relocate reads or set the identity root?

Raised by the FOSS track ([foss/gap-analysis.md](foss/gap-analysis.md)). Today
`--source-dir` assigns `cfg.Project.Root`, which is the identity root handed to
`anchors.Assemble`. Pointing it at a relocated source tree therefore re-anchors
every file and changes the document, instead of relocating a single read — and
a build directory restored in a second CI job silently produces NOASSERTION for
every licence, with no finding saying why.

The build root already separates the two: `logicalFor` / `physicalFor`
(`internal/generate/linkgraph.go:130`, `:145`) map the logical build root the
evidence records onto the directory being read. Milestone F2 proposes the
symmetric mapping for the source root, with identity unchanged.

The open part is the flag's meaning. Three options, in order of preference:

1. `--source-dir` becomes the physical root; identity comes from the File API's
   source root. Fixes the defect, changes documented behaviour for anyone who
   used it to override identity.
2. A second flag for the physical root. No behaviour change, two flags that
   differ in a way nobody will remember.
3. Leave it. Then licence detection stays unreliable in every split CI setup.

Recorded rather than decided; F2 must settle it and record the answer in
`deviations.md`.

## Q5 — Modification against the recorded upstream

The introspection allowlist of section 9.2 permits `git rev-parse HEAD`,
`git describe --tags --always --dirty`, `git status --porcelain` and
`git config --get remote.origin.url`. There is no `rev-list` and no `log`, so
"this component is N commits ahead of the tag it claims" cannot be established.

Milestone F6 therefore restricts modification evidence to a dirty tree and to
package metadata recording applied patches, and reports `unknown` otherwise.
Adding `git rev-list --count <upstream>..HEAD` to the allowlist would answer
the question properly; it needs the security argument that every allowlist
entry needs, since the argument is what keeps the list from growing by habit.

## Q6 — Per-file licence divergence inside one component

A component whose files carry different SPDX identifiers currently resolves at
component level. The attribution document would then print one licence for a
component that contains two.

Proposed for the FOSS track: emit `FOSS_PER_FILE_LICENSE_DIVERGENCE` (info) and
keep the notices document per component, until somebody with a real project
asks for per-file entries. Breaking the document out per file is a large change
to its shape and would be justified by evidence, not by symmetry.

## Q7 — Two versions of one package in a single assembly

Raised by the FOSS review of assembly mode
([foss/decisions.md](foss/decisions.md) Q10). Package anchor keys are
deliberately version-free — `pkg:conan/mbedtls`, never `mbedtls/3.5.0` — so that
`bom-ref`s stay stable across version bumps and a release-to-release diff stays
readable (§7.2, §28.4).

In assembly mode a product may contain two artifacts that use **different
versions** of the same package: a bootloader with mbedtls 2.28 and an
application with mbedtls 3.5. Both roots want the key `pkg:conan/mbedtls`, and
the registry keeps the first and skips the second, so the second package's files
lose their anchor.

This matters more for the FOSS export than for the SBOM: two versions can carry
two different licence texts, and only one of them would be retained.

Not resolved. The candidates are a version-qualified key for the second and any
further root, which costs `bom-ref` stability for that component, or a finding
that names the collision and leaves it to curation. Recorded rather than
guessed at; no fixture exercises it today.

## Q8 — Relocating package caches and other roots

Milestone F2 relocates the source root only: the logical root the evidence
records maps to where the bytes are now. Package caches keep the paths the build
wrote — Conan reads its package folder out of a file the build generated
(`internal/adapters/pkgmanager/conan.go:86`), so that path belongs to the build
machine.

The general form would be a list of prefix replacements, exactly as the compiler
does with `-ffile-prefix-map` and the debugger with `substitute-path`. It was
considered and deliberately not built ([foss/decisions.md](foss/decisions.md)
Q17): sbomb normally runs right after the build on the same machine, where every
path exists.

The case that would force it is a project compiling with `-ffile-prefix-map`,
where the evidence carries deliberately rewritten paths even on the build
machine. That would already show today as missing file hashes rather than as a
licence problem. Revisit when somebody reports it.
