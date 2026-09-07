### Milestone F8 — Documentation, catalogues, CI, Action

**Goal:** make it usable, and keep it from drifting.

**Depends on:** F7.

---

**Deliverables**

* `docs/foss.md` — what the four outputs are, what they are not, and the
  CRA/FOSS separation stated plainly:

  * the CRA asks which components are in the artifact, so that vulnerabilities
    can be handled; it says nothing about licence obligations;
  * FOSS obligations come from the licences themselves and applied before the
    CRA existed;
  * they share one data model because they need the same facts, and nothing
    more than that;
  * **sbomb produces data; the manufacturer performs the assessment.** This
    sentence must appear.

  Also: which of the four files ships with the product and which three do not.

  And one sentence on where to ask "why is this in here?" — `sbomb explain
  --component <name>` for whether the component is really in the product, and
  `foss-review.txt` for what sbomb saw about it. `explain` gains no FOSS mode
  (decision [Q20](../decisions.md)): both answers already exist, and adding one
  would mean changing the format of `evidence.json`, which other tools read.

* `docs/findings.md` and `docs/properties.md` **regenerated**, never edited:

  ```
  go run ./tools/findingsdoc && go run ./tools/propertydoc
  ```

  If either produces a diff after Appendix A and B were amended in the earlier
  milestones, the appendix and the code disagree and one of them is wrong.

* The "not built" table, in `docs/foss.md`, as governance:

  | Not built | Why |
  |---|---|
  | Licence compatibility verdicts | a legal judgement; non-goal §1.4 |
  | Source bundles, corresponding source | cannot be derived from usage evidence; producing one would be a compliance defect |
  | Obligation fulfilment tracking | sbomb cannot observe whether a notice actually shipped |
  | Per-licence exemptions for binary distribution | a legal conclusion, even where the licence text is plain |
  | Indicating whether an asset was changed | CC-BY requires it; sbomb answers modification per component, not per embedded blob (decision [Q18](../decisions.md)) |
  | Checking reserved font names | OFL-1.1 restricts naming a modified font; that is a naming rule, not a document rule |
  | Similarity or percentage licence matching | forbidden by §22.3; a score is not evidence |
  | A licence database or network lookup | §30.8 |
  | VEX / CVE mapping | real value, but security rather than FOSS; separate work |

* `docs/getting-started.md` gains the `foss` command; `docs/configuration.md`
  gains `licenseTextInSBOM`. `tools/docexamples` runs over both, so a
  documented configuration that would not load fails CI.

* GitHub Action input `foss: true`, running `sbomb foss` and uploading `<out>/`
  as a build artifact. It must **not** gate the build.

* A CI job asserting the fixture's four output files are unchanged, so the
  notices format cannot drift unnoticed.

* `docs/dev/status.md` and `docs/CHANGELOG.md` updated; the two defect fixes of
  F2 and F3 named there as fixes, because users will see licences appear that
  did not resolve before.

**Tests**

* `go run ./tools/findingsdoc --check` and `--check` for properties: clean.
* `tools/docexamples`: clean.
* The documentation contains no written-out release version (the existing CI
  check).
* An Action run on the fixture uploads four files and exits `0` even when
  components are incomplete.

**Acceptance**

```
go test ./... -race                                                     # 0
go run ./tools/findingsdoc --check                                      # 0
go run ./tools/propertydoc --check                                      # 0
go run ./tools/docexamples                                              # 0
```

**Definition of done:** somebody who has not read this directory can run
`sbomb foss`, understand which file ships and which do not, and see in the
documentation what the tool refuses to decide.
