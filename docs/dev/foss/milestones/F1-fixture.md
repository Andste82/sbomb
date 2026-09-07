### Milestone F1 — Fixture project, source harvest, inventory dump

**Goal:** a fixture that exercises every case the attribution export has to
handle, committed together with the source files the export reads. Without it
the rest of this track is built on guesses: the current corpus contains no
sources, so every golden SBOM says `NOASSERTION` and nothing about licence
behaviour can be observed.

**Depends on:** nothing.

---

**Deliverables**

*The project* — `tools/fixtures/projects/p14-foss/`, a CMake project of
original code, built by the existing harness like every other project:

| Path | Case it covers |
|---|---|
| `src/main.c` | the application; `SPDX-License-Identifier` and a copyright header |
| `dep/mit-lib/` | static archive, three sources, **one linked**; `LICENSE` with a filled-in MIT text and a named holder |
| `dep/apache-lib/` | `LICENSE` **and** `NOTICE`; two sources, both linked |
| `dep/bsd-hdr/` | header-only, included by `main.c`; `LICENSE` BSD-3-Clause |
| `dep/lgpl-lib/` | static archive, LGPL-2.1, statically linked — the relinking case |
| `dep/gpl-gen/` | a generator run by `add_custom_command`, GPL-2.0, **never linked** — the build-time-only case |
| `dep/multi-license/` | `LICENSE-MIT` and `LICENSE-APACHE` side by side |
| `dep/nolicense/` | one linked source, no licence file, no SPDX header |
| `dep/nocopyright/` | one linked source with an SPDX header and no copyright line |

`regen.sh` already turns every `dep/*/` into a git repository with a fixed
identity, date and tag, so the modification cases of F6 come for free. Add one
dependency that is left dirty after the commit, for the `modified=true` case.

*The toolchains* — `p14-foss` runs on `gcc-ninja` and `gcc-make` only, through
the `PROJECT_TOOLCHAINS` mechanism `regen.sh` already has for `p11-conan`
(decision [Q15](../decisions.md)). An LGPL archive and a code generator say
nothing extra when cross-compiled to bare-metal ARM, and the corpus is already
12 MB.

*The harvest* — `tools/fixtures/regen.sh` gains a source harvest for this
project: the files under `$SRC_ROOT` are copied into
`<corpus>/p14-foss-src/`, preserving the layout relative to the sentinel root,
subject to the existing `MAX_FILE_BYTES` refusal. **Once, not per toolchain** —
the sources do not depend on the compiler, and committing them twice would be
waste that grows with every toolchain added. `--check` learns to require
`p14-foss-src/dep/mit-lib/LICENSE` so an incomplete corpus is caught.

*The policy* — `testdata/fixtures/POLICY.md` amended per
[spec-delta.md §12](../spec-delta.md). `PROVENANCE.md` records the origin of
the licence texts.

*`.gitattributes`* — `tools/fixtures/projects/** -text`. The retained bytes are
compared across three platforms in `determinism.yaml`; a CRLF conversion on a
Windows checkout would look like a tool defect.

*`--inventory-dump`* — the flag §32.1 and §40 specify and the CLI does not
implement. `internal/inventory.InventoryFile` already exists
(`internal/inventory/inventory.go:17`); wire it to `generate`. Every milestone
after this one needs it for golden comparison, and the specification already
promises it.

**Tests**

* The project configures and builds under all toolchains the harness runs.
* Exactly the expected object set reaches the artifact: one member from
  `mit-lib`, both from `apache-lib`, both from `lgpl-lib`, none from `gpl-gen`.
  This is the assertion the whole track leans on.
* `regen.sh --check` fails when the harvested source tree is missing.
* Regenerating twice produces the same corpus (`check-reproducible.sh`).
* A golden SBOM for the fixture joins the regression set now, before any FOSS
  code exists. At this point it still resolves no licences — that is correct
  and is what F2 changes.

**Acceptance**

```
tools/fixtures/regen.sh --only gcc-ninja/p14-foss                      # 0
tools/fixtures/regen.sh --check                                        # 0
go test ./tools/fixtures/... -race                                     # 0
sbomb generate --build-dir testdata/fixtures/gcc-ninja/p14-foss/build \
  --output /tmp/f1.cdx.json --inventory-dump /tmp/f1.json --reproducible   # 0
cmp /tmp/f1.cdx.json testdata/golden/gcc-ninja-p14-foss.cdx.json        # 0
```

**Not in this milestone:** any licence retention, any new property or finding.
The corpus gains a project; the tool does not change except for the specified
flag.

**Definition of done:** the fixture builds reproducibly under the existing
harness, its linked-object set is asserted, its source tree is committed and
checked, and `--inventory-dump` produces the document §40 describes.
