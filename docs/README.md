# Documentation

This directory is for people who run sbomb on their own build. Everything that
is only relevant to working *on* sbomb is under [dev/](dev/).

| Document | Read it when |
|---|---|
| [getting-started.md](getting-started.md) | First run: what your build must produce, the CMake integration, reading the result |
| [architecture.md](architecture.md) | Understanding how the evidence chain works and what backs each step |
| [configuration.md](configuration.md) | Writing the configuration file: artifacts, anchors, curated components |
| [findings.md](findings.md) | A run failed and you need to know why, which flag changes it, and how to waive one |
| [properties.md](properties.md) | Reading the `sbomb:` properties in a generated document |
| [ci.md](ci.md) | Adding sbomb to a GitHub workflow |
| [windows.md](windows.md) | Analysing a Windows build, or analysing one from Linux |
| [CHANGELOG.md](CHANGELOG.md) | What changed between releases |

The README at the repository root covers what sbomb is and the shape of a
typical run; these documents pick up where it stops.

## For contributors

[dev/](dev/) holds the specification, the milestone plan, the recorded
deviations from the specification, the open questions and the implementation
status. Start at [dev/README.md](dev/README.md), which states the gate every
change has to pass.
