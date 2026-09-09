# Configuration

sbomb runs without a configuration file. You add one when you need to name the
deliverable explicitly, curate component metadata for the CRA fields, give
external directories a portable identity, or pin a policy.

The file is JSON and is loaded when selected with `--config <file>`. There is
no default configuration file. **Unknown keys are an error at every level of
the document**, so a typo cannot silently
disable a policy gate — `{"policy": {"failOnMisingHash": true}}` is refused,
not ignored.

```json
{
  "project": {"name": "app", "root": "."},
  "artifacts": [{"path": "build/app", "role": "application"}]
}
```

`build.dir` is not required: `--build-dir` says which directory to read, and a
configuration shared across `build/debug`, `build/release` and the rest should
not have to name whichever one is current. Leave it out unless you need what it
actually does — see below.

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
| `dir` | What the build root is **called** — not where it is read from. Optional |
| `config` | Which configuration to read for a multi-config generator, e.g. `Debug` |
| `introspection` | Which subprocess groups may run — see below |

`dir` and `--build-dir` are not two ways to say the same thing. `--build-dir`
is where sbomb reads; `dir` is the name every file identity under the build
root is anchored against.

Left out, that name comes from the CMake File API — the build system's own
answer, and the portable one. Set it and you override that, so a value that
does not match what the evidence records will re-anchor files and change which
of them end up in the document. Set it only when the build directory has moved
since it was built and you need to name the root the evidence knows.

Relative paths are resolved from `project.root`.

sbomb runs no subprocesses at all unless you allow them, group by group:

```json
{"build": {"introspection": {"git": true}}}
```

`--allow-introspection` turns every group on for one run. Everything works
without them: introspection only adds evidence that would otherwise be missing,
and an adapter that is not allowed to ask degrades and says so in a finding.

Each group permits a fixed set of argument shapes and nothing else. There is no
shell, and no command line is ever assembled from a string — the paths are
filled into fixed slots and must lie inside a registered anchor. The one
exception is the compiler a probe runs: it is the program, not an argument, and
it is whatever the build evidence named, so it must exist but need not be
anchored.

| Group | What it may run | What it buys |
|---|---|---|
| `git` | `rev-parse HEAD`, `describe --tags --always --dirty`, `config --get remote.origin.url`, each with `-C <dir>` | The version, commit, repository URL and dirty state of a dependency fetched by `FetchContent` or checked out as a submodule, and the `"git"` and `"commit"` rules of `components[].versionFrom`. Without it those components have no version and `UNKNOWN_VERSION` says so |
| `ninja` | `-C <build-dir> -t commands <target>`, `-t inputs <target>` | Two fallbacks: the compile lines when there is no `compile_commands.json`, and the objects an archive was built from when `build.ninja` does not say. Both read `build.ninja` including the `include`/`subninja` files sbomb's own parser does not follow |
| `compiler` | `<compiler> --version`, `-dumpmachine`, `-print-search-dirs` | The compiler's installation directory as a toolchain anchor and the implicit link directories, when the File API reported no toolchain. Not the implicit *include* directories: `-print-search-dirs` names none, so those still come from the File API alone |

**A command is always the second source.** Each group replaces a file, and it is
asked only when that file is missing or cannot be read; with the file in place
nothing is executed, whatever you enabled. When the file is missing and the
group is off, the run says which evidence it did not get:

| Missing file | Group that replaces it | Finding when it is off |
|---|---|---|
| `compile_commands.json` | `ninja` | `MISSING_COMPILE_EVIDENCE` |
| an archive's inputs, which `build.ninja` may hide behind a `subninja` | `ninja` | `ARCHIVE_MEMBERS_UNRESOLVED` |
| CMake File API `toolchains-v1` | `compiler` | `TOOLCHAIN_LAYOUT_UNKNOWN` |

One gap has no group behind it. A Ninja deps log that cannot be read is
reported as `NINJA_DEPS_UNAVAILABLE`, and nothing can be enabled to recover it:
`ninja -t deps` reads the same file and rewrites it when it cannot — measured
against ninja 1.11, which truncates a damaged log and deletes one whose header
it does not accept. sbomb reads a build directory; it does not repair one. The
log is written while ninja builds, so building again is what brings it back.

