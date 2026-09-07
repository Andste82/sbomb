# sbomb — CMake Used-Files CycloneDX SBOM Specification v3.1

**Status:** Implementation-ready normative specification.
**Tool name:** `sbomb`. Go module path: `github.com/<org>/sbomb`.
**Supersedes:** v2.0 and v3.0 (v3.1 records the scope decisions of Appendix J).
**Target audience:** an autonomous coding agent (or a human developer) implementing the tool `sbomb` from scratch, without further architectural decisions.

---

## 0. How to Use This Document

### 0.1 Reader Contract

This document is written so that an implementing agent can work top-to-bottom without asking design questions.

* **Part I (§1–§34)** is the normative product specification. Every behaviour that affects output is fixed here.
* **Part II (§35–§40)** is the implementation blueprint: package layout, Go type signatures, internal file formats.
* **Part III (§41)** is the milestone plan. Every milestone is independently buildable, independently testable, and has literal acceptance commands with expected exit codes.
* **Part IV (Appendices A–H)** contains the machine-checkable catalogues: findings, properties, configuration JSON Schema, depfile grammar, fixture layout.

### 0.2 Rules for the Implementing Agent

1. **Never** invent behaviour that is not specified. If this document is silent on something that affects output, stop and record it as an open question in `docs/dev/open-questions.md`; do not guess.
2. **Never** skip a milestone's tests. A milestone is complete only when its acceptance commands return the specified exit codes on a clean checkout.
3. **Never** implement a later milestone's behaviour early. Each milestone must be a shippable state of the repository.
4. Every milestone MUST end with `go build ./... && go vet ./... && go test ./...` succeeding.
5. Any deviation from this specification MUST be recorded in `docs/dev/deviations.md` with a rationale.

### 0.3 Terminology Shortcuts

Throughout, `SUT` means the tool under construction (`sbomb`). "Agent" means the implementing coding agent. "Consumer" means downstream software reading the produced SBOM.

---

# PART I — NORMATIVE SPECIFICATION

## 1. Purpose and Scope

### 1.1 Goal

The tool generates a CycloneDX JSON SBOM containing only files and software components that can be connected to one or more configured **final deliverables** through concrete build, link, dependency, generation, packaging, or image evidence.

It answers exactly one question:

> Which source files, headers, generated files, libraries, binaries, assets, and software components demonstrably contributed to this concrete build artifact?

### 1.2 Anti-Goal

The tool MUST NOT use broad source-tree scanning as the primary mechanism for determining SBOM scope. Repository presence is not evidence.

The following MUST NOT be included solely because they exist on disk: unused source files, unused headers, unused libraries, complete SDK directories, complete Git submodules, examples, tests, documentation, unused CMake targets, unused package-manager dependencies, unused toolchain files, arbitrary license files.

### 1.3 Supported Environments

| Dimension | Supported |
|---|---|
| Host OS the tool runs on | Linux x86_64 and Linux ARM64 are supported and CI-verified. A Windows x86_64 binary is cross-compiled and released but is **not** exercised by CI; Windows path semantics are covered by path-flavor unit tests (§41 M14). macOS is out of scope. |
| CMake generators | Ninja, Ninja Multi-Config, Unix Makefiles, NMake Makefiles. Visual Studio generators are **parked** (§41 M18). |
| Compilers | GCC and Clang. MSVC is **parked** (M18). IAR and other vendor compilers are **parked** (M21). |
| Linkers | GNU ld, GNU gold, LLVM lld. MSVC `link.exe` and IAR ILINK are parked. |
| Output | CycloneDX JSON 1.6. The writer layer is format-agnostic (§36.1) so SPDX or CycloneDX 1.7 can be added without touching discovery. |
| Binary formats for inspection | ELF and PE/COFF. Mach-O is out of scope. |
| Distribution | A single statically linked executable per platform (`CGO_ENABLED=0`). |

### 1.4 Non-Goals

The tool does not: discover every dependency in a source tree; perform vulnerability scanning; determine legal license compatibility; replace a compliance review; reconstruct builds without build evidence; infer dependencies from directory names; guarantee exact source attribution for all optimized LTO binaries; act as a general CMake dependency visualizer.

### 1.5 Target Compliance Regime: EU Cyber Resilience Act

The intended compliance target is the EU Cyber Resilience Act (Regulation (EU) 2024/2847). The relevant obligation is Annex I Part II point 1 together with Article 13: manufacturers must draw up a software bill of materials in a commonly used, machine-readable format covering at the very least the top-level dependencies. Reporting obligations apply from 11 September 2026, the main obligations from 11 December 2027. The SBOM need not be published, but must be part of the technical documentation and available to market surveillance authorities on request.

The CRA text itself does not enumerate data fields. The concrete field-level target for this tool is therefore **BSI TR-03183 Part 2 (SBOM)**, currently version 2.1.0 of 2025-08-20, which requires CycloneDX 1.6 as its minimum version — this is why §28.1 fixes 1.6.

Consequences that are normative here:

1. Every component in the SBOM MUST carry, or explicitly account for the absence of: component name, version, creator/supplier, at least one cryptographic hash, license information, file name, and dependency relationships.
2. `metadata.timestamp` and `metadata.tools` (the SBOM creator) MUST be present in normal mode. In `--reproducible` mode the timestamp is omitted, and the run MUST emit `REPRODUCIBLE_MODE_OMITS_TIMESTAMP` (info) so nobody ships a reproducible-mode artifact as the deliverable SBOM by accident.
3. TR-03183-2 requires the *executable*, *archive*, and *structured* properties per component. The tool MUST emit `sbomb:cdx:executableProperty`, `sbomb:cdx:archiveProperty`, and `sbomb:cdx:structuredProperty`, derived from the file class: an ELF/PE artifact or shared library is `executable`; an `ar` or zip container is `archive`; a source, header, or other text file is `structured`.
4. A `cra` policy profile (§33.2) fails the run when any field in (1) is missing and unwaived.
5. The tool asserts nothing about legal compliance. It produces data; the manufacturer's assessment stays out of scope (§1.4). The exact BSI field list MUST be re-verified against the then-current TR revision before any release claims the `cra` profile is complete. This is a release-checklist item in `docs/compliance.md`.

---

## 2. Normative Language

The key words MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT, SHOULD, SHOULD NOT, MAY, and OPTIONAL are to be interpreted as described in RFC 2119 as updated by RFC 8174, when and only when they appear in all capitals.

---

## 3. Glossary (Normative Definitions)

These terms are used with exactly the meanings below. Previously ambiguous terms are marked **[was ambiguous in v2.0]**.

| Term | Definition |
|---|---|
| **Project root** **[was ambiguous]** | The directory resolved, in this priority order: (1) the value of `project.root` in the configuration; (2) the directory containing the configuration file; (3) the value of `--source-dir`. It MUST be an existing directory. It is NOT automatically the Git root. |
| **Build root** | The directory given by `--build-dir`, or `build.dir` in configuration. |
| **Final deliverable** | An artifact explicitly configured (or deterministically discovered per §5.3) that is intended to be flashed, installed, delivered, packaged, executed, deployed, or consumed as a product output. |
| **Product** | The unit the root CycloneDX component represents. Either one final deliverable (single mode) or an assembly of several (assembly mode). |
| **Anchor** | A named, portable path root (§7). Every file identity is `(anchor, relative path)`. |
| **Used file** | A file for which at least one valid evidence chain to a final deliverable exists (§4). |
| **Evidence node** | A node in the evidence graph representing an artifact, unit, or file. |
| **Evidence edge** | A directed relationship between two evidence nodes, carrying type, strength, confidence, source, and adapter. |
| **Evidence strength** **[was conflated]** | The *kind* of relationship (§8.4). Fixed enum. |
| **Confidence** **[was conflated]** | The *trust in a specific evidence instance* (§8.5). Fixed enum, mapped to a CycloneDX float. |
| **Grouping component** | A CycloneDX component of type `library`, `framework`, or `application` that groups file components into a meaningful software unit. |
| **File component** | A CycloneDX component of type `file` representing one used file. |
| **Transient build artifact** | An object file, response file, dependency file, unity aggregation source, PCH object, or project-generated static archive that exists only inside the build tree and whose inputs are fully represented. |
| **Intermediate generated file** **[was ambiguous]** | A transient build artifact. A generated *source* or *header* that appears in compile or dependency evidence is NOT an intermediate generated file. |
| **Finding** | A structured, machine-readable diagnostic (§26). |
| **Discovery** | Everything that produces the evidence graph, inventory, components, versions, and licenses. |
| **Policy** | The pass/fail evaluation applied after discovery. |

---

## 4. Core Principle: Evidence-Based Scope

### 4.1 Evidence Chains

A file is included in the SBOM only if a valid evidence chain connects it to at least one final deliverable. Valid chain shapes:

```
final deliverable -> link input -> archive member -> object -> translation unit -> source
final deliverable -> link input -> object -> translation unit -> header
final deliverable -> package/image input -> generated artifact -> generator input
final deliverable -> embedded asset
```

Existence of a file is never sufficient evidence.

### 4.2 What Counts as Used

A file is used if at least one of the following holds:

1. it is directly passed to the final linker;
2. it is an archive member extracted by the linker;
3. it is an object file used by the final linker;
4. it is a source file producing an object used by the final linker;
5. it is a header appearing in dependency information for a translation unit whose object is used by the final linker;
6. it is a generated source or header used by a linked translation unit;
7. it is an input to a generator producing an artifact used by the final deliverable;
8. it is explicitly consumed by a package/image/firmware assembly process;
9. it is an embedded asset included in the final deliverable.

### 4.3 What Is Not Sufficient

Being compiled; being part of a CMake target; appearing in `compile_commands.json`; being present in an include directory, Git repository, Git submodule, SDK, or package-manager cache; being referenced by a CMake file without final-artifact evidence.

### 4.4 The Compile/Include Asymmetry (Normative Clarification)

**[Resolves a v2.0 contradiction.]** §4.3 rejects "being compiled" as usage, while §4.2(5) accepts compile-time header inclusion as usage. This asymmetry is deliberate:

* A compiled source is rejected because its object may never reach the linker. The evidence chain is *incomplete*.
* An included header is accepted because the chain is *complete* — the including translation unit is already proven to reach the linker. The remaining imprecision is only over-reporting caused by preprocessor conditionals (`#if 0`, feature guards) and declaration-only inclusion.

The tool deliberately accepts this over-reporting: it is conservative in the safe direction for a bill of materials. Implementations MUST NOT attempt to prune headers by preprocessor analysis.

**Primary header evidence source.** DWARF line-table evidence (§11.4) is the *primary* header source, because it reflects what actually reached the emitted translation unit rather than what the preprocessor opened. Depfiles are the fallback. This is controlled by `policy.headerEvidence`:

| Value | Behaviour |
|---|---|
| `dwarf-preferred` (default) | Use the DWARF header set when DWARF is available for the CU. Fall back to depfiles per CU when it is not, emitting `HEADER_EVIDENCE_FALLBACK` (info). |
| `union` | Union of DWARF and depfile sets. Most conservative, largest SBOM. |
| `depfiles` | Depfiles only; DWARF header evidence ignored. For builds that ship stripped artifacts. |

Under `dwarf-preferred`, headers seen only in depfiles for a DWARF-covered CU are **not** included, but they MUST be counted and reported: the review report shows `headers excluded by DWARF narrowing: N` per component, and `--report-chains all` lists them. This makes the narrowing auditable rather than silent.

### 4.5 Dead Code Elimination (New)

Linkers may discard input sections (`--gc-sections`, `/OPT:REF`) or fold identical ones (ICF, COMDAT folding). A linked object may therefore contribute nothing to the final image.

The tool MUST detect discarded sections when the evidence source reports them (GNU ld `--print-gc-sections` output, map file "Discarded input sections" block, MSVC `/VERBOSE` output).

Behaviour is controlled by `policy.sectionGarbageCollection`:

| Value | Behaviour |
|---|---|
| `ignore` (default) | Discarded sections are not tracked. Objects remain used. |
| `annotate` | Objects whose sections were entirely discarded receive property `sbomb:evidence:link:fullyDiscarded=true` and confidence is downgraded one level. They remain in the SBOM. |
| `exclude` | Objects whose sections were entirely discarded are removed from the used set, and a `SECTION_GC_EXCLUDED` finding is emitted per removal. |

An object is "fully discarded" only when the evidence source enumerates all of its contributed sections and all are listed as discarded. Partial information MUST NOT trigger exclusion.

### 4.6 Whole-Archive and Group Semantics

* `--whole-archive` / `/WHOLEARCHIVE` / `-force_load`: every member of the archive is used. When link evidence does not enumerate members, the tool MUST enumerate members by reading the archive file itself and mark each as `linked` with confidence `medium`, emitting `WHOLE_ARCHIVE_MEMBERS_ENUMERATED`.
* `--start-group` / `--end-group`: no special handling; extracted members are reported by the linker as usual.
* Thin archives (`ar T`): the archive contains references, not member data. The referenced object paths MUST be resolved relative to the archive's directory and treated as directly linked objects.

---

## 5. Final Deliverables

### 5.1 Definition and Configuration

The tool MUST operate on explicitly configured or deterministically discovered final deliverables.

```json
{
  "artifacts": [
    { "path": "build/firmware.elf", "role": "application" }
  ]
}
```

`role` is one of: `application`, `bootloader`, `library`, `filesystem`, `image`, `package`, `data`, `other`.

### 5.2 Prohibited Heuristics

The tool MUST NOT select a final artifact because it is the newest, largest, or only binary in the build directory.

### 5.3 Permitted Automatic Discovery

Automatic discovery is permitted **only** from unambiguous structured build-system metadata, and only when `artifacts` is absent or empty:

1. CMake File API `codemodel-v2` targets of type `EXECUTABLE` that have at least one `install` rule → role `application`.
2. If no such target exists: CMake File API targets of type `EXECUTABLE` that are not marked as test or example (per §5.4) → role `application`.
3. `install_manifest.txt` entries that are executables or shared libraries → role `application` / `library`.

If discovery yields zero artifacts, the tool MUST fail with `MISSING_FINAL_DELIVERABLE` and exit code 1.
If discovery yields more than one artifact and `mode` is not configured, the tool MUST emit `AMBIGUOUS_FINAL_DELIVERABLE` and exit code 1, listing candidates.

### 5.4 Test/Example Exclusion Heuristic

A discovered target is skipped when its CMake target name matches, case-insensitively, any of the configured `discovery.excludeTargetPatterns` (default: `["*test*", "*example*", "*sample*", "*benchmark*"]`), or when it is registered via `add_test()` as reported by the File API. This heuristic applies **only** to automatic discovery, never to explicitly configured artifacts.

### 5.5 Artifact Consistency

Each configured artifact MUST exist and be readable, otherwise `MISSING_ARTIFACT` (error, exit 2).

---

## 6. SBOM Modes

### 6.1 Single Artifact Mode (default)

One SBOM per configured final deliverable. The root component is the deliverable.

Output naming: if `--output` is given and there is exactly one artifact, that path is used. If there is more than one artifact in single mode, `--output-dir` MUST be given, and files are written as `<output-dir>/<artifact-basename-without-extension>.cdx.json`. **[Resolves a v2.0 contradiction: `--output` alone with multiple artifacts is a usage error, exit 1.]**

### 6.2 Product Assembly Mode

Selected by `mode: "assembly"` in configuration or `--mode assembly`.

One SBOM describes all configured artifacts as one product. The root component is the product (`project.name`, `project.type`). Each artifact appears as a CycloneDX component of type matching its role, with `bom-ref` per §28.4, and is a direct dependency of the root.

### 6.3 File Sharing Across Artifacts in Assembly Mode

A file used by several artifacts appears exactly once as a file component. Its grouping component is a dependency target of every artifact that uses it. Evidence properties MUST aggregate; `sbomb:evidence:artifacts` lists the `bom-ref`s of all artifacts whose chains reach the file, sorted.

---

## 7. Path and Identity Model (Anchors)

**[New in v3.0. This section resolves the v2.0 contradictions between project-relative canonical paths, out-of-tree dependencies, deterministic `bom-ref`s, and CI/local equivalence.]**

### 7.1 Motivation

