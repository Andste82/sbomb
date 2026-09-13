### Milestone F3 — The component root as a resolved fact

**Status: the code landed, ahead of this plan and without its fixture.**
`634ba4a` resolved the root (`resolveRoot`, `internal/generate/components.go:1198`),
populated `domain.Component.Root`, emitted `sbomb:component:root` and
`COMPONENT_ROOT_UNRESOLVED`, and made a recognized licence file a boundary
marker. `bee9a03` removed `resolveLicense`'s parent walk. `2717c1b` replaced
the fixed name list with §22.3 matching, `LICENSE-<id>` included. §19.2 and
§22.3 are amended accordingly.

What is **not** done is the evidence for it: the tests below were written
against the `p14-foss` fixture of F1, which does not exist, so none of them was
run. Treat this milestone as "implemented, unverified against the corpus" and
keep the test list — it is the acceptance criteria for F1, not dead text.

**Goal:** stop guessing where a component begins. Everything the attribution
export retains is read from the component root, so a root that moves with the
link result produces an attribution document that moves with the link result.

**Depends on:** F2.

---

**The two defects**

1. `componentRoot` (`internal/generate/components.go:507`) returns the deepest
   common directory of the component's **used** files. A library with three
   sources of which the linker kept one has the root `dep/mit-lib/src`, and its
   `LICENSE` at `dep/mit-lib/` is never opened. Enable `--gc-sections` and the
   licence a product ships can change. `domain.Component.Root`
   (`internal/domain/domain.go:139`) is declared and never assigned.
2. `resolveLicense` (`internal/generate/generate.go:528`) ascends from a file's
   directory to the filesystem root looking for `LICENSE`, `COPYING` or
   `NOTICE`. §22.1 forbids scanning outside mapped component roots. For an
   identifier this is a wrong answer; for a retained text it would print a
   stranger's licence under this component's name.

**Deliverables**

* §19.2 amended per [spec-delta.md §2](../spec-delta.md): the root is resolved
  in the mapping priority order — curated `components[].path`, package-manager
  root (`pkgmanager.Package.Root`, and `LicenseFile` when the manager placed
  one), git submodule boundary, SDK root, anchor root — and the common
  directory of used files is the **last** fallback.
* **`nearestPackageRoot` becomes the licence lookup's root.** The upward walk of
  strategy 6 already exists (`internal/generate/components.go:184`), already
  stops at the anchor root, and already has a depth cap. Nothing new is
  invented here; the defect is that `componentRoot` recomputes a common
  directory instead of using the answer that walk produced.
* **A recognized licence file joins the marker list** beside `conanfile.txt`,
  `vcpkg.json`, `CMakeLists.txt` and the rest (decision
  [Q7](../decisions.md)). Without it a library copied into the tree with
  nothing but a `LICENSE` and an `include/` directory — the most common shape in
  embedded work — is never recognized as a component and never gets a licence
  text. This is not the layout heuristic §19.2 forbids: that rule is about
  directory *names*, and a licence file is a file with content, like
  `vcpkg.json`.
* `domain.Component.Root` populated. `sbomb:component:root` changes from
  reserved to emitted.
* Finding `COMPONENT_ROOT_UNRESOLVED` (info) when only the fallback applied,
  so a reviewer can see which components have a guessed root.
* `resolveLicense`'s parent walk removed. A file with no own SPDX identifier
  gets its licence from its component, through the component root, with
  evidence class `inherited` and confidence `low` exactly as §22.8 requires.
* The recognized file names follow §22.3 — case-insensitive, optional
  `.txt`/`.md`, and the `LICENSE-<id>` form — rather than a hand-written list.
  *Done in `2717c1b`: `recognizedLicenseFile` and `licenseFilesIn`.*

**Tests**

* `mit-lib`: root is `dep/mit-lib`, not `dep/mit-lib/src`, although only one
  source is linked.
* The same fixture built with and without `--gc-sections` resolves the same
  root and the same licence.
* A curated `components[].path` beats the package-manager root; the
  package-manager root beats the fallback.
* Nested vendoring: a library inside a library maps by longest prefix, and the
  inner component's root is the inner directory. Assert that the outer
  component's `LICENSE` is **not** attributed to the inner one — this is the
  case the old parent walk got wrong.
* `multi-license`: both `LICENSE-MIT` and `LICENSE-APACHE` are recognized.
* `bsd-hdr`, which has only a `LICENSE` and an `include/` directory and no build
  file at all: root is `dep/bsd-hdr`, and the licence text is found.
* Nested licence files: `mbedtls/LICENSE` plus `mbedtls/3rdparty/everest/LICENSE`
  splits `everest` out as its own component. The nearer marker wins.
* The upward walk never leaves the anchor: a component whose root has no licence
  file must not be given the project's own top-level `LICENSE`.
* `COMPONENT_ROOT_UNRESOLVED` is emitted for the unknown component and not for
  the managed ones.

**Acceptance**

```
go test ./internal/generate/... ./internal/componentmap/... -race       # 0
sbomb generate --build-dir testdata/fixtures/gcc-ninja/p14-foss/build \
  --source-dir testdata/fixtures/gcc-ninja/p14-foss/src \
  --inventory-dump /tmp/f3.json --output /dev/null --reproducible       # 0
cmp /tmp/f3.json testdata/golden/gcc-ninja-p14-foss-inventory.json      # 0
go run ./tools/propertydoc --check                                      # 0
```

**Intended golden change:** components gain `sbomb:component:root`, and
licences that the parent walk previously mis-resolved change. Regenerate and
explain each changed component in the commit.

**Not in this milestone:** retaining bytes from the root. F3 establishes where
to read; F4 reads.

**Definition of done:** every component's root is a resolved fact with a named
source, no licence is resolved from outside a component root, and the root does
not depend on which files the linker kept.
