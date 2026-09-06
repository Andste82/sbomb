# Cross-compiled PE/COFF, so the corpus covers Windows artifacts without a
# Windows host. DWARF is present because the producer is GCC, not MSVC.
set(CMAKE_SYSTEM_NAME Windows)
set(CMAKE_SYSTEM_PROCESSOR x86_64)
set(CMAKE_C_COMPILER x86_64-w64-mingw32-gcc)
set(CMAKE_RC_COMPILER x86_64-w64-mingw32-windres)
set(CMAKE_FIND_ROOT_PATH /usr/x86_64-w64-mingw32)
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)

# A PE header carries a link timestamp, which is wall clock, so an unchanged
# fixture produced a different .exe on every regeneration. --no-insert-timestamp
# is the standard reproducible-build flag for the GNU PE linker; it changes a
# build input rather than rewriting harvested evidence afterwards.
set(CMAKE_EXE_LINKER_FLAGS_INIT "-Wl,--no-insert-timestamp")