A Conan cache header at `/home/alice/.conan2/p/ab12/include/aes.h`, a toolchain header at `/opt/gcc-arm-none-eabi-13.2/...`, or a sibling checkout at `../shared/` cannot be expressed as a stable project-relative path. Absolute paths are machine-specific and break reproducibility, CI/local equivalence, and privacy.

### 7.2 Anchor Definition

An **anchor** is a named, absolute directory with a stable key. Every file in the model is identified as `(anchorKey, relPath)` where `relPath` is POSIX-style, relative, and never contains `..`.

Anchor keys are of the form `<kind>` or `<kind>:<name>`:

| Anchor key | Meaning | Resolution |
|---|---|---|
| `project` | Project root (§3) | Always present. |
| `build` | Build root | Always present. |
| `sdk:<name>` | An SDK root | From configuration or SDK adapter. |
| `pkg:<type>/<name>` | A package-manager package root. **Deliberately version-free**, so that file `bom-ref`s stay stable across version bumps (§28.4) and SBOM diffs between releases remain readable. | From package-manager adapter, e.g. `pkg:conan/mbedtls`. The resolved version lives on the component (§20) and in `sbomb:component:root`, never in the anchor key. |
| `toolchain:<id>` | Compiler/toolchain installation root | From compiler probing (§24.4). |
| `sysroot:<id>` | Cross-compilation sysroot | From compile flags (`--sysroot=`). |
| `extern:<name>` | Explicitly configured external root | From `anchors` in configuration. |
| `abs` | Fallback for files under no other anchor | See §7.5. |

### 7.3 Anchor Resolution Algorithm (Normative)

For an absolute, symlink-preserved path `P`:

1. Normalize `P`: convert `\` to `/`, collapse duplicate separators, remove `.` segments, resolve `..` lexically, and on Windows uppercase the drive letter. Do **not** resolve symlinks (§7.6).
2. Among all registered anchors whose absolute directory is a path prefix of `P` **at a path-segment boundary**, choose the one with the **longest** directory. Ties are impossible because anchor directories are deduplicated at registration.
3. If a match is found, the identity is `(anchorKey, P relative to that directory)`.
4. If no match is found, apply §7.5.

Path prefix comparison is case-sensitive under the POSIX path flavor and case-insensitive under the Windows flavor (§41 M14).

### 7.4 Anchor Registration Order

Anchors are registered from these sources, and later registrations do not override earlier ones with the same directory:

1. `project`, `build` from configuration/CLI.
2. `anchors` array from configuration (explicit, highest authority for naming).
3. Package-manager adapters (§21.5).
4. SDK adapters (§25).
5. Toolchain probing (§24.4).
6. Sysroot from compile flags.

### 7.5 Unanchored Files

A file matching no anchor is assigned anchor `abs`. Its `relPath` is the absolute path with the leading `/` removed, or on Windows `C/Users/...` (drive letter as first segment, colon removed).

Every `abs` file MUST produce an `UNANCHORED_FILE` finding (severity `warning` by default). `policy.failOnUnanchoredFile` controls whether this fails the run (default `false`).

When `--redact-unanchored-paths` is set (default off), `abs` relPaths are replaced by `redacted/<sha256(relPath)[:16]>` and the original is omitted entirely from output, including diagnostics.

### 7.6 Symlinks, Junctions, Reparse Points

The **logical build path** — the path as it appeared in build evidence — is the primary identity. The tool MUST NOT resolve symlinks, Windows junctions, or reparse points when computing identity.

The resolved target MAY be recorded as property `sbomb:file:resolvedTarget` when `--record-resolved-targets` is set; it is then also subject to anchoring and redaction.

When hashing (§23), the tool follows the link to read content, but MUST refuse to read a path whose resolved target escapes all registered anchors *and* `--allow-unanchored-reads` is not set; in that case the hash is missing and `MISSING_FILE_HASH` is emitted.

### 7.7 Canonical Path String

The canonical string form used in output and in `bom-ref`s is:

```
<anchorKey>:<relPath>
```

Examples:

```
project:src/main.cpp
project:dep/mbedtls/include/mbedtls/aes.h
build:generated/version.h
pkg:conan/mbedtls:include/mbedtls/aes.h
toolchain:gcc-arm-none-eabi-13.2.1:arm-none-eabi/include/stdio.h
abs:opt/vendor/blob.a
```

Backslashes MUST NOT appear. Absolute Windows drive paths MUST NOT appear.

### 7.8 Human-Facing Paths

Reports and `explain` output SHOULD additionally show the absolute path for anchors `project` and `build` when `--absolute-paths` is set. Absolute paths MUST NOT appear in the CycloneDX document.

---

## 8. Evidence Model

### 8.1 Evidence Graph

The internal discovery model MUST be a directed evidence graph. CycloneDX output is generated from this graph and never the other way round.

Nodes have a `kind`:

```
product | artifact | package | image | archive | archive-member | object |
translation-unit | source | header | asset | generator | generator-input | toolchain-file
```

### 8.2 Evidence Edge

Each edge MUST record:

| Field | Required | Notes |
|---|---|---|
| `from` | yes | Node ID (consumer / closer to deliverable) |
| `to` | yes | Node ID (input / closer to source) |
| `type` | yes | §8.3 |
| `strength` | yes | §8.4 |
| `confidence` | yes | §8.5 |
| `source` | yes | Evidence source identifier, e.g. `ninja:build.ninja`, `ld:firmware.map`, `dwarf:firmware.elf` |
| `adapter` | yes | Adapter ID that produced the edge |
| `raw` | no | Verbatim excerpt (line, offset) retained only when `--keep-raw-evidence` is set |
| `attributes` | no | Free key/value map, e.g. `archive=libfoo.a` |

Edges are deduplicated on `(from, to, type, source, adapter)`. When duplicates differ in confidence, the **highest** confidence is kept and the lower one is retained under `attributes.supersededConfidence`.

### 8.3 Evidence Types

```
link                 archive-member       compile
source-mapping       header-dependency    generated
package              image                asset
generator-input      generator-output     toolchain
install              debug-info
```

### 8.4 Evidence Strength

| Strength | Meaning |
|---|---|
| `direct` | The file is directly observed as an input to the final deliverable. |
| `linked` | Observed as a linker input or extracted archive member. |
| `derived` | Deterministically derived from structured build metadata. |
| `packaged` | Explicitly consumed by a packaging/image process. |
| `generated` | Comes from a generator dependency declaration. |
| `weak` | Inferred from fallback evidence such as build logs or basename matching. |

Weak evidence MUST NOT be silently treated as equivalent to direct or linked evidence. `policy.failOnWeakEvidence` exists for this purpose.

### 8.5 Confidence

**[Resolves the v2.0 conflation of strength and confidence.]**

Confidence is a four-value enum with a fixed mapping to the CycloneDX `evidence.identity.confidence` float:

| Confidence | Float | Use |
|---|---|---|
| `high` | 0.9 | Structured, authoritative, artifact-correlated evidence |
| `medium` | 0.6 | Structured but indirect, or authoritative but not artifact-correlated |
| `low` | 0.3 | Heuristic or fallback |
| `unknown` | 0.1 | Present but unverified |

### 8.6 Default Confidence Derivation

The confidence of an edge is derived from `(strength, source class)` using this table, then adjusted by §8.7.

| Strength \ Source class | structured-authoritative | structured-secondary | textual-fallback |
|---|---|---|---|
| `direct` | high | high | medium |
| `linked` | high | medium | low |
| `derived` | high | medium | low |
| `packaged` | high | medium | low |
| `generated` | medium | medium | low |
| `weak` | low | low | low |

Source classes:

* **structured-authoritative**: CMake File API, linker `--dependency-file`, DWARF, `ninja -t deps`, MSBuild `.tlog`, package manifests with explicit input lists.
* **structured-secondary**: `build.ninja` parsing, `compile_commands.json`, `.d` files, linker map files, `install_manifest.txt`.
* **textual-fallback**: build logs, linker trace text, basename matching.

### 8.7 Confidence Adjustments

Confidence is downgraded by exactly one level when any of:

* the evidence source could not be correlated to the artifact (§27.3);
* LTO is detected and the edge is an object→source mapping (§17.3);
* `policy.sectionGarbageCollection = annotate` and the object is fully discarded;
* whole-archive members were enumerated from the archive rather than reported by the linker.

Downgrades are cumulative and floor at `unknown`. Every downgrade MUST be recorded in `attributes.confidenceDowngrades` as a sorted list of reason codes.

### 8.8 Graph Invariants (Testable)

An implementation MUST maintain and be able to assert:

1. Every node except `product` has at least one incoming edge.
2. The graph is acyclic. Cycle detection MUST run before inventory; a cycle is an internal error (exit 70).
3. Every included file's node is reachable from at least one `artifact` node.
4. Node IDs are canonical path strings (§7.7) for file-like nodes, and `product:<name>` / `artifact:<canonical path>` otherwise.

---

## 9. Evidence Sources and Priority

### 9.1 Global Evidence Priority

When two sources disagree, the higher-priority source wins and the lower-priority one is retained as a corroborating edge with its own confidence.

1. Explicit configuration (curated overrides).
2. CMake File API `codemodel-v2` / `toolchains-v1`.
3. Linker `--dependency-file` output.
4. DWARF / PDB debug information read from the artifact.
5. `ninja -t deps` / `.ninja_deps`, MSBuild `.tlog`.
6. `build.ninja` / Makefile / MSBuild project parsing.
7. `compile_commands.json`.
8. `.d` dependency files.
9. Linker map files.
10. Linker trace output (`-Wl,-t`, `/VERBOSE`).
11. Build logs.

Build-log-derived evidence MUST have `weak` strength and MUST NOT override contradictory evidence from any higher-priority source.

### 9.2 Introspection Command Allowlist

**[Resolves the v2.0 contradiction between "MUST NOT execute build commands" and "invoke optional build-system introspection".]**

The tool MUST NOT execute build commands. The tool MAY execute a fixed allowlist of **introspection** commands, and only when `--allow-introspection` is passed (default: off) or the corresponding `build.introspection.*` configuration flag is true.

Allowlisted commands (exact argv shapes; no shell, no string interpolation of untrusted data):

```
cmake --version
cmake -E capabilities
cmake --file-api ...        (implemented as writing a query file, then reading the reply; no execution needed if a reply already exists)
ninja -C <build-dir> -t deps
ninja -C <build-dir> -t commands <target>
ninja -C <build-dir> -t inputs <target>
ninja --version
git -C <dir> rev-parse HEAD
git -C <dir> describe --tags --always --dirty
git -C <dir> config --get remote.origin.url
git -C <dir> status --porcelain
<compiler> --version | -dumpmachine | -print-search-dirs
dpkg -S <path>        (only when systemLibraries adapter enabled)
rpm -qf <path>        (only when systemLibraries adapter enabled)
```

Every executed command MUST be logged, MUST have a timeout (default 30 s), MUST have its output size bounded (default 64 MiB), and MUST NOT be run with a shell. Paths passed as arguments MUST be validated to exist and be within a registered anchor.

If `--allow-introspection` is off and an adapter requires a command, the adapter degrades gracefully and emits an informational finding naming the missing evidence (e.g. `NINJA_DEPS_UNAVAILABLE`).

### 9.3 Response File Expansion

Response files (`@file`, `.rsp`) referenced in compile or link command lines MUST be expanded before parsing. Expansion:

* is recursive with a depth limit of 8 (`RSP_DEPTH_EXCEEDED` on overflow);
* uses GNU-style quoting on POSIX and MSVC-style quoting on Windows toolchains, selected by the detected compiler;
* MUST bound total expanded size to 64 MiB.

---

## 10. CMake File API Adapter

**[New dedicated section. In v2.0 this was one bullet despite being the richest available source.]**

### 10.1 Query and Reply

The adapter reads `<build-dir>/.cmake/api/v1/reply/`. If no reply exists and `--allow-introspection` is set, the adapter writes a query file at `<build-dir>/.cmake/api/v1/query/client-sbomb/query.json` requesting `codemodel-v2`, `cache-v2`, `cmakeFiles-v1`, `toolchains-v1`, and re-runs `cmake -S <source-dir> -B <build-dir>` **only if** `--allow-cmake-regenerate` is additionally set. Without it, the adapter emits `CMAKE_FILE_API_UNAVAILABLE` and degrades.

### 10.2 Extracted Information

| From | Used for |
|---|---|
| `codemodel-v2` targets: `sources[]`, `sourceGroups`, `compileGroups` | Object→source candidate mapping, language, defines, include dirs |
| `targets[].artifacts[]` | Mapping target → produced binary |
| `targets[].link.commandFragments` | Link line reconstruction, library paths, `-Wl,` flags |
| `targets[].archive` | Static archive production |
| `targets[].dependencies` | Target graph (evidence hints only, never usage) |
| `targets[].install` | Automatic deliverable discovery (§5.3) |
| `sources[].isGenerated` | Classification of generated files |
| `cache-v2` | `CMAKE_BUILD_TYPE`, compiler paths, toolchain file |
| `toolchains-v1` | Compiler ID, version, implicit include dirs (drives `system-header` classification, §14.4) |
| `cmakeFiles-v1` | List of CMake input files — used only for staleness detection, never as usage evidence |

### 10.3 Multi-Config Handling

`codemodel-v2` reply contains one `configuration` per config for multi-config generators. The tool MUST select exactly one configuration:

1. `--config <name>` if given;
2. `build.config` from configuration;
3. the single configuration if only one exists;
4. otherwise `AMBIGUOUS_BUILD_CONFIG`, exit 1.

One SBOM describes exactly one configuration. The selected configuration MUST be recorded as property `sbomb:build:config` on the root component.

### 10.4 Prohibition

CMake targets are build-system constructs and are NOT automatically SBOM components. `add_library()`, `add_executable()`, `target_link_libraries()`, `target_include_directories()`, `add_subdirectory()`, and `FetchContent` do not by themselves create components or used files. File API data may only *refine* mappings for files already proven used by link/compile/dependency evidence.

---

## 11. Link Evidence

### 11.1 Purpose

Link evidence determines which compiled or prebuilt units contributed to the final binary. It is the anchor of every chain.

### 11.2 Source Preference Order

The tool MUST attempt sources in this order and merge all that succeed:

1. **Linker dependency file** (`--dependency-file=`, GNU ld ≥ 2.35 and lld). Highest fidelity: a Make-format list of every file the link consumed, including extracted archive members.
2. **DWARF / PDB debug information** read from the artifact itself (§11.4).
3. **Linker map file** (`-Map`, `/MAP`) (§11.5).
4. **Linker trace output** (`-Wl,-t`, `link /VERBOSE:LIB`) (§11.6).
5. **Reconstructed link command line** from File API / Ninja / MSBuild, with response files expanded.

If none succeed: `MISSING_LINK_EVIDENCE`, severity `error`, exit code 2 unless `policy.allowMissingLinkEvidence` is true.

### 11.3 Linker Dependency File

Parsed with the depfile grammar of Appendix D. Every listed path becomes a `linked` edge from the artifact node. Paths ending in `.a`/`.lib` with a member suffix notation (`libfoo.a(bar.o)`) are recorded as archive-member edges.

### 11.4 DWARF / PDB Adapter

Go's standard library provides `debug/elf`, `debug/pe`, `debug/macho`, and `debug/dwarf`. The adapter:

1. Enumerates DWARF compilation units. For each CU, `DW_AT_name` + `DW_AT_comp_dir` yields the translation unit source path. This is an authoritative list of TUs actually present in the binary and survives archive extraction and (mostly) LTO.
2. Reads the DWARF line-table file table per CU, yielding the set of files referenced by the line program — that is, the headers that actually contributed code or declarations to the emitted CU.
3. For PE, reads the PDB path and signature from the debug directory for identity correlation only. **PDB parsing is out of scope for this version.** On PE, DWARF evidence is therefore available only for GCC/Clang-produced binaries (mingw-w64); MSVC-produced binaries have no debug-info adapter, which is one reason MSVC support is parked (§41 M18).

DWARF header evidence is recorded with type `debug-info`. Per §4.4, the depfile header set and the DWARF header set are **unioned**; files present only in DWARF get confidence `high`, files present only in depfiles get their normal depfile confidence, and files in both get `high`.

If the artifact has been stripped, the adapter emits `DEBUG_INFO_UNAVAILABLE` (informational) and degrades.

### 11.5 Linker Map Parsers

Map formats are not standardized. The implementation MUST provide separate parsers selected by format sniffing:

| Parser | Sniff signature |
|---|---|
| `gnu-ld` | Contains a line beginning `Archive member included` or `Memory Configuration` |
| `gnu-gold` | Contains `Archive member included to satisfy reference by file` and gold-specific spacing |
| `lld` | Columns `VMA LMA Size Align Out In Symbol` |
| `msvc` | Contains `Preferred load address is` or ` Address         Publics by Value` |
| `iar` | Contains `IAR ELF Linker` or `*******************************************************************************` banner with `MODULE SUMMARY` (fixture-only) |

Each parser MUST extract, where the format permits: directly linked object files; static archives; extracted archive members; shared libraries; prebuilt objects; binary blobs; discarded input sections; linker scripts (only when `includeLinkerScripts` is true).

Parsers MUST be streaming, MUST bound line length (1 MiB) and total allocation, and MUST tolerate truncated files by emitting `MALFORMED_LINK_EVIDENCE` and returning whatever was parsed before the failure point.

### 11.6 Linker Trace

`-Wl,-t` prints each file the linker opens, one per line, with archive members as `libfoo.a(bar.o)`. Treated as `textual-fallback` source class.

### 11.7 Artifact Correlation

**[New.]** Link evidence MUST be correlated to the specific artifact where the format permits:

* ELF: compare GNU build-id (`.note.gnu.build-id`) recorded in the map/dependency file directory sidecar, if available; otherwise compare mtime and size.
* PE: compare the PDB signature+age from the debug directory against the `.pdb` referenced by the map.

If correlation is possible and fails, emit `LINK_EVIDENCE_ARTIFACT_MISMATCH` (severity `error` by default). If correlation is impossible, emit `LINK_EVIDENCE_UNCORRELATED` (informational) and apply the §8.7 confidence downgrade.

---

## 12. Static Libraries and Archive Members

An archive is used when it contributes at least one member to the final link.

```
libfoo.a
  a.o  <- extracted   -> used
  b.o                 -> unused
  c.o                 -> unused
