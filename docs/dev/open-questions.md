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

One cost is already settled and is not in question: a committed build directory
is always stale. Git restores no modification times, so the sources a checkout
writes are newer than the artifacts it writes beside them, in whatever order the
checkout happened to use. Section 27.2 signal 4 is mtime comparison, so
`STALE_BUILD_EVIDENCE` (severity `error`) is correct and unavoidable for the
corpus, and the `default` policy gate fails on it with exit code 3. Every run
against a fixture therefore passes `--policy lenient`, tests included; an
acceptance command that expects exit 0 from a corpus fixture needs it too.

It is a property of the corpus and not of a build directory in the field, where
the artifacts are newer than the sources that produced them. Suppressing the
finding for fixtures would disable the one staleness signal the corpus can
exercise, so the flag is the answer rather than a special case in the detector.

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

## Q4 — Does the Makefiles adapter apply to the NMake Makefiles generator?

§1.3 lists NMake Makefiles as supported. `internal/adapters/make` selects on the
presence of a `Makefile` and then reads `CMakeFiles/<target>.dir/build.make`,
`link.txt` and `compiler_depend.make`. The NMake generator writes files of those
names, but with NMake's syntax and quoting rather than GNU make's, and nothing
has ever run the adapter against one.

So the support claim rests on a resemblance. Milestone 18A resolves it by
measurement: either the adapter reads an NMake tree unchanged, or §1.3 is
corrected, or the adapter learns the difference. Guessing which before a fixture
exists is what this file is for.

## Q5 — What is the Windows sentinel root?

`tools/fixtures/regen.sh` builds under `/__fixture_src__` so that every captured
file records a portable path natively. A Windows path is drive-qualified, so the
sentinel has to be something like `C:/__fixture_src__`, and
`TestFixturesContainNoHostPaths` — which rejects `C:\Users` — has to tell a
sentinel drive letter from a host one.

The alternative is a substituted drive (`subst S: ...`) so the sentinel is a
letter of its own, which keeps the tests' rule simple at the cost of a step the
runner has to perform before every regeneration. Decided in milestone 18A, when
there is a fixture to try it on.

## Q6 — How does a Windows fixture reach the repository?

Every fixture in the corpus is committed, and `ci.yaml` says regenerating it
"needs the pinned toolchains of the devcontainer and is therefore a developer
task". A developer with a devcontainer has GCC, Clang and mingw; nobody has
`cl.exe` there, and CI does not commit.

So the Windows corpus needs a route the Linux corpus never needed: a
`workflow_dispatch` job that runs the Windows half of `regen.sh` on
`windows-latest` and uploads the result as an artifact, which a developer then
commits and CI thereafter only checks. The alternative -- requiring a Windows
machine with Visual Studio to contribute -- puts the corpus out of reach of the
developers who have one of everything else.

What this does not settle is trust: a fixture built by a workflow and committed
by hand is evidence nobody watched being produced. The `PROVENANCE.md` rule of
milestone 0 is what carries that weight, and it may need the workflow run URL
added to it.

## Q7 — reserved

The FOSS attribution plan (`docs/dev/foss/decisions.md`, on its own branch)
refers to Q7 by number: two versions of one package in a single assembly,
milestone F7. The number is held so the reference keeps pointing at what it was
written for.

## Q8 — Should anything but the source root be relocatable?

Section 7.9 gives the source tree a logical and a physical root, so a build
directory restored in a second CI job can be read. Two kinds of path are
deliberately left out, and both are read from files the build wrote:

* a **package-manager cache** — Conan reads its package folder out of the
  per-configuration data file its CMakeDeps generator wrote into the build
  directory (`internal/adapters/pkgmanager/conan.go`), which is a build-machine
  path, and vcpkg is the same shape;
* a **toolchain or sysroot root**, which the File API reply reports as the
  compiler found it.

The general form that covers all of them is one mechanism, and it is a known
one: a list of prefix replacements, exactly what `-ffile-prefix-map` writes into
a binary and what a debugger's `substitute-path` consumes. It would be a
configuration array of `{from, to}` pairs applied to every path before it is
read, with identity still computed against the recorded path.

It was not built, for one reason: **nothing needs it yet.** sbomb normally runs
right after the build on the machine that built, where every one of those paths
exists. The source root is the exception only because the fixture corpus cannot
be tested without it — the corpus commits sources and no caches — and a feature
justified by its own tests stays as small as it can. The other setup that would
need it, a project compiling with `-ffile-prefix-map`, shows up today as missing
file hashes rather than as a wrong answer.

