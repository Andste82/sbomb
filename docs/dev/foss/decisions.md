# Decisions

Every question the plan raised, and what was decided. IDs are the ones used
during the review, so a conversation about "Q13" and this file agree.

Nine were settled from the code, the specification, or from what other tools
already do. Seven were product decisions and were taken by the maintainer. One
question is genuinely open and lives in
[../open-questions.md](../open-questions.md).

| ID | Question | Decision |
|---|---|---|
| B1 | `--foss-out` and `licenseTextInSBOM` contradicted each other | The FOSS outputs never change the document |
| Q7 | Component root for header-only libraries | A licence file marks a component boundary |
| Q8 | Redaction | Applies to the review files; the notices document carries no paths |
| Q9 | Cost of copyright extraction | Rides on the hashing pass, no second read |
| Q10 | Assembly mode | One notices document per run, per-artifact detail in the review record |
| Q11 | Waivers on FOSS findings | Allowed, and never touch the notices document |
| Q12 | Where "third party" stops | Component type, not anchor scope |
| Q13 | Repeated licence texts | Each unique text once, components listed under it |
| Q14 | `--out` already exists | Overwrite the four names, like `--output` does |
| Q15 | Fixture toolchain matrix | Two toolchains, source tree harvested once |
| Q16 | Relocation without a CMake File API reply | Relocation is inactive, `--source-dir` behaves as today |
| Q17 | Relocation of package caches | Not built; source root only |
| Q18 | Fonts, images and other assets | Carried like any other component |
| Q19 | The copyleft list | A small curated list, not an imported database |
| Q20 | `explain` for the notices | Not extended |
| Q21 | Package boundaries | §35 already answers it |

---

## B1 — The FOSS outputs never change the document

An earlier draft had `sbomb foss` turning the licence text on for "the document
it writes". After `generate --foss-out` became the primary entry point there is
only **one** document, so that rule would have made `app.cdx.json` depend on
whether notices were requested on the side.

**Decision.** The retained licence text always goes into the notices files, and
into the CycloneDX document only when `licenseTextInSBOM: "evidence"` is
configured. `--foss-out` never changes a byte of the document.

*Implemented in:* F7, spec-delta §8.

## Q7 — A licence file marks a component boundary

sbomb needs a component's root directory, because that is where its `LICENSE`
lives. Today the root is the deepest common directory of the files that were
*used*, so a library whose sources sit in `src/` or `include/` has a root one
level below its licence file, and the text is never found.

Half the fix already exists: `nearestPackageRoot`
(`internal/generate/components.go:184`) walks up from a file looking for
`conanfile.txt`, `vcpkg.json`, `CMakeLists.txt` and friends, and stops at the
anchor root so the search never leaves the component tree. That is strategy 6
of §19.2. It is simply not used for the licence lookup.

**Decision.** Use it — and add a recognized licence file to the marker list, so
that a library copied into the tree with nothing but a `LICENSE` and an
`include/` directory is recognized too.

**Why this is not the layout heuristic §19.2 forbids.** That rule is about
directory *names*: `vendor/`, `dep/`, `third_party/` mean nothing by
themselves. A `LICENSE` is a file with content, like `vcpkg.json`. The upward
walk already stops at the anchor root, so a project's own top-level licence can
never be claimed by a dependency. And the nearest match wins, so
`mbedtls/3rdparty/everest/LICENSE` correctly splits `everest` out as its own
component instead of hiding it inside mbedtls.

*Implemented in:* F3, spec-delta §2 and §3.

## Q8 — Redaction

§30.7 requires `--redact-unanchored-paths` to apply to the SBOM, the findings
JSON and the review report equally. The FOSS outputs are a fourth surface.

**Decision.** Redaction applies to `foss-review.txt` and `foss-review.json`.
`THIRD-PARTY-NOTICES.txt` needs no rule: it contains licence text, copyright
lines and an origin URL, and no filesystem paths at all. A test asserts that.

