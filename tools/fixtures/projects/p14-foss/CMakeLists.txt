cmake_minimum_required(VERSION 3.20)
project(p14_foss C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)

# The FOSS attribution fixture. Every dependency lives under dep/ and carries
# the files an attribution export has to read: a licence text, sometimes a
# NOTICE, sometimes two licences side by side, and sometimes nothing at all.
# None of them has a build file of its own, so the only thing that marks where
# a component begins is the licence file beside its sources (section 19.2).

# A GPL-2.0 code generator. It is compiled and executed during the build and
# no part of it reaches the deliverable, which is the case the whole export
# turns on: a tool that produced a file is not a component of the product.
add_executable(table_gen dep/gpl-gen/table_gen.c)

add_custom_command(
  OUTPUT "${CMAKE_CURRENT_BINARY_DIR}/generated/table.c"
  COMMAND ${CMAKE_COMMAND} -E make_directory "${CMAKE_CURRENT_BINARY_DIR}/generated"
  COMMAND table_gen "${CMAKE_CURRENT_BINARY_DIR}/generated/table.c"
  DEPENDS table_gen
  COMMENT "Generating table.c with the GPL-2.0 generator")

# Three sources, one of them referenced: the other two archive members are
# never extracted and must not appear in the SBOM, while the component root
# stays dep/mit-lib rather than dep/mit-lib/src.
add_library(mit_lib STATIC
  dep/mit-lib/src/mit_a.c
  dep/mit-lib/src/mit_b.c
  dep/mit-lib/src/mit_c.c)
target_include_directories(mit_lib PUBLIC dep/mit-lib/include)

# LICENSE and NOTICE side by side. Both members are extracted.
add_library(apache_lib STATIC
  dep/apache-lib/src/apache_one.c
  dep/apache-lib/src/apache_two.c)
target_include_directories(apache_lib PUBLIC dep/apache-lib/include)

# Statically linked LGPL-2.1: the relinking case. Both members are extracted.
add_library(lgpl_lib STATIC
  dep/lgpl-lib/src/lgpl_core.c
  dep/lgpl-lib/src/lgpl_extra.c)
target_include_directories(lgpl_lib PUBLIC dep/lgpl-lib/include)

# LICENSE-MIT and LICENSE-APACHE beside each other.
add_library(multi_lib STATIC dep/multi-license/src/multi.c)
target_include_directories(multi_lib PUBLIC dep/multi-license/include)

# No licence file and no SPDX identifier anywhere.
add_library(nolicense_lib STATIC dep/nolicense/src/nolicense.c)
target_include_directories(nolicense_lib PUBLIC dep/nolicense/include)

# An SPDX identifier and no copyright holder, in the source or in the licence.
add_library(nocopyright_lib STATIC dep/nocopyright/src/nocopyright.c)
target_include_directories(nocopyright_lib PUBLIC dep/nocopyright/include)

# Header-only: a LICENSE beside an include/ directory, nothing else.
add_library(bsd_hdr INTERFACE)
target_include_directories(bsd_hdr INTERFACE dep/bsd-hdr/include)

fixture_executable(fossapp src/main.c "${CMAKE_CURRENT_BINARY_DIR}/generated/table.c")
target_link_libraries(fossapp PRIVATE
  mit_lib apache_lib lgpl_lib multi_lib nolicense_lib nocopyright_lib bsd_hdr)