What would settle it: a real project where a package cache is not where the
build left it. Two questions have to be answered with it, and neither can be
answered from here. Whether the replacements are ordered or longest-match, since
a cache root often lies inside a home directory that is itself remapped. And
whether a replacement may introduce a path that no anchor covers, which section
30.4 refuses for a symlink and would have to refuse here for the same reason.

## Q9 — reserved

**Settled: it does not, and it turns out not to need to.**

`tools/fixtures/regen.sh` turns every `dep/*/` of `p14-foss` into a repository
with a fixed identity, date and tag, and leaves one of them dirty. The harvest
skips `.git`, so the committed corpus carries none and every fixture component
reports `unknown` there. Three routes were considered for changing that, and
two facts were measured while F1 was written: git will not put a path with a
`.git` component into its index at all, so any committed repository needs
renaming and a materialization step; and a harvested `.git/index` stores each
entry's `ctime`, `mtime`, `dev` and `ino`, which are drawn fresh on every
regeneration and would put back exactly the churn `tools/fixtures/replynorm`
was written to remove.

Neither cost has to be paid. What the corpus already commits is the two trees
the history is made of: `tools/fixtures/projects/p14-foss` is what the tag
names, and `testdata/fixtures/p14-foss-src` is what was compiled. A test
rebuilds the repositories from them with regen.sh's own identity and date, so
every commit hash is the same on every machine — `internal/testutil` copies a
tree with a fixed `0o644`, so the tree entries do not depend on the checkout's
permissions either.

Both things the question was still costing are closed by that:

* **The tri-state over the corpus's own build evidence**, through the source
  tree relocation of §7.9. `cmd/sbomb/modification_acceptance_test.go` runs
  `generate --build-dir <corpus> --source-dir <materialized tree>` and asserts
  the answers come from the relocated copy: `lgpl-lib` modified by its dirty
  tree, `apache-lib` modified by standing a commit past its tag, `mit-lib`
  unmodified on its tag, each with its `pedigree`.
* **A document that shows what a settled status looks like.**
  `testdata/golden/gcc-ninja-p14-foss-modified.cdx.json` carries all three
  states and seven commit uids. Every other golden shows `unknown`, which is
  the honest answer for a corpus with no repository and tells a reader nothing
  about the shape of the others.

So the corpus keeps no `.git`, `regen.sh` is unchanged, and the goldens of the
ordinary run still say `unknown` — which remains correct, because there is
nothing there to read. The test that needs a repository builds one out of what
is committed. The number stays reserved so that references to Q10 and later
keep their meaning.

---

## Q10 — reserved

**Settled: `license.id`.** §28.7 orders the two encodings by what the value is,
and a known SPDX identifier is the more specific statement, so a component
resolved to the bare identifier `MIT` is published as
`{"license": {"id": "MIT"}}`. A compound expression stays an `expression`, and
so does an identifier the SPDX list does not carry, because `license.id` is an
enum in both schemas; the membership test reads that enum out of the embedded
SPDX schema that validates the document. The cost this question was raised
about turned out to be three goldens, all of them the FOSS fixture's. The
number stays reserved so that references to Q11 and later keep their meaning.

## Q11 — Does `FOSS_LICENSE_TEXT_MISSING` apply to the manufacturer's own code?

The finding is defined for a component with a resolved licence identifier and no
retained text. The FOSS plan's own table narrows it to a **distributed**
component. F4 emitted it for every component the criterion matched, because the
distribution role was F6 work and did not exist yet; since F6 the criterion is
`DistributionRole == distributed` (`internal/generate/components.go`).

On the corpus that is exactly one component — `p14-foss`'s own application. It
declares `MIT` in `src/main.c` and carries no licence file at its root, so the
finding says the document's attribution material cannot be assembled from what
sbomb saw. That is *true*, and arguably the most useful instance of it: a
manufacturer shipping MIT code without its own LICENSE file has a real gap.

But F7 builds `THIRD-PARTY-NOTICES.txt` from `type != application` (decision
Q12), so the component this fires for is the one the notices document leaves
out — and a finding whose subject never reaches the document it is about reads
as noise in a CI log.

Three answers, none of them obviously right:

* leave it as it is, and let a manufacturer's own missing LICENSE be reported;
* restrict it to `type != application`, which makes the finding agree with the
  document but stops reporting the one gap an auditor would ask about;
* restrict it to `distributed` components once F6 exists, which is what the
  plan says, and which is a third criterion again — a build-time-only code
  generator under GPL is not in the notices document either.

