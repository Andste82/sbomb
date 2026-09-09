# Changelog

## 0.17.0

### MSVC and Visual Studio builds are supported

MSVC builds using Ninja or Ninja Multi-Config are supported through compiler,
Ninja dependency and linker-map evidence. Visual Studio 17 2022 builds are
supported through MSBuild `.tlog` evidence, including object/source mappings,
headers and link inputs. Use `--config-name` for a multi-config build and
`--path-flavor windows` when analyzing Windows evidence from Linux.

PDB parsing remains out of scope. MSVC header evidence therefore comes from
`/showIncludes`, Ninja dependencies or MSBuild TLogs, and a build may report
`DEBUG_INFO_UNAVAILABLE` where a DWARF-based build would provide debug evidence.

### A Zephyr workspace is read from the manifest west wrote

Until now a Zephyr build hid its modules inside the application. A driver out of
`modules/hal/nordic` got no component of its own at all: its files were counted
as part of the component the application anchor stands for, so there was no
name, no version, no repository and no purl telling that module apart from the
code that linked it. The `west.yml` marker sbomb already knew could not help,
because a workspace keeps its manifest at `<topdir>/zephyr/west.yml`, which lies
above no module a build links — the marker fired only for a file inside the
manifest repository itself, and such a file arrived under that repository's
directory name and nothing more.

sbomb reads the manifest now. It finds the workspace by its `.west` directory,
walking up from your sources and looking nowhere else, reads the manifest that
workspace names, and takes from it what west itself was told: the project's
name, the directory it was cloned into, the revision that was asked for and the
repository it came from. A project that ships a `zephyr/module.yml` is named the
way the module names itself. The component carries `west` beside its version, so
the document says where the answer came from, and a `pkg:generic` purl with the
repository and, where there is one, the commit.

**A project pinned to a commit gets no version, deliberately.** Zephyr manifests
pin most projects to a full SHA, and a commit is not a version: it goes into the
purl and into `vcsCommit`, and the component keeps `UNKNOWN_VERSION`. Where you
allow git introspection the checkout answers instead of the manifest — but only
for a project inside the trees sbomb is reading, which in the usual Zephyr
layout, with the workspace above your application, is none of them.

**Two honest limits.** A manifest that `import`s another repository's manifest
is followed only as far as it states projects itself: west resolves an import in
memory and writes nothing down, so a workspace built that way lists fewer
projects than you may expect. And a real manifest declares dozens of projects
while a build links a handful, so the report grows one `PACKAGE_NOT_LINKED`
(info) for each project nothing linked — the same thing vcpkg has always said,
and correct: a project that was cloned but never linked is not part of your
product, so it is not in the document.

### An MCU vendor pack carries a version and a supplier at last

A CMSIS pack is the way ARM silicon vendors ship their code, and until now a
pack your build linked arrived in the document with its directory name and
nothing else: no version, no supplier, no licence anybody could state. The
answer was in the box the whole time. Every pack carries a `.pdsc` descriptor
naming the vendor, the pack and its release history.

sbomb reads it now. A pack whose directory is already a component — because a
licence file, a manifest or a package manager settled it — gets the version of
its newest release and the vendor as its supplier, with `cmsis-pack` beside the
version so the document names where the answer came from. Where the pack sits in
the layout a pack installer writes, `ARM/CMSIS/5.9.0/ARM.CMSIS.pdsc`, the
version on disk is used and the one the release history declares is kept as the
origin that lost. Nothing is executed and no file is fetched: the descriptor is
read, and that is all.

**No purl comes with it, and that is deliberate.** There is no registered
package-URL type for CMSIS packs, so anything sbomb wrote there would be an
identifier no tool can resolve. These components keep `UNKNOWN_PURL`. The
descriptor's `<license>` names a *file* inside the pack rather than an SPDX
expression, so no licence is taken from it either — the licence keeps coming
from the licence file at the pack root, as before.

**Nothing is discovered by this, and the coverage is honestly partial.** A pack
your build never linked is still absent from the document, and it is not
reported either: nothing here installs or enumerates a pack, and no pack cache
is searched for. The descriptor is read only in a directory something else has
already settled as a component — a pack directory holding nothing but its
`.pdsc` is still read by nobody, because the file's name is the vendor's and
there is no fixed name to look for.

### A distribution library is named after its package, not after the sysroot

A host build links `libfoo.so.3` out of `/usr/lib`, and until now every system
file of the run arrived in one component named after the sysroot, with no
version anybody could state for it. The distribution had written the answer down
right beside the library: `<libdir>/pkgconfig/libfoo.pc`, naming the module and
its version.

sbomb reads it now. A system library your build actually linked gets a component
of its own, named after the pkg-config module, carrying the version the file
states and saying `pkg-config` beside it so the document names where the answer
came from. It stays under `build-environment` and it stays system scope: naming
a distribution library does not turn it into one of your product's dependencies.
No process is started for this and no permission is needed — the file is read,
nothing is executed.

**This only matters where distribution libraries are in the document at all.**
The default is `systemLibraries: exclude`, and there they are filtered out before
components are formed, so not one .pc file is opened and your document is byte
for byte the one you had. Run `--profile-overlay host-linux`, set
`systemLibraries` yourself, or turn on `includeSystemHeaders`, and the new
naming appears — which is a visible change to those documents: what was one
component per sysroot becomes one per named package.

**Nothing is guessed, and the coverage is honestly small.** The .pc file is
addressed by a name derived from the used file — `libfoo.so.3` asks for `foo.pc`
and `libfoo.pc`, a header asks for the .pc file named after the directory it sits
in — and no `pkgconfig` directory is ever listed. So `libz.so` does not find
`zlib.pc` and `libssl.so` does not find `openssl.pc`; those stay where they were.
A file that is found must also *verify*: it has to say the library lies in the
directory it really lies in and name it in its `Libs:` line, or the mapping does
not happen. Two .pc files that both verify and disagree map nothing and report
`COMPONENT_MAPPING_CONFLICT` — two statements are no statement. Two *packages*
of one module name in one sysroot — a distribution `libfoo` in `/usr/lib` and a
hand-built one in `/opt/lib` — are that same rule one level up: both libraries
belong in the component, and it carries the version of neither, with the
disagreement reported against it. A .pc file names
no licence, so `UNKNOWN_LICENSE` stays with these components, and it names no
distribution package, so no purl is invented for them. And a .pc file for a
library your build never linked adds nothing at all: no component, no file, not
one finding.

### Yocto and Buildroot describe the components your image build already knows

An embedded-Linux image build writes down what it built: Yocto a
`license.manifest` in the deploy directory, Buildroot a
`legal-info/manifest.csv`. Name, version and licence for every package, from the
build itself — and sbomb walked past both, so a library that arrived through the
distribution reached the document with its directory name and nothing else.

Name one with `distroManifests` in the configuration or `--distro-manifest` on
the command line, and every component your build actually used that the manifest
names arrives with that version and that licence, saying `yocto` or `buildroot`
beside it so the document names the file the answer came from. Relative paths
are resolved against the project root, which is what lets one configuration name
a deploy directory that sits outside the build tree.

**The manifest describes your image; it does not decide what is in your
document.** An 800-package manifest against a build that links three libraries
produces exactly the components, files and relations it produced before —
three of them are described, the other 797 entries add no component, no file, no
relation and not one `PACKAGE_NOT_LINKED`. Being installed in an image is no
evidence that anything linked it, which is the rule this whole tool is built on.
The one thing a manifest reports on its own is a contradiction it states itself:
one package name given two versions describes nothing and says so once. A Yocto
recipe whose packages carry different licences — what `LICENSE:${PN}` does — is
no contradiction, so the recipe name simply describes nothing there and stays
silent. Matching is by name only, against the package name and, for Yocto,
the recipe name; nothing is matched by path or by resemblance, because a wrong
match publishes a wrong version with high confidence.

What the manifest says loses to your configuration, to an SBOM a dependency
ships with itself, to the checkout, and — at equal standing — to the package
manager that installed the package, because that one put it there. It ranks as
installed state, which means it outranks a manifest that only declares what was
asked for: a project that configures both an image manifest and, say, an
ESP-IDF component manifest will see the image build's answer win. Every claim
that loses is kept rather than dropped.

Both formats are read strictly. The CSV is parsed as CSV and not split on
commas, so a licence expression like `GPL-2.0+, LGPL-2.1+ with exceptions`
arrives whole, and a row with a field too few refuses the whole file instead of
quietly shifting every column after it. A manifest that breaches a parser bound
is refused whole with `INPUT_LIMIT_EXCEEDED`, one that is neither format with
`EVIDENCE_UNREADABLE`, and a configured path that is not there with
`MISSING_PACKAGE_EVIDENCE` — a typo has to be visible. Two entries that state
different versions for one name describe nothing at all and say so. Configure no
manifest and nothing at all happens: no file is opened and the document is the
one you had.

### CPM.cmake dependencies carry the version CPM recorded

CPM.cmake writes a lock file into your build directory while it configures —
`cpm-package-lock.cmake`, with the name, version and repository of every package
it fetched. sbomb walked past it. CPM drives FetchContent, so the dependencies
were found either way, but their version came from the git tag in the script
CMake generated: a package asked for by tag published that tag, and a package
pinned to a commit published a forty-character hash as its version.

The lock is read now. A CPM dependency whose files your build actually used
arrives with the version the lock recorded, says `cpm` beside it so the document
names the file the answer came from, and takes the repository from the lock where
the populate script named none. `sbomb:component:detectedBy` now says `cpm` for
these packages instead of `fetchcontent`, which is the manager your project
actually uses. Where git introspection is enabled the checkout still outranks
both — it says what is there, the lock says what was asked for — and every
claim that loses is kept rather than dropped.

Nothing about the rules changed. CPM gets no adapter of its own, so a CPM project
produces exactly the components it did before and not one
`COMPONENT_MAPPING_CONFLICT` more; no file joins the used set because the lock
mentions it, no component boundary moves, and a lock entry with no directory
under `_deps` produces no component. A dependency CPM fetched that nothing links
is still absent and still reported as `PACKAGE_NOT_LINKED`, naming `cpm` now.

CMake is not interpreted for this. One file is opened, in the build directory,
and a deliberately narrow reader takes seven keys out of it and ignores every
other line; the copy of the lock committed to a source tree is not read, because
its path is free and its content may be older than the build. A lock that
breaches a parser bound of §30 is refused whole with `INPUT_LIMIT_EXCEEDED`, and
one whose structure is broken with `EVIDENCE_UNREADABLE` — in both cases nothing
at all is taken from it. A missing lock, an empty one, and a package CPM wrote
into it as a comment are all silent, because none of them is a fault.

### ESP-IDF components you pulled in are now components in the document

The ESP-IDF component manager writes down exactly what your project depends on —
`dependencies.lock` names every component it resolved with its version, and each
component it unpacked into `managed_components/` carries its own manifest with
its licence. sbomb walked past both. A project built on the component manager
therefore had its components mapped by whatever generic rule happened to catch
them, or not at all.

