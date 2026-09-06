cmake_minimum_required(VERSION 3.20)
project(p06_unity C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)
# A unity build compiles one generated aggregation file instead of the three
# real sources. Only the generated file appears as a compiler input, so the
# constituents have to be recovered (section 17.1).
fixture_executable(unity main.c mod_a.c mod_b.c)
set_target_properties(unity PROPERTIES UNITY_BUILD ON UNITY_BUILD_BATCH_SIZE 8)
