### Milestone F7 — `sbomb foss` and its outputs

**Goal:** one command, four files, no judgement. Everything it prints was
collected in F3–F6; this milestone is rendering, ordering, and one guarantee
that has to be machine-checked.

**Depends on:** F4, F5, F6.

---

**7.0 One run, two renderings**

The FOSS outputs are an **additional rendering of a `generate` run**, not a
second analysis. sbomb already works this way: `--review-report`,
`--findings-json` and `--evidence-dump` are all further outputs of one
discovery, and the FOSS documents belong in that row.

The primary entry point is therefore a flag:

```
sbomb generate --build-dir <dir> [--source-dir <dir>] \
  --output app.cdx.json --review-report review.txt --foss-out <dir>
```

`sbomb foss` stays, as a thin front end over the same code path for the case
"I want the notices, not an SBOM on disk":

```
sbomb foss --build-dir <dir> [--source-dir <dir>] --out <dir>
           [--format text|markdown] [--reproducible]
```

Both call one function over one `generate.Result`. In no workflow does
discovery run twice.

**The FOSS outputs never change the document** (decision
[B1](../decisions.md)). The retained licence text always goes into the notices
files; it reaches the CycloneDX document only when `licenseTextInSBOM:
"evidence"` is configured. `--foss-out` must not alter a byte of
`app.cdx.json`, and a test asserts that the same build with and without it
produces the same document.

`--out` writes its four files and overwrites them, the way `--output`
overwrites an SBOM. It deletes nothing and it refuses nothing (decision
[Q14](../decisions.md)).

**Why this is not a convenience question.** Two invocations means two graphs
built from a tree that may have changed between them, and then the SBOM and the
notices document can disagree while each is internally correct. An earlier draft
of this milestone answered that with a test asserting that `generate` and `foss`
report the same component set — a test that is only needed because the design
permits the divergence. Remove the possibility instead of checking for it.

The cost is the second reason, not the first: §31 budgets 15 s for 10 000 used
files and 90 s for 50 000, and the second run re-parses the File API, the
compile database, `.ninja_deps`, the DWARF of the artifact, the linker map and
the archive indexes, then re-hashes every used file — to arrive at exactly the
same answer. `foss` needs no evidence source that `generate` does not already
read.

**7.1 The licence view**

The FOSS view is computed with `headerEvidence=union`, regardless of what the
SBOM view uses, and this is not configurable.

DWARF narrowing is right for a bill of materials and wrong for a licence
question. Whether a header emitted code is not the question; whether its
interface was used is. A narrowed view shrinks the documented licence surface
without shrinking the real one.

This is the only place where the two views genuinely differ, and it is **not** a
reason to run the pipeline twice. The narrowed headers are retained with their
names — `NarrowingCount.Headers` exists for `--report-chains all` — so the union
set is `used ∪ narrowed`, reconstructible from one run.
`narrowingByComponent` (`internal/generate/generate.go:573`) already maps them to
components, which is the delta this section reports.

What the union view adds beyond that is reading the narrowed headers for
copyright statements (F5). That is a bounded extra step over a known file list,
not a second discovery: no graph is rebuilt, no artifact is re-parsed, nothing
is re-hashed. The review record states the delta per component and in total, so
the difference between the two views is visible rather than implied.

**7.2 Output**

```
<out>/
  THIRD-PARTY-NOTICES.txt     the shippable attribution document
  foss-review.txt             the internal record, human-readable
  foss-review.json            the same, machine-readable
  source-obligations.txt      which components owe source material, and why
```

One of these ships with the product; three are internal. That split is the
structure of the milestone, and the documentation must state it in those words.

**Who is in it** (decision [Q12](../decisions.md)): every distributed
component whose CycloneDX type is not `application`. §19.1 gives the project's
own code the type `application`; the anchor scope is the wrong criterion,
because a library copied into the tree carries `scope=project` and would be
dropped — exactly backwards. Correctness therefore rests on component mapping,
and `UNKNOWN_COMPONENT` and `COMPONENT_ROOT_UNRESOLVED` are the signals that it
went wrong.

Embedded **assets** — fonts, icon sets, images — are components like any other
and appear here (decision [Q18](../decisions.md)). A binary carries no
`SPDX-License-Identifier`, so an asset gets a licence only from a file beside
it, which F3's boundary rule now finds.

