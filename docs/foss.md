# FOSS attribution

Every build ships somebody else's code. The licences that code carries ask for
things — the licence text, the copyright lines, a statement about whether the
component was changed, and for the copyleft family a source offer. This
document is about the four files sbomb writes for that, what each one is for,
and what sbomb deliberately refuses to decide.

**sbomb produces data; the manufacturer performs the assessment.**

## The four files

```bash
# As a further output of an ordinary run
sbomb generate --build-dir build --output build/app.cdx.json --foss-out build/foss

# Or on its own, when no SBOM is wanted on disk
sbomb foss --build-dir build --out build/foss
```

| File | Ships with the product | What it is |
|---|:--:|---|
| `THIRD-PARTY-NOTICES.txt` | **yes** | The attribution document. The licence text each component actually carries, verbatim, its copyright statements, and its origin |
| `foss-review.txt` | no | The internal record: every component with its licence, distribution role, linkage form, modification status and retained files |
| `foss-review.json` | no | The same facts, machine-readable |
| `source-obligations.txt` | no | Which components owe source material, what triggered it, and that sbomb does not produce it |

One of the four is for the recipient of the product; the other three are for
the people who have to defend it. The two review records name filesystem paths
and are therefore subject to `--redact-unanchored-paths` like the SBOM and the
review report. `THIRD-PARTY-NOTICES.txt` and `source-obligations.txt` add no
path of their own — licence text, copyright lines, an origin URL and component
names is all they carry — which is asserted by a test over the fixture corpus
rather than promised.

Both commands write these four names and overwrite them, exactly as `--output`
overwrites an SBOM. Nothing else in the directory is read, moved or deleted.

Neither command runs discovery twice, and that is not a performance note: two
invocations mean two graphs built from a tree that may have changed in between,
and the SBOM and the notices document could then disagree while each is
internally correct. The two entry points are asserted to produce
byte-identical files.

## What is in the notices document, and what is not

Every component whose files reach what is distributed and which is not the
manufacturer's own code. A component that only helped build the product —
a code generator, a build tool — is named in `foss-review.txt` and is in
neither `THIRD-PARTY-NOTICES.txt` nor `source-obligations.txt`: naming a
component the product does not contain invites an obligation that was never
triggered, and a source request nobody can answer.

This is the one thing a repository scanner cannot do. The used-file set comes
from the link, archive and compile evidence rather than from a directory walk,
so a build-time-only GPL generator is not reported as a shipped GPL component.

Embedded assets — fonts, icon sets, images — are components like any other and
are in it. A binary carries no `SPDX-License-Identifier`, so an asset's licence
depends entirely on a licence file lying beside it.

Where a licence text or a copyright line could not be established, the document
says so in place of the entry:

```
[licence text not found in component - attribution incomplete]
```

No canonical SPDX text is ever substituted for a missing one. The generic SPDX
text for `MIT` carries a placeholder where the rights holder belongs, so
shipping it would ship a template where a notice was required. A waiver never
changes `THIRD-PARTY-NOTICES.txt` either — that document reports what sbomb
saw, not what somebody decided about it.

## The CRA and FOSS obligations are two different things

They are easy to conflate, because both end in a list of components, and both
lists come out of the same run. They are not the same question.

* The **CRA** asks which components are in the artifact, so that
  vulnerabilities in them can be found, tracked and fixed. It says nothing
  about licence obligations.
* **FOSS obligations** come from the licences themselves. They applied long
  before the CRA existed and would apply if it were repealed tomorrow.

They share one data model because they need the same facts — what is in this
artifact, where did it come from, which version is it — and nothing more than
that. The consequences differ: a component with no supplier is a CRA gap, a
component with no licence text is an attribution gap, and neither implies the
other.

Which is why the sentence at the top of this document is the whole boundary.
sbomb reports what the evidence supports. Whether that discharges an obligation
is a legal assessment about a particular distribution, and the manufacturer
makes it.

## "Why is this in here?"

Two questions, two answers that already exist:

* **Is this component really in our product?** `foss-review.txt` states the
  distribution role and the linkage form per component, and lists separately
  the components that only helped build it.
* **Why does sbomb believe that?** `sbomb explain --file <id>` prints the chain
  of evidence for any one of the component's files back to the artifact — the
  identifiers are the ones the review record and the document use — and prints
  nothing at all where there is no chain.

