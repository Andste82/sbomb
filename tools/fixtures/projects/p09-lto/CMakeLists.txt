cmake_minimum_required(VERSION 3.20)
project(p09_lto C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)
# Link-time optimization degrades symbol- and section-level attribution, so
# section 17.3 downgrades object-to-source confidence by one level. File-level
# attribution must survive.
fixture_executable(ltoapp main.c helper.c)
# -frandom-seed pins the seed GCC otherwise draws per invocation and embeds in
# the LTO sections, which made the object files and the linker map differ on
# every regeneration. It is a build input, not a rewrite of harvested output,
# and it changes nothing about what the fixture demonstrates.
target_compile_options(ltoapp PRIVATE -flto -frandom-seed=p09-lto)
target_link_options(ltoapp PRIVATE -flto)