They are read now. A managed component whose files your build actually used
arrives with the version the lock recorded, the licence the component declares
about itself, the repository it names, and a purl of the form
`pkg:idf/espressif/led_strip@2.5.3`. The version says `idf` beside it, so the
document names the file the answer came from. Where the lock and the component's
own manifest disagree, the lock wins — it says what was installed, the manifest
says what the component claims to be — and the losing claim is kept rather than
dropped.

Nothing about the rules changed. No file joins the used set because a lock file
mentioned it, no component boundary moves, and a component that was downloaded
but never linked is absent from the document and reported as
`PACKAGE_NOT_LINKED`, as before. The IDF's own `components/` tree is not touched:
those are part of the framework, not packages the manager installed. Two known
paths are opened and no directory is searched.

A component that a `.gitmodules` line and the component manager both claim now
belongs to the component manager, and the submodule's claim is reported as
`COMPONENT_MAPPING_CONFLICT` instead of winning. A component the manager
resolved whose directory is not there is `MISSING_PACKAGE_EVIDENCE` naming the
directory that was looked for. Without a lock file the unpacked directories still
answer, one rank weaker and saying so; without either, sbomb stays silent.

No YAML library was added for this. The two files are read by a deliberately
small reader with a written list of what it refuses, and a file it does not fully
understand supplies nothing at all rather than something partial —
`EVIDENCE_UNREADABLE` naming it, or `INPUT_LIMIT_EXCEEDED` when it breaches a
parser bound. This is the component manager only; the ESP-IDF SDK adapter,
with `project_description.json` and `sdkconfig`, is still parked.

### A dependency that ships its own SBOM is now read from it

A `sbom.cdx.json` or `sbom.spdx.json` at the root of a dependency is the best
evidence about that dependency there is. Everything the CRA asks for is in it
already — version, licence, supplier, purl — put there by the people who ship
the package and checked by their tooling, instead of by our reading of somebody's
manifest format. Until now sbomb walked past it.

It is read now, and it ranks accordingly: above every manifest and above what the
package manager that installed the dependency recorded, below what your
configuration says and below the checkout itself — a document states what was
shipped, a checkout shows what is there. The claim it displaces is kept rather
than dropped, and the version it supplies is published with `bundled-sbom` beside
it, so the document says which file the answer came from.

Only the one component the document is about is read: `metadata.component` in
CycloneDX, the package `documentDescribes` names in SPDX. The dependencies such a
document lists are ignored, and that is the point — a dependency list is no
evidence that anything was linked, and nothing enters your SBOM because a file
mentioned it. No file joins the used set, no component boundary moves, and a
dependency that was installed but never linked is still absent and still reported
as `PACKAGE_NOT_LINKED`.

A document that cannot be trusted end to end supplies nothing at all rather than
something partial: over the size limit of §30 it is `INPUT_LIMIT_EXCEEDED`, and
unparseable, of an unknown format, SPDX 3.0, or describing more than one package,
it is the new `EVIDENCE_UNREADABLE`. Both name the file. A root with no document
is silence: a dependency that shipped no SBOM has nothing missing about it.

vcpkg is deliberately untouched. Its `vcpkg.spdx.json` looks like a bundled SBOM
and is not one — vcpkg wrote it itself, so it is that manager's record of what it
installed — and the reader skips it by name. Reading it again would have let a
generic reader outrank the adapter that installed the package and quietly
relabelled the origin of every vcpkg component.

A directory that carries such a document is also a component boundary now, like
one carrying a licence file, and under the same exception: not at your own
project root, where your own SBOM describes your project rather than a dependency
inside it. Deviation D34 records the six questions §21.1 left open and how each
was answered.

### A copied-in library can now be described by what lies beside it

`internal/adapters/pkgmanager` had one kind of reader: an adapter, asked for
the packages it can prove, which searches wherever its manager is known to keep
things. That leaves a gap the mapping rules make visible. A library somebody
copied into the source tree is found by §19.2 strategy 6, which walks up from a
used file until it meets a marker — and for such a library the marker is its
`LICENSE` and nothing else. The component is found and its boundary is right,
and then it is named after its directory and left blank: no version, no
supplier, no purl, three findings saying so. What states them is usually lying
in the very directory that was just resolved.

Readers therefore come in two kinds now. An adapter enumerates and searches; an
**enricher** is handed a directory that already stands as a component root and
describes what the files directly in it say. It does not search, does not
descend and does not walk up. It hands back claims and findings — no paths, no
roots — so it cannot add a file to the used set and cannot move a component
boundary; there is nowhere in the signature to say either. Both call sites
exist: on the identity root of every discovered package, and on the root of a
component that only the marker walk found.

The first reader to use this is the bundled-SBOM reader above. The order it
applies in was fixed here — curated configuration above everything, a
`versionFrom` rule the user wrote above any manifest that would answer in its
place, and inside a package a manager owns the rank table of §21.1. One limit
is deliberate: a component keeps its directory name even where a manifest
beside it states another, because the name settles before the files are grouped
and changing it there would move a boundary rather than describe one.

Deviation D33 records the split and the two orderings, which §21 does not
provide for.

### Contradictions are now reported instead of being settled in silence

The architecture promises that when two strategies disagree, the higher-priority
one wins and the disagreement is reported rather than hidden. Four places kept
only the first half of that promise:

* Two mapping strategies naming different sources for one object computed the
  string `"cmake-file-api vs ninja-buildgraph"`, hung it on the evidence edge and
  told nobody. `OBJECT_SOURCE_MAPPING_CONFLICT` had been reserved for exactly
  this since the first draft of the specification and was never emitted.
* A source two CMake targets both compile — a library and its test binary, the
  most ordinary case there is — was deleted from the target map without a word,
  not even a debug line. The file then loses target ownership and falls through
  to the marker search or the anchor root, so it can end up in a third component
  altogether.
* A directory two package managers both claim stayed with the first adapter
  asked, silently.
* A file two packages both list was given to neither, and said so only to a
  debug log that is off in a normal run.

All four now build the same report: what was disputed, what each side said,
where each value came from, which side was used and why. Where nothing wins —
two targets, two packages — the finding says that outright and names the
strategy the file falls through to, because inventing a winner would be the
guess this tool refuses to make. The first case reports under
`OBJECT_SOURCE_MAPPING_CONFLICT`, the other three under the new
`COMPONENT_MAPPING_CONFLICT`; all four are informational.

Informational deliberately. A lock file and a manifest that differ are normal,
and confidence is not lowered because a second answer existed: §8.7 lists the
reasons a confidence may be downgraded and a disagreement is not among them. The
finding exists so that somebody can look, not to devalue an answer that is right.

Nothing about the resolution changed. The same file lands in the same component
as before, every fixture document is byte-identical, and no test that checks a
mapping had to be touched. What changed is that a run now says where it had to
choose.

### Package metadata now says where each value came from

A package manager's version, licence, supplier and purl were bare strings, and
only the version recorded a source. That worked because, in practice, exactly
one file supplied each value — except for FetchContent, which has two origins
for a version and settled them by which line of code ran last. Nothing said
which origin should win, so nothing could say which one had.

Each of the four values now carries its origin and that origin's rank, the
strongest origin wins, and equally strong origins keep whichever was found
first. The ranking is separate from the confidence the document already
publishes: a manifest can state a version exactly and still describe a revision
that is no longer checked out, so the checkout itself outranks every
declaration. Values that lost are kept rather than dropped, so that a later
release can report the disagreement instead of silently picking a side.

Nothing in the output moves. Every fixture document and findings file is
byte-identical to what the code produced before this change; this is the data
structure the next step needs, put in place on its own so that it can be
reviewed on its own.
`docs/dev/spec.md` §21.1 states the ranking, and deviation D32 says why it was
needed and why the checkout sits above the declarations.

### The introspection allowlist now describes what sbomb can actually do

The allowlist held fifteen command shapes. Three of them were reachable: the
`git` forms a package-manager adapter uses. Everything else was a permission
granted for nothing, the compiler probes included — nothing called those, and
the path rule below had to be loosened before anything could. And
`sbomb generate -v --allow-introspection` printed the whole table, so the log
named commands the run could never start.

Three fallbacks are now wired, each strictly behind the file it replaces. With
the file in place nothing is executed, whatever group is enabled:

* no `compile_commands.json` → `ninja -t commands <deliverable>` for the compile
  lines. A line that does not state its translation unit with an explicit
  `-c <path>` is skipped rather than searched for something source-shaped. The
  object→source mappings it yields are recorded as strategy 2 of §13.2, the
  Ninja build graph, which is where the specification puts that command — the
  evidence dump and the review report would otherwise name a compile database
  that this build directory does not have.
* `build.ninja` silent about an archive → `ninja -t inputs <archive>` for the
  objects it was built from. That answer is transitive, so a nested archive can
  report a member name twice; two candidates are no answer, and such a member
  keeps `ARCHIVE_MEMBERS_UNRESOLVED` instead of being given the wrong object.
* no `toolchains-v1` in the File API reply → the compiler probes for its
  installation directory and its implicit link directories. Not for the implicit
  *include* directories: `-print-search-dirs` reports none, so
  `TOOLCHAIN_LAYOUT_UNKNOWN` still fires and now says what each way can answer.

With the file missing and the group off, the run says which evidence it did not
get, under the identifiers it already had, each with a remediation naming the
group that could have supplied the answer.

A fourth gap is now named that used to be swallowed whole: a Ninja deps log
that cannot be read produced no log line, no finding and no error.
`NINJA_DEPS_UNAVAILABLE` is emitted for the first time — it was specified and
reserved — but with no group behind it, and this is the finding of this pass:
`ninja -t deps` reads the same file sbomb reads, and when it cannot read it, it
*rewrites* it. Measured against ninja 1.11, a log whose header it does not
accept is deleted and a damaged one is truncated. A tool pointed at somebody's
build directory reads it; it does not repair it. The command is gone from the
allowlist with the rest. The finding is only raised for a Ninja build; a
Makefiles tree has no deps log to miss.

Seven shapes are removed rather than left idle: `cmake --version`,
`cmake -E capabilities`, `ninja --version`, `ninja -t deps`,
`git status --porcelain`, `dpkg -S <path>` and `rpm -qf <path>`, and with them
two whole groups — `build.introspection.cmake` and
`build.introspection.osPackages` are now unknown configuration keys, and
`--allow-introspection=cmake` and `=osPackages` end the run instead of enabling
anything. A File API reply is written while CMake configures, and configuring is
a build command, so there was never a permitted fallback for the cmake group to
be. The dirty state already comes from the `-dirty` suffix of `git describe`,
which runs anyway.

The `osPackages` group promised the distribution package behind a system
library, and nothing was behind it: no system-library adapter, and no way to
build one from those two shapes. Measured on Ubuntu 24.04, `dpkg -S` names a
package and an architecture — no version, no supplier — so the component it
could have produced would still carry `UNKNOWN_VERSION` and `MISSING_SUPPLIER`;
and the system path it would be asked about lies outside every anchor a runner
is given, so the call would be refused before it started. Nothing a run reports
changes: a distribution library already came out with `UNKNOWN_VERSION`,
`MISSING_SUPPLIER` and `UNKNOWN_PURL`, and still does. The gap was never silent
and is no quieter now. `policy.systemLibraries` is untouched; it decides
inclusion, not origin.

