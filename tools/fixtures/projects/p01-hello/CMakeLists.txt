cmake_minimum_required(VERSION 3.20)
project(p01_hello C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)
fixture_executable(hello main.c)
