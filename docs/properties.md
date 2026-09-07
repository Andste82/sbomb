# Properties

Every property sbomb adds to a CycloneDX document is namespaced `sbomb:`.
Booleans are the strings `"true"` and `"false"`, and no value ever contains an
absolute path — a path-valued property carries the canonical identity, anchor
and all.

They exist because CycloneDX has no field for most of what an evidence-based
tool knows: which technique determined a licence, how a component was detected,
what an edge's confidence was and why it was downgraded. A consumer that does
not understand them can ignore them; one that does can audit the answer.

## Catalogue

<!-- BEGIN GENERATED CATALOGUE -->
This build writes 34 of the 72 properties below. The rest are specified and
reserved: they describe evidence this version does not yet record, and no
document sbomb writes will contain them. They are listed and marked so that the
table is the whole catalogue rather than a snapshot of one version.

| Property | Where | Values | Status |
|---|---|---|---|
| `sbomb:artifact:buildId` | Root / artifact components | — | reserved |
| `sbomb:artifact:correlation` | Root / artifact components | correlated | uncorrelated | mismatch | reserved |
| `sbomb:artifact:role` | Root / artifact components | — | emitted |
| `sbomb:build:compilerId` | Run-level (on `metadata.properties`) | — | reserved |
| `sbomb:build:compilerVersion` | Run-level (on `metadata.properties`) | — | reserved |
| `sbomb:build:config` | Run-level (on `metadata.properties`) | — | emitted |
| `sbomb:build:generator` | Run-level (on `metadata.properties`) | — | emitted |
| `sbomb:build:linkerId` | Run-level (on `metadata.properties`) | — | reserved |
| `sbomb:build:linkerVersion` | Run-level (on `metadata.properties`) | — | reserved |
| `sbomb:build:ltoDetected` | Run-level (on `metadata.properties`) | — | reserved |
| `sbomb:build:target` | File components | — | reserved |
| `sbomb:build:targetTriple` | Run-level (on `metadata.properties`) | — | reserved |
| `sbomb:cdx:archiveProperty` | Grouping components | archive | no-archive | emitted |
| `sbomb:cdx:executableProperty` | Grouping components | executable | non-executable | emitted |
| `sbomb:cdx:structuredProperty` | Grouping components | structured | unstructured | emitted |
| `sbomb:component:detectedBy` | Grouping components | — | emitted |
| `sbomb:component:headerOnly` | Grouping components | — | reserved |
| `sbomb:component:root` | Grouping components | — | reserved |
| `sbomb:component:scope` | Grouping components | project | third-party | sdk | toolchain | system | emitted |
| `sbomb:component:vcsCommit` | Grouping components | — | emitted |
| `sbomb:component:vcsDirty` | Grouping components | — | emitted |
| `sbomb:component:vcsTag` | Grouping components | — | reserved |
| `sbomb:evidence:artifacts` | File components | repeated, sorted bom-refs | emitted |
| `sbomb:evidence:confidence` | File components | highest present | reserved |
| `sbomb:evidence:downgrades` | File components | repeated, sorted | reserved |
| `sbomb:evidence:generated:by` | File components | — | reserved |
| `sbomb:evidence:generated:input` | File components | — | reserved |
| `sbomb:evidence:header:class` | File components | — | emitted |
| `sbomb:evidence:header:directInclude` | File components | — | reserved |
| `sbomb:evidence:header:dwarfCovered` | File components | — | reserved |
| `sbomb:evidence:header:includedBy` | File components | repeated, sorted | reserved |
| `sbomb:evidence:header:narrowedByDwarf` | File components | — | reserved |
| `sbomb:evidence:header:viaPch` | File components | — | emitted |
| `sbomb:evidence:link:archive` | File components | — | reserved |
| `sbomb:evidence:link:fullyDiscarded` | File components | — | emitted |
| `sbomb:evidence:link:object` | File components | — | reserved |
| `sbomb:evidence:source` | File components | repeated, sorted | reserved |
| `sbomb:evidence:strength` | File components | strongest present | reserved |
| `sbomb:evidence:type` | File components | repeated, sorted | reserved |
| `sbomb:evidence:unity:parent` | File components | — | reserved |
| `sbomb:file:anchor` | File components | — | reserved |
| `sbomb:file:class` | File components | — | reserved |
| `sbomb:file:headerClass` | File components | — | reserved |
| `sbomb:file:missing` | File components | — | reserved |
| `sbomb:file:path` | File components | — | reserved |
| `sbomb:file:resolvedTarget` | File components | — | reserved |
| `sbomb:file:role` | File components | — | reserved |
| `sbomb:file:size` | File components | — | reserved |
| `sbomb:go:goarch` | Go binary components (`sbomb self`) | — | emitted |
| `sbomb:go:goos` | Go binary components (`sbomb self`) | — | emitted |
| `sbomb:go:mainPackage` | Go binary components (`sbomb self`) | — | emitted |
| `sbomb:go:module` | Go binary components (`sbomb self`) | — | emitted |
| `sbomb:go:moduleSum` | Go binary components (`sbomb self`) | — | emitted |
| `sbomb:go:replaces` | Go binary components (`sbomb self`) | module@version the linker substituted | emitted |
| `sbomb:go:toolchain` | Go binary components (`sbomb self`) | — | emitted |
| `sbomb:license:confidence` | Grouping components | — | reserved |
| `sbomb:license:conflictingValue` | Grouping components | — | emitted |
| `sbomb:license:evidenceClass` | Grouping components | — | emitted |
| `sbomb:license:reason` | Grouping components | — | emitted |
| `sbomb:license:review` | Grouping components | — | emitted |
| `sbomb:license:source` | Grouping components | — | emitted |
| `sbomb:license:technique` | Grouping components | — | emitted |
| `sbomb:path:canonical` | File components | — | emitted |
| `sbomb:review:required` | Grouping components | — | emitted |
| `sbomb:run:adapters` | Run-level (on `metadata.properties`) | — | reserved |
| `sbomb:run:mode` | Run-level (on `metadata.properties`) | — | reserved |
| `sbomb:run:policyProfile` | Run-level (on `metadata.properties`) | — | emitted |
| `sbomb:run:reproducible` | Run-level (on `metadata.properties`) | — | reserved |
| `sbomb:run:sourceDateEpoch` | Run-level (on `metadata.properties`) | — | emitted |
| `sbomb:run:specVersion` | Run-level (on `metadata.properties`) | — | emitted |
| `sbomb:run:timestamp` | Run-level (on `metadata.properties`) | — | emitted |
| `sbomb:run:toolVersion` | Run-level (on `metadata.properties`) | — | emitted |
<!-- END GENERATED CATALOGUE -->