Deviations D29 and D30 have the reasoning and the conditions for their
return.

One rule was loosened to make the compiler group reachable at all: a compiler
probe no longer requires an absolute compiler path to lie inside a registered
anchor, only to exist. The old rule refused `/usr/bin/cc` while permitting the
bare name `cc`, which PATH resolves to the same binary — it turned down the
exact statement and accepted the vague one. Path *arguments* are unchanged.

The review report gained an `introspection` line per executed command under
`== Run ==`, listing the argv and nothing else — a duration would break the
byte-for-byte guarantee. A run that started no process says so.

Two consequences worth expecting. A run with `--allow-introspection` can now
find evidence a run without it does not, so two runs over one build directory
with different groups produce different SBOMs; that is the point, and it was
already true of the `git` group. And a build with no File API reply, run with
`--allow-introspection=compiler`, gains a `toolchain:` anchor it did not have,
which changes the canonical identity of the files beneath it and therefore which
of them reach the document.

### `versionFrom` now reads what it promised to read

`components[].versionFrom` accepted `"git"` and `"commit"`, and neither ever
asked git. The first checked whether a `.git` directory existed and then
returned the *directory path* as the version; the second returned the constant
`0.0.0-git.000000000000`. Both reported success, so both would have suppressed
the `UNKNOWN_VERSION` finding that exists to say a version could not be
established — a placeholder published as a fact is the one thing this tool must
not do.

Worse, the rule list never arrived. It was read from a field of the component
that nothing outside the tests ever filled, so `versionFrom` had no effect at
all, `header:` included. The list is now passed in from the configuration where
it is written.

`"git"` runs `git describe --tags --always --dirty` at the component root and
`"commit"` runs `git rev-parse HEAD`, both through the introspection gateway
and only with `--allow-introspection=git`. Without that group neither rule is
attempted and no value appears; `UNKNOWN_VERSION` then names the missing
permission instead of leaving the reader to wonder. A tag from a modified
working tree is published with `VCS_DIRTY` beside it, because such a version
does not identify the content it names. The check that a component is a
checkout is now git's answer rather than a `.git` *directory*, so submodules and
linked worktrees — which carry a `.git` file — resolve like anything else.

Two runs over the same build can now disagree when the checkouts differ. That
is the point: the answer describes the tree that was built from.

### `build.dir` is optional

The configuration file no longer has to name the build directory. `--build-dir`
already says which directory to read, and a configuration shared across
`build/debug`, `build/release` and the rest should not have to repeat whichever
one is current.

What `build.dir` does is easy to misread, so the documentation now says it:
`--build-dir` is where sbomb reads, `build.dir` is what the build root is
*called* — the name every file identity under it is anchored against. Left out,
that name comes from the CMake File API, which is the build system's own answer
and the portable one. Set to something the evidence does not record, it
re-anchors files and changes which of them reach the document.

The `policy` section of the documentation also gained what it was missing: a
table of what actually differs between the four profiles, and one line per gate
saying when it fails.

### A package now knows every directory it lives in, and vcpkg proves its files

A package used to know exactly one directory, and everything that did not lie
under it belonged to somebody else. For FetchContent that directory is
`_deps/<name>-src`, while every file CMake generated for the package — a header
written by `configure_file`, the libraries built from the checkout — is in
`_deps/<name>-build`. Such a file matched no package at all and came out as the
manufacturer's own code. A package now carries a list of directories, and both
of those are on it. The first one remains the identity: the anchor, the
component root, the licence file and every `git` question still refer to the
checkout, so a version is never read out of a build tree. Only that first root
becomes an anchor, because a package has one key and a second one would name a
package that does not exist.

vcpkg had the worst version of the same problem and the best evidence to fix it
with. It merges every port into one triplet tree — all headers in `include/`,
all libraries in `lib/` — so the `share/<name>` directory that is the package
by layout contains none of the files anybody uses. In practice the prefix never
matched: a used vcpkg header fell through to the licence heuristic or to the
build anchor, and `vcpkg.spdx.json`, which states the name, the version, the
purl, the licence and the supplier outright, never reached the file it was
about. vcpkg also writes down every file it installed, one list per package
under `installed/vcpkg/info/`, and that list is now read. A header is no longer
matched against a directory it was never in; it is looked up in the record of
what was installed. Where there is no such list the document is exactly what it
was — the list improves an attribution that works without it. The findings do
change there: a vcpkg package whose files the `share/<name>` prefix never
matches now says so, as the `PACKAGE_NOT_LINKED` below. A list that is there
but breaches a parser bound of §30 is refused whole, with
`INPUT_LIMIT_EXCEEDED` naming it, rather than attributing half a package.

A file two packages both claim goes to neither, the same rule two CMake targets
sharing a source already met: two statements are no statement.

### A dependency that was installed but never linked is now said out loud

`PACKAGE_NOT_LINKED` (info) is new. A package no used file belongs to is
correctly missing from the document — installed is not linked, and only what
the deliverable contains is in the SBOM — but until now it went missing without
a word, and "why is the library I installed not in my SBOM" had no answer
except reading the source. It fails nothing and gates nothing; it says what was
left out and which manager installed it.

## 0.16.0

### `sbomb_enable` takes MAP, DEPFILE and OUTPUT — and they now do something

`MAP` and `DEPFILE` exist for projects whose toolchain file already sets
`-Wl,-Map=` and the link dependency file. They mean *the build already produces
this, here it is*.

Two things were missing for that to work. The module still added its own linker
flag beside the project's, so `-Wl,-Map=` stood on the link line twice and the
command-line order decided which file won. And the path was never passed on:
sbomb looks beside the artifact and nowhere else unless told, so the map was
written and never read — the SBOM lost its archive-member evidence silently,
which is the part that separates it from a repository scan.

Now a named path means the module adds no flag and hands the path to sbomb.

### A named evidence file that is not there is an error

`--map`, `--link-depfile`, `artifacts[].map` and `artifacts[].linkDepfile` used
to fall back to the locations beside the artifact when the path they named did
not exist — silently, exit 0. A path somebody wrote down is a statement, not a
hint, and reading a different file than the one it names puts evidence in the
document that nobody asked for.

It is `CONFIGURED_EVIDENCE_MISSING` and exit code 2 now, the same treatment a
configured artifact that does not exist has always had.

Whether such a path is right cannot be established while CMake is configuring:
a flag can reach the link line through a wrapper, a response file, or an
overridden link rule, and none of that is visible to a module scanning the
flags. So it is checked where the answer is certain — the file exists when the
SBOM is built, or it does not.

## 0.15.0

### A vendored library is a component again

A library copied into the source tree has no `conanfile.txt` and no
`vcpkg.json`, and its `CMakeLists.txt` cannot be read without interpreting
CMake. Nothing marked it, so nothing found it: it fell through to the anchor
root and became part of the manufacturer's own application component. It did
not appear in the SBOM as a component at all, and no finding said so.

A recognized licence file now marks a component boundary. It is an existence
check and not a parse, and a directory carrying its own licence is a distinct
work by convention. The search still stops at the anchor root, so a project's
own top-level licence can never be claimed by a dependency, and the nearest
marker wins, so a library vendored inside another library is split out rather
than hidden in it.

`NOTICE` and `COPYRIGHT` mark nothing. They are attribution material, not a
licence grant.

### The component root no longer depends on what the linker kept

Where a component begins decided where its licence file and its version header
were read from, and it was worked out separately from everything else: the
deepest common directory of the files that were *used*. A library with three
sources in `src/` of which the linker kept one had the root `src/`, so the
`LICENSE` one level up was never opened, and the answer moved when
`--gc-sections` moved it.

The root is a resolved fact now, settled by the same strategy that named the
component: the configured `path`, the package-manager root, the directory the
marker file was found in, or the anchor root. It is published as
`sbomb:component:root`. The common directory of the used files is the last
resort, and taking it emits `COMPONENT_ROOT_UNRESOLVED`, because a guessed root
should not look like a resolved one.

Licences that were NOASSERTION only because the root was too deep now resolve,
which is a change in output for anyone whose dependencies keep their sources in
a subdirectory.

### Components can be mapped by CMake target

`components[].targets` names CMake targets whose sources belong to a component.
The File API states which sources a target owns, so this is the build system's
own answer rather than a path prefix somebody has to keep in step with the
directory layout — useful where a dependency's files are spread over several
directories.

A source two targets both compile stays unmapped and falls through to the next
strategy. The build system said two things; picking one would be a guess.

### `CMakeLists.txt` is not a component marker, and will not be

Section 19.2 listed it. Detecting `project()` means interpreting CMake —
variables, `include()`, macros, `if()`, generated files — and the failure mode
is the wrong one: a false positive invents a component, with an invented
boundary, licence and name. Recorded as deviation D27, with the two signals
that cover the same ground without interpreting anything.

## 0.14.0

### A cached binary is checked again before it is used

`sbomb_fetch_binary` skipped everything once the binary was in the build tree.
The download verified what arrived, but that was some earlier configure: from
the second one on nothing looked at the file at all, so "the download is
checked against SHA256SUMS, and there is no option to skip that" was true of
the first run and of no other.

It is re-hashed now, against the pin where there is one and otherwise against
the `SHA256SUMS` fetched beside it the first time. No network is involved. A
file that no longer matches is discarded and fetched again rather than refused,
because making the caller clean the build tree is a worse answer than fixing
it. So is a file with nothing left to check it against — an unverifiable binary
is not a usable one.

Reading the digest for one asset out of `SHA256SUMS` is one function now,
called by the download path and the cache check alike. The same question asked
by two slightly different regexes is a bug waiting for the day they drift.

### The TLS check can be steered for sbomb alone

`CMAKE_TLS_VERIFY` covers every download a project makes. `SBOMB_FETCH_TLS_VERIFY`
covers ours, and is consulted first, so the two can differ: a project that has
to fetch something else unverified does not have to fetch sbomb unverified as
well. Unset, both, the answer is `ON` — the default does not move.

It is the same shape the CA already had, where `SBOMB_FETCH_TLS_CAINFO` comes
before `CMAKE_TLS_CAINFO`. Until now the pair was lopsided: you could say whom
to trust for sbomb alone, but not whether to check.

The value is read as a boolean and refused when it is not one, naming the
variable and what was expected. `if(${value})` would have been the short way
and the wrong one: a bare string in `if()` is taken as a variable name and
expanded, so a typo would quietly have become "off". An empty
`CMAKE_TLS_VERIFY` is now treated as "not set" rather than passed on, where it
reached `file(DOWNLOAD)` as `missing bool value for TLS_VERIFY` — an error
pointing into this file, for a value the caller left empty.

