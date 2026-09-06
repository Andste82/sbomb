# Phase 2 — The through-line: the graph as the only source of truth

**Status: complete.** Commit `7b99804`.

## Why here

`generate.go` filled the graph and built the component list *beside* it -- the
output was never derived from the graph. That means there was no reachability
filter, which is the entire thesis of the tool. This phase turned it around.

## Work

* **Artifact resolution**: `artifacts[]` with an existence check,
  `MISSING_ARTIFACT` / `MISSING_FINAL_DELIVERABLE`, then the permitted File API
  discovery of §5.3 with test and example exclusion.
* **Link evidence in the preference order of §11.2**, corrected by deviation
  D1: the dependency file for the link inputs, the map for the archive members,
  DWARF for the translation units actually present, the trace as textual
  fallback.
* **Archive members per §12**: extracted members only, never the whole archive.
* **Object to source** through the resolver of §13.2; basename matching stays
  forbidden.
* **The reachability filter.** The used-file set is `Reachable(artifactNodes)`,
  not the collected list of everything the adapters saw. This is the change that
  makes the tool sbomb.
* **`sbomwriter.Document`** as a format-neutral hand-off type (§36.1),
  introduced while the rebuild was happening anyway.

## Acceptance, met

`p02-static` yields exactly `{main.c, crypto.c, crypto.h}` plus the artifact --
no libc entries, no `crtbeginS.o`. `explain --file project:crypto.c` shows the
four-step chain header → compile → archive member → link.

## Defects this uncovered

**The map parsers produced garbage.** They scanned every line for tokens with a
file extension, so a linker-script pattern like `*crtbegin.o(.ctors)` counted as
an archive member, and in an lld map even section placements like `foo.o:(.text)`
did -- with each member appearing once per contributed section. A map format is
section-structured; rewritten so the archive-member block, the as-needed block,
the discarded sections and the memory map are each read for what they are.

**The `.ninja_deps` parser never stripped the NUL padding** of its path records.
Its loop condition was already false on entry:

```go
pathEnd := size - 4
for pathEnd > 0 && record[pathEnd-1] == 0 && size-pathEnd < 3 {   // 4 < 3
    pathEnd--                                                     // never runs
}
```

Every path whose length was not a multiple of four ended in a NUL byte and
matched nothing. Header evidence had therefore never worked.

Also fixed: `LOAD linker stubs` (ARM veneers) recorded as a file; the mingw
deliverable being ambiguous between `app.exe` and `app.pdb`; and identity
resolving everything to `abs:` because the logical build root was conflated
with the physical one.
