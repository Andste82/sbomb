# Configuration

sbomb runs without a configuration file. You add one when you need to name the
deliverable explicitly, curate component metadata for the CRA fields, give
external directories a portable identity, or pin a policy.

The file is JSON, `sbomb.json` by default, selected with `--config`. **Unknown
keys are an error at every level of the document**, so a typo cannot silently
disable a policy gate — `{"policy": {"failOnMisingHash": true}}` is refused,
not ignored.

```json
{
  "project": {"name": "app", "root": "."},
  "build": {"dir": "build"},
  "artifacts": [{"path": "build/app", "role": "application"}]
}
```

`build.dir` is the only required field, and `--build-dir` supplies it too.
Relative paths are resolved from `project.root`.

`sbomb schema` prints the current schema document.

## `project`

Describes the product the SBOM is about. These values become the root component.

| Field | Meaning |
|---|---|
| `name` | The product name. Optional — see below |
| `version` | Product version. Optional — see below |
| `supplier` | The supplier, which BSI TR-03183-2 requires |
| `license` | SPDX expression for the product itself |
| `type` | CycloneDX component type; derived from the artifact's role when absent |
| `root` | Source root; defaults to the current directory |

### Neither name nor version has to be written twice

The build system already states both, and sbomb reads them from the CMake File
API rather than making you repeat them. A configured value always wins; this
only fills a gap.

* **`version`** comes from `CMAKE_PROJECT_VERSION` — what `project(… VERSION …)`
  declared. The document then says where it came from, in
  `evidence.identity`, so a read version and a curated one are not the same
  claim.
* **`name`** comes from `CMAKE_PROJECT_NAME`, and only in `assembly` mode.
  In single-artifact mode the root component is the deliverable itself
  ([§6.1](dev/spec.md)), so the artifact's own name is used and the project's
  name would be the wrong answer.

Without a File API reply there is nothing to read — the bundled CMake module
files the query that produces one. Then `name` falls back to the deliverable,
and `version` is simply absent, which `UNKNOWN_VERSION` reports.

## `build`

| Field | Meaning |
|---|---|
| `dir` | The build directory; `--build-dir` overrides it |
| `config` | Which configuration to read for a multi-config generator, e.g. `Debug` |
| `introspection` | Which subprocess groups may run — see below |

sbomb runs no subprocesses at all unless you allow them, group by group:

```json
{"build": {"introspection": {"git": true, "ninja": true}}}
```

The groups are `cmake`, `ninja`, `git`, `osPackages` and `compiler`. Each
permits a small, fixed set of argument shapes and nothing else; a command line
is never assembled from a string. `--allow-introspection` turns them all on for
one run. Everything works without them — introspection only adds evidence that
would otherwise be missing.

## `artifacts`

The final deliverables. This is the entry point of the whole evidence chain.