**Switching it off is never silent.** With nothing pinned it warns, because
`SHA256SUMS` then arrives over the same unverified connection as the binary it
vouches for: the checksum still runs, but it can only show that the two agree,
not who sent them. With `SBOMB_FETCH_SHA256` or `SBOMB_FETCH_SHA256SUMS` set it
says so quietly instead — a pinned digest is the one thing an untrusted
transport cannot supply, and it is what the download is held to.

The documentation said "there is no switch to turn the verification off, and
none is planned". There is one, so that sentence is gone and the section says
what it costs and what to pin instead.

Seven cases run in CI against a self-signed HTTPS server, which is an
intercepting proxy in miniature: verified by default, accepted with the
certificate named, accepted and warned about when off, accepted quietly when
off and pinned, ours winning over CMake's, an empty value not disabling
anything, and a non-boolean refused.

## 0.13.0

### The fetched binary can be verified against something the network did not say

The download has always been checked against the release's `SHA256SUMS`, and
that stays the default. It catches a truncated transfer and a swapped asset,
but it is trust on first use: the list travels beside the file it vouches for,
so whoever could substitute one could substitute the other.

`SBOMB_FETCH_SHA256SUMS` pins the list, which covers every platform with one
value, and `SBOMB_FETCH_SHA256` pins this host's binary — and when the binary
is pinned, `SHA256SUMS` is not fetched at all, because a list downloaded beside
the file can only confirm what the same connection already supplied and cannot
outrank a value that came from elsewhere. Both are the digests the release
already publishes.

A mistyped digest is refused as a mistyped digest, naming the variable and what
was expected. It would otherwise arrive as a checksum mismatch, which reads as
though the download had been tampered with — the one message that must not be
spent on a typo. A mismatch now also says which of the two it was checked
against.

The same applies one level up, to the bundle `FetchContent` pulls, and there
the answer is CMake's rather than ours: `FetchContent` verifies nothing by
default — `TLS_VERIFY` is off unless asked and no digest is checked unless
given — so the documented example now carries `URL_HASH` and `TLS_VERIFY ON`.

### A CA bundle can be named, for networks that intercept TLS

A proxy that re-signs TLS presents its own certificate, and `file(DOWNLOAD)`
with `TLS_VERIFY ON` rejects it: the verification working, not breaking.
`SBOMB_FETCH_TLS_CAINFO` names the CA it re-signs with, and `CMAKE_TLS_CAINFO`,
`SSL_CERT_FILE` and `CURL_CA_BUNDLE` are consulted after it, so a machine
already set up for curl or OpenSSL needs nothing. Which bundle is in use, and
which of the four named it, is printed before the download.

`CMAKE_TLS_CAINFO` is read by both downloads — CMake's own for the bundle, and
sbomb's for the binary — so one setting covers the pair, and the documentation
leads with that rather than with the two separate knobs. It has to be joined by
`CMAKE_TLS_VERIFY=ON`, which is the half that is easy to leave out and silent
when left out: `FetchContent` checks no certificate unless asked, and the CA is
then simply unused.

Naming a CA adds trust rather than replacing it — the system store is still
consulted — so this makes an intercepted connection work without narrowing the
download to one authority. Turning the verification off remains impossible, and
a CA file that does not exist is refused up front rather than as a download
failure per file.

### A failed download says what failed

`sbomb: the release has no SHA256SUMS, so the download cannot be verified` was
printed for *any* failure fetching that file — a claim about the release, made
on the evidence of a refused request, which sent whoever read it to look in the
wrong place. It happened for real: three CI jobs pulled one release at the same
moment, one was throttled, and the build reported a missing file that was there
all along.

libcurl's own message is reported now, as the download of the binary beside it
already did, and a certificate failure with no CA named suggests naming one.
Each download is attempted three times with a growing pause, because a single
refused request is not an answer about anything.

## 0.12.0

### One command installs sbomb

```bash
curl -fsSL https://andste82.github.io/sbomb/install.sh | sh
```

```powershell
irm https://andste82.github.io/sbomb/install.ps1 | iex
```

It takes the latest release for the platform it is running on, verifies it, and
puts it where the shell will find it. `--version` pins a release, `--bin-dir`
chooses where it lands, and `--with-sbom` fetches the release's own CycloneDX
document beside the binary, so the thing you just installed can be audited with
the thing you just installed.

**The checksum check has no off switch.** Not a flag that defaults to on: there
is no flag. A tool whose whole argument is that you should be able to verify
what you were given cannot hand you a binary it did not verify itself. The
digest is compared against the line for *that asset* in `SHA256SUMS`, so a
matching line for some other file proves nothing, and a release carrying no
`SHA256SUMS` is refused rather than installed unchecked.

`latest` is resolved from where `/releases/latest` redirects to, not from the
API. The API is rate limited per IP for unauthenticated callers, which a shared
CI runner or an office behind one address will hit through no fault of its own
— and an installer that fails because somebody else installed too often is not
an installer. The redirect has no such limit and needs no token.

Two smaller refusals. The script never runs `sudo` on your behalf: it takes
`/usr/local/bin` where that is writable and `~/.local/bin` where it is not,
because a script fetched from the network does not get to decide it is root.
And a platform the release does not carry is told so by name, rather than
through a 404 on a URL that could never have existed.

Both scripts are served from GitHub Pages, assembled from the tree by the
`pages` workflow and never edited in place, so the install URL survives a
branch rename and the served copy cannot drift from the repository. Both are
checked before either is published — `sh -n` and `shellcheck` for one, the
PowerShell parser for the other — because a broken install script is worse than
a broken feature: it is the first thing a new user runs.

### CMake fetches sbomb itself

A release now carries a twelfth asset, `sbomb-cmake.tar.gz`, and it is the
whole integration:

```cmake
include(FetchContent)
FetchContent_Declare(sbomb
  URL https://github.com/Andste82/sbomb/releases/download/v0.12.0/sbomb-cmake.tar.gz)
FetchContent_MakeAvailable(sbomb)

add_executable(app src/main.c)
sbomb_enable(TARGET app POLICY lenient)
```

One declaration gives both halves at once: `sbomb_enable`, since CMake
functions are global once defined, and a binary that matches it. Until now the
module was a file to vendor and the binary a thing to install separately, which
is two versions to keep in step and no way to notice when they drift.

The bundle fetches the binary for the **host**, not for the target. sbomb reads
a build tree and runs wherever cmake runs: a firmware project cross-compiling
to bare-metal ARM still needs the binary for the developer's laptop, and
`CMAKE_SYSTEM_PROCESSOR` there names the microcontroller. The download is
checked against the release's `SHA256SUMS`, with no option to skip it for the
same reason the install scripts have none, and lands under `_sbomb/<version>`
in the build tree — renamed into place only once verified, so an interrupted
configure cannot leave a half-written binary that the next run treats as
cached. A path the caller has already put in `SBOMB_EXECUTABLE` wins, and then
nothing is fetched at all.

Fetching happens at `MakeAvailable` rather than inside `sbomb_enable`, so a
configure either has the tool or has failed, and no target is defined that
would fail later for a reason the configure already knew.

The version lives in the asset. The release script substitutes it into the
fetcher while building the bundle and stops if the placeholder survives; a copy
taken from the source tree still carries the placeholder and says so, instead
of guessing which version to download. The archive is deterministic — sorted
members, fixed ownership and mtime — and `SHA256SUMS` covers it like everything
else in the release.

### The CMake module configures once, and says when it was included too late

`sbomb_enable` used to switch on `CMAKE_EXPORT_COMPILE_COMMANDS` itself, and
that is the whole reason the `sbomb-<target>` target had to re-run cmake before
it could generate anything. The generator decides whether to record a compile
command when it processes a target, so setting the variable afterwards — which
is when `sbomb_enable` runs, since a target must exist before it can be named —
reaches the cache and misses the run, and the compile database turns up only on
the *next* configure. It is set at `include(Sbomb)` time now, before any target
is defined, and one configure is enough.

It follows that `include(Sbomb)` belongs above the targets it will be asked
about, and being below them is otherwise silent: the build succeeds, the
document is written, and it is quietly worse because no object could be traced
back to a source. The module warns about it at the moment the include can still
be moved.

On CMake 3.27 and above the File API query is filed with `cmake_file_api()`,
which takes effect in the run that is happening, so the SBOM target has nothing
to prepare and the re-configure is gone. Below 3.27 a query is read only at the
*start* of a run, so it takes effect on the next one — there the target still
re-configures, because a reply that never arrives is not a degraded answer but
no answer: sbomb would know no targets, no anchors and no toolchain.

**A default configuration file, without a cache variable that leaks into
projects that have none.** `sbomb_enable` with no `CONFIG` now uses
`${CMAKE_SOURCE_DIR}/sbomb.json` when that file is really there, and passes
nothing when it is not, because naming a file that does not exist turns a run
which would have worked on defaults into a failure. The cache variable is
called `SBOMB_DEFAULT_CONFIG` and not `SBOMB_CONFIG` on purpose:
`cmake_parse_arguments` leaves `SBOMB_CONFIG` undefined when the caller passed
no `CONFIG`, and an undefined normal variable lets a cache variable of the same
name show through — so a cache `SBOMB_CONFIG` would have been passed as
`--config` exactly as though somebody had asked for it, and every project
without that file would fail at build time on a configuration it never named.

**Linker evidence only where a linker runs.** A static or object library is
archived rather than linked, and an imported target is somebody else's build;
`target_link_options` on either is at best ignored. Those targets are skipped
with a line that says so, rather than appearing to have been given evidence
flags that never took.

### The project's name and version are not written twice

A CMake project already declares both — `project(device VERSION 1.4.2)` — and
sbomb made you say them again in `sbomb.json`. Two places for one fact is a
chance for the two to disagree, and the loader *insisted* on `project.name`, so
there was no way to avoid it.

Both now come from the CMake File API when the configuration gives none. This
is read rather than passed: the values are in the cache reply sbomb already
parses, two lines from where `CMAKE_GENERATOR` and `CMAKE_BUILD_TYPE` are read,
so the manual route gets them as readily as the bundled module and no new
command-line surface exists to keep in step. A flag would also have been a
claim by the caller, and the point of reading it is that it is evidence.

`project.name` is no longer a required field. What a document describes is
still always answered — configuration, then build system, then the deliverable
— just not by insisting the answer be typed out.

**The name only in assembly mode.** §6.1 says the root component of a
single-artifact document is the deliverable, and the deliverable is not the
project: a build of `firmware.elf` describes `firmware.elf` whatever the
enclosing `project()` is called. Taking `CMAKE_PROJECT_NAME` there would have
renamed the root component, and its `bom-ref` with it, in every document whose
configuration named nothing. §6.2 is where `project.name` is the root's, and
that is where it is read.

The version is recorded with its source, so a read version and a curated one
are not the same claim: `CMAKE_PROJECT_VERSION` appears in
`evidence.identity` as `manifest-analysis` with `cmake` as the method's value,
beside conan, vcpkg and fetchcontent. The product's version had carried no
source at all until now.

### The getting-started page works when followed