Commands the specification lists that are gone rather than idle: the whole
`cmake` group, `ninja --version`, `ninja -t deps` and `git status --porcelain`.
Deviation D29 records what was measured about each. The `osPackages` group went
the same way, with `dpkg -S <path>` and `rpm -qf <path>`: there is no
system-library adapter, and those shapes could not feed one — `dpkg -S` names a
package and an architecture, never a version and never a supplier, and the
system path it would be asked about lies outside every anchor. A distribution
library therefore still reaches the document with `UNKNOWN_VERSION`,
`MISSING_SUPPLIER` and `UNKNOWN_PURL`, which is what it did before. Deviation
D30 records the measurement and the three conditions for the group's return.

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

Leave `map` and `linkDepfile` out and sbomb looks beside the artifact. Name
them and it looks only there: a path you wrote down is a statement, not a hint,
and reading some other file instead would put evidence in the document you did
not ask for. A named file that is not there stops the run with
`CONFIGURED_EVIDENCE_MISSING`. `--map` and `--link-depfile` do the same from the
command line.

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
| `"git"` | `git describe --tags --always --dirty` in the component root. A leading `v` is dropped, and a `-dirty` tree also reports `VCS_DIRTY`. An exact tag on a clean tree is high confidence; a tag with distance, a dirty tree, or a bare commit abbreviation is medium |
| `"commit"` | `git rev-parse HEAD` in the component root, written as `0.0.0-git.<first twelve characters>`. A commit names the content, not a release, so it is never tried unless the rule asks for it |
| `"header:<file>:<macro>"` | A version macro from a header, e.g. `"header:include/mbedtls/version.h:MBEDTLS_VERSION_STRING"`. Reads a file, needs no introspection |

`git` and `commit` need the `git` introspection group. Without it neither rule
is attempted, no version is invented, and `UNKNOWN_VERSION` says that the rule
named git and git was never asked. The component root must also lie inside a
registered anchor, because that is where a path handed to a subprocess is
allowed to point.

Files are mapped to components in a fixed priority order: these curated entries
first, then package-manager metadata (vcpkg, Conan, FetchContent and CPM.cmake,
the ESP-IDF component manager, git submodules), then a configured CMake target,
then the nearest ancestor directory holding a package manifest (`conanfile.txt`, `vcpkg.json`, `idf_component.yml`,
`Cargo.toml`, `west.yml`) **or a licence file** (`LICENSE`, `LICENCE`,
`COPYING`), then the anchor root, and finally an explicit `unknown:` component
flagged for review. **A file is never dropped because its component could not be
determined.**

Package-manager metadata answers in two ways, the precise one first. Where a
manager wrote down which files it installed — vcpkg keeps such a list per
package — the file is looked up in that record; only otherwise is its path
matched against the package's directories. A package may have more than one:
FetchContent puts the checkout in `_deps/<name>-src` and everything CMake
generated for the package in `_deps/<name>-build`, and both belong to it. A file
that two packages claim belongs to neither and falls through to the strategies
below.

CPM.cmake is read through FetchContent rather than beside it, because that is
what CPM does: it calls FetchContent, so the checkout and the build tree are
FetchContent's and the same package is found either way. What the lock CPM
writes into your build directory adds is the version — `1.2.0` as CPM recorded
it, rather than whatever the git tag happened to be, which for a dependency
pinned to a commit was a forty-character hash — the repository, and a
`detectedBy` that says `cpm`. Nothing else moves: no file joins the used set
because the lock mentions it, no component boundary shifts, and a lock entry
whose `_deps` directory is not there produces no component at all. The copy of
the lock you commit to your source tree is not read; the one in the build
directory is, because that is the one your build actually produced.

A dependency a manager installed but nothing linked is not part of your product
and is not in the document. That is not silent: it reports `PACKAGE_NOT_LINKED`
(info), which fails nothing and names what was left out.

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

## `distroManifests`

