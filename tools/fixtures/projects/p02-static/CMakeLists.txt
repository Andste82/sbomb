cmake_minimum_required(VERSION 3.20)
project(p02_static C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)
# unused.c is deliberately never referenced: its archive member must not be
# extracted by the linker, and must therefore not appear in the SBOM.
add_library(crypto STATIC crypto.c unused.c)
fixture_executable(app main.c)
target_link_libraries(app PRIVATE crypto)