Checked by building a project against the module rather than by reading it. The
CMake example said `POLICY cra`, which is the release-gating profile: it fails
on `MISSING_SUPPLIER`, `UNKNOWN_LICENSE` and `UNKNOWN_VERSION`, which every
component of an uncurated project trips, and the custom target turns that exit
code into a build error. A reader following the page from the top reached a red
build with no explanation, while two sections further down the same page told
them to start with `lenient`. The example says `lenient` now, and says why the
target fails when the policy does: a policy that cannot stop anything is
decoration.

Three things a reader meets and the page did not mention are in it now — that
the compile database is switched on for them, what the `sbomb-<target>` target
does before generating, and that `sbomb_enable` per target gives one document
per target, where a product made of several deliverables is an assembly
document instead, which is `mode` and `artifacts[]` in the configuration and
not the CMake module. The install section leads with the install script and
keeps the manual route below it, being the same four steps written out.

## 0.11.0

### A version now says where it came from

sbomb has always worked out which source supplied a component's version and how
much that source is worth — a conan manifest, a `git describe`, a macro in a
header, a line somebody typed into the configuration — and then published
neither. `sbomb:version:source` and `sbomb:version:confidence` were in the
catalogue and marked `reserved`: listed, and never written. The only place the
answer surfaced was the human review report, so a consumer reading the document
saw a bare version string with no way to weigh it.

It is now `component.evidence.identity` with `field: "version"`: the value in
`concludedValue`, the confidence as a number, and the source as one method. The
`technique` vocabulary is closed and coarse — three of sbomb's sources are all
`manifest-analysis` — so the exact source stays in the method's `value`, which
is the field that survives the mapping. A source nobody has mapped becomes
`other` rather than the nearest-looking technique, and a version with no
recorded source produces no evidence at all: a claim about how a value was
established is worth less than nothing when it is invented.

`evidence.identity` predates 1.6, so this is written at both specification
versions and the two reserved property names are removed from appendix B.

**Two bugs came out with it.** The `IdentityEvidence` and `Method` types were
already declared, and `canonicalizeBOM` already sorted them; nothing had ever
filled them in, so nothing had ever checked them. `IdentityEvidence.Value`
serialised as `"value"`, which the schema does not have and, with
`additionalProperties: false`, does not allow — the field is `concludedValue`.
And `Method.Confidence` carried `omitempty` although `confidence` is required
on a method, so a method of confidence 0 would have dropped it. Dead structure
is bad enough; dead structure that is wrong hands the bug to whoever uses it
first.

### CycloneDX 1.7 is available, and 1.6 stays the default

`--spec-version 1.7`, or `output.specVersion` in the configuration file, writes
CycloneDX 1.7. Nothing changes for anyone who does not ask: 1.6 remains what
`generate` and `self` write, because BSI TR-03183-2 names it as the minimum and
nothing in 1.7 changes that. Every consumer that reads 1.6 today keeps working;
nobody is moved.

1.7 is additive over 1.6 — 108 definitions against 91, nothing removed, the
same required top-level fields, still JSON Schema draft-07, so deviation D6 is
untouched. The goldens say it plainly: the same build written at both versions
differs in three lines, being `specVersion`, the `sbomb:run:specVersion`
property that records it, and the serial number, which is a digest of the
canonical document and so moves once anything in it does. A document must not
change shape with the version beyond what the version requires, and that pair
of goldens is what enforces it.

One 1.7-only difference exists, behind the version and documented rather than
discovered. `component.evidence.licenses` may mix SPDX expressions with licence
identifiers at 1.7; at 1.6 `licenseChoice` is a choice between a list of
licence objects and a tuple of exactly one expression, which is why
[D19](dev/deviations.md) has to render an observation as identifiers as soon as
there are two of them. Where an observation carries a relation between licences
rather than naming one, 1.7 can now say so beside observations that do not.
Observation yields bare identifiers today, and those are written identically at
both versions, so this lifts the ceiling rather than changing current output.

`sbomb validate` no longer assumes what it is reading. It detects the format
from the document itself and checks it against the schema of the version the
document declares, so a 1.7 file another tool wrote validates as 1.7 — and a
1.7-only field in a document labelled 1.6 now fails, which is the proof the
choice is real. A version this build has no schema for is refused rather than
checked against the wrong one. `sbomb schema --cyclonedx --spec-version 1.7`
prints the schema that check uses.

Both schemas are embedded, which is what lets `validate` check either; half a
megabyte of JSON is the price, and a validator that can only check what this
build happens to emit is not a validator.

Two gaps in the writer seam are closed on the way, because this is the first
release where a writer offers more than one version. `Writer.DefaultVersion()`
states which version is written when the caller has no opinion, rather than
leaving it to a convention about slice order that breaks the first time
somebody reorders one; `sbomwriter.Resolve` is the single place where "no
version given" becomes a version. `sbomwriter.DetectFormat` and the `Detector`
interface are what `validate` uses, and are the seam a second format will
arrive through.

### A standard field beats a property in the sbomb namespace

Where CycloneDX specifies a field for something sbomb records, the specified
field now carries it. Three of the structures 1.7 adds are emitted on that
basis, and one change reaches 1.6 as well.

**The repository URL is an external reference, at both versions.** The code
already said so — "the repository URL belongs in externalReferences", in
`components.go` — and then wrote `sbomb:component:vcsUrl`, because sbomb
emitted no external references at all. It does now, as a reference of type
`vcs`; that type predates 1.6, so the change is not gated on a version and the
1.6 output moved deliberately. `sbomb:component:vcsUrl` is removed from
appendix B rather than left as a name nothing writes.

**`sbomb:component:vcsCommit` and `vcsDirty` qualify that URL**, and 1.7 gives
external references a property bag to put them in, beside the thing they
describe. At 1.6 they stay on the component, which is the only place there is.

**`component.isExternal`** marks a component the target expects to find rather
than to carry. System scope alone is not enough to claim that: a system archive
linked statically ends up inside the artifact, so the mark needs a shared
library among the component's files as evidence that the environment really
does provide it. No `versionRange` goes with it — `DT_NEEDED` records a soname,
and deriving a range from whatever the build host has installed would describe
that host rather than the product.

**`metadata.distributionConstraints.tlp`** is written when `output.tlp` is set,
and never otherwise. Nothing infers it from `--redact-unanchored-paths`: a TLP
marking states what the recipient may do, which redaction does not. There is no
default, because the schema annotates one — an absent constraint has to mean
sbomb was not told, not that the document may travel. Setting it at 1.6, which
cannot carry it, is a usage error rather than a silent omission.

`citations` is still not emitted, and the reason is now a finding rather than
an appetite. `evidence.identity` cannot carry a licence technique: its `field`
enum admits identity fields only, and its `methods[].technique` vocabulary does
not contain the SPDX techniques of §22.3. So `sbomb:license:technique` has no
standard field at either version and stays a property — correctly, since it
records something the standard does not model.

### A prebuilt library's sources are named, not left opaque

Strategy 6 of section 13.2 -- the compilation-unit name in an object's own
debug information -- was specified, the resolver already had a slot for it, and
nothing ever filled it. Every other strategy asks the build system, so an
object the build system does not know about could not be resolved at all: a
static library a vendor ships appeared in the SBOM as an opaque
`libvendor.a(vendor_blob.o)` with `LINKED_OBJECT_SOURCE_UNRESOLVED` beside it.

It reads a member of a static archive as readily as a standalone object, and
only objects no earlier strategy claimed -- opening every object file of a
fifty-thousand-unit build to parse DWARF would cost more than the whole run.
`binfmt.InspectBytes` and the member content the archive parser already had in
hand are what makes the archive case work without unpacking anything to disk.

The corpus gains `p13-prebuilt`, which compiles and archives a source at
configure time so that nothing in `build.ninja`, `compile_commands.json` or any
depfile records how it was made. It is the first fixture where strategies 1 to
5 all fail.


### The property catalogue is checked too

Appendix B lists the `sbomb:` properties a document may carry, and nothing
checked it. Eleven were written into documents with no catalogue entry, so a
consumer meeting `sbomb:go:moduleSum` had nowhere to look it up; thirty-nine
were catalogued and never written.

`tools/propertydoc` does for properties what `tools/findingsdoc` does for
findings: emitting one the appendix does not define fails the build, and
`docs/properties.md` is generated from the appendix and marks each entry
`emitted` or `reserved`. Deviation D26; two catalogue entries are renamed to
the names documents actually carry.

`docs/properties.md` is new user documentation: what the properties are for,
how to read a licence answer from the four that describe it, and the three BSI
TR-03183-2 properties every component gets.


### `sbomb schema` describes the configuration the tool actually reads

The published schema was written by hand and had drifted: it named five of the
eleven sections the loader accepts and set `additionalProperties: false`, so it
rejected `schemaVersion`, `output`, `anchors`, `discovery`, `components` and
`manifests` — every documented example failed against the document the tool
hands out, and `docs/configuration.md` pointed readers at it. A wrong schema is
worse than none, because it is machine readable and therefore believed.

It is derived from the Go types now (deviation D24), so it and the loader
cannot disagree about what a valid file is. A test validates every committed
configuration against it and checks that it refuses what the loader refuses.

`tools/docexamples` runs every documented configuration through the loader
(D25). It found one on its first run: the documentation described an artifact
role `firmware`, which the loader has never accepted — `firmware` is the
CycloneDX type that the `bootloader`, `image` and `filesystem` roles produce.
Both checks are in the gate and in CI.


### The specified command line and the real one are the same list again

The specification described 43 flags and 20 existed. Nothing was silently
wrong — an absent flag is refused — but a normative table twice the size of the
tool is not a reference anybody can use.

Seven are built, each mirroring a setting the configuration file already had, so
that a one-off run against somebody else's build tree needs no file written
first: `--source-dir`, `--mode`, `--config-name`, `--map`, `--link-depfile`,
`--image-manifest`, `--evidence-dump`.

Nine are removed from the specification rather than built (deviation D22):
`--hash-alg`, `--jobs`, `--log-level`, `--log-format`, `--absolute-paths`,
`--keep-raw-evidence`, `--compile-commands`, `--buildgraph`, `--depfile-mode`.
Seven stay specified and absent, each named with the feature it waits on.

`sbomb generate` also stops writing into a directory it does not own without
being asked: the evidence dump still defaults to `<build-dir>/evidence.json`,
because that is where `explain` looks, but `--evidence-dump` moves it and
`--evidence-dump=off` suppresses it (deviation D23).


### A typo in a policy gate is no longer ignored

The specification promises that a typo cannot silently disable a policy gate,
and the documentation repeats it. It held only for top-level keys:
`{"policy": {"failOnMisingHash": true}}` loaded without complaint and the gate
stayed off — the one failure a configuration file must not have. Loading now
refuses an unknown field at every level of the document (deviation D21). This
is stricter than before and can reject a file that used to load; a file it
rejects was one whose author believed something that was not happening.

### Configuration keys that did nothing are gone, and one is wired

