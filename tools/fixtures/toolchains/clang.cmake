# Native Clang with LLD, so the corpus covers the lld map format and lld's
# --dependency-file output alongside GNU ld's.
set(CMAKE_C_COMPILER clang)
set(CMAKE_CXX_COMPILER clang++)
set(CMAKE_EXE_LINKER_FLAGS_INIT "-fuse-ld=lld")
