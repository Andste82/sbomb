cmake_minimum_required(VERSION 3.20)
project(p10_fetchcontent C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)
include(FetchContent)
# regen.sh turns dep/tinylog into a git repository with a fixed commit and the
# tag v1.2.0, so the populated dependency carries real package-manager evidence
# without needing a network.
FetchContent_Declare(tinylog
  GIT_REPOSITORY ${CMAKE_CURRENT_SOURCE_DIR}/dep/tinylog
  GIT_TAG v1.2.0)
FetchContent_MakeAvailable(tinylog)
fixture_executable(fcapp main.c)
target_link_libraries(fcapp PRIVATE tinylog)
