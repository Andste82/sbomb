# Phase 5 — Arming the policy model

**Status: complete.** Commit `1691596`.

## Why here

The policy machinery was finished and handled eighteen finding identifiers. The
pipeline emitted four of them, one of which triggered a gate. `strict` and `cra`
produced the same verdict as `default`, so the profiles decided nothing.

## Work

* **All 27 options of §33.1** settable from the configuration file and from a
  CLI flag, with the precedence of §32.2 -- CLI over profile over configuration
  over default. One table maps flag name, configuration key and field, so the
  three cannot drift apart. An invalid value is an error.
* **`config.Policy` fields became pointers**, because a plain bool cannot tell
  "absent" from "explicitly false": a configuration could only ever turn a gate
  on, never off.
* **Scope options applied before output** (§33.3), which required resolving the
  policy ahead of `generate.RunWithOptions`.
* **The `host-linux` overlay** and `--profile-overlay`.
* **The review report** built out to the nine sections of §34.

## Result

On the same evidence: `lenient` exit 0 with 13 findings, `default` and `cra`
exit 3 with 17, `strict` exit 3 with 18. Toolchain and system components hang
under a synthetic `build-environment` component rather than off the product
(§24.2).

## A test corrected rather than worked around

The acceptance test asserted, citing §33.3, that two profiles must produce
byte-identical documents. That holds only for profiles with the *same scope*:
`strict` deliberately turns on linker scripts and section garbage collection,
which §33.3 expressly exempts. The comparison is now `default` against `cra`,
which differ only in their gates.
