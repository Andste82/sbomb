# Phase 4 — Components, versions, licences

**Status: complete.** Commit `42992c0`.

## Why here

BSI TR-03183-2 requires a name, a version, a supplier, at least one hash, a
licence, a filename and dependency relationships per component. Four of those
could not be produced.

## Work

* **`componentmap` wired** with the priority chain of §19.2. For the first pass:
  curated configuration, nearest directory carrying package metadata, the anchor
  root, otherwise `unknown:…` with a review flag.
* **Versions §20**: curated, git tag, git SHA and header macro -- the last only
  when `versionFrom` names it explicitly. Never from a directory name.
* **Supplier** from curated configuration or package metadata only; deriving it
  from a git host is expressly forbidden.
* **Licence detection completed**: `tools/spdxgen` generates the digest table of
  the official SPDX texts -- it held two entries where it needed about seven
  hundred -- and `--check` keeps it current in CI.
* **Licence search bounded to component roots.** The parent-directory loop ran
  up to `/` and left the project tree, against the trust boundary of §30.

## Result

698 digests instead of two; current identifiers beat deprecated ones, so
`GPL-2.0-only` no longer comes out as `GPL-2.0`. Held down by an end-to-end test
that builds a real project, because the corpus commits no sources and nothing
there can be hashed.

## Defects this uncovered

* `componentmap.MapFile` reported a match **always**, which made every later
  strategy unreachable.
* Hashing treated the identity `project:main.c` as a filesystem path and
  therefore hashed nothing at all.
* The findings JSON used Go field names instead of the normative keys of §26.1.
