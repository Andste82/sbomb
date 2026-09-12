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

## Q7, Q8 — reserved

The FOSS attribution plan (`docs/dev/foss/decisions.md`, on its own branch)
already refers to two entries by number that the milestones adding them have
not reached: Q7, two versions of one package in a single assembly (milestone
F7), and Q8, relocating package-manager caches rather than only the source
root (milestone F2). The numbers are held so those references keep pointing at
what they were written for.

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
