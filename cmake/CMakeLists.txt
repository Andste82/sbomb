# The entry point FetchContent uses.
#
# This is not a project. It is the module bundle a consumer pulls in so that
# `sbomb_enable` and a matching binary are both there:
#
#   include(FetchContent)
#   FetchContent_Declare(sbomb
#     URL https://github.com/Andste82/sbomb/releases/download/v0.12.0/sbomb-cmake.tar.gz)
#   FetchContent_MakeAvailable(sbomb)
#
#   add_executable(app src/main.c)
#   sbomb_enable(TARGET app POLICY lenient)
#
# CMake functions are global once defined, so sbomb_enable is available in the
# consumer's own lists after MakeAvailable adds this directory.
cmake_minimum_required(VERSION 3.19)

include("${CMAKE_CURRENT_LIST_DIR}/SbombFetch.cmake")
include("${CMAKE_CURRENT_LIST_DIR}/Sbomb.cmake")

# Fetching happens here rather than inside sbomb_enable, so that a configure
# either has the tool or has failed, and no target is defined that would fail
# later for a reason the configure already knew.
sbomb_fetch_binary()
