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

## Q9 — How does a fixture carry a git repository?

`tools/fixtures/regen.sh` turns every `dep/*/` of a fixture into a git
repository with a fixed identity, date and tag, and leaves one of `p14-foss`'s
dependencies dirty after the commit. That is what section 19.4's modification
status is read from, and the harvested source tree does not carry it: the
harvest skips `.git`, because git tracks a nested repository as a gitlink and
not as files, so committing one would need it renamed and the tool taught to
look for the new name.

What that costs is narrow and real: a run over the committed corpus can see
that `dep/lgpl-lib` differs from the tree that produced the tag only if it can
ask git, and it cannot. The modification status of every fixture component is
therefore `unknown` there, which is the honest answer and not the interesting
one.

Three routes exist and none is obviously right. Harvest `.git` under another
name and give sbomb a way to be pointed at it -- that is a production feature
invented for a test. Record the expected answer in the fixture's manifest and
assert against that -- which tests the assertion rather than the tool. Or build
the repository in a temporary directory at test time from the committed tree,
which is honest but makes the test non-hermetic and toolchain-dependent, the
same trade Q1 records for built artifacts.

Two things were measured while F1 was written, so that whoever settles this
starts from facts rather than from the first route that looks plausible.

Renaming is not optional in any route that commits the repository. Git will not
put a path with a `.git` component into the index at all -- not only a nested
repository as a gitlink: `git add -f p14-foss-src/dep/mit-lib/.git/HEAD`
succeeds and adds nothing. Something therefore has to put the name back before
a repository exists on disk, which is a materialization step in the test and
not a property of the corpus.

A harvested repository is not reproducible as it stands. `.git/index` stores
each entry's `ctime`, `mtime`, `dev` and `ino`, which are drawn fresh on every
regeneration, so committing the directory as `git add` left it would put the
churn that `tools/fixtures/replynorm` was written to remove straight back.
Deleting the index is not a way out: git then reports every tracked file as
both deleted and untracked, which is a wrong answer rather than `unknown`.
Rebuilding it with `git read-tree HEAD` writes zeroed stat fields and is
reproducible, and git falls back to comparing content when the stat cache does
not match, so the answers stay right.

What the corpus does carry is the dirty content: the bytes harvested from
`dep/lgpl-lib/src/lgpl_extra.c` are the bytes that were compiled, comment and
all, and they differ from `tools/fixtures/projects/p14-foss/`. Whatever route
F6 takes, the tree it needs is already committed.

**Settled in F6: none of the three routes, because none was needed.** The
modification derivation asks git through the `exec.Runner` of section 9.2 —
`git describe --tags --always --dirty` and `git rev-parse HEAD` — and takes the
component root from `componentRootResult`. Both inputs are parameters, so every
state the tri-state has is reachable in a unit test over a temporary directory
that the test itself turns into a repository
(`internal/generate/modification_test.go`): clean on its tag, dirty, no
repository at all, a repository with introspection off, clean but off its tag,
and a vendored directory inside a dirty enclosing repository. That last one is
the case a committed corpus repository would *not* have caught, because the
corpus has no nesting.

So the corpus keeps no `.git`, every fixture component reports `unknown`, and
`cmd/sbomb/componentattributes_test.go` asserts exactly that — including that an
unknown status writes no `pedigree` node, which is the property an auditor
depends on. The test that needs a repository builds one; the test that needs a
corpus reads the corpus. Route 3 was the honest one and it turns out not to need
the corpus at all.

What it still costs is worth stating: no test exercises the derivation over
*harvested* build evidence whose component roots are git repositories, so the
interaction between relocation (section 7.9) and the git check is untested. The
physical root is what the check is handed, and relocation is what produces it,
so a relocated tree that carries a repository would be answered from the
relocated copy — which is correct, and is not asserted anywhere. It becomes
testable the day the corpus carries a repository, for whatever reason makes that
worth doing.

## Q10 — Is a resolved single identifier an `expression` or a `license.id`?

Section 28.7 lists two encodings for `components[].licenses`: a known SPDX
**identifier** becomes `{"license": {"id": "<SPDX-ID>"}}`, and a known SPDX
**expression** becomes `{"expression": "<expr>"}`. `licensesToCyclone`
(`internal/cyclonedx/writer.go`) tests the expression field first, so a licence
resolved to the bare identifier `MIT` is written as `{"expression": "MIT"}`.
Read strictly, that is the second encoding applied to a case the first one
covers; read as a whole, `MIT` is a well-formed SPDX expression and the document
is valid either way.

Nothing observed this before F2. Every licence in every golden was
`NOASSERTION`, so the branch was never reached with a resolved value, and the
F2 milestone's own acceptance command greps for `"id": "MIT"` and finds
`"expression": "MIT"` instead.

What it would cost to change is the reason it is a question rather than a fix:
the condition sits in the shared writer, so every resolved licence in every
document and every golden changes shape at once — including the ones F3 to F8
are about to add — and a consumer that reads only one of the two forms sees a
different document than before. It is settled where the attribution document is
rendered and the licence expressions are actually consumed (F5 or F8), with the
NOASSERTION encoding of section 28.7 and deviation D19 in one view, not as a
by-product of a milestone about where files are read from.

