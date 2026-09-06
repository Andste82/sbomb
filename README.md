![sbomb logo](./assets/sbomb_logo.jpg)

# sbomb

An evidence-based SBOM generator for CMake that traces precise build evidence to ensure CRA compliance according to BSI TR-03183.

sbomb is a tool for generating machine-readable software bills of materials from real build evidence instead of guessing from a source tree. It starts from configured final deliverables such as firmware images, executables, libraries, or package artifacts and follows the evidence chain through link inputs, archives, object files, translation units, headers, and generated files to determine exactly which files and components were actually used.

This makes it suitable for regulated embedded and firmware projects where provenance matters: you want a CycloneDX SBOM that reflects the concrete build, not a broad repository inventory.

## Why sbomb exists

The core question sbomb answers is:

> Which source files, headers, generated files, libraries, binaries, assets, and software components demonstrably contributed to this concrete build artifact?

The tool is intentionally evidence-based. It does not include files just because they exist on disk. A file is included only when there is a valid evidence chain to a final deliverable. That means unused sources, examples, tests, SDK trees, package-manager caches, and unrelated files are excluded unless they are proven to contribute to the shipped output.

## Primary use cases

### 1. CRA and BSI TR-03183 compliance

sbomb is designed to support compliance work under the EU Cyber Resilience Act and the BSI TR-03183 SBOM requirements. It emits CycloneDX 1.6 data with the required component metadata, dependency relationships, hashes, and evidence properties needed for downstream review and audit.

### 2. Embedded firmware and CMake projects

The tool targets CMake-based builds, including Ninja, Ninja Multi-Config, Unix Makefiles, and NMake Makefiles. It is especially useful for firmware, bootloader, and embedded software projects where build graphs are complex and the final product contains many generated and external inputs.

### 3. Accurate software provenance

Rather than scanning a repository and inferring dependencies, sbomb traces what actually reached the final artifact. This gives you a realistic SBOM for the built product and helps distinguish:

- used source and header files
- generated configuration and asset files
- link inputs, archives, and libraries
- external components and package metadata
- toolchain/runtime dependencies when relevant

### 4. Auditable build evidence

sbomb can expose the evidence graph and explain why a file or component is present in the SBOM. This is useful for engineering reviews, release sign-off, and dispute resolution when the build output needs to be traced back to the origin files.

### 5. Policy and review workflows

The tool can validate output against policy profiles, fail on stale build artifacts, missing hashes, missing suppliers, or review-required conditions, and emit machine-readable findings. This makes it useful in CI pipelines and controlled release processes.

## How sbomb works

sbomb follows a simple flow:

1. Identify the final deliverable(s) to analyze.
2. Read build metadata from the CMake File API, linker maps, dependency files, and compile evidence.
3. Build an evidence graph linking deliverables to linked objects, archives, source files, generated files, and headers.
4. Filter the graph to only those files with valid chain evidence.
5. Emit a CycloneDX SBOM and optional review report, findings, and evidence dump.

This is a deliberate contrast to broad source-tree scanning: repository presence is not evidence by itself.

## How to use sbomb

### Generate an SBOM

```bash
sbomb generate \
  --source-dir . \
  --build-dir build/debug \
  --config sbomb.json \
  --output build/debug/app.cdx.json \
  --policy strict
```

This produces a CycloneDX JSON SBOM for the configured artifact or discovered deliverable. In single-artifact mode, a single output file is generated; in assembly mode, the product is treated as a combined artifact set.

### Explain a file or component

```bash
sbomb explain --build-dir build/debug --file dep/mbedtls/include/mbedtls/aes.h
sbomb explain --build-dir build/debug --component mbedtls
sbomb explain --build-dir build/debug --bom-ref file:project:src/main.cpp
```

This prints the evidence chains connecting the selected item back to the final deliverable. It is useful when reviewing why a file appears in the SBOM or when debugging a missing or unexpected dependency.

### Validate an existing SBOM

```bash
sbomb validate --input build/debug/app.cdx.json
```

This checks whether the generated CycloneDX document conforms to the expected structure and policy expectations.

### Inspect evidence and schemas

```bash
sbomb evidence --build-dir build/debug --output evidence.json
sbomb schema --config
```

These commands expose internal evidence data or print the embedded configuration schema for the tool.

### Print version information

```bash
sbomb version
```

## CLI arguments and common flags

The `generate` command is the primary entry point. The most important arguments are:

- `--source-dir`: project source directory; defaults to the current directory.
- `--build-dir`: required build directory containing the CMake build output and generated metadata.
- `--config`: path to a sbomb JSON configuration file; defaults to `sbomb.json` if present.
- `--output`: output file path for a single artifact.
- `--output-dir`: output directory for multiple artifacts in single mode.
- `--mode`: selects `single` or `assembly` mode.
- `--config-name`: build configuration name for multi-config generators.
- `--policy`: policy profile such as `default`, `strict`, `lenient`, or a custom JSON profile.
- `--format`: output format; currently `cyclonedx-json`.
- `--spec-version`: CycloneDX version; currently `1.6`.
- `--map`: explicit linker map path.
- `--link-depfile`: linker dependency file path.
- `--compile-commands`: explicit `compile_commands.json` path.
- `--buildgraph`: explicit build graph file such as `build.ninja`.
- `--allow-introspection`: enables limited introspection commands for build metadata collection.
- `--allow-cmake-regenerate`: allows `cmake -S -B` regeneration when File API data is not yet available.
- `--reproducible`: omits timestamps for reproducible output.
- `--header-evidence`: chooses how header evidence is resolved: `dwarf-preferred`, `union`, or `depfiles`.
- `--waivers`: path to waiver definitions.
- `--log-level`: verbosity (`error`, `warn`, `info`, `debug`, `trace`).
- `--jobs`: number of worker threads.
- `--max-input-size`: maximum parser input size.

Other flags control inclusion policies such as system headers, toolchain runtime, linker scripts, assets, runtime libraries, and fail-on conditions. Flags that start with `--include-*` or `--fail-on-*` override the corresponding policy value.

## Configuration model

sbomb is configured through a JSON file that can define project metadata, build roots, artifact selection, anchors, policies, and output details. The configuration file is the primary way to define the final deliverables and to control what should be included or treated as review-required.

Example structure:

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
    "config": "Debug"
  },
  "mode": "single",
  "artifacts": [
    {
      "path": "build/debug/firmware.elf",
      "role": "application"
    }
  ],
  "policy": {
    "profile": "cra",
    "headerEvidence": "dwarf-preferred",
    "failOnStaleBuildArtifacts": true,
    "includeAssets": true
  },
  "output": {
    "format": "cyclonedx-json",
    "specVersion": "1.6",
    "reproducible": false,
    "hashAlgorithms": ["sha256"]
  }
}
```

### Key configuration sections

- `project`: identifies the product and root metadata.
- `build`: defines the build directory and selected build configuration.
- `mode`: selects single-artifact or assembly mode.
- `anchors`: maps named roots such as external dependency directories to stable anchor keys.
- `artifacts`: final deliverables to analyze.
- `discovery`: optimization and exclusion rules for automatically discovered build outputs.
- `components`: curated component metadata such as version, supplier, license, and upstream URL.
- `generators`: generated file mappings.
- `policy`: review policy, inclusion toggles, evidence handling, and fail conditions.
- `output`: output format and reproducibility options.

### Default behavior and constraints

- Final deliverables must be explicitly configured or deterministically discovered.
- Automatic discovery is only used when `artifacts` is absent or empty.
- If no deliverable is found, the run fails with `MISSING_FINAL_DELIVERABLE`.
- If a configured artifact does not exist, the run fails with `MISSING_ARTIFACT`.
- Multi-config builds require a selected configuration if more than one exists.
- Unknown keys in configuration are treated as an error, so typos cannot silently disable a policy gate.

## Output and review

sbomb writes CycloneDX JSON as its primary output and can additionally emit:

- a review report for human-readable audit output
- machine-readable findings
- an evidence graph dump
- documentation of unresolved or excluded chains

This makes it usable both for downstream software inventory and for internal engineering review before release.

## Typical workflow

1. Configure the project root, build directory, and final artifact(s).
2. Generate the SBOM from the build tree.
3. Review findings and evidence chains.
4. Validate the output against policy or compliance requirements.
5. Integrate the generation step into CI or release automation.

## Subcommands summary

- `sbomb generate`: produce SBOM(s) and evaluate policy
- `sbomb explain`: explain inclusion of a file or component
- `sbomb validate`: validate an generated CycloneDX SBOM
- `sbomb evidence`: dump evidence graph details
- `sbomb schema`: print embedded configuration and findings schemas
- `sbomb version`: print tool version information

For a complete description of the normative behavior, see [docs/dev/spec.md](docs/dev/spec.md).

## License

sbomb is distributed under the MIT license; see [LICENSE](LICENSE).

It has three third-party Go dependencies, all under permissive licenses
compatible with MIT, which is why `vendor/` is committed and redistributed with
the source; what each one is for is recorded in
[docs/dev/dependencies.md](docs/dev/dependencies.md). The SPDX license texts
that `internal/license` matches against are digests of the official SPDX list,
not copies of the texts, and are regenerated by `tools/spdxgen`.