`output.reproducible` was parsed and never read, so a project asking for
reproducible output got a timestamp and a random serial number. It works now.
It belongs in the file as well as on the command line because reproducibility
is a property of a project, not of an invocation.

`output.hashAlgorithms`, `generators[]`, `components[].cdxType` and
`components[].upstream` are removed rather than implemented — deviation D20
gives the reason for each, and the specification is amended. `output.format`
and `output.specVersion` stay and are now checked against the one value each
admits, instead of being accepted and ignored.


### The user documentation describes the tool that exists

The README documented a dozen flags that were never implemented --
`--source-dir`, `--mode`, `--spec-version`, `--jobs`, `--log-level`, `--map`
and more -- and omitted most of the ones that are. All three user documents
were rewritten against the actual command line, configuration and behaviour,
and every example in them was run.

`docs/architecture.md` is new: how the evidence chain works, what backs each
step, and where sbomb refuses to guess, with diagrams. The README says in three
sentences how sbomb relates to Syft and its kin, because anyone who finds it
will ask.

Several configuration keys turned out to be accepted and then ignored --
`output.*`, `generators[]`, `components[].cdxType` and `.upstream`. They are
listed as such in `docs/configuration.md` rather than described as if they
worked.


### smoke-test runs again

`release: published` never fired it: GitHub does not start workflows from
events created with the default `GITHUB_TOKEN`, which is what
`gh release create` uses. So the check that downloads a published binary and
runs it has not run on any release yet. `release.yaml` calls it now, after
publishing, which leaves no gap for either failure mode this has had.

### A skill for cutting a release

`.claude/skills/release` records the whole sequence: how the version is decided
from the changes, the gate, the local five-target build, the tag, and what to
verify afterwards -- together with the five things that have gone wrong before,
each of which cost a release.

## 0.10.0

### A file with two licences in it says so

- The commonest reason a licence file resolved to nothing was that it holds
  *two* licences: "dual licensed under MIT or Apache-2.0" is two complete texts
  one after the other. Compared as a whole the file is neither, so every
  technique returned nothing — while both licences were plainly there. Of 142
  real licence files, 18 were of this shape and 15 more held one licence with
  other material around it.
- sbomb now records which licence texts a file contains as
  `component.evidence.licenses`, and leaves `component.licenses` at NOASSERTION
  with the new reason `license-composition-unresolved`. Whether both apply or
  the recipient may choose is written in the prose between them; reading that
  would be the keyword heuristic the specification forbids. The finding names
  the licences, so a reviewer knows what to decide, and curating
  `components[].license` fills in the conclusion beside the evidence that
  supports it. Deviation D19; section 22.7 gains the reason code.
- Of the 51 files no whole-file technique resolved, 23 now carry licence
  evidence.

### Licences with a filled-in copyright holder are recognized

- The digest table of section 22.3 technique 2 only ever matched a verbatim
  text, and a real licence file is rarely verbatim: `github.com/google/uuid`
  writes "Neither the name of **Google Inc.**" where SPDX writes "the copyright
  holder", and bullets its clauses instead of numbering them. Unmistakable to a
  person, invisible to a hash. **All three of this tool's own dependencies
  failed**, which is why the release script asserted their licences by hand.
- sbomb now matches against the SPDX `standardLicenseTemplate`, which declares
  in the licence list itself which spans may vary and, as a regular expression,
  what into. Measured over 142 distinct real licence files: 32 matched by
  digest before, 55 more match now. The release script no longer curates
  anything -- every dependency's licence is derived from the vendored text.
- It is not the similarity matching section 22.7 forbids: no score, no
  threshold, exact outside the declared spans. It is a fourth technique where
  the specification said three, so it is recorded as deviation D18 and section
  22.3 is amended. The binary grows from 5.17 MB to 6.21 MB; the templates are
  decompressed only after a digest lookup has missed.
- Each component now carries `sbomb:license:technique`, so a reviewer can tell
  a declared identifier from a digest match from a template match.
- Optional blocks in a template nest, and closing an outer one on an inner
  one's marker truncated it silently: GPL-2.0 and LGPL-3.0 matched nothing at
  all. They are reported now as an ambiguity naming both `-only` and
  `-or-later`, which the licence text genuinely does not distinguish.
- A deprecated identifier is used only when nothing current matched, so
  `GPL-2.0` no longer contradicts `GPL-2.0-only`.
- **An `SPDX-License-Identifier:` tag could swallow the following line.** The
  expression's character class admitted `\s`, so a tag above a copyright
  statement yielded `MIT\nCopyright (c) 2009` as the licence. Found by a test
  written for the template work.

### macOS builds

A release now carries darwin/amd64 and darwin/arm64 as well, each with its own
SBOM, and the composite action resolves them on a macOS runner. They cost
nothing to produce: the tool is `CGO_ENABLED=0` and only reads files, so it
cross-compiles without an SDK or a macOS host. They are not signed or
notarized, which the release notes say.

### smoke-test could never have passed on a tag

It triggered on the same tag push as `release`, so it downloaded a release the
other workflow had not created yet. v0.9.0 is the first release there has ever
been, and it failed with a 404 while `release` was still building. It runs on
`release: published` now, which fires when `gh release create` returns.

And once it got that far it failed again, for the second reason nothing had
ever exercised: this repository is private, so an unauthenticated download of a
release asset answers 404 -- indistinguishable from a missing tag. The
composite action takes a `token`, defaulting to `${{ github.token }}`, and
downloads with `gh` when it has one.

Past that, the two platforms produced different documents -- correctly. The
path flavor defaults to the host's and the flavors differ in case sensitivity,
so comparing bytes across platforms only means something with it pinned, which
is what `scripts/determinism-check.sh` has always done and the smoke test did
not. The action takes a `path-flavor` now.

## 0.9.0

### Regenerating the fixture corpus is a no-op again

- Two runs of `tools/fixtures/regen.sh` over unchanged sources rewrote around
  150 files, which makes a real corpus change impossible to review. Five causes
  were ours and are fixed: the File API index filename carried the configure
  wall clock, `.ninja_deps` carried each output's modification time, parallel
  builds reordered that log, the GNU PE linker stamped a link time into every
  `.exe`, and GCC drew a fresh random seed for the LTO sections.
- Three projects still move and the toolchain is why in each case: CMake orders
  a target's dependency list unstably, the LTO map names GCC's temporary
  objects, and Conan gives a locally built package a random cache folder.
  Deviation D17 records what was measured.
- `tools/fixtures/check-reproducible.sh` regenerates twice and fails on any
  difference outside those three, which it names rather than pattern-matches.
- `tools/fixtures/regen.sh --only <toolchain>[/<project>]` regenerates one
  toolchain or one pair, because verifying that a regeneration is a no-op means
  running one and a full corpus takes minutes.
- `--redact-unanchored-paths` is now tested against all three outputs at once.
  Section 30.7 requires it of the SBOM, the findings JSON and the review report
  equally; that it held was assumed rather than checked.

### A release describes itself, from evidence

- `sbomb self <binary>` writes a CycloneDX document for a Go executable from
  the module record its linker embedded: every module linked in, at the version
  and with the `go.sum` hash that reached the artifact. It reads the binary and
  nothing else -- no subprocess, no network, no source-tree scan -- so it works
  on a cross-compiled binary for a platform the host cannot run.
- Every released binary now ships that document beside it, and the release
  workflow re-validates each one from the published file. This closes deviation
  D16: the previous self-SBOM was generated from a compile database the release
  script wrote on the spot naming one Go file, which is the guessing this tool
  exists to refuse.
- Licence evidence comes from a vendor directory, but only for modules whose
  vendored version matches what the binary records -- a vendor tree from
  another commit would otherwise hand the wrong licence to the right component
  and look exactly as confident as a correct answer. `--license
  <module>=<SPDX>` curates what exact text matching cannot recognize, is marked
  as curated rather than detected, and is reported as a conflict if the licence
  text contradicts it.
- **The tool did not build for Windows.** `internal/limits` used
  `syscall.O_NOFOLLOW`, which does not exist there, so every cross-compile to
  windows/amd64 failed -- including the one in the release script. The
  no-follow open is platform-split now: the kernel enforces it where it can,
  and where it cannot the check precedes the open and the remaining race is
  documented rather than hidden.

### Bounded parsers, and fuzzing for the newest ones

- `internal/limits` holds the four bounds of section 30 in one place. Eighteen
  files each had their own before, which is how they came to disagree about
  what "the maximum line length" is.
- `--max-input-size` and `--strict-symlinks` exist. The first refuses a file
  before allocating for it; the second refuses a path whose final component is
  a symbolic link, with `O_NOFOLLOW` so the kernel decides rather than a check
  that something can race.
- Six new fuzz targets, fourteen in total, covering everything phases 6 and 7
  added. Two defects on the first run: the response-file tokenizers replaced
  every non-UTF-8 byte with U+FFFD, so a Latin-1 or code-page path would have
  entered the SBOM under a name matching no file; and `#include ""` produced an
  empty path that was then identified as a file.

### The smoke test checks the release, not itself

`action-smoke` fed the released binary a `compile_commands.json` naming
`README.md` as a compiler input and a linker map whose contents were the word
`map`, then asserted that the output file was not empty. An SBOM with no
components would have passed. A step before it ran the whole end-to-end suite
whose output nothing used.

It is `smoke-test` now and uses the evidence this repository already commits.
The released binary runs against `gcc-ninja/p02-static` and has to produce
exactly `main.c`, `crypto.c` and `crypto.h` -- and not `unused.c`, which is
compiled into an archive the linker never extracts. Both platforms run
`--reproducible` and a third job checks they produced the same bytes, so the
published Linux and Windows binaries are held to the same answer.

The composite action gained `build-dir` and `reproducible` inputs. It could
only ever read the workspace root, which is rarely where a build directory is.

### A release workflow, and a release build that works

Nothing published a release. `scripts/release.sh` produced the artifacts
locally and no workflow ever called it, so no release existed, and the
composite action -- which downloads a release asset and verifies its checksum
-- could only ever answer 404. That is what `action-smoke` has been reporting.

`release.yaml` runs on a `v*` tag: it checks that the build is reproducible,
builds the three targets, verifies that the binary reports the tag it was built
from, checks the checksums cover every artifact, and publishes the release. It
is the only workflow with write access; the other three now say `contents: read`
explicitly.

Two defects surfaced while making it work. `scripts/release.sh build` failed
every time: the self-SBOM it generated tripped the default policy, `sbomb`
returned exit 3, and `set -e` aborted before the checksums were written. And
the release binary reported `sbomb v0.8.0` where every development build
reported `sbomb 0.8.0`, so the tag prefix reached `metadata.tools` in the SBOM.

The fabricated self-SBOM is gone rather than fixed in place; see deviation D16.

Workflow files are `.yaml` now, including the composite action.

## 0.8.0 - 2026-09-06

Roadmap phase 7 is complete: sbomb reads what package managers recorded, so a
dependency Conan, vcpkg, FetchContent or CPM installed becomes its own
component with the manager's name, version, purl and licence -- without a line
of curated configuration. Git submodules are component boundaries, and package
and image manifests make what a firmware image contains part of the SBOM.