The manifest an embedded-Linux distribution build wrote about the image it
produced: a Yocto `license.manifest` out of the deploy directory, or a
Buildroot `legal-info/manifest.csv`. Relative paths are resolved against the
project root, so the deploy directory beside the build tree is named once:

```json
{"distroManifests": ["../deploy/licenses/core-image-minimal/license.manifest"]}
```

For an image that is the most complete answer there is — name, version and
licence for every package, written by the build itself — and sbomb uses it for
exactly one thing: describing components your build already reached. **It never
adds one.** A manifest with 800 packages against a build that links three
libraries produces the same three components, the same files and the same
relations as a run without it; the three that appear in the manifest gain a
version and a licence, and the other 797 entries add no component, no file and
no relation. Being in the image is no evidence that this build linked anything.
The only thing a manifest reports by itself is a contradiction it states: one
package name given two different versions describes nothing and is reported once
as `COMPONENT_MAPPING_CONFLICT`. A Yocto recipe whose packages carry different
licences — `LICENSE:${PN}`, ordinary in a real image — contradicts nothing, so
the recipe name describes nothing there and no finding is written.

Matching is by name: the component name (`sbomb:component:root` shows where it
came from) against the manifest's package name and, for Yocto, its recipe name.
Nothing is matched by path or by resemblance, so a component whose name differs
from the distribution's is not described until you say so with
`components[].name`. What the manifest states loses to what you configured, to
an SBOM the dependency ships and to the package manager that installed it,
because it describes the image and they describe the package.

Point it at the target manifest. Buildroot's `legal-info/host-manifest.csv`
describes the tools that ran on the build machine, and its versions are not the
versions in your image. A path that does not exist is reported as
`MISSING_PACKAGE_EVIDENCE` and the run goes on; a file that is neither format,
or whose structure breaks part of the way through, is refused whole with
`EVIDENCE_UNREADABLE`, because half a manifest would describe some components
out of the image and leave the rest with nothing to say which is which.

The flag is `--distro-manifest`, repeatable. It is a different setting from
`manifests`: that one is sbomb's own JSON manifest format, and a Yocto file
listed there is reported as unreadable evidence.

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

Four profiles. `strict` is the engineering profile, `cra` the compliance one —
`cra` is deliberately **not** the strictest: it insists on the fields the
regulation asks for and tolerates weak evidence, because the CRA is about
documenting components rather than proving build provenance. A project can run
both in CI and gate the release on `cra` alone.

Only the rows that differ are listed; everything else is the same in all four.

| | `lenient` | `default` | `cra` | `strict` |
|---|:--:|:--:|:--:|:--:|
| **Fails on** | | | | |
| missing file hash | – | ✓ | ✓ | ✓ |
| object with no source | – | ✓ | ✓ | ✓ |
| stale build artifacts | – | ✓ | ✓ | ✓ |
| unknown component | – | – | ✓ | ✓ |
| unknown licence | – | – | ✓ | ✓ |
| unknown version | – | – | ✓ | ✓ |
| missing supplier | – | – | ✓ | ✓ |
| missing component hash | – | – | ✓ | ✓ |
| review required | – | – | – | ✓ |
| weak evidence | – | – | – | ✓ |
| missing header evidence | – | – | – | ✓ |
| unanchored file | – | – | – | ✓ |
| no link evidence at all | – | ✓ | ✓ | ✓ |
| **Content** | | | | |
| `headerEvidence` | dwarf-preferred | dwarf-preferred | dwarf-preferred | **union** |
| `includeToolchainRuntime` | report-only | separate-component | separate-component | separate-component |
| `includeLinkerScripts` | – | – | – | ✓ |
| `sectionGarbageCollection` | ignore | ignore | ignore | **annotate** |
| `prebuiltLibrariesRequireMapping` | – | ✓ | ✓ | ✓ |

The four *Content* rows are why two profiles can produce different documents.
Everything above them only decides whether the run passes.

**Start with `lenient`.** A first run on an unprepared project has findings —
that is the tool working. Work upwards from there.

`profileOverlay: "host-linux"` changes one thing: `systemLibraries` becomes
`separate-component`, so distribution libraries appear in the document. Noise on
an embedded target, correct on a host build. Combine it, do not replace:
`--policy cra --profile-overlay host-linux`.

