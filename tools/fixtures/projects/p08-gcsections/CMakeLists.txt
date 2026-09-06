cmake_minimum_required(VERSION 3.20)
project(p08_gcsections C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)
# deadcode.c is compiled into the executable and referenced by nothing. With
# per-function sections the linker discards every section it contributed, so
# the map lists it under "Discarded input sections" and never places one of its
# sections. That is what section 4.5 calls fully discarded.
fixture_executable(gcapp main.c live.c deadcode.c)
target_compile_options(gcapp PRIVATE -ffunction-sections -fdata-sections)
target_link_options(gcapp PRIVATE "-Wl,--gc-sections")
