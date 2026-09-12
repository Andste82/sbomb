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

Milestone F6 is where it has to be settled, because that is where modification
status is derived.

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

It is settled in F6, where the role exists and all three criteria can be
compared against one corpus instead of argued about. Until then the finding is
informational, gates nothing, and over-reports rather than under-reports, which
is the right direction for a compliance signal.