The table is generated from appendix B of the specification by
`go run ./tools/propertydoc` and annotated with what this build writes. A
property cannot be emitted without being catalogued: CI refuses the change.

## The BSI properties

Three properties are required per component by BSI TR-03183-2 and appear on
every component sbomb writes:

| Property | Values |
|---|---|
| `sbomb:cdx:executableProperty` | `executable`, `non-executable` |
| `sbomb:cdx:archiveProperty` | `archive`, `no-archive` |
| `sbomb:cdx:structuredProperty` | `structured`, `unstructured` |

They are derived from the file class, not asserted: a source file is
structured, an archive is an archive, a firmware image is executable.

## Where a property sits

Most properties sit on the component or on `metadata.properties`, as the
catalogue's second column says. Two move at CycloneDX 1.7:
`sbomb:component:vcsCommit` and `sbomb:component:vcsDirty` qualify the
repository URL, and 1.7 gives external references a property bag, so at that
version they sit on the `vcs` reference rather than on the component. At 1.6,
where references have no such bag, they stay on the component.

Where a version came from is not a property either. It is
`component.evidence.identity` with `field: "version"` — the value in
`concludedValue`, the confidence as a number, and the source as one method
whose `value` names it exactly. `sbomb:version:source` and
`sbomb:version:confidence` are gone from the catalogue: they were listed and
never written, and the answer now has a specified field to live in, at both
specification versions.

The URL itself is not a property at all. CycloneDX specifies
`externalReferences` of type `vcs` for it, and a specified field takes
precedence over one in the `sbomb:` namespace — at both versions, since that
reference type predates 1.6. `sbomb:component:vcsUrl` is therefore gone from
the catalogue rather than merely unused.

## Reading a licence answer

Four properties together say how a licence was determined, which is what makes
one reviewable:

| Property | Says |
|---|---|
| `sbomb:license:technique` | `spdx-identifier`, `spdx-digest` or `spdx-template` — which of the permitted techniques answered |
| `sbomb:license:evidenceClass` | `file-level`, `component-level`, `curated`, `unknown` |
| `sbomb:license:source` | Where the text was read, in canonical form |
| `sbomb:license:reason` | Why nothing was concluded, when nothing was |

A component whose licence could not be concluded also carries
`sbomb:license:review=true`. Where the licence texts found in a file are
recorded rather than concluded, they are in `evidence.licenses` and not here.