Two corrections in this release matter more than the new adapters. Licence
detection matched 1 of 160 real licence files and now matches 93. And section
garbage collection and the LTO confidence downgrade, shipped in 0.7.0 with unit
tests only, are exercised against real linker output -- which is how three
defects in them came out.

What 1.0.0 still needs is the hardening of phase 8: the performance budget of
section 31 measured rather than assumed, the parser limits of section 30 as one
policy, and a self-SBOM produced from evidence rather than fabricated.

### A findings catalogue that cannot drift

`docs/findings.md` explained how findings work and named none of them. It now
carries the whole catalogue, generated by `tools/findingsdoc` from appendix A of
the specification and annotated with which identifiers this build actually
emits -- 40 of 52 -- so a reserved one cannot be mistaken for something a run
might report.

The generator cross-checks three sets that have to agree: what the
specification defines, what the code emits, and what the documentation lists.
On its first run it found four identifiers the code emits and appendix A never
defined, including `WEAK_EVIDENCE`, whose gate `failOnWeakEvidence` appears in
three profile tables. They were added to the specification; see deviation D15.
`go run ./tools/findingsdoc --check` runs in CI.

### Documentation split by audience

`docs/` now holds what someone running sbomb on their own build needs: getting
started, the configuration format, what findings mean and how to waive them,
the GitHub action, Windows, and this changelog. Everything that is only
relevant to working on sbomb moved to `docs/dev/`: the specification, the
milestone plan and phase plans, the recorded deviations, the open questions and
the dependency rationale. Both directories gained an index.

`docs/dev/sbomb-spec-v3.1.md` is now `docs/dev/spec.md`: the version belongs in
the document, not in a filename that every reference has to be updated for.

The universal gate was stated in two places that had already drifted apart --
the milestone index listed four commands, the one actually run has nine. It is
stated once now, in `docs/dev/README.md`, which is what CI points at.

### Package managers, introspection and response files (roadmap phase 7)

- `internal/adapters/pkgmanager` implements strategy 2 of section 19.2: a
  dependency a package manager installed becomes its own component, named and
  versioned by the manager rather than by a directory layout. The FetchContent
  adapter is the first, and it works with introspection off -- the tag and the
  repository come from the populate script CMake generates -- and improves with
  it, where the checkout's own git metadata reports the exact describe output
  and a dirty tree. Version confidence follows section 20.3, the purl is the
  generic form of section 20.4 carrying repository and commit, and the licence
  comes from the dependency's own file. No supplier is invented: section 20.5
  forbids deriving one from a repository host.
- Adapters never add a file to the used set. Discovery stays evidence-based, so
  a dependency that was populated but never linked does not appear.
- Conan and vcpkg adapters. Conan is read from the CMakeDeps files: the
  version from `<name>-config-version.cmake`, the package root from the
  per-configuration data file, and the licence Conan copied into the package.
  vcpkg is read from the SPDX document it writes per package, which states the
  name, the version, the licence and the purl outright.
- Package managers now register anchors of their own (section 21), which is
  what makes a `bom-ref` portable: a Conan cache path contains a random
  component, so without an anchor the identity of every file in an installed
  dependency would differ from machine to machine.
- Package and image manifests are evidence (section 18). What a manifest names
  as an input to an image or a package reaches the SBOM; what merely sits in the
  same directory does not, which is the distinction section 18 exists to draw.
  A `generatedFrom` entry links a generated asset to what produced it, and a
  generated asset that names nothing reports
  `MISSING_GENERATOR_INPUT_EVIDENCE` instead of having an input guessed for it.
  The CMake install manifest is read as a manifest kind of its own.
- `includeAssets` decides something. It was settable from three places and read
  from none.
- Git submodules are component boundaries (strategy 3 of section 19.2), read
  from `.gitmodules` because that is where the project declares them. Without
  introspection the boundary and the repository URL are known and the version
  is reported missing, since `.gitmodules` records neither a tag nor a commit
  and section 20.1 forbids guessing one; with `--allow-introspection=git` the
  checkout answers for itself. A submodule that nothing links does not become a
  component: section 19.4 lets git metadata describe a component but never
  expand the used-file set.
- Licence resolution follows section 22.2 in order: an SPDX identifier in a
  used file, then what the manager declared or placed in the package, then a
  licence file found by walking the component root.


- `internal/exec` is the only place sbomb can start a process. It carries the
  fixed allowlist of section 9.2, exact argument shapes with named slots, no
  shell, an environment reduced to `PATH`, a 30 second timeout, a 64 MiB output
  bound, path arguments validated against the registered anchors, and a log
  record per invocation. Off by default; enabled by `--allow-introspection`,
  `--allow-introspection=<groups>` or the `build.introspection` block.
- `internal/respfile` implements section 9.3: recursive `@file` expansion with
  a depth limit of 8 and a 64 MiB budget, and the two quoting rule sets --
  GNU, where a backslash escapes, and MSVC, where it does not unless it
  precedes a quote. The choice follows the toolchain the build evidence named,
  not the host, so a Windows path in a response file keeps its separators. The
  compile-database and Makefiles adapters had each grown their own expander;
  both now use this one, which is how they gained the MSVC rules and the size
  bound.

### Section garbage collection and LTO, verified against real toolchain output

Both were implemented in 0.7.0 but covered by unit tests only: no fixture
project built with `--gc-sections` or `-flto`, so `SECTION_GC_EXCLUDED` never
fired across the whole corpus. Two new corpus projects close that, and doing so
uncovered three defects in the implementation.

- Retention was decided from `LOAD` lines, and GNU ld writes one for every
  input it opens, including the ones it discards entirely. The memory map's
  placement lines are what say a section was kept; the map parser now reads
  them.
- Retention counted every section, including `.debug_*`, `.comment` and a
  zero-length `.eh_frame`. None of those reach the image, so no object in a
  build with debug information was ever fully discarded. Decided over
  image-contributing sections now; see deviation D12.
- `sectionGarbageCollection=exclude` removed the object but left the source it
  was the only chain to, because reachability was computed before the
  exclusion. The exclusion now breaks the chain, which is what discarding an
  object means.

`SECTION_GC_INFO_UNAVAILABLE` was also testing for evidence edges that are
never created, so it fired whenever the mode was on. It now reports what it
claims to: that the link evidence enumerates only one of the two halves.

### License detection worked on 1 file in 160

Normalization dropped every line beginning with `copyright`, which is not a
normalization at all: it depends on where the source wrapped its lines. The
Apache-2.0 text wraps "copyright notice that is included in or attached to the
work" onto its own line, so the clause was deleted and no digest could ever
match. The official SPDX text wraps the same words differently, so the two
sides disagreed even though both used the same function.

Only actual copyright *statements* are ignored now, recognized by what follows
the keyword -- a year, a `(c)` or `©`, or an unfilled SPDX placeholder -- so
the result no longer depends on line wrapping. Measured against 160 real
licence files found on the build host, detection goes from 1 match to 93.
Regenerating the digest table changed 86 of its 698 entries, which is how many
official texts contained a wrapped `copyright` line of their own.

## 0.7.0 - 2026-09-06

First numbered version. Seven of the nine roadmap phases are complete: the tool
produces an evidence-based, schema-validated, CRA-field-complete CycloneDX 1.6
document and gates on it, with header evidence taken from DWARF and dependency
files. What 1.0.0 still needs is adapter breadth -- package managers, packaging
and images, an SDK adapter -- and the performance budget of section 31.

sbomb is released under the MIT license.

### Header evidence in full

- DWARF line tables are the primary header source and dependency files the
  fallback, selected by `--header-evidence` / `policy.headerEvidence` in the
  three modes of section 4.4. Headers that DWARF narrowing removes are counted
  per component in the review report and listed by `--report-chains all`, so
  the narrowing is auditable instead of silent.
- Headers are classified into the seven classes of section 14.4 using the
  implicit include directories the toolchain reports through `toolchains-v1`,
  not a hardcoded path list. An unclassifiable header is included and flagged.
- Unity builds are detected and their constituent sources recovered by parsing
  the generated aggregation file's `#include` directives, which section 17.1
  permits explicitly, with the dependency file as fallback
  (`UNITY_SOURCE_UNRESOLVED` when neither works).
- Precompiled headers are handled per section 14.5: the aggregation header and
  its source are transient build artifacts, headers the build forces in are
  annotated `sbomb:evidence:header:viaPch` and carry medium confidence, and
  `--pch-headers=exclude` removes those that no compilation unit shows using,
  reporting `PCH_HEADERS_EXCLUDED` with a count.
- Link-time optimization downgrades object-to-source confidence by one level
  with reason `lto` (section 17.3), recorded in `attributes.confidenceDowngrades`
  as section 8.7 requires. `LTO_ATTRIBUTION_DEGRADED` is emitted when no debug
  information is available to attribute the result.
- `--section-garbage-collection` acts: `annotate` marks a fully discarded
  object `sbomb:evidence:link:fullyDiscarded` and downgrades its confidence,
  `exclude` removes it and reports `SECTION_GC_EXCLUDED`. An object counts as
  fully discarded only when the evidence enumerates both retained and discarded
  sections, so partial information can never remove a file.

#### Defects this uncovered

- The DWARF adapter had never produced a single header. It walked line
  *entries* and recorded their file, but a header that contributes only
  declarations produces no line entry; section 11.4 asks for the line-table
  *file table*, which does name it. The adapter was also not reachable from the
  binary at all.
- `ScopeOfPath` was being handed canonical identities instead of paths in the
  node-materialization path, which re-anchored `project:crypto.c` against the
  build root and recorded its scope as `build`. Visible in the Makefiles golden,
  where two project sources carried `sbomb:component:scope: build`.
- The fixture corpus never harvested the generated unity and precompiled-header
  files, so neither construct could be tested against real generator output.

### The policy model does its job

- All 27 options of section 33.1 exist and can be set from the configuration
  file or from a CLI flag, with the precedence of section 32.2: CLI flag over
  policy profile over configuration file over built-in default. One table maps
  a flag, a configuration key and the field it sets, so the three cannot drift
  apart. An invalid value is an error, not a silently ignored setting.
- The `host-linux` overlay and `--profile-overlay` exist. An overlay changes
  scope only, never a gate.
- Scope options are applied before output, as section 33.3 requires, and every
  exclusion is counted and reported. Toolchain and system components hang under
  a synthetic `build-environment` component rather than appearing as product
  dependencies (section 24.2).
- New findings give the remaining gates something to act on: `WEAK_EVIDENCE`,
  `MISSING_HEADER_DEPENDENCY_EVIDENCE`, `UNKNOWN_HEADER_CLASS`,
  `PREBUILT_LIBRARY_UNMAPPED`, `SECTION_GC_INFO_UNAVAILABLE`, and staleness
  detection from section 27.3.