**Order and repetition** (decision [Q13](../decisions.md)): components sort by
name, then by `bom-ref` — which is version-free, so a diff between two releases
stays readable. A licence text that several components share is printed
**once**, with those components listed under it, keyed by the SHA-256 of the
retained bytes. This is the established convention: Android's
`generate-notice-files.py` hashes each notice file and emits every unique text
exactly once, and Chrome OS "factors out the licenses used by multiple packages
[…] and creating pointers to them". No holder is lost by it — two MIT texts
naming different holders have different hashes, which is precisely why the
component's own file is retained instead of a canonical one.

`THIRD-PARTY-NOTICES.txt`, per distributed component, in deterministic order:

```
================================================================
mbedtls 3.5.0
Origin: https://github.com/Mbed-TLS/mbedtls @ a3f19c2
Licence: Apache-2.0

Copyright 2006-2015, ARM Limited, All Rights Reserved

--- LICENSE ---
<verbatim bytes of the component's own licence file>

--- NOTICE ---
<verbatim bytes of the component's NOTICE, when present>
```

A component with no retained text appears with
`[licence text not found in component - attribution incomplete]` rather than a
substituted canonical text (R10).

**A waiver never changes this file** (decision [Q11](../decisions.md)). FOSS
findings are waivable — sbomb's waivers annotate rather than delete, so a
waived finding keeps its reason, approver and expiry in the report and only
stops failing the build. But the marker above stays where it is regardless:
this document reports what sbomb saw, not what somebody decided about it.

**Redaction** (decision [Q8](../decisions.md)): §30.7 requires
`--redact-unanchored-paths` to reach the SBOM, the findings JSON and the review
report equally, so it reaches `foss-review.txt` and `foss-review.json` too.
`THIRD-PARTY-NOTICES.txt` needs no rule — it contains licence text, copyright
lines and an origin URL and no filesystem paths at all — and a test asserts
that.

`foss-review.txt` adds linkage form, distribution role, modification status,
used-file count and the narrowing delta, plus a completeness block in absolute
numbers:

```
COMPLETENESS
  components                       9
  with resolved licence            7 / 9
  with retained licence text       6 / 9
  with copyright statements        7 / 9
  with resolved version            8 / 9
  modification status unknown      2
  licence view vs. SBOM view      +14 files
```

Percentages are deliberately absent: "86% complete" is a number nobody can act
on, and "6 of 9" names the three to look at.

Build-time-only components are listed in a section of their own, with the note
that their obligations depend on the individual licence's terms for build
tools — a question sbomb does not answer.

**In assembly mode** the review record breaks the components down per artifact
(decision [Q10](../decisions.md)). There is still exactly one notices document
per run: the mode already declares what a product is, and two things
distributed separately are two products and two runs. A library several
artifacts share appears once, as §6.3 already models shared files, and its
entry may then carry several linkage forms — static in the bootloader,
header-only in the application. "Which artifact pulled in the LGPL component"
is the question that actually gets asked, so the review record answers it.

`foss-review.json` is **not a fourth format of sbomb's own invention.** There is
no standard for a notices document — the three text files above are deliberately
house style, because the obligation is about content and every vendor formats it
differently — but a machine-readable review record has no such excuse. §36.1
requires the writer layer to be format-agnostic so that SPDX can be added
without touching anything below it, and a hand-rolled JSON schema beside it
would violate exactly that rule.

So: `foss-review.json` is a rendering of `sbomwriter.Document` through the
writer registry, not a new schema. When the SPDX writer of §36.1 arrives, this
file is produced by it and the intermediate form is retired — replaced, not
placed beside it. The reason the FOSS data is bound to CycloneDX first is
recorded in [spec-delta.md §8](../spec-delta.md): SPDX has no clean slot for the
verbatim text of a *listed* licence as a component carries it, which is the
whole point for MIT and BSD.

`source-obligations.txt`, for every distributed component whose licence is on
the copyleft list:

```
lgpl-lib 2.1 - LGPL-2.1-only - static-archive-member
  A source offer is required. Static linkage additionally implicates the
  relinking provision of LGPL-2.1 section 6.
  sbomb does not and cannot produce this material.
```

The list is a flat, committed set of SPDX identifiers in
`internal/foss/copyleft.go` — roughly thirty entries mapping an identifier to
the obligation names it triggers. It is not a rules engine: the single
condition anywhere in it is the linkage form in the LGPL sentence above, and
that condition is written out rather than expressed in data. An identifier that
is on no list produces `FOSS_LICENSE_UNCLASSIFIED` (info) and is never assumed
permissive.

