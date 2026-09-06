cmake_minimum_required(VERSION 3.20)
project(p07_pch C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)
# pch_only.h is reached exclusively through the precompiled header: no
# translation unit includes it directly (section 14.5).
fixture_executable(pchapp main.c helper.c)
target_precompile_headers(pchapp PRIVATE
  "${CMAKE_CURRENT_SOURCE_DIR}/pch_only.h"
  "${CMAKE_CURRENT_SOURCE_DIR}/pch_unused.h")