- The review report has the nine sections of section 34: run metadata,
  deliverables, counts by kind and strength, components with their version and
  license provenance, unresolved items, staleness, findings by severity with
  waived ones separated, the policy verdict, and evidence chains selected by
  `--report-chains`.

### Fixed

- The `policy` block of the configuration file was read and then ignored
  entirely. Its fields are now pointers, so that a configuration can turn a
  profile gate off rather than only on.
- The review report embedded the directory the run read from, which put a
  temporary path into the golden file on every run.

### The CRA fields

- Files are mapped to components by the priority chain of section 19.2:
  curated `components[]` entries, then the nearest ancestor directory carrying
  a package manifest, then the anchor root, then an explicit `unknown:`
  component that is flagged for review. Which strategy decided a mapping is
  recorded in `sbomb:component:detectedBy`.
- Components carry a version, supplier, purl and license when an authorized
  source supplies one, and a finding when none does: `UNKNOWN_VERSION`,
  `MISSING_SUPPLIER`, `UNKNOWN_PURL`, `UNKNOWN_LICENSE`,
  `MISSING_COMPONENT_HASH`, `UNKNOWN_COMPONENT`. Nothing is inferred from a
  directory name, and a supplier is never derived from a repository host.
- License resolution follows section 22.2 and stays inside the component root,
  as section 22.1 requires. A disagreement between configuration and the
  component's own files keeps the curated value, records the other in
  `sbomb:license:conflictingValue` and reports `LICENSE_CONFLICT` rather than
  resolving it silently.
- `tools/spdxgen` generates the embedded license table from the official SPDX
  list: 698 digests of normalized license texts, with current identifiers
  preferred over deprecated ones. `--check` detects drift, and CI reports it
  without blocking.

### Fixed

- `componentmap.MapFile` always reported a match, answering "unknown" when no
  rule applied. That made curated configuration the only mapping strategy that
  could ever run, because no later strategy was ever reached.
- Hashing resolved a file identity as if it were a filesystem path, so
  `project:main.c` was looked up relative to the working directory and nothing
  was ever hashed. The caller now supplies the identity-to-path mapping.
- Hashing runs on a bounded worker pool with order-independent results, as
  section 23 requires; it was sequential.
- The findings JSON used Go field names instead of the normative keys of
  section 26.1, so a consumer looking for `id` or `severity` found neither.
- `UNANCHORED_FILE` was reported once per mention of a path rather than once
  per file.

### A document a consumer can use

- The SBOM now has a root component in `metadata.component` describing the
  deliverable, grouping components the files belong to, and a dependency
  cascade in which every `bom-ref` appears exactly once, even when it depends
  on nothing (section 28.5). `bom-ref` values follow the scheme of section
  28.4, with the normative `slug()` normalization.
- Every component carries the three properties BSI TR-03183-2 requires:
  `sbomb:cdx:archiveProperty`, `sbomb:cdx:executableProperty` and
  `sbomb:cdx:structuredProperty`, derived from the file class (section 1.5(3)).
- Documents are validated in two layers before the temporary file is renamed
  into place (section 32.5): the official CycloneDX 1.6 JSON Schema, embedded
  with `go:embed`, and the semantic checks a schema cannot express. Either
  layer failing is exit code 4.
- Discovery hands a format-neutral `sbomwriter.Document` to a registered
  writer (section 36.1). It carries no `bom-ref` strings and no CycloneDX
  property names; the writer derives both.
- Added `sbomb validate --input <file>` and `sbomb evidence --build-dir <dir>`,
  and `sbomb schema --cyclonedx` prints the embedded schema.
- `vendor/` is committed and `go build -mod=vendor` is verified in CI, so a
  build needs no network (section 37.3). `docs/dev/dependencies.md` records what
  each direct dependency does and what removing it would cost.

### Fixed

- The dependency-closure check compared the component references against
  themselves and therefore proved nothing. It now checks that every reference
  has an entry in `dependencies[]`, as section 28.5 requires.
- Hashes were recorded under the algorithm name `sha256`, which CycloneDX does
  not accept; the schema layer caught it on its first run. The spelling is now
  `SHA-256` in both the inventory dump and the SBOM.
- `pathmodel.Slug` did not implement the normalization of section 28.4: it
  neither lowercased nor replaced characters outside `[a-z0-9._-]`.
- Internal annotations that discovery leaves on a file no longer reach the
  document; only the property catalogue of appendix B does.

### The output is derived from the evidence graph

- Final deliverables are resolved per section 5: configured artifacts win, and
  automatic discovery is limited to installed executable targets reported by
  the CMake File API, never to the newest or only binary in the build tree.
  Debug byproducts such as `app.pdb` are not deliverables.
- Link evidence is read per section 11.2 with deviation D1: the linker
  dependency file names what the link consumed, the map names which archive
  members were extracted, and a Makefiles `link.txt` reconstructs the link
  command when neither exists.
- Extracted archive members are traced to the build-tree object they were
  archived from, so a member the linker never extracted has no path to the
  artifact and does not appear (section 12).
- Objects are resolved to sources through the strategies of section 13.2, and
  headers are attached from the Ninja deps log and Makefiles dependency output.
- The used-file set is now what is reachable from a deliverable. The same
  project built with `gcc-ninja`, `gcc-make`, `clang-ninja`, `arm-none-eabi`
  and `mingw-w64` yields the same three files, and `unused.c` -- compiled into
  the archive but never extracted -- appears in none of them.
- Graph invariants are checked before anything is written; a violation is an
  internal error with exit code 70.

### Fixed

- The linker map parsers were unusable against real map files. They scanned
  every line for anything containing a file extension, so GNU ld linker-script
  wildcards such as `*crtbegin.o(.ctors)` and lld section placements such as
  `foo.o:(.text)` were reported as archive members, and a single member was
  reported once per section it contributed. The parsers are now section-aware:
  the archive-member block, the as-needed block, the discarded-section block
  and the memory map are each read for what they actually contain, and lld's
  input column is parsed as a column. Sniffing now distinguishes GNU ld from
  gold by the memory map, which recent binutils is the only way to tell apart.
  `LOAD linker stubs`, which GNU ld writes for ARM veneers, is no longer
  recorded as a file.
- The Ninja deps log parser never stripped the NUL padding from path records:
  its trim condition was already false on entry. Every path whose length was
  not a multiple of four carried a trailing NUL, so header evidence never
  matched anything.
- Findings about the build no longer carry the directory the run happened to
  read from, which put a temporary path into every golden file.
- `LINKED_OBJECT_SOURCE_UNRESOLVED` is no longer reported for the C runtime
  startup objects, which section 24.1 excludes from the SBOM anyway.
- Paths that an adapter resolved against the directory being read are mapped
  back to the logical build root before being identified, so evidence from the
  Makefiles adapter anchors the same way as evidence from a map.
- `evidence.json`, which is sbomb's own output, was committed into the fixture
  corpus; the determinism script now works on a copy.

### Anchors and scope

- Added the section 7 anchor model: a registry with the registration order of
  section 7.4, longest-prefix matching at path-segment boundaries,
  case-sensitive matching under the POSIX flavor and case-insensitive under
  Windows, and the `abs` fallback with an `UNANCHORED_FILE` finding.
  `--redact-unanchored-paths` replaces unanchored identities with a digest.
- `toolchain:` anchors are registered from `toolchains-v1`, `sysroot:` from the
  cache or a `--sysroot` compile flag, and `extern:`/`sdk:`/`pkg:` from the
  configuration's `anchors[]`.
- Files are classified by origin scope and carry `sbomb:component:scope`.
  Toolchain and system files are excluded from the SBOM by default per section
  24.1, using the compiler-reported implicit include and link directories
  rather than a hardcoded path list. The dynamic loader in `/lib64` is covered
  by a conventional-root fallback, because no compiler reports that directory.
- Relative paths are now resolved against the directory they were recorded
  against -- a compile database entry against its `directory` field -- instead
  of being left unanchored.
- Identity uses the logical build path from the evidence (section 7.6) rather
  than the directory the evidence happens to be read from.

### Fixed

- `cmakeapi.ParseReplyDir` could not read a real reply directory. It expected
  the filenames `codemodel-v2.json`, `cache-v2.json` and `toolchains-v1.json`,
  while CMake writes content-addressed names listed in `index-*.json`, and it
  read `targets[]` from the codemodel although target detail lives in one file
  per target. It now follows the index, loads each target file, and exposes the
  source and build roots, install rules, link fragments and the implicit
  include and link directories. Reply filenames are rejected unless they are
  plain names, so a crafted index cannot read outside the reply directory.

### Fixture corpus

- The golden corpus is now generated from real toolchain output.
  `tools/fixtures/regen.sh` configures and builds five CMake projects for
  `gcc-ninja`, `gcc-make`, `clang-ninja`, `arm-none-eabi` and `mingw-w64`,
  then harvests only build evidence. Builds run under the sentinel roots
  `/__fixture_src__` and `/__fixture_build__`, so binary evidence
  (`.ninja_deps`, DWARF) carries portable paths natively.
- The previous corpus contained placeholder text such as
  `link trace for gcc-13/p02-static` in place of linker output.
- Toolchain directories were renamed to describe the build configuration
  rather than a compiler version: `gcc-13`/`gcc-12` to `gcc-ninja`,
  `gcc-12-make` to `gcc-make`, `clang-17` to `clang-ninja`. The hand-written
  `portable` fixture was replaced by real `gcc-ninja/p02-static` evidence plus
  `testdata/config/portable.json`.

### Fixed

- The Makefiles adapter resolved every object to `compiler_depend.ts` instead
  of its source. `looksLikeSource` was a blocklist that accepted any
  prerequisite that was not an object, and repeated `build.make` rules for one
  object overwrote an already resolved source. Source classification is now an
  allowlist of the compiler inputs in section 14.6, and the first resolved
  source per object wins.
- The tool version was hard-coded in three places with three different values
  while `scripts/release.sh` injected a fourth. All of them now read
  `internal/buildinfo.Version`, which release builds override via `-ldflags`.
- A global `-v` before a subcommand was appended to the subcommand's arguments
  and rejected as unknown, so `sbomb -v explain ...` failed. Verbosity is now
  parsed once and passed down.
- Tests wrote `evidence.json` into the committed corpus. They now operate on a
  copy via `testutil.CorpusBuildDir`.

### Added

- `.github/workflows/ci.yml` runs the universal gate (build, vet, test,
  gofmt), the race detector, the corpus checks and the end-to-end CMake test
  on every push and pull request. Previously only the determinism script ran.
- Golden files can be regenerated with `go test ./cmd/sbomb -update`, and test
  binaries pin the reported tool version so releases do not rewrite them.

### Removed

- `internal/findings`, an unused duplicate of `domain.Finding` whose
  `IsWaived` always returned false.
- `internal/adapters/linkers/{gnuld,gold,lld,msvc}`, four eleven-line packages
  that called `mapparser.Parse` with a fixed format string. The `msvc` one also
  contradicted milestone 18 being parked.
- Two `init()` functions that existed only to silence unused imports, and the
  unused `cyclonedx.versionString`.