**It stays small and curated** (decision [Q19](../decisions.md)). Importing the
ScanCode LicenseDB would cover every exotic licence and would put two thousand
classifications nobody at the manufacturer has read into a document that
carries the manufacturer's name. Thirty entries can be read and defended. A
test asserts that every identifier in the list exists in the SPDX table already
embedded for §22.3 — free, because the table is there. If the list ever needs
to grow, the customer-owned obligation matrix of Stage 2 is the vehicle: then
it is the manufacturer's data, with an explicit adoption.

**7.3 What the command does not do**

No status column, no `OK`, no compliance verdict. Exit codes are those of
§32.4 — `0`, `1`, `2`, `4`, `70` with the documented precedence — and **none of
them depends on licence content**. Policy gates (exit `3`) are not evaluated
here. Everything the FOSS view finds is informational.

**7.4 The structural guarantee**

A test asserts that no file matching `*.c`, `*.h`, `*.cpp`, `*.hpp`, `*.S`,
`*.tar*`, `*.zip` or `*.patch` is ever written into `--out`.

This is the machine-checked promise that the output is **not** a source offer.
Corresponding Source is the complete source of the work plus the scripts that
control compilation and installation; an evidence-derived subset would look
like a source offer while being materially incomplete, which turns the tool's
precision into a compliance defect (R6). `--out-source` and any equivalent must
not be added.

**Tests**

* Golden comparison of all four files, byte-identical under `--reproducible`.
* `mit-lib`'s entry contains the holder line from the component's own text, and
  the bytes match the retained artifact exactly — not a canonical MIT text.
* `gpl-gen` appears only in the build-time-only section and never in
  `source-obligations.txt`.
* `lgpl-lib` produces an entry naming static linkage.
* `nolicense` appears in the notices document with the incompleteness marker,
  and it still appears there when a waiver covers its finding.
* The project's own component (type `application`) does **not** appear in the
  notices document; a library copied into the source tree does.
* An embedded font with a `LICENSE` beside it appears with its text; one
  without appears with the incompleteness marker.
* Two components sharing a licence text produce **one** text block listing both;
  two MIT texts with different holders produce two blocks.
* `generate` with and without `--foss-out` produces a byte-identical document.
* `--redact-unanchored-paths` reaches `foss-review.txt` and `foss-review.json`;
  `THIRD-PARTY-NOTICES.txt` contains no path in any fixture run.
* `--out` pointed at a directory that already holds the four files overwrites
  them and leaves anything else alone.
* Every identifier in `internal/foss/copyleft.go` exists in the embedded SPDX
  table.
* The structural guarantee test.
* The narrowing delta is non-zero on the fixture and matches a hand-counted
  value in the golden.
* `sbomb foss --out X` and `sbomb generate --foss-out X` produce byte-identical
  files. This replaces the consistency test an earlier draft had between the two
  commands: they are one code path, so the assertion is that they agree
  byte-for-byte, not that their component sets happen to match.
* `generate --foss-out` performs exactly one discovery. Assert it over the
  run's own counters — the number of graph builds and of hashed files — rather
  than by timing, which is not a test.
* Two runs produce identical bytes; `scripts/determinism-check.sh` covers the
  new outputs.
* A `--out` directory that exists and is not empty is refused rather than
  merged, so a stale notices file cannot survive a run.

**Acceptance**

```
go test ./internal/foss/... -race                                       # 0
sbomb generate --build-dir testdata/fixtures/gcc-ninja/p14-foss/build \
  --source-dir testdata/fixtures/gcc-ninja/p14-foss/src \
  --output /tmp/f7.cdx.json --foss-out /tmp/foss-a --reproducible       # 0
sbomb foss --build-dir testdata/fixtures/gcc-ninja/p14-foss/build \
  --source-dir testdata/fixtures/gcc-ninja/p14-foss/src \
  --out /tmp/foss-b --reproducible                                      # 0
diff -r /tmp/foss-a /tmp/foss-b                                         # 0
diff -r /tmp/foss-a testdata/golden/foss/gcc-ninja-p14-foss/            # 0
sbomb foss --build-dir /nonexistent --out /tmp/x                        # 2
sbomb foss --build-dir testdata/fixtures/gcc-ninja/p14-foss/build       # 1  (no --out)
```

**Definition of done:** a shippable notices document and an internal review
record are produced deterministically from evidence, from **one** discovery that
also produces the SBOM; the notices document contains the components' own texts;
and the guarantee that the output is not a source offer is asserted by a test
rather than promised in prose.
