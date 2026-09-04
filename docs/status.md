# Status

- Milestone 00: fixture corpus skeleton created and repository is buildable.
- Milestone 01: CLI skeleton, config validation, path canonicalization, and empty CycloneDX generation are implemented and buildable.
- Milestone 07: Ninja buildgraph parsing and object-to-source resolution are implemented, including evidence-graph integration, strategy priority, conflict recording, and test coverage.

## Next Work

- Milestone 08: implement compile database parsing and compile/header evidence. The planned scope includes `compile_commands.json` command and arguments forms, response files, output mapping, header evidence from depfiles/Ninja/DWARF, toolchain-based header classification, PCH and unity-build handling, and non-C/C++ inputs.
- Milestone 08 acceptance remains outstanding: complete the compiledb and header evidence tests, then verify the `p05-headeronly` inventory against its golden output.