```

Unused members MUST NOT be included.

If member-level information is unavailable, the archive is classified used and `ARCHIVE_MEMBERS_UNRESOLVED` is emitted; member-level provenance remains unresolved.

Representation rules:

| Case | Representation |
|---|---|
| Prebuilt external archive | File component (always) |
| Project-generated archive, all member sources resolved, `includeTransientBuildArtifacts=false` | Evidence only, no file component |
| Project-generated archive, any member source unresolved | File component + `LINKED_OBJECT_SOURCE_UNRESOLVED` |
| `includeTransientBuildArtifacts=true` | File component (always) |

An archive is "project-generated" iff its canonical path anchor is `build`, or it is listed as an artifact of a CMake target in the File API reply.

---

## 13. Object Files and Object→Source Mapping

### 13.1 Object Representation

An object used by the final linker is sufficient evidence that its associated source was used.

Objects MUST NOT become file components when they are transient build artifacts, their source is known, and the source is represented.

Objects MUST become file components when: they are prebuilt external objects; they are delivered artifacts; their source cannot be resolved; or `includeTransientBuildArtifacts=true`.

If a linked object cannot be mapped to a source, emit `LINKED_OBJECT_SOURCE_UNRESOLVED`. The object remains valid evidence and MUST appear as a file component so that nothing silently disappears (§39.2).

### 13.2 Mapping Algorithm (Normative)

Object→source mapping MUST NOT rely on basenames alone. The resolver runs these strategies in order and stops at the first that yields exactly one source:

1. **CMake File API**: match object path against `targets[].compileGroups`/`sources` combined with the generator's object naming for the selected configuration.
2. **Ninja build rule**: the `build <obj>: CXX_COMPILER__<target> <src>` edge in `build.ninja` (or `ninja -t commands`).
3. **MSBuild `.tlog`**: `CL.write.1.tlog` maps sources to written objects.
4. **`compile_commands.json`**: match on the `output` field if present; otherwise match `(directory, file)` against the object's expected path.
5. **Depfile adjacency**: the `.d`/`.o.d` file next to the object names the object as its target and its first prerequisite as the source.
6. **DWARF**: the compilation-unit name inside the object, when it still carries debug information. This is the only strategy that asks the object rather than the build system, which is why it is the one that answers for a prebuilt archive: nothing in the compile database, the build graph or a depfile mentions its members, because this build did not compile them. It reads a member of a static archive as readily as a standalone object, and only objects no earlier strategy claimed, because opening every object of a large build would cost more than the run does.
7. **Build log fallback** (`weak`).

Strategies 1–6 are `derived`; strategy 7 is `weak`. Basename-only matching is **forbidden** as a standalone strategy.

If two strategies yield different sources, the higher-priority one wins and `OBJECT_SOURCE_MAPPING_CONFLICT` is emitted (informational).

### 13.3 Duplicate Basenames

Duplicate basenames MUST be supported. These are distinct objects:

```
build/targetA/CMakeFiles/foo.dir/main.cpp.o
build/targetB/CMakeFiles/foo.dir/main.cpp.o
```

Object node IDs are canonical paths, so distinctness is structural.

---

## 14. Compile and Header Evidence

### 14.1 Compile Evidence Sources

`compile_commands.json`, compiler dependency files, generated build rules, MSBuild `.tlog`, build logs.

`compile_commands.json` MUST NOT by itself determine SBOM scope. A source appearing in it but producing an unused object MUST NOT be included. It may establish: source/object relationships, compiler, language, architecture, ABI, defines, include paths, flags, depfile locations.

### 14.2 Header Usage Rule

A header is used if it appears in dependency information for a translation unit whose object is used by the final deliverable, or in DWARF line-table evidence for a CU present in the artifact.

Supported dependency sources: GCC/Clang `.d` files; `ninja -t deps` / `.ninja_deps`; MSVC `/showIncludes` output captured in build logs or `.tlog`; IAR dependency output; vendor-specific dependency files.

If a used translation unit has no dependency evidence at all, emit `MISSING_HEADER_DEPENDENCY_EVIDENCE` naming the TU.

### 14.3 Depfile Parsing

Depfiles MUST be parsed with the grammar in Appendix D, which fixes the escaping rules that v2.0 left implicit (backslash-escaped spaces, `$$`, `\` line continuations, Windows drive colons, Ninja's `C$:` form, `#` comments, multiple targets).

### 14.4 Header Classification

Every used header is classified into exactly one class:

| Class | Rule |
|---|---|
| `generated-header` | Anchor `build`, or `isGenerated` in File API, or produced by a known generator edge |
| `project-header` | Anchor `project` and not inside a mapped third-party/SDK component root |
| `third-party-header` | Anchor `pkg:*`, or `project` inside a component root mapped from a non-project source (submodule, curated mapping) |
| `sdk-header` | Anchor `sdk:*` |
| `compiler-runtime-header` | Anchor `toolchain:*` and within the compiler's own resource/include dirs |
| `system-header` | Anchor `sysroot:*`, or within implicit system include dirs reported by `toolchains-v1` / `-print-search-dirs`, or anchor `abs` under `/usr/include`, `/usr/local/include` |
| `unknown-header` | None of the above |

Classification MUST use toolchain-reported implicit include directories rather than hardcoded path lists whenever they are available.

Default policy:

```
project-header          -> include
third-party-header      -> include
sdk-header              -> include
generated-header        -> include
system-header           -> exclude
compiler-runtime-header -> exclude
unknown-header          -> include + UNKNOWN_HEADER_CLASS finding (review)
```

**[Clarified vs v2.0: `review` now means "include and flag", not "omit". Nothing disappears silently.]**

Transitive headers present in dependency information are used. Where the source distinguishes direct from transitive inclusion (DWARF line table, MSVC `/showIncludes` nesting depth), the distinction MUST be preserved in property `sbomb:evidence:header:directInclude`.

### 14.5 Precompiled Headers

**[New.]** CMake `target_precompile_headers` creates `cmake_pch.hxx`/`cmake_pch.cxx` in the build tree, and every TU in the target then depends on the entire PCH header set.

Handling:

* The PCH aggregation source (`cmake_pch.cxx`) and its object are transient build artifacts.
* Headers reached only via the PCH are marked with property `sbomb:evidence:header:viaPch=true` and get confidence `medium`.
* `policy.pchHeaders` controls inclusion: `include` (default), `annotate-only`, `exclude`. `exclude` removes headers whose *only* evidence path is via the PCH, and emits `PCH_HEADERS_EXCLUDED` with a count.

### 14.6 Non-C/C++ Inputs

In scope as sources when they produce a linked object: `.S`/`.s`/`.asm` (assembly, may produce depfiles), `.rc` (Windows resources, `RC.write.1.tlog`), `.def` (module definition, when passed to the linker), `.cu` (CUDA), `.m`/`.mm` (Objective-C/C++).

In scope as generated sources when generator evidence exists: Qt `moc`/`uic`/`rcc` output, protobuf/flatbuffers output, `.inc`/`.ipp` includes.

Out of scope: linker scripts (unless `includeLinkerScripts`), CMake files, documentation.

---

## 15. Header-Only Libraries

A header-only library is included when a header belonging to its component appears in the dependencies of a used translation unit. The tool MUST NOT require a dedicated object file.

A header-only component is a normal grouping component containing file components. It receives property `sbomb:component:headerOnly=true`.

---

## 16. Generated Files

Generated files are included only when a valid evidence chain connects them to the final deliverable.

```
schema.yaml -> generator -> generated.c -> generated.o -> firmware
```

`generated.c` MUST be included when its object is linked. `schema.yaml` MUST be included only when generator/buildgraph evidence identifies it as an input. A generator script's existence is never evidence that it ran.

Generator input evidence sources, in priority order:

1. CMake File API custom-command `dependencies`/`byproducts`.
2. Ninja `build` edge inputs for the generating rule (including implicit and order-only inputs; order-only inputs are recorded with strength `weak`).
3. Depfile emitted by the generator.

An explicit `generators[]` mapping was a fourth source. It is removed: it is an unverifiable assertion from a configuration file in a tool that otherwise records only what it can prove, and no build has needed it (deviation D20).

If a generated file is used but no generator input evidence exists, emit `MISSING_GENERATOR_INPUT_EVIDENCE`.

Classification: `generated-source`, `generated-header`, `generated-binary`, `generated-asset`, `generated-config`.

---

## 17. Unity Builds, LTO, and Optimization Effects

### 17.1 Unity Builds

Unity builds MUST be detected. Detection signals: CMake `UNITY_BUILD` in File API; a compiled source under `CMakeFiles/*/Unity/unity_*_cxx.cxx`; a source whose content consists only of comments and `#include` directives of other project sources.

When a unity TU is detected, the tool MUST recover the constituent sources by, in order:

1. File API `sources[]` of the target combined with `UNITY_BUILD_BATCH_SIZE` grouping metadata, when present;
2. **parsing the generated unity file's `#include` directives** — this is deterministic, requires no execution, and is explicitly permitted;
3. depfile of the unity TU, filtered to files with source extensions.

Each recovered source gets a `source-mapping` edge with strength `derived`, confidence `high` for (1) and (2), `medium` for (3).

If no strategy succeeds, every source of the target is marked unresolved and `UNITY_SOURCE_UNRESOLVED` is emitted.

### 17.2 LTO Detection

LTO is detected from compile/link flags (`-flto`, `-flto=thin`, `/GL`, `/LTCG`), or from ELF sections `.gnu.lto_*`, or from the linker map naming LTO temporary objects.

### 17.3 LTO Effects (Clarified)

