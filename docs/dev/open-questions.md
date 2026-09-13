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

## Q11 — reserved

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

## Q13 — reserved

**Settled: no, and it turned out not to be needed.** The question asked whether
§9.2 should permit a command that takes a caller-supplied *revision* in an
argument slot, so that the distance could be measured from a recorded revision
rather than from the nearest reachable tag. It should not, and the comparison
the question was after is reachable without it.

Two observations closed it. The first is that the comparison never needed a
distance: what says whether a checkout is the declared state is whether its
commit **is** the declared one, and equality of object names is not a question
about history. The second is that resolving a declared tag to its commit can be
done with `git show-ref --tags`, whose argument shape is as fixed as every other
entry on the allowlist — the tag name is matched inside sbomb, against the
answer, rather than handed to git. No revision slot, no grammar to validate, no
security argument to write.

What was built instead is in §19.4: a package manager's declared revision is
kept as it was written, beside the commit the checkout reports and never
collapsed into it, and the two are compared. `git describe`'s distance survives
as a refinement — it is reported only where the tag describe answered with is
the declared one, because a count against any other tag says nothing about the
declared revision.

The residual is deviation D47, and it is a different residual than before: not
"the distance is measured from the wrong tag" but "there are components nobody
declared a revision for", which is a gap in the evidence rather than in the
rule. The number stays reserved so that references to Q14 and later keep their
meaning.

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

## Q16 — reserved

**Settled: all three reach the report.** A waived finding carries a `waiver`
object with `reason`, `approvedBy` and `expires`, in `findings.json` and in
both review records. Decision Q11 of the FOSS plan had asked for exactly that,
and the question recorded why F7 left it: carrying the approver meant widening
`domain.Finding`, which is the findings JSON, whose field names appendix A is
normative about.

Widening it was the right call, and the shape the question sketched is the one
that was built: one object rather than three fields beside `waived`. Three
fields would have carried the reason twice, since `waiverReason` was already
there; the object replaces it. An audit asks for the approver before it asks
for the reason -- a reason is an assertion, and the approver is the person
answering for it -- so an acceptance decision that records who accepted it
nowhere is the wrong half to keep.

Removing a field is what `schemaVersion` exists for, and the findings document
is at 2. The number stays reserved so that references to Q17 and later keep
their meaning.

## Q17 — reserved

**Settled: the flag was built.** `generate --foss-format text|markdown`
selects the rendering `--foss-out` writes, with the same two values and the
same default as `foss --format`. It is spelled `--foss-format` because
`--format` on `generate` already names the SBOM writer.

The question had recorded the flag as absent because no milestone asked for
it, which is a reason to leave surface out and not a reason a user can act on.
§32.6 requires the two entry points to produce byte-identical files for the
same build, and a rendering only one of them could reach was the closest thing
to a contradiction of that: the same discovery, the same renderer, and one of
the two callers unable to choose. Nothing had to be built for it — the
`Format` field was already on the struct both entry points fill
(`cmd/sbomb/foss.go`), and `generate` was the only caller leaving it empty.

`--foss-format` without `--foss-out` is a usage error rather than a silent
no-op, which is what §32.2 says about a flag that cannot be honoured. The
number stays reserved so that references to Q18 and later keep their meaning.

## Q18 — reserved

**Settled: the smoke-test workflow already ran the action, and now runs its
attribution step too.** The question had weighed a shared argument script
against a production input that skips the download, and both were answers to a
premise that was wrong: `.github/workflows/smoke-test.yaml` calls the action
for real (`uses: ./.github/actions/sbomb`), on two operating systems, against a
published release. It asked for the SBOM step alone, so the attribution step
was the only part of the action nothing executed.

`foss: "true"` in that call, and a step that checks the four documents of
§32.6 are there and not empty, is the whole change. The step cannot fail the
workflow -- attribution is informational and the action marks it
`continue-on-error` -- so the files are what says it ran.
`TestTheSmokeTestExercisesTheAttributionStep` asserts the workflow still asks
for it, because dropping the input would remove the coverage silently.

The residual is what the smoke test is: it runs against a **release**, not
against the working tree, and it is called by the release workflow once the
release exists. A break in the action's own shell is therefore found when a
release is made and not in the pull request that caused it. Closing that too
would mean testing a binary the release does not carry, which is the
contradiction the question started from. The number stays reserved so that
references to Q19 keep their meaning.

## Q19 — reserved

**Settled: the third way, and it cost no format change.** The question listed
three, and the first two were the ones decision Q20 had already refused:
component nodes in the evidence dump, which is a format change to a file other
tools read and makes the dump depend on the layer above it; and `explain`
running discovery of its own, which would stop it being a reader and let it
answer about a build nobody made.

The third was to read the document beside the dump, and it holds up. §28's
dependency cascade gives every grouping component a `dependsOn` list of its
`file:` refs, and a file bom-ref is its identity behind that prefix — the key
the graph uses. It was checked against every golden document before it was
built: nine documents, every grouping component carrying its files, none
missing, and the bom-ref `component:<name>` makes the name unique per document
so the expansion is a lookup rather than a search.

`explain --component <name> --sbom <file>` expands the name and explains each
file. The document is named rather than guessed, because a build directory
holds any number of them. Three refusals say which of three things went wrong:
no document named, no such component in it, or none of its files in this dump —
the last meaning the two describe different builds, which is not the same
answer as an absent chain.

What is left is that one subcommand reads two files where the other subjects
read one. That follows from where the two facts are, and deviation D46 records
it.