```json
{
  "artifacts": [
    {
      "path": "build/debug/firmware.elf",
      "role": "bootloader",
      "map": "build/debug/firmware.map",
      "linkDepfile": "build/debug/firmware.d"
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `path` | Path to the built artifact |
| `role` | `application` (default), `bootloader`, `library`, `filesystem`, `image`, `package`, `data`, `other` |
| `map` | Linker map, when it is not beside the artifact |
| `linkDepfile` | Link dependency file, when it is not beside the artifact |

The role decides the CycloneDX type of the root component: `bootloader`,
`image` and `filesystem` produce `firmware`, `library` produces `library`,
`data` and `package` produce `file`, and everything else `application`. There
is no `firmware` role — it is what those three roles mean.

When `artifacts` is absent, sbomb asks the CMake File API which targets produce
artifacts and uses those. Discovery is deterministic: it never picks "the
newest" or "the largest" binary in the build directory. If nothing is found the
run fails with `MISSING_FINAL_DELIVERABLE`; if a configured artifact does not
exist, with `MISSING_ARTIFACT`.

`discovery.excludeTargetPatterns` narrows automatic discovery. It has no effect
on configured artifacts:

```json
{"discovery": {"excludeTargetPatterns": ["*test*", "*example*"]}}
```

## `mode`

`single` (default) expects exactly one deliverable. When automatic discovery
finds several, the run stops with `AMBIGUOUS_FINAL_DELIVERABLE` rather than
picking one.

`assembly` says that is expected — a firmware image alongside its bootloader —
and lets discovery proceed.

## `anchors`

Every file identity is an anchor plus a relative path, so that the same build
on two machines produces the same document. The project root, the build root,
the compiler installation and the sysroot are anchored automatically from what
CMake reports. Everything else you name:

```json
{
  "anchors": [
    {"key": "shared", "path": "/opt/shared"},
    {"key": "sdk:espidf", "path": "/opt/esp-idf"}
  ]
}
```

A bare key becomes an `extern:` anchor, so files under `/opt/shared` are
identified as `extern:shared:<relative path>`. A key that already names a kind
— `sdk:`, `pkg:`, `toolchain:`, `sysroot:`, `extern:` — is used verbatim.

Files under no anchor keep their absolute path and are reported as
`UNANCHORED_FILE`. `--redact-unanchored-paths` replaces those paths with a
digest, in the SBOM, the findings and the review report alike.

## `components`

Groups files into the components the SBOM reports, and supplies the metadata
compliance asks for. **Nothing here is guessed:** a version, supplier or licence
that no authorized source provides is reported as missing rather than inferred
from a directory name or a repository URL.

```json
{
  "components": [
    {
      "path": "dep/mbedtls",
      "name": "mbedtls",
      "type": "library",
      "version": "3.5.0",
      "supplier": "Trusted Firmware",
      "license": "Apache-2.0",
      "purl": "pkg:generic/mbedtls@3.5.0"
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `path` | Directory whose files belong to this component |
| `match` | Glob alternative to `path` |
| `targets` | CMake targets whose sources belong to this component |
| `name` | Component name |
| `type` | Component type |
| `version` | Version, as an assertion |
| `versionFrom` | Where to read the version instead — see below |
| `supplier` | Supplier |
| `license` | SPDX expression |
| `purl` | Package URL |

`path` selects a directory; `match` selects by glob instead. Use one or the
other.

`targets` selects by CMake target, which is what the build system itself says
rather than what the directory layout suggests:

```json
{
  "components": [
    { "targets": ["mbedcrypto", "mbedx509"], "name": "mbedtls", "license": "Apache-2.0" }
  ]
}
```

The sources of those targets become this component wherever they live, so a
dependency whose files are spread over several directories needs one entry
rather than one rule per directory. A source that two targets both compile is
left unmapped and falls through to the next strategy: the build system said two
things, and choosing one of them would be a guess. `path` still wins over
`targets` when both would claim a file.

`versionFrom` names authorized sources, tried in order:

| Rule | Reads |
|---|---|
| `"git"` | The nearest git tag in the component root |
| `"commit"` | The commit hash |
| `"header:<file>:<macro>"` | A version macro from a header, e.g. `"header:include/mbedtls/version.h:MBEDTLS_VERSION_STRING"` |

`git` and `commit` need the `git` introspection group.

Files are mapped to components in a fixed priority order: these curated entries
first, then package-manager metadata (vcpkg, Conan, FetchContent, git
submodules), then a configured CMake target, then the nearest ancestor directory
holding a package manifest (`conanfile.txt`, `vcpkg.json`, `idf_component.yml`,
`Cargo.toml`, `west.yml`) **or a licence file** (`LICENSE`, `LICENCE`,
`COPYING`), then the anchor root, and finally an explicit `unknown:` component
flagged for review. **A file is never dropped because its component could not be
determined.**

A directory carrying its own licence file is treated as a distinct component,
which is how a library copied into the source tree is recognized when it has no
package manifest. Your own top-level licence is not a boundary: the search
stops at the anchor root, so a dependency can never be given the project's
licence. `NOTICE` and `COPYRIGHT` mark nothing — they are attribution material
rather than a licence grant.

Every mapped component also has a **root**, published as
`sbomb:component:root`. It comes from the same strategy that named the
component: the configured `path`, the package-manager root, the directory a
marker file was found in, or the anchor root. Only when none of those applies is
it taken to be the deepest common directory of the files that were used, and
that case reports `COMPONENT_ROOT_UNRESOLVED` — a root derived from the used
files moves when the linker keeps a different set, and a licence or version read
from it would move with it.

## `manifests`

Extra package manifests to read for component identity, beyond those found
next to the files themselves:

```json
{"manifests": ["deps/conanfile.txt"]}
```

## `policy`

Policy has two separable halves. **Scope** decides what belongs in the
document; **gates** decide whether the run passes. Two profiles with the same
scope produce the same document — a verdict never changes the content.

```json
{
  "policy": {
    "profile": "cra",
    "headerEvidence": "dwarf-preferred",
    "includeAssets": true,
    "failOnStaleBuildArtifacts": true,
    "waiversFile": "sbomb-waivers.json"
  }
}
```

### Profiles

| Profile | Character |
|---|---|
| `lenient` | For an unprepared project: missing hashes, unresolved sources and stale artifacts do not fail |
| `default` | Fails on missing file hashes, unresolved sources of linked objects and stale artifacts |
| `cra` | The Cyber Resilience Act fields, gated |
| `strict` | Every gate on, header evidence `union`, linker scripts included |

`profileOverlay: "host-linux"` adds distribution libraries as separate
components — noise on an embedded target, correct on a host build.

### Gates

Each is a boolean and each has a `--fail-on-…` flag:

`failOnUnknownComponent`, `failOnUnknownLicense`, `failOnUnknownVersion`,
`failOnMissingSupplier`, `failOnMissingHash`, `failOnMissingComponentHash`,
`failOnMissingSourceForLinkedObject`, `failOnStaleBuildArtifacts`,
`failOnReviewRequired`, `failOnWeakEvidence`, `failOnMissingHeaderEvidence`,
`failOnUnanchoredFile`, `allowMissingLinkEvidence`.

### Scope

| Setting | Values | Default |
|---|---|---|
| `headerEvidence` | `dwarf-preferred`, `union`, `depfiles` | `dwarf-preferred` |
| `includeSystemHeaders` | boolean | `false` |
| `includeToolchainRuntime` | `separate-component`, `report-only`, `exclude` | `separate-component` |
| `includeLinkerScripts` | boolean | `false` |
| `includeGeneratedIntermediateFiles` | boolean | `false` |
| `includeAssets` | boolean | `true` |
| `includeTransientBuildArtifacts` | boolean | `false` |
| `systemLibraries` | `exclude`, `separate-component`, `report-only` | `exclude` |
| `pchHeaders` | `include`, `exclude`, `annotate-only` | `include` |
| `sectionGarbageCollection` | `ignore`, `annotate`, `exclude` | `ignore` |
| `prebuiltLibrariesRequireMapping` | boolean | `true` |
| `staleToleranceSeconds` | integer | `5` |

`severityOverrides` maps a finding identifier to `error`, `warning` or `info`
when your process disagrees with the default severity.

### Precedence

Command line, then profile, then this file, then the built-in default. Because
every gate in the file is a tri-state, a configuration can turn a profile's
gate *off* as well as on — and `--fail-on-x=false` can do the same from the
command line.

## Waivers

A finding you have judged and accepted belongs in a waiver, with a reason and
an expiry — not in a permanently loosened gate.

```json
[
  {
    "id": "UNKNOWN_LICENSE",
    "subject": "component:go/example.com/one",
    "reason": "Vendor confirmed BSD-3-Clause by mail, ticket SEC-412",
    "approvedBy": "a.steinbart",
    "expires": "2027-01-31"
  }
]
```

`id` accepts `*` for any finding. An expired waiver stops suppressing and is
reported as `WAIVER_EXPIRED`; one that matches nothing is reported as
`WAIVER_UNUSED`, so the file cannot quietly rot.

Select it with `--waivers <file>` or `policy.waiversFile`.

## `output`

| Field | Values | Default |
|---|---|---|
| `format` | `cyclonedx-json` | `cyclonedx-json` |
| `specVersion` | `1.6`, `1.7` | `1.6` |
| `tlp` | `CLEAR`, `GREEN`, `AMBER`, `AMBER_AND_STRICT`, `RED` | none |
| `reproducible` | boolean | `false` |

`reproducible` omits the timestamp and derives the serial number from the
document's content, so two runs over the same evidence produce the same bytes.
It belongs here as well as on the command line because it is a property of the
project: a project whose SBOMs have to be comparable wants that of every run,
not only of the ones where somebody remembered `--reproducible`. The flag still
forces it on for a single run, and neither can turn the other off.

`format` and `specVersion` are checked against the values the writer registry
actually offers, so a file asking for something that does not exist says so
rather than being ignored. The values in the table above are the ones
`sbomb schema` publishes.

### Choosing a specification version

`specVersion` selects the CycloneDX revision. **1.6 is the default and stays
the default**: BSI TR-03183-2 names it as the minimum, and nothing in 1.7
changes that. Write 1.7 when a consumer asks for it.

`--spec-version` on `generate` and `self` overrides this for a single run;
`sbomb schema --cyclonedx --spec-version 1.7` prints the schema a 1.7 document
is checked against.

The two documents describe the same evidence and differ only in saying so: the
`specVersion` field, the `sbomb:run:specVersion` property that records it, and
the serial number, which is a digest of the canonical document and therefore
moves once anything in it does. A document written at 1.6 is structurally valid
at 1.7 with only `specVersion` changed — 1.7 is additive over 1.6, with nothing
removed and the same required top-level fields.

Four differences are 1.7-only, and each appears only when the evidence calls
for it:

| 1.7 | At 1.6 instead |
|---|---|
| `component.evidence.licenses` may mix SPDX expressions with licence identifiers — see [D19](dev/deviations.md) | licence identifiers only |
| `component.isExternal` marks a library the environment provides | `sbomb:component:scope` and the build-environment grouping |
| `sbomb:component:vcsCommit` / `vcsDirty` sit on the repository reference they qualify | the same properties on the component |
| `metadata.distributionConstraints.tlp`, when `tlp` is set | setting `tlp` is refused, rather than silently dropped |

Everything else is written identically at both versions.

### The repository URL, and where things live

Where CycloneDX specifies a field for something sbomb records, the specified
field carries it. The repository URL is therefore an `externalReferences` entry
of type `vcs` rather than a `sbomb:component:vcsUrl` property — and because that
reference type predates 1.6, **that change applies to 1.6 documents too**.

### `tlp`

`tlp` marks how widely the document may be shared, using the Traffic Light
Protocol. Nothing infers it: a TLP marking states what the recipient may do,
which is not something a tool concludes from how the document was produced —
`--redact-unanchored-paths` says paths were removed and nothing more. There is
no default either. An absent constraint means sbomb was not told, not that the
document may be shared freely.

`sbomb validate` needs no version: it reads what the document declares and
checks it against the matching embedded schema, including a 1.7 document
written by another tool. A version this build has no schema for is refused
rather than checked against the wrong one.

## A worked example

```json
{
  "schemaVersion": 3,
  "project": {
    "name": "example-firmware",
    "type": "firmware",
    "version": "1.4.2",
    "supplier": "Example Org",
    "license": "Proprietary",
    "root": "."
  },
  "build": {"dir": "build/debug", "config": "Debug"},
  "mode": "single",
  "artifacts": [
    {"path": "build/debug/firmware.elf", "role": "image"}
  ],
  "anchors": [
    {"key": "sdk:vendor", "path": "/opt/vendor-sdk"}
  ],
  "components": [
    {
      "path": "dep/mbedtls",
      "name": "mbedtls",
      "version": "3.5.0",
      "supplier": "Trusted Firmware",
      "license": "Apache-2.0"
    },
    {
      "path": "src/bootloader",
      "name": "bootloader",
      "versionFrom": ["git"]
    }
  ],
  "policy": {
    "profile": "cra",
    "includeAssets": true,
    "waiversFile": "sbomb-waivers.json"
  }
}
```