**Settled in F6: the third answer, which is what the plan says.** The finding is
restricted to a `distributed` component (section 24.5), and
`FOSS_COPYRIGHT_MISSING` is restricted the same way — section 22.10 said it
asked every mapped component only "until the distribution role of §24.5 is a
resolved fact", and it now is. A build-time-only code generator with a licence
identifier and no retained text is not an attribution gap: nothing of it is
shipped, so there is no notice to reproduce and no source request to answer.

The first two answers were both rejected for the same reason. Leaving it as it
is would keep reporting a gap for components the export does not describe, and
restricting it to `type != application` would agree with the notices document at
the price of never reporting the one gap an auditor asks about — a manufacturer
shipping MIT code with no LICENSE file of its own.

What this changes on the corpus is nothing, which is the point: `p14-foss`'s own
application is `distributed`, so the finding still fires for it, and the
criterion that was chosen is the one that also excludes the GPL-2.0 generator
the moment a fixture puts one in a document. It was narrowed on a criterion, not
on an observation.

---

## Q12 — reserved

**Settled.** The fixture's LGPL and GPL texts had lost every opening angle
bracket of the placeholders in their "how to apply this licence" appendix, so
`internal/license/copyright.go` could not recognize them as placeholders and
attributed the appendix to the component as a copyright notice. The texts now
carry the rendering of the embedded SPDX templates, whose spacing differs
between the two licences and is not normalized, and
`TestFixtureLicenceTextsCarryBalancedPlaceholders` asserts that no fixture
licence text closes a placeholder it never opened. The number stays reserved so
that references to Q13 and later keep their meaning.

## Q13 — Should `§9.2` permit `git rev-list --count <upstream>..HEAD`?

**Narrowed. The premise this question was written on was wrong.** It said that
§9.2 permits no command that counts commits past a tag. It does:
`git describe --tags --always --dirty`, which the allowlist already carries and
which §19.4 already calls, answers `<tag>-<count>-g<hash>`. The count was being
matched and discarded. Since deviation D47 it is read, and a clean checkout
standing past its tag is `modified` rather than `unknown` — with no change to
the allowlist at all.

What is left is narrower and still real: the count is measured from the
**nearest reachable** tag. A maintainer who tags their own fix (`v1.2.0-acme1`)
gets distance zero and therefore `false`, although the component is modified
relative to the release it was pinned to. Measuring from a *recorded* revision
is what would need `rev-list`, and that is where the original cost applies:

**What it costs is not the command, it is the allowlist.** §9.2 is a security
boundary and the table in `internal/exec/exec.go` is its enforcement: five
shapes the specification lists are absent because nothing could call them
without guessing (deviation D29), and two more were removed because the
permitted shape did not answer the question (D30). `rev-list` is the first entry
that would take a **caller-supplied revision** in an argument slot rather than a
path. A revision is not a path, so `checkPath` does not apply to it, and a tag
name comes out of a repository sbomb was pointed at — which is untrusted input
by §30. `git rev-list --count <x>..HEAD` with a hostile `<x>` is not known to be
exploitable, and "not known to be" is not the standard an allowlist is held to:
the argument would have to be that the revision slot is validated against a
grammar before it is passed, and that grammar would have to be written and
tested.

Two things would then have to land together:

1. a revision slot in the allowlist table, with validation of its own —
   `PathSlots` has no equivalent for revisions today;
2. the security argument in §9.2, stated rather than assumed.

The third thing the question used to list — a fixture with a repository in it —
is no longer a precondition for the *rule*, because the rule is covered by
tests that build a repository from real git in a temporary directory
(`internal/generate/modification_test.go`). It remains a precondition for
seeing any of this in a golden document, which is Q9.

---

## Q14 — Two of section 16's three generator-input sources are still unread, and no source answers for the Makefiles generator

Section 16 lists three sources of generator-input evidence in priority order:
CMake File API custom-command `dependencies`/`byproducts`, Ninja `build` edge
inputs for the generating rule, and a depfile the generator emitted. **Source 2
is now implemented** (`internal/generate/generator.go`, deviation D45) and with
it the case the whole attribution track turns on: `p14-foss` compiles
`dep/gpl-gen/table_gen.c` into a `table_gen` executable, runs it during the
build and links the `generated/table.c` it produced, and the document now
carries `component:gpl-gen` as `GPL-2.0-only`, `build-time-only`, `build-tool`,
`scope: excluded` — a GPL-2.0 generator described and excluded rather than
unseen. What remains open is the other two sources and, more importantly, the
gap between build systems.

**Source 1 cannot be read as written.** The CMake File API's codemodel has no
custom-command object: a generated file appears among a target's sources with
`isGenerated`, and nothing in the reply says what produced it. Either the
section names something the File API does not have, or it means the
`backtraceGraph`, which records where in the CMake code a source was added and
not what generated it. Settling that is a reading of the File API schema, not a
decision.

