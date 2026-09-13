cmake_minimum_required(VERSION 3.20)
project(p15_shared C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)

# The assembly fixture with a component that both deliverables reach, which is
# the case decision Q10 of the FOSS plan exists for. p12-assets has two
# artifacts and their reachable sets are disjoint, so it cannot state it.
#
# dep/shared-lib is one component -- its licence file marks the boundary
# (section 19.2) -- and it arrives in the two deliverables in two different
# ways:
#
#   * app       links its archive member statically, which is the linkage form
#     LGPL-2.1 section 6 attaches the relinking obligation to;
#   * image.img packs one of its data files verbatim, which carries the
#     attribution obligation and not the relinking one.
#
# So "which deliverable pulled in the LGPL component" has two different answers
# for one component, and the obligation differs with them.
add_library(sharedbits STATIC dep/shared-lib/src/shared_bits.c)
target_include_directories(sharedbits PUBLIC dep/shared-lib/include)

fixture_executable(app src/main.c)
target_link_libraries(app PRIVATE sharedbits)

add_custom_command(
  OUTPUT "${CMAKE_CURRENT_BINARY_DIR}/image.img"
  COMMAND "${CMAKE_COMMAND}" -E cat
          "${CMAKE_CURRENT_SOURCE_DIR}/dep/shared-lib/data/table.bin"
          > "${CMAKE_CURRENT_BINARY_DIR}/image.img"
  DEPENDS "${CMAKE_CURRENT_SOURCE_DIR}/dep/shared-lib/data/table.bin"
  VERBATIM
  COMMENT "Packing image.img")

add_custom_target(image ALL DEPENDS "${CMAKE_CURRENT_BINARY_DIR}/image.img")

# The image's inputs are declared where the build writes them, as p12-assets
# does: an image is packed by a command sbomb does not interpret, so what went
# into it is build evidence rather than something inferred from a directory.
file(WRITE "${CMAKE_CURRENT_BINARY_DIR}/sbomb-manifest.json"
"{\n"
"  \"schemaVersion\": 1,\n"
"  \"outputs\": [\n"
"    {\n"
"      \"path\": \"${CMAKE_CURRENT_BINARY_DIR}/image.img\",\n"
"      \"kind\": \"image\",\n"
"      \"inputs\": [\n"
"        { \"path\": \"${CMAKE_CURRENT_SOURCE_DIR}/dep/shared-lib/data/table.bin\", \"role\": \"asset\" }\n"
"      ]\n"
"    }\n"
"  ]\n"
"}\n")