---

## Q11 — Does `FOSS_LICENSE_TEXT_MISSING` apply to the manufacturer's own code?

The finding is defined for a component with a resolved licence identifier and no
retained text. The FOSS plan's own table narrows it to a **distributed**
component, and the distribution role is F6 work: it does not exist yet, so F4
fires the finding for every component the criterion matches.

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

## Q12 — The fixture's LGPL text lost its placeholder brackets

`testdata/fixtures/p14-foss-src/dep/lgpl-lib/LICENSE` line 133 reads

```
Copyright (C)  year >  name of author >
```

where the licence's own "how to apply these terms" appendix says
`Copyright (C) <year>  <name of author>`. The opening angle brackets are missing
from the corpus project itself
(`tools/fixtures/projects/p14-foss/dep/lgpl-lib/LICENSE`), which is harvested
verbatim, so they are missing from the committed source tree too — and those
bytes are what the licence-file digests in the goldens pin.
`dep/gpl-gen/LICENSE` has the same damage in two places.

The consequence is visible in the p14 document: section 22.10 refuses a holder
that is nothing but a bracketed placeholder, and without its brackets this line
is no longer recognizable as one, so `lgpl-lib` publishes it as a copyright
statement beside its two real ones. The extractor is behaving correctly on the
bytes it was given; the bytes are wrong.

It is not fixed here because fixing it is not a copyright change: the file's
SHA-256 is in the inventory golden and in `sbomb:component:licenseFile`, so
regenerating it moves goldens this milestone has no business moving, and the
fixture is regenerated by `tools/fixtures/regen.sh` rather than edited. It is
settled where the fixture is next regenerated for another reason — F6 or F7 —
by repairing the licence files in `tools/fixtures/projects/p14-foss` and
re-harvesting, so that the corpus and the committed tree change together. Until then the fixture also documents, accidentally, that extraction is
mechanical: it stores what the file says.

---

## Q13 — Should `§9.2` permit `git rev-list --count <upstream>..HEAD`?

Section 19.4's modification status has three states and reaches `false` only
from a clean checkout standing exactly on its recorded tag. A clean checkout
four commits past its tag is `unknown`, and it is the commonest real shape of a
vendored dependency somebody has fixed and committed: the tree is clean, the tag
is still `v1.2.0`, and the component *is* modified.

Answering it needs one command:

```
git -C <root> rev-list --count <tag>..HEAD
```

which returns a number, reads nothing outside the repository, writes nothing,
and would turn that case from `unknown` into `true` with a count beside it.

**What it costs is not the command, it is the allowlist.** Section 9.2 is a
security boundary and the table in `internal/exec/exec.go` is its enforcement:
five shapes the specification lists are absent because nothing could call them
without guessing (deviation D29), and two more were removed because the
permitted shape did not answer the question (D30). `rev-list` is the first entry
that would take a **caller-supplied revision** in an argument slot rather than a
path. A revision is not a path, so `checkPath` does not apply to it, and a tag
name comes out of a repository sbomb was pointed at — which is untrusted input
by section 30. `git rev-list --count <x>..HEAD` with a hostile `<x>` is not
known to be exploitable, and "not known to be" is not the standard an allowlist
is held to: the argument would have to be that the revision slot is validated
against a grammar before it is passed, and that grammar would have to be
written and tested.

Three things would have to land together, and none of them belongs in a
milestone about deriving attributes:

1. a revision slot in the allowlist table, with validation of its own —
   `PathSlots` has no equivalent for revisions today;
2. the security argument in section 9.2, stated rather than assumed;
3. a fixture with a repository in it, because a counting rule nothing counts is
   an untested branch (see Q9).

Until then the restriction is stated in section 19.4 rather than worked around,
and the state it produces is `unknown` — which is the answer that cannot be
wrong.

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

## Q15 — `FOSS_PER_FILE_LICENSE_DIVERGENCE` is specified by no milestone

`docs/dev/foss/spec-delta.md` §10 lists ten new findings for the attribution
track. Nine of them are claimed by a milestone and are now emitted. The tenth,
`FOSS_PER_FILE_LICENSE_DIVERGENCE` (info — "files of one component carry
different SPDX identifiers"), appears in no milestone's deliverables or tests,
so F7 did not add it to appendix A: a catalogue entry nothing emits describes a
document sbomb does not produce, and the two generated catalogues are checked
against the code.

It is not redundant. §22.5 already reports `LICENSE_CONFLICT` when two sources
disagree about *one* component's licence, and §22.2 decides the component's
identifier from the file-level evidence — but a component whose files carry
`MIT` in one directory and `GPL-2.0-only` in another is a different situation
from a conflict: both readings are correct, and the component has two licences
rather than a disputed one. The fixture's `dep/multi-license` is the benign form
(one dual-licensed dependency); the form worth a finding is a directory that was
assembled from two upstreams.

Deciding it needs two things this milestone had no mandate for: what the
threshold is (any two distinct identifiers? or only where neither is implied by
the component's own expression?), and a fixture that exhibits the malignant
case. Recorded here rather than guessed.

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
