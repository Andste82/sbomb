### Milestone F2 — Source tree relocation

**Goal:** read a source tree that is no longer where it was built. This is a
defect fix before it is FOSS work: today licence detection silently degrades to
NOASSERTION whenever the sources moved, and no finding says so.

**Depends on:** F1, for something to test against.

---

**Why it is needed at all**

The build root already has the mapping. `logicalFor` and `physicalFor`
(`internal/generate/linkgraph.go:130`, `:145`) translate the logical build root
recorded in the evidence onto the directory being read — that is why the
fixture corpus works despite recording `/__fixture_build__`.

The source root has no counterpart, so `/__fixture_src__/dep/mit-lib/LICENSE`
is opened literally. In production this is the ordinary CI split: build in one
job, generate the SBOM in another from a restored build directory.

`--source-dir` is not the answer today. It assigns `cfg.Project.Root`
(`cmd/sbomb/main.go:650`), which becomes the **identity** root handed to
`anchors.Assemble`. Pointing it at a relocated tree re-anchors every file and
changes the document instead of relocating a single read.

**Deliverables**

* §7.9 of the specification, per [spec-delta.md §1](../spec-delta.md).
* A logical/physical source-root pair on the builder, mirroring the build-root
  pair. The logical root comes from the File API (`replyModel.SourceRoot`); the
  physical root comes from `--source-dir` / `project.root` and defaults to the
  logical one.
* **Identity is unaffected.** Anchoring continues to use the logical root. A
  test asserts that relocating a tree changes no `bom-ref` and no canonical
  path — that is the whole point of the anchor model.
* Reads that would leave every registered anchor are refused (§30.4). **There
  is no escape hatch to reuse.** `--allow-unanchored-reads` is named in §7.6
  and does not exist as a flag: `AllowUnanchoredReads` is an option of
  `internal/inventory` (`inventory.go:48`), consulted only for a symlink whose
  target escapes every anchor, and nothing outside a test sets it. This
  milestone either builds the flag or states that a relocated read is refused
  outright.
* Finding `SOURCE_TREE_UNAVAILABLE` (warning) when the physical source root
  does not exist, emitted once per run rather than once per file.
* **Without a CMake File API reply there is no logical source root**, so
  relocation is inactive and `--source-dir` behaves exactly as it does today.
  The existing `CMAKE_FILE_API_UNAVAILABLE` finding already says why; no new
  finding (decision [Q16](../decisions.md)).
* **Only the source root is relocated.** Package caches keep the paths the
  build recorded — Conan reads its package folder out of a file the build wrote
  (`internal/adapters/pkgmanager/conan.go:99`), so those paths are
  build-machine paths. The general form, a list of prefix replacements as
  `-ffile-prefix-map` and the debugger's `substitute-path` use, is **not built**:
  see decision [Q17](../decisions.md) for why the case is narrower than it
  looks, and `open-questions.md` Q8 for the day it is needed.
* `deviations.md` entry for the changed meaning of `--source-dir`, with the
  observed behaviour that motivated it.

**Tests**

* A build evidence set recording root `A` plus a tree at `B`: licences resolve,
  canonical paths and `bom-ref`s are identical to the run where the tree is at
  `A`.
* No `--source-dir`, tree absent: `SOURCE_TREE_UNAVAILABLE` once,
  NOASSERTION with reason `no-evidence`, exit code unchanged.
* A relocation that would read outside the anchors is refused.
* Windows path flavour: relocation works with mixed separators.
* The existing goldens for p01–p13 are byte-identical, because none of them
  relocates anything.

**Acceptance**

```
go test ./internal/generate/... -race                                  # 0
sbomb generate --build-dir testdata/fixtures/gcc-ninja/p14-foss/build \
  --source-dir testdata/fixtures/gcc-ninja/p14-foss/src \
  --output /tmp/f2.cdx.json --reproducible                             # 0
grep -q '"id": "MIT"' /tmp/f2.cdx.json                                 # 0
cmp /tmp/f1.cdx.json testdata/golden/gcc-ninja-p14-foss.cdx.json       # non-zero: licences now resolve
```

The last line is the intended golden change of this milestone: the fixture's
document goes from NOASSERTION to resolved identifiers. Regenerate the golden
and explain the diff in the commit.

**Not in this milestone:** retention of any bytes. F2 makes the existing
detection reach the files; it changes nothing about what is kept.

**Definition of done:** a build directory and its source tree can live in
different places, identity is provably unchanged by that, and a missing source
tree is a finding rather than silence.
