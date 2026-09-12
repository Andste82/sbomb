# Provenance

Source: tools/fixtures/projects/p14-foss, harvested from $SRC_ROOT by
tools/fixtures/regen.sh. Harvested once, not per toolchain: the bytes do not
depend on the compiler.
License: MIT (the repository licence; the fixture sources are part of it)
Date: 2026-09-05

## The licence texts under dep/

Original fixture material, not third-party payload. Every dependency here was
written for this corpus, and the licence text beside it is the text of the
licence it declares, with a holder invented for the fixture:

| Component | Text | Holder |
|---|---|---|
| dep/mit-lib | MIT | Fixture MIT Library Authors |
| dep/apache-lib | Apache-2.0, with a NOTICE | Fixture Apache Library Authors |
| dep/bsd-hdr | BSD-3-Clause | Fixture BSD Header Authors |
| dep/lgpl-lib | LGPL-2.1 | Fixture LGPL Library Authors |
| dep/gpl-gen | GPL-2.0 | Fixture GPL Generator Authors |
| dep/multi-license | MIT and Apache-2.0, side by side | Fixture Dual Licensed Authors |
| dep/nolicense | none | -- |
| dep/nocopyright | 0BSD, with no copyright line | -- |

The licence texts themselves are the published ones from the SPDX license list
(https://spdx.org/licenses/); the long ones were rendered from the standard
license templates this repository already embeds in
internal/license/spdxtemplates.gz. That is what a real dependency ships. They
are data the tool is meant to recognize, and no code in this repository is
licensed by them.
