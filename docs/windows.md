# Windows

The release includes a statically linked `windows/amd64` executable. Windows
build paths can be analyzed from Linux with `--path-flavor windows`; this keeps
drive letters and separators stable in the generated evidence and SBOM.

Visual Studio generators and MSVC evidence are not supported yet. Use a
supported CMake generator and GCC or Clang when generating evidence on Windows.
The Microsoft toolchain is milestone 18, planned in
[dev/milestones/phase-09-plan.md](dev/milestones/phase-09-plan.md).