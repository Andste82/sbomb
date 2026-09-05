cmake_minimum_required(VERSION 3.20)
project(p04_generated C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)
add_custom_command(
  OUTPUT "${CMAKE_CURRENT_BINARY_DIR}/generated_version.c"
  COMMAND ${CMAKE_COMMAND} -DINPUT=${CMAKE_CURRENT_SOURCE_DIR}/version.txt
          -DOUTPUT=${CMAKE_CURRENT_BINARY_DIR}/generated_version.c
          -P ${CMAKE_CURRENT_SOURCE_DIR}/generate.cmake
  DEPENDS version.txt generate.cmake
  COMMENT "Generating generated_version.c from version.txt")
fixture_executable(gen main.c "${CMAKE_CURRENT_BINARY_DIR}/generated_version.c")
