cmake_minimum_required(VERSION 3.20)
project(p11_conan C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)
# regen.sh runs "conan create" on dep/tinycbor and "conan install" for this
# project before configuring, so the CMakeDeps files below are real
# package-manager evidence rather than something hand-written.
find_package(tinycbor CONFIG REQUIRED)
fixture_executable(conanapp main.c)
target_link_libraries(conanapp PRIVATE tinycbor::tinycbor)