**Source 3 needs a build that emits one.** A generator's own depfile is read
the way any depfile is; what is missing is a fixture whose generator writes
one, and a rule for associating it with the output (`--depfile` is the
generator's own flag, and sbomb cannot know it was passed).

**The gap that matters is the Makefiles generator.** It records the same
dependency — `generated/table.c: table_gen` in `CMakeFiles/<target>.dir/
build.make`, which the Makefiles adapter already reads line by line for object
sources and archive inputs — and section 16 lists no source that covers it. The
consequence is visible in the corpus and asserted by the tests: built with
Ninja, `p14-foss` carries the generator as a component; built with Make, it does
not and `MISSING_GENERATOR_INPUT_EVIDENCE` says why. One project, two build
systems, two used-file sets — which is exactly the property
`TestEvidenceChainYieldsTheSameFilesAcrossToolchains` exists to defend for a
project without a generator.

Adding it means amending section 16 with a fourth source, and deviation D20
refused a fourth source once before — for a different reason, an unverifiable
assertion from a configuration file, where this one would be a build system's
own rule file. The work is:

1. a generic output-to-prerequisites map from `build.make`, which the adapter
   discards today;
2. a rule for the bookkeeping prerequisites Make carries that Ninja does not —
   `flags.make`, `compiler_depend.ts`, `link.txt` stand beside the object in
   the same rule, and none of them is a generator input;
3. the amendment, because `build.make` is evidence section 16 does not list.

Item 2 is the whole difficulty: with no rule for it, a generator input list
would name CMake's own bookkeeping files as inputs of the product, and a
suffix list is the kind of heuristic section 19.2 exists to keep out.

---

## Q15 — reserved

**Settled: what the component states about itself is enough; the rest is
reported.** The threshold is not "two distinct identifiers" -- that would put an
info finding on every dual-licensed dependency there is, `dep/multi-license`
included, which resolves to `MIT OR Apache-2.0` and has already said what it is.
It is the identifier the component's own resolved expression does not account
for: §22.2 stops at the first identifier it reads, so a directory assembled from
two upstreams resolves to one of them and says nothing about the other.
`FOSS_PER_FILE_LICENSE_DIVERGENCE` (info) names what was left over, and §22.5
separates it from `LICENSE_CONFLICT`: two sources disagreeing about one licence
is a conflict, two upstreams in one directory is not -- both readings are right,
and the component has two licences rather than a disputed one.

The malignant case the question asked for is `p14-foss`'s `dep/vendored-mix`:
its `LICENSE` and its own source say MIT, and one file copied in from elsewhere
still declares `GPL-2.0-only`. The component resolves to MIT, its obligations
are MIT's, and the finding is the only thing that says a GPL file is in the
deliverable. The number stays reserved so that references to Q16 and later keep
their meaning.

## Q16 — The review record shows a waiver's reason and not its approver

Decision Q11 says a waived FOSS finding "keeps its reason, approver and expiry
in the report". `foss-review.txt` prints the reason, because that is what the
finding carries: `policy.Evaluate` copies `waiver.reason` onto
`domain.Finding.WaiverReason` and drops `approvedBy` and `expires`
(`internal/policy/policy.go`). The approver and the expiry are in the waiver
file, which is committed beside the configuration.

Carrying them would mean widening `domain.Finding` — and therefore the findings
JSON, which is a format other tools read (appendix A is normative about its
field names). That is a change to a consumed format for information that is one
file away, so F7 left it alone. If an auditor wants the approver in the record
rather than in the waiver file, the change is a `waiver` sub-object on the
finding rather than three more top-level strings, and it belongs with whatever
else widens that schema next.

## Q17 — `generate --foss-out` writes the text rendering and cannot select markdown

§32.6 gives `sbomb foss` a `--format text|markdown` and gives `generate` only
`--foss-out <dir>`. Milestone F7 lists exactly that flag surface, so `generate`
has no rendering flag: `--foss-out` writes the text rendering, and a user who
wants the markdown one runs `sbomb foss`. An earlier draft of F7 did add
`--foss-format` to `generate`; it was removed as surface no milestone asked
for.

The asymmetry is real rather than principled. `--review-report` has
`--report-format` beside it, which is the same shape of question, and a CI job
that produces the SBOM and the notices in one run is exactly the case where the
rendering would be chosen. Against it: every flag on `generate` is a flag
forever, the renderings are house style anyway (§32.6), and the two entry
points are one code path — so whatever is decided must keep them
byte-identical, which is asserted today for the text rendering only.

Deciding it needs no evidence and no fixture, only a view on whether `generate`
should carry a second output's rendering flag. Recorded here so that the
asymmetry is a decision and not an oversight.

## Q18 — Nothing tests the composite action end to end

Milestone F8 lists "an Action run on the fixture uploads four files and exits 0
even when components are incomplete" as a test. It cannot be run from this
repository. A composite action needs a runner, and its first step downloads a
released binary — so an end-to-end run would test a release that does not yet
carry the command under test, and on the very first release that does, the
workflow would be testing the published artifact rather than the tree.

What exists instead is the property split in two, which is what the `foss-out`
and `foss` steps were built to make possible:
`TestTheActionsAttributionStepProducesFourFilesOnAnIncompleteFixture` runs the
command the step runs and asserts the four files and exit 0 on a fixture whose
notices document carries the incompleteness marker;
`TestTheActionCannotGateTheBuildOnAttribution` reads `action.yaml` and asserts
the input, the condition, the upload and `continue-on-error` on both steps; and
the `foss-outputs` CI job runs the first half outside the test binary.

The seam nothing checks is that the shell in `action.yaml` assembles the same
arguments the test does. Two ways to close it, neither obviously right:

* Have the action call a script committed here (`scripts/foss.sh`) that the
  test also calls, so there is one argument assembly. It costs a file and makes
  the action depend on a checkout of this repository, which composite actions
  otherwise do not need.
* Run the action in CI against a locally built binary, by giving it an input
  that skips the download. That is a production flag existing only for a test,
  which §32 rejects elsewhere.

Deciding it needs no evidence, only a view on how much a composite action's
shell is worth insuring. Recorded so that the gap is a decision rather than an
oversight.

## Q19 — `explain --component` answers nothing, and fixing it means changing `evidence.json`

§32.3 specifies three subjects for `explain`:

```
sbomb explain --build-dir build/debug --file dep/mbedtls/include/mbedtls/aes.h
sbomb explain --build-dir build/debug --component mbedtls
sbomb explain --build-dir build/debug --bom-ref file:project:src/main.cpp
```

The three flags exist, and all three are the same thing: `--file`,
`--component` and `--bom-ref` set one `subject` string, which is looked up as a
node id in the evidence graph loaded from the dump (`cmd/sbomb/main.go`,
`handleExplain`). The graph carries source, header, object, archive and
artifact nodes. It carries **no component nodes** — component mapping happens
above the graph, in `internal/componentmap` and `internal/generate`, and
nothing writes its result back into it. So every component name answers `no
evidence chain for <name>` and exits 1, on every project. Verified on
`p14-foss`: `--component mit-lib` fails while `--file
project:dep/mit-lib/src/mit_a.c` prints the archive-member and link hops to
`artifact:build:fossapp`.

It surfaced while writing `docs/foss.md`. Decision Q20 declined a FOSS mode for
`explain` on the grounds that "is this library really in our product?" is
already answered by `explain --component`; it is not, and the documentation
names `--file` instead, which is true and which the review record's identifiers
line up with.

What a fix costs is exactly what Q20 refused to spend. The subject has to
resolve to a set of file nodes, which means either:

* **Components in the dump.** Appendix C gains component nodes, or a
  component-to-files index beside the nodes. That is a format change to a file
  other tools read, and it makes the dump depend on the component mapping — a
  layer above it (§35).
* **`explain` runs discovery.** It would stop being a reader of the dump, and
  a second run on a tree that has changed answers about a build nobody made.
* **`explain` reads the SBOM.** The document already states the mapping: the
  §28 dependency cascade gives every grouping component a `dependsOn` list of
  its `file:` refs, so `explain --component mit-lib` could take the document
  beside the build directory, expand the name to those file ids, and explain
  each of them from the dump. It needs no format change and no second
  discovery, but it makes one subcommand read two files, and it answers nothing
  where no document was kept.

None of the three is obviously right, the third is cheap, and the flag is
specified — so this is a defect with a real cost of repair rather than an
oversight.

**Two thirds of it are fixed; the component subject is what is left.** §32.3
spells its other two examples as a path relative to a root and as a `file:`
bom-ref, and neither reached a node either, because the graph is keyed by
canonical identity. `explainSubject` (`cmd/sbomb/main.go`) now takes the `file:`
prefix off a bom-ref and offers a relative path to every anchor of the dump,
naming the candidates rather than choosing when two anchors carry one path. So
both documented forms work, and the third says what it cannot do instead of
answering "no evidence chain" — which read as "that component is not in the
product". Deviation D46 records the gap that remains.