```bash
sbomb explain --build-dir build --file project:dep/mit-lib/src/mit_a.c
```

`explain` gains no attribution mode of its own. Everything the FOSS view
establishes — the linkage form, where the licence text came from, which
obligation was triggered — is already written out per component in
`foss-review.txt`, and putting it into `explain` as well would mean writing it
into `evidence.json`, which is a format change to a file other tools read.

## The licence text in the document itself

`policy.licenseTextInSBOM` decides whether the retained texts are written into
the CycloneDX document, as `evidence.licenses[].license.text`:

```json
{
  "policy": {
    "licenseTextInSBOM": "evidence"
  }
}
```

It is `off` by default, because base64 inflates a licence text by a third and
most consumers of a document want the identifier. Turn it on where the document
itself has to carry the attribution material. The texts are retained either
way, and the FOSS outputs always carry them: `--foss-out` never changes a byte
of the document, so an SBOM never depends on which side outputs somebody asked
for. See [configuration.md](configuration.md#scope).

## Not built, on purpose

Every row here is something a reader might reasonably expect and will not find.
Each is left out for a reason, not for lack of time.

| Not built | Why |
|---|---|
| Licence compatibility verdicts | A legal judgement about a combination of terms; a stated non-goal (section 1.4) |
| Source bundles, corresponding source | Cannot be derived from usage evidence; producing one would be a compliance defect |
| Obligation fulfilment tracking | sbomb cannot observe whether a notice actually shipped |
| Per-licence exemptions for binary distribution | A legal conclusion, even where the licence text is plain |
| Indicating whether an asset was changed | CC-BY requires it; sbomb answers modification per component, not per embedded blob |
| Checking reserved font names | OFL-1.1 restricts naming a modified font; that is a naming rule, not a document rule |
| Similarity or percentage licence matching | Forbidden by section 22.3: a score is not evidence |
| A licence database or network lookup | sbomb makes no network request at all (section 30, point 8) and carries no database beyond the SPDX identifier list |
| VEX / CVE mapping | Real value, but security rather than FOSS; separate work |

The second row is the one worth dwelling on. For the copyleft family the
obligation is Complete Corresponding Source: the source of the whole work plus
the scripts used to control compilation and installation. That is not the
subset of files the linker touched. An evidence-derived source bundle would
*look* like a source offer while being materially incomplete, which turns
sbomb's precision into a compliance defect — a worse outcome than producing
nothing. `source-obligations.txt` therefore names the obligation, its trigger
and its consequence, and says plainly that the material is out of scope. No
`--out-source` flag exists and none will; that the output directory contains no
source archive and no patch is asserted by a test.

Two limits of the same kind are worth stating rather than leaving to be
discovered. CC-BY requires indicating whether changes were made, and sbomb
answers modification per component, so an asset changed inside an otherwise
untouched component is not distinguished. And OFL's reserved font names are a
rule about what a modified font may be called, which no document can check.

## The header view

The FOSS view is computed with `headerEvidence=union` regardless of the view
the SBOM uses, and this is not configurable. DWARF narrowing is right for a
bill of materials and wrong for a licence question: whether a header emitted
code is not the question, whether its interface was used is. `foss-review.txt`
states the difference per component and in total, so the two views are visible
rather than implied.

## Findings

Everything the FOSS view establishes or fails to establish is informational —
no exit code depends on licence content, and `sbomb foss` evaluates no policy
gate at all. The findings it can emit are in
[findings.md](findings.md): `FOSS_LICENSE_TEXT_MISSING`,
`FOSS_LICENSE_ARTIFACT_LIMIT`, `FOSS_COPYRIGHT_MISSING`,
`FOSS_COPYRIGHT_LIMIT`, `FOSS_MODIFICATION_UNKNOWN`, `FOSS_SOURCE_OBLIGATION`
and `FOSS_LICENSE_UNCLASSIFIED`. They are waivable like any other finding, and
a waiver leaves the notices document unchanged.

`FOSS_LICENSE_UNCLASSIFIED` deserves a note: the source-obligation
classification is a small curated list of SPDX identifiers, not an imported
database. An identifier on neither list is reported and never assumed
permissive, so a gap in the list is visible rather than silent.

## In CI

The composite action takes `foss: true` and uploads the output directory as a
build artifact. It never gates the build. See [ci.md](ci.md).
