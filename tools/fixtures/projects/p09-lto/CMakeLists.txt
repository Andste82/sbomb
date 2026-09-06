cmake_minimum_required(VERSION 3.20)
project(p09_lto C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)
# Link-time optimization degrades symbol- and section-level attribution, so
# section 17.3 downgrades object-to-source confidence by one level. File-level
# attribution must survive.
fixture_executable(ltoapp main.c helper.c)
target_compile_options(ltoapp PRIVATE -flto)
target_link_options(ltoapp PRIVATE -flto)