**[Corrects v2.0's overstatement.]** LTO does not usually destroy *file-level* attribution: object files still appear on the link line, archives are still extracted, and DWARF CUs still name their sources. What LTO degrades is *symbol- and section-level* attribution and, with `-flto` without `-ffat-lto-objects`, object-internal structure.

Therefore:

* Object→source mapping continues to use the normal strategies of §13.2.
* Confidence for `source-mapping` edges is downgraded one level (§8.7) with reason `lto`.
* The tool MUST NOT fabricate an object-level chain when the linker reports only LTO temporaries; in that case DWARF (§11.4) is the required fallback, and if DWARF is absent, `LTO_ATTRIBUTION_DEGRADED` is emitted.

### 17.4 Section GC and ICF

See §4.5. ICF/COMDAT folding MUST NOT be used to remove files; it may only annotate.

---

## 18. Assets, Packaging, and Images

Firmware projects contain inputs outside the compiler/linker graph: filesystem files, ROMFS content, certificates, web assets, partition tables, bootloader binaries, configuration blobs, OTA metadata, compressed assets, firmware fragments.

A package or image **manifest** is sufficient evidence when it explicitly identifies a file as an input. Supported manifest kinds:

| Kind | Format |
|---|---|
| `sbomb-manifest` | Native JSON manifest, Appendix E |
| `cmake-custom-command` | File API custom command with `dependencies` |
| `esp-idf-partition-table` | ESP-IDF `partitions.csv` + generated binaries |
| `cpack` | `install_manifest.txt` |

Given

```
asset.json -> generated.asset.bin -> filesystem.img -> firmware-package
```

the tool SHOULD include both `asset.json` and `generated.asset.bin` when the generator relationship is known. The original asset MUST NOT be included solely because it sits next to the generated output.

`policy.includeAssets` (default `true`) controls asset inclusion. `policy.includeGeneratedIntermediateFiles` (default `false`) controls whether `generated.asset.bin` itself appears as a file component or only as evidence — note that per §3 an intermediate file is a *transient build artifact*, and a generated asset that is physically embedded in the deliverable is **not** transient and is always included.

---

## 19. Component Model

### 19.1 Structure

The SBOM consists of a root product component, grouping components, and file components.

| Level | CycloneDX `type` |
|---|---|
| Root product | `application`, `firmware`, `device`, or `library` from `project.type` |
| Artifact (assembly mode) | `application`, `firmware`, `file`, or `data` from artifact `role` |
| Grouping component | `library` by default; `framework` when configured; `application` for the project's own code |
| File component | `file` |

### 19.2 Component Mapping

Every used file MUST have exactly one **primary** grouping component. Mapping strategies, in priority order:

1. Curated mapping from configuration (`components[]`, matched by path prefix or glob).
2. Exact package-manager metadata (Conan, vcpkg, CPM, FetchContent, ESP-IDF component manager, west).
3. Git submodule boundary (`.gitmodules` + `git rev-parse --show-superproject-working-tree`).
4. Known SDK layout (SDK adapter).
5. Explicit CMake target → component mapping from configuration.
6. Nearest ancestor directory containing recognized package metadata (`conanfile.py/txt`, `vcpkg.json`, `CMakeLists.txt` with `project()`, `package.json`-like SDK manifests, `idf_component.yml`, `Cargo.toml` for mixed repos).
7. Anchor root itself (e.g. everything under `pkg:conan/mbedtls` maps to that package).
8. Unknown component.

**Longest matching path prefix wins** within a strategy. A directory named `vendor/`, `dep/`, `sdk/`, or `third_party/` does not by itself define a component.

### 19.3 Unknown Components

Unknown mappings MUST NOT be silently ignored. An unknown component MUST be emitted with:

```
name          = "unknown:<anchorKey>/<first-relPath-segment>"
type          = "library"
properties    sbomb:component:detectedBy = "unresolved"
licenses      = NOASSERTION (per §28.7)
properties    sbomb:review:required = "true"
```

and an `UNKNOWN_COMPONENT` finding listing the affected file count.

### 19.4 Git Metadata

Git repository boundaries MAY be used as component boundaries. Git metadata MAY supply repository URL, commit, tag, dirty state, and component root. Git metadata MUST NOT expand the used-file set. A checked-out submodule is not automatically a used dependency.

Git URLs MUST be normalized: `git@host:org/repo.git` → `https://host/org/repo`, credentials stripped. If the working tree is dirty, property `sbomb:component:vcsDirty=true` is set and `VCS_DIRTY` is emitted (informational).

The repository URL MUST be written as an external reference of type `vcs`, not as a property: CycloneDX specifies a field for it and §28.1 gives the specified field precedence. It is emitted at both specification versions. `sbomb:component:vcsCommit` and `sbomb:component:vcsDirty` qualify that URL and sit in the reference's property bag at 1.7, where external references have one, and on the component at 1.6. Neither is ever a stand-in for a supplier.

---

## 20. Version and PURL Resolution

**[New in v3.0. v2.0 specified license resolution in detail but left versions and PURLs entirely undefined, which made the output unusable for downstream consumers.]**

### 20.1 Requirement

Every grouping component MUST have either a resolved `version` or an explicit `UNKNOWN_VERSION` finding. Version is never guessed from a directory name.

### 20.2 Resolution Priority

1. Curated configuration (`components[].version`).
2. Package-manager metadata (exact declared version).
3. SDK adapter metadata (e.g. ESP-IDF `version.txt`, `idf_component.yml`).
4. Git tag via `git describe --tags --always --dirty` at the component root, when `versionFrom` includes `git` and introspection is allowed.
5. Git commit SHA (short, 12 chars), recorded as version `0.0.0-git.<sha>` **only when** `components[].versionFrom` explicitly requests it.
6. A version macro in a used header of the component, when `components[].versionFrom` names it explicitly, e.g. `{"versionFrom": "header:include/mbedtls/build_info.h:MBEDTLS_VERSION_STRING"}`. Only literal string or integer macro definitions are read; no preprocessing is performed.
7. None → omit `version`, emit `UNKNOWN_VERSION`.

`versionFrom` accepts a string or an ordered array of strings, restricting which strategies apply to that component.

### 20.3 Version Confidence

| Source | Confidence |
|---|---|
| Curated | high |
| Package-manager | high |
| SDK metadata | high |
| Git tag (clean tree, exact tag) | high |
| Git describe (with distance / dirty) | medium |
| Git commit only | medium |
| Header macro | medium |
| None | unknown |

Published as `component.evidence.identity` with `field: "version"`: the value in `concludedValue`, the confidence above as the numeric `confidence`, and the source as one method — its `technique` from the closed CycloneDX vocabulary, its `value` the exact source string, since three sources share `manifest-analysis` and only the value says which manifest. §28.1 gives the specified field precedence, and `evidence.identity` predates 1.6, so this is written at both specification versions.

| `VersionSource` | `technique` |
|---|---|
| `curated` | `attestation` |
| `conan`, `vcpkg`, `fetchcontent` | `manifest-analysis` |
| `header` | `source-code-analysis` |
| `go-build-info` | `binary-analysis` |
| `git`, `git-describe`, `git-commit`, anything unmapped | `other` |

A version with no recorded source produces no identity evidence. An unmapped source becomes `other` rather than the nearest-looking technique: a guess presented as a measurement is worse than an honest `other`.

### 20.4 PURL Construction

A `purl` MUST be emitted when, and only when, a package type and name can be asserted from a package-manager or SDK adapter, or from curated configuration.

| Adapter | purl form |
|---|---|
| Conan | `pkg:conan/<name>@<version>` (with `?channel=`/`?user=` when present) |
| vcpkg | `pkg:vcpkg/<name>@<version>` |
| ESP-IDF component manager | `pkg:idf/<namespace>/<name>@<version>` |
| Git-derived (submodule, FetchContent, CPM) | `pkg:generic/<name>@<version>?vcs_url=git%2B<url>%40<commit>` |
| Curated with explicit `purl` | verbatim |
| Otherwise | no purl; emit `UNKNOWN_PURL` (informational) |

Purl components MUST be percent-encoded per the purl specification. `cpe` is emitted only when curated.

### 20.5 Supplier and Author

`supplier` is emitted only from curated configuration or package-manager metadata. It MUST NOT be inferred from a Git URL host. `externalReferences` of type `vcs` SHOULD carry the normalized repository URL and `distribution` the package registry URL when known.

---

## 21. Package-Manager Adapters

Core discovery MUST NOT depend on package-manager metadata; it is a component-mapping, version, and license enhancement.

Adapters and their evidence files:

| Adapter | Read |
|---|---|
| Conan | `conanbuildinfo.json` / `conan_toolchain.cmake` / `conandata.yml` / `<build>/conan/*.json`, package root layout |
| vcpkg | `vcpkg.json`, `vcpkg-configuration.json`, `<vcpkg-root>/installed/<triplet>/share/<pkg>/vcpkg_abi_info.txt`, `CONTROL`/`vcpkg.json` in port |
| CPM.cmake | `CPM` cache directory layout, `cpm-package-lock.cmake` |
| FetchContent | File API + `_deps/<name>-src` layout + Git metadata |
| ESP-IDF component manager | `idf_component.yml`, `dependencies.lock` |
| Zephyr west | `west.yml`, `.west/config` |

Each adapter registers anchors (§7.4) and supplies component name, version, purl, license hint, and root path. Adapters MUST NOT add files to the used set.

---

## 22. License Resolution

### 22.1 Scope

License detection is scoped to used files and mapped components only. The tool MUST NOT scan the repository for license files outside mapped component roots.

### 22.2 Resolution Priority

1. Curated configuration.
2. SPDX identifier in the used file itself (`SPDX-License-Identifier:` within the first 64 KiB).
3. Explicit component metadata (package manifest `license` field).
4. Package-manager metadata.
5. Recognized license file in the component root (§22.3).
6. Recognized documentation in the component root (`README*` with an explicit `SPDX-License-Identifier:` line only).
7. SDK metadata.
8. External scanner results supplied via `--license-scan <file>` (SPDX or CycloneDX JSON input).
9. NOASSERTION.

Configured upstream metadata (`components[].upstream`) was a further step. It is removed: a repository URL says nothing about a licence without fetching it, and this tool does not access the network (deviation D20).

### 22.3 Recognized License Files and Permitted Detection Techniques

Recognized filenames (case-insensitive, optional extension `.txt`, `.md`): `LICENSE`, `LICENCE`, `COPYING`, `NOTICE`, `COPYRIGHT`, and `LICENSE-<id>`.

**Permitted detection techniques are exhaustively:**

1. `SPDX-License-Identifier:` expression extraction.
2. Exact SHA-256 match of the normalized file text against an embedded table of hashes of the official SPDX license texts. **Only the hashes are embedded, not the texts** — roughly 700 entries at 32 bytes each, so under 50 KB, which keeps the single-executable requirement (§37) unaffected. The table is generated at build time from the SPDX license list by `tools/spdxgen` into a committed Go file, and `tools/spdxgen --check` runs in CI to detect drift.
3. Normalized-text match after: lowercasing, collapsing whitespace, stripping copyright lines (`Copyright (c) ...`), stripping punctuation-only lines. Match must be exact after normalization.
4. Match against the SPDX `standardLicenseTemplate`, which declares — in the license list itself — which spans of the text may vary and, as a regular expression, what each may vary into. Outside those spans the comparison is exact under the normalization of technique 3 minus its line stripping, because a template covers the copyright statement with a variable of its own. Several templates matching is an ambiguity, not a choice: the result is NOASSERTION with reason `conflicting-evidence` and the candidates named. The templates are embedded gzipped beside the digest table and decompressed only after technique 2 has missed. Added by deviation D18; see it for what this costs.

**Forbidden:** fuzzy/similarity/percentage matching, keyword heuristics ("permission is hereby granted" → MIT), and any ML-based classification. Technique 4 is none of these: it has no score and no threshold, and what may vary is declared by the same authority that publishes the text technique 2 hashes. If none of techniques 1–4 succeed, the result is NOASSERTION with reason `license-text-unrecognized`.

### 22.4 License Evidence Classes

`file-level`, `component-level`, `inherited`, `scanner`, `upstream`, `unknown`.

The tool MUST distinguish evidence from assumption. A license found in an included header MUST NOT automatically become the license of the including source file.

### 22.5 Conflicts

Conflicting evidence MUST NOT be resolved silently. Example: configured `MIT`, file SPDX `Apache-2.0` → emit `LICENSE_CONFLICT`, mark review required, and emit **the curated value** as the effective license while recording the conflicting value in property `sbomb:license:conflictingValue`.

Multiple licenses within a component MUST NOT be combined into an SPDX expression unless their legal relationship is explicitly known (i.e. the expression came verbatim from a single source).

### 22.6 Confidence

```
curated configuration -> high
SPDX identifier       -> high
package metadata      -> high
license file (exact)  -> high
SDK metadata          -> high
README SPDX line      -> medium
external scanner      -> medium
inherited from parent -> low
NOASSERTION           -> unknown
```

### 22.7 NOASSERTION

Used when no reliable assertion is possible. Never invent a license from weak similarity. Every NOASSERTION MUST carry `sbomb:license:review=true` and `sbomb:license:reason=<reason-code>` from: `no-evidence`, `license-text-unrecognized`, `conflicting-evidence`, `component-unresolved`, `scanner-inconclusive`, `license-composition-unresolved`.

`license-composition-unresolved` reports a file that contains one or more complete license texts without being any one of them — two licenses one after the other, or a license with other material around it. The licenses found are recorded as observation in `component.evidence.licenses`; how they combine is stated in the prose between them and is not decidable by the permitted techniques, so `component.licenses` stays NOASSERTION until curated. Added by deviation D19.

### 22.8 Level Separation

Product license, component license, and file license are independent. Licenses MUST NOT be blindly propagated downward or upward. A file with no own evidence inherits the component license only with evidence class `inherited` and confidence `low`.

---

## 23. Hashing

* Every locally readable included file MUST receive a SHA-256 hash of its **raw bytes**. No line-ending or encoding normalization is performed.
* Additional algorithms MAY be emitted via `--hash-alg sha256,sha1,sha512`.
* If a file is unavailable or unreadable, no hash is emitted and `MISSING_FILE_HASH` is created.
* The tool MUST NOT recursively hash the source tree. Only files selected through evidence, plus license files of mapped components, are read.
* Hashing MUST be parallelized with a bounded worker pool (`--jobs`, default `runtime.NumCPU()`), and results MUST be order-independent.

Hashes reflect the file's current on-disk content, which may differ from what was built. This is why §27 staleness detection is mandatory and why `policy.failOnStaleBuildArtifacts` defaults to `true`.

---

## 24. System, Toolchain, and Distribution Files

### 24.1 Categories

| Category | Default |
|---|---|
| System headers (`sysroot:*`, implicit system include dirs) | excluded |
| Compiler internal headers (`toolchain:*` resource dirs) | excluded |
| Compiler runtime libraries (`libgcc`, `compiler-rt`, `libc++abi`, `msvcrt`) | `separate-component` |
| C/C++ standard library implementation (`libstdc++`, `libc++`, MSVC STL, newlib) | `separate-component` |
| Distribution shared libraries (e.g. `/usr/lib/x86_64-linux-gnu/libssl.so.3`) | `exclude` (default); `separate-component` in the `host-linux` profile |

### 24.2 Configuration Values

`includeSystemHeaders`: `false` | `true`.
`includeToolchainRuntime`: `exclude` | `main-sbom` | `separate-component` | `report-only`.
`systemLibraries`: `exclude` (default) | `main-sbom` | `separate-component` | `report-only`.

**Rationale for the default.** Embedded targets link statically, so distribution shared libraries do not exist there and scanning for them is pure noise. They matter only for host-system builds, where the `host-linux` policy profile switches this to `separate-component` and enables §24.3. When the tool detects a dynamically linked ELF artifact with `DT_NEEDED` entries while `systemLibraries` is `exclude`, it emits `DYNAMIC_DEPENDENCIES_IGNORED` (info) naming the count, so the omission is never silent.

`separate-component` means the files are included but grouped under a component with `sbomb:component:scope=toolchain` or `=system`, and the component is **not** a dependency of the product root but of a synthetic `build-environment` component.

Toolchain files MUST NOT silently appear as ordinary project dependencies.

### 24.3 OS Package Attribution

**[New.]** When `systemLibraries` is not `exclude` and `--allow-introspection` is set, the tool MAY attribute distribution libraries to OS packages using `dpkg -S <path>` or `rpm -qf --qf ...` and emit purls `pkg:deb/<distro>/<name>@<version>?arch=<arch>` or `pkg:rpm/...`. Failure to attribute is non-fatal and yields `UNKNOWN_COMPONENT`.

### 24.4 Toolchain Probing

Toolchain anchors and implicit include dirs are obtained from `toolchains-v1` (File API) when available; otherwise, with introspection enabled, from `<compiler> -print-search-dirs` / `-E -v -x c++ /dev/null` / `cl /Bv`. Without either, `TOOLCHAIN_LAYOUT_UNKNOWN` is emitted and system-header classification falls back to path heuristics with confidence `low`.

### 24.5 Dynamic Dependencies

For hosted executables, the tool MAY record the `DT_NEEDED` (ELF) or import table (PE) entries of the artifact when `--include-runtime-libraries` is set. Resolution of a soname to a concrete file uses the link-time library paths only; runtime loader search is not simulated. `dlopen`-loaded libraries are explicitly out of scope.

### 24.6 Linker Scripts

Linker scripts and memory layout files are excluded by default (`includeLinkerScripts=false`). When included they are classified `linker-script`, `memory-layout`, or `linker-config` and grouped under the `build-environment` component.

---

## 25. SDK Support

SDKs MUST NOT be scanned wholesale. SDK files are included only when evidence connects them to a final deliverable.

An SDK adapter MAY improve component boundaries, versions, license metadata, package metadata, and generated-file provenance. It MUST register an `sdk:<name>` anchor.

The first concrete embedded SDK adapter targets **ESP-IDF** (Milestone 20).

---

## 26. Findings Model

**[New in v3.0: v2.0 had finding IDs but no severities, no machine format, and no waiver mechanism, which makes strict policies unusable in real CI.]**

### 26.1 Finding Structure

```json
{
  "id": "UNKNOWN_COMPONENT",
  "severity": "warning",
  "subject": { "kind": "file", "ref": "project:dep/foo/bar.c" },
  "message": "No component mapping could be determined.",
  "detail": { "anchor": "project", "candidates": [] },
  "evidence": ["link:firmware.map#L1204"],
  "remediation": "Add a components[] entry with path \"dep/foo\".",
  "waived": false,
  "waiverReason": null
}
```

`severity` ∈ `error` | `warning` | `info`. Severity is a *default* per finding ID (Appendix A) and can be overridden per ID in `policy.severityOverrides`.

`subject.kind` ∈ `product` | `artifact` | `component` | `file` | `evidence` | `configuration` | `run`.

### 26.2 Output

Findings are always available as machine-readable JSON via `--findings-json <path>`. The document is:

```json
{ "schemaVersion": 1, "toolVersion": "...", "findings": [ ... ], "summary": { "error": 0, "warning": 3, "info": 12, "waived": 1 } }
```

Findings MUST be sorted by `(id, subject.kind, subject.ref, message)`.

### 26.3 Waivers

A waiver file (`--waivers <path>` or `policy.waiversFile`) suppresses findings deterministically:

```json
{
  "schemaVersion": 1,
  "waivers": [
    {
      "id": "UNKNOWN_LICENSE",
      "subject": "project:dep/legacy_blob/**",
      "reason": "Vendor confirmed proprietary; ticket SEC-1234.",
      "approvedBy": "compliance@example.com",
      "expires": "2027-01-01"
    }
  ]
}
```

Rules:

* `subject` is a glob over the canonical path string, or `*`.
* A waived finding still appears in output with `waived=true` and does not contribute to policy failure.
* An expired waiver does **not** suppress; it additionally produces `WAIVER_EXPIRED` (severity `warning`).
* A waiver that matches nothing produces `WAIVER_UNUSED` (severity `info`), enabling waiver hygiene in CI.
* Waiver evaluation uses the run's date, or `SOURCE_DATE_EPOCH` when set, so it is reproducible.

---

## 27. Stale Build Detection

### 27.1 Requirement

The tool MUST detect potentially stale evidence and MUST NOT present a complete-looking SBOM built from mismatched inputs.

### 27.2 Evidence Precedence for Staleness

1. **Artifact identity correlation** (§11.7): GNU build-id or PDB signature.
2. **Content hashes** recorded in generated manifests, when present.
3. **Ninja restat / `.ninja_log` output hashes**, when present.
4. **Timestamps** (fallback).

Stronger evidence takes precedence; timestamps MUST NOT override a successful identity correlation.

### 27.3 Timestamp Checks

Compare mtimes: source ≤ object ≤ archive ≤ artifact; depfile ≥ object; map ≈ artifact (within `policy.staleToleranceSeconds`, default 5). Violations produce `STALE_BUILD_EVIDENCE` with the offending pair in `detail`.

Also compare the CMake input files reported by `cmakeFiles-v1` against the File API reply mtime; a newer `CMakeLists.txt` produces `STALE_CMAKE_CONFIGURATION`.

### 27.4 Policy

`policy.failOnStaleBuildArtifacts` defaults to `true`.

---

## 28. CycloneDX Output Binding

**[This section fixes every previously open output decision. An implementation MUST follow it literally.]**

### 28.1 Specification Version

The default output is CycloneDX **1.6**, and it stays the default. This is fixed by §1.5: BSI TR-03183-2 v2.1.0 requires CycloneDX 1.6 as its minimum, and nothing in a later revision changes that. The default MUST NOT move until the compliance target does.

CycloneDX **1.7** MAY be written on request, via `--spec-version 1.7` or `output.specVersion`. `--spec-version` accepts `1.6` and `1.7`; any other value is a usage error (exit 1), raised before discovery runs rather than at the write. The `bomFormat` and `specVersion` fields MUST match the version written, and `sbomb:run:specVersion` MUST record it.

1.7 is additive over 1.6: 108 definitions against 91, nothing removed, the same required top-level fields, and JSON Schema draft-07 in both, so D6 is unaffected. A document written at 1.6 is therefore structurally valid at 1.7 with only `specVersion` changed.

**A standard field beats a property in the `sbomb:` namespace.** Where CycloneDX specifies a field for something this tool records, the specified field carries it. Where that field exists at 1.6 as well, it is emitted at **both** versions rather than gated behind 1.7; the version decides only what the version alone can express.

**A document MUST NOT change shape with the version beyond what the version requires.** A field that 1.7 permits and 1.6 forbids MAY be emitted at 1.7 only, and every such difference MUST be documented here rather than discovered. They are:

| 1.7-only | Instead, at 1.6 |
|---|---|
| `component.evidence.licenses` MAY mix SPDX expressions with licence identifiers, which 1.6's `licenseChoice` forbids (D19) | licence identifiers only |
| `component.isExternal` marks a component the environment provides | `sbomb:component:scope` and the build-environment grouping of §24.2, which both versions carry |
| `externalReference.properties` carries `sbomb:component:vcsCommit` and `sbomb:component:vcsDirty` beside the repository URL they qualify | the same properties on the component |
| `metadata.distributionConstraints.tlp`, written only when `output.tlp` is set | a TLP cannot be written; setting `output.tlp` at 1.6 is a usage error rather than a silent omission |

`component.externalReferences` is **not** in that table: the `vcs` reference type predates 1.6, so the repository URL is emitted at both versions and `sbomb:component:vcsUrl` is removed from appendix B.

The `citations` structure is not emitted. `evidence.identity` cannot carry a licence technique — its `field` enum admits identity fields only, and its `methods[].technique` vocabulary does not include the SPDX techniques of §22.3 — so `sbomb:license:technique` has no standard field and remains a property.

Both schemas MUST be embedded, so that `validate` can check either — including a document this tool did not write (§32.5).

### 28.2 Document Shape: Flat

`components` is a **flat array**. Nested `components[].components` MUST NOT be used. All structure is expressed via `dependencies[]`.

Rationale: consumer tooling handles the flat + dependency-graph form far more reliably than nesting, and merging SBOMs stays trivial.

`metadata.component` is the root product component and MUST NOT also appear in `components[]`.

### 28.3 Metadata

```json
{
  "metadata": {
    "timestamp": "<RFC3339 UTC or omitted>",
    "tools": { "components": [ { "type": "application", "name": "sbomb", "version": "<semver>",
                                 "hashes": [ { "alg": "SHA-256", "content": "<self-hash or omitted>" } ] } ] },
    "component": { ... root ... },
    "properties": [ ... run-level properties ... ]
  }
}
```

`metadata.timestamp` is the current UTC time, or the `SOURCE_DATE_EPOCH` instant when that variable is set, or omitted entirely in `--reproducible` mode.

### 28.4 bom-ref Scheme (Normative)

| Node | bom-ref |
|---|---|
| Root product | `product:<slug(project.name)>` |
| Artifact | `artifact:<canonicalPath>` |
| Grouping component | `component:<slug(name)>` when unique; otherwise `component:<slug(name)>@<slug(version)>`; otherwise `component:<slug(name)>#<sha256(componentRootCanonicalPath)[:12]>` |
| File component | `file:<canonicalPath>` |
| Build environment | `component:build-environment` |

`slug(s)`: lowercase; replace every character outside `[a-z0-9._-]` with `-`; collapse runs of `-`; trim leading/trailing `-`; truncate to 64 characters appending `-<sha256(s)[:8]>` if truncation occurred.

`canonicalPath` is §7.7 verbatim, including the anchor prefix.

`bom-ref` values MUST be stable across runs and MUST NOT change when a file's content changes. Collisions are impossible by construction; the implementation MUST nevertheless assert uniqueness and fail with exit 70 on collision.

### 28.5 Dependencies

```
product        -> [ artifact refs (assembly mode) | grouping component refs (single mode) ]
artifact       -> [ grouping component refs that contribute to it ]
grouping comp  -> [ file component refs it contains ]
file component -> [] (files have no outgoing dependencies)
product        -> build-environment (when any toolchain/system component exists)
```

Every `bom-ref` in `components[]` plus `metadata.component` MUST appear exactly once as a `dependencies[].ref` entry, even when `dependsOn` is empty. Arrays are sorted lexicographically by `ref` and by each `dependsOn` value.

The tool MUST NOT invent relationships to make the graph look complete.

### 28.6 Evidence Encoding

Use native CycloneDX fields first:

* `components[].evidence.identity`: an array (1.6+) with `field: "purl"` or `"name"`/`"version"`, `confidence` (float per §8.5), `methods[]` with `technique` ∈ `source-code-analysis` | `binary-analysis` | `manifest-analysis` | `filename` | `attestation` | `other`, `value`, and `confidence`.
* `components[].evidence.occurrences`: `[{ "bom-ref": "...", "location": "<canonicalPath>" }]` for file location.
* `components[].evidence.licenses`: license evidence, when the selected spec version supports it.

Everything not representable natively goes into `properties[]` (§28.8). The tool MUST NOT encode evidence solely in properties when a native field exists.

### 28.7 Licenses and NOASSERTION

* Known SPDX ID → `"licenses": [{ "license": { "id": "<SPDX-ID>" } }]`.
* Known SPDX expression → `"licenses": [{ "expression": "<expr>" }]`.
* Non-SPDX named license → `"licenses": [{ "license": { "name": "<name>" } }]`.
* No reliable assertion → `"licenses": [{ "license": { "name": "NOASSERTION" } }]` **plus** properties `sbomb:license:review=true` and `sbomb:license:reason=<code>`.

**[Clarified: CycloneDX has no native NOASSERTION concept; this encoding is normative for this tool so consumers see an explicit, greppable marker rather than a silently absent field.]**

### 28.8 Property Namespace

All properties use the prefix `sbomb:`. Names are `sbomb:<area>:<key>`. The complete catalogue is Appendix B. Property arrays are sorted by `(name, value)`. Multi-valued properties repeat the name.

### 28.9 serialNumber

Format `urn:uuid:<uuid>`.

* Normal mode: UUIDv4 from `crypto/rand`.
* `--reproducible` mode: UUIDv5 (RFC 4122, SHA-1 based) with namespace UUID `6ba7b811-9dad-11d1-80b4-00c04fd430c8` (the OID namespace) over the byte string `sbomb:` + the SHA-256 hex digest of the canonically serialized document with `serialNumber` set to the empty string and `metadata.timestamp` removed.

This makes the whole document a fixed point: identical inputs yield an identical `serialNumber`.

### 28.10 Output Serialization

* UTF-8, no BOM.
* Two-space indentation, `\n` line endings on all platforms, trailing newline.
* Object keys emitted in the order defined by the CycloneDX schema's property order, which the implementation encodes explicitly via ordered struct fields — **not** Go map iteration.
* HTML escaping in `encoding/json` MUST be disabled (`Encoder.SetEscapeHTML(false)`).

---

## 29. Determinism and Reproducibility

Given identical final artifacts, build evidence, source files, configuration, policy, waivers, and tool version, the tool MUST produce **byte-identical** output, except for `metadata.timestamp` and `serialNumber` outside `--reproducible` mode.

Mandatory ordering rules:

| Collection | Sort key |
|---|---|
| `components[]` | `(type, bom-ref)` |
| `dependencies[]` | `ref` |
| `dependsOn[]` | value |
| `properties[]` | `(name, value)` |
| `hashes[]` | `alg` |
| `licenses[]` | rendered license string |
| `evidence.identity[]` | `(field, value)` |
| `evidence.occurrences[]` | `location` |
| `externalReferences[]` | `(type, url)` |
| findings | `(id, subject.kind, subject.ref, message)` |

All sorting uses byte-wise comparison of UTF-8, not locale collation.

`--reproducible` additionally: omits `metadata.timestamp`, derives `serialNumber` per §28.9, omits `sbomb:run:*` volatile properties, and honours `SOURCE_DATE_EPOCH`.

A cross-platform determinism test is mandatory (Milestone 14): the same fixture processed on Linux and Windows MUST yield identical bytes.

---

## 30. Security and Trust Boundaries

Build metadata is untrusted input. Parsers MUST avoid arbitrary command execution, unsafe path traversal, unbounded allocation, and shell interpretation.

Concrete requirements:

1. No `sh -c` / `cmd /c`. All subprocess invocation uses `exec.Command` with an explicit argv from the §9.2 allowlist.
2. Every parser is streaming with explicit limits: max line 1 MiB, max tokens per line 100 000, max total input configurable (`--max-input-size`, default 2 GiB), max recursion depth 64.
3. Archive/manifest handling MUST reject entries with absolute paths or `..` segments (zip-slip).
4. Symlink handling per §7.6; reads outside registered anchors are refused by default.
5. All file reads use `O_NOFOLLOW` semantics for the final component where the platform supports it, when `--strict-symlinks` is set.
6. Regular expressions applied to untrusted input MUST be from `regexp` (RE2, linear time). Backtracking engines are forbidden.
7. Redaction (`--redact-unanchored-paths`) MUST apply to the SBOM, the findings JSON, and the review report equally.
8. The tool MUST NOT make network requests. There is no online license or vulnerability lookup.

---

## 31. Performance Requirements

Measured on a 4-core x86_64 runner with warm page cache, using the `large` fixture (§Appendix F):

| Scenario | Requirement |
|---|---|
| 50 000 used files, 200 MB linker map, 50 000 depfiles | wall time ≤ 90 s, peak RSS ≤ 1.5 GiB |
| 10 000 used files | wall time ≤ 15 s, peak RSS ≤ 512 MiB |
| Hashing throughput | ≥ 400 MB/s aggregate on 4 cores |
| Memory growth | MUST be linear in used-file count, not in source-tree size |

The tool MUST NOT read files that are not evidence-selected or required for configured metadata resolution.

A benchmark suite (`go test -bench`) MUST exist for: map parsing, depfile parsing, graph construction, and serialization.

---

## 32. CLI

### 32.1 Subcommands

```
sbomb generate   [flags]     Generate SBOM(s) and evaluate policy
sbomb explain    [flags]     Explain why a file or component is in the SBOM
sbomb validate   [flags]     Validate an existing CycloneDX document produced by this tool
sbomb evidence   [flags]     Dump the evidence graph without producing an SBOM
sbomb schema     [flags]     Print the embedded configuration / findings / evidence JSON Schemas
sbomb version                Print version, commit, build date, spec support
```

### 32.2 `generate`

```
sbomb generate \
  --source-dir . \
  --build-dir build/debug \
  --config sbomb.json \
  --output build/debug/app.cdx.json \
  --policy strict
```

| Flag | Default | Meaning |
|---|---|---|
| `--source-dir` | `.` | Source directory |
| `--build-dir` | required | Build directory |
| `--config` | `sbomb.json` if present | Configuration file |
| `--output` | — | Output file (single artifact) |
| `--output-dir` | — | Output directory (multiple artifacts) |
| `--mode` | from config, else `single` | `single` \| `assembly` |
| `--config-name` | — | Build configuration (multi-config generators) |
| `--policy` | `default` | `default` \| `strict` \| `lenient` \| path to a policy JSON |
| `--format` | `cyclonedx-json` | Output format |
| `--spec-version` | `1.6` | `1.6` \| `1.7` (§28.1) |
| `--map` | auto | Linker map path (repeatable) |
| `--link-depfile` | auto | Linker dependency file (repeatable) |
| `--image-manifest` | — | Package/image manifest (repeatable) |
| `--license-scan` | — | External scanner results |
| `--waivers` | from config | Waiver file |
| `--review-report` | — | Human-readable report path |
| `--findings-json` | — | Machine-readable findings path |
| `--evidence-dump` | `<build-dir>/evidence.json` | Evidence graph dump path, or `off` |
| `--allow-introspection` | off | Permit §9.2 allowlisted commands |
| `--allow-cmake-regenerate` | off | Permit `cmake -S -B` for File API queries |
| `--reproducible` | off | Deterministic serialNumber, no timestamp |
| `--redact-unanchored-paths` | off | Hash unanchored paths |
| `--include-system-headers` | policy | Override |
| `--include-toolchain-runtime` | policy | Override |
| `--include-linker-scripts` | policy | Override |
| `--include-assets` | policy | Override |
| `--include-runtime-libraries` | off | DT_NEEDED closure |
| `--fail-on-review-required` | policy | Override |
| `--max-input-size` | `2GiB` | Parser limit |
| `--profile-overlay` | — | Additional profile merged over `--policy`, e.g. `host-linux` |
| `--header-evidence` | policy | `dwarf-preferred` \| `union` \| `depfiles` |
| `--path-flavor` | from `runtime.GOOS` | `posix` \| `windows`. Hidden flag; exists so Windows path semantics are testable on Linux (§41 M14). |
| `--adapter` | — | Force an adapter, `<class>=<id>` (repeatable) |
| `--inventory-dump` | — | Internal inventory dump path (§40) |

Six of these are specified and not implemented, each waiting on the feature it
belongs to rather than on effort:
`--license-scan` on external scanner input (§22.2), `--output-dir` on assembly
mode, `--adapter` on adapter-selection override (§9.1),
`--allow-cmake-regenerate` on File API regeneration,
`--include-runtime-libraries` on the `DT_NEEDED` closure, and
`--inventory-dump` on §40. A flag that is specified and absent is refused with
`unknown flag`, never accepted and ignored.

Nine further flags were specified and are removed rather than built; deviation
D22 gives the reason for each.

Every `--include-*` / `--fail-on-*` flag overrides the corresponding policy value. Precedence: CLI flag > policy file/profile > configuration file `policy` block > built-in default.

### 32.3 `explain`

```
sbomb explain --build-dir build/debug --file dep/mbedtls/include/mbedtls/aes.h
sbomb explain --build-dir build/debug --component mbedtls
sbomb explain --build-dir build/debug --bom-ref file:project:src/main.cpp
```

Prints all evidence chains from the file/component to every reaching final deliverable, with evidence type, source, adapter, strength, and confidence per hop. `--format json` prints the same as structured data. Exit 0 if found, 3 if the subject exists but has no chain (should be impossible), 1 if the subject is unknown.

### 32.4 Exit Codes

```
0  = SBOM generated and policy passed
1  = usage or configuration error
2  = discovery error (evidence could not be collected)
3  = policy failed
4  = CycloneDX validation failed
70 = internal error / invariant violation
```

**Precedence when several apply** (highest first): `1`, `70`, `2`, `4`, `3`. **[Resolves a v2.0 gap.]**

### 32.5 Validation Strategy

Third-party Go modules are permitted (§37), so validation is performed **in-process** and is mandatory:

* The official CycloneDX JSON Schema files of **every version §28.1 permits** (`bom-1.6.schema.json`, `bom-1.7.schema.json`, `spdx.schema.json`, `jsf-0.82.schema.json`, `cryptography-defs.schema.json`) MUST be embedded with `go:embed` and validated against with a pure-Go JSON Schema validator. Every generated document is validated before it is written to its final path; validation runs on the exact bytes that will be written.
* The schema is selected by the **document's own `specVersion`**, not by the caller's: a file is checked against what it claims to be. A document declaring a version the build has no schema for MUST be refused, never checked against another version's schema.
* In addition, the tool MUST perform **semantic validation** that a JSON Schema cannot express: `bom-ref` uniqueness, dependency-ref closure (every `ref` and `dependsOn` resolves), hash length matching the declared algorithm, purl syntax, RFC 3339 timestamps, property-name membership in Appendix B, and the §1.5 CRA field completeness check when the `cra` profile is active.
* `sbomb schema --cyclonedx [--spec-version <v>]` prints the embedded schema of one version; `sbomb validate --input <file>` runs both layers against an existing document.
* `validate` MUST **detect** the serialization format from the document rather than assuming or requiring one: `bomFormat` identifies CycloneDX, and a second format identifies itself by its own marker. Somebody checking a file another tool sent them knows they have an SBOM, not which serialization it is in. A document in no format the build can read is a failure that names what it can read.
* Output is written atomically: to a temporary file in the destination directory, validated, then renamed. A failed validation MUST NOT leave a partial or invalid file at the target path.

Either validation layer failing → exit 4.

---

## 33. Policy Model

### 33.1 Options

```
failOnUnknownComponent            failOnUnknownLicense
failOnMissingHash                 failOnMissingSourceForLinkedObject
failOnStaleBuildArtifacts         failOnReviewRequired
failOnWeakEvidence                failOnMissingHeaderEvidence
failOnUnanchoredFile              failOnUnknownVersion
allowMissingLinkEvidence

includeSystemHeaders              includeToolchainRuntime
includeLinkerScripts              includeGeneratedIntermediateFiles
includeAssets                     includeTransientBuildArtifacts
systemLibraries                   pchHeaders
sectionGarbageCollection          prebuiltLibrariesRequireMapping

staleToleranceSeconds             severityOverrides
waiversFile                       headerEvidence
failOnMissingSupplier             failOnMissingComponentHash
```

### 33.2 Built-in Profiles

Five profiles exist: `lenient`, `default`, `strict`, `cra`, and `host-linux`. `cra` is the CRA/BSI field-completeness profile of §1.5. `host-linux` is a *scope* profile for hosted (non-embedded) Linux targets and is meant to be combined with one of the other three via `--policy cra --profile-overlay host-linux`.

| Option | `lenient` | `default` | `strict` | `cra` | `host-linux` overlay |
|---|---|---|---|---|---|
| `failOnUnknownComponent` | false | false | true | true | — |
| `failOnUnknownLicense` | false | false | true | true | — |
| `failOnUnknownVersion` | false | false | true | true | — |
| `failOnMissingSupplier` | false | false | true | true | — |
| `failOnMissingHash` | false | true | true | true | — |
| `failOnMissingComponentHash` | false | false | true | true | — |
| `failOnMissingSourceForLinkedObject` | false | true | true | true | — |
| `failOnStaleBuildArtifacts` | false | true | true | true | — |
| `failOnReviewRequired` | false | false | true | false | — |
| `failOnWeakEvidence` | false | false | true | false | — |
| `failOnMissingHeaderEvidence` | false | false | true | false | — |
| `failOnUnanchoredFile` | false | false | true | false | — |
| `allowMissingLinkEvidence` | true | false | false | false | — |
| `headerEvidence` | dwarf-preferred | dwarf-preferred | union | dwarf-preferred | — |
| `includeSystemHeaders` | false | false | false | false | — |
| `includeToolchainRuntime` | report-only | separate-component | separate-component | separate-component | — |
| `systemLibraries` | exclude | exclude | exclude | exclude | **separate-component** |
| `includeLinkerScripts` | false | false | true | false | — |
| `includeGeneratedIntermediateFiles` | false | false | false | false | — |
| `includeTransientBuildArtifacts` | false | false | false | false | — |
| `includeAssets` | true | true | true | true | — |
| `pchHeaders` | include | include | include | include | — |
| `sectionGarbageCollection` | ignore | ignore | annotate | ignore | — |
| `prebuiltLibrariesRequireMapping` | false | true | true | true | — |

Note that `cra` is deliberately **not** the strictest profile. It fails on missing CRA/BSI *fields* but tolerates weak evidence and review items, because the regulation is about documenting components, not about proving build provenance. `strict` is the engineering profile; `cra` is the compliance profile. A project may run both in CI, with only `cra` gating the release.

### 33.3 Discovery vs Policy Separation (Mandatory)

Discovery answers: *what can be proven associated with the final deliverable?*
Policy answers: *is that evidence complete and trustworthy enough for this project?*

Discovery MUST continue after unresolved findings whenever possible. Policy produces the final pass/fail. Therefore: **discovery completeness ≠ policy compliance**.

Policy MUST NOT remove components or files from the SBOM. It may only fail the run. The only exceptions are the explicitly inclusion-controlling `include*` / `systemLibraries` / `pchHeaders` / `sectionGarbageCollection=exclude` options, which are *discovery scope* settings applied before output, and each of which emits a finding recording what it removed.

---

## 34. Review Report

`--review-report <path>` produces a deterministic, plain-text (or `--report-format markdown`) report containing:

1. Run metadata: tool version, configuration path, policy profile, build directory, selected build configuration, adapters used.
2. Final deliverables with role, hash, and correlation status.
3. Counts: components by kind, files by class, evidence edges by type and strength.
4. Grouping components with version, version source/confidence, license, license evidence class/confidence, file count.
5. Unresolved items: source mappings, components, versions, licenses, hashes, anchors.
6. Stale evidence.
7. Findings grouped by severity, with waived findings in a separate section.
8. Policy result and exit code.
9. Evidence chains for a configurable sample (`--report-chains all|unresolved|none`, default `unresolved`).

Chain rendering format:

```
project:dep/mbedtls/include/mbedtls/aes.h
  used because:
    aes.h
      <- [header-dependency | derived | high | ninja:.ninja_deps] included by project:src/crypto.c
      <- [compile | derived | high | filapi:codemodel-v2] crypto.c compiled to build:CMakeFiles/app.dir/crypto.c.o
      <- [archive-member | linked | high | ld:--dependency-file] crypto.c.o extracted from build:libmbedtls.a
      <- [link | linked | high | ld:--dependency-file] libmbedtls.a linked into build:firmware.elf
```

---

# PART II — IMPLEMENTATION BLUEPRINT

## 35. Layered Architecture

```
CLI
 |-> Config          (load, merge, validate, policy profile resolution)
 |-> PathModel       (anchors, canonicalization)
 |-> BuildContext    (locate artifacts, detect generator/compiler/linker, select config)
 |-> Adapters        (File API, Ninja, Make, MSBuild, linkers, depfiles, DWARF, packaging, pkg-managers, SDK)
 |-> EvidenceGraph   (nodes, edges, invariants, dump)
 |-> Inventory       (merge, classify, dedupe, hash, staleness)
 |-> ComponentMap    (mapping, versions, purls)
 |-> LicenseEngine   (resolution, conflicts, confidence)
 |-> Policy          (findings evaluation, waivers, exit code)
 |-> Writers         (CycloneDX, findings JSON, evidence dump, review report)
```

Rules:

* The domain model MUST NOT import any adapter package.
* Adapters MUST NOT import the CycloneDX writer.
* Adapters produce normalized `evidence.Edge`/`evidence.Node` values only; they never mutate output structures.
* Only `internal/cli` may call `os.Exit`.
* Only `internal/exec` may spawn processes, and only from the §9.2 allowlist.

## 36. Repository Layout

```
cmd/sbomb/main.go
internal/cli/            subcommands, flag parsing, exit codes
internal/config/         config schema, loading, merging, policy profiles, waivers
internal/pathmodel/      anchors, canonical paths, slug, redaction
internal/domain/         core types (no I/O)
internal/evidence/       graph, edges, invariants, dump format
internal/buildctx/       build directory probing, generator/compiler/linker detection
internal/exec/           allowlisted subprocess execution with limits
internal/adapters/cmakeapi/
internal/adapters/ninja/
internal/adapters/make/
internal/adapters/msbuild/
internal/adapters/linkers/depfile/   (linker --dependency-file)
internal/adapters/linkers/gnuld/
internal/adapters/linkers/gold/
internal/adapters/linkers/lld/
internal/adapters/linkers/msvc/
internal/adapters/linkers/iar/
internal/adapters/binfmt/            (ELF/PE + DWARF)
internal/adapters/compiledb/         (compile_commands.json)
internal/adapters/depfiles/          (Make-format .d, .ninja_deps, showIncludes)
internal/adapters/packaging/
internal/adapters/pkgmgr/conan|vcpkg|cpm|fetchcontent|idf|west
internal/adapters/sdk/espidf/
internal/inventory/
internal/componentmap/
internal/version/        component version + purl resolution
internal/license/
internal/policy/
internal/findings/
internal/sbomwriter/     format-agnostic writer interface + registry (§36.1)
internal/cyclonedx/      CycloneDX 1.6 writer + schema and semantic validation
internal/report/
internal/testutil/       golden helpers, fixture loading
testdata/fixtures/       golden fixtures (see Appendix F)
docs/
.github/actions/sbomb/
```

Module path: `github.com/<org>/sbomb`. Minimum Go version: **1.22**.

### 36.1 Output Format Abstraction

Although only CycloneDX is implemented, the writer layer MUST be format-agnostic, so that SPDX 3.x can be added without touching discovery, inventory, mapping, licensing, or policy.

**One writer per serialization format; versions live inside it.** A consumer asks for a format, not for a shape, so CycloneDX 1.6 and 1.7 are one writer and SPDX 2.3 and 3.0.1 will be another. Where two versions of a format are a handful of fields on the same structure, one writer with version-conditional fields is honest. Where they share nothing at the document level, "one writer" MUST NOT become one function with a switch at the top: it dispatches to a renderer per version in separate files, over a shared mapping layer that decides which evidence edge means which relationship. That mapping is the reuse; the serialization is not.

```go
package sbomwriter

// Document is the format-neutral hand-off from discovery to a writer.
// It contains ONLY resolved facts; no format vocabulary appears in it.
type Document struct {
    Product     domain.Component
    Artifacts   []domain.Component
    Components  []domain.Component
    Files       []domain.UsedFile
    Relations   []Relation          // product/artifact/component/file edges, already sorted
    Evidence    *evidence.Graph     // for writers that can express provenance
    Findings    []domain.Finding
    Run         RunMetadata         // tool version, timestamps, policy profile, adapters
}

type Writer interface {
    ID() string                     // "cyclonedx-json"
    Versions() []string             // {"1.6", "1.7"}
    DefaultVersion() string         // "1.6"
    Write(w io.Writer, d *Document, opts Options) error
    Validate(r io.Reader) error
}

// Detector is optional, and is how `validate` identifies a document
// nobody told it the format of (§32.5).
type Detector interface {
    Detect(data []byte) (version string, ok bool)
}

func Register(w Writer)
func Get(id, version string) (Writer, error)
func Resolve(id, version string) (Writer, string, error)  // fills in the default
func DetectFormat(data []byte) (Writer, string, error)
```

Rules:

* **There is no implicit default version.** A writer MUST state its `DefaultVersion()`; a caller MUST NOT assume `Versions()[0]` is special, because that convention breaks the first time somebody reorders a slice. `Resolve` is the single place where a caller's empty version becomes a version, and every call site goes through it.
* A version a writer does not list is a usage error, raised before work begins. Silently downgrading a document a consumer asked for is worse than refusing.

* `internal/domain` and every layer above it MUST NOT import `internal/cyclonedx`.
* `Document` MUST NOT contain CycloneDX-specific field names, `bom-ref` strings, or property keys. `bom-ref` generation (§28.4) belongs to the CycloneDX writer; other formats derive their own identifiers from the same canonical paths.
* Adding a format means adding one package that implements `Writer` plus its golden tests. If adding a format requires changing anything below `sbomwriter`, the abstraction has been violated and that is a bug.
* `--format` lists registered writers; an unknown value is a usage error (exit 1) listing what is available.

## 37. Dependency Policy

Third-party Go modules are permitted. The binding constraints are instead:

1. **Single static executable.** Every release binary MUST build with `CGO_ENABLED=0` and run with no runtime dependency beyond the kernel. Any module requiring cgo is disqualified.
2. **No network at runtime.** A module that performs network I/O in the code paths used is disqualified.
3. **Vendored and pinned.** `go.mod`, `go.sum`, and `vendor/` are committed. Builds MUST work with `-mod=vendor` and no network.
4. **Justified.** Each direct dependency is listed in `docs/dev/dependencies.md` with what it does and what removing it would cost.
5. **Determinism is the implementation's responsibility, not the library's.** Where a library's serialization order is not guaranteed, the writer MUST impose the ordering of §29 itself before handing data to the library, and the golden tests MUST prove byte-stability.

Recommended direct dependencies:

| Purpose | Module | Note |
|---|---|---|
| CycloneDX model and JSON serialization | `github.com/CycloneDX/cyclonedx-go` | Removes hand-written schema drift risk. Ordering is still imposed by `internal/cyclonedx` (§29). |
| JSON Schema validation | a pure-Go JSON Schema draft 2020-12 validator | Required by §32.5. Must be cgo-free. |
| purl parsing/formatting | `github.com/package-url/packageurl-go` | Avoids hand-rolled percent-encoding bugs (§20.4). |

Everything else — depfile parsing, map parsing, Ninja parsing, DWARF, archive reading, path model — MUST remain standard library (`debug/elf`, `debug/pe`, `debug/dwarf`, `archive/tar`, `regexp`, and so on). Adding a dependency for any of those requires a recorded deviation (§0.2).

`debug/macho` is not used; Mach-O is out of scope.

## 38. Core Domain Types

These signatures are normative in shape; field names may not be changed, additional fields may be added.

```go
package domain

type AnchorKey string           // "project", "build", "pkg:conan/mbedtls", ... (never version-bearing)

type FileID struct {            // canonical identity of any file-like thing
    Anchor  AnchorKey
    RelPath string              // POSIX, no "..", never absolute
}

func (f FileID) Canonical() string   // "<anchor>:<relpath>"

type NodeKind string
const (
    NodeProduct         NodeKind = "product"
    NodeArtifact        NodeKind = "artifact"
    NodePackage         NodeKind = "package"
    NodeImage           NodeKind = "image"
    NodeArchive         NodeKind = "archive"
    NodeArchiveMember   NodeKind = "archive-member"
    NodeObject          NodeKind = "object"
    NodeTranslationUnit NodeKind = "translation-unit"
    NodeSource          NodeKind = "source"
    NodeHeader          NodeKind = "header"
    NodeAsset           NodeKind = "asset"
    NodeGenerator       NodeKind = "generator"
    NodeGeneratorInput  NodeKind = "generator-input"
    NodeToolchainFile   NodeKind = "toolchain-file"
)

type EvidenceType string   // link, archive-member, compile, source-mapping,
                           // header-dependency, generated, package, image, asset,
                           // generator-input, generator-output, toolchain, install, debug-info

type Strength string       // direct, linked, derived, packaged, generated, weak
type Confidence string     // high, medium, low, unknown

func (c Confidence) Float() float64      // 0.9 / 0.6 / 0.3 / 0.1
func (c Confidence) Downgrade() Confidence

type NodeID string         // FileID.Canonical() for file-like nodes; "product:<slug>" otherwise

type Node struct {
    ID         NodeID
    Kind       NodeKind
    File       *FileID       // nil for product
    Attributes map[string]string
}

type Edge struct {
    From       NodeID
    To         NodeID
    Type       EvidenceType
    Strength   Strength
    Confidence Confidence
    Source     string        // "ld:build/firmware.map", "dwarf:build/firmware.elf"
    Adapter    string        // "gnuld", "cmakeapi", ...
    Raw        string        // only when --keep-raw-evidence
    Attributes map[string]string
    Downgrades []string      // sorted reason codes
}

type FileClass string
// source, header, generated-source, generated-header, generated-binary,
// generated-asset, generated-config, object, archive, shared-library,
// prebuilt-object, asset, linker-script, memory-layout, linker-config,
// system-header, compiler-runtime-header, unknown

type UsedFile struct {
    ID          FileID
    Class       FileClass
    HeaderClass string        // only for headers, per §14.4
    Hashes      map[string]string
    SizeBytes   int64
    Missing     bool
    ComponentID string
    Properties  map[string][]string
}

type Component struct {
    ID            string      // internal stable id
    BomRef        string
    Name          string
    Version       string
    VersionSource string
    VersionConf   Confidence
    Type          string      // cyclonedx type
    PURL          string
    CPE           string
    Supplier      string
    Root          *FileID     // component root directory, when known
    Scope         string      // "", "toolchain", "system"
    Licenses      []LicenseFinding
    DetectedBy    string
    Properties    map[string][]string
    Files         []FileID
}

type LicenseFinding struct {
    Expression string
    SPDXID     string
    Name       string
    Evidence   string      // file-level, component-level, inherited, scanner, upstream, unknown
    Confidence Confidence
    Source     string      // path or adapter that produced it
    Reason     string      // for NOASSERTION
    Conflicts  []string
}

type Severity string  // error, warning, info

type Finding struct {
    ID          string
    Severity    Severity
    Subject     Subject
    Message     string
    Detail      map[string]any
    Evidence    []string
    Remediation string
    Waived      bool
    WaiverReason string
}

type Subject struct {
    Kind string   // product, artifact, component, file, evidence, configuration, run
    Ref  string
}
```

Graph API:

```go
package evidence

type Graph struct{ /* ... */ }

func New() *Graph
func (g *Graph) AddNode(n domain.Node) domain.NodeID
func (g *Graph) AddEdge(e domain.Edge)              // dedupes per §8.2
func (g *Graph) Nodes() []domain.Node               // sorted by ID
func (g *Graph) Edges() []domain.Edge               // sorted by (From,To,Type,Source,Adapter)
func (g *Graph) EdgesFrom(id domain.NodeID) []domain.Edge
func (g *Graph) EdgesTo(id domain.NodeID) []domain.Edge
func (g *Graph) Roots() []domain.NodeID             // product + artifact nodes
func (g *Graph) Reachable(from domain.NodeID) map[domain.NodeID]bool
func (g *Graph) Chains(to domain.NodeID, max int) [][]domain.Edge   // for explain/report
func (g *Graph) CheckInvariants() error             // §8.8
func (g *Graph) Dump(w io.Writer) error             // Appendix C format
func Load(r io.Reader) (*Graph, error)
```

Adapter interface:

```go
package adapters

type Adapter interface {
    ID() string
    Detect(ctx context.Context, bc *buildctx.Context) (confidence float64, err error)
    Collect(ctx context.Context, bc *buildctx.Context, sink Sink) error
}

type Sink interface {
    Node(domain.Node) domain.NodeID
    Edge(domain.Edge)
    Finding(domain.Finding)
    Anchor(key domain.AnchorKey, absDir string)
}
```

## 39. Cross-Cutting Invariants

### 39.1 Adapter Selection

Selection priority: explicit configuration → unambiguous build-system metadata → detected linker/map format → detected compiler metadata → fallback adapter → unresolved.

If two adapters of the same class both report `Detect` confidence above 0.5 and are mutually incompatible (e.g. `ninja` and `msbuild`), emit `AMBIGUOUS_ADAPTER_SELECTION` and exit 1. The tool MUST NOT choose arbitrarily. `--adapter <class>=<id>` forces a choice.

### 39.2 Nothing Disappears

A component or file MUST NOT vanish from the SBOM because its mapping, version, or license is unresolved. It remains represented with explicit review metadata. The only removals permitted are the discovery-scope options of §33.3, each of which emits a finding with a count.

### 39.3 Logging vs Findings

Logs are for humans debugging the tool. Findings are for users and CI. A condition that affects the SBOM's trustworthiness MUST be a finding, not merely a log line.

## 40. Internal Serialization Formats

Two internal formats are normative because tests depend on them:

* **Evidence dump** (`--evidence-dump`, `sbomb evidence`): Appendix C.
* **Inventory dump** (`--inventory-dump`, used by golden tests): a JSON document with `schemaVersion`, `files[]` (sorted by canonical path) with class, hashes, size, missing flag, component id, and properties; and `components[]` sorted by bom-ref.

Both MUST be stable, sorted, and independent of the CycloneDX writer, so that inventory correctness can be tested before any CycloneDX code exists.

---

# PART III — MILESTONE PLAN

## 41. Milestones

The milestone plan is maintained in the dedicated markdown files under [docs/dev/milestones/README.md](milestones/README.md) and the per-milestone entries linked there.

This specification intentionally does not duplicate the detailed milestone text; the milestone documents are the canonical source for scope, acceptance criteria, and implementation sequencing.

- [00-fixture-harness-and-golden-corpus.md](milestones/00-fixture-harness-and-golden-corpus.md)
- [01-cli-configuration-path-model-empty-sbom.md](milestones/01-cli-configuration-path-model-empty-sbom.md)
- [02-evidence-graph-and-dump-format.md](milestones/02-evidence-graph-and-dump-format.md)
- [03-cmake-file-api-adapter.md](milestones/03-cmake-file-api-adapter.md)
- [04-link-evidence-i-dependency-file-and-trace.md](milestones/04-link-evidence-i-dependency-file-and-trace.md)
- [05-link-evidence-ii-dwarf-binary-inspection.md](milestones/05-link-evidence-ii-dwarf-binary-inspection.md)
- [06-link-evidence-iii-map-parsers-fallback.md](milestones/06-link-evidence-iii-map-parsers-fallback.md)
- [07-ninja-buildgraph-and-object-source-resolution.md](milestones/07-ninja-buildgraph-and-object-source-resolution.md)
- [08-compile-and-header-evidence.md](milestones/08-compile-and-header-evidence.md)
- [09-inventory-classification-hashing-staleness.md](milestones/09-inventory-classification-hashing-staleness.md)
- [10-component-mapping-versions-and-purls.md](milestones/10-component-mapping-versions-and-purls.md)
- [11-license-resolution.md](milestones/11-license-resolution.md)
- [12-cyclonedx-writer.md](milestones/12-cyclonedx-writer.md)
- [13-policy-findings-waivers-report-explain.md](milestones/13-policy-findings-waivers-report-explain.md)
- [14-determinism-and-cross-platform-reproducibility.md](milestones/14-determinism-and-cross-platform-reproducibility.md)
- [15-robustness-fuzzing-performance.md](milestones/15-robustness-fuzzing-performance.md)
- [16-cmake-integration-github-action-release.md](milestones/16-cmake-integration-github-action-release.md)
- [17-makefiles-generator-adapter.md](milestones/17-makefiles-generator-adapter.md)
- [18-msvc-msbuild-adapter-parked.md](milestones/18-msvc-msbuild-adapter-parked.md)
- [19-packaging-images-and-assets.md](milestones/19-packaging-images-and-assets.md)
- [20-esp-idf-sdk-adapter.md](milestones/20-esp-idf-sdk-adapter.md)
- [21-iar-and-vendor-linkers-parked.md](milestones/21-iar-and-vendor-linkers-parked.md)

Any normative requirements that mention a milestone number refer to the corresponding file above rather than a duplicate narrative in this specification.

---

## 42. Definition of Done (Whole Tool, per Adapter)

For a given adapter, the implementation conforms when:

final deliverables are deterministically identified; link evidence is collected; linked objects and libraries are resolved; source mappings are established or explicitly unresolved; header dependencies are collected where configured; generated and package evidence is collected where configured; the evidence graph is complete for the supported build format and passes its invariants; used files are deduplicated; hashes are generated; components are mapped; versions and purls are resolved or explicitly unknown; licenses are resolved or NOASSERTION with a reason; unresolved items are reported as structured findings; policy is enforced with correct exit codes; CycloneDX output passes structural validation; the review report and `explain` are generated; output is byte-reproducible; and all fixture tests pass.

A component or file MUST NOT disappear from the SBOM merely because its mapping, version, or license is unresolved.

---

# PART IV — APPENDICES

## Appendix A — Findings Catalogue

Severity shown is the default and may be changed via `policy.severityOverrides`. "Gate" names the policy option that turns the finding into a run failure; `—` means the finding never fails the run by itself.

| ID | Severity | Gate | Meaning |
|---|---|---|---|
| `MISSING_FINAL_DELIVERABLE` | error | always (exit 1) | No artifact configured or discovered |
| `AMBIGUOUS_FINAL_DELIVERABLE` | error | always (exit 1) | Discovery found several candidates |
| `MISSING_ARTIFACT` | error | always (exit 2) | Configured artifact does not exist |
| `AMBIGUOUS_BUILD_CONFIG` | error | always (exit 1) | Multi-config build without `--config-name` |
| `AMBIGUOUS_ADAPTER_SELECTION` | error | always (exit 1) | Two incompatible adapters both detected |
| `MISSING_LINK_EVIDENCE` | error | `allowMissingLinkEvidence` | No link evidence source succeeded |
| `MALFORMED_LINK_EVIDENCE` | warning | — | Link evidence truncated or unparsable past a point |
| `LINK_EVIDENCE_ARTIFACT_MISMATCH` | error | `failOnStaleBuildArtifacts` | Evidence does not correspond to the artifact |
| `LINK_EVIDENCE_UNCORRELATED` | info | — | Correlation not possible for this format |
| `LINKED_OBJECT_SOURCE_UNRESOLVED` | warning | `failOnMissingSourceForLinkedObject` | Object could not be mapped to a source |
| `OBJECT_SOURCE_MAPPING_CONFLICT` | info | — | Two strategies disagreed; higher priority used |
| `ARCHIVE_MEMBERS_UNRESOLVED` | warning | — | Archive used, member-level detail unavailable |
| `WHOLE_ARCHIVE_MEMBERS_ENUMERATED` | info | — | Members read from the archive, not reported by the linker |
| `SECTION_GC_EXCLUDED` | info | — | Objects removed by `sectionGarbageCollection=exclude` |
| `LTO_ATTRIBUTION_DEGRADED` | warning | `failOnWeakEvidence` | LTO present and no DWARF fallback available |
| `UNITY_SOURCE_UNRESOLVED` | warning | `failOnMissingSourceForLinkedObject` | Unity TU constituents not recoverable |
| `MISSING_HEADER_DEPENDENCY_EVIDENCE` | warning | `failOnMissingHeaderEvidence` | Used TU has no dependency evidence |
| `UNKNOWN_HEADER_CLASS` | info | `failOnReviewRequired` | Header could not be classified |
| `PCH_HEADERS_EXCLUDED` | info | — | Headers removed by `pchHeaders=exclude` |
| `MISSING_GENERATOR_INPUT_EVIDENCE` | warning | — | Generated file used, generator inputs unknown |
| `MISSING_PACKAGE_EVIDENCE` | warning | — | Package/image artifact without a manifest |
| `STALE_BUILD_EVIDENCE` | error | `failOnStaleBuildArtifacts` | Timestamps or hashes indicate a stale build |
| `STALE_CMAKE_CONFIGURATION` | warning | `failOnStaleBuildArtifacts` | CMake inputs newer than the File API reply |
| `UNKNOWN_COMPONENT` | warning | `failOnUnknownComponent` | File could not be mapped to a component |
| `UNKNOWN_VERSION` | warning | `failOnUnknownVersion` | Component version could not be resolved |
| `UNKNOWN_PURL` | info | — | No package type assertable |
| `UNKNOWN_LICENSE` | warning | `failOnUnknownLicense` | Component license is NOASSERTION |
| `LICENSE_CONFLICT` | warning | `failOnReviewRequired` | Conflicting license evidence |
| `MISSING_FILE_HASH` | warning | `failOnMissingHash` | File unavailable or unreadable |
| `UNANCHORED_FILE` | warning | `failOnUnanchoredFile` | File matched no anchor |
| `VCS_DIRTY` | info | `failOnReviewRequired` | Component working tree is dirty |
| `TOOLCHAIN_LAYOUT_UNKNOWN` | warning | — | Implicit include dirs unknown; heuristic classification |
| `DEBUG_INFO_UNAVAILABLE` | info | — | Artifact stripped or no DWARF |
| `CMAKE_FILE_API_UNAVAILABLE` | warning | — | No reply directory and regeneration not permitted |
| `NINJA_DEPS_UNAVAILABLE` | info | — | `ninja -t deps` not permitted or failed |
| `RSP_DEPTH_EXCEEDED` | warning | — | Response file recursion limit hit |
| `INPUT_LIMIT_EXCEEDED` | warning | — | A parser limit of §30 was reached |
| `PREBUILT_LIBRARY_UNMAPPED` | warning | `prebuiltLibrariesRequireMapping` | Prebuilt library has no component mapping |
| `WAIVER_EXPIRED` | warning | — | A waiver's `expires` date has passed |
| `WAIVER_UNUSED` | info | — | A waiver matched no finding |
| `CONFIG_DEPRECATED_OPTION` | warning | — | Deprecated configuration key used |
| `HEADER_EVIDENCE_FALLBACK` | info | — | A CU had no DWARF coverage; depfile used instead |
| `DYNAMIC_DEPENDENCIES_IGNORED` | info | — | Artifact has `DT_NEEDED`/imports while `systemLibraries=exclude` |
| `REPRODUCIBLE_MODE_OMITS_TIMESTAMP` | info | — | `--reproducible` output lacks `metadata.timestamp`; not a CRA deliverable |
| `MISSING_SUPPLIER` | warning | `failOnMissingSupplier` | Component has no supplier/creator (CRA/BSI field) |
| `MISSING_COMPONENT_HASH` | warning | `failOnMissingComponentHash` | Component has no hash for its deployable form (CRA/BSI field) |
| `CRA_FIELD_INCOMPLETE` | error | `cra` profile | Aggregate: one or more §1.5(1) fields missing on some component |
| `SECTION_GC_INFO_UNAVAILABLE` | info | — | `sectionGarbageCollection` requested but the evidence source does not report discarded sections |
| `WEAK_EVIDENCE` | warning | `failOnWeakEvidence` | The only evidence for a file is a textual fallback source (§8.4) |
| `MISSING_COMPILE_EVIDENCE` | warning | — | No compile database was found, so object-to-source mapping loses a strategy (§14.1) |
| `MALFORMED_BINARY` | info | — | An artifact could not be parsed as ELF or PE, so no debug-info evidence was read (§11.4) |
| `INTERNAL_INVARIANT_VIOLATION` | error | always (exit 70) | A graph invariant of §8.8 or a `bom-ref` uniqueness assertion of §28.4 failed |

An implementation MUST NOT emit a finding ID that is not in this table without also adding it to the table and to `docs/findings.md`.

---

## Appendix B — Property Catalogue

All properties are namespaced `sbomb:`. Booleans are the strings `"true"`/`"false"`.

**Run-level (on `metadata.properties`)**

```
sbomb:run:toolVersion            sbomb:run:specVersion
sbomb:run:timestamp              sbomb:run:sourceDateEpoch
sbomb:run:policyProfile          sbomb:run:mode
sbomb:run:reproducible           sbomb:run:adapters        (repeated, sorted)
sbomb:build:generator            sbomb:build:config
sbomb:build:compilerId           sbomb:build:compilerVersion
sbomb:build:linkerId             sbomb:build:linkerVersion
sbomb:build:targetTriple         sbomb:build:ltoDetected
```

**Root / artifact components**

```
sbomb:artifact:role              sbomb:artifact:buildId
sbomb:artifact:correlation       (correlated | uncorrelated | mismatch)
```

**Grouping components**

```
sbomb:component:detectedBy       sbomb:component:root
sbomb:component:scope            (project | third-party | sdk | toolchain | system)
sbomb:component:headerOnly
sbomb:component:vcsCommit        sbomb:component:vcsTag
sbomb:component:vcsDirty
sbomb:license:source             sbomb:license:evidenceClass
sbomb:license:confidence         sbomb:license:review
sbomb:license:reason             sbomb:license:conflictingValue
sbomb:license:technique
sbomb:review:required
sbomb:cdx:executableProperty     (executable | non-executable)   [BSI TR-03183-2]
sbomb:cdx:archiveProperty        (archive | no-archive)          [BSI TR-03183-2]
sbomb:cdx:structuredProperty     (structured | unstructured)     [BSI TR-03183-2]
```

**Go binary components (`sbomb self`)**

```
sbomb:go:module                  sbomb:go:mainPackage
sbomb:go:toolchain               sbomb:go:moduleSum
sbomb:go:goos                    sbomb:go:goarch
sbomb:go:replaces                (module@version the linker substituted)
```

**File components**

```
sbomb:path:canonical             sbomb:file:anchor
sbomb:file:role                  sbomb:file:class
sbomb:evidence:header:class      sbomb:file:size
sbomb:file:missing               sbomb:file:resolvedTarget
sbomb:evidence:type              (repeated, sorted)
sbomb:evidence:source            (repeated, sorted)
sbomb:evidence:strength          (strongest present)
sbomb:evidence:confidence        (highest present)
sbomb:evidence:downgrades        (repeated, sorted)
sbomb:evidence:artifacts         (repeated, sorted bom-refs)
sbomb:evidence:link:archive      sbomb:evidence:link:object
sbomb:evidence:link:fullyDiscarded
sbomb:evidence:header:includedBy (repeated, sorted)
sbomb:evidence:header:directInclude
sbomb:evidence:header:viaPch
sbomb:evidence:generated:by      sbomb:evidence:generated:input (repeated)
sbomb:evidence:unity:parent
sbomb:build:target
sbomb:evidence:header:dwarfCovered
sbomb:evidence:header:narrowedByDwarf
sbomb:cdx:executableProperty     sbomb:cdx:archiveProperty
sbomb:cdx:structuredProperty
```

`sbomb:file:path` and `sbomb:file:headerClass` were the earlier names of
`sbomb:path:canonical` and `sbomb:evidence:header:class`. The names the writer
uses are the ones documents carry, so they are the ones recorded here
(deviation D26).

Property values MUST NOT contain absolute paths. Every path-valued property uses canonical form (§7.7).

---

## Appendix C — Evidence Dump Format

```json
{
  "schemaVersion": 1,
  "toolVersion": "1.0.0",
  "generatedAt": "<omitted in --reproducible>",
  "anchors": [ { "key": "project", "hint": "<basename only>" } ],
  "nodes": [
    { "id": "project:src/main.cpp", "kind": "source", "attributes": {} }
  ],
  "edges": [
    {
      "from": "build:firmware.elf",
      "to": "build:CMakeFiles/app.dir/main.cpp.o",
      "type": "link",
      "strength": "linked",
      "confidence": "high",
      "source": "linkdep:build/firmware.elf.d",
      "adapter": "linkdepfile",
      "attributes": {},
      "downgrades": []
    }
  ],
  "findings": [ ... ]
}
```

`nodes` sorted by `id`; `edges` sorted by `(from, to, type, source, adapter)`; `anchors` sorted by `key`. Anchor absolute directories are **never** written; only the key and an optional basename hint.

---

## Appendix D — Depfile Grammar (Normative)

Applies to GCC/Clang `.d` files, `-Wl,--dependency-file` output, and `ninja -t deps` text conversion.

```
depfile     := line*
line        := comment | rule | blank
comment     := WS* '#' [^\n]* NEWLINE
rule        := targets WS* ':' WS* prereqs? NEWLINE
targets     := token (WS+ token)*
prereqs     := token (WS+ token)*
token       := (escaped | plain)+
escaped     := '\' ' '      -> literal space
             | '\' '\t'     -> literal tab
             | '\' '#'      -> literal '#'
             | '\' ':'      -> literal ':'
             | '\' '\'      -> literal backslash
             | '$' '$'      -> literal '$'
             | '$' ':'      -> literal ':'   (Ninja form, e.g. C$:/src)
             | '\' NEWLINE  -> line continuation (consumed, treated as WS)
plain       := any character except WS, ':', '#', NEWLINE, '\', '$'
```

Additional normative rules:

1. **Windows drive letters.** A `:` immediately following a single alphabetic character at the start of a token, and followed by `/` or `\`, is a drive separator and does **not** terminate the target list. Implementations MUST apply this rule before the general `:` handling.
2. **Line endings.** `\r\n` and `\n` are both accepted; a lone `\r` before `\n` is stripped. A `\` immediately before `\r\n` is a continuation.
3. **Backslash before a non-special character** is a literal backslash (GNU make behaviour), so `C:\src\main.cpp` parses as a path on Windows-produced depfiles.
4. **Multiple rules** in one file are all processed; targets are unioned.
5. **Empty prerequisite lists** are valid and produce no edges.
6. **Duplicate prerequisites** are deduplicated.
7. **Phony targets** (a prerequisite that also appears as a target with no prerequisites, as emitted by `-MP`) MUST be ignored.
8. **Order.** Prerequisite order is not meaningful; output is sorted.
9. **Limits.** Max line 1 MiB (after continuation joining: max logical line 8 MiB), max tokens per rule 1 000 000.

A conforming implementation MUST pass the table-driven test vectors in `testdata/depfiles/vectors.json`, which MUST contain at minimum one case per rule above.

---

## Appendix E — Native Package/Image Manifest

```json
{
  "schemaVersion": 1,
  "outputs": [
    {
      "path": "build/filesystem.img",
      "kind": "image",
      "inputs": [
        { "path": "assets/index.html", "role": "asset" },
        { "path": "build/generated/config.bin", "role": "generated-asset",
          "generatedFrom": ["config/config.yaml"], "generator": "tools/genconfig.py" }
      ]
    },
    {
      "path": "build/ota-package.bin",
      "kind": "package",
      "inputs": [
        { "path": "build/bootloader.bin", "role": "artifact" },
        { "path": "build/application.bin", "role": "artifact" },
        { "path": "build/filesystem.img", "role": "image" }
      ]
    }
  ]
}
```

All paths are resolved relative to the project root unless absolute. Paths containing `..` after resolution that escape every anchor are rejected with `INPUT_LIMIT_EXCEEDED`. `role` ∈ `asset` | `generated-asset` | `artifact` | `image` | `config` | `data`.

---

## Appendix F — Fixture Layout

```
testdata/
  projects/<pNN-name>/                     minimal CMake sources
  fixtures/<toolchain>/<pNN-name>/
      manifest.json                        toolchain versions, flags, volatile fields
      src/                                 copied sources with sentinel-rooted paths
      build/
        build.ninja | Makefile tree | *.vcxproj
        compile_commands.json
        .cmake/api/v1/reply/**
        CMakeFiles/**/*.d
        .ninja_deps        ninja-deps.txt
        firmware.map       link.d          link-trace.txt
        install_manifest.txt
        <artifact>         <artifact>.stripped
      expected/
        evidence.json      inventory.json
        sbom.cdx.json      report.txt      findings.json
  golden/                                  cross-fixture golden outputs
  depfiles/vectors.json                    Appendix D test vectors
  config/                                  configuration files used by tests
```

A golden fixture is complete when `expected/` contains all five files and they are regenerable by `scripts/update-golden.sh` (which MUST fail if the working tree is dirty, to prevent accidental golden drift).

---

## Appendix G — Configuration Schema (Illustrative Instance)

The normative JSON Schema is embedded in the binary and printed by `sbomb schema --config`. This instance shows every key.

```json
{
  "schemaVersion": 3,
  "project": {
    "name": "example-firmware",
    "type": "firmware",
    "root": ".",
    "version": "1.4.2",
    "supplier": "Example Org",
    "license": "Proprietary"
  },
  "build": {
    "dir": "build/debug",
    "config": "Debug",
    "introspection": { "cmake": false, "ninja": true, "git": true, "osPackages": false }
  },
  "mode": "single",
  "anchors": [
    { "key": "extern:shared", "path": "../shared" }
  ],
  "artifacts": [
    { "path": "build/debug/firmware.elf", "role": "application",
      "map": "build/debug/firmware.map", "linkDepfile": "build/debug/firmware.elf.d" }
  ],
  "discovery": {
    "excludeTargetPatterns": ["*test*", "*example*", "*sample*", "*benchmark*"]
  },
  "components": [
    {
      "path": "dep/mbedtls",
      "name": "mbedtls",
      "type": "library",
      "versionFrom": ["git", "header:include/mbedtls/build_info.h:MBEDTLS_VERSION_STRING"],
      "license": "Apache-2.0",
      "supplier": "Trusted Firmware",
      "purl": null
    },
    { "match": "dep/vendor-*/**", "name": "vendor-blobs", "type": "library", "license": "NOASSERTION" }
  ],
  "manifests": ["packaging/firmware-manifest.json"],
  "policy": {
    "profile": "cra",
    "profileOverlay": null,
    "failOnUnknownComponent": false,
    "failOnUnknownLicense": false,
    "failOnUnknownVersion": false,
    "failOnMissingHash": true,
    "failOnMissingSourceForLinkedObject": true,
    "failOnStaleBuildArtifacts": true,
    "failOnReviewRequired": false,
    "failOnWeakEvidence": false,
    "failOnMissingHeaderEvidence": false,
    "failOnUnanchoredFile": false,
    "allowMissingLinkEvidence": false,
    "headerEvidence": "dwarf-preferred",
    "failOnMissingSupplier": true,
    "failOnMissingComponentHash": true,
    "includeSystemHeaders": false,
    "includeToolchainRuntime": "separate-component",
    "systemLibraries": "exclude",
    "includeLinkerScripts": false,
    "includeGeneratedIntermediateFiles": false,
    "includeTransientBuildArtifacts": false,
    "includeAssets": true,
    "pchHeaders": "include",
    "sectionGarbageCollection": "ignore",
    "prebuiltLibrariesRequireMapping": true,
    "staleToleranceSeconds": 5,
    "severityOverrides": { "UNKNOWN_PURL": "warning" },
    "waiversFile": "sbomb-waivers.json"
  },
  "output": {
    "format": "cyclonedx-json",
    "specVersion": "1.6",
    "reproducible": false
  }
}
```

Unknown keys are a configuration error (exit 1) **at every level of the document**, so that a typo in a policy option name cannot silently disable a gate. Checking only the top level leaves exactly that hole: `{"policy": {"failOnMisingHash": true}}` loaded without complaint and the gate stayed off. Deprecated keys are accepted with `CONFIG_DEPRECATED_OPTION`.

---

## Appendix H — Design Summary

```
                    FINAL DELIVERABLE
                           |
      +--------------------+--------------------+
      |          |         |          |         |
  link depfile  DWARF    map      trace    packaging manifest
      +--------------------+--------------------+
                           |
                    CMake File API / buildgraph
                           |
                    dependency evidence
                           |
                    EVIDENCE GRAPH  (typed, strength, confidence)
                           |
                    FILE INVENTORY  (classify, dedupe, hash, staleness)
                           |
                    COMPONENT MAP   (mapping, version, purl)
                           |
                    LICENSE ENGINE
                           |
                    POLICY ENGINE
                    /      |       \
        CycloneDX JSON  Findings   Review Report / explain
```

The fundamental rules:

```
Repository presence is not evidence.
Compilation is not usage.
CMake declaration is not usage.
Build availability is not usage.

Final-artifact evidence is usage.
```

Every included file MUST be explainable as a concrete chain of build inputs and relationships, and `sbomb explain` MUST be able to print that chain.

---

## Appendix I — Change Log Relative to v2.0

| Area | Change |
|---|---|
| §7 | **New** anchor/path-identity model, replacing project-relative-only canonical paths |
| §20 | **New** version and PURL resolution (absent in v2.0) |
| §26 | **New** structured findings, severities, machine format, waivers |
| §28 | Output binding fully pinned: flat components, bom-ref schemes, native evidence fields, NOASSERTION encoding, property namespace, deterministic serialNumber, key ordering |
| §4.5, §4.6 | **New** section-GC, ICF, whole-archive, thin-archive semantics |
| §9.2 | Introspection command allowlist, resolving the "no execution" vs "introspection" contradiction |
| §10 | CMake File API promoted from one bullet to a dedicated adapter section |
| §11.2–11.4 | Link evidence reordered: `--dependency-file` and DWARF before map parsing |
| §14.5 | **New** precompiled-header handling |
| §17.3 | LTO impact corrected: file-level attribution usually survives |
| §24.3, §24.5 | **New** OS-package attribution and dynamic-dependency handling |
| §32.4, §32.5 | Exit-code precedence defined; schema-validation vs stdlib-only contradiction resolved |
| §3 | Project root, intermediate file, strength vs confidence all given normative definitions |
| §6.1 | `--output` with multiple artifacts is now an explicit usage error |
| §29, §31 | Ordering rules and concrete performance budgets made testable |
| §41 | Milestone 0 (fixtures) added; MSVC split out; IAR marked fixture-only; determinism, fuzzing/performance, and explain given their own milestones; every milestone given literal acceptance commands and exit codes |

## Appendix J — Scope Decisions Recorded in v3.1

These were open questions in v3.0 and are now settled. An implementing agent MUST treat them as fixed and MUST NOT reopen them.

| # | Decision | Where it lands |
|---|---|---|
| 1 | The tool **may** influence the build, but only as an opt-in evidence enablement plus a **separate build target**. No implicit `POST_BUILD` step. | §41 M16 |
| 2 | Unstripped artifacts are available. DWARF is a first-class evidence source. | §11.4, §41 M5 |
| 3 | Compliance target is the **EU CRA**, with **BSI TR-03183-2 v2.1.0** as the concrete field-level target. A `cra` policy profile exists. | §1.5, §33.2 |
| 4 | **CycloneDX only** for now, but the writer layer is format-agnostic so SPDX or CDX 1.7 can be added without touching discovery. | §36.1 |
| 5 | **macOS / Mach-O out of scope.** | §1.3 |
| 6 | **Third-party Go modules allowed**, under a single-static-executable constraint. `cyclonedx-go` and an in-process JSON Schema validator are used. | §32.5, §37 |
| 7 | **CycloneDX 1.6 only.** 1.7 not implemented. | §28.1 |
| 8 | Anchor keys are **version-free** (`pkg:conan/mbedtls`, not `...@3.5.0`), so file `bom-ref`s stay stable across dependency version bumps and release-to-release SBOM diffs stay readable. The version lives on the component. | §7.2, §28.4 |
| 9 | **DWARF is the primary header evidence source** (`headerEvidence=dwarf-preferred`); depfiles are the fallback. Headers dropped by DWARF narrowing are counted and reported, never silently discarded. | §4.4, §33.2 |
| 10 | `systemLibraries` defaults to **`exclude`**, because embedded targets link statically. A `host-linux` profile overlay switches it on for hosted builds. Ignored dynamic dependencies are reported. | §24.1, §24.2, §33.2 |
| 11 | **MSVC parked** (M18). PDB parsing out of scope. | §11.4, §41 M18 |
| 12 | The SPDX license corpus is embedded as **hashes only** (~700 entries, under 50 KB), not full texts, so the single-executable requirement is unaffected. Generated by `tools/spdxgen`, drift-checked in CI. | §22.3, §41 M11 |
| 13 | **IAR and vendor linkers parked** (M21) until real fixtures are supplied. | §41 M21 |
| 14 | **Linux CI only.** Windows binaries are cross-compiled and released but untested by CI; Windows path semantics are covered by an injectable path flavor plus synthetic and mingw fixtures. | §41 M14, M16 |
| 15 | **Fixture licensing policy:** only build-metadata *text* and self-built artifacts from original fixture projects may be committed. No third-party source, headers, toolchain files, or SDK trees. Every fixture carries a `PROVENANCE.md`. | §41 M0 |
| 16 | Tool name is **`sbomb`**; module path `github.com/<org>/sbomb`; property namespace `sbomb:`. | throughout |

### Still Open

| # | Question | Current placeholder |
|---|---|---|
| J-1 | The `<org>` part of the module path and the repository's own license. | `github.com/<org>/sbomb`, license unset |
| J-2 | Whether the ESP-IDF adapter (M20) is still wanted, given that it needs a large committed fixture under the §41 M0 licensing policy. | M20 retained as written |
| J-3 | Whether an `--attest`/in-toto provenance output is wanted alongside the SBOM for CRA technical documentation. | not specified |
