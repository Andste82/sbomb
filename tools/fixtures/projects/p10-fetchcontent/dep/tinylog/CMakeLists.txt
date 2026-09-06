cmake_minimum_required(VERSION 3.20)
project(tinylog C)
add_library(tinylog STATIC tinylog.c)
target_include_directories(tinylog PUBLIC ${CMAKE_CURRENT_SOURCE_DIR})