Turning it on is also what makes distribution libraries *nameable*. With
`systemLibraries` at its default, they never reach the document, so nothing
describes them. With `separate-component` — or `includeSystemHeaders: true` for
headers — sbomb reads the `pkg-config` file the distribution installed beside a
library it linked (`<libdir>/pkgconfig/<name>.pc`) and gives that library a
component of its own, named after the pkg-config module and carrying the version
the file states, instead of folding every system file into one component named
after the sysroot. No process is run for this and no permission is needed.

It works where the .pc file is named after the library: `libfoo.so.3` finds
`foo.pc` or `libfoo.pc`, and a header finds the .pc file named after the
directory it sits in. Nothing is guessed — `libz.so` does not find `zlib.pc`,
and a library whose .pc file says it lives elsewhere is not claimed by it. Such
a library stays in the sysroot component, exactly as before. A .pc file names no
licence and no distribution package, so `UNKNOWN_LICENSE` and `UNKNOWN_PURL`
remain for these components; only introspection can close that.

### Gates

Each is a boolean with a matching `--fail-on-…` flag. Each names the findings
that make the run fail.

| Gate | Fails when |
|---|---|
| `failOnUnknownComponent` | a used file could not be mapped to any component |
| `failOnUnknownLicense` | a component's licence is NOASSERTION |
| `failOnUnknownVersion` | no authorized source supplied a component's version |
| `failOnMissingSupplier` | a component has no supplier — a BSI TR-03183-2 field |
| `failOnMissingHash` | a used file could not be read, so it has no hash |
| `failOnMissingComponentHash` | a component has no hashable file at all |
| `failOnMissingSourceForLinkedObject` | an object could not be traced back to a source |
| `failOnStaleBuildArtifacts` | the evidence does not describe the artifact that is there |
| `failOnReviewRequired` | something needs a human: a licence conflict, a dirty tree, an unclassified header |
| `failOnWeakEvidence` | a file rests only on a textual fallback, or LTO left no usable chain |
| `failOnMissingHeaderEvidence` | a translation unit contributed no header evidence |
| `failOnUnanchoredFile` | a file matched no anchor and would carry an absolute path |
| `allowMissingLinkEvidence` | inverted: **permits** a run in which no link evidence source succeeded |

`severityOverrides` maps a finding identifier to `error`, `warning` or `info`
when your process disagrees with the default severity.

### Scope

These decide what is in the document. Changing one changes the output, so two
runs that differ here are not comparable.

| Setting | Values (default first) | What it changes |
|---|---|---|
| `headerEvidence` | `dwarf-preferred`, `union`, `depfiles` | Which headers count as used. DWARF knows which ones emitted code; a dependency file also lists those an `#ifdef` skipped |
| `includeSystemHeaders` | `false`, `true` | Headers from the sysroot and the compiler's own directories |
| `includeToolchainRuntime` | `separate-component`, `report-only`, `exclude` | `libgcc`, `libstdc++` and friends: their own component, a finding only, or nothing |
| `includeLinkerScripts` | `false`, `true` | Linker scripts and memory layout files |
| `includeGeneratedIntermediateFiles` | `false`, `true` | Files a generator produced only to be consumed again |
| `includeAssets` | `true`, `false` | Fonts, images and blobs that packaging embedded |
| `includeTransientBuildArtifacts` | `false`, `true` | Objects and archives that exist only during the build |
| `systemLibraries` | `exclude`, `separate-component`, `report-only` | Libraries the distribution provides |
| `pchHeaders` | `include`, `exclude`, `annotate-only` | Headers that arrived through a precompiled header |
| `sectionGarbageCollection` | `ignore`, `annotate`, `exclude` | Objects the linker discarded with `--gc-sections`: keep them, mark them, or drop them |
| `prebuiltLibrariesRequireMapping` | `true`, `false` | Whether a prebuilt library without a component mapping is an error |
| `staleToleranceSeconds` | `5` | How much clock skew counts as "not stale" |

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