*Implemented in:* F7.

## Q9 — Copyright extraction rides on the hashing pass

Reading the first 64 KiB of every used file looked like a second pass over the
whole tree, against a §31 budget of 15 s for 10 000 files.

It is not, because `hashOne` (`internal/inventory/inventory.go:166`) already
does `os.ReadFile` — the bytes are in memory when the hash is computed.

**Decision.** `HashOptions` gains an `Observe func(domain.FileID, []byte)`
callback, supplied by the `generate` layer. Zero extra I/O, and the layering of
§35 holds: `internal/inventory` learns nothing about licences, the caller above
it does the work.

*Implemented in:* F5.

## Q10 — One notices document per run

In assembly mode one SBOM describes several artifacts as one product.

**Decision.** One notices document per run. The mode already states what a
product is; if two things are distributed separately they are two products and
sbomb runs twice, in the ordinary single-artifact mode.

A library used by several artifacts appears **once**, which is how §6.3 already
models shared files. Its entry may therefore carry several linkage forms — for
example static in the bootloader and header-only in the application. The
per-artifact breakdown goes into the review record, because "which artifact
pulled in the LGPL component" is the question that actually gets asked.

*Implemented in:* F7. One consequence is open: see
[../open-questions.md](../open-questions.md) Q7 on two versions of one package
in a single assembly.

## Q11 — Waivers are allowed and never touch the notices document

The worry was that waiving "no licence text found" would paper over a gap in a
document that gets shipped.

It does not, because sbomb's waivers do not delete anything: a waived finding
stays in the report with `waived: true`, its reason, its approver and its
expiry, and only stops failing the build. An expired waiver reports
`WAIVER_EXPIRED`; one that matches nothing reports `WAIVER_UNUSED`.

**Decision.** FOSS findings are waivable, with one rule stated normatively: a
waiver never changes `THIRD-PARTY-NOTICES.txt`. The incompleteness marker stays
where it is, because that document reports what sbomb saw, not what somebody
decided about it.

*Implemented in:* F7, spec-delta §10.

## Q12 — Where "third party" stops

`THIRD-PARTY-NOTICES.txt` must not contain the manufacturer's own code.

The anchor scope is the wrong criterion: a library copied into the tree has
`scope=project` and would be dropped, which is exactly backwards. §19.1 gives
the project's own code the CycloneDX type `application` and everything else
`library` or `framework`.

**Decision.** The criterion is `type != application`. Correctness therefore
depends on component mapping, and the existing `UNKNOWN_COMPONENT` and
`COMPONENT_ROOT_UNRESOLVED` findings are the signals that it went wrong.

*Implemented in:* F7.

## Q13 — Each unique licence text appears once

A firmware with twenty MIT components would otherwise carry twenty nearly
identical texts.

The convention is established. Android's `generate-notice-files.py` hashes each
notice file and emits every unique text exactly once, mapping files to it by a
content id. Chrome OS "factors out the licenses used by multiple packages […]
and creating pointers to them".

**Decision.** Group by the SHA-256 of the retained bytes; each unique text
appears once with its components listed under it. No holder is lost by this:
two MIT texts naming different holders have different hashes, which is the
whole point of retaining the component's own file rather than a canonical one.

Ordering: components by name, then by `bom-ref` — which is version-free, so a
diff between two releases stays readable.

*Implemented in:* F7.

## Q14 — `--out` overwrites

The draft refused to write into a directory that was not empty. That was
invented, and it breaks a CI job whose output directory already exists.

**Decision.** `--out` writes its four files and overwrites them, exactly as
`--output` overwrites an SBOM. Nothing is deleted.

*Implemented in:* F7.

## Q15 — Two toolchains, one source tree

The corpus is 12 MB across five toolchains. Committing the FOSS project's
source tree five times would be waste, and an LGPL archive plus a code
generator says nothing extra when cross-compiled to bare-metal ARM.

**Decision.** `p14-foss` runs on `gcc-ninja` and `gcc-make` only, through the
`PROJECT_TOOLCHAINS` mechanism `regen.sh` already has for `p11-conan`. The
harvested source tree is toolchain-independent and is stored once.

*Implemented in:* F1.

## Q16 — Relocation without a File API reply

Without a reply directory there is no logical source root to map from.

**Decision.** Relocation is inactive and `--source-dir` behaves as it does
today. The existing `CMAKE_FILE_API_UNAVAILABLE` finding already says why, so
no new finding is needed.

*Implemented in:* F2.

## Q17 — Package caches are not relocated

The general form — a list of "everything that was under X is now under Y"
replacements, as `-ffile-prefix-map` and the debugger's `substitute-path` do —
would also cover Conan and vcpkg caches, whose roots are read from files the
build wrote and are therefore build-machine paths
(`internal/adapters/pkgmanager/conan.go:86`).

**Decision.** Not built. The case that needs it is narrower than it first
looked: sbomb normally runs right after the build, on the same machine, where
every path exists. The one setup where it bites without a machine change is a
project compiling with `-ffile-prefix-map`, and that would already show today
as missing file hashes.

The source-root pair is built because the fixture corpus cannot be tested
without it. A feature justified by its own tests stays as small as it can.
Recorded for the day somebody needs it:
[../open-questions.md](../open-questions.md) Q8.

*Implemented in:* F2.

## Q18 — Assets are carried like any other component

A font under OFL-1.1 or an icon set under CC-BY has real attribution
obligations, and the law does not care whether the file was compiled.

**Decision.** Embedded assets appear in the notices document like any other
component. A binary carries no `SPDX-License-Identifier`, so detection depends
entirely on a licence file beside it — which the Q7 decision now recognizes.

Two limits are stated in the documentation rather than left to be discovered:
CC-BY requires indicating whether changes were made, which sbomb can only
answer for a component and not for a single blob; and OFL's reserved font names
are a naming rule that sbomb cannot check at all.

*Implemented in:* F7, F8, requirements.md R7.

## Q19 — A small curated list

`source-obligations.txt` needs to know which identifiers carry a source
obligation. The ScanCode LicenseDB has over 2 000 licences with curated
categories and would cover every exotic case.

**Decision.** A flat list of roughly thirty identifiers in
`internal/foss/copyleft.go`, plus a free test asserting every entry is a real
SPDX identifier against the table already embedded for §22.3.

Importing the database would put two thousand licence classifications nobody at
the manufacturer has read into a document that carries the manufacturer's name.
Thirty entries can be read and defended. An identifier on no list reports
`FOSS_LICENSE_UNCLASSIFIED` and is never assumed permissive, so a gap in the
list is visible rather than silent.

If it ever needs to grow, the customer-owned obligation matrix of Stage 2 is
the right vehicle: then it is the manufacturer's data, with an explicit
adoption, rather than a judgement the tool brought along.

*Implemented in:* F7, F8.

## Q20 — `explain` is not extended

**Decision.** No FOSS mode for `explain`.

The question that gets asked — "is this library really in our product?" — is
already answered by `sbomb explain --component <name>`, and the chain shows a
build-time-only component reaching the artifact through generator edges only.
Everything else — linkage form, where the licence text came from, which
obligation was triggered — is already written out per component in
`foss-review.txt`.

Extending `explain` would mean writing the FOSS attributes into `evidence.json`,
which is a format change to a file other tools read, in exchange for
information the user already has twice. The documentation gets one sentence
pointing at both instead.

*Implemented in:* F8, gap-analysis.md R11.

## Q21 — Package boundaries

§35 already answers it: `LicenseEngine` resolves and retains, `ComponentMap`
derives, `Writers` render.

**Decision.** Retention lives in `internal/license`, the three attributes in the
component mapping layer, and `internal/foss` is a **writer** — it renders and
touches no evidence.

*Implemented in:* milestones/README.md.
